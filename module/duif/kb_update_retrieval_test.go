package duif

import (
	"context"
	"errors"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

func TestRetrievalRefusesIncompleteImportBeforeProviderOrSearch(t *testing.T) {
	for _, state := range []ImportState{ImportImporting, ImportIncomplete, ImportFailed} {
		t.Run(string(state), func(t *testing.T) {
			// A nil pipeline would panic if any work started before the guard.
			r := &retrieval{status: func() importStatus { return importStatus{State: state} }}
			got, err := r.run(context.Background(), "A pigeon question")
			if !errors.Is(err, errKBUnavailable) || len(got.Sources) > 0 {
				t.Fatal("incomplete corpus became answer evidence")
			}
		})
	}
}

func TestRetrievalDiscardsEvidenceIfImportStartsDuringSearch(t *testing.T) {
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "old", samenvatting: "A summary", quote: "An original passage", paper: "Research paper", score: 0.9})
	status := testImporter(f, nil, false)
	status.setStatus(importStatus{State: ImportReady})
	f.onSearch = func(_ int, _ string) ([]data.SemanticResult, bool) {
		status.setStatus(importStatus{State: ImportImporting})
		return nil, false
	}
	r := newTestRetrieval(t, f, nil)
	r.status = status.snapshot
	got, err := r.run(context.Background(), "A pigeon question")
	if !errors.Is(err, errKBUnavailable) || len(got.Sources) > 0 {
		t.Fatal("an in-flight read survived a partial corpus update")
	}
}

// Een lege of nooit geïmporteerde kennisbank is een storing, geen kennisgat.
// Er is dan niets onderzocht, dus er is ook geen corpus waarover een gat iets
// kan zeggen.
//
// De nil-pipeline is hier het bewijsmiddel: kwam de poort niet vóór de embedder
// en de zoekopdracht, dan paniekt deze test in plaats van te falen.
func TestRetrievalTreatsAnEmptyKnowledgeBaseAsFailureNotGap(t *testing.T) {
	routes := map[string]func(*retrieval) func(context.Context, string) (retrievalResult, error){
		"chat":        func(r *retrieval) func(context.Context, string) (retrievalResult, error) { return r.run },
		"vergadering": func(r *retrieval) func(context.Context, string) (retrievalResult, error) { return r.runMeeting },
		"verwant":     func(r *retrieval) func(context.Context, string) (retrievalResult, error) { return r.runRelated },
	}
	for naam, route := range routes {
		t.Run(naam, func(t *testing.T) {
			r := &retrieval{status: func() importStatus { return importStatus{State: ImportEmpty} }}
			got, err := route(r)(context.Background(), "Kunnen stadsduiven ziekten overdragen op mensen?")
			if !errors.Is(err, errKBUnavailable) {
				t.Fatalf("err = %v, want errKBUnavailable — een lege kennisbank is een storing", err)
			}
			if got.Status == KBGap || len(got.Sources) > 0 {
				t.Fatalf("een lege kennisbank kwam als inhoudelijke uitkomst terug: %+v", got)
			}
		})
	}
}

// Een bank die wél geladen is en niets vindt, blijft een gewoon kennisgat: de
// poort van hierboven mag de gatroute niet meenemen.
func TestRetrievalKeepsTheGapWhenTheImportIsReady(t *testing.T) {
	f := &fakeKnowledge{}
	r := newTestRetrieval(t, f, nil)
	r.status = func() importStatus { return importStatus{State: ImportReady} }
	got, err := r.run(context.Background(), "Kunnen stadsduiven ziekten overdragen op mensen?")
	if err != nil {
		t.Fatalf("een geladen bank zonder treffers hoort geen storing te zijn: %v", err)
	}
	if got.Status != KBGap {
		t.Fatalf("status = %q, want %q", got.Status, KBGap)
	}
}
