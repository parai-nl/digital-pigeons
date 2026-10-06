package duif

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/parai-nl/parai-core/data"
)

// Use the pinned core's public operations. CreateNodeWithID replaces vectors,
// whereas UpdateNode preserves them. No provider internals or direct SQL here.
type kbUpdateWriter interface {
	GetNode(context.Context, data.NodeID) (map[string]any, error)
	UpdateNode(context.Context, data.NodeID, map[string]any) error
	data.EdgeLister
	Unrelate(context.Context, data.NodeID, data.NodeID, string) error
}

// Legacy imports did not retain their precise embedding input/order. We can
// preserve their index only when its complete evidence basis is unchanged:
// same summary, same quote membership, text and grounding. This is content-set
// equivalence, not a claim that a fresh provider call produces identical bytes.
// The caller has already checked namespace, embedding model and dimension.
func (i *kbImporter) planEmbeddingReuse(ctx context.Context, b *kbBundle) (map[data.NodeID]bool, error) {
	w, ok := i.writer.(kbUpdateWriter)
	if !ok {
		return nil, nil // legacy writers can still perform a full import
	}
	quotes := make(map[data.NodeID]kbNode)
	for _, n := range b.Structure {
		if n.Table == tableQuote {
			quotes[n.ID] = n
		}
	}
	reuse := map[data.NodeID]bool{}
	var mu sync.Mutex
	err := i.parallel(ctx, len(b.Destillaten), func(ctx context.Context, idx int) error {
		d := b.Destillaten[idx]
		old, err := w.GetNode(ctx, d.Node.ID)
		if err != nil {
			return err
		}
		if old == nil || old["kb_id"] != b.KBID || !validKBVector(old["embedding"], i.embeddingDimension) || !sameEmbedSummary(b.Schema, old, d.Node.Props) {
			return nil
		}
		current, err := w.ListEdges(ctx, data.RelHasQuote, []data.NodeID{d.Node.ID}, nil)
		if err != nil {
			return err
		}
		want := map[data.NodeID]bool{}
		for _, e := range d.Edges {
			if e.Relation == data.RelHasQuote && e.From == d.Node.ID {
				want[e.To] = true
			}
		}
		if len(current) != len(want) {
			return nil
		}
		seen := map[data.NodeID]bool{}
		for _, e := range current {
			if e.From != d.Node.ID || !want[e.To] || seen[e.To] {
				return nil
			}
			seen[e.To] = true
			q, err := w.GetNode(ctx, e.To)
			if err != nil {
				return err
			}
			next, exists := quotes[e.To]
			if !exists || q == nil || q["kb_id"] != b.KBID || !sameQuoteBasis(q, next.Props) {
				return nil
			}
		}
		mu.Lock()
		reuse[d.Node.ID] = true
		mu.Unlock()
		return nil
	})
	return reuse, err
}

// sameEmbedSummary vergelijkt de samenvatting waarop geëmbed is, niet de
// samenvatting die op het scherm staat. Bij duiven/3 is dat de Engelse: een
// gewijzigde Nederlandse vertaling naast een onveranderde Engelse tekst hoeft
// geen nieuwe vector. Een knoop uit een vórige import die nog géén duiven/3 was
// komt nooit in aanmerking — zijn vector is op Nederlandse tekst gemaakt en is
// niet vergelijkbaar met een Engelse.
func sameEmbedSummary(schema string, old, next map[string]any) bool {
	if schema != kbV3Schema {
		return old["samenvatting"] == next["samenvatting"]
	}
	if old["bundle_schema"] != kbV3Schema {
		return false
	}
	key := kbLocalizedProp("samenvatting")
	english, ok := next[key].(string)
	return ok && old[key] == english
}

func sameQuoteBasis(a, b map[string]any) bool {
	// JSON normalizes in-memory int and database float64 quote indices.
	project := func(p map[string]any) []byte {
		out := map[string]any{}
		for _, key := range []string{"text", "grounded", "speaker", "quote_index"} {
			if value, present := p[key]; present {
				out[key] = value
			}
		}
		raw, _ := json.Marshal(out)
		return raw
	}
	return string(project(a)) == string(project(b))
}

