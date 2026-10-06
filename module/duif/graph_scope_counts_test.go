package duif

import (
	"context"
	"testing"
)

// Total, truncated en nextOffset horen te beschrijven wat de lezer
// mag zien. Ze kwamen uit de ongefilterde edge-lijst, terwijl alleen
// fetchNodes op kennisbank filterde: een vreemd thema onder een eigen domein
// gaf total=3 bij twee nodes.
func TestGraphChildren_TotalCountsOnlyWhatTheReaderMaySee(t *testing.T) {
	g := newGraphFixture(t).build().foreign().foreignChild().service()

	out, err := g.children(context.Background(), gid(tableDomein, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if out.Total != 2 {
		t.Errorf("total = %d, wil 2 (het vreemde thema telt niet mee)", out.Total)
	}
	if out.Total != len(out.Nodes) {
		t.Errorf("total = %d bij %d nodes: de telling belooft meer dan de lezer krijgt", out.Total, len(out.Nodes))
	}
	for _, n := range out.Nodes {
		if n.ID == gid(tableThema, 901) {
			t.Error("het vreemde thema staat op de wire")
		}
	}
	for _, rel := range out.Relationships {
		if rel.TargetID == gid(tableThema, 901) {
			t.Error("de relatie naar het vreemde thema staat op de wire")
		}
	}
}

// Dezelfde vraag langs de kaart: de telling daar en de lijst achter de klik
// mogen niet uiteenlopen, ook niet met een vreemd kind in de graaf.
func TestGraphOverview_ChildCountSkipsAForeignChild(t *testing.T) {
	g := newGraphFixture(t).build().foreign().foreignChild().service()

	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if got := out.ChildCounts[gid(tableDomein, 1)]; got != 2 {
		t.Errorf("childCounts domein = %d, wil 2 (het vreemde thema telt niet mee)", got)
	}
	kids, err := g.children(context.Background(), gid(tableDomein, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if out.ChildCounts[gid(tableDomein, 1)] != kids.Total {
		t.Errorf("kaart telt %d, de lijst achter de klik %d",
			out.ChildCounts[gid(tableDomein, 1)], kids.Total)
	}
}

// De paginering telt mee vanaf de afgebakende lijst: een vreemd kind mag geen
// gat in de laatste pagina slaan.
func TestGraphChildren_ForeignChildDoesNotShiftTheLastPage(t *testing.T) {
	f := newGraphFixture(t).build()
	// 250 eigen thema's onder hetzelfde domein, plus één vreemd thema ertussen.
	for i := 0; i < 250; i++ {
		id := f.node(gid(tableThema, 4000+i), map[string]any{"titel": "Eigen thema"})
		f.relate(gid(tableDomein, 1), id, relContainsThema)
	}
	g := f.foreignChild().service()

	first, err := g.children(context.Background(), gid(tableDomein, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if first.Total != 252 {
		t.Fatalf("total = %d, wil 252 (250 + 2 eigen thema's)", first.Total)
	}
	if first.NextOffset == nil || *first.NextOffset != graphChildCap {
		t.Fatalf("nextOffset = %+v, wil %d", first.NextOffset, graphChildCap)
	}
	second, err := g.children(context.Background(), gid(tableDomein, 1), *first.NextOffset)
	if err != nil {
		t.Fatalf("children offset: %v", err)
	}
	if len(second.Nodes) != 252-graphChildCap {
		t.Errorf("laatste pagina = %d nodes, wil %d", len(second.Nodes), 252-graphChildCap)
	}
	if second.Truncated || second.NextOffset != nil {
		t.Errorf("laatste pagina is toch afgekapt: %+v", second)
	}
}

// walkChildren loopt alle pagina's van één ouder af en geeft terug hoe vaak
// elke node langskwam, plus de total die elke pagina beloofde.
func walkChildren(t *testing.T, g *graphService, parent string) (gezien map[string]int, total int) {
	t.Helper()
	gezien = map[string]int{}
	offset, total, pages := 0, -1, 0
	for {
		page, err := g.children(context.Background(), parent, offset)
		if err != nil {
			t.Fatalf("children(%d): %v", offset, err)
		}
		if total < 0 {
			total = page.Total
		} else if page.Total != total {
			t.Errorf("total verandert tussen pagina's: %d na %d", page.Total, total)
		}
		for _, n := range page.Nodes {
			gezien[n.ID]++
		}
		pages++
		if page.NextOffset == nil {
			if page.Truncated {
				t.Error("de laatste pagina meldt zich als afgekapt zonder vervolg")
			}
			return gezien, total
		}
		if *page.NextOffset <= offset {
			t.Fatalf("nextOffset %d loopt niet vooruit op %d", *page.NextOffset, offset)
		}
		offset = *page.NextOffset
		if pages > 20 {
			t.Fatal("de paginering eindigt niet")
		}
	}
}

// De paginering over alle pagina's: total staat vast, elke eigen rij
// komt precies één keer langs, er komt geen vreemde rij langs, en het aantal
// unieke rijen is gelijk aan total. Een belofte die de laatste pagina niet
// waarmaakt is hier een fout, geen afronding.
func TestGraphChildren_PagingIsExactAcrossAllPages(t *testing.T) {
	f := newGraphFixture(t).build()
	eigen := map[string]bool{}
	for i := 0; i < 250; i++ {
		id := f.node(gid(tableThema, 4000+i), map[string]any{"titel": "Eigen thema"})
		f.relate(gid(tableDomein, 1), id, relContainsThema)
		eigen[id] = true
	}
	// De twee thema's uit de basisfixture horen er ook bij.
	eigen[gid(tableThema, 1)] = true
	eigen[gid(tableThema, 2)] = true
	g := f.foreignChild().service()

	gezien, total := walkChildren(t, g, gid(tableDomein, 1))
	if total != len(eigen) {
		t.Errorf("total = %d, wil %d", total, len(eigen))
	}
	if len(gezien) != total {
		t.Errorf("%d unieke nodes over alle pagina's, total = %d", len(gezien), total)
	}
	for id, n := range gezien {
		if n != 1 {
			t.Errorf("node %s kwam %d keer langs", id, n)
		}
		if !eigen[id] {
			t.Errorf("vreemde node %s staat op een pagina", id)
		}
	}
	for id := range eigen {
		if gezien[id] == 0 {
			t.Errorf("eigen thema %s kwam op geen enkele pagina langs", id)
		}
	}
}

// Dezelfde belofte op de laag onder de structuur. Standpunten en
// citaten gingen ongetoetst mee: een subthema met graphChildCap eigen en vijf
// vreemde standpunten beloofde een tweede pagina die leeg bleek.
func TestGraphChildren_ForeignDestillatenNeverPromiseAnEmptyPage(t *testing.T) {
	f := newGraphFixture(t).build()
	kb := f.kbID
	eigen := map[string]bool{}
	for i := 0; i < graphChildCap; i++ {
		f.kbID = kb
		id := f.node(gid(tableDestillaat, 5000+i), map[string]any{"samenvatting": "Eigen standpunt."})
		f.relate(id, gid(tableSubthema, 2), relBelongsToSubthema)
		eigen[id] = true
	}
	// Het standpunt uit de basisfixture hangt ook onder subthema 2.
	eigen[gid(tableDestillaat, 2)] = true
	for i := 0; i < 5; i++ {
		f.kbID = graphForeignKBID
		id := f.node(gid(tableDestillaat, 6000+i), map[string]any{"samenvatting": "Vreemd standpunt."})
		f.kbID = kb
		f.relate(id, gid(tableSubthema, 2), relBelongsToSubthema)
	}
	f.kbID = kb
	g := f.service()

	gezien, total := walkChildren(t, g, gid(tableSubthema, 2))
	if total != len(eigen) {
		t.Errorf("total = %d, wil %d (de vreemde standpunten tellen niet mee)", total, len(eigen))
	}
	if len(gezien) != total {
		t.Errorf("%d unieke nodes over alle pagina's, total = %d", len(gezien), total)
	}
	for id, n := range gezien {
		if n != 1 {
			t.Errorf("node %s kwam %d keer langs", id, n)
		}
		if !eigen[id] {
			t.Errorf("vreemd standpunt %s staat op een pagina", id)
		}
	}
}

// Eén vreemd standpunt onder een eigen subthema telt nergens meer
// mee, ook niet in total.
func TestGraphChildren_TotalExcludesAForeignDestillaat(t *testing.T) {
	f := newGraphFixture(t).build()
	kb := f.kbID
	f.kbID = graphForeignKBID
	f.node(gid(tableDestillaat, 900), map[string]any{"samenvatting": "Vreemd standpunt."})
	f.kbID = kb
	f.relate(gid(tableDestillaat, 900), gid(tableSubthema, 1), relBelongsToSubthema)
	g := f.service()

	out, err := g.children(context.Background(), gid(tableSubthema, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if out.Total != len(out.Nodes) {
		t.Errorf("total = %d bij %d nodes: de telling belooft meer dan de lezer krijgt", out.Total, len(out.Nodes))
	}
	for _, n := range out.Nodes {
		if n.ID == gid(tableDestillaat, 900) {
			t.Error("het vreemde standpunt staat op de wire")
		}
	}
	for _, rel := range out.Relationships {
		if rel.SourceID == gid(tableDestillaat, 900) {
			t.Error("de relatie naar het vreemde standpunt staat op de wire")
		}
	}
}
