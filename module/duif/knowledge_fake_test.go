package duif

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/parai-nl/parai-core/data"
)

// fakeKnowledge is de in-memory kennisgraaf voor tests: nodes met props en
// edges met een relatie, precies genoeg om de leesseam van de turn
// (SemanticSearch, GetDestillaat, GetQuote, GetWithContext, GetNode), de
// schrijfseam van de import (CreateNodeWithID, Relate, ListNodes, Delete) en
// de graaf-endpoints (ListEdges, Neighbors) zonder Postgres te toetsen.
//
// De typed reads volgen core's gedrag: props worden via JSON in de
// data-structs gegoten (onbekende props vallen weg), GetWithContext volgt
// has_quote, belongs_to_perspective en de inkomende contains-edge, en een
// node die niet bestaat is (nil, nil). Alles wat hier niet staat panikeert via
// de embedded nil-interface — bewust: de seams mogen het niet aanraken.
//
// `results`/`err` sturen SemanticSearch zoals de oude mockKnowledge deed;
// staan ze leeg, dan levert de zoekopdracht alle nodes van de tabel mét een
// embedding, op volgorde van `score` in de props (default 1).
type fakeKnowledge struct {
	data.KnowledgeRepo
	mu      sync.Mutex
	nodes   map[data.NodeID]map[string]any
	embeds  map[data.NodeID]string // embedText per node ("" = geen embedding)
	edges   []data.Edge
	results []data.SemanticResult
	err     error
	// tellers voor idempotentie- en gedragstoetsen
	calls   int
	writes  int
	relates int
	deletes int
	// failOn laat één schrijfoperatie falen (id-prefix of relatie), voor
	// de foutpaden van de importer.
	failOn func(op string, id data.NodeID) error
	// onSearch mag per zoekaanroep (1-based, met de tabel) een uitkomst
	// opdringen: zo scoort dezelfde graaf anders voor de ene vraag dan voor
	// de andere, wat de echte vectorzoekopdracht ook doet.
	onSearch func(call int, table string) ([]data.SemanticResult, bool)
}

// mockKnowledge is de oude naam; de bestaande tests bouwen hem met
// `results`/`err` en dat blijft werken.
type mockKnowledge = fakeKnowledge

func (f *fakeKnowledge) init() {
	if f.nodes == nil {
		f.nodes = map[data.NodeID]map[string]any{}
	}
	if f.embeds == nil {
		f.embeds = map[data.NodeID]string{}
	}
}

func (f *fakeKnowledge) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeKnowledge) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes + f.relates + f.deletes
}

func cloneProps(p map[string]any) map[string]any {
	out := make(map[string]any, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}

// --- schrijven (importseam) -------------------------------------------------

func (f *fakeKnowledge) CreateNodeWithID(_ context.Context, id data.NodeID, props map[string]any, embedText string) (data.NodeID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	if f.failOn != nil {
		if err := f.failOn("create", id); err != nil {
			return "", err
		}
	}
	if existing, ok := f.nodes[id]; ok && existing["_table"] != tableOf(string(id)) {
		return "", errors.New("fake: id already exists under a different table")
	}
	p := cloneProps(props)
	p["_table"] = tableOf(string(id))
	f.nodes[id] = p
	f.embeds[id] = embedText
	f.writes++
	return id, nil
}

func (f *fakeKnowledge) Relate(_ context.Context, from, to data.NodeID, relation string, props map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn != nil {
		if err := f.failOn("relate", from); err != nil {
			return err
		}
	}
	for i := range f.edges {
		if e := f.edges[i]; e.From == from && e.To == to && e.Relation == relation {
			// Zoals de echte upsert: dezelfde relatie komt er niet dubbel in,
			// maar de props worden wél bijgewerkt. Over de range-variabele
			// schrijven muteerde een kopie, en dan bleef `import_hash` van een
			// vorige import staan.
			f.edges[i].Props = props
			return nil
		}
	}
	f.edges = append(f.edges, data.Edge{Relation: relation, From: from, To: to, Props: props})
	f.relates++
	return nil
}

func (f *fakeKnowledge) Delete(_ context.Context, id data.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	delete(f.nodes, id)
	delete(f.embeds, id)
	kept := f.edges[:0]
	for _, e := range f.edges {
		if e.From != id && e.To != id {
			kept = append(kept, e)
		}
	}
	f.edges = kept
	f.deletes++
	return nil
}

// --- lezen ------------------------------------------------------------------

func (f *fakeKnowledge) GetNode(_ context.Context, id data.NodeID) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	p, ok := f.nodes[id]
	if !ok {
		return nil, nil
	}
	out := cloneProps(p)
	delete(out, "_table")
	// Zoals de Postgres-adapter: de id is een data.NodeID, geen string.
	out["id"] = id
	return out, nil
}

