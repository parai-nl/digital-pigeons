package duif

import (
	"context"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

// runGate draait alleen de gate-stap op een handgezette state. De props die de
// zoekopdracht zou hebben opgeleverd gaan mee via de recorder in de context —
// precies de weg die de DAG ook gebruikt.
func runGate(t *testing.T, r *retrieval, sources []interaction.RAGSource, props ...map[data.NodeID]map[string]any) *turnState {
	t.Helper()
	rec := &searchRecorder{}
	for _, p := range props {
		for id, node := range p {
			rec.keep([]data.SemanticResult{{ID: id, Node: node}})
		}
	}
	ctx := context.WithValue(t.Context(), searchRecorderKey{}, rec)
	st := &turnState{}
	st.RAGSources = sources
	if err := r.gateStep(ctx, st); err != nil {
		t.Fatalf("gateStep: %v", err)
	}
	return st
}

func TestGate_KeepsOnlyDestillatenAndRenumbers(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	st := runGate(t, r, []interaction.RAGSource{
		{Number: 1, Type: "quote", ID: "quote:a", Text: "een citaat"},
		{Number: 2, Type: tableDestillaat, ID: "destillaat:b", Text: "eerste standpunt"},
		{Number: 3, Type: "quote", ID: "quote:c", Text: "nog een citaat"},
		{Number: 4, Type: tableDestillaat, ID: "destillaat:d", Text: "tweede standpunt"},
	})

	if len(st.RAGSources) != 2 {
		t.Fatalf("RAGSources = %+v, want alleen de twee standpunten", st.RAGSources)
	}
	if st.RAGSources[0].Number != 1 || st.RAGSources[1].Number != 2 {
		t.Errorf("hernummering ontbreekt: %+v", st.RAGSources)
	}
	// De prompt en de wire moeten van dezelfde nummers uitgaan; daarom
	// schrijft de gate zijn uitkomst terug in de state.
	for i, s := range st.sources {
		if s.Number != st.RAGSources[i].Number || s.ID != string(st.RAGSources[i].ID) {
			t.Errorf("bron %d loopt uit de pas: %+v vs %+v", i, s, st.RAGSources[i])
		}
	}
	if !st.UseCitations {
		t.Error("met bronnen hoort de citatiemodus aan te staan")
	}
	if st.status != KBSources {
		t.Errorf("status = %q, want sources", st.status)
	}
}

func TestGate_NoDestillaten_SetsGapAndNoCitations(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	st := runGate(t, r, []interaction.RAGSource{{Number: 1, Type: "quote", ID: "quote:a", Text: "een citaat"}})

	if st.status != KBGap {
		t.Fatalf("status = %q, want gap", st.status)
	}
	if len(st.sources) != 0 || len(st.RAGSources) != 0 {
		t.Errorf("gap mag geen bronnen opleveren: %+v / %+v", st.sources, st.RAGSources)
	}
	if st.UseCitations {
		t.Error("zonder bronnen mag de citatiemodus niet aanstaan")
	}
}

func TestGate_CompactEvidenceRanksBeforeNumberingAndSkipsIneligible(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	input := []interaction.RAGSource{
		{Number: 8, Type: tableDestillaat, ID: "destillaat:tail", Text: "Not selected.", Score: .74},
		{Number: 1, Type: "quote", ID: "quote:best", Text: "A quote cannot occupy a source slot.", Score: .99},
		{Number: 2, Type: tableDestillaat, ID: "destillaat:empty", Text: " ", Score: .98},
		{Number: 7, Type: tableDestillaat, ID: "destillaat:b", Text: "Tied B.", Score: .8},
		{Number: 6, Type: tableDestillaat, ID: "destillaat:a", Text: "Tied A.", Score: .8},
		{Number: 5, Type: tableDestillaat, ID: "destillaat:top", Text: "Highest eligible.", Score: .9},
	}
	st := runGate(t, r, input)
	// Alleen het ongeschikte valt af — de lege tekst en het citaat. Wat
	// overblijft staat op score, met stabiele ID's bij gelijkspel.
	want := []string{"destillaat:top", "destillaat:a", "destillaat:b", "destillaat:tail"}
	if len(st.sources) != len(want) || len(st.RAGSources) != len(want) || !st.UseCitations || st.status != KBSources {
		t.Fatalf("selection is not shared by prompt and wire: %+v", st.sources)
	}
	for i, id := range want {
		if st.sources[i].ID != id || string(st.RAGSources[i].ID) != id || st.sources[i].Number != i+1 || st.RAGSources[i].Number != i+1 {
			t.Fatalf("rank/citation mismatch at source %d", i)
		}
	}
	if input[0].ID != "destillaat:tail" || input[0].Number != 8 {
		t.Fatal("selection mutated the input candidate slice")
	}
}

func TestGate_ReadablePassageSurvivesHigherRankedLegacySummaries(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	st := runGate(t, r, []interaction.RAGSource{
		{Type: tableDestillaat, ID: "destillaat:legacy-a", Text: "Legacy A.", Score: .99},
		{Type: tableDestillaat, ID: "destillaat:legacy-b", Text: "Legacy B.", Score: .98},
		{Type: tableDestillaat, ID: "destillaat:legacy-c", Text: "Legacy C.", Score: .97},
		{Type: tableDestillaat, ID: "destillaat:original", Text: "Summary.", Score: .8,
			Context: &data.DestillaatContext{Quotes: []data.Quote{{ID: "quote:original", Text: "Synthetic original passage."}}}},
	})
	if len(st.sources) != 4 || st.sources[0].ID != "destillaat:original" || st.sources[0].Number != 1 || originalExcerpts(st.sources[0]) != 1 {
		t.Fatalf("readable original evidence lost its place: %+v", st.sources)
	}
	if st.sources[1].ID != "destillaat:legacy-a" || st.sources[2].ID != "destillaat:legacy-b" || st.sources[1].Grounding != groundingUnverified {
		t.Fatal("remaining legacy records lost their honest ranking or status")
	}
	for i, s := range st.sources {
		if s.Number != i+1 || st.RAGSources[i].Number != i+1 || s.ID != string(st.RAGSources[i].ID) {
			t.Fatal("readable-first ordering broke wire/prompt numbering")
		}
	}
}

func TestGate_NoSources_SetsGap(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	if st := runGate(t, r, nil); st.status != KBGap {
		t.Fatalf("status = %q, want gap", st.status)
	}
}

// De gate vult het thema ook wanneer RAGRetrieve geen graafcontext meegaf: de
// props uit de zoekopdracht dragen het gedenormaliseerde `thema_titel`.
func TestGate_ThemeWithoutGraphContext(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)

	st := runGate(t, r, []interaction.RAGSource{{Type: tableDestillaat, ID: "destillaat:1", Text: "Iets."}},
		map[data.NodeID]map[string]any{"destillaat:1": {"thema_titel": "Overlast"}})
	if st.sources[0].Theme != "Overlast" {
		t.Errorf("theme = %q, want Overlast", st.sources[0].Theme)
	}
	if st.RAGSources[0].Context == nil || st.RAGSources[0].Context.Thema == nil {
		t.Error("het thema hoort ook in de prompt-context terecht te komen")
	}
}

