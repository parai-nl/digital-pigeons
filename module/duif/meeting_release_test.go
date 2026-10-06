package duif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/parai-nl/parai-core/llm"
)

func trackedSession(t *testing.T) (*meetingEngine, *meetingSession, *fakeClock) {
	t.Helper()
	e, clock := newTestEngine(t, nil, nil)
	s, err := e.openWith(testUser.ID, "individualist", "en", meetingSettings{Autonomous: true, PlaybackTracking: true})
	if err != nil {
		t.Fatal(err)
	}
	return e, s, clock
}

func TestMeetingRelease_StopPausesAutomaticButManualStillWorks(t *testing.T) {
	_, s, _ := trackedSession(t)
	ch, unsub := s.subscribe()
	defer unsub()
	state := nextEvent(t, ch, EventState)
	if state.AutonomyPaused == nil || *state.AutonomyPaused || !state.PlaybackTracking {
		t.Fatalf("new state=%+v", state)
	}
	s.stopSpeaking()
	stop := nextEvent(t, ch, EventStopped)
	if stop.AutonomyPaused == nil || !*stop.AutonomyPaused {
		t.Fatal("stop failed to publish persistent pause")
	}
	if _, _, _, ok := s.beginTurn(true); ok {
		t.Fatal("automatic generation bypassed pause")
	}
	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("manual invitation blocked by pause")
	}
	cancel()
	s.endTurn(gen)
	if !s.settingsSnapshot().AutonomyPaused {
		t.Fatal("manual invitation resumed automation")
	}
	s.applySettings(boolPointer(true), "", nil)
	if !s.settingsSnapshot().AutonomyPaused {
		t.Fatal("redundant settings update resumed automation")
	}
	s.resumeAutonomy(s.settingsSnapshot().TurnID)
	gen, _, cancel, ok = s.beginTurn(true)
	if !ok {
		t.Fatal("explicit resume did not permit automation")
	}
	cancel()
	s.endTurn(gen)
	s.stopSpeaking()
	s.applySettings(boolPointer(false), "", nil)
	s.applySettings(boolPointer(true), "", nil)
	if !s.settingsSnapshot().AutonomyPaused {
		t.Fatal("settings change bypassed the versioned resume route")
	}
}

func TestMeetingRelease_PlaybackOwnsEchoWindowUntilActualEnd(t *testing.T) {
	for _, earlyEnd := range []bool{false, true} {
		t.Run(strconv.FormatBool(earlyEnd), func(t *testing.T) {
			_, s, clock := trackedSession(t)
			ch, unsub := s.subscribe()
			defer unsub()
			nextEvent(t, ch, EventState)
			gen, _, cancel, ok := s.beginTurn()
			if !ok {
				t.Fatal("begin failed")
			}
			defer cancel()
			id := strconv.FormatUint(gen, 10)
			s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
			if ev := nextEvent(t, ch, EventTTSAudio); ev.TurnID != id {
				t.Fatalf("unstamped audio=%+v", ev)
			}
			if s.ackPlayback(id, "ended") {
				t.Fatal("accepted end before all audio was sent")
			}
			if s.ackPlayback("older", "started") {
				t.Fatal("accepted stale generation")
			}
			s.ackPlayback(id, "started")
			s.emitTurn(gen, MeetingEvent{Type: EventTTSDone, Frames: 1})
			if earlyEnd {
				if !s.ackPlayback(id, "ended") {
					t.Fatal("actual early end rejected")
				}
				if !s.isSpeaking() {
					t.Fatal("freed slot while generation still running")
				}
			}
			s.emitTurn(gen, MeetingEvent{Type: EventResponseDone})
			s.endTurn(gen)
			if !earlyEnd {
				if !s.isSpeaking() {
					t.Fatal("generation complete was mistaken for audible end")
				}
				if _, _, _, ok := s.beginTurn(); ok {
					t.Fatal("overlapping invitation admitted")
				}
				s.appendSegment("speaker echo", "")
				s.mu.Lock()
				echo := s.transcript.segments[len(s.transcript.segments)-1].PigeonWindow
				s.mu.Unlock()
				if !echo {
					t.Fatal("client audio was admitted as fresh human speech")
				}
				if !s.ackPlayback(id, "ended") {
					t.Fatal("actual end rejected")
				}
			}
			if s.isSpeaking() {
				t.Fatal("playback ended but slot remained stuck")
			}
			if s.ackPlayback(id, "started") {
				t.Fatal("late acknowledgement restarted finished turn")
			}
			clock.skip(s.cfg.SpeakMargin + time.Millisecond)
			s.appendSegment("human after playback", "")
			s.mu.Lock()
			echo := s.transcript.segments[len(s.transcript.segments)-1].PigeonWindow
			s.mu.Unlock()
			if echo {
				t.Fatal("human speech remained marked as echo after margin")
			}
		})
	}
}

