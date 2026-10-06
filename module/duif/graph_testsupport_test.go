package duif

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

// Testgereedschap voor de kennisgraaf. De graaf leest uit de gedeelde
// fakeKnowledge (knowledge_fake_test.go); hier staat alleen wat de graaf er
// bovenop nodig heeft: een mini-kennisbank met alle acht tabellen, een router
// die enkel de graafroutes mount, en twee decorators voor de foutpaden.

// testKBID is de kennisbank van de mini-graaf; graphForeignKBID hoort bij een
// tweede kennisbank in dezelfde tabellen (de graaf moet die negeren).
const (
	testKBID         = "duiven-proef"
	graphForeignKBID = "andere-kb"
)

// gid maakt een deterministische node-id in core's `tabel:uuid`-vorm. De uuid
// is nep maar wel vormgeldig, zodat de id-validatie hem accepteert.
func gid(table string, n int) string {
	return fmt.Sprintf("%s:%08x-0000-4000-8000-%012x", table, n, n)
}

// graphFixture bouwt een kennisbank in een fakeKnowledge.
type graphFixture struct {
	t    *testing.T
	k    *fakeKnowledge
	kbID string
}

func newGraphFixture(t *testing.T) *graphFixture {
	t.Helper()
	return &graphFixture{t: t, k: &fakeKnowledge{}, kbID: testKBID}
}

// node schrijft één node; `props` wordt aangevuld met kb_id en import_hash,
// zoals de importer dat doet.
func (f *graphFixture) node(id string, props map[string]any) string {
	f.t.Helper()
	p := map[string]any{"kb_id": f.kbID, "import_hash": "hash-" + id}
	for k, v := range props {
		p[k] = v
	}
	if _, err := f.k.CreateNodeWithID(context.Background(), data.NodeID(id), p, ""); err != nil {
		f.t.Fatalf("fixture: node %s: %v", id, err)
	}
	return id
}

func (f *graphFixture) relate(from, to, relation string) {
	f.t.Helper()
	if err := f.k.Relate(context.Background(), data.NodeID(from), data.NodeID(to), relation, nil); err != nil {
		f.t.Fatalf("fixture: relate %s-%s->%s: %v", from, relation, to, err)
	}
}

// marker schrijft de importmarker die de graaf als "kennisbank X is klaar"
// leest. counts komt als map[string]any binnen, precies zoals JSON hem uit de
// database teruggeeft.
func (f *graphFixture) marker(counts map[string]any) {
	f.t.Helper()
	f.node(gid(tableImportMarker, 1), map[string]any{
		"kb_id":               f.kbID,
		"content_hash":        "0f0f0f",
		"mapper_version":      kbMapperVersion,
		"embedding_model":     "vertex/gemini-embedding-001",
		"embedding_dimension": float64(3072),
		"imported_at":         "2026-09-03T10:00:00Z",
		"counts":              counts,
	})
}

// longSummary is een samenvatting die ruim over de labelgrens van 140 tekens
// heen gaat, zodat het afkappen echt getoetst wordt.
const longSummary = "De stadsduif wordt in beleidsstukken vrijwel altijd als overlast beschreven, " +
	"terwijl de dieren zelf in de stad een ecologische rol vervullen die in dezelfde stukken " +
	"nergens wordt benoemd of onderzocht door de betrokken diensten."

