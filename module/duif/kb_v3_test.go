package duif

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

// De duiven/3-fixture is dezelfde synthetische graaf als de duiven/2-fixture,
// met de vertaalde velden in hun tweetalige vorm. Alles wat hier verzonnen is,
// is voor deze test verzonnen; er staat geen tekst uit de echte kennisbank in.

// kbV3LocalizedFixtureFields is per label wat de bundel vertaalt. Het is
// bewust een aparte lijst in de test: hij hoort gelijk te lopen met het
// contract in module/README.md ("Het bundelformaat duiven/3"), en als
// de code hem zelf zou leveren toetst de test alleen zichzelf.
var kbV3LocalizedFixtureFields = map[string][]string{
	"Domein":      {"titel", "beschrijving", "kernconcepten"},
	"Thema":       {"titel", "beschrijving", "kernconcepten"},
	"Subthema":    {"titel", "beschrijving", "kernconcepten"},
	"Perspectief": {"naam", "kernstandpunt", "beschrijving"},
	"Destillaat":  {"samenvatting"},
}

// kbV3Pair maakt van één brontaalwaarde het tweetalige paar. Het voorvoegsel
// maakt de Engelse kant herkenbaar zonder dat er een echte vertaling nodig is.
func kbV3Pair(value any) any {
	switch v := value.(type) {
	case string:
		return map[string]any{kbLangSource: v, kbLangEnglish: "EN " + v}
	case []any:
		english := make([]any, 0, len(v))
		for _, item := range v {
			english = append(english, "EN "+item.(string))
		}
		return map[string]any{kbLangSource: v, kbLangEnglish: english}
	}
	return value
}

// kbV3Base is de duiven/2-fixture met élk vertaalbaar veld gevuld, zodat de
// tweetalige variant erover heen gelegd kan worden zonder gaten.
func kbV3Base(t *testing.T) kbV2Document {
	t.Helper()
	doc := kbV2Fixture(t)
	for _, label := range []string{"Domein", "Thema", "Subthema"} {
		doc.Nodes[label][0]["beschrijving"] = "Synthetische beschrijving."
		doc.Nodes[label][0]["kernconcepten"] = []any{"synthetisch concept"}
	}
	doc.Nodes["Perspectief"][0]["kernstandpunt"] = "Synthetisch kernstandpunt"
	doc.Nodes["Perspectief"][0]["beschrijving"] = "Synthetische beschrijving."
	kbV2RefreshStatistics(&doc)
	return doc
}

func kbV3Fixture(t *testing.T) kbV2Document {
	t.Helper()
	doc := kbV3Base(t)
	doc.Schema = kbV3Schema
	doc.Languages = []string{kbLangSource, kbLangEnglish}
	doc.SourceLanguage = kbLangSource
	for label, fields := range kbV3LocalizedFixtureFields {
		for _, row := range doc.Nodes[label] {
			for _, field := range fields {
				row[field] = kbV3Pair(row[field])
			}
		}
	}
	kbV2RefreshStatistics(&doc)
	return doc
}