func TestMeetingRelease_PlaybackTimeoutDisconnectAndFailureAreBounded(t *testing.T) {
	for _, path := range []string{"timeout", "disconnect", "failed", "aborted"} {
		t.Run(path, func(t *testing.T) {
			_, s, clock := trackedSession(t)
			ch, unsub := s.subscribe()
			defer unsub()
			nextEvent(t, ch, EventState)
			gen, _, cancel, _ := s.beginTurn()
			defer cancel()
			id := strconv.FormatUint(gen, 10)
			s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "frame"})
			s.emitTurn(gen, MeetingEvent{Type: EventTTSDone})
			s.endTurn(gen)
			switch path {
			case "timeout":
				clock.skip(playbackTimeout - time.Second)
				s.ackPlayback(id, "started")
				clock.skip(time.Second)
				s.tick(clock.Now()) // a late started ack cannot extend the fixed deadline
			case "disconnect":
				unsub()
			default:
				if !s.ackPlayback(id, path) {
					t.Fatal("failure acknowledgement rejected")
				}
			}
			if s.isSpeaking() || !s.settingsSnapshot().AutonomyPaused {
				t.Fatalf("%s left speaking or auto running", path)
			}
			if s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "late"}) {
				t.Fatal("late generation still emitted audio")
			}
			if s.ackPlayback(id, "ended") {
				t.Fatal("old playback changed the stopped turn")
			}
		})
	}
}

func TestMeetingRelease_SoundEventsAreUniqueAndCannotOverlap(t *testing.T) {
	rig := nonverbalRig(t, nil, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "playbackTracking": true})
	s, _ := rig.engine.session(id, testUser.ID)
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)
	if status, body := rig.intervene(t, id); status != http.StatusAccepted {
		t.Fatalf("sound=%d %s", status, body)
	}
	first := nextEvent(t, ch, EventSound)
	if first.EventID == "" || first.TurnID == "" || first.Sound == nil || !first.PlaybackTracking {
		t.Fatalf("sound lacks playback identity=%+v", first)
	}
	if status, _ := rig.intervene(t, id); status != http.StatusConflict {
		t.Fatalf("overlap status=%d", status)
	}
	if !s.ackPlayback(first.TurnID, "ended") {
		t.Fatal("sound end rejected")
	}
	rig.intervene(t, id)
	second := nextEvent(t, ch, EventSound)
	if first.EventID == second.EventID || first.TurnID == second.TurnID {
		t.Fatal("a replay is indistinguishable from a new sound")
	}
}

func waitDetectorIdle(t *testing.T, d *candidateEngine, n int) {
	t.Helper()
	waitFor(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return !d.running && d.counts.Started >= n }, "de generator rondde niet af")
}

// Een storing kost één evaluatie, met een oplopende wachttijd — en het venster
// wordt niet bewaard: de volgende evaluatie leest gewoon het gesprek van dan.
func TestMeetingRelease_FailureBacksOffWithoutRetainingAWindow(t *testing.T) {
	svc := newCandidateLLM(candidateStep{err: errors.New("503 provider " + sentinel)}, pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true})
	d := rig.candidates(t, id)
	rig.say(t, id, "first original speech")
	waitDetectorIdle(t, d, 1)
	if st := d.status(); st.State != "retrying" || st.RetryAfterMS != 1000 {
		t.Fatalf("retry status=%+v", st)
	}
	// Binnen de wachttijd gaat er niets uit.
	rig.say(t, id, "second newly arrived speech")
	if n := len(svc.candidateCalls()); n != 1 {
		t.Fatalf("nieuwe spraak omzeilde de wachttijd: %d call(s)", n)
	}
	rig.clock.skip(time.Second)
	d.onTick(rig.clock.Now())
	waitDetectorIdle(t, d, 2)
	calls := svc.candidateCalls()
	if !strings.Contains(calls[1].Messages[1].Content, "second newly arrived speech") {
		t.Fatal("de volgende evaluatie las niet het gesprek van dat moment")
	}
	if d.status().State != "ready" {
		t.Fatal("een geslaagde evaluatie herstelde de stand niet")
	}
	if strings.Contains(rig.logs(), sentinel) {
		t.Fatal("provider error leaked private content")
	}
}

