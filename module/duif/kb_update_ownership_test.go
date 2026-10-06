package duif

import (
	"context"
	"errors"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

func TestKBUpdate_RemovedDerivedThemeCannotSurviveVectorReuse(t *testing.T) {
	i, w, f := importedUpdateFixture(t)
	id := derived(f.Project, tableDestillaat, "des-1")
	before, _ := w.GetNode(t.Context(), id)
	if before["thema_titel"] == nil {
		t.Fatal("fixture lacks the imported derived theme")
	}
	delete(f.Rels, "BELONGS_TO")
	if st := importFixture(t, i, f); st.State != ImportReady || w.updates != 2 {
		t.Fatal("unchanged evidence did not take metadata-only reuse path")
	}
	after, _ := w.GetNode(t.Context(), id)
	if after["thema_titel"] != nil {
		t.Fatal("removed derived theme survived the metadata-only update")
	}
}

func TestKBUpdate_DeletingOwnedNodeCannotCascadeForeignRelations(t *testing.T) {
	i, w, f := importedUpdateFixture(t)
	ctx := t.Context()
	obsolete := derived(f.Project, tablePersona, "pers-1")
	foreign := data.NodeID("persona:00000000-0000-4000-8000-000000000071")
	if _, err := w.CreateNodeWithID(ctx, foreign, map[string]any{"kb_id": "another-project"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.Relate(ctx, foreign, obsolete, "foreign_reference", map[string]any{"kb_id": "another-project"}); err != nil {
		t.Fatal(err)
	}
	delete(f.Nodes, "Persona")
	delete(f.Rels, "HAS_PERSPECTIVE")
	b, err := loadKBBundle(f.writeZip(t, "synthetic-snapshot.zip"))
	if err != nil {
		t.Fatal(err)
	}
	// A safe refusal is acceptable. Deleting another owner's relation is not.
	_ = i.reconcileBundle(ctx, b)
	edges, err := w.ListEdges(ctx, "foreign_reference", []data.NodeID{foreign}, nil)
	if err != nil || len(edges) != 1 {
		t.Fatal("owned snapshot deletion also removed another owner's incoming relation")
	}
	if node, _ := w.GetNode(ctx, obsolete); node == nil {
		t.Fatal("foreign reference was left without its retained target")
	}
}

type countReadFailureKnowledge struct{ *fakeKnowledge }

func (w *countReadFailureKnowledge) ListEdges(context.Context, string, []data.NodeID, []data.NodeID) ([]data.Edge, error) {
	return nil, errors.New("SYNTHETIC_PRIVATE_COUNT_ERROR")
}

func TestKBImport_CountReadFailureCannotWriteCompleteMarker(t *testing.T) {
	w := &countReadFailureKnowledge{&fakeKnowledge{}}
	logger, _ := captureLogs()
	i := testImporter(w, logger, false)
	st := importFixture(t, i, newMosaicFixture())
	if st.State == ImportReady {
		t.Fatal("failed relationship verification produced a complete import")
	}
	if got := readStatus(t, w); got.State == ImportReady {
		t.Fatal("failed relationship verification persisted a complete marker")
	}
}