func kbV3Write(t *testing.T, doc kbV2Document) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "kb.json")
	if err := os.WriteFile(name, kbV2Encode(t, doc), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func kbV3Parse(t *testing.T, doc kbV2Document) *kbBundle {
	t.Helper()
	mapped, err := parseKBV2JSON(kbV2Encode(t, doc))
	if err != nil {
		t.Fatalf("duiven/3-bundel afgekeurd: %v", err)
	}
	return mapped
}

func kbNodeByTable(t *testing.T, b *kbBundle, table, exportID string) kbNode {
	t.Helper()
	id := derived(kbV2Namespace, table, exportID)
	for _, n := range b.Structure {
		if n.ID == id {
			return n
		}
	}
	for _, d := range b.Destillaten {
		if d.Node.ID == id {
			return d.Node
		}
	}
	t.Fatalf("knoop %s ontbreekt", id)
	return kbNode{}
}

// Een duiven/3-bundel landt met beide talen in de graaf: de brontaal in de
// bestaande kolom, het Engels in de zijspoorprop ernaast.
func TestKBV3StoresBothLanguagesInSidecarProps(t *testing.T) {
	b := kbV3Parse(t, kbV3Fixture(t))
	if b.Schema != kbV3Schema || b.SourceLanguage != kbLangSource || !reflect.DeepEqual(b.Languages, []string{kbLangSource, kbLangEnglish}) {
		t.Fatalf("bundelkop = %q / %q / %v", b.Schema, b.SourceLanguage, b.Languages)
	}
	theme := kbNodeByTable(t, b, tableThema, "theme-1")
	if theme.Props["titel"] != "Synthetic theme" || theme.Props["titel_en"] != "EN Synthetic theme" {
		t.Fatalf("thematitel = %v / %v", theme.Props["titel"], theme.Props["titel_en"])
	}
	if !reflect.DeepEqual(theme.Props["kernconcepten"], []any{"synthetisch concept"}) ||
		!reflect.DeepEqual(theme.Props["kernconcepten_en"], []any{"EN synthetisch concept"}) {
		t.Fatalf("kernconcepten = %v / %v", theme.Props["kernconcepten"], theme.Props["kernconcepten_en"])
	}
	if theme.Props["bundle_schema"] != kbV3Schema || theme.Props["source_language"] != kbLangSource {
		t.Fatalf("knoop mist schema of brontaal: %v", theme.Props)
	}
	perspective := kbNodeByTable(t, b, tablePerspective, "perspective-1")
	if perspective.Props["naam"] != "Synthetic perspective" || perspective.Props["naam_en"] != "EN Synthetic perspective" {
		t.Fatalf("perspectiefnaam = %v", perspective.Props)
	}
	dest := kbNodeByTable(t, b, tableDestillaat, "dest-1")
	if dest.Props["samenvatting"] != "Synthetic editorial summary." || dest.Props["samenvatting_en"] != "EN Synthetic editorial summary." {
		t.Fatalf("samenvatting = %v", dest.Props)
	}
	// Het gedenormaliseerde thema draagt dezelfde twee talen.
	if dest.Props["thema_titel"] != "Synthetic theme" || dest.Props["thema_titel_en"] != "EN Synthetic theme" {
		t.Fatalf("thema_titel = %v / %v", dest.Props["thema_titel"], dest.Props["thema_titel_en"])
	}
}

// Er wordt op het Engels geëmbed: de samenvatting in het Engels plus het
// (al Engelse) citaat. De Nederlandse tekst hoort er niet in te staan.
func TestKBV3EmbedsOnTheEnglishSummary(t *testing.T) {
	v3 := kbV3Parse(t, kbV3Fixture(t))
	embed := v3.Destillaten[0].Node.EmbedText
	if !strings.HasPrefix(embed, "EN Synthetic editorial summary.") {
		t.Fatalf("embed-tekst begint niet met de Engelse samenvatting: %q", embed)
	}
	if strings.Contains(embed, "\nSynthetic editorial summary.") {
		t.Fatalf("embed-tekst draagt ook de brontaal: %q", embed)
	}
	if !strings.Contains(embed, "The synthetic study reported a limited association.") {
		t.Fatalf("embed-tekst mist het citaat: %q", embed)
	}
	// Ter controle: duiven/2 embedt onveranderd op de enige taal die er is.
	v2 := kbV3Parse(t, kbV3Base(t))
	if !strings.HasPrefix(v2.Destillaten[0].Node.EmbedText, "Synthetic editorial summary.") {
		t.Fatalf("duiven/2 embedt niet meer op zijn eigen samenvatting: %q", v2.Destillaten[0].Node.EmbedText)
	}
}

// Alles wat niet vertaald wordt, komt uit een duiven/3-bundel exact zo de graaf
// in als uit de duiven/2-bundel waar hij uit voortkomt.
func TestKBV3LeavesUntranslatedPropertiesByteIdentical(t *testing.T) {
	v2 := kbV3Parse(t, kbV3Base(t))
	v3 := kbV3Parse(t, kbV3Fixture(t))
	localized := map[string]bool{}
	for _, fields := range kbV3LocalizedFixtureFields {
		for _, field := range fields {
			localized[field] = true
		}
	}
	// Het gedenormaliseerde thema komt uit een vertaald veld en verschilt dus
	// wél; de brontaalkant ervan is hierboven al getoetst.
	localized["thema_titel"] = true
	for table, exportID := range map[string]string{
		tableInterview: "source-1", tableQuote: "quote-1", tableThema: "theme-1",
		tableSubthema: "sub-1", tableDomein: "domain-1", tablePerspective: "perspective-1",
		tableDestillaat: "dest-1",
	} {
		before := kbNodeByTable(t, v2, table, exportID).Props
		after := kbNodeByTable(t, v3, table, exportID).Props
		for key, want := range before {
			// bundle_schema en import_hash zijn herkomst van de import zelf,
			// geen inhoud van de bundel: die hóren te verschillen.
			if localized[key] || key == "bundle_schema" || key == "import_hash" {
				continue
			}
			if got := after[key]; !reflect.DeepEqual(got, want) {
				t.Fatalf("%s.%s = %v, wil onveranderd %v", table, key, got, want)
			}
		}
		// De vertaling voegt alleen toe; een onvertaald veld mag niet
		// verdwijnen doordat het in duiven/3 anders gelezen wordt.
		for key := range before {
			if _, exists := after[key]; !exists {
				t.Fatalf("%s verliest prop %q in duiven/3", table, key)
			}
		}
	}
	// De citaattekst is het origineel en gaat nooit door de vertaalmolen.
	if kbNodeByTable(t, v3, tableQuote, "quote-1").Props["text"] != "The synthetic study reported a limited association." {
		t.Fatal("citaattekst veranderd")
	}
}

// Een onvolledige vertaling is geen bundel. Elk van deze gevallen hoort te
// stranden op de validatie, niet stil half te landen.
func TestKBV3RejectsAnIncompleteTranslation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*kbV2Document)
	}{
		{"engels ontbreekt", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["titel"] = map[string]any{kbLangSource: "Alleen Nederlands"}
		}},
		{"engels leeg", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["titel"] = map[string]any{kbLangSource: "Nederlands", kbLangEnglish: "   "}
		}},
		{"brontaal ontbreekt", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["titel"] = map[string]any{kbLangEnglish: "Only English"}
		}},
		{"kale string", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["titel"] = "Een kale string"
		}},
		{"derde taal", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["titel"] = map[string]any{kbLangSource: "Nederlands", kbLangEnglish: "English", "de": "Deutsch"}
		}},
		{"lijst ongelijk lang", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["kernconcepten"] = map[string]any{kbLangSource: []any{"een", "twee"}, kbLangEnglish: []any{"one"}}
		}},
		{"lijst als string", func(d *kbV2Document) {
			d.Nodes["Thema"][0]["kernconcepten"] = map[string]any{kbLangSource: "een", kbLangEnglish: "one"}
		}},
		{"verplichte samenvatting leeg", func(d *kbV2Document) {
			d.Nodes["Destillaat"][0]["samenvatting"] = map[string]any{kbLangSource: "  ", kbLangEnglish: "English"}
		}},
		{"talenkop ontbreekt", func(d *kbV2Document) { d.Languages = nil }},
		{"brontaal engels", func(d *kbV2Document) { d.SourceLanguage = kbLangEnglish }},
		{"onbekende taal in de kop", func(d *kbV2Document) {
			d.Languages = []string{kbLangSource, "de"}
		}},
		{"taal dubbel in de kop", func(d *kbV2Document) {
			d.Languages = []string{kbLangSource, kbLangSource}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := kbV3Fixture(t)
			tc.mutate(&doc)
			kbV2RefreshStatistics(&doc)
			if got, err := parseKBV2JSON(kbV2Encode(t, doc)); got != nil || err == nil || !strings.HasPrefix(err.Error(), "duif: kb_v2_") {
				t.Fatal("onvolledige vertaling overleefde de validatie")
			}
		})
	}
}