// build zet de mini-graaf neer: één domein, twee thema's met elk een subthema
// en een perspectief, één persona-knoop, één bronpaper, twee standpunten en twee
// citaten — met van elk type een prop die nooit de wire op mag. De
// perspectieven en de persona-knoop staan er zoals in de echte kennisbank, zodat
// de tests kunnen bewijzen dat de graaf ze niet toont.
func (f *graphFixture) build() *graphFixture {
	f.t.Helper()
	f.node(gid(tableDomein, 1), map[string]any{"titel": "Stad en dier", "beschrijving": "Het domein.", "taxonomy_level": "domein"})
	f.node(gid(tableThema, 1), map[string]any{"titel": "Overlast", "beschrijving": "Thema een.", "kernconcepten": []any{"overlast"}})
	f.node(gid(tableThema, 2), map[string]any{"titel": "Welzijn", "beschrijving": "Thema twee."})
	f.node(gid(tableSubthema, 1), map[string]any{"titel": "Voeren", "beschrijving": "Subthema een."})
	f.node(gid(tableSubthema, 2), map[string]any{"titel": "Broedplekken", "beschrijving": "Subthema twee."})
	f.node(gid(tablePerspective, 1), map[string]any{"naam": "De kritische wetenschapper", "kernstandpunt": "Meet eerst.", "beschrijving": "Perspectief een."})
	f.node(gid(tablePerspective, 2), map[string]any{"naam": "De bezorgde bewoner", "kernstandpunt": "Het is te veel.", "beschrijving": "Perspectief twee."})
	f.node(gid(tablePersona, 1), map[string]any{"naam": "Wetenschapper", "voornaam": "Ria", "kernprofiel": "Onderzoekt de stad.", "samenvatting": "Lang.", "published": false})
	f.node(gid(tableInterview, 1), map[string]any{
		"title": "lewald1987", "interview_id": "iv-1",
		// storage_key en content_hash zijn de twee props die de wire nooit mag
		// dragen; ze staan hier juist wél in de fixture.
		"storage_key": "opslag/intern/bron-1.pdf", "content_hash": "deadbeef",
	})
	f.node(gid(tableDestillaat, 1), map[string]any{
		"samenvatting": longSummary, "persona": "De kritische wetenschapper", "domain": "ecologie",
		"scope": "stad", "abstraction_level": "midden", "thema_titel": "Overlast",
		"primary_themes": []any{"Overlast"}, "embedding": "[0.1,0.2]",
	})
	f.node(gid(tableDestillaat, 2), map[string]any{
		"samenvatting": "Een kort standpunt.", "persona": "De bezorgde bewoner", "domain": "beleid",
		"scope": "wijk", "thema_titel": "Welzijn", "embedding": "[0.3]",
	})
	f.node(gid(tableQuote, 1), map[string]any{"text": longSummary, "speaker": "R.", "quote_index": float64(0), "content_hash": "cafe"})
	f.node(gid(tableQuote, 2), map[string]any{"text": "Kort citaat.", "speaker": "J."})

	f.relate(gid(tableDomein, 1), gid(tableThema, 1), relContainsThema)
	f.relate(gid(tableDomein, 1), gid(tableThema, 2), relContainsThema)
	f.relate(gid(tableThema, 1), gid(tableSubthema, 1), relContainsSubthema)
	f.relate(gid(tableThema, 2), gid(tableSubthema, 2), relContainsSubthema)
	f.relate(gid(tableThema, 1), gid(tablePerspective, 1), data.RelContainsPerspectief)
	f.relate(gid(tableThema, 2), gid(tablePerspective, 2), data.RelContainsPerspectief)
	f.relate(gid(tablePersona, 1), gid(tablePerspective, 1), data.RelHasPerspective)
	f.relate(gid(tablePersona, 1), gid(tablePerspective, 2), data.RelHasPerspective)
	f.relate(gid(tableInterview, 1), gid(tableDestillaat, 1), data.RelContains)
	f.relate(gid(tableInterview, 1), gid(tableDestillaat, 2), data.RelContains)
	f.relate(gid(tableDestillaat, 1), gid(tableSubthema, 1), relBelongsToSubthema)
	f.relate(gid(tableDestillaat, 2), gid(tableSubthema, 2), relBelongsToSubthema)
	f.relate(gid(tableDestillaat, 1), gid(tableThema, 1), "belongs_to")
	f.relate(gid(tableDestillaat, 1), gid(tablePerspective, 1), data.RelBelongsToPerspectief)
	f.relate(gid(tableDestillaat, 2), gid(tablePerspective, 2), data.RelBelongsToPerspectief)
	f.relate(gid(tableDestillaat, 1), gid(tableQuote, 1), data.RelHasQuote)
	f.relate(gid(tableDestillaat, 1), gid(tableQuote, 2), data.RelHasQuote)

	f.marker(map[string]any{
		tableDomein: float64(1), tableThema: float64(2), tableSubthema: float64(2),
		tablePerspective: float64(2), tablePersona: float64(1), tableInterview: float64(1),
		tableDestillaat: float64(2), tableQuote: float64(2),
		// De marker telt ook relaties; die horen niet in `totals` per tabel.
		relContainsThema: float64(2),
	})
	return f
}

