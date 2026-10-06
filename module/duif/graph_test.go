package duif

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

// Tests van de graaflogica. Ze draaien op de gedeelde
// fakeKnowledge, zonder HTTP ertussen; het wire-contract zelf staat in
// graph_handlers_test.go.

func TestGraphOverview_StructureAndRelationships(t *testing.T) {
	g := newGraphFixture(t).build().foreign().service()

	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.KB.State != string(ImportReady) {
		t.Fatalf("kb-state = %q, wil %q", out.KB.State, string(ImportReady))
	}
	want := []string{
		gid(tableDomein, 1),
		gid(tableSubthema, 1), gid(tableSubthema, 2),
		gid(tableThema, 1), gid(tableThema, 2),
	}
	if got := nodeIDs(out.Nodes); !reflect.DeepEqual(got, want) {
		t.Fatalf("nodes = %v, wil %v (gesorteerd op id, zonder standpunten/citaten)", got, want)
	}
	// Standpunten, citaten en bronpapers horen niet in het structuurniveau, en
	// perspectieven en persona-knopen horen nergens in de graaf — ook al
	// staan ze in de kennisbank.
	for _, n := range out.Nodes {
		switch n.Type {
		case tableDestillaat, tableQuote, tableInterview, tablePerspective, tablePersona:
			t.Fatalf("overview bevat %s %s", n.Type, n.ID)
		}
	}
	wantRels := 4 // 2 contains_thema, 2 contains_subthema
	if len(out.Relationships) != wantRels {
		t.Fatalf("relationships = %d, wil %d: %+v", len(out.Relationships), wantRels, out.Relationships)
	}
	for _, rel := range out.Relationships {
		switch rel.RelationshipType {
		case relContainsThema, relContainsSubthema:
		default:
			t.Fatalf("onverwachte relatie %q op de wire", rel.RelationshipType)
		}
	}
	// totals komen uit de marker en bevatten alleen tabellen die de graaf toont.
	if out.Totals[tableDestillaat] != 2 || out.Totals[tableQuote] != 2 || out.Totals[tableThema] != 2 {
		t.Fatalf("totals = %v", out.Totals)
	}
	for _, key := range []string{relContainsThema, tablePerspective, tablePersona} {
		if _, ok := out.Totals[key]; ok {
			t.Fatalf("totals draagt %q: %v", key, out.Totals)
		}
	}
}

func TestGraphOverview_Labels(t *testing.T) {
	g := newGraphFixture(t).build().service()
	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	labels := map[string]string{}
	for _, n := range out.Nodes {
		labels[n.ID] = graphLabelText(n.Label, kbLangSource)
	}
	for id, want := range map[string]string{
		gid(tableDomein, 1):   "Stad en dier",
		gid(tableThema, 1):    "Overlast",
		gid(tableSubthema, 1): "Voeren",
	} {
		if labels[id] != want {
			t.Errorf("label %s = %q, wil %q", id, labels[id], want)
		}
	}
}

func TestGraphOverview_EmptyKnowledgeBase(t *testing.T) {
	// Geen marker: de graaf antwoordt leeg, met de reden erbij.
	f := newGraphFixture(t)
	f.node(gid(tableThema, 1), map[string]any{"titel": "Wees"}) // node zonder marker
	out, err := f.service().overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.KB.State != string(ImportEmpty) {
		t.Fatalf("kb-state = %q, wil %q", out.KB.State, string(ImportEmpty))
	}
	if len(out.Nodes) != 0 || len(out.Relationships) != 0 || len(out.Totals) != 0 {
		t.Fatalf("lege kennisbank levert toch data: %+v", out)
	}
}

func TestGraphOverview_AMarkerWithoutAKBID(t *testing.T) {
	// Een stukke marker mag de graaf niet openzetten voor alle kennisbanken.
	f := newGraphFixture(t).build()
	if err := f.k.Delete(context.Background(), data.NodeID(gid(tableImportMarker, 1))); err != nil {
		t.Fatal(err)
	}
	// Rechtstreeks geschreven: f.node zou er een kb_id in stoppen, en juist
	// het ontbreken daarvan is hier het geval.
	if _, err := f.k.CreateNodeWithID(context.Background(), data.NodeID(gid(tableImportMarker, 2)), map[string]any{"content_hash": "x"}, ""); err != nil {
		t.Fatal(err)
	}
	out, err := f.service().overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.KB.State != string(ImportEmpty) || len(out.Nodes) != 0 {
		t.Fatalf("marker zonder kb_id opent de graaf: %+v", out)
	}
}

func TestGraphOverview_PagesBeyondTwoHundred(t *testing.T) {
	// ListNodes levert maximaal 200 rijen per aanroep; de overview moet
	// doorbladeren tot een korte pagina.
	f := newGraphFixture(t).build()
	for i := 0; i < 250; i++ {
		f.node(gid(tableSubthema, 2000+i), map[string]any{"titel": fmt.Sprintf("Extra %03d", i)})
	}
	out, err := f.service().overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	var subthemas int
	for _, n := range out.Nodes {
		if n.Type == tableSubthema {
			subthemas++
		}
	}
	if subthemas != 252 {
		t.Fatalf("subthema's = %d, wil 252 (2 uit de mini-graaf + 250)", subthemas)
	}
}

