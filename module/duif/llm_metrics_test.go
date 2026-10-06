package duif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// fakeTracker verzamelt de metrieken die de begrenzer wegschrijft. Hij is
// bewust goroutine-veilig: een streamende call telt zichzelf af op de
// pomp-goroutine, terwijl de test meeleest.
type fakeTracker struct {
	mu     sync.Mutex
	events []trackedMetric
}

type trackedMetric struct {
	event string
	dims  map[string]string
	value float64
}

func (f *fakeTracker) Track(_ context.Context, event string, dims map[string]string, value float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := map[string]string{}
	for k, v := range dims {
		copied[k] = v
	}
	f.events = append(f.events, trackedMetric{event: event, dims: copied, value: value})
}

func (f *fakeTracker) PageView(context.Context, string, string) {}

func (f *fakeTracker) durations() []trackedMetric {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []trackedMetric
	for _, e := range f.events {
		if e.event == metricLLMDuration {
			out = append(out, e)
		}
	}
	return out
}

// routes geeft de gemeten routes op volgorde.
func (f *fakeTracker) routes() []string {
	var out []string
	for _, e := range f.durations() {
		out = append(out, e.dims["route"])
	}
	return out
}

// Een modelcall die 2 s of 60 s duurt is van buiten niet te
// onderscheiden zolang niemand meet. De begrenzer is de enige plek waar álle
// calls langskomen, dus daar hoort de duurmetriek: model, route, milliseconden
// — en verder niets.
func TestBoundedLLM_TracksDurationPerCall(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		tracker := &fakeTracker{}
		b := newBoundedLLM(llmtesting.NewMockService())
		b.tracker = tracker
		if _, err := b.Complete(withLLMRoute(context.Background(), routeAudit),
			llm.CompletionRequest{Model: "test-model"}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		got := tracker.durations()
		if len(got) != 1 {
			t.Fatalf("metrieken = %+v, want er precies één", got)
		}
		if got[0].dims["route"] != routeAudit || got[0].dims["model"] != "test-model" {
			t.Errorf("dimensies = %v", got[0].dims)
		}
		if got[0].value < 0 {
			t.Errorf("duur = %v ms", got[0].value)
		}
	})

	t.Run("stream", func(t *testing.T) {
		tracker := &fakeTracker{}
		mock := llmtesting.NewMockService()
		mock.SetStreamScript([]llmtesting.StreamStep{textStep("Een antwoord.")})
		b := newBoundedLLM(mock)
		b.tracker = tracker
		ch, err := b.CompleteStream(withLLMRoute(context.Background(), routePigeon),
			llm.CompletionRequest{Model: "test-model", Stream: true})
		if err != nil {
			t.Fatalf("CompleteStream: %v", err)
		}
		// De metriek hoort bij het einde van de stream, niet bij de start:
		// pas dan is bekend hoe lang de lezer gewacht heeft.
		for range ch { //nolint:revive // leegdraaien
		}
		waitFor(t, func() bool { return len(tracker.durations()) == 1 }, "de duurmetriek van de stream bleef uit")
		got := tracker.durations()[0]
		if got.dims["route"] != routePigeon || got.dims["model"] != "test-model" {
			t.Errorf("dimensies = %v", got.dims)
		}
	})

	t.Run("zonder markering blijft de call meetbaar", func(t *testing.T) {
		tracker := &fakeTracker{}
		b := newBoundedLLM(llmtesting.NewMockService())
		b.tracker = tracker
		if _, err := b.Complete(context.Background(), llm.CompletionRequest{Model: "test-model"}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if got := tracker.routes(); len(got) != 1 || got[0] != routeUnknown {
			t.Errorf("routes = %v, want [%s]", got, routeUnknown)
		}
	})
}