// Een duiven/2-bundel met een talenkop is waarschijnlijk een half vertaalde
// bundel met het oude etiket; die hoort te stranden in plaats van stil de
// Nederlandse helft te importeren.
func TestKBV2WithALanguageHeaderIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*kbV2Document)
	}{
		{"languages", func(d *kbV2Document) { d.Languages = []string{kbLangSource, kbLangEnglish} }},
		{"source_language", func(d *kbV2Document) { d.SourceLanguage = kbLangSource }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := kbV3Base(t)
			tc.set(&doc)
			if got, err := parseKBV2JSON(kbV2Encode(t, doc)); got != nil || err == nil || !strings.Contains(err.Error(), reasonV2Language) {
				t.Fatalf("duiven/2 met talenkop geaccepteerd: %v", err)
			}
		})
	}
}

// Een duiven/2-bundel importeert onder de nieuwe mapper precies zoals eerst:
// geen zijspoorprops, geen brontaal, en de tekstvelden onveranderd.
func TestKBV2ImportsUnchangedUnderTheNewMapper(t *testing.T) {
	b := kbV3Parse(t, kbV3Base(t))
	theme := kbNodeByTable(t, b, tableThema, "theme-1")
	if theme.Props["titel"] != "Synthetic theme" {
		t.Fatalf("thematitel = %v", theme.Props["titel"])
	}
	if theme.Props["bundle_schema"] != kbV2Schema {
		t.Fatalf("schemamarkering = %v", theme.Props["bundle_schema"])
	}
	for _, key := range []string{"titel_en", "beschrijving_en", "kernconcepten_en", "source_language"} {
		if _, exists := theme.Props[key]; exists {
			t.Fatalf("duiven/2 kreeg een taalprop %q", key)
		}
	}
	if b.Languages != nil || b.SourceLanguage != "" {
		t.Fatalf("duiven/2-bundel draagt talen: %v / %q", b.Languages, b.SourceLanguage)
	}
}