func TestGraphChildren_RulesPerTable(t *testing.T) {
	g := newGraphFixture(t).build().service()

	cases := []struct {
		naam   string
		parent string
		want   []string
	}{
		{"domein levert thema's", gid(tableDomein, 1), []string{gid(tableThema, 1), gid(tableThema, 2)}},
		{"thema levert alleen zijn subthema", gid(tableThema, 1), []string{gid(tableSubthema, 1)}},
		{"subthema levert standpunten", gid(tableSubthema, 1), []string{gid(tableDestillaat, 1)}},
		{"bronpaper levert standpunten", gid(tableInterview, 1), []string{gid(tableDestillaat, 1), gid(tableDestillaat, 2)}},
		{"standpunt levert citaten en het bronpaper", gid(tableDestillaat, 1), []string{gid(tableInterview, 1), gid(tableQuote, 1), gid(tableQuote, 2)}},
		{"citaat is een blad", gid(tableQuote, 1), []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.naam, func(t *testing.T) {
			out, err := g.children(context.Background(), tc.parent, 0)
			if err != nil {
				t.Fatalf("children: %v", err)
			}
			if got := nodeIDs(out.Nodes); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("kinderen = %v, wil %v", got, tc.want)
			}
			if out.Total != len(tc.want) {
				t.Fatalf("total = %d, wil %d", out.Total, len(tc.want))
			}
			if out.Truncated || out.NextOffset != nil {
				t.Fatalf("onterecht afgekapt: %+v", out)
			}
			if len(out.Relationships) != len(tc.want) {
				t.Fatalf("relationships = %d, wil %d", len(out.Relationships), len(tc.want))
			}
		})
	}
}