// De metriek draagt getallen, geen inhoud: geen prompt, geen antwoord, geen
// sessie. Deze toets kijkt naar wat er werkelijk in de dimensies staat.
func TestBoundedLLM_DurationMetricCarriesNoContent(t *testing.T) {
	tracker := &fakeTracker{}
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("Duiven op de Coolsingel.")})
	b := newBoundedLLM(mock)
	b.tracker = tracker
	ch, err := b.CompleteStream(withLLMRoute(context.Background(), routeMeeting), llm.CompletionRequest{
		Model:    "test-model",
		Stream:   true,
		Messages: []llm.Message{{Role: "user", Content: "Wat vind je van de Coolsingel?"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	for range ch { //nolint:revive // leegdraaien
	}
	waitFor(t, func() bool { return len(tracker.durations()) == 1 }, "de duurmetriek van de stream bleef uit")
	for _, e := range tracker.durations() {
		if len(e.dims) != 2 {
			t.Errorf("dimensies = %v, want alleen model en route", e.dims)
		}
		for key, value := range e.dims {
			if strings.Contains(value, "Coolsingel") {
				t.Errorf("dimensie %q draagt inhoud: %q", key, value)
			}
		}
	}
}

// De vergaderbeurt merkt zijn eigen route: zonder markering zou elke call van
// de tafel als "unknown" in de analytics landen en was de diagnose van trage
// beurten stuurloos.
func TestMeetingTurn_MarksItsRoute(t *testing.T) {
	tracker := &fakeTracker{}
	rig := newMeetingRig(t, func(m *Module) {
		bounded := newBoundedLLM(newGatedLLM("Een korte bijdrage."))
		bounded.tracker = tracker
		m.llm = bounded
	}, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Wat vind je van het plein?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	collectSpokenText(t, es.ch)

	waitFor(t, func() bool { return len(tracker.durations()) > 0 }, "de vergaderbeurt schreef geen duurmetriek")
	// Twee calls, twee eigen routes: eerst de queryherformulering die vóór de
	// retrieval staat, dan de beurt zelf. Geen van beide mag als "unknown"
	// landen — dan was de diagnose van trage beurten stuurloos, en zou
	// bovendien niet te zien zijn wat de extra stap kost.
	got := tracker.routes()
	if len(got) < 2 || got[0] != routeMeetingQuery || got[1] != routeMeeting {
		t.Errorf("routes = %v, want %q gevolgd door %q", got, routeMeetingQuery, routeMeeting)
	}
	for _, route := range got {
		if route == routeUnknown {
			t.Errorf("routes = %v, want geen enkele ongemarkeerde call", got)
		}
	}
}

// De chatbeurt markeert de duif-call en de facilitator-call apart: dat is het
// onderscheid waar de diagnose op leunt — een trage facilitator is iets anders
// dan een trage duif.
func TestChatTurn_MarksFacilitatorAndPigeonRoutes(t *testing.T) {
	tracker := &fakeTracker{}
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{
		askCoolsingel(),
		textStep("Die brede stoep is van mij."),
		textStep("Ze heeft er duidelijke ideeën over."),
	})
	m, _ := newTestModule(t, func(m *Module) {
		bounded := newBoundedLLM(mock)
		bounded.tracker = tracker
		m.llm = bounded
	})
	agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "Wat vindt de duif van de Coolsingel?", "lang": "nl",
	})

	waitFor(t, func() bool { return len(tracker.durations()) >= 3 }, "de chatbeurt schreef minder dan drie duurmetrieken")
	seen := map[string]bool{}
	for _, route := range tracker.routes() {
		seen[route] = true
	}
	for _, want := range []string{routeFacilitator, routePigeon, routeMediation} {
		if !seen[want] {
			t.Errorf("route %q ontbreekt; gemeten: %v", want, tracker.routes())
		}
	}
	if seen[routeUnknown] {
		t.Errorf("een chatbeurt hoort geen ongemarkeerde call te doen; gemeten: %v", tracker.routes())
	}
}

// Beurten die een lezer als "geen antwoord" ervoer, eindigden niet in een
// fout: de lezer viel weg (herladen, opnieuw vragen) terwijl de modelcall nog
// liep, en de server zweeg daarover. Eén WARN-regel maakt zo'n beurt achteraf
// verklaarbaar: in welke fase hij stond, hoe lang hij al liep, en hoeveel
// tekens de duif geleverd had — nooit de tekst zelf.
func TestAgentTurn_LogsWhenTheReaderDropsBeforeTheAnswer(t *testing.T) {
	logger, logs := captureLogs()
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{askCoolsingel(), textStep("bemiddeling")})
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.logger = logger
	})

	// De duif-stream blokkeert tot de lezer weg is: precies de stand waarin
	// een trage proxy de beurt zet.
	var once sync.Once
	inPigeonStream := make(chan struct{})
	m.answerStream = func(ctx context.Context, _ llm.Service, _ llm.CompletionRequest, _ []Source, _ answerPolicy) (<-chan llm.StreamChunk, error) {
		once.Do(func() { close(inPigeonStream) })
		out := make(chan llm.StreamChunk)
		go func() {
			<-ctx.Done()
			close(out)
		}()
		return out, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-inPigeonStream
		cancel()
	}()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/turn",
		turnBody(t, map[string]any{"pigeonId": "individualist", "message": "Wat vindt de duif van de Coolsingel?", "lang": "nl"}))
	req.Header.Set("Content-Type", "application/json")
	serve(t, m, req.WithContext(ctx), false)

	logged := aboveDebug(logs())
	if !strings.Contains(logged, "beurt eindigde doordat de lezer wegviel") {
		t.Fatalf("de wegvalmelding ontbreekt:\n%s", logged)
	}
	if !strings.Contains(logged, "level=WARN") {
		t.Errorf("de melding hoort op WARN te staan:\n%s", logged)
	}
	if !strings.Contains(logged, "fase="+routePigeon) {
		t.Errorf("de melding noemt de fase niet:\n%s", logged)
	}
	for _, want := range []string{"ms=", "tekens="} {
		if !strings.Contains(logged, want) {
			t.Errorf("de melding mist %q:\n%s", want, logged)
		}
	}
	// De vraag van de mens en de tekst van de duif blijven eruit.
	if strings.Contains(logged, "Coolsingel") {
		t.Errorf("de melding draagt inhoud:\n%s", logged)
	}
}

// Aan de vergadertafel eindigt een afgebroken beurt net zo stil. Daar is de
// reden wél bekend — de stopknop is geen wegvallende lezer — en die hoort in
// dezelfde regel te staan, anders leest elke stop als een storing.
func TestMeetingTurn_LogsWhenTheTurnEndsBeforeTheAnswer(t *testing.T) {
	gllm := newGatedLLM("ik ", "praat ", "door")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Iemand stelt de duif een vraag.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)
	gllm.let(t)
	nextEvent(t, es.ch, EventText)

	if status, body := rig.post(t, id, "/stop", nil); status != http.StatusAccepted {
		t.Fatalf("stop: status = %d, body = %s", status, body)
	}
	waitFor(t, func() bool {
		return strings.Contains(rig.logs(), "vergaderbeurt eindigde vóór het antwoord af was")
	}, "de afbreekmelding van de vergaderbeurt bleef uit")

	logged := aboveDebug(rig.logs())
	for _, want := range []string{"level=WARN", "reden=user_stop", "fase=" + routeMeeting, "ms=", "tekens="} {
		if !strings.Contains(logged, want) {
			t.Errorf("de melding mist %q:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "praat door") {
		t.Errorf("de melding draagt de bijdrage van de duif:\n%s", logged)
	}
}