// Herhaalde storingen degraderen zichtbaar; de handmatige herstelknop werkt,
// en een kostenlimiet laat zich er niet mee omzeilen.
func TestMeetingRelease_RepeatedFailuresDegradeAndRetryIsBudgetSafe(t *testing.T) {
	rig := eagerRig(t, newCandidateLLM(candidateStep{err: errors.New("503")}), nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true})
	d := rig.candidates(t, id)
	for i := range maxDetectorFailures {
		rig.say(t, id, fmt.Sprintf("zin %d met genoeg woorden erin", i))
		waitDetectorIdle(t, d, i+1)
		rig.clock.skip(10 * time.Second)
		d.onTick(rig.clock.Now())
	}
	if st := d.status(); st.Enabled || !st.Retryable || st.State != "unavailable" {
		t.Fatalf("permanent outage=%+v", st)
	}
	before := d.counters().Started
	rig.clock.skip(time.Minute)
	d.onTick(rig.clock.Now())
	if d.counters().Started != before {
		t.Fatal("een uitgezette generator bleef doorproberen")
	}
	if status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/detector/retry", nil); status != http.StatusAccepted {
		t.Fatalf("conscious retry=%d", status)
	}
	waitFor(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return !d.running && d.counts.Started > before },
		"de gevraagde poging kwam niet op gang")
	// Een kostenlimiet laat zich niet met de herstelknop omzeilen. De stand
	// wordt hier expliciet gezet: of de zojuist gevraagde poging al klaar is,
	// doet voor die regel niet ter zake.
	d.mu.Lock()
	d.enabled, d.reason = true, ""
	d.mu.Unlock()
	d.disable(ReasonBudgetExceeded)
	governor := d.gov.counters()
	if status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/detector/retry", nil); status != http.StatusConflict {
		t.Fatalf("budget retry bypass=%d", status)
	}
	if d.gov.counters() != governor || d.status().State != "budget_stopped" {
		t.Fatal("retry reset budget/clock state")
	}
}

func TestMeetingRelease_DiagnosticsPrivateBoundedAndOwned(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	s, _ := rig.engine.session(id, testUser.ID)
	for range maxTimingMarks + 20 {
		s.appendSegment(sentinel, "")
	}
	status, body := rig.do(t, http.MethodGet, "/v1/meeting/sessions/"+id+"/diagnostics", nil)
	if status != http.StatusOK || strings.Contains(body, sentinel) || strings.Contains(rig.logs(), sentinel) {
		t.Fatalf("unsafe diagnosis status=%d", status)
	}
	var decoded struct{ Timing timingSnapshot }
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Timing.Recent) != maxTimingMarks || decoded.Timing.Totals["segment_commit"] != maxTimingMarks+20 {
		t.Fatalf("unbounded or inaccurate diagnostics=%+v", decoded.Timing)
	}
	foreign, _ := rig.engine.open("different-owner", "individualist", "en")
	routes := []struct{ method, suffix string }{{http.MethodGet, "diagnostics"}, {http.MethodPost, "resume"}, {http.MethodPost, "playback"}, {http.MethodPost, "detector/retry"}}
	for _, route := range routes {
		if status, _ := rig.do(t, route.method, "/v1/meeting/sessions/"+foreign.id+"/"+route.suffix, nil); status != http.StatusNotFound {
			t.Fatalf("foreign %s status=%d", route.suffix, status)
		}
		router := chi.NewRouter()
		rig.engine.routes(router)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(route.method, "/v1/meeting/sessions/"+id+"/"+route.suffix, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status=%d", route.suffix, rec.Code)
		}
	}
	s.close(CloseReasonUser)
	if status, _ := rig.do(t, http.MethodGet, "/v1/meeting/sessions/"+id+"/diagnostics", nil); status != http.StatusNotFound {
		t.Fatal("closed diagnosis still available")
	}
	if len(s.diagnostics.snapshot().Recent) != 0 || len(s.transcript.segments) != 0 {
		t.Fatal("closed session retained private/diagnostic state")
	}
}

