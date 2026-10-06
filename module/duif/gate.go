package duif

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

// De gate is de deterministische bronbeslisstap: welke retrieval-kandidaten
// worden bronnen, en wat is de status van deze beurt. De beslissing ligt in
// code, niet bij het model.
//
// Hij is ook de enige plek waar genummerd wordt. RAGRetrieve zoekt altijd in
// twee tabellen (standpunten én citaten) en nummert die door elkaar; de duif
// citeert alleen standpunten. Door hier te filteren, te hernummeren en het
// resultaat terúg in de state te schrijven, rendert de serialize-stap erna
// dezelfde nummers als het sources-frame en het done-frame. Eén bron van
// waarheid voor [1] tot en met [20].

// Reden-codes van de gate (log-only; ze halen de wire niet).
const (
	reasonSourceWithoutPaper = "source_without_paper"
	reasonSourcePropsMissing = "source_props_missing"
)

// De redenen waarom een gevonden kandidaat alsnog geen bron wordt.
//
// Deze gingen vroeger stil: hoogstens een Warn in het serverlog, en op het
// scherm een antwoord met minder bronnen of helemaal geen — zonder dat iemand
// kon zien dat er iets was weggevallen. Voor een begeleider die aan een stand
// moet uitleggen wat er gebeurt, was dat het echte probleem.
//
// Ze halen nu wél de wire, maar als telling en niet als foutmelding: de
// gebruiker leest één neutrale zin, de begeleider ziet in devMode welke reden
// hoe vaak voorkwam.
//
// Het zijn er vier; vroeger waren het er zes. Wat overblijft zijn de harde
// fouten: de bron bestáát niet meer voor de lezer (404 in de graaf), hij was
// niet te lezen, of hij heeft geen tekst om te tonen. De twee
// kwaliteitsoordelen — geen oorspronkelijke passages, afgekeurde citaatgronding
// — laten een kandidaat niet meer vallen; hij gaat mee als samenvattingsbron en
// draagt dat zichtbaar op zijn bronkaart.
const (
	// dropEmptyText: een standpunt zonder tekst kan niets dragen.
	dropEmptyText = "no_text"
	// dropOtherKB: een standpunt uit een vórige kennisbank; de graaf verbergt
	// het als 404.
	dropOtherKB = "other_knowledge_base"
	// dropEvidenceUnreadable: de graafcontext van deze kandidaat was niet te
	// lezen; een samenvatting mag die uitval niet stilletjes opvullen.
	dropEvidenceUnreadable = "evidence_unreadable"
	// dropPartialSearch: één van de twee zoektabellen viel om terwijl er
	// bruikbare bronnen overbleven.
	dropPartialSearch = "partial_search"
)

// SourceDrops is wat er wegviel vóór de bronlijst. Count is voor de gebruiker,
// Reasons voor de begeleider. Hij is diagnostiek van één beurt en wordt niet
// opgeslagen: bij herladen zou hij een uitkomst beschrijven die niet meer te
// controleren valt.
type SourceDrops struct {
	Count   int            `json:"count"`
	Reasons map[string]int `json:"reasons,omitempty"`
}

// drop boekt één weggevallen kandidaat.
func (s *turnState) drop(reason string) {
	if s.drops == nil {
		s.drops = map[string]int{}
	}
	s.drops[reason]++
}

// dropsOf maakt de wire-vorm, of nil wanneer er niets wegviel.
func dropsOf(counts map[string]int) *SourceDrops {
	total := 0
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return nil
	}
	reasons := make(map[string]int, len(counts))
	for reason, n := range counts {
		reasons[reason] = n
	}
	return &SourceDrops{Count: total, Reasons: reasons}
}

