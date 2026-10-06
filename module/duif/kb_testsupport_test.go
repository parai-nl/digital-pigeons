package duif

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-interaction/modules/interaction/bundleimport"
)

// mosaicFixture bouwt een mini-bundel in mosaic/1-vorm: klein genoeg om in een
// test te lezen, compleet genoeg om elke tabel, elke relatie en beide
// embed-tekstpaden te raken. De echte proef-KB staat buiten de repo en komt er
// nooit in; dit is de enige testdata.
type mosaicFixture struct {
	Project string
	Schema  string
	Nodes   map[string][]map[string]any
	Rels    map[string][]map[string]any
	// Extra entries die náást de twee JSON-bestanden in de zip komen.
	Extra map[string][]byte
	// NoProjectDetails laat project_details.json weg (kb-id-fallback).
	NoProjectDetails bool
	// NoKnowledgeBase laat knowledge_base.json weg.
	NoKnowledgeBase bool
}

// rel bouwt één relatie in de exportvorm: het volledige eindknooppunt genest,
// met de graaf-id onder properties.
func rel(typ, from, to string, props map[string]any) map[string]any {
	return map[string]any{
		"type":       typ,
		"properties": props,
		"start_node": map[string]any{"labels": []string{}, "properties": map[string]any{"id": from}},
		"end_node":   map[string]any{"labels": []string{}, "properties": map[string]any{"id": to}},
	}
}