func (f *fakeKnowledge) ListNodes(_ context.Context, table string, opts data.ListOpts) ([]map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	var ids []string
	for id, p := range f.nodes {
		if p["_table"] == table {
			ids = append(ids, string(id))
		}
	}
	sort.Strings(ids)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var out []map[string]any
	for i, id := range ids {
		if i < opts.Offset {
			continue
		}
		if len(out) >= limit {
			break
		}
		p := cloneProps(f.nodes[data.NodeID(id)])
		delete(p, "_table")
		p["id"] = data.NodeID(id)
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeKnowledge) ListEdges(_ context.Context, relation string, fromIDs, toIDs []data.NodeID) ([]data.Edge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	from := map[data.NodeID]bool{}
	for _, id := range fromIDs {
		from[id] = true
	}
	to := map[data.NodeID]bool{}
	for _, id := range toIDs {
		to[id] = true
	}
	var out []data.Edge
	for _, e := range f.edges {
		if relation != "" && e.Relation != relation {
			continue
		}
		if len(from) > 0 && !from[e.From] {
			continue
		}
		if len(to) > 0 && !to[e.To] {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Neighbors volgt uitgaande edges (depth 1, zoals core).
func (f *fakeKnowledge) Neighbors(_ context.Context, id data.NodeID, relation string, depth int) ([]data.NodeID, error) {
	if depth > 1 {
		return nil, errors.New("fake: depth > 1 not supported")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []data.NodeID
	for _, e := range f.edges {
		if e.From == id && (relation == "" || e.Relation == relation) {
			out = append(out, e.To)
		}
	}
	return out, nil
}

func (f *fakeKnowledge) incoming(id data.NodeID, relation string) []data.NodeID {
	var out []data.NodeID
	for _, e := range f.edges {
		if e.To == id && e.Relation == relation {
			out = append(out, e.From)
		}
	}
	return out
}

func (f *fakeKnowledge) SemanticSearch(_ context.Context, table string, _ []float32, k int) ([]data.SemanticResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.onSearch != nil {
		if out, ok := f.onSearch(f.calls, table); ok {
			return out, nil
		}
	}
	if f.results != nil {
		// Gescripte uitkomsten horen bij de standpunten; het quote-kanaal van
		// RAGRetrieve blijft dan leeg, zoals bij een echte KB zonder
		// quote-embeddings.
		if table == "quote" {
			return nil, nil
		}
		return f.results, nil
	}
	if k <= 0 {
		k = 10
	}
	var out []data.SemanticResult
	for id, p := range f.nodes {
		if p["_table"] != table || f.embeds[id] == "" {
			continue
		}
		score := float32(1)
		if s, ok := p["score"].(float64); ok {
			score = float32(s)
		}
		node := cloneProps(p)
		delete(node, "_table")
		out = append(out, data.SemanticResult{ID: id, Score: score, Node: node})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

// failingKnowledge laat gekozen operaties op de gedeelde fake falen. Eén
// decorator voor alle foutpaden: de graaf toetst er zijn drie leespaden mee
// (marker, tabel, relaties) en de import zijn lijst-, verwijder- en
// relatiepaden. Een lege filterwaarde betekent "elke tabel" of "elke node".
//
// De methoden staan op een waarde-ontvanger, zodat zowel failingKnowledge{…}
// als &failingKnowledge{…} de leesseams en de schrijfseam vervult.
type failingKnowledge struct {
	*fakeKnowledge
	listErr    error
	listTable  string
	getNodeErr error
	getNodeFor data.NodeID
	deleteErr  error
	edgesErr   error
}

func (f failingKnowledge) ListNodes(ctx context.Context, table string, opts data.ListOpts) ([]map[string]any, error) {
	if f.listErr != nil && (f.listTable == "" || f.listTable == table) {
		return nil, f.listErr
	}
	return f.fakeKnowledge.ListNodes(ctx, table, opts)
}

func (f failingKnowledge) GetNode(ctx context.Context, id data.NodeID) (map[string]any, error) {
	if f.getNodeErr != nil && (f.getNodeFor == "" || f.getNodeFor == id) {
		return nil, f.getNodeErr
	}
	return f.fakeKnowledge.GetNode(ctx, id)
}

func (f failingKnowledge) ListEdges(ctx context.Context, relation string, fromIDs, toIDs []data.NodeID) ([]data.Edge, error) {
	if f.edgesErr != nil {
		return nil, f.edgesErr
	}
	return f.fakeKnowledge.ListEdges(ctx, relation, fromIDs, toIDs)
}

func (f failingKnowledge) Delete(ctx context.Context, id data.NodeID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.fakeKnowledge.Delete(ctx, id)
}

// typed giet de props van één node in een data-struct, zoals core's typed reads.
func typed[T any](f *fakeKnowledge, id data.NodeID, table string) (*T, error) {
	p, ok := f.nodes[id]
	if !ok || p["_table"] != table {
		return nil, nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (f *fakeKnowledge) GetDestillaat(_ context.Context, id data.NodeID) (*data.Destillaat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	d, err := typed[data.Destillaat](f, id, "destillaat")
	if d != nil {
		d.ID = id
	}
	return d, err
}

func (f *fakeKnowledge) GetQuote(_ context.Context, id data.NodeID) (*data.Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	q, err := typed[data.Quote](f, id, "quote")
	if q != nil {
		q.ID = id
	}
	return q, err
}

func (f *fakeKnowledge) GetPerspectief(_ context.Context, id data.NodeID) (*data.Perspective, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	p, err := typed[data.Perspective](f, id, "perspective")
	if p != nil {
		p.ID = id
	}
	return p, err
}

func (f *fakeKnowledge) GetInterview(_ context.Context, id data.NodeID) (*data.Interview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	i, err := typed[data.Interview](f, id, "interview")
	if i != nil {
		i.ID = id
	}
	return i, err
}

func (f *fakeKnowledge) GetWithContext(ctx context.Context, id data.NodeID) (*data.DestillaatContext, error) {
	d, err := f.GetDestillaat(ctx, id)
	if err != nil || d == nil {
		return nil, err
	}
	out := &data.DestillaatContext{Destillaat: *d}
	f.mu.Lock()
	quoteIDs := []data.NodeID{}
	perspIDs := []data.NodeID{}
	for _, e := range f.edges {
		if e.From != id {
			continue
		}
		switch e.Relation {
		case data.RelHasQuote:
			quoteIDs = append(quoteIDs, e.To)
		case data.RelBelongsToPerspectief:
			perspIDs = append(perspIDs, e.To)
		}
	}
	interviewIDs := f.incoming(id, data.RelContains)
	f.mu.Unlock()
	for _, qid := range quoteIDs {
		if q, err := f.GetQuote(ctx, qid); err == nil && q != nil {
			out.Quotes = append(out.Quotes, *q)
		}
	}
	if len(perspIDs) > 0 {
		if p, err := f.GetPerspectief(ctx, perspIDs[0]); err == nil {
			out.Perspective = p
		}
	}
	if len(interviewIDs) > 0 {
		if i, err := f.GetInterview(ctx, interviewIDs[0]); err == nil {
			out.Interview = i
		}
	}
	return out, nil
}