// Vectorbehoud kijkt naar de tekst waarop geëmbed is. Een nieuwe Nederlandse
// vertaling naast een onveranderd Engels origineel hoeft geen nieuwe vector;
// een gewijzigde Engelse tekst wél.
func TestKBV3EmbeddingReuseKeysOnTheEnglishSummary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(map[string]any)
		reuse bool
	}{
		{"alleen de Nederlandse tekst wijzigt", func(v map[string]any) { v[kbLangSource] = "Andere Nederlandse samenvatting." }, true},
		{"de Engelse tekst wijzigt", func(v map[string]any) { v[kbLangEnglish] = "Other English summary." }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &updateKnowledge{fakeKnowledge: &fakeKnowledge{}}
			logger, _ := captureLogs()
			imp := testImporter(w, logger, false)
			doc := kbV3Fixture(t)
			imp.run(context.Background(), kbV3Write(t, doc))
			if st := imp.snapshot(); st.State != ImportReady {
				t.Fatalf("eerste import: %+v", st)
			}
			w.updates = 0
			tc.edit(doc.Nodes["Destillaat"][0]["samenvatting"].(map[string]any))
			imp.run(context.Background(), kbV3Write(t, doc))
			if st := imp.snapshot(); st.State != ImportReady {
				t.Fatalf("herimport: %+v", st)
			}
			if reused := w.updates == 1; reused != tc.reuse {
				t.Fatalf("hergebruikte vectoren = %v, wil %v (updates=%d)", reused, tc.reuse, w.updates)
			}
		})
	}
}

// De overstap van duiven/2 naar duiven/3 verlegt de embed-tekst van het
// Nederlands naar het Engels. De oude vectoren zijn dan niet vergelijkbaar, ook
// niet als de Nederlandse samenvatting bytegelijk is gebleven.
func TestKBV3DoesNotReuseVectorsFromADutchImport(t *testing.T) {
	w := &updateKnowledge{fakeKnowledge: &fakeKnowledge{}}
	logger, _ := captureLogs()
	imp := testImporter(w, logger, false)
	imp.run(context.Background(), kbV3Write(t, kbV3Base(t)))
	if st := imp.snapshot(); st.State != ImportReady {
		t.Fatalf("duiven/2-import: %+v", st)
	}
	w.updates = 0
	imp.run(context.Background(), kbV3Write(t, kbV3Fixture(t)))
	if st := imp.snapshot(); st.State != ImportReady {
		t.Fatalf("duiven/3-import: %+v", st)
	}
	if w.updates != 0 {
		t.Fatalf("%d standpunt(en) behielden een Nederlandse vector", w.updates)
	}
}

// De marker legt vast wélk formaat er binnengekomen is; dat is wat een
// herimport vóór en ná vergelijkbaar maakt.
func TestKBV3MarkerRecordsSchemaAndLanguages(t *testing.T) {
	w := &updateKnowledge{fakeKnowledge: &fakeKnowledge{}}
	logger, _ := captureLogs()
	imp := testImporter(w, logger, false)
	imp.run(context.Background(), kbV3Write(t, kbV3Fixture(t)))
	markers, err := imp.readMarkers(context.Background())
	if err != nil || len(markers) != 1 {
		t.Fatalf("markers = %v (%v)", markers, err)
	}
	m := markers[0]
	if m.BundleSchema != kbV3Schema || m.SourceLanguage != kbLangSource ||
		!reflect.DeepEqual(m.Languages, []string{kbLangSource, kbLangEnglish}) {
		t.Fatalf("marker = %+v", m)
	}
	if m.MapperVersion != kbMapperVersion {
		t.Fatalf("mapperversie = %d, wil %d", m.MapperVersion, kbMapperVersion)
	}
}

