package duif

import (
	"context"
	"errors"
	"testing"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

// De stille paden. Een gevonden kandidaat die alsnog geen bron wordt,
// verdween vroeger met hoogstens een Warn in het serverlog. Nu telt de gate
// hem, per reden, en reist die telling mee naar de wire.
//
// Die lijst is teruggebracht tot de harde fouten: geen tekst, een
// vórige kennisbank, onleesbaar bewijs en een halve zoekopdracht. Een
// kwaliteitsoordeel over de gronding laat niets meer vallen.

func TestGateDrops_CountsEveryReasonItDropsFor(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	r.status = func() importStatus { return importStatus{State: ImportReady, KBID: "duiven-nu"} }

	props := map[data.NodeID]map[string]any{
		"destillaat:leeg": {"kb_id": "duiven-nu"},
		"destillaat:oud":  {"kb_id": "duiven-toen"},
		"destillaat:goed": {"kb_id": "duiven-nu"},
	}
	st := runGate(t, r, []interaction.RAGSource{
		{Type: tableDestillaat, ID: "destillaat:leeg", Text: "   "},
		{Type: tableDestillaat, ID: "destillaat:oud", Text: "Een standpunt uit de vorige bank."},
		{Type: tableDestillaat, ID: "destillaat:goed", Text: "Een bruikbaar standpunt."},
	}, props)

	if len(st.sources) != 1 || st.sources[0].ID != "destillaat:goed" {
		t.Fatalf("bronnen = %+v, want alleen het bruikbare standpunt", st.sources)
	}
	want := map[string]int{dropEmptyText: 1, dropOtherKB: 1}
	for reason, n := range want {
		if st.drops[reason] != n {
			t.Errorf("drops[%s] = %d, want %d (alle: %v)", reason, st.drops[reason], n, st.drops)
		}
	}
	got := dropsOf(st.drops)
	if got == nil || got.Count != 2 {
		t.Fatalf("wire-vorm = %+v, want twee weggevallen kandidaten", got)
	}
}

// Een v2-bron zonder oorspronkelijke passages is een datafout in de
// bundel, geen reden om de lezer een bron te onthouden. Hij gaat mee als
// samenvattingsbron — en wel áchter de bron die haar eigen woorden wél kan
// laten zien, ook al scoort de kale bron hoger.
func TestGateDrops_V2WithoutPassagesIsKeptAsASummarySource(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	r.status = func() importStatus { return importStatus{State: ImportReady, KBID: "duiven-nu"} }

	props := map[data.NodeID]map[string]any{
		"destillaat:v2kaal": {"kb_id": "duiven-nu", "bundle_schema": "duiven/2"},
		"destillaat:goed":   {"kb_id": "duiven-nu"},
		"quote:goed":        {"grounded": true},
	}
	st := runGate(t, r, []interaction.RAGSource{
		{Type: tableDestillaat, ID: "destillaat:v2kaal", Text: "Een v2-bron zonder passages.", Score: 0.99},
		{Type: tableDestillaat, ID: "destillaat:goed", Text: "Een bruikbaar standpunt.", Score: 0.80,
			Context: &data.DestillaatContext{Quotes: []data.Quote{{ID: "quote:goed", Text: "Een oorspronkelijke passage."}}}},
	}, props)

	if len(st.sources) != 2 {
		t.Fatalf("bronnen = %+v, want beide", st.sources)
	}
	if st.sources[0].ID != "destillaat:goed" || st.sources[1].ID != "destillaat:v2kaal" {
		t.Errorf("volgorde = %q, %q; passages horen vóór samenvatting", st.sources[0].ID, st.sources[1].ID)
	}
	if got := dropsOf(st.drops); got != nil {
		t.Errorf("drops = %+v, want geen enkele", got)
	}
}

// Een onleesbare graafcontext is een eigen reden: een samenvatting mag die
// uitval niet stilletjes opvullen.
func TestGateDrops_UnreadableEvidenceHasItsOwnReason(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	rec := &searchRecorder{}
	rec.rejectContext("destillaat:stuk")
	ctx := context.WithValue(t.Context(), searchRecorderKey{}, rec)
	st := &turnState{}
	st.RAGSources = []interaction.RAGSource{{Type: tableDestillaat, ID: "destillaat:stuk", Text: "Iets."}}
	if err := r.gateStep(ctx, st); err != nil {
		t.Fatalf("gateStep: %v", err)
	}
	if st.drops[dropEvidenceUnreadable] != 1 {
		t.Fatalf("drops = %v, want één %s", st.drops, dropEvidenceUnreadable)
	}
	if st.status != KBGap {
		t.Errorf("status = %q, want gap", st.status)
	}
}

// Vallen álle kandidaten af, dan is de uitkomst een gat — maar de lezer hoort
// te zien dat er wél iets gevonden was. Dat verschil bleef vroeger
// onzichtbaar.
func TestGateDrops_GapCausedByDropsStillReportsThem(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	r.status = func() importStatus { return importStatus{State: ImportReady, KBID: "duiven-nu"} }
	st := runGate(t, r, []interaction.RAGSource{
		{Type: tableDestillaat, ID: "destillaat:oud", Text: "Een standpunt uit de vorige bank."},
	}, map[data.NodeID]map[string]any{"destillaat:oud": {"kb_id": "duiven-toen"}})

	if st.status != KBGap {
		t.Fatalf("status = %q, want gap", st.status)
	}
	drops := dropsOf(st.drops)
	if drops == nil || drops.Count != 1 || drops.Reasons[dropOtherKB] != 1 {
		t.Fatalf("drops = %+v, want één %s", drops, dropOtherKB)
	}
}

// Zonder uitval is er niets te melden: een beurt die alles doorlaat draagt geen
// mededeling, want dan zou elke beurt er een krijgen.
func TestGateDrops_NothingDroppedIsNoMessage(t *testing.T) {
	r := newTestRetrieval(t, &fakeKnowledge{}, nil)
	st := runGate(t, r, []interaction.RAGSource{
		{Type: tableDestillaat, ID: "destillaat:goed", Text: "Een bruikbaar standpunt."},
	})
	if got := dropsOf(st.drops); got != nil {
		t.Fatalf("drops = %+v, want nil", got)
	}
}

// Een halve zoekopdracht naast bruikbare bronnen was tot nu toe alleen een
// Warn. De beurt gaat door, maar de lezer weet nu dat zij op een halve
// zoekopdracht staat.
func TestRetrievalDrops_PartialSearchReachesTheResult(t *testing.T) {
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{
		id: "vol", samenvatting: "Een standpunt over daken.", score: 0.9,
		quote: "Een oorspronkelijke passage.", paper: "synthetische-studie",
	})
	r := newTestRetrieval(t, tableFailSearcher{
		KnowledgeReader: f, table: "quote", err: errors.New("test: quotetabel plat"),
	}, nil)
	got, err := r.run(context.Background(), "Wat doen daken met duiven?")
	if err != nil {
		t.Fatalf("een halve zoekopdracht mét bronnen is geen storing: %v", err)
	}
	if got.Status != KBSources || len(got.Sources) == 0 {
		t.Fatalf("resultaat = %+v, want bronnen", got)
	}
	if got.Dropped == nil || got.Dropped.Reasons[dropPartialSearch] != 1 {
		t.Fatalf("dropped = %+v, want één %s", got.Dropped, dropPartialSearch)
	}
}
