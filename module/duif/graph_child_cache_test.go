package duif

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/data"
)

// nodeCounter telt de GetNode-aanroepen van de graaf. De teller is atomair,
// want fetchNodes leest met acht werkers tegelijk.
type nodeCounter struct {
	*fakeKnowledge
	reads atomic.Int64
}

func (c *nodeCounter) GetNode(ctx context.Context, id data.NodeID) (map[string]any, error) {
	c.reads.Add(1)
	return c.fakeKnowledge.GetNode(ctx, id)
}

// De telling achter de klik is exact doordat élk kandidaat-kind gelezen
// wordt. Zonder cache gebeurde dat per verzoek opnieuw, dus een ouder met N kinderen kostte
// een bladertocht N reads per pagina in plaats van N in totaal. De gecachete
// kinderlijst betaalt die ronde één keer.
func TestGraphChildren_PagedWalkPaysTheFullReadOnce(t *testing.T) {
	const kinderen = 900
	f := newGraphFixture(t).build()
	f.destillaten(gid(tableSubthema, 1), kinderen)
	c := &nodeCounter{fakeKnowledge: f.k}
	g := &graphService{nodes: c, edges: c}

	gezien, total := walkChildren(t, g, gid(tableSubthema, 1))
	// Het ene standpunt uit de basis-fixture hangt er ook onder.
	if total != kinderen+1 || len(gezien) != kinderen+1 {
		t.Fatalf("de bladertocht gaf total=%d en %d unieke kinderen, wil %d", total, len(gezien), kinderen+1)
	}
	reads := int(c.reads.Load())
	t.Logf("%d GetNode-aanroepen voor %d kinderen", reads, kinderen+1)
	// Zonder cache: één volledige ronde per pagina (7 × 901 ≈ 6300). Met cache:
	// één volledige ronde plus de nodes van de pagina's erna.
	if wil := 2 * (kinderen + 1); reads > wil {
		t.Errorf("%d GetNode-aanroepen voor %d kinderen: de volledige ronde wordt per pagina herhaald (hoogstens %d)", reads, kinderen+1, wil)
	}
}