// De graaf stuurt een vertaald veld als taalobject en een onvertaald veld als
// kale string. Zonder dat verschil kan de client niet zien wat het origineel is.
func TestKBV3GraphNodeCarriesBothLanguagesAndTheSourceLanguage(t *testing.T) {
	props := map[string]any{
		"samenvatting": "Nederlandse samenvatting.", "samenvatting_en": "English summary.",
		"thema_titel": "Overlast", "thema_titel_en": "Nuisance",
		"scope": "stad", "source_language": kbLangSource, "bundle_schema": kbV3Schema,
	}
	n := graphNodeOf(gid(tableDestillaat, 1), props)
	if n.SourceLanguage != kbLangSource {
		t.Fatalf("brontaal = %q", n.SourceLanguage)
	}
	if got, want := n.Label, (Bilingual{kbLangSource: "Nederlandse samenvatting.", kbLangEnglish: "English summary."}); !reflect.DeepEqual(got, want) {
		t.Fatalf("label = %v, wil %v", got, want)
	}
	if got, want := n.Metadata["text"], (Bilingual{kbLangSource: "Nederlandse samenvatting.", kbLangEnglish: "English summary."}); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata.text = %v, wil %v", got, want)
	}
	if got, want := n.Metadata["thema_titel"], (Bilingual{kbLangSource: "Overlast", kbLangEnglish: "Nuisance"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata.thema_titel = %v, wil %v", got, want)
	}
	// De opsommingswaarde blijft een kale string: die wordt niet vertaald.
	if n.Metadata["scope"] != "stad" {
		t.Fatalf("scope = %v", n.Metadata["scope"])
	}
	// Het citaat is het origineel en draagt geen taalobject.
	quote := graphNodeOf(gid(tableQuote, 1), map[string]any{"text": "The original passage.", "speaker": "R.", "source_language": kbLangSource})
	if quote.Label != "The original passage." || quote.Metadata["text"] != "The original passage." {
		t.Fatalf("citaat werd tweetalig gemaakt: %+v", quote)
	}
}

// Pick valt terug op de taal die er wél is; een leeg scherm is nooit het
// antwoord op een corpus dat maar één taal heeft.
func TestBilingualPickFallsBackToTheOtherLanguage(t *testing.T) {
	both := Bilingual{kbLangSource: "Nederlands", kbLangEnglish: "English"}
	if both.Pick(kbLangEnglish) != "English" || both.Pick(kbLangSource) != "Nederlands" {
		t.Fatal("pick koos de verkeerde taal")
	}
	only := Bilingual{kbLangSource: "Nederlands"}
	if only.Pick(kbLangEnglish) != "Nederlands" {
		t.Fatal("pick gaf een leeg scherm op een eentalig corpus")
	}
	if (Bilingual{}).Pick(kbLangSource) != "" {
		t.Fatal("lege tekst werd iets")
	}
}

// Élk vertaalbaar veld, op élk label, moet in duiven/3 de tweetalige vorm
// dragen. Eén veld dat als kale string doorglipt is precies de halve vertaling
// die dit schema hoort te weigeren.
func TestKBV3EveryLocalizedFieldMustCarryBothLanguages(t *testing.T) {
	for label, fields := range kbV3LocalizedFixtureFields {
		for _, field := range fields {
			t.Run(label+"/"+field, func(t *testing.T) {
				doc := kbV3Fixture(t)
				pair, ok := doc.Nodes[label][0][field].(map[string]any)
				if !ok {
					t.Fatalf("fixture mist het tweetalige veld %s/%s", label, field)
				}
				// Positieve controle: mét beide talen komt hij er wel door.
				if _, err := parseKBV2JSON(kbV2Encode(t, doc)); err != nil {
					t.Fatalf("volledige vertaling afgekeurd: %v", err)
				}
				delete(pair, kbLangEnglish)
				if _, err := parseKBV2JSON(kbV2Encode(t, doc)); err == nil {
					t.Fatalf("%s/%s zonder vertaling overleefde de validatie", label, field)
				}
			})
		}
	}
}

