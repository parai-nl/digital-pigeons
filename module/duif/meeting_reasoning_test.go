package duif

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/parai-nl/parai-core/llm"
)

// --- welk denkbudget hoort bij welke route? ----------------------------------
//
// Het besluit staat in module.go: de chat denkt op chatReasoningEffort
// ("low"; vroeger "medium") en de gesproken beurt aan tafel op
// meetingReasoningEffort ("low"). Niet denken doen de twee herformuleringen
// (meetingQueryReasoningEffort, "none") en de detectie
// (candidateReasoningEffort, "none"; vroeger low). De bronaudit, die vroeger
// zonder denkbudget achter de beurt aan liep, bestaat niet meer in een live
// beurt.
//
// Dat besluit is per route genomen en hoort dus ook per route bewaakt te
// worden. De route staat in de context (withLLMRoute, llm_metrics.go) en het
// denkbudget in de aanvraag; de dienst is de enige plek waar die twee samen
// langskomen. Een assertie op alleen de aanvragen — zoals in answer_label_test.go
// — kan niet zien wélke route er te hard of te zacht denkt.

// effortNone is hoe de recorder "deze call stuurde geen reasoning_effort mee"
// noteert. Een woord in plaats van nil, zodat een foutmelding leesbaar is.
const effortNone = "(niets)"

// routeEffortRecorder onthoudt per route welke denkbudgetten er werkelijk de
// deur uit gingen, en laat elke call verder ongemoeid doorlopen.
type routeEffortRecorder struct {
	llm.Service
	mu   sync.Mutex
	seen map[string]map[string]int
}

func recordEfforts(inner llm.Service) *routeEffortRecorder {
	return &routeEffortRecorder{Service: inner, seen: map[string]map[string]int{}}
}

func (r *routeEffortRecorder) record(ctx context.Context, req llm.CompletionRequest) {
	effort := effortNone
	if req.ReasoningEffort != nil {
		effort = *req.ReasoningEffort
	}
	route := llmRouteFrom(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[route] == nil {
		r.seen[route] = map[string]int{}
	}
	r.seen[route][effort]++
}

func (r *routeEffortRecorder) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	r.record(ctx, req)
	return r.Service.Complete(ctx, req)
}

func (r *routeEffortRecorder) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	r.record(ctx, req)
	return r.Service.CompleteStream(ctx, req)
}

// efforts zijn de verschillende denkbudgetten die één route meekreeg,
// gesorteerd. Meer dan één betekent dat dezelfde route niet elke keer hetzelfde
// meestuurt — ook een fout.
func (r *routeEffortRecorder) efforts(route string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.seen[route]))
	for effort := range r.seen[route] {
		out = append(out, effort)
	}
	slices.Sort(out)
	return out
}

func (r *routeEffortRecorder) routes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.seen))
	for route := range r.seen {
		out = append(out, route)
	}
	slices.Sort(out)
	return out
}

// checkEfforts toetst het besluit in beide richtingen: elke verwachte route
// kwam langs met precies dat denkbudget, en er liep geen route mee waarvoor
// hier niets besloten is. Die tweede helft is het punt — een nieuwe modelcall
// die stil meeliftte op een standaard is precies wat dit besluit uitsluit.
func checkEfforts(t *testing.T, rec *routeEffortRecorder, want map[string]string) {
	t.Helper()
	for _, route := range rec.routes() {
		got := rec.efforts(route)
		expect, besloten := want[route]
		switch {
		case !besloten:
			t.Errorf("route %q liep mee zonder besluit over haar denkbudget (%v)", route, got)
		case len(got) != 1 || got[0] != expect:
			t.Errorf("route %q dacht op %v, want precies %q", route, got, expect)
		}
	}
	for route := range want {
		if len(rec.efforts(route)) == 0 {
			t.Errorf("route %q kwam in deze beurt niet voor; dan bewijst deze test haar niet", route)
		}
	}
}

// Het besluit zelf, letterlijk. De toetsen hieronder vergelijken met de
// constanten, en dat is ook wat zij horen te doen: zij bewaken dat élke route
// meestuurt wat er besloten is. Maar een constante die van waarde verandert
// schuift dan stil mee — zet iemand meetingReasoningEffort op "high", dan blijft
// alles groen terwijl de vergadertafel het dubbele staat te denken. Daarom
// staan de vier waarden hier één keer uitgeschreven: de gesproken beurt en de
// chat op low, de twee herformuleringen en de detectie op none (voor de
// snelheid). Verandert het besluit, verander dan deze test mét de constante en
// niet in plaats daarvan.
func TestReasoningEffort_TheDecisionIsLowForChatAndSpokenTurnNoneForDetectionAndQueries(t *testing.T) {
	if meetingReasoningEffort != "low" {
		t.Errorf("de gesproken beurt denkt op %q, en het besluit is \"low\"", meetingReasoningEffort)
	}
	if candidateReasoningEffort != "none" {
		t.Errorf("de detectie denkt op %q, en het besluit is \"none\"", candidateReasoningEffort)
	}
	if chatReasoningEffort != "low" {
		t.Errorf("de chat denkt op %q, en het besluit is \"low\"", chatReasoningEffort)
	}
	if meetingQueryReasoningEffort != "none" {
		t.Errorf("de herformulering denkt op %q, en het besluit is \"none\"", meetingQueryReasoningEffort)
	}
}

// De knop-route: de gesproken beurt denkt laag en haar herformulering denkt
// niet, en er loopt niets anders achter de beurt aan (de bronaudit is uit de
// live beurt verdwenen).
func TestReasoningEffort_TheSpokenTurnThinksLowAndItsQueryNotAtAll(t *testing.T) {
	rec := recordEfforts(newGatedLLM("Een korte bijdrage [1]."))
	rig, _ := queryRig(t, rec)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, spokenAboutRoofs)
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	collectSpokenText(t, es.ch)

	checkEfforts(t, rec, map[string]string{
		routeMeetingQuery: meetingQueryReasoningEffort,
		routeMeeting:      meetingReasoningEffort,
	})
}

// De zelfstandige route: dezelfde herformulering, die evenmin denkt, en de
// detectie zelf. Die laatste is de gevoeligste van de vier — de duif breekt in
// op het moment zelf, en een tragere detectie mist dat moment; daarom staat
// zij op "none" en niet meer op "low", met een eigen
// constante los van de gesproken beurt.
func TestReasoningEffort_TheAutonomousDetectionAndItsQueryDoNotThink(t *testing.T) {
	// PASS: de duif beoordeelt het venster en zegt niets. Zo blijft deze
	// meting bij de twee routes die zij bedoelt, zonder de spreekbeurt en de
	// audit die op een SPEAK volgen.
	rec := recordEfforts(newCandidateLLM(pass()))
	rig := eagerRig(t, rec, nil)

	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "lang": "nl"})
	d := rig.candidates(t, id)
	rig.say(t, id, spokenAboutRoofs)
	waitForEvaluations(t, d, 1)

	checkEfforts(t, rec, map[string]string{
		routeCandidateQuery: meetingQueryReasoningEffort,
		routeCandidate:      candidateReasoningEffort,
	})
}