// Deze basismeting meet de orkestratie met een gescripte generator (200 ms),
// geen echte modelnauwkeurigheid en geen providerlatentie. De casusteksten zijn
// synthetisch; alleen veilige totalen en codes gaan het log in.
type timedCandidates struct {
	llm.Service
	clock *fakeClock
}

func (t timedCandidates) CompleteStream(ctx context.Context, r llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	t.clock.skip(200 * time.Millisecond)
	return t.Service.CompleteStream(ctx, r)
}

func TestMeetingRelease_SyntheticTimingBaseline(t *testing.T) {
	cases := []struct {
		name, text string
		score      float64
		broken     bool
	}{
		{"short", "birds are here now", .95, false},
		{"long", strings.Repeat("city birds nest here ", 100), .95, false},
		{"unrelated", "the printer is broken again", .05, false},
		{"negation", "we are not removing birds", .1, false},
		{"silence", "", 0, false},
		{"provider_failure", "birds need somewhere safe", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := speak(tc.score, "threat-and-removal", "")
			if tc.score < .3 {
				step = pass()
			}
			if tc.broken {
				step = candidateStep{err: errors.New("503")}
			}
			svc := newCandidateLLM(step)
			rig := nonverbalRig(t, svc, func(e *meetingEngine) {
				e.llm = timedCandidates{Service: svc, clock: e.clock.(*fakeClock)}
			})
			id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "autonomous": true, "eagerness": "eager", "playbackTracking": true})
			s := rig.session(t, id)
			d := s.candidates
			if tc.text != "" {
				rig.say(t, id, tc.text)
				waitDetectorIdle(t, d, 1)
			} else {
				d.onTick(rig.clock.Now())
			}
			timings := s.diagnostics.snapshot()
			measured := false
			for _, mark := range timings.Recent {
				if mark.Stage == "evaluation_done" {
					measured = true
					if mark.DurationMS != 200 {
						t.Fatalf("kandidaatduur=%d", mark.DurationMS)
					}
				}
			}
			if measured != (tc.text != "") {
				t.Fatal("silence or evaluation timing incorrect")
			}
			t.Logf("fixture=%s evaluations=%d held=%d marks=%d candidateMs=200 (scripted)",
				tc.name, d.counters().Started, d.counters().Held, len(s.diagnostics.snapshot().Recent))
		})
	}
}

// Een tekst-only beurt verwacht geen geluid. De client sluit haar af met
// "ended" en de server neemt die melding aan: anders zou de interface over een
// stille opstelling melden dat het geluid niet volledig afspeelde.
func TestMeetingRelease_TextOnlyTurnAcceptsTheEndedAcknowledgement(t *testing.T) {
	_, s, _ := trackedSession(t)
	gen, _, cancel, _ := s.beginTurn()
	defer cancel()
	turnID := strconv.FormatUint(gen, 10)
	s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: "alleen tekst"})
	s.emitTurn(gen, MeetingEvent{Type: EventResponseDone})
	s.endTurn(gen)

	if !s.ackPlayback(turnID, "ended") {
		t.Error("de ended-bevestiging van een beurt zonder audio werd geweigerd")
	}
	// De andere meldingen slaan nergens op zonder afspeelvenster.
	if s.ackPlayback(turnID, "started") {
		t.Error("een started-melding zonder audio hoort geweigerd te worden")
	}
	if s.ackPlayback("999", "ended") {
		t.Error("een bevestiging van een andere beurt werd aangenomen")
	}
}

func TestMeetingRelease_StaleDisconnectCannotStopANewerTurn(t *testing.T) {
	_, s, _ := trackedSession(t)
	old, _, cancel, _ := s.beginTurn()
	cancel()
	s.endTurn(old)
	newer, _, cancel, _ := s.beginTurn()
	defer cancel()
	if s.stopSpeakingGeneration("playback_disconnected", old) || !s.isSpeaking() {
		t.Fatal("old disconnect stopped newer turn")
	}
	s.endTurn(newer)
}