// De bronkaart draagt de vertaalde samenvatting en het vertaalde thema naast de
// brontaal. De props komen uit de zoekopdracht zelf, dus dit kost geen extra
// lezing.
func TestKBV3WireSourceCarriesBothLanguages(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	id := data.NodeID("destillaat:1")
	rec := &searchRecorder{props: map[data.NodeID]map[string]any{id: {
		"samenvatting": "Nederlandse samenvatting.", "samenvatting_en": "English summary.",
		"thema_titel": "Overlast", "thema_titel_en": "Nuisance",
		"source_language": kbLangSource, "bundle_schema": kbV3Schema,
	}}}
	got := r.wireSources(rec, []interaction.RAGSource{{
		Number: 1, Type: tableDestillaat, ID: id, Text: "Nederlandse samenvatting.",
		Context: &data.DestillaatContext{Thema: &data.Thema{Titel: "Overlast"}},
	}})
	if len(got) != 1 || got[0].Localized == nil {
		t.Fatalf("bron zonder talen: %+v", got)
	}
	if got[0].SourceLanguage != kbLangSource {
		t.Fatalf("brontaal = %q", got[0].SourceLanguage)
	}
	if got[0].Text != "Nederlandse samenvatting." {
		t.Fatalf("brontaalveld gewijzigd: %q", got[0].Text)
	}
	if got[0].Localized.Text.Pick(kbLangEnglish) != "English summary." || got[0].Localized.Theme.Pick(kbLangEnglish) != "Nuisance" {
		t.Fatalf("vertaling ontbreekt op de bronkaart: %+v", got[0].Localized)
	}
	// Zonder props (een oude rij, of een recorder die niets ving) blijft de
	// bronkaart precies de eentalige kaart die hij was.
	plain := r.wireSources(nil, []interaction.RAGSource{{Number: 1, Type: tableDestillaat, ID: id, Text: "Nederlandse samenvatting."}})
	if plain[0].Localized != nil || plain[0].SourceLanguage != "" {
		t.Fatalf("eentalige bron kreeg taalvelden: %+v", plain[0])
	}
}

// Het model krijgt de citaten, niet de samenvatting. Bij een duiven/3-bron
// vervangt de gate de modeltekst door de bronverwijzing, precies zoals bij
// duiven/2: de vertaalde samenvatting concurreert dus in geen van beide talen
// met het originele citaat als bewijs. De bronkaart houdt hem wél, want daar is
// hij leesstof en geen bewijs.
func TestKBV3SummaryNeverReachesTheModelInEitherLanguage(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	id := data.NodeID("destillaat:1")
	quote := data.Quote{ID: "quote:1", Text: "The original passage stays the original."}
	st := runGate(t, r,
		[]interaction.RAGSource{{
			Type: tableDestillaat, ID: id, Text: "Nederlandse samenvatting.", Score: 1,
			Context: &data.DestillaatContext{
				Quotes:    []data.Quote{quote},
				Interview: &data.Interview{ID: "interview:1", Title: "veldnotitie"},
			},
		}},
		map[data.NodeID]map[string]any{
			id: {
				"samenvatting": "Nederlandse samenvatting.", "samenvatting_en": "English summary.",
				"bundle_schema": kbV3Schema, "source_language": kbLangSource,
			},
			quote.ID: {"grounded": true, "text": quote.Text},
		},
	)
	if len(st.sources) != 1 {
		t.Fatalf("duiven/3-bron viel weg: %+v", st.sources)
	}
	model := st.RAGSources[0].Text
	if !strings.HasPrefix(model, "Source reference: ") {
		t.Fatalf("modeltekst = %q, wil de bronverwijzing", model)
	}
	for _, summary := range []string{"Nederlandse samenvatting.", "English summary."} {
		if strings.Contains(model, summary) {
			t.Fatalf("de samenvatting bereikte het model: %q", model)
		}
	}
	// Het citaat is wat het model als bewijs krijgt, onveranderd.
	if len(st.sources[0].Ref.Passages) != 1 || st.sources[0].Ref.Passages[0].Text != quote.Text {
		t.Fatalf("citaat veranderd: %+v", st.sources[0].Ref.Passages)
	}
	// De bronkaart houdt beide talen: daar is de samenvatting leesstof.
	if st.sources[0].Localized == nil || st.sources[0].Localized.Text.Pick(kbLangEnglish) != "English summary." {
		t.Fatalf("bronkaart verloor de vertaling: %+v", st.sources[0].Localized)
	}
}