func TestGraphChildren_RelationDirectionFollowsTheData(t *testing.T) {
	// Ouder → kind bij contains_thema, kind → ouder bij het bronpaper onder
	// een standpunt: de wire draagt de edge zoals hij in de graaf staat.
	g := newGraphFixture(t).build().service()

	out, err := g.children(context.Background(), gid(tableDomein, 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Relationships[0].SourceID != gid(tableDomein, 1) {
		t.Fatalf("contains_thema wijst de verkeerde kant op: %+v", out.Relationships[0])
	}

	out, err = g.children(context.Background(), gid(tableDestillaat, 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, rel := range out.Relationships {
		if rel.RelationshipType != data.RelContains {
			continue
		}
		found = true
		if rel.SourceID != gid(tableInterview, 1) || rel.TargetID != gid(tableDestillaat, 1) {
			t.Fatalf("contains hoort van bronpaper naar standpunt te wijzen: %+v", rel)
		}
	}
	if !found {
		t.Fatal("geen contains-relatie in het antwoord van het standpunt")
	}
}

func TestGraphChildren_PaginationAboveTheCap(t *testing.T) {
	sub := gid(tableSubthema, 1)
	g := newGraphFixture(t).build().destillaten(sub, 250).service()

	first, err := g.children(context.Background(), sub, 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if first.Total != 251 {
		t.Fatalf("total = %d, wil 251", first.Total)
	}
	if len(first.Nodes) != graphChildCap {
		t.Fatalf("eerste pagina = %d, wil %d", len(first.Nodes), graphChildCap)
	}
	if !first.Truncated || first.NextOffset == nil || *first.NextOffset != graphChildCap {
		t.Fatalf("truncatie ontbreekt of klopt niet: %+v", first)
	}

	second, err := g.children(context.Background(), sub, *first.NextOffset)
	if err != nil {
		t.Fatalf("children offset: %v", err)
	}
	if len(second.Nodes) != 101 {
		t.Fatalf("tweede pagina = %d, wil 101", len(second.Nodes))
	}
	if second.Truncated || second.NextOffset != nil {
		t.Fatalf("laatste pagina is toch afgekapt: %+v", second)
	}
	// Geen overlap tussen de pagina's: de volgorde is stabiel over verzoeken.
	seen := map[string]bool{}
	for _, id := range append(nodeIDs(first.Nodes), nodeIDs(second.Nodes)...) {
		if seen[id] {
			t.Fatalf("id %s staat in beide pagina's", id)
		}
		seen[id] = true
	}
	if len(seen) != 251 {
		t.Fatalf("samen %d unieke kinderen, wil 251", len(seen))
	}
}

func TestGraphChildren_OffsetPastTheEnd(t *testing.T) {
	g := newGraphFixture(t).build().service()
	out, err := g.children(context.Background(), gid(tableDomein, 1), 99)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(out.Nodes) != 0 || out.Truncated || out.NextOffset != nil {
		t.Fatalf("offset voorbij het einde levert data: %+v", out)
	}
	if out.Total != 2 {
		t.Fatalf("total = %d, wil 2", out.Total)
	}
}

func TestGraphChildren_AVanishedChildDropsOut(t *testing.T) {
	// Een edge naar een node die er niet meer is, is een datafout: het kind
	// valt weg, het antwoord blijft een 200.
	f := newGraphFixture(t).build()
	if err := f.k.Delete(context.Background(), data.NodeID(gid(tableQuote, 2))); err != nil {
		t.Fatal(err)
	}
	// Delete haalt ook de edge weg; leg hem opnieuw zodat de wees echt bestaat.
	f.relate(gid(tableDestillaat, 1), gid(tableQuote, 2), data.RelHasQuote)

	out, err := f.service().children(context.Background(), gid(tableDestillaat, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	for _, n := range out.Nodes {
		if n.ID == gid(tableQuote, 2) {
			t.Fatal("verdwenen citaat staat toch op de wire")
		}
	}
	for _, rel := range out.Relationships {
		if rel.TargetID == gid(tableQuote, 2) {
			t.Fatal("relatie naar een verdwenen citaat staat op de wire")
		}
	}
}

func TestGraphChildren_AForeignKnowledgeBase(t *testing.T) {
	g := newGraphFixture(t).build().foreign().service()
	_, err := g.children(context.Background(), gid(tableDomein, 900), 0)
	assertGraphFault(t, err, 404, ReasonNodeNotFound)
}

func TestGraphNode_TrailFromQuoteToRoot(t *testing.T) {
	g := newGraphFixture(t).build().service()
	out, err := g.node(context.Background(), gid(tableQuote, 1))
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if out.Node.Type != tableQuote {
		t.Fatalf("type = %q", out.Node.Type)
	}
	want := []string{gid(tableDomein, 1), gid(tableThema, 1), gid(tableSubthema, 1), gid(tableDestillaat, 1)}
	got := make([]string, len(out.Trail))
	for i, ref := range out.Trail {
		got[i] = ref.ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trail = %v, wil %v (van wortel naar ouder)", got, want)
	}
}

func TestGraphNode_TrailPerTable(t *testing.T) {
	g := newGraphFixture(t).build().service()
	cases := []struct {
		id    string
		trail []string
	}{
		{id: gid(tableDomein, 1), trail: []string{}},
		{id: gid(tableThema, 1), trail: []string{gid(tableDomein, 1)}},
		{id: gid(tableSubthema, 1), trail: []string{gid(tableDomein, 1), gid(tableThema, 1)}},
		{id: gid(tableInterview, 1), trail: []string{}},
		{id: gid(tableDestillaat, 1), trail: []string{gid(tableDomein, 1), gid(tableThema, 1), gid(tableSubthema, 1)}},
	}
	for _, tc := range cases {
		t.Run(tableOf(tc.id), func(t *testing.T) {
			out, err := g.node(context.Background(), tc.id)
			if err != nil {
				t.Fatalf("node: %v", err)
			}
			got := make([]string, len(out.Trail))
			for i, ref := range out.Trail {
				got[i] = ref.ID
			}
			if !reflect.DeepEqual(got, tc.trail) {
				t.Fatalf("trail = %v, wil %v", got, tc.trail)
			}
		})
	}
}

// Perspectieven en persona-knopen staan in de kennisbank maar horen niet in de
// graaf. Hun id is op elke route wat een onbekende tabel is: een 404
// vóór er een query loopt, zodat er ook geen omweg naar hun inhoud is.
func TestGraph_PerspectivesAndPersonasAreNotInTheGraph(t *testing.T) {
	g := newGraphFixture(t).build().service()
	for _, id := range []string{gid(tablePerspective, 1), gid(tablePersona, 1)} {
		t.Run(tableOf(id), func(t *testing.T) {
			_, err := g.node(context.Background(), id)
			assertGraphFault(t, err, 404, ReasonNodeNotFound)
			_, err = g.children(context.Background(), id, 0)
			assertGraphFault(t, err, 404, ReasonNodeNotFound)
		})
	}
}

func TestGraphNode_AMissingParentShortensTheTrail(t *testing.T) {
	f := newGraphFixture(t).build()
	if err := f.k.Delete(context.Background(), data.NodeID(gid(tableThema, 1))); err != nil {
		t.Fatal(err)
	}
	// De edge subthema→thema weer terugleggen: de wees is juist het geval.
	f.relate(gid(tableThema, 1), gid(tableSubthema, 1), relContainsSubthema)

	out, err := f.service().node(context.Background(), gid(tableSubthema, 1))
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if len(out.Trail) != 0 {
		t.Fatalf("trail = %+v, wil leeg (de ouder ontbreekt)", out.Trail)
	}
}

func TestGraphNode_ALoopInTheDataStopsTheTrail(t *testing.T) {
	// Een cyclische contains_subthema/contains_thema-keten mag de klim niet
	// laten rondtollen; graphMaxTrailHops en de bezochtset zijn samen de rem.
	f := newGraphFixture(t).build()
	f.relate(gid(tableSubthema, 1), gid(tableDomein, 1), relContainsThema)

	out, err := f.service().node(context.Background(), gid(tableSubthema, 1))
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if len(out.Trail) > graphMaxTrailHops {
		t.Fatalf("trail = %d hops, wil hooguit %d", len(out.Trail), graphMaxTrailHops)
	}
	ids := map[string]bool{}
	for _, ref := range out.Trail {
		if ids[ref.ID] {
			t.Fatalf("trail bevat %s dubbel: %+v", ref.ID, out.Trail)
		}
		ids[ref.ID] = true
	}
}

func TestGraphNode_AForeignKBAndAnUnknownTableGiveTheSameError(t *testing.T) {
	g := newGraphFixture(t).build().foreign().service()
	for _, id := range []string{
		gid(tableDomein, 900),                        // bestaat, maar hoort bij een andere kennisbank
		gid("run", 1),                                // tabel buiten de allowlist
		gid(tableImportMarker, 1),                    // de marker zelf is geen graafnode
		gid(tableThema, 99),                          // bestaat niet
		"thema:niet-eens-een-uuid",                   // vormfout
		"THEMA:00000001-0000-4000-8000-000000000001", // hoofdletters
		"thema00000001-0000-4000-8000-000000000001",  // geen scheidingsteken
	} {
		t.Run(id, func(t *testing.T) {
			_, err := g.node(context.Background(), id)
			assertGraphFault(t, err, 404, ReasonNodeNotFound)
			_, err = g.children(context.Background(), id, 0)
			assertGraphFault(t, err, 404, ReasonNodeNotFound)
		})
	}
}

func TestGraph_AReadFailureIsA503(t *testing.T) {
	boem := errors.New("test: verbinding weg (met een fouttekst die nooit de wire mag halen)")

	t.Run("marker", func(t *testing.T) {
		f := newGraphFixture(t).build()
		k := &failingKnowledge{fakeKnowledge: f.k, listErr: boem, listTable: tableImportMarker}
		g := &graphService{nodes: k, edges: k}
		_, err := g.overview(context.Background())
		assertGraphFault(t, err, 503, ReasonGraphUnavailable)
	})
	t.Run("table", func(t *testing.T) {
		f := newGraphFixture(t).build()
		k := &failingKnowledge{fakeKnowledge: f.k, listErr: boem, listTable: tableThema}
		g := &graphService{nodes: k, edges: k}
		_, err := g.overview(context.Background())
		assertGraphFault(t, err, 503, ReasonGraphUnavailable)
	})
	t.Run("relations", func(t *testing.T) {
		f := newGraphFixture(t).build()
		k := &failingKnowledge{fakeKnowledge: f.k, edgesErr: boem}
		g := &graphService{nodes: k, edges: k}
		if _, err := g.overview(context.Background()); !isGraphFault(err, 503, ReasonGraphUnavailable) {
			t.Fatalf("overview: %v", err)
		}
		if _, err := g.children(context.Background(), gid(tableDomein, 1), 0); !isGraphFault(err, 503, ReasonGraphUnavailable) {
			t.Fatalf("children: %v", err)
		}
		if _, err := g.node(context.Background(), gid(tableSubthema, 1)); !isGraphFault(err, 503, ReasonGraphUnavailable) {
			t.Fatalf("node: %v", err)
		}
	})
	t.Run("node", func(t *testing.T) {
		f := newGraphFixture(t).build()
		k := &failingKnowledge{fakeKnowledge: f.k, getNodeErr: boem}
		g := &graphService{nodes: k, edges: k}
		_, err := g.node(context.Background(), gid(tableThema, 1))
		assertGraphFault(t, err, 503, ReasonGraphUnavailable)
		_, err = g.children(context.Background(), gid(tableThema, 1), 0)
		assertGraphFault(t, err, 503, ReasonGraphUnavailable)
	})
	t.Run("child", func(t *testing.T) {
		// GetNode faalt pas op het kind: de fan-out moet de 503 doorgeven,
		// niet stilletjes een halve lijst opleveren.
		f := newGraphFixture(t).build()
		k := &failingKnowledge{fakeKnowledge: f.k, getNodeErr: boem, getNodeFor: data.NodeID(gid(tableThema, 2))}
		g := &graphService{nodes: k, edges: k}
		_, err := g.children(context.Background(), gid(tableDomein, 1), 0)
		assertGraphFault(t, err, 503, ReasonGraphUnavailable)
	})
}

func TestGraphMetadata_Allowlist(t *testing.T) {
	g := newGraphFixture(t).build().service()

	// Het bronpaper draagt storage_key en content_hash, het standpunt een
	// embedding, elke node een import_hash. Niets daarvan hoort in metadata.
	out, err := g.children(context.Background(), gid(tableDestillaat, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	byID := map[string]graphNode{}
	for _, n := range out.Nodes {
		byID[n.ID] = n
	}
	iv := byID[gid(tableInterview, 1)]
	if iv.Label != "lewald1987" {
		t.Fatalf("bronpaperlabel = %q", iv.Label)
	}
	if got, want := iv.Metadata, map[string]any{"interview_id": "iv-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bronpaper-metadata = %v, wil %v", got, want)
	}
	q := byID[gid(tableQuote, 1)]
	if got, want := q.Metadata, map[string]any{"text": longSummary, "speaker": "R."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("citaat-metadata = %v, wil %v", got, want)
	}

	// Het standpunt: label afgekapt, volledige tekst in metadata.text.
	node, err := g.node(context.Background(), gid(tableDestillaat, 1))
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if graphLabelText(node.Node.Metadata["text"], kbLangSource) != longSummary {
		t.Fatalf("metadata.text draagt niet de volledige samenvatting")
	}
	// persona hoort bij de redactionele laag die de graaf niet toont.
	for _, key := range []string{"abstraction_level", "primary_themes", "embedding", "import_hash", "kb_id", "persona"} {
		if _, ok := node.Node.Metadata[key]; ok {
			t.Fatalf("prop %q lekt in metadata: %v", key, node.Node.Metadata)
		}
	}
	if graphLabelText(node.Node.Metadata["thema_titel"], kbLangSource) != "Overlast" || node.Node.Metadata["scope"] != "stad" {
		t.Fatalf("standpunt-metadata mist toegestane velden: %v", node.Node.Metadata)
	}
}

func TestGraphLabel_Truncates(t *testing.T) {
	g := newGraphFixture(t).build().service()
	out, err := g.node(context.Background(), gid(tableDestillaat, 1))
	if err != nil {
		t.Fatal(err)
	}
	label := graphLabelText(out.Node.Label, kbLangSource)
	if !strings.HasSuffix(label, "…") {
		t.Fatalf("label %q eindigt niet op een beletselteken", label)
	}
	if n := len([]rune(label)); n > graphLabelMaxRunes+1 {
		t.Fatalf("label is %d tekens, wil hooguit %d", n, graphLabelMaxRunes+1)
	}
	if !strings.HasPrefix(longSummary, strings.TrimSuffix(label, "…")) {
		t.Fatalf("label %q is geen prefix van de samenvatting", label)
	}
	if strings.HasSuffix(strings.TrimSuffix(label, "…"), " ") {
		t.Fatalf("label %q eindigt op een spatie", label)
	}
}

func TestTruncateLabel_CutsOnAWordBoundary(t *testing.T) {
	cases := []struct{ in, want string }{
		{"kort", "kort"},
		{"regel een\nregel twee", "regel een regel twee"},
		{strings.Repeat("a", 200), strings.Repeat("a", 140) + "…"},
		{strings.Repeat("ab ", 80), strings.TrimSpace(strings.Repeat("ab ", 46)) + "…"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := truncateLabel(tc.in, graphLabelMaxRunes); got != tc.want {
			t.Errorf("truncateLabel(%.20q) = %q, wil %q", tc.in, got, tc.want)
		}
	}
	// Meerbyte-tekens tellen als één teken, niet als drie bytes.
	long := strings.Repeat("é", 200)
	if n := len([]rune(truncateLabel(long, graphLabelMaxRunes))); n != graphLabelMaxRunes+1 {
		t.Errorf("meerbyte-label = %d tekens, wil %d", n, graphLabelMaxRunes+1)
	}
}

func TestCountsFromProps_ReadsJSONAndGoNumbers(t *testing.T) {
	got := countsFromProps(map[string]any{
		"thema": float64(26), "quote": 4225, "domein": int64(3), "los": "geen getal",
	})
	want := map[string]int{"thema": 26, "quote": 4225, "domein": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %v, wil %v", got, want)
	}
	if countsFromProps("geen map") != nil {
		t.Fatal("counts uit een niet-map hoort nil te zijn")
	}
}

// assertGraphFault toetst status en reden-code van een graaffout.
func assertGraphFault(t *testing.T, err error, status int, reason string) {
	t.Helper()
	if !isGraphFault(err, status, reason) {
		t.Fatalf("fout = %v, wil %d/%s", err, status, reason)
	}
}

func isGraphFault(err error, status int, reason string) bool {
	var f *graphFault
	return errors.As(err, &f) && f.status == status && f.reason == reason
}

// --- randgevallen -----------------------------------------------------------

func TestGraphFault_CarriesOnlyTheReasonCode(t *testing.T) {
	// De fouttekst van een graaffout is de reden-code zelf: er is geen
	// onderliggende tekst die per ongeluk kan meeliften naar een log of body.
	if errGraphNotFound.Error() != ReasonNodeNotFound {
		t.Fatalf("Error() = %q", errGraphNotFound.Error())
	}
	if errGraphUnavailable.Error() != ReasonGraphUnavailable {
		t.Fatalf("Error() = %q", errGraphUnavailable.Error())
	}
}

func TestErrorBody_AnUnknownCodeFallsBack(t *testing.T) {
	body := errorBody("verzonnen")
	if body.Error != "verzonnen" {
		t.Fatalf("error = %q", body.Error)
	}
	if body.Message["en"] == "" || body.Message["nl"] == "" {
		t.Fatalf("melding leeg: %+v", body)
	}
}

func TestGraphLabel_ATableWithoutALabelProp(t *testing.T) {
	// De marker heeft geen label-prop; hij hoort ook nooit op de wire, maar de
	// projectie moet er niet op stukgaan.
	if got := graphLabel(tableImportMarker, map[string]any{"kb_id": "x"}); got != "" {
		t.Fatalf("label = %v, wil leeg", got)
	}
	// Een thematitel is vertaalbaar en komt dus als taalobject terug; zonder
	// vertaling staat alleen de brontaal erin.
	if got := graphLabel(tableThema, map[string]any{"titel": "  Overlast  "}); !reflect.DeepEqual(got, Bilingual{kbLangSource: "Overlast"}) {
		t.Fatalf("label = %v", got)
	}
	if got := graphLabel(tableThema, map[string]any{"titel": "Overlast", kbLocalizedProp("titel"): "Nuisance", "source_language": kbLangSource}); !reflect.DeepEqual(got, Bilingual{kbLangSource: "Overlast", kbLangEnglish: "Nuisance"}) {
		t.Fatalf("tweetalig label = %v", got)
	}
	if got := graphLabel(tableThema, map[string]any{"titel": 42}); got != "" {
		t.Fatalf("niet-tekstuele titel = %v, wil leeg", got)
	}
}

func TestGraphNodeOf_EmptyAndMissingPropsAreDropped(t *testing.T) {
	n := graphNodeOf(gid(tableThema, 1), map[string]any{"titel": "Overlast", "beschrijving": "   ", "embedding": "[0.1]"})
	if n.Metadata != nil {
		t.Fatalf("metadata = %v, wil nil (lege beschrijving telt niet)", n.Metadata)
	}
	n = graphNodeOf(gid(tableSubthema, 1), map[string]any{"titel": "S", "kernconcepten": nil, "beschrijving": "Wel iets."})
	if got, want := n.Metadata, map[string]any{"beschrijving": Bilingual{kbLangSource: "Wel iets."}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %v, wil %v", got, want)
	}
}

func TestSortRelationships_FullOrdering(t *testing.T) {
	rels := []graphRelationship{
		{SourceID: "b", TargetID: "a", RelationshipType: "z"},
		{SourceID: "a", TargetID: "b", RelationshipType: "b"},
		{SourceID: "a", TargetID: "b", RelationshipType: "a"},
		{SourceID: "a", TargetID: "a", RelationshipType: "z"},
	}
	sortRelationships(rels)
	want := []graphRelationship{
		{SourceID: "a", TargetID: "a", RelationshipType: "z"},
		{SourceID: "a", TargetID: "b", RelationshipType: "a"},
		{SourceID: "a", TargetID: "b", RelationshipType: "b"},
		{SourceID: "b", TargetID: "a", RelationshipType: "z"},
	}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("gesorteerd = %+v", rels)
	}
}

func TestGraphNode_AParentReadFailureIsA503(t *testing.T) {
	f := newGraphFixture(t).build()
	k := &failingKnowledge{fakeKnowledge: f.k, getNodeErr: errors.New("weg"), getNodeFor: data.NodeID(gid(tableThema, 1))}
	g := &graphService{nodes: k, edges: k}
	_, err := g.node(context.Background(), gid(tableSubthema, 1))
	assertGraphFault(t, err, 503, ReasonGraphUnavailable)
}

func TestGraphOverview_ATableWithoutNodesSkipsItsRelations(t *testing.T) {
	// Zonder thema's is er niets om contains_subthema vanaf te lezen. Een lege
	// bronlijst is voor ListEdges "geen filter", dus de overview hoort die
	// relatie dan over te slaan in plaats van de hele graaf te lezen.
	f := newGraphFixture(t).build()
	for _, id := range []string{gid(tableThema, 1), gid(tableThema, 2)} {
		if err := f.k.Delete(context.Background(), data.NodeID(id)); err != nil {
			t.Fatal(err)
		}
	}
	c := &edgeCounter{fakeKnowledge: f.k}
	g := &graphService{nodes: c, edges: c}
	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if c.unbounded != 0 {
		t.Fatalf("%d ongefilterde ListEdges bij een lege tabel", c.unbounded)
	}
	if len(out.Nodes) != 3 {
		t.Fatalf("nodes = %d, wil 3 (het domein en twee subthema's)", len(out.Nodes))
	}
}

func TestCountsFromProps_Float32(t *testing.T) {
	if got := countsFromProps(map[string]any{"thema": float32(26)}); got["thema"] != 26 {
		t.Fatalf("counts = %v", got)
	}
}

// De overview draagt de volledige importstand, niet alleen "leeg of klaar":
// een graaf die leeg oogt omdat de import nog loopt is iets anders dan een lege
// kennisbank, en de bezoeker hoort dat verschil te zien.
func TestGraphOverview_ReportsTheFullImportState(t *testing.T) {
	states := []ImportState{ImportImporting, ImportIncomplete, ImportFailed}
	for _, want := range states {
		t.Run(string(want), func(t *testing.T) {
			g := newGraphFixture(t).service()
			g.status = func() importStatus { return importStatus{State: want} }
			out, err := g.overview(context.Background())
			if err != nil {
				t.Fatalf("overview: %v", err)
			}
			if out.KB.State != string(want) {
				t.Fatalf("kb.state = %q, wil %q", out.KB.State, want)
			}
		})
	}
}

// Zonder gewirede importer kan de graaf alleen leeg zijn; een lege stand is
// evengoed "empty".
func TestGraphImportState_FallsBackToEmpty(t *testing.T) {
	g := newGraphFixture(t).service()
	if got := g.importState(); got != ImportEmpty {
		t.Errorf("zonder statusbron = %q, wil empty", got)
	}
	g.status = func() importStatus { return importStatus{} }
	if got := g.importState(); got != ImportEmpty {
		t.Errorf("lege stand = %q, wil empty", got)
	}
}

// Een stand zonder kb_id maakt van de afbakening een wildcard: `got != kbID`
// klopt dan voor elke rij zonder kb_id-prop, en de graaf toont alle
// kennisbanken tegelijk. De module schrijft zo'n marker zelf niet
// (kbIDPattern), maar een vreemde of beschadigde marker levert hem wel op — en
// dan hoort de graaf dicht te vallen in plaats van open.
func TestGraphScope_WithoutAKBIDTheGraphShowsNothing(t *testing.T) {
	cases := map[string]func(f *graphFixture){
		"marker zonder kb_id": func(f *graphFixture) {
			f.k.nodes[data.NodeID(gid(tableImportMarker, 1))]["kb_id"] = ""
		},
		"geen marker, module meldt klaar": func(f *graphFixture) {
			delete(f.k.nodes, data.NodeID(gid(tableImportMarker, 1)))
		},
	}
	for naam, breek := range cases {
		t.Run(naam, func(t *testing.T) {
			f := newGraphFixture(t).build().foreign()
			breek(f)
			g := f.service()
			// De module meldt "klaar" zonder bruikbare kb_id; het afleiden van
			// de stand mag die stand niet terugzetten.
			g.status = func() importStatus { return importStatus{State: ImportReady} }

			kb, err := g.readKB(context.Background())
			if err != nil {
				t.Fatalf("readKB: %v", err)
			}
			if kb.State == ImportReady {
				t.Errorf("stand = %q; zonder bruikbare kb_id hoort de graaf niet klaar te staan", kb.State)
			}

			out, err := g.overview(context.Background())
			if err != nil {
				t.Fatalf("overview: %v", err)
			}
			if len(out.Nodes) != 0 || len(out.Relationships) != 0 {
				t.Errorf("de overview toont %d knopen en %d relaties zonder actieve kennisbank", len(out.Nodes), len(out.Relationships))
			}
			if _, err := g.node(context.Background(), gid(tableThema, 1)); !errors.Is(err, errGraphNotFound) {
				t.Errorf("node = %v, wil %v", err, errGraphNotFound)
			}
			if _, err := g.children(context.Background(), gid(tableDomein, 1), 0); !errors.Is(err, errGraphNotFound) {
				t.Errorf("children = %v, wil %v", err, errGraphNotFound)
			}
		})
	}
}

// De afbakening faalt dicht op de plek waar hij toegepast wordt, en niet
// alleen bij de aanroeper: geen kennisbank betekent niets lezen. De rij zonder
// kb_id-prop is daarbij het geval dat telt — die matchte in `got != kbID` op
// een lege kbID en kwam er dus uit.
func TestGraphScope_ReadsRefuseWithoutAKnowledgeBank(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t).build().foreign()
	zonderKB := gid(tableThema, 901)
	if _, err := f.k.CreateNodeWithID(ctx, data.NodeID(zonderKB), map[string]any{"titel": "Rij zonder kennisbank"}, ""); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	g := f.service()

	if _, err := g.getScopedNode(ctx, "", zonderKB); !errors.Is(err, errGraphNotFound) {
		t.Errorf("een rij zonder kb_id kwam er zonder afbakening uit: %v", err)
	}
	if _, err := g.getScopedNode(ctx, "", gid(tableThema, 1)); !errors.Is(err, errGraphNotFound) {
		t.Errorf("getScopedNode = %v, wil %v", err, errGraphNotFound)
	}
	nodes, err := g.listScopedTable(ctx, "", tableThema)
	if err != nil || len(nodes) != 0 {
		t.Errorf("listScopedTable = %v, %v; wil een leeg antwoord", nodeIDs(nodes), err)
	}
	nodes, err = g.fetchNodes(ctx, "", []string{gid(tableThema, 1)})
	if err != nil || len(nodes) != 0 {
		t.Errorf("fetchNodes = %v, %v; wil een leeg antwoord", nodeIDs(nodes), err)
	}

	// Mét kennisbank leest dezelfde graaf gewoon door.
	if _, err := g.getScopedNode(ctx, testKBID, gid(tableThema, 1)); err != nil {
		t.Errorf("getScopedNode met kennisbank: %v", err)
	}
	if nodes, err := g.listScopedTable(ctx, testKBID, tableThema); err != nil || len(nodes) != 2 {
		t.Errorf("listScopedTable met kennisbank = %v, %v; wil de twee thema's", nodeIDs(nodes), err)
	}
	if nodes, err := g.fetchNodes(ctx, testKBID, []string{gid(tableThema, 1)}); err != nil || len(nodes) != 1 {
		t.Errorf("fetchNodes met kennisbank = %v, %v; wil het thema", nodeIDs(nodes), err)
	}
}

// --- childCounts ------------------------------------------------------------
//
// De kaart tekent een "+" op een node die nog open kan. Dat teken mag geen
// belofte zijn die de data niet nakomt: de telling in de overview hoort exact
// gelijk te zijn aan wat children voor diezelfde node als `total` meldt. Die
// gelijkheid wordt hieronder in één test meegenomen, zodat de twee routes niet
// stilletjes uit elkaar kunnen lopen.

func TestGraphOverview_ChildCountsMatchChildren(t *testing.T) {
	g := newGraphFixture(t).build().foreign().service()
	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}

	cases := []struct {
		naam string
		id   string
		want int
	}{
		{"domein telt zijn thema's", gid(tableDomein, 1), 2},
		{"thema telt alleen zijn subthema, niet zijn perspectief", gid(tableThema, 1), 1},
		{"subthema telt zijn standpunten", gid(tableSubthema, 1), 1},
	}
	for _, tc := range cases {
		t.Run(tc.naam, func(t *testing.T) {
			if got := out.ChildCounts[tc.id]; got != tc.want {
				t.Fatalf("childCounts[%s] = %d, wil %d", tc.id, got, tc.want)
			}
			// De toets die drift vangt: dezelfde vraag langs de andere route.
			kids, err := g.children(context.Background(), tc.id, 0)
			if err != nil {
				t.Fatalf("children: %v", err)
			}
			if kids.Total != tc.want {
				t.Fatalf("children(%s).total = %d, wil %d", tc.id, kids.Total, tc.want)
			}
		})
	}
}

func TestGraphChildren_DeduplicateAcrossRules(t *testing.T) {
	// Een standpunt leest langs twee regels (zijn citaten via has_quote, zijn
	// bronpaper via een inkomende contains). Hetzelfde kind langs beide regels
	// telt één keer — dat is wat `seen` in collectChildEdges bewaakt.
	f := newGraphFixture(t).build()
	f.relate(gid(tableDestillaat, 1), gid(tableInterview, 1), data.RelHasQuote)

	kids, err := f.service().children(context.Background(), gid(tableDestillaat, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	want := []string{gid(tableInterview, 1), gid(tableQuote, 1), gid(tableQuote, 2)}
	if got := nodeIDs(kids.Nodes); !reflect.DeepEqual(got, want) || kids.Total != len(want) {
		t.Fatalf("kinderen = %v (total %d), wil %v (het dubbel gelegde bronpaper telt één keer)", got, kids.Total, want)
	}
}

func TestGraphOverview_ChildCountsOmitZero(t *testing.T) {
	// Een node zonder kinderen staat niet met een 0 in de kaart maar helemaal
	// niet: de frontend leest afwezigheid als "geen plusje".
	f := newGraphFixture(t).build()
	kaal := f.node(gid(tableSubthema, 3), map[string]any{"titel": "Zonder standpunten"})

	out, err := f.service().overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if _, ok := out.ChildCounts[kaal]; ok {
		t.Fatalf("kinderloos subthema staat toch in childCounts: %v", out.ChildCounts)
	}
	// Wel gewoon zichtbaar als node — alleen de telling ontbreekt.
	var gezien bool
	for _, n := range out.Nodes {
		if n.ID == kaal {
			gezien = true
		}
	}
	if !gezien {
		t.Fatal("het kinderloze subthema hoort wel in nodes te staan")
	}
}

func TestGraphOverview_ChildCountsSkipInvalidChildIDs(t *testing.T) {
	// Een edge naar een id buiten de tabel-allowlist (of met een kapotte vorm)
	// telt niet mee: validGraphID is dezelfde filter als in children.
	f := newGraphFixture(t).build()
	f.relate(gid(tableDomein, 1), gid("run", 7), relContainsThema)
	f.relate(gid(tableDomein, 1), "thema:geen-uuid", relContainsThema)

	g := f.service()
	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if got := out.ChildCounts[gid(tableDomein, 1)]; got != 2 {
		t.Fatalf("childCounts domein = %d, wil 2 (ongeldige kind-ids tellen niet)", got)
	}
	kids, err := g.children(context.Background(), gid(tableDomein, 1), 0)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if kids.Total != 2 {
		t.Fatalf("children total = %d, wil 2", kids.Total)
	}
}

func TestGraphOverview_ChildCountsFailClosedOnAnEdgeError(t *testing.T) {
	// Een kapotte edge-read levert een 503, geen halve overview met een te lage
	// telling: een ontbrekend plusje zou een stille leugen zijn.
	f := newGraphFixture(t).build()
	k := &failingKnowledge{fakeKnowledge: f.k, edgesErr: errors.New("test: relaties weg")}
	g := &graphService{nodes: k, edges: k}
	out, err := g.overview(context.Background())
	assertGraphFault(t, err, 503, ReasonGraphUnavailable)
	if len(out.ChildCounts) != 0 {
		t.Fatalf("mislukte overview draagt toch tellingen: %v", out.ChildCounts)
	}
}

func TestGraphOverview_ChildCountsAreEmptyWhenTheImportIsNotReady(t *testing.T) {
	// Zonder afgeronde import is er geen structuur en dus niets te tellen.
	g := newGraphFixture(t).service()
	g.status = func() importStatus { return importStatus{State: ImportImporting} }
	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.KB.State != string(ImportImporting) {
		t.Fatalf("kb.state = %q", out.KB.State)
	}
	if len(out.ChildCounts) != 0 {
		t.Fatalf("childCounts = %v, wil leeg", out.ChildCounts)
	}
}

// De telling is een bovengrens, net als `total` in children: het kind wordt
// niet op kennisbank gefilterd, want dat zou een GetNode per kind kosten — de
// unbounded read die graph.go juist vermijdt. Wat wél vaststaat is dat de
// ouder gefilterd is: een node uit een andere kennisbank krijgt geen telling,
// want hij haalt de overview niet eens.
func TestGraphOverview_ChildCountsOnlyForNodesOfTheActiveKB(t *testing.T) {
	g := newGraphFixture(t).build().foreign().service()
	out, err := g.overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if _, ok := out.ChildCounts[gid(tableDomein, 900)]; ok {
		t.Fatalf("vreemd domein staat in childCounts: %v", out.ChildCounts)
	}
	known := map[string]bool{}
	for _, n := range out.Nodes {
		known[n.ID] = true
	}
	for id := range out.ChildCounts {
		if !known[id] {
			t.Fatalf("childCounts draagt %s, die niet in nodes staat", id)
		}
	}
}

// edgeCounter telt de ListEdges-aanroepen van een verzoek en houdt bij of er
// ooit één zonder bronlijst langskomt. Regel 1 van graph.go is dat er nooit
// unbounded gelezen wordt; een telling per node zou daar zonder groepering per
// tabel dwars tegenin gaan.
type edgeCounter struct {
	*fakeKnowledge
	calls     int
	unbounded int
}

func (c *edgeCounter) ListEdges(ctx context.Context, relation string, fromIDs, toIDs []data.NodeID) ([]data.Edge, error) {
	c.calls++
	if len(fromIDs) == 0 && len(toIDs) == 0 {
		c.unbounded++
	}
	return c.fakeKnowledge.ListEdges(ctx, relation, fromIDs, toIDs)
}

func TestGraphOverview_ChildCountsCostAHandfulOfQueries(t *testing.T) {
	f := newGraphFixture(t).build()
	// 250 extra subthema's: de kosten horen van het aantal tabellen en regels
	// af te hangen, niet van het aantal nodes.
	for i := 0; i < 250; i++ {
		f.node(gid(tableSubthema, 3000+i), map[string]any{"titel": fmt.Sprintf("Extra %03d", i)})
	}
	c := &edgeCounter{fakeKnowledge: f.k}
	g := &graphService{nodes: c, edges: c}

	if _, err := g.overview(context.Background()); err != nil {
		t.Fatalf("overview: %v", err)
	}
	// 2 relatieregels van de overview + 3 kindregels (domein 1, thema 1,
	// subthema 1).
	if c.calls != 5 {
		t.Fatalf("ListEdges-aanroepen = %d, wil 5", c.calls)
	}
	if c.unbounded != 0 {
		t.Fatalf("%d ongefilterde ListEdges: de overview leest unbounded", c.unbounded)
	}
}