// gateStep filtert, hernummert, verrijkt met het thema en bouwt de wire-DTO.
func (r *retrieval) gateStep(ctx context.Context, s any) error {
	st, ok := s.(*turnState)
	if !ok {
		// De DAG geeft state als any door; een verkeerd type is een bouwfout,
		// geen paniek waard.
		return fmt.Errorf("duif: gate kreeg %T in plaats van *turnState", s)
	}
	ist := st.Interaction()
	rec, _ := ctx.Value(searchRecorderKey{}).(*searchRecorder)
	activeKB := r.activeKB()

	kept := make([]interaction.RAGSource, 0, len(ist.RAGSources))
	grounding := []string{}
	type candidate struct {
		source interaction.RAGSource
		proof  string
	}
	var readable, legacy []candidate
	// Alles wat boven de drempel kwam is relevant; maxAnswerSources zet er een
	// plafond omheen. Readable original passages come before
	// summary-only legacy records: een bron die haar eigen woorden kan laten
	// zien staat vóór een bron met alleen een samenvatting, ook als die laatste
	// hoger scoort.
	// No generated sentence or required citation is clipped after generation.
	// Scores rank candidates, not scientific quality; ties use stable IDs.
	ranked := append([]interaction.RAGSource(nil), ist.RAGSources...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return string(ranked[i].ID) < string(ranked[j].ID)
	})
	for _, src := range ranked {
		if src.Type != tableDestillaat {
			continue
		}
		if rec.contextFailed(src.ID) {
			st.drop(dropEvidenceUnreadable)
			continue // never promote a summary fallback after an evidence outage
		}
		if strings.TrimSpace(src.Text) == "" {
			// Een standpunt zonder tekst kan niets dragen: het staat als lege
			// bronkaart op het scherm en als leeg nummer in de prompt.
			st.drop(dropEmptyText)
			continue
		}
		props := rec.node(src.ID)
		if props == nil {
			// Zonder props valt er niets te toetsen. De bron laten vallen zou
			// van een lege recorder een permanent kennisgat maken; dit is een
			// datafout, geen reden om te zwijgen.
			r.log().Warn("bron zonder props uit de zoekopdracht", "component", "duif", "reason", reasonSourcePropsMissing)
		} else if activeKB != "" {
			if got, _ := props["kb_id"].(string); got != activeKB {
				// Een standpunt van een vórige kennisbank: de graaf verbergt
				// hem als 404, dus citeren zou naar een knoop wijzen die
				// volgens de kennisgraaf niet bestaat.
				st.drop(dropOtherKB)
				continue
			}
		}
		// Bundles in v2 declare original quote grounding explicitly. Een v2-bron zonder
		// leesbare oorspronkelijke passages is een datafout in de bundel en
		// blijft dus een logregel waard — maar geen reden om de bron te laten
		// vallen. Hij gaat mee als samenvattingsbron, achter de
		// bronnen die hun eigen woorden wél kunnen laten zien.
		// duiven/3 is dezelfde graaf met twee talen en valt onder dezelfde eis.
		if kbStructuredNode(props) && (src.Context == nil || len(src.Context.Quotes) == 0) {
			r.log().Warn("v2 source has no original passages", "component", "duif", "reason", "source_original_passages_missing")
		}
		var proof string
		src, proof = r.filterGrounding(ctx, src, rec)
		if proof == groundingUnreadable {
			// Niet te lezen is niet hetzelfde als afgekeurd: hier is de controle
			// niet eens gelukt, en een samenvatting mag die uitval niet
			// stilletjes opvullen. Dezelfde reden als een gevallen graafcontext.
			st.drop(dropEvidenceUnreadable)
			continue
		}
		if proof == groundingRejected {
			// Afgekeurde gronding betekent: déze passages zijn niet terug te
			// vinden in het oorspronkelijke stuk. De bron zelf blijft staan, de
			// passages gaan er af — als bewijs zijn ze waardeloos, en zonder
			// passages is dit precies een samenvattingsbron. De prompt en de
			// bronkaart zeggen het er allebei bij (hieronder en `Grounding`).
			r.log().Warn("bron niet aantoonbaar gegrond", "component", "duif", "reason", "source_grounding_rejected")
			if src.Context != nil {
				stripped := *src.Context
				stripped.Quotes = nil
				src.Context = &stripped
			}
		}
		fillTheme(&src, props)
		if src.Context != nil && len(src.Context.Quotes) > 0 {
			readable = append(readable, candidate{src, proof})
		} else {
			legacy = append(legacy, candidate{src, proof})
		}
	}
	// Preserve honest legacy cards when they do not displace usable evidence.
	// Their lack of original passages remains explicit in the editor input.
	chosen := append(readable, legacy...)
	for i, item := range chosen[:min(len(chosen), maxAnswerSources)] {
		item.source.Number = i + 1
		kept = append(kept, item.source)
		grounding = append(grounding, item.proof)
	}

	ist.RAGSources = kept
	// Citatienummers horen alleen te bestaan als er bronnen zijn om naar te
	// wijzen; zonder bronnen mag de prompt er niet eens over beginnen.
	ist.UseCitations = len(kept) > 0
	st.sources = r.wireSources(rec, kept)
	r.hydrateBibliography(ctx, kept, st.sources)
	r.hydratePerspective(ctx, kept, st.sources)
	for i, proof := range grounding {
		st.sources[i].Grounding = proof
		// Keep the editorial summary in the source card/graph, but do not give
		// it to the model as competing evidence. Live answers showed summaries
		// that silently removed "might" and a study's location. The original
		// passages survive the serializer's separate quotes section.
		if ist.RAGSources[i].Context != nil && len(ist.RAGSources[i].Context.Quotes) > 0 {
			ist.RAGSources[i].Text = "Source reference: " + st.sources[i].Ref.Paper + ". The original research passages below are the evidence for this source."
			// These are editorial categories, not attributes observed in birds
			// or additional scientific positions. Keep them in wire metadata.
			projected := *ist.RAGSources[i].Context
			projected.Destillaat = data.Destillaat{}
			projected.Related = nil
			projected.Personas = nil
			projected.Perspective = nil
			projected.Thema = nil
			ist.RAGSources[i].Context = &projected
		}
		if proof == groundingUnverified {
			ist.RAGSources[i].Text = "[Extraction grounding is unverified; do not present this as independently checked evidence.] " + ist.RAGSources[i].Text
		}
		if proof == groundingRejected {
			// Sterker dan `unverified`: hier is de controle gedáán en afgekeurd.
			// De passages zijn al weg; deze regel zorgt dat de samenvatting die
			// overblijft niet als gecontroleerd bewijs gelezen wordt.
			ist.RAGSources[i].Text = "[The original passages of this source could not be traced back to the paper and were removed; this summary is unchecked and may not be presented as verified evidence.] " + ist.RAGSources[i].Text
		}
	}
	if len(kept) == 0 {
		st.status = KBGap
		return nil
	}
	st.status = KBSources
	return nil
}