// Een bron zonder tekst kan niets dragen: hij zou als lege kaart op het scherm
// staan en als leeg nummer in de prompt.
func TestGate_SkipsSourcesWithoutText(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	st := runGate(t, r, []interaction.RAGSource{
		{Type: tableDestillaat, ID: "destillaat:leeg", Text: "   "},
		{Type: tableDestillaat, ID: "destillaat:vol", Text: "Een standpunt."},
	})
	if len(st.sources) != 1 || st.sources[0].ID != "destillaat:vol" {
		t.Fatalf("bronnen = %+v, want alleen het standpunt mét tekst", st.sources)
	}
}

// Zonder props valt er niets te toetsen; de bron blijft, met een reden-code in
// het log. Alles wegfilteren zou van een datafout een permanent kennisgat maken.
func TestGate_KeepsSourcesWithoutProps(t *testing.T) {
	logger, logged := captureLogs()
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	r.logger = logger
	r.status = func() importStatus { return importStatus{State: ImportReady, KBID: "duiven-proef"} }

	st := runGate(t, r, []interaction.RAGSource{{Type: tableDestillaat, ID: "destillaat:1", Text: "Iets."}})
	if len(st.sources) != 1 {
		t.Fatalf("bronnen = %+v, want de bron blijft", st.sources)
	}
	if !strings.Contains(logged(), reasonSourcePropsMissing) {
		t.Errorf("de ontbrekende props horen met reden-code gelogd te worden:\n%s", logged())
	}
}

// De DAG geeft state als `any` door; een verkeerd type is een nette fout.
func TestGate_WrongStateType_ReturnsError(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	if err := r.gateStep(t.Context(), "geen state"); err == nil {
		t.Fatal("gateStep hoort een fout te geven op een vreemd statetype")
	}
}

func TestThemeTitle_PrefersDenormalisedTitle(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"thema_titel wint", map[string]any{"thema_titel": "Cognitie", "primary_themes": []any{"voedsel"}}, "Cognitie"},
		{"fallback op primary_themes", map[string]any{"primary_themes": []any{"voedsel", "overlast"}}, "voedsel"},
		{"lijst van strings", map[string]any{"primary_themes": []string{"voedsel"}}, "voedsel"},
		{"niet-string thema telt niet", map[string]any{"primary_themes": []any{42}}, ""},
		{"lege lijst", map[string]any{"primary_themes": []any{}}, ""},
		{"niets", map[string]any{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := themeTitle(tc.props); got != tc.want {
				t.Errorf("themeTitle = %q, want %q", got, tc.want)
			}
		})
	}
}

// De wire-DTO draagt nooit een score, ook niet indirect.
func TestWireSources_DropsScores(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	got := r.wireSources(nil, []interaction.RAGSource{{
		Number: 1, Type: tableDestillaat, ID: "destillaat:1", Text: "Iets.", Score: 0.87,
		Context: &data.DestillaatContext{
			Perspective: &data.Perspective{Naam: "De kritische wetenschapper"},
			Thema:       &data.Thema{Titel: "Cognitie"},
			Quotes:      []data.Quote{{Text: "eerste"}, {Text: "tweede"}},
			Interview:   &data.Interview{Title: "lewald1987", StorageKey: "opslag/pad"},
		},
	}})
	if len(got) != 1 {
		t.Fatalf("bronnen = %+v", got)
	}
	if got[0].Ref.Quote != "eerste" {
		t.Errorf("ref.quote = %q, want het eerste citaat", got[0].Ref.Quote)
	}
	if got[0].Ref.Paper != "lewald1987" || got[0].Ref.File != "" || got[0].Ref.DOI != "" {
		t.Errorf("ref = %+v", got[0].Ref)
	}
}
