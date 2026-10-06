//go:build e2e

package e2e

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// De kennisbank-fixture van de boot-e2e: één mosaic/1-bundel als zip, klein
// genoeg om in één oogopslag te lezen en compleet genoeg om elke tabel, elke
// relatie en elk graafpad van de module te raken.
//
// Alle inhoud hieronder is voor deze test verzonnen. De echte proef-kennisbank
// staat buiten deze repository en komt er nooit in; er staat hier geen tekst
// uit een private repo of uit onderzoeksmateriaal.
//
// De teksten zijn met opzet uit elkaar geschreven: de drie standpunten delen
// vrijwel geen inhoudswoorden, zodat de tekstafhankelijke stub-embedder
// (stub_llm_test.go) precies één standpunt boven de relevantiedrempel tilt
// wanneer de vraag de samenvatting van dát standpunt is — en geen enkel
// standpunt bij een systemische vraag.

const (
	// kbFixtureID is de kennisbank-id: hij komt uit project_details.json en
	// zit daarna in elke afgeleide node-id en in elke prop.
	kbFixtureID = "e2e-kb"

	// kbSummaryNesting is de samenvatting waar de bronvraag letterlijk op
	// mikt; kbQuoteNesting is het gegronde citaat dat eraan hangt en dus in
	// `ref.quote` hoort te belanden.
	kbSummaryNesting = "Netten onder de perronkap verplaatsen het nestelen naar de balkons ernaast."
	kbQuoteNesting   = "Sinds de netten zitten ze bij mij op het balkon."

	// kbSummaryFeeding hoort bij het standpunt met twee citaten: dat is het
	// standpunt waarvan de graaf-e2e de kinderen (citaten + bronpaper) telt.
	kbSummaryFeeding = "Broodkorsten op het marktplein trekken meer duiven aan dan welke maatregel ook tegenhoudt."
	// kbQuoteMarket is het gegronde citaat van dat standpunt; de trail-test
	// klimt vanaf dit citaat naar het domein.
	kbQuoteMarket = "Na de markt komen ze massaal."

	// kbPaperTitle is de titel van het enige bronpaper.
	kbPaperTitle = "veldnotitie-oostplein"
	// kbStorageKey staat wél in de graaf (de import laat hem door) en mag
	// nooit op de wire komen: de graaf-e2e zoekt hem in elke rauwe body.
	kbStorageKey = "kb/veldnotitie-oostplein.pdf"

	// kbSubthemaTitle is het label van het subthema waaronder alle drie de
	// standpunten hangen.
	kbSubthemaTitle = "Voeren op straat"
	// kbThemaTitle en kbDomeinTitle zijn de twee niveaus daarboven.
	kbThemaTitle  = "Overlast in de wijk"
	kbDomeinTitle = "Stadsnatuur"
)

// kbFixtureCounts is wat de bundel per tabel belooft. De importmarker draagt
// dezelfde tellingen, en de statusroute en de graaf-overview geven ze terug.
var kbFixtureCounts = map[string]int{
	"domein":      1,
	"thema":       1,
	"subthema":    1,
	"perspective": 2,
	"persona":     1,
	"interview":   1,
	"destillaat":  3,
	"quote":       3,
}

// kbRel bouwt één relatie in de exportvorm: het volledige eindknooppunt genest,
// met de graaf-id onder properties — precies zoals de ZIP-bundel hem draagt.
func kbRel(typ, from, to string) map[string]any {
	return map[string]any{
		"type":       typ,
		"properties": map[string]any{},
		"start_node": map[string]any{"labels": []string{}, "properties": map[string]any{"id": from}},
		"end_node":   map[string]any{"labels": []string{}, "properties": map[string]any{"id": to}},
	}
}