func validKBVector(value any, dimension int) bool {
	vector, ok := value.([]float32) // the pinned PostgreSQL adapter's public DTO
	if !ok || dimension <= 0 || len(vector) != dimension {
		return false
	}
	nonzero := false
	for _, v := range vector {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
		nonzero = nonzero || v != 0
	}
	return nonzero
}

type kbEdgeKey struct {
	from, to data.NodeID
	relation string
}

// A duiven/2 bundle is a complete snapshot. Retire only rows/edges explicitly
// owned by this namespace; preserve other modules and unpublished chat data.
// In particular the old corpus Persona records disappear; product pigeons are
// configured independently and are not these research-corpus nodes.
func (i *kbImporter) reconcileBundle(ctx context.Context, b *kbBundle) error {
	w, ok := i.writer.(kbUpdateWriter)
	if !ok {
		return errors.New("knowledge snapshot update unavailable")
	}
	wantNodes := map[data.NodeID]bool{}
	wantEdges := map[kbEdgeKey]bool{}
	addEdge := func(e kbEdge) { wantEdges[kbEdgeKey{e.From, e.To, e.Relation}] = true }
	for _, n := range b.Structure {
		wantNodes[n.ID] = true
	}
	for _, d := range b.Destillaten {
		wantNodes[d.Node.ID] = true
		for _, e := range d.Edges {
			addEdge(e)
		}
	}
	for _, e := range b.StructureEdges {
		addEdge(e)
	}
	for _, e := range b.RelatesTo {
		addEdge(e)
	}
	var owned, obsolete []data.NodeID
	for _, table := range kbTables() {
		if table == tableImportMarker {
			continue
		}
		for offset := 0; ; offset += kbListPage {
			rows, err := i.writer.ListNodes(ctx, table, data.ListOpts{Limit: kbListPage, Offset: offset})
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row["kb_id"] != b.KBID {
					continue
				}
				id := data.NodeID(rowID(row))
				if id == "" {
					return errors.New("knowledge snapshot node missing id")
				}
				owned = append(owned, id)
				if !wantNodes[id] {
					obsolete = append(obsolete, id)
				}
			}
			if len(rows) < kbListPage {
				break
			}
		}
	}
	sort.Slice(owned, func(a, b int) bool { return owned[a] < owned[b] })
	// Core Delete removes ALL incident edges, even an unknown relation from
	// another module. Check both directions without restricting the relation
	// name before any pruning. A shared obsolete node needs explicit dependency
	// resolution; a complete marker must not hide its destructive removal.
	for start := 0; start < len(obsolete); start += kbListPage {
		ids := obsolete[start:min(start+kbListPage, len(obsolete))]
		for _, direction := range []struct{ from, to []data.NodeID }{{from: ids}, {to: ids}} {
			edges, err := w.ListEdges(ctx, "", direction.from, direction.to)
			if err != nil {
				return err
			}
			for _, e := range edges {
				if e.Props["kb_id"] != b.KBID {
					return errors.New("knowledge snapshot has foreign references")
				}
			}
		}
	}
	for _, name := range sortedKeys(kbRelations) {
		relation := kbRelations[name]
		for start := 0; start < len(owned); start += kbListPage {
			edges, err := w.ListEdges(ctx, relation, owned[start:min(start+kbListPage, len(owned))], nil)
			if err != nil {
				return err
			}
			for _, e := range edges {
				if e.Props["kb_id"] != b.KBID || wantEdges[kbEdgeKey{e.From, e.To, relation}] {
					continue
				}
				if err := i.retry(ctx, func() error { return w.Unrelate(ctx, e.From, e.To, relation) }); err != nil {
					return err
				}
			}
		}
	}
	return i.parallel(ctx, len(obsolete), func(ctx context.Context, idx int) error {
		return i.retry(ctx, func() error { return i.writer.Delete(ctx, obsolete[idx]) })
	})
}
