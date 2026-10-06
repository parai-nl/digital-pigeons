//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// De tweetalige kennisbank-e2e. Hij boot de echte server op een eigen
// database met een duiven/3-bundel en volgt de keten die de vertaling raakt:
// import → graafoverzicht met beide talen → een chatbeurt waarvan de bronkaart
// de vertaling én de brontaal draagt, met het citaat onaangeroerd.
//
// Alle inhoud hieronder is voor deze test verzonnen. De echte kennisbank staat
// buiten deze repository en komt er nooit in.
//
// De bundel is een JSON-bestand in plaats van een zip: dat is de weg die
// duiven/2 en duiven/3 lopen, en de kennisbank-id ligt daarbij vast op de
// corpusnaamruimte van de module.

const (
	// kbBiFeeding is het standpunt waar de bronvraag letterlijk op mikt. De
	// Nederlandse en de Engelse tekst delen bewust geen inhoudswoorden met de
	// andere twee standpunten, zodat de tekstafhankelijke stub-embedder er
	// precies één boven de drempel tilt.
	kbBiFeedingNL = "Broodkorsten op het marktplein trekken meer duiven aan dan welke maatregel ook tegenhoudt."
	kbBiFeedingEN = "Bread crusts on the market square attract more pigeons than any measure holds back."
	// kbBiQuote is het citaat bij dat standpunt. Het is al Engels en wordt
	// nooit vertaald; de e2e toetst dat het byte-identiek op de wire komt.
	kbBiQuote = "After the market they arrive in numbers."

	kbBiNestingNL = "Netten onder de perronkap verplaatsen het nestelen naar de balkons ernaast."
	kbBiNestingEN = "Nets under the platform roof move nesting to the balconies next door."
	kbBiNestQuote = "Since the nets went up they sit on my balcony."

	kbBiThemeNL = "Overlast in de wijk"
	kbBiThemeEN = "Nuisance in the neighbourhood"
	kbBiSubNL   = "Voeren op straat"
	kbBiSubEN   = "Street feeding"
	kbBiDomNL   = "Stadsnatuur"
	kbBiDomEN   = "Urban nature"
	kbBiPerspNL = "De tellende onderzoeker"
	kbBiPerspEN = "The counting researcher"

	// kbBiPaper en kbBiCitation worden niet vertaald: een papertitel en een
	// bronvermelding zijn het origineel.
	kbBiPaper    = "veldnotitie-oostplein"
	kbBiCitation = "Synthetische veldnotitie (2026). Oostplein."

	// kbBiKBID is de kennisbank-id van de JSON-route: de corpusnaamruimte van
	// de module, niet de bestandsnaam.
	kbBiKBID = "Duiven"
)

// pair maakt één tweetalig veld.
func pair(nl, en string) map[string]any { return map[string]any{"nl": nl, "en": en} }

// pairList maakt één tweetalige lijst; beide talen zijn even lang.
func pairList(nl, en []string) map[string]any {
	return map[string]any{"nl": nl, "en": en}
}