// kbFixtureNodes is de knopenkant van knowledge_base.json.
func kbFixtureNodes() map[string][]map[string]any {
	return map[string][]map[string]any{
		"Domein": {{
			"id": "dom-1", "ext_id": "D1", "titel": kbDomeinTitle,
			"beschrijving":  "Hoe dieren en mensen dezelfde straten delen.",
			"kernconcepten": []string{"stadsnatuur"}, "taxonomy_level": "domein",
		}},
		"Thema": {{
			"id": "the-1", "ext_id": "T1", "titel": kbThemaTitle,
			"beschrijving":  "Wat bewoners melden over duiven in hun straat.",
			"kernconcepten": []string{"overlast"}, "taxonomy_level": "thema",
		}},
		"Subthema": {{
			"id": "sub-1", "ext_id": "S1", "titel": kbSubthemaTitle,
			"beschrijving":  "Voedselaanbod op straat en wat dat met de populatie doet.",
			"kernconcepten": []string{"voeren"}, "taxonomy_level": "subthema",
			"destillaat_count": 3,
		}},
		"Perspectief": {
			{"id": "per-1", "ext_id": "P1", "naam": "De tellende onderzoeker",
				"kernstandpunt": "eerst meten", "beschrijving": "wil cijfers voor maatregelen"},
			{"id": "per-2", "ext_id": "P2", "naam": "De buurvrouw op driehoog",
				"kernstandpunt": "eerst schoon", "beschrijving": "wil rust op haar balkon"},
		},
		"Persona": {{
			"id": "pers-1", "ext_id": "PS1", "naam": "Wanda", "voornaam": "Wanda",
			"kernprofiel": "marktkoopvrouw", "samenvatting": "staat al jaren op het plein", "age": 58,
		}},
		"Interview": {{
			"id": "int-1", "ext_id": "I1", "title": kbPaperTitle,
			"storage_key": kbStorageKey, "interview_id": "iv-e2e-1",
			"origin": "veldnotitie", "source_kind": "pdf",
		}},
		"Destillaat": {
			{
				"id": "des-1", "ext_id": "DS1", "samenvatting": kbSummaryFeeding,
				"primary_themes": []string{kbThemaTitle}, "secondary_themes": []string{kbSubthemaTitle},
				"domain": kbDomeinTitle, "scope": "lokaal", "abstraction_level": "midden", "persona": "Wanda",
			},
			{
				"id": "des-2", "ext_id": "DS2", "samenvatting": kbSummaryNesting,
				"primary_themes": []string{kbThemaTitle}, "secondary_themes": []string{},
				"domain": kbDomeinTitle, "scope": "lokaal", "abstraction_level": "midden", "persona": "Wanda",
			},
			{
				"id": "des-3", "ext_id": "DS3",
				"samenvatting":   "Klachtenformulieren registreren ergernis, geen aantallen; daardoor mist elk dossier een nulmeting.",
				"primary_themes": []string{kbThemaTitle}, "secondary_themes": []string{},
				"domain": kbDomeinTitle, "scope": "regionaal", "abstraction_level": "hoog", "persona": "Wanda",
			},
		},
		"Quote": {
			// Het eerste citaat van des-1 is niet gegrond; de embed-tekst van de
			// import hoort het tweede (gegronde) citaat te pakken.
			{"id": "quo-1", "ext_id": "Q1", "text": "Elke ochtend liggen er korsten.",
				"destillaat_id": "des-1", "speaker": "Wanda", "quote_index": 0,
				"start_byte": 0, "end_byte": 31, "grounded": false},
			{"id": "quo-2", "ext_id": "Q2", "text": kbQuoteMarket,
				"destillaat_id": "des-1", "speaker": "Wanda", "quote_index": 1,
				"start_byte": 31, "end_byte": 60, "grounded": true},
			{"id": "quo-3", "ext_id": "Q3", "text": kbQuoteNesting,
				"destillaat_id": "des-2", "speaker": "Wanda", "quote_index": 0,
				"start_byte": 60, "end_byte": 108, "grounded": true},
		},
	}
}

// kbFixtureRels is de relatiekant: alle negen relatietypen die de ZIP-lezer
// kent en de import naar core's canon vertaalt.
func kbFixtureRels() map[string][]map[string]any {
	return map[string][]map[string]any{
		"CONTAINS_THEMA":       {kbRel("CONTAINS_THEMA", "dom-1", "the-1")},
		"CONTAINS_SUBTHEMA":    {kbRel("CONTAINS_SUBTHEMA", "the-1", "sub-1")},
		"CONTAINS_PERSPECTIEF": {kbRel("CONTAINS_PERSPECTIEF", "the-1", "per-1"), kbRel("CONTAINS_PERSPECTIEF", "the-1", "per-2")},
		"HAS_PERSPECTIVE":      {kbRel("HAS_PERSPECTIVE", "pers-1", "per-1"), kbRel("HAS_PERSPECTIVE", "pers-1", "per-2")},
		"CONTAINS": {
			kbRel("CONTAINS", "int-1", "des-1"),
			kbRel("CONTAINS", "int-1", "des-2"),
			kbRel("CONTAINS", "int-1", "des-3"),
		},
		"HAS_QUOTE": {
			kbRel("HAS_QUOTE", "des-1", "quo-1"),
			kbRel("HAS_QUOTE", "des-1", "quo-2"),
			kbRel("HAS_QUOTE", "des-2", "quo-3"),
		},
		"BELONGS_TO": {
			kbRel("BELONGS_TO", "des-1", "the-1"),
			kbRel("BELONGS_TO", "des-2", "the-1"),
			kbRel("BELONGS_TO", "des-3", "the-1"),
		},
		"BELONGS_TO_SUBTHEMA": {
			kbRel("BELONGS_TO_SUBTHEMA", "des-1", "sub-1"),
			kbRel("BELONGS_TO_SUBTHEMA", "des-2", "sub-1"),
			kbRel("BELONGS_TO_SUBTHEMA", "des-3", "sub-1"),
		},
		"BELONGS_TO_PERSPECTIEF": {
			kbRel("BELONGS_TO_PERSPECTIEF", "des-1", "per-1"),
			kbRel("BELONGS_TO_PERSPECTIEF", "des-2", "per-2"),
			kbRel("BELONGS_TO_PERSPECTIEF", "des-3", "per-1"),
		},
	}
}

// writeKBFixture schrijft de bundel als zip in een tempdir van de test en geeft
// het absolute pad. Dat pad gaat als DUIF_IMPORT_KB het serverproces in; de
// tempdir blijft staan tot de test klaar is, zodat een tweede boot dezelfde
// bundel nog vindt.
func writeKBFixture(t *testing.T) string {
	t.Helper()
	kb, err := json.Marshal(map[string]any{
		"bundle_schema": "mosaic/1",
		"export_date":   "2026-09-03",
		"nodes":         kbFixtureNodes(),
		"relationships": kbFixtureRels(),
	})
	if err != nil {
		t.Fatalf("knowledge_base.json serialiseren: %v", err)
	}
	pd, err := json.Marshal(map[string]any{"name": kbFixtureID})
	if err != nil {
		t.Fatalf("project_details.json serialiseren: %v", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range []struct {
		name string
		body []byte
	}{
		{"knowledge_base.json", kb},
		{"project_details.json", pd},
	} {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatalf("zip-entry %s: %v", entry.name, err)
		}
		if _, err := w.Write(entry.body); err != nil {
			t.Fatalf("zip-entry %s schrijven: %v", entry.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip sluiten: %v", err)
	}

	path := filepath.Join(t.TempDir(), kbFixtureID+".zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("bundel wegschrijven: %v", err)
	}
	return path
}