// newMosaicFixture is de standaardbundel: 1 domein, 1 thema, 1 subthema,
// 2 perspectieven, 1 persona-knoop, 1 bronpaper, 2 standpunten, 3 citaten.
func newMosaicFixture() *mosaicFixture {
	return &mosaicFixture{
		Project: "duiven-proef",
		Schema:  kbBundleSchema,
		Nodes: map[string][]map[string]any{
			"Domein": {{
				"id": "dom-1", "ext_id": "D1", "titel": "Stad", "beschrijving": "de stad",
				"kernconcepten": []string{"stad"}, "taxonomy_level": "domein",
			}},
			"Thema": {{
				"id": "the-1", "ext_id": "T1", "titel": "Overlast", "beschrijving": "over overlast",
				"kernconcepten": []string{"overlast"}, "taxonomy_level": "thema",
			}},
			"Subthema": {{
				"id": "sub-1", "ext_id": "S1", "titel": "Voeren", "beschrijving": "over voeren",
				"kernconcepten": []string{"voeren"}, "taxonomy_level": "subthema", "destillaat_count": 2,
			}},
			"Perspectief": {
				{"id": "per-1", "ext_id": "P1", "naam": "De kritische wetenschapper", "kernstandpunt": "meten", "beschrijving": "kritisch"},
				{"id": "per-2", "ext_id": "P2", "naam": "De bezorgde bewoner", "kernstandpunt": "rust", "beschrijving": "bezorgd"},
			},
			"Persona": {{
				"id": "pers-1", "ext_id": "PS1", "naam": "Ria", "voornaam": "Ria",
				"kernprofiel": "bewoner", "samenvatting": "woont in de stad", "age": 62,
			}},
			"Interview": {{
				"id": "int-1", "ext_id": "I1", "title": "lewald1987", "storage_key": "kb/lewald1987.pdf",
				"interview_id": "iv-1", "origin": "paper", "source_kind": "pdf", "content_hash": "abc",
			}},
			"Destillaat": {
				{
					"id": "des-1", "ext_id": "DS1", "samenvatting": "Duiven voeren vergroot de overlast.",
					"primary_themes": []string{"Overlast"}, "secondary_themes": []string{"Voeren"},
					"domain": "Stad", "scope": "lokaal", "abstraction_level": "midden", "persona": "Ria",
				},
				{
					"id": "des-2", "ext_id": "DS2", "samenvatting": "Duiven horen bij de stad.",
					"primary_themes": []string{"Overlast"}, "secondary_themes": []string{},
					"domain": "Stad", "scope": "lokaal", "abstraction_level": "hoog", "persona": "Ria",
				},
			},
			"Quote": {
				// Het eerste citaat van des-1 is níét gegrond: de embed-tekst
				// hoort het tweede (gegronde) te pakken.
				{"id": "quo-1", "ext_id": "Q1", "text": "Ik zie ze elke dag.", "destillaat_id": "des-1", "speaker": "Ria", "quote_index": 0, "grounded": false},
				{"id": "quo-2", "ext_id": "Q2", "text": "Bij de bakker liggen broodkorsten.", "destillaat_id": "des-1", "speaker": "Ria", "quote_index": 1, "grounded": true},
				{"id": "quo-3", "ext_id": "Q3", "text": "Ze horen erbij.", "destillaat_id": "des-2", "speaker": "Ria", "quote_index": 0, "grounded": false},
			},
		},
		Rels: map[string][]map[string]any{
			"CONTAINS_THEMA":       {rel("CONTAINS_THEMA", "dom-1", "the-1", nil)},
			"CONTAINS_SUBTHEMA":    {rel("CONTAINS_SUBTHEMA", "the-1", "sub-1", nil)},
			"CONTAINS_PERSPECTIEF": {rel("CONTAINS_PERSPECTIEF", "the-1", "per-1", nil), rel("CONTAINS_PERSPECTIEF", "the-1", "per-2", nil)},
			"HAS_PERSPECTIVE": {
				rel("HAS_PERSPECTIVE", "pers-1", "per-1", map[string]any{"uitleg": "past bij haar"}),
				rel("HAS_PERSPECTIVE", "pers-1", "per-2", nil),
			},
			"CONTAINS":  {rel("CONTAINS", "int-1", "des-1", nil), rel("CONTAINS", "int-1", "des-2", nil)},
			"HAS_QUOTE": {rel("HAS_QUOTE", "des-1", "quo-1", nil), rel("HAS_QUOTE", "des-1", "quo-2", nil), rel("HAS_QUOTE", "des-2", "quo-3", nil)},
			"BELONGS_TO": {
				rel("BELONGS_TO", "des-1", "the-1", nil), rel("BELONGS_TO", "des-2", "the-1", nil),
			},
			"BELONGS_TO_SUBTHEMA": {rel("BELONGS_TO_SUBTHEMA", "des-1", "sub-1", nil), rel("BELONGS_TO_SUBTHEMA", "des-2", "sub-1", nil)},
			"BELONGS_TO_PERSPECTIEF": {
				rel("BELONGS_TO_PERSPECTIEF", "des-1", "per-1", map[string]any{"confidence": 0.9, "uitleg": "meet en telt"}),
				rel("BELONGS_TO_PERSPECTIEF", "des-2", "per-2", nil),
			},
			"RELATES_TO": {rel("RELATES_TO", "des-1", "des-2", nil)},
		},
	}
}

// knowledgeBaseJSON levert de rauwe bytes van knowledge_base.json.
func (f *mosaicFixture) knowledgeBaseJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"bundle_schema": f.Schema,
		"export_date":   "2026-09-01",
		"nodes":         f.Nodes,
		"relationships": f.Rels,
	})
	if err != nil {
		t.Fatalf("fixture serialiseren: %v", err)
	}
	return raw
}

// writeZip schrijft de bundel als zip in een tempdir en geeft het pad. De
// bestandsnaam (zonder extensie) is de kb-id-fallback.
func (f *mosaicFixture) writeZip(t *testing.T, name string) string {
	t.Helper()
	entries := map[string][]byte{}
	if !f.NoKnowledgeBase {
		entries[kbFileKnowledgeBase] = f.knowledgeBaseJSON(t)
	}
	if !f.NoProjectDetails {
		pd, err := json.Marshal(map[string]any{"name": f.Project})
		if err != nil {
			t.Fatal(err)
		}
		entries[kbFileProjectDetails] = pd
	}
	for k, v := range f.Extra {
		entries[k] = v
	}
	return writeZipFile(t, name, entries)
}