// kbBilingualNodes is de knopenkant van de duiven/3-bundel.
func kbBilingualNodes() map[string][]map[string]any {
	return map[string][]map[string]any{
		"Domein": {{
			"id": "dom-1", "ext_id": "D1",
			"titel":          pair(kbBiDomNL, kbBiDomEN),
			"beschrijving":   pair("Hoe dieren en mensen dezelfde straten delen.", "How animals and people share the same streets."),
			"kernconcepten":  pairList([]string{"stadsnatuur"}, []string{"urban nature"}),
			"taxonomy_level": "domein",
		}},
		"Thema": {{
			"id": "the-1", "ext_id": "T1",
			"titel":          pair(kbBiThemeNL, kbBiThemeEN),
			"beschrijving":   pair("Wat bewoners melden over duiven in hun straat.", "What residents report about pigeons in their street."),
			"kernconcepten":  pairList([]string{"overlast"}, []string{"nuisance"}),
			"taxonomy_level": "thema",
		}},
		"Subthema": {{
			"id": "sub-1", "ext_id": "S1",
			"titel":            pair(kbBiSubNL, kbBiSubEN),
			"beschrijving":     pair("Voedselaanbod op straat en wat dat met de populatie doet.", "Food on the street and what it does to the population."),
			"kernconcepten":    pairList([]string{"voeren"}, []string{"feeding"}),
			"taxonomy_level":   "subthema",
			"destillaat_count": 2,
		}},
		"Perspectief": {{
			"id": "per-1", "ext_id": "P1",
			"naam":             pair(kbBiPerspNL, kbBiPerspEN),
			"kernstandpunt":    pair("eerst meten", "measure first"),
			"beschrijving":     pair("wil cijfers voor maatregelen", "wants numbers before measures"),
			"destillaat_count": 2,
		}},
		"Bron": {{
			"id": "bron-1", "ext_id": "B1", "key": "veldnotitie-oostplein",
			"titel": kbBiPaper, "citatie": kbBiCitation,
			"doi": "10.1234/synthetisch", "source_kind": "document",
			"storage_key": "kb/veldnotitie-oostplein.pdf",
		}},
		"Destillaat": {
			{
				"id": "des-1", "ext_id": "DS1",
				"samenvatting":   pair(kbBiFeedingNL, kbBiFeedingEN),
				"primary_themes": []string{"overlast"}, "secondary_themes": []string{"voeren"},
				"domain": "wonen_en_leefomgeving", "scope": "lokaal", "abstraction_level": "midden",
			},
			{
				"id": "des-2", "ext_id": "DS2",
				"samenvatting":   pair(kbBiNestingNL, kbBiNestingEN),
				"primary_themes": []string{"overlast"}, "secondary_themes": []string{},
				"domain": "wonen_en_leefomgeving", "scope": "lokaal", "abstraction_level": "midden",
			},
		},
		"Quote": {
			{"id": "quo-1", "ext_id": "Q1", "text": kbBiQuote, "speaker": "respondent",
				"destillaat_id": "des-1", "bron_id": "bron-1", "bron_key": "veldnotitie-oostplein",
				"quote_index": 0, "start_byte": 0, "end_byte": len(kbBiQuote), "grounded": true},
			{"id": "quo-2", "ext_id": "Q2", "text": kbBiNestQuote, "speaker": "respondent",
				"destillaat_id": "des-2", "bron_id": "bron-1", "bron_key": "veldnotitie-oostplein",
				"quote_index": 0, "start_byte": 100, "end_byte": 100 + len(kbBiNestQuote), "grounded": true},
		},
	}
}

// kbBiRel bouwt één relatie mét de labels van beide eindknopen. duiven/2 en
// duiven/3 toetsen die labels — een relatie waarvan de eindknoop een ander
// label draagt dan de knoop zelf is een bundel die niet klopt — terwijl de
// oudere mosaic/1-route ze negeert. Vandaar een eigen bouwer naast kbRel.
func kbBiRel(labels map[string]string, typ, from, to string) map[string]any {
	return map[string]any{
		"type":       typ,
		"properties": map[string]any{},
		"start_node": map[string]any{"labels": []string{labels[from]}, "properties": map[string]any{"id": from}},
		"end_node":   map[string]any{"labels": []string{labels[to]}, "properties": map[string]any{"id": to}},
	}
}

// kbBilingualLabels koppelt elke export-id aan zijn label, zodat de relaties
// nooit uit de pas kunnen lopen met de knopen.
func kbBilingualLabels() map[string]string {
	out := map[string]string{}
	for label, rows := range kbBilingualNodes() {
		for _, row := range rows {
			out[row["id"].(string)] = label
		}
	}
	return out
}

func kbBilingualRels() map[string][]map[string]any {
	l := kbBilingualLabels()
	return map[string][]map[string]any{
		"CONTAINS_THEMA":         {kbBiRel(l, "CONTAINS_THEMA", "dom-1", "the-1")},
		"CONTAINS_SUBTHEMA":      {kbBiRel(l, "CONTAINS_SUBTHEMA", "the-1", "sub-1")},
		"CONTAINS_PERSPECTIEF":   {kbBiRel(l, "CONTAINS_PERSPECTIEF", "the-1", "per-1")},
		"CONTAINS":               {kbBiRel(l, "CONTAINS", "bron-1", "des-1"), kbBiRel(l, "CONTAINS", "bron-1", "des-2")},
		"HAS_QUOTE":              {kbBiRel(l, "HAS_QUOTE", "des-1", "quo-1"), kbBiRel(l, "HAS_QUOTE", "des-2", "quo-2")},
		"BELONGS_TO":             {kbBiRel(l, "BELONGS_TO", "des-1", "the-1"), kbBiRel(l, "BELONGS_TO", "des-2", "the-1")},
		"BELONGS_TO_SUBTHEMA":    {kbBiRel(l, "BELONGS_TO_SUBTHEMA", "des-1", "sub-1"), kbBiRel(l, "BELONGS_TO_SUBTHEMA", "des-2", "sub-1")},
		"BELONGS_TO_PERSPECTIEF": {kbBiRel(l, "BELONGS_TO_PERSPECTIEF", "des-1", "per-1"), kbBiRel(l, "BELONGS_TO_PERSPECTIEF", "des-2", "per-1")},
	}
}

// kbBilingualCounts is wat de bundel per tabel belooft.
var kbBilingualCounts = map[string]int{
	"domein": 1, "thema": 1, "subthema": 1, "perspective": 1,
	"interview": 1, "destillaat": 2, "quote": 2,
}