// foreign zet een tweede kennisbank in dezelfde tabellen neer. Niets daarvan
// mag in een antwoord opduiken.
func (f *graphFixture) foreign() *graphFixture {
	f.t.Helper()
	kb := f.kbID
	f.kbID = graphForeignKBID
	f.node(gid(tableDomein, 900), map[string]any{"titel": "Vreemd domein"})
	f.node(gid(tableThema, 900), map[string]any{"titel": "Vreemd thema"})
	f.relate(gid(tableDomein, 900), gid(tableThema, 900), relContainsThema)
	f.kbID = kb
	return f
}

// foreignChild hangt een node uit de tweede kennisbank onder een eigen ouder.
// De relatie bestaat, het kind mag deze lezer niet bereiken — en dus ook niet
// meetellen.
func (f *graphFixture) foreignChild() *graphFixture {
	f.t.Helper()
	kb := f.kbID
	f.kbID = graphForeignKBID
	f.node(gid(tableThema, 901), map[string]any{"titel": "Vreemd thema onder een eigen domein"})
	f.kbID = kb
	f.relate(gid(tableDomein, 1), gid(tableThema, 901), relContainsThema)
	return f
}

// destillaten hangt n standpunten onder een subthema, voor de pagineringstoets.
func (f *graphFixture) destillaten(subthema string, n int) *graphFixture {
	f.t.Helper()
	for i := 0; i < n; i++ {
		id := f.node(gid(tableDestillaat, 1000+i), map[string]any{"samenvatting": fmt.Sprintf("Standpunt %03d.", i)})
		f.relate(id, subthema, relBelongsToSubthema)
	}
	return f
}

// service bouwt de graaf-leesdienst rechtstreeks op de fixture (voor de
// logica-tests, zonder HTTP ertussen).
func (f *graphFixture) service() *graphService {
	return &graphService{nodes: f.k, edges: f.k}
}

// module bouwt een Module met de fixture als kennisgraaf (voor de HTTP-tests).
func (f *graphFixture) module() *Module {
	f.t.Helper()
	return newGraphModule(f.t, f.k)
}

func newGraphModule(t *testing.T, k data.KnowledgeRepo) *Module {
	t.Helper()
	m, _ := newTestModule(t, func(m *Module) { m.knowledge = k })
	return m
}

// serveGraph draait één GET door een router die alleen de graafroutes mount —
// zoals module.go ze mount, maar zonder de rest van de module ernaast.
func serveGraph(t *testing.T, m *Module, target string, anonymous bool) *httptest.ResponseRecorder {
	t.Helper()
	return serveOn(t, m.graphRoutes, httptest.NewRequest(http.MethodGet, target, nil), anonymous)
}

// decodeGraph parset een antwoordbody naar het gevraagde type en faalt netjes.
func decodeGraph[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return out
}

// noEdgeLister is een kennisgraaf zonder de optionele EdgeLister-port: het
// geval waarin de graaf geen relaties kan opsommen en dus 503 moet geven.
type noEdgeLister struct{ data.KnowledgeRepo }

// nodeIDs is de idlijst van een antwoord, voor korte assertions.
func nodeIDs(nodes []graphNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}
