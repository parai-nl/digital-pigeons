package duif

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

// Model the pinned adapter's vector-preserving JSON merge, not a second
// implementation of the importer. Existing fixture embeds retain their text.
type updateKnowledge struct {
	*fakeKnowledge
	readFailure   bool
	updateFailure bool
	updates       int
}

func (w *updateKnowledge) GetNode(ctx context.Context, id data.NodeID) (map[string]any, error) {
	if w.readFailure {
		return nil, errors.New(kbSentinel)
	}
	p, err := w.fakeKnowledge.GetNode(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.embeds[id] != "" {
		vec := make([]float32, 1536)
		vec[0] = 1
		p["embedding"] = vec
	}
	return p, nil
}

func (w *updateKnowledge) UpdateNode(_ context.Context, id data.NodeID, props map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.updateFailure {
		return errors.New(kbSentinel)
	}
	if w.nodes[id] == nil {
		return errors.New("missing fixture node")
	}
	for key, value := range props {
		w.nodes[id][key] = value
	}
	w.updates++
	return nil
}

func (w *updateKnowledge) Unrelate(_ context.Context, from, to data.NodeID, relation string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	kept := w.edges[:0]
	for _, e := range w.edges {
		if e.From != from || e.To != to || e.Relation != relation {
			kept = append(kept, e)
		}
	}
	w.edges = kept
	return nil
}

func importedUpdateFixture(t *testing.T) (*kbImporter, *updateKnowledge, *mosaicFixture) {
	t.Helper()
	w := &updateKnowledge{fakeKnowledge: &fakeKnowledge{}}
	logger, _ := captureLogs()
	i := testImporter(w, logger, false)
	f := newMosaicFixture()
	if st := importFixture(t, i, f); st.State != ImportReady {
		t.Fatalf("initial import: %+v", st)
	}
	return i, w, f
}

func TestKBUpdateBibliographyPreservesVectorAndRetiresRemovedProps(t *testing.T) {
	i, w, f := importedUpdateFixture(t)
	before := w.embeds[derived(f.Project, tableDestillaat, "des-1")]
	f.Nodes["Interview"][0]["title"] = "A fuller bibliographic title"
	delete(f.Nodes["Destillaat"][0], "persona")
	st := importFixture(t, i, f)
	if st.State != ImportReady || w.updates != 2 {
		t.Fatalf("metadata update: %+v / preserved %d", st, w.updates)
	}
	node, _ := w.GetNode(context.Background(), derived(f.Project, tableDestillaat, "des-1"))
	if node["persona"] != nil {
		t.Fatal("removed persona still attached")
	}
	if w.embeds[derived(f.Project, tableDestillaat, "des-1")] != before {
		t.Fatal("unchanged evidence embedded again")
	}
	if got := readStatus(t, w); got.State != ImportReady {
		t.Fatalf("persisted status: %+v", got)
	}
}

func readStatus(t *testing.T, w knowledgeLister) importStatus {
	t.Helper()
	st, err := readImportStatus(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestKBUpdateChangedEvidenceRequiresNewEmbedding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*mosaicFixture)
	}{
		{"summary", func(f *mosaicFixture) { f.Nodes["Destillaat"][0]["samenvatting"] = "Changed evidence summary" }},
		{"quote", func(f *mosaicFixture) { f.Nodes["Quote"][1]["text"] = "Changed original passage" }},
		{"grounding", func(f *mosaicFixture) { f.Nodes["Quote"][1]["grounded"] = false }},
		{"quote membership", func(f *mosaicFixture) { f.Rels["HAS_QUOTE"] = f.Rels["HAS_QUOTE"][1:] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, w, f := importedUpdateFixture(t)
			tc.change(f)
			if st := importFixture(t, i, f); st.State != ImportReady {
				t.Fatalf("update: %+v", st)
			}
			if w.updates != 1 {
				t.Fatalf("preserved %d, want only unchanged second source", w.updates)
			}
		})
	}
}

func TestKBUpdatePreflightFailureMakesNoWrites(t *testing.T) {
	i, w, f := importedUpdateFixture(t)
	f.Nodes["Interview"][0]["title"] = "Updated title"
	before := w.writeCount()
	w.readFailure = true
	st := importFixture(t, i, f)
	if st.Reason != ReasonImportUpdateCheckFailed || w.writeCount() != before {
		t.Fatalf("preflight mutated graph: %+v", st)
	}
	if got := readStatus(t, w); got.State != ImportReady {
		t.Fatal("old untouched corpus lost its marker")
	}
}

func TestKBUpdatePartialWriteCannotKeepSuccessMarker(t *testing.T) {
	i, w, f := importedUpdateFixture(t)
	f.Nodes["Interview"][0]["title"] = "Updated title"
	w.updateFailure = true
	st := importFixture(t, i, f)
	if st.State != ImportFailed {
		t.Fatalf("update: %+v", st)
	}
	if got := readStatus(t, w); got.State != ImportIncomplete {
		t.Fatalf("old marker survived partial update: %+v", got)
	}
}

func TestKBUpdateSnapshotRetiresOwnedStaleRowsAndEdges(t *testing.T) {
	i, w, f := importedUpdateFixture(t)
	ctx := context.Background()
	foreign := data.NodeID("persona:foreign")
	w.CreateNodeWithID(ctx, foreign, map[string]any{"kb_id": "another-project"}, "")
	// A changed complete snapshot removes the corpus persona and one relation
	// between retained nodes. Other namespace rows/edges must survive.
	d1, d2 := derived(f.Project, tableDestillaat, "des-1"), derived(f.Project, tableDestillaat, "des-2")
	w.Relate(ctx, d2, d1, data.RelRelatesTo, map[string]any{"kb_id": "another-project"})
	delete(f.Nodes, "Persona")
	delete(f.Rels, "HAS_PERSPECTIVE")
	delete(f.Rels, "RELATES_TO")
	b, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := i.reconcileBundle(ctx, b); err != nil {
		t.Fatal(err)
	}
	if row, _ := w.GetNode(ctx, derived(f.Project, tablePersona, "pers-1")); row != nil {
		t.Fatal("obsolete persona survives")
	}
	if row, _ := w.GetNode(ctx, foreign); row == nil {
		t.Fatal("foreign row deleted")
	}
	edges, err := w.ListEdges(ctx, data.RelRelatesTo, []data.NodeID{d1, d2}, nil)
	if err != nil || len(edges) != 1 || edges[0].From != d2 {
		t.Fatal("snapshot failed to distinguish owned and foreign edges")
	}
}

func TestKBUpdateVectorValidation(t *testing.T) {
	for _, value := range []any{nil, []float32{}, []float32{0, 0}, []float32{1}, []any{1, 0}} {
		if validKBVector(value, 2) {
			t.Fatalf("invalid vector accepted: %T", value)
		}
	}
	if !validKBVector([]float32{1, 0}, 2) {
		t.Fatal("valid vector rejected")
	}
}

func TestKBImportDebugNeverEchoesPrivateError(t *testing.T) {
	w := &fakeKnowledge{failOn: func(string, data.NodeID) error { return errors.New(kbSentinel) }}
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	i := testImporter(w, logger, false)
	importFixture(t, i, newMosaicFixture())
	if strings.Contains(logs.String(), kbSentinel) {
		t.Fatal("debug logged private provider error")
	}
	if !strings.Contains(logs.String(), ReasonImportWriteFailed) {
		t.Fatal("fixed diagnostic missing")
	}
}