// writeZipFile schrijft een zip met exact deze entries (volgorde onbepaald).
func writeZipFile(t *testing.T, name string, entries map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for entryName, body := range entries {
		w, err := zw.Create(entryName)
		if err != nil {
			t.Fatalf("zip-entry %q: %v", entryName, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("zip-entry %q schrijven: %v", entryName, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip sluiten: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("zip wegschrijven: %v", err)
	}
	return path
}

// derived is de node-id die de import voor een export-id maakt.
func derived(kbID, table, exportID string) data.NodeID {
	return data.NodeID(bundleimport.DeriveID(kbID, table, exportID))
}

// testImporter bouwt een importer op de meegegeven schrijfseam, zonder echte
// wachttijd tussen twee pogingen: de wachtseam meldt alleen of de context er
// nog is. Dat is de enige knop die een test aan het wachten heeft.
func testImporter(w knowledgeWriter, logger *slog.Logger, force bool) *kbImporter {
	imp := newKBImporter(w, logger, 4, force, "test-embedder", 1536)
	imp.sleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	return imp
}

// newTestImporter is de veelgebruikte vorm: een verse fakeKnowledge en een
// logger die de test kan meelezen.
func newTestImporter(t *testing.T) (*kbImporter, *fakeKnowledge) {
	t.Helper()
	fake := &fakeKnowledge{}
	logger, _ := captureLogs()
	return testImporter(fake, logger, false), fake
}

// countingFailer laat de eerste n aanroepen van een operatie falen en daarna
// slagen — het gedrag van een provider die 429 geeft en bijtrekt.
type countingFailer struct {
	mu    sync.Mutex
	op    string
	fails int
	seen  int
	err   error
}

// count geeft het aantal geziene aanroepen; de hook draait in de workers, dus
// lezen gaat achter dezelfde mutex.
func (c *countingFailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

func (c *countingFailer) hook(op string, _ data.NodeID) error {
	if op != c.op {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen++
	if c.seen <= c.fails {
		return c.err
	}
	return nil
}

// dropRelateWriter meldt succes op één relatie maar bewaart hem niet: zo
// ontstaat precies het gat dat de telling vóór de marker hoort te vangen.
type dropRelateWriter struct {
	*fakeKnowledge
	drop string
}

func (d *dropRelateWriter) Relate(ctx context.Context, from, to data.NodeID, relation string, props map[string]any) error {
	if relation == d.drop {
		return nil
	}
	return d.fakeKnowledge.Relate(ctx, from, to, relation, props)
}

// listerOnlyWriter verbergt ListEdges: een repository zonder de optionele
// EdgeLister-poort. De telling valt dan terug op alleen de knopen.
type listerOnlyWriter struct{ inner knowledgeWriter }

func (l listerOnlyWriter) ListNodes(ctx context.Context, table string, opts data.ListOpts) ([]map[string]any, error) {
	return l.inner.ListNodes(ctx, table, opts)
}

func (l listerOnlyWriter) CreateNodeWithID(ctx context.Context, id data.NodeID, props map[string]any, embedText string) (data.NodeID, error) {
	return l.inner.CreateNodeWithID(ctx, id, props, embedText)
}

func (l listerOnlyWriter) Relate(ctx context.Context, from, to data.NodeID, relation string, props map[string]any) error {
	return l.inner.Relate(ctx, from, to, relation, props)
}

func (l listerOnlyWriter) Delete(ctx context.Context, id data.NodeID) error {
	return l.inner.Delete(ctx, id)
}

// serveKnowledge draait één verzoek langs uitsluitend de kennisbankroutes.
// Bewust niet via m.Routes: de aanroep daarvan staat in module.go, en deze
// tests horen niet af te hangen van de volgorde waarin die regel landt.
func serveKnowledge(t *testing.T, m *Module, req *http.Request, anonymous bool) *httptest.ResponseRecorder {
	t.Helper()
	return serveOn(t, m.knowledgeRoutes, req, anonymous)
}
