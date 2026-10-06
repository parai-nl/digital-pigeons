package duif

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Het wire-contract van de drie graafroutes: statuscodes, headers, de vorm van
// de JSON en — de belangrijkste toets — dat er niets op de wire staat wat er
// niet op hoort.

func TestGraphRoute_Overview(t *testing.T) {
	m := newGraphFixture(t).build().foreign().module()
	rec := serveGraph(t, m, "/v1/graph/overview", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, wil no-store", got)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body: %v", err)
	}
	for _, key := range []string{"nodes", "relationships", "totals", "childCounts", "kb"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("veld %q ontbreekt in de overview: %s", key, rec.Body.String())
		}
	}
	out := decodeGraph[graphOverviewResponse](t, rec)
	if out.KB.State != string(ImportReady) || len(out.Nodes) != 5 {
		t.Fatalf("overview = %+v", out)
	}
	// De relatievelden zijn bewust snake_case (de vorm die de renderer leest).
	if !strings.Contains(rec.Body.String(), `"source_id"`) || !strings.Contains(rec.Body.String(), `"relationship_type"`) {
		t.Fatalf("relatievorm klopt niet: %s", rec.Body.String())
	}
}

func TestGraphRoute_EmptyOverviewIsNeverNull(t *testing.T) {
	// Zonder marker moeten nodes en relationships lege arrays zijn, geen null:
	// de frontend mag er zonder guard overheen lopen.
	f := newGraphFixture(t)
	rec := serveGraph(t, f.module(), "/v1/graph/overview", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"nodes":[]`) || !strings.Contains(body, `"relationships":[]`) || !strings.Contains(body, `"totals":{}`) {
		t.Fatalf("lege overview = %s", body)
	}
	// childCounts is camelCase (de snake_case-uitzondering geldt alleen voor
	// relationships) en is een lege map, geen null.
	if !strings.Contains(body, `"childCounts":{}`) {
		t.Fatalf("childCounts ontbreekt of is null: %s", body)
	}
	if !strings.Contains(body, `"kb":{"state":"empty"}`) {
		t.Fatalf("kb-stand ontbreekt: %s", body)
	}
}

func TestGraphRoute_Children(t *testing.T) {
	m := newGraphFixture(t).build().module()
	rec := serveGraph(t, m, "/v1/graph/nodes/"+gid(tableDomein, 1)+"/children", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	out := decodeGraph[graphChildrenResponse](t, rec)
	if len(out.Nodes) != 2 || out.Total != 2 || out.Truncated {
		t.Fatalf("children = %+v", out)
	}
	// nextOffset staat er alleen als er meer is.
	if strings.Contains(rec.Body.String(), "nextOffset") {
		t.Fatalf("nextOffset op een volledige pagina: %s", rec.Body.String())
	}
}

func TestGraphRoute_ChildrenPagination(t *testing.T) {
	sub := gid(tableSubthema, 1)
	m := newGraphFixture(t).build().destillaten(sub, 250).module()

	rec := serveGraph(t, m, "/v1/graph/nodes/"+sub+"/children?offset=0", false)
	first := decodeGraph[graphChildrenResponse](t, rec)
	if !first.Truncated || first.NextOffset == nil || *first.NextOffset != 150 || first.Total != 251 {
		t.Fatalf("eerste pagina = %+v", first)
	}
	rec = serveGraph(t, m, "/v1/graph/nodes/"+sub+"/children?offset=150", false)
	second := decodeGraph[graphChildrenResponse](t, rec)
	if second.Truncated || len(second.Nodes) != 101 {
		t.Fatalf("tweede pagina = %+v", second)
	}
}

func TestGraphRoute_InvalidOffset(t *testing.T) {
	m := newGraphFixture(t).build().module()
	for _, q := range []string{"?offset=abc", "?offset=-1", "?offset=1.5", "?offset=", "?offset=+3"} {
		t.Run(q, func(t *testing.T) {
			rec := serveGraph(t, m, "/v1/graph/nodes/"+gid(tableDomein, 1)+"/children"+q, false)
			if q == "?offset=" {
				// Leeg is "geen offset": dat is 0, geen fout.
				if rec.Code != http.StatusOK {
					t.Fatalf("lege offset = %d", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, wil 400: %s", rec.Code, rec.Body.String())
			}
			assertAPIError(t, rec.Body.Bytes(), ReasonInvalidOffset)
		})
	}
}

func TestGraphRoute_NodeWithTrail(t *testing.T) {
	m := newGraphFixture(t).build().module()
	rec := serveGraph(t, m, "/v1/graph/nodes/"+gid(tableQuote, 1), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeGraph[graphNodeResponse](t, rec)
	if out.Node.ID != gid(tableQuote, 1) || len(out.Trail) != 4 {
		t.Fatalf("node = %+v", out)
	}
	// Het perspectief hing vroeger als zij-tak aan een standpunt; de graaf
	// toont het niet meer, dus ook niet op de wire.
	if strings.Contains(rec.Body.String(), "perspective") {
		t.Fatalf("perspective op de wire: %s", rec.Body.String())
	}
	rec = serveGraph(t, m, "/v1/graph/nodes/"+gid(tableDomein, 1), false)
	if !strings.Contains(rec.Body.String(), `"trail":[]`) {
		t.Fatalf("leeg spoor hoort een lege array te zijn: %s", rec.Body.String())
	}
}

// De frontend codeert de dubbele punt in de id (`destillaat%3A…`); de route
// moet die decoderen, anders is elke "open in de kennisgraaf" een 404.
func TestGraphRoute_NodeAcceptsEncodedID(t *testing.T) {
	m := newGraphFixture(t).build().module()
	encoded := strings.Replace(gid(tableQuote, 1), ":", "%3A", 1)
	rec := serveGraph(t, m, "/v1/graph/nodes/"+encoded, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveGraph(t, m, "/v1/graph/nodes/"+encoded+"/children", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("children status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGraphRoute_NodeNotFound(t *testing.T) {
	m := newGraphFixture(t).build().foreign().module()
	for _, id := range []string{
		gid(tableThema, 77),
		gid(tableDomein, 900), // andere kennisbank
		gid("run", 1),         // tabel buiten de allowlist
		"kapot",
	} {
		for _, suffix := range []string{"", "/children"} {
			rec := serveGraph(t, m, "/v1/graph/nodes/"+id+suffix, false)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s%s: status = %d, wil 404: %s", id, suffix, rec.Code, rec.Body.String())
			}
			assertAPIError(t, rec.Body.Bytes(), ReasonNodeNotFound)
		}
	}
}

func TestGraphRoute_WithoutASignedInUser(t *testing.T) {
	m := newGraphFixture(t).build().module()
	for _, target := range []string{
		"/v1/graph/overview",
		"/v1/graph/nodes/" + gid(tableThema, 1),
		"/v1/graph/nodes/" + gid(tableThema, 1) + "/children",
	} {
		rec := serveGraph(t, m, target, true)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, wil 401", target, rec.Code)
		}
	}
}

func TestGraphRoute_WithoutAKnowledgeGraph(t *testing.T) {
	m := newGraphModule(t, nil)
	for _, target := range []string{
		"/v1/graph/overview",
		"/v1/graph/nodes/" + gid(tableThema, 1),
		"/v1/graph/nodes/" + gid(tableThema, 1) + "/children",
	} {
		rec := serveGraph(t, m, target, false)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, wil 503", target, rec.Code)
		}
		assertAPIError(t, rec.Body.Bytes(), ReasonGraphUnavailable)
	}
}

func TestGraphRoute_WithoutAnEdgeLister(t *testing.T) {
	// ListEdges is een optionele port van core. Een repo zonder die port kan
	// geen relaties tonen; dat is een zichtbare 503, geen halve graaf.
	f := newGraphFixture(t).build()
	m := newGraphModule(t, noEdgeLister{KnowledgeRepo: f.k})
	rec := serveGraph(t, m, "/v1/graph/overview", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, wil 503: %s", rec.Code, rec.Body.String())
	}
	assertAPIError(t, rec.Body.Bytes(), ReasonGraphUnavailable)
}

func TestGraphRoute_NoSecretsOnTheWire(t *testing.T) {
	// De harde privacytoets: geen enkele route mag props dragen die
	// server-side horen te blijven — ook niet als de fixture ze wél draagt.
	m := newGraphFixture(t).build().module()
	targets := []string{
		"/v1/graph/overview",
		"/v1/graph/nodes/" + gid(tableInterview, 1),
		"/v1/graph/nodes/" + gid(tableInterview, 1) + "/children",
		"/v1/graph/nodes/" + gid(tableDestillaat, 1),
		"/v1/graph/nodes/" + gid(tableDestillaat, 1) + "/children",
		"/v1/graph/nodes/" + gid(tableQuote, 1),
		"/v1/graph/nodes/" + gid(tableSubthema, 1) + "/children",
	}
	verboden := []string{"storage_key", "embedding", "content_hash", "import_hash", "opslag/intern", "deadbeef", "cafe"}
	for _, target := range targets {
		rec := serveGraph(t, m, target, false)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		body := rec.Body.String()
		for _, verbod := range verboden {
			if strings.Contains(body, verbod) {
				t.Fatalf("%s: %q staat op de wire: %s", target, verbod, body)
			}
		}
	}
}

// assertAPIError toetst de foutvorm van de module: stabiele code plus een
// tweetalige melding, en nooit een onderliggende fouttekst.
func assertAPIError(t *testing.T, body []byte, reason string) {
	t.Helper()
	var out apiError
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("foutbody %q: %v", body, err)
	}
	if out.Error != reason {
		t.Fatalf("error = %q, wil %q", out.Error, reason)
	}
	if out.Message["en"] == "" || out.Message["nl"] == "" {
		t.Fatalf("melding niet tweetalig: %+v", out.Message)
	}
}

func TestWriteGraphError_AnUnknownErrorBecomes503(t *testing.T) {
	// Een fout zonder reden-code kan hier niet ontstaan, maar de wire moet
	// sluitend zijn: nooit een lege body of een kale 200.
	rec := httptest.NewRecorder()
	writeGraphError(rec, errors.New("iets anders"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertAPIError(t, rec.Body.Bytes(), ReasonGraphUnavailable)
}

func TestGraphRoute_OverviewReadFailure(t *testing.T) {
	f := newGraphFixture(t).build()
	k := &failingKnowledge{fakeKnowledge: f.k, edgesErr: errors.New("weg")}
	rec := serveGraph(t, newGraphModule(t, k), "/v1/graph/overview", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	assertAPIError(t, rec.Body.Bytes(), ReasonGraphUnavailable)
}