// writeBilingualFixture schrijft de duiven/3-bundel als JSON en geeft het
// absolute pad. De statistieken worden uit de knopen zelf afgeleid, zodat ze
// nooit uit de pas kunnen lopen met de bundel eronder.
func writeBilingualFixture(t *testing.T) string {
	t.Helper()
	nodes, rels := kbBilingualNodes(), kbBilingualRels()
	stats := map[string]any{
		"node_labels":        map[string]int{},
		"relationship_types": map[string]int{},
	}
	for label, rows := range nodes {
		stats["node_labels"].(map[string]int)[label] = len(rows)
	}
	for typ, rows := range rels {
		stats["relationship_types"].(map[string]int)[typ] = len(rows)
	}
	raw, err := json.Marshal(map[string]any{
		"bundle_schema":   "duiven/3",
		"languages":       []string{"nl", "en"},
		"source_language": "nl",
		"export_date":     "2026-09-15",
		"nodes":           nodes,
		"relationships":   rels,
		"statistics":      stats,
	})
	if err != nil {
		t.Fatalf("duiven/3-bundel serialiseren: %v", err)
	}
	path := filepath.Join(t.TempDir(), "kennisbank.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("bundel wegschrijven: %v", err)
	}
	return path
}

// TestBilingualKnowledgeBootE2E boot de server met een duiven/3-bundel op een
// eigen database.
func TestBilingualKnowledgeBootE2E(t *testing.T) {
	bundle := writeBilingualFixture(t)
	dbURL := env(t, "DUIF_E2E_BILINGUAL_DB_URL")
	srv := bootServerWith(t, serverConfig{dbURL: dbURL, env: []string{
		"DUIF_IMPORT_KB=" + bundle,
		"DUIF_IMPORT_WORKERS=3",
	}})

	var destillaatID string

	t.Run("de tweetalige bundel importeert als gewone bundel", func(t *testing.T) {
		st := srv.waitImportReady(t, 30*time.Second)
		if st.KBID != kbBiKBID {
			t.Errorf("kbId = %q, want %q", st.KBID, kbBiKBID)
		}
		if st.Skipped || st.Reason != "" {
			t.Errorf("eerste import op een lege database: %+v", st)
		}
		for table, want := range kbBilingualCounts {
			if st.Counts[table] != want {
				t.Errorf("counts[%s] = %d, want %d (volledig %+v)", table, st.Counts[table], want, st.Counts)
			}
		}
		// De statusroute zegt zelf welk formaat er binnenkwam; zonder
		// die drie velden is van buiten niet te zien dat deze bank vertaald is.
		if st.BundleSchema != "duiven/3" {
			t.Errorf("bundleSchema = %q, want duiven/3", st.BundleSchema)
		}
		if st.SourceLanguage != "nl" {
			t.Errorf("sourceLanguage = %q, want nl", st.SourceLanguage)
		}
		if !reflect.DeepEqual(st.Languages, []string{"nl", "en"}) {
			t.Errorf("languages = %v, want [nl en]", st.Languages)
		}
	})

	t.Run("de graaf draagt beide talen en noemt de brontaal", func(t *testing.T) {
		var overview graphOverviewBody
		raw := srv.getJSON(t, "/api/duif/v1/graph/overview", &overview)
		assertNoStorageKey(t, "overview", raw)

		thema := graphNodeByType(t, overview.Nodes, "thema")
		if got := wireText(thema.Label, "nl"); got != kbBiThemeNL {
			t.Errorf("Nederlands themalabel = %q, want %q", got, kbBiThemeNL)
		}
		if got := wireText(thema.Label, "en"); got != kbBiThemeEN {
			t.Errorf("Engels themalabel = %q, want %q", got, kbBiThemeEN)
		}
		if thema.SourceLanguage != "nl" {
			t.Errorf("brontaal van het thema = %q, want nl", thema.SourceLanguage)
		}
		if got := wireText(thema.Metadata["beschrijving"], "en"); !strings.HasPrefix(got, "What residents report") {
			t.Errorf("Engelse themabeschrijving = %q", got)
		}

		// Het perspectief staat in de bundel, maar niet in de graaf.
		for _, n := range overview.Nodes {
			if n.Type == "perspective" {
				t.Errorf("de graaf toont een perspectief: %+v", n)
			}
		}

		domein := graphNodeByType(t, overview.Nodes, "domein")
		if got := wireText(domein.Label, "en"); got != kbBiDomEN {
			t.Errorf("Engels domeinlabel = %q, want %q", got, kbBiDomEN)
		}
	})

	t.Run("het standpunt is tweetalig, het citaat is het origineel", func(t *testing.T) {
		// Van het subthema naar zijn standpunten, en van daar naar de citaten.
		var overview graphOverviewBody
		srv.getJSON(t, "/api/duif/v1/graph/overview", &overview)
		subthema := graphNodeByType(t, overview.Nodes, "subthema")

		var kinderen graphChildrenBody
		srv.getJSON(t, "/api/duif/v1/graph/nodes/"+subthema.ID+"/children", &kinderen)
		for _, n := range kinderen.Nodes {
			if wireText(n.Metadata["text"], "nl") == kbBiFeedingNL {
				destillaatID = n.ID
			}
		}
		if destillaatID == "" {
			t.Fatalf("het gezochte standpunt staat niet onder het subthema: %+v", kinderen.Nodes)
		}

		var node graphNodeResponseBody
		srv.getJSON(t, "/api/duif/v1/graph/nodes/"+destillaatID, &node)
		if got := wireText(node.Node.Metadata["text"], "en"); got != kbBiFeedingEN {
			t.Errorf("Engelse samenvatting = %q, want %q", got, kbBiFeedingEN)
		}
		if got := wireText(node.Node.Metadata["text"], "nl"); got != kbBiFeedingNL {
			t.Errorf("Nederlandse samenvatting = %q, want %q", got, kbBiFeedingNL)
		}
		if got := wireText(node.Node.Metadata["thema_titel"], "en"); got != kbBiThemeEN {
			t.Errorf("gedenormaliseerd thema in het Engels = %q, want %q", got, kbBiThemeEN)
		}
		// De opsommingswaarde blijft een kale string: die wordt niet vertaald.
		if scope, _ := node.Node.Metadata["scope"].(string); scope != "lokaal" {
			t.Errorf("scope = %v, want de onvertaalde sleutel", node.Node.Metadata["scope"])
		}

		srv.getJSON(t, "/api/duif/v1/graph/nodes/"+destillaatID+"/children", &kinderen)
		var sawQuote bool
		for _, n := range kinderen.Nodes {
			if n.Type != "quote" {
				continue
			}
			sawQuote = true
			// Het citaat komt als kale string: het is het origineel en draagt
			// dus geen taalobject.
			if _, isText := n.Label.(string); !isText {
				t.Errorf("citaatlabel is een taalobject geworden: %v", n.Label)
			}
			if got, _ := n.Metadata["text"].(string); got != kbBiQuote {
				t.Errorf("citaattekst = %v, want %q byte-identiek", n.Metadata["text"], kbBiQuote)
			}
		}
		if !sawQuote {
			t.Fatalf("geen citaat onder het standpunt: %+v", kinderen.Nodes)
		}
	})

	t.Run("de bronkaart draagt de vertaling naast het originele citaat", func(t *testing.T) {
		status, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "biological",
			"message":  kbBiFeedingEN,
			"lang":     "en",
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, raw)
		}
		var sourcesFrame struct {
			Sources []sourceBody `json:"sources"`
		}
		reMarshal(t, lastFrame(t, frames, "sources"), &sourcesFrame)
		if len(sourcesFrame.Sources) == 0 {
			t.Fatalf("geen bronnen bij een vraag die letterlijk de samenvatting is: %s", raw)
		}
		bron := sourcesFrame.Sources[0]
		if bron.SourceLanguage != "nl" {
			t.Errorf("brontaal op de bronkaart = %q, want nl", bron.SourceLanguage)
		}
		if bron.Localized == nil {
			t.Fatalf("bronkaart zonder talen: %+v", bron)
		}
		if bron.Localized.Text["en"] != kbBiFeedingEN || bron.Localized.Text["nl"] != kbBiFeedingNL {
			t.Errorf("tweetalige samenvatting = %+v", bron.Localized.Text)
		}
		if bron.Localized.Theme["en"] != kbBiThemeEN {
			t.Errorf("tweetalig thema = %+v", bron.Localized.Theme)
		}
		if bron.Localized.Perspective["en"] != kbBiPerspEN {
			t.Errorf("tweetalig perspectief = %+v", bron.Localized.Perspective)
		}
		// Het brontaalveld blijft ernaast staan, zodat een opgeslagen antwoord
		// van vóór de vertaling dezelfde vorm houdt.
		if bron.Text != kbBiFeedingNL {
			t.Errorf("brontaalveld = %q, want de Nederlandse samenvatting", bron.Text)
		}
		// Het citaat is niet vertaald en komt byte-identiek terug.
		if bron.Ref.Quote != kbBiQuote {
			t.Errorf("ref.quote = %q, want %q", bron.Ref.Quote, kbBiQuote)
		}
		if bron.Ref.Paper != kbBiPaper {
			t.Errorf("ref.paper = %q, want %q", bron.Ref.Paper, kbBiPaper)
		}
	})
}