// activeKB is de kennisbank waar de import zijn marker voor schreef — dezelfde
// waar de graaf op filtert. Leeg betekent "niet te bepalen" (geen importer, of
// een kennisbank zonder marker): dan filtert de gate niet, want alles wegfilteren
// zou van een onbekende stand een kennisgat maken.
func (r *retrieval) activeKB() string {
	if r.status == nil {
		return ""
	}
	return r.status().KBID
}

// fillTheme hangt het gedenormaliseerde thema aan een bron. De props komen uit
// de zoekopdracht zelf: SemanticResult.Node draagt ze al, zónder de embedding
// van ± 25 KB die een GetNode per bron wél zou meeslepen. Core's typed read
// (data.Destillaat) laat `thema_titel` vallen, dus dit gaat via de rauwe props.
func fillTheme(src *interaction.RAGSource, props map[string]any) {
	titel := themeTitle(props)
	if titel == "" {
		return
	}
	if src.Context == nil {
		// Buiten ContextDepth (of na een gevallen GetWithContext) is er geen
		// context-struct; het thema hoort er toch bij te kunnen.
		src.Context = &data.DestillaatContext{}
	}
	src.Context.Thema = &data.Thema{Titel: titel}
}

// themeTitle leest de themanaam uit de rauwe props: eerst het bij de import
// gedenormaliseerde `thema_titel`, anders het eerste primaire thema.
func themeTitle(props map[string]any) string {
	if s, ok := props["thema_titel"].(string); ok && s != "" {
		return s
	}
	return firstString(props["primary_themes"])
}

// firstString geeft het eerste element van een lijst uit de graaf. Props komen
// als JSON binnen ([]any), maar een in-memory bron kan []string leveren.
func firstString(v any) string {
	switch list := v.(type) {
	case []any:
		if len(list) > 0 {
			if s, ok := list[0].(string); ok {
				return s
			}
		}
	case []string:
		if len(list) > 0 {
			return list[0]
		}
	}
	return ""
}

// wireSources maakt de wire-DTO's. Scores verlaten de server nooit: ze sturen
// de drempel en zijn verder een intern getal dat op het scherm alleen maar
// autoriteit suggereert. De node-id gaat wél mee — hij opent het standpunt in
// de kennisgraaf.
// kbStructuredNode zegt of een knoop uit een gevalideerde duiven-bundel komt
// (duiven/2 of duiven/3). Alleen die knopen dragen de expliciete
// grondingsverklaring waar de gate op steunt.
func kbStructuredNode(props map[string]any) bool {
	schema, _ := props["bundle_schema"].(string)
	return schema == kbV2Schema || schema == kbV3Schema
}

func (r *retrieval) wireSources(rec *searchRecorder, sources []interaction.RAGSource) []Source {
	if len(sources) == 0 {
		return nil
	}
	out := make([]Source, 0, len(sources))
	for _, src := range sources {
		s := Source{
			Number: src.Number,
			Type:   tableDestillaat,
			ID:     string(src.ID),
			Text:   src.Text,
		}
		// De rauwe props van het standpunt dragen naast de brontaal ook de
		// vertaling; ze zijn bij de zoekopdracht al gelezen, dus dit kost geen
		// extra query. De samenvatting en het thema komen er allebei uit.
		props := rec.node(src.ID)
		if props != nil {
			s.SourceLanguage, _ = props["source_language"].(string)
			localized := SourceLocalized{
				Text:  bilingualProp(props, "samenvatting"),
				Theme: bilingualProp(props, "thema_titel"),
			}
			if localized.Text != nil || localized.Theme != nil {
				s.Localized = &localized
			}
		}
		if c := src.Context; c != nil {
			if c.Perspective != nil {
				s.Perspective = c.Perspective.Naam
			}
			if c.Thema != nil {
				s.Theme = c.Thema.Titel
			}
			if len(c.Quotes) > 0 {
				s.Ref.Quote = c.Quotes[0].Text
				for _, quote := range c.Quotes {
					s.Ref.Passages = append(s.Ref.Passages, SourcePassage{ID: string(quote.ID), Text: quote.Text})
				}
			}
			if c.Interview != nil {
				s.Ref.ID = string(c.Interview.ID)
				s.Ref.Paper = c.Interview.Title
			}
		}
		if s.Ref.Paper == "" {
			// Elk standpunt hoort uit een bronpaper te komen (CONTAINS). Geen
			// paper is een datafout in de import, geen reden om de bron te
			// laten vallen: de terugverwijzing wordt alleen korter.
			r.log().Warn("bron zonder bronpaper", "component", "duif", "reason", reasonSourceWithoutPaper, "nummer", s.Number)
		}
		out = append(out, s)
	}
	return out
}