// De cache mag geen tweede waarheid worden: zodra een import klaar is staat er
// andere data in de graaf, en dan hoort de telling opnieuw geteld te worden.
func TestGraphChildCache_ImportInvalidatesTheList(t *testing.T) {
	f := newGraphFixture(t).build()
	g := f.service()
	ouder := gid(tableSubthema, 1)

	eerst, err := g.children(context.Background(), ouder, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if eerst.Total != 1 {
		t.Fatalf("total = %d, wil 1", eerst.Total)
	}

	// Een kind erbij zonder import: binnen de vervaltijd blijft de bewaarde
	// telling staan, en dat is precies de staleness-grens die de cache belooft.
	id := f.node(gid(tableDestillaat, 7001), map[string]any{"samenvatting": "Nieuw standpunt."})
	f.relate(id, ouder, relBelongsToSubthema)
	tussen, err := g.children(context.Background(), ouder, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if tussen.Total != 1 {
		t.Errorf("total = %d binnen de vervaltijd, wil de bewaarde 1", tussen.Total)
	}

	g.invalidateChildCache()
	na, err := g.children(context.Background(), ouder, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if na.Total != 2 || len(na.Nodes) != 2 {
		t.Errorf("na de import: total = %d bij %d nodes, wil 2", na.Total, len(na.Nodes))
	}
}

// Zonder import verloopt de lijst vanzelf. De klok is een veld, zodat de test
// geen minuut hoeft te wachten.
func TestGraphChildCache_ExpiresOnItsOwn(t *testing.T) {
	f := newGraphFixture(t).build()
	g := f.service()
	nu := time.Now()
	g.childCache.now = func() time.Time { return nu }
	ouder := gid(tableSubthema, 1)

	if _, err := g.children(context.Background(), ouder, 0); err != nil {
		t.Fatalf("children: %v", err)
	}
	id := f.node(gid(tableDestillaat, 7002), map[string]any{"samenvatting": "Nog een standpunt."})
	f.relate(id, ouder, relBelongsToSubthema)

	nu = nu.Add(graphChildCacheTTL + time.Second)
	na, err := g.children(context.Background(), ouder, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if na.Total != 2 {
		t.Errorf("total = %d na de vervaltijd, wil 2", na.Total)
	}
}

// Verdwijnt een kind uit een bewaarde lijst, dan zou de pagina een rij minder
// tonen dan total belooft — precies de fout die de exacte telling wegnam. De graaf gooit
// de lijst dan weg en telt opnieuw.
func TestGraphChildCache_AVanishedChildForcesARecount(t *testing.T) {
	f := newGraphFixture(t).build()
	g := f.service()
	ouder := gid(tableSubthema, 1)
	id := f.node(gid(tableDestillaat, 7003), map[string]any{"samenvatting": "Verdwijnt zo."})
	f.relate(id, ouder, relBelongsToSubthema)

	eerst, err := g.children(context.Background(), ouder, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if eerst.Total != 2 {
		t.Fatalf("total = %d, wil 2", eerst.Total)
	}

	if err := f.k.Delete(context.Background(), data.NodeID(id)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	na, err := g.children(context.Background(), ouder, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if na.Total != len(na.Nodes) {
		t.Errorf("total = %d bij %d nodes: de belofte klopt niet meer", na.Total, len(na.Nodes))
	}
	if na.Total != 1 {
		t.Errorf("total = %d, wil 1 na het verdwenen kind", na.Total)
	}
}

// De cache is begrensd: meer ouders dan er regels zijn laat hem niet groeien.
func TestGraphChildCache_IsBounded(t *testing.T) {
	var c graphChildCache
	for i := 0; i < graphChildCacheEntries*3; i++ {
		c.put(graphChildCacheKey(testKBID, gid(tableSubthema, i)), []graphChildEdge{{childID: gid(tableDestillaat, i)}})
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > graphChildCacheEntries {
		t.Errorf("de cache draagt %d regels, hoogstens %d", n, graphChildCacheEntries)
	}
	// En de laatst gezette lijst staat er nog: de oudste gaat eruit, niet de
	// nieuwste.
	laatste := graphChildCacheKey(testKBID, gid(tableSubthema, graphChildCacheEntries*3-1))
	if _, ok := c.get(laatste); !ok {
		t.Error("de laatst bewaarde lijst is er al uit gegooid")
	}
}

// En de haak zit er werkelijk aan: de importer meldt elke run, geslaagd of
// niet, en initKnowledgeImport hangt de graaf eraan.
func TestKBImport_ReportsEveryRunSoTheGraphCanForget(t *testing.T) {
	imp, _ := newTestImporter(t)
	var gemeld int
	imp.onImportDone = func() { gemeld++ }

	importFixture(t, imp, newMosaicFixture())
	if gemeld != 1 {
		t.Errorf("een geslaagde import meldde zich %d keer, wil één keer", gemeld)
	}
	// Een mislukte run kan al geschreven hebben; ook die hoort zich te melden.
	imp.run(context.Background(), "/deze/bundel/bestaat/niet.zip")
	if gemeld != 2 {
		t.Errorf("een mislukte import meldde zich niet: %d", gemeld)
	}
}

func TestInitKnowledgeImport_HangsTheGraphOnTheImport(t *testing.T) {
	logger, _ := captureLogs()
	m, _ := newTestModule(t, func(m *Module) {
		m.logger = logger
		m.knowledge = &fakeKnowledge{}
	})
	m.graph = m.newGraphService()
	t.Setenv(envImportKB, "")

	m.initKnowledgeImport(nil)
	if m.kbImport == nil || m.kbImport.onImportDone == nil {
		t.Fatal("de importer meldt zijn runs niet aan de graaf: de kinderlijst blijft na een import staan")
	}
	// En wat hij meldt is werkelijk de cache van déze graaf.
	m.graph.childCache.put("k", []graphChildEdge{{childID: "x"}})
	m.kbImport.onImportDone()
	if _, ok := m.graph.childCache.get("k"); ok {
		t.Error("de melding leegde de kinderlijst niet")
	}
}