// Een vastgelopen antwoordketen zonder audio is één mislukte beurt: de fout
// komt op het kanaal, maar zelfstandig meedoen blijft aan en de generator
// blijft staan. Wat de autonomie degradeert is haar eigen storingsreeks.
func TestMeetingRelease_AnswerChainErrorDoesNotPauseAutonomy(t *testing.T) {
	gllm := newGatedLLM()
	gllm.startErr = errors.New("test: 503 upstream")
	e, _ := newTestEngine(t, func(m *Module) { m.llm = gllm }, nil)
	s, _ := e.open("user-1", "biological", "en")
	s.appendSegment("er wordt iets gevraagd", "")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	if err := e.respondNow(s); err != nil {
		t.Fatalf("respondNow: %v", err)
	}
	nextEvent(t, ch, EventError)
	nextEvent(t, ch, EventResponseDone)
	waitFor(t, func() bool { return !s.isSpeaking() }, "de beurt gaf het spreekslot niet vrij")

	if s.settingsSnapshot().AutonomyPaused {
		t.Error("een mislukte antwoordketen pauzeerde zelfstandig meedoen")
	}
	if s.candidates == nil || !s.candidates.status().Enabled {
		t.Error("één mislukte beurt zette de kandidaatgenerator uit")
	}
	if got := s.diagnostics.snapshot().Totals["turn_failed"]; got != 1 {
		t.Errorf("turn_failed-markeringen = %d, wil 1", got)
	}
}

func TestMeetingRelease_TerminalGeneratorFailureReleasesPartialPlayback(t *testing.T) {
	_, s, _ := trackedSession(t)
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)
	gen, _, cancel, _ := s.beginTurn()
	defer cancel()
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "partial"})
	s.emitTurn(gen, MeetingEvent{Type: EventError, Error: ReasonLLMUnavailable})
	s.endTurn(gen)
	state := nextEvent(t, ch, EventPlaybackState)
	// De halve weergave wordt vrijgegeven, maar zelfstandig meedoen blijft aan:
	// een vastgelopen antwoordketen is één beurt, geen weergavestoring.
	if state.Playback != "failed" || state.Speaking {
		t.Fatalf("failure did not silence: %+v", state)
	}
	if s.settingsSnapshot().AutonomyPaused {
		t.Fatal("an answer-chain error paused autonomous participation")
	}
	if s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "late"}) {
		t.Fatal("late partial audio revived failed turn")
	}
	reconnect, done := s.subscribe()
	defer done()
	state = nextEvent(t, reconnect, EventState)
	if state.Playback != "" || state.Speaking {
		t.Fatal("reconnect reported failed playback as active")
	}
}

func TestMeetingRelease_ProviderStreamErrorsNeverEnterLogs(t *testing.T) {
	for _, start := range []bool{false, true} {
		t.Run(strconv.FormatBool(start), func(t *testing.T) {
			svc := newGatedLLM()
			if start {
				svc.startErr = errors.New(sentinel)
			} else {
				svc.chunks = []llm.StreamChunk{{Err: errors.New(sentinel)}}
			}
			rig := newMeetingRig(t, func(m *Module) { m.llm = svc }, nil)
			id := rig.openSession(t, "individualist", "en")
			es := rig.events(t, id)
			rig.say(t, id, "synthetic speech before failure")
			rig.respond(t, id)
			ev := nextEvent(t, es.ch, EventError)
			if ev.Error != ReasonLLMUnavailable {
				t.Fatalf("failure code=%s", ev.Error)
			}
			if strings.Contains(rig.logs(), sentinel) {
				t.Fatal("raw stream error leaked to logs")
			}
		})
	}
}

func TestMeetingRelease_ResumeReportsResolvedStopGeneration(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/stop", nil)
	s, _ := rig.engine.session(id, testUser.ID)
	status, raw := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/resume", map[string]any{"turnId": s.settingsSnapshot().TurnID})
	var out struct {
		AutonomyPaused bool            `json:"autonomyPaused"`
		Settings       SessionSettings `json:"settings"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusAccepted || out.AutonomyPaused || out.Settings.TurnID != "1" {
		t.Fatalf("resume lacks stop barrier proof: status=%d body=%s", status, raw)
	}
}
