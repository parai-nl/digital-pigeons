package duif

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio"
)

// --- testgereedschap voor de STT-brug -----------------------------------------

// errSTTDial staat voor een provider die de verbinding niet aanneemt.
var errSTTDial = errors.New("test: stt-dial faalde")

// fakeSTT is één nep-STT-sessie achter audio.RealtimeTranscriber. Hij houdt de
// audioframes vast die de brug hem doorgeeft, zodat een test kan zien wat er
// werkelijk de provider in ging — en wat er tijdens een gat sneuvelde.
type fakeSTT struct {
	in     chan []byte
	commit chan struct{}
	events chan audio.TranscriptionEvent
	done   chan struct{}

	mu     sync.Mutex
	frames [][]byte
	closed bool
}

func newFakeSTT() *fakeSTT {
	f := &fakeSTT{
		in:     make(chan []byte, 64),
		commit: make(chan struct{}, 8),
		events: make(chan audio.TranscriptionEvent, 32),
		done:   make(chan struct{}),
	}
	go f.drain()
	return f
}

// drain speelt de provider: alles wat binnenkomt wordt geteld en losgelaten.
func (f *fakeSTT) drain() {
	for {
		select {
		case <-f.done:
			return
		case frame, ok := <-f.in:
			if !ok {
				return
			}
			f.mu.Lock()
			f.frames = append(f.frames, frame)
			f.mu.Unlock()
		}
	}
}

func (f *fakeSTT) session() *audio.RealtimeSTTSession {
	return &audio.RealtimeSTTSession{
		Audio:  f.in,
		Commit: f.commit,
		Events: f.events,
		Done:   f.done,
		Close:  func() error { f.kill(); return nil },
	}
}

// kill laat de sessie sterven — de provider die er middenin mee ophoudt.
func (f *fakeSTT) kill() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	close(f.done)
}

func (f *fakeSTT) emit(t *testing.T, ev audio.TranscriptionEvent) {
	t.Helper()
	select {
	case f.events <- ev:
	case <-time.After(2 * time.Second):
		t.Fatal("de STT-brug las het event niet")
	}
}

func (f *fakeSTT) frameCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}

func (f *fakeSTT) commitCount() int { return len(f.commit) }

// fakeTranscriber is de nep achter audio.RealtimeTranscriber: hij mag de
// eerste `fails` starts weigeren, en onthoudt met welke taal hij gestart werd.
type fakeTranscriber struct {
	mu       sync.Mutex
	langs    []string
	fails    int
	sessions []*fakeSTT
}

func (f *fakeTranscriber) Start(_ context.Context, lang string) (*audio.RealtimeSTTSession, error) {
	f.mu.Lock()
	f.langs = append(f.langs, lang)
	if f.fails > 0 {
		f.fails--
		f.mu.Unlock()
		return nil, errSTTDial
	}
	s := newFakeSTT()
	f.sessions = append(f.sessions, s)
	f.mu.Unlock()
	return s.session(), nil
}

func (f *fakeTranscriber) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.langs)
}

func (f *fakeTranscriber) languages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.langs...)
}

// waitSession wacht op de n-de geslaagde STT-sessie (1-based).
func (f *fakeTranscriber) waitSession(t *testing.T, n int) *fakeSTT {
	t.Helper()
	var s *fakeSTT
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.sessions) < n {
			return false
		}
		s = f.sessions[n-1]
		return true
	}, fmt.Sprintf("er kwam geen %d-de STT-sessie", n))
	return s
}

// sleepLog vervangt het wachten tussen twee herstelpogingen: de test leest de
// backoff af in plaats van hem uit te zitten.
type sleepLog struct {
	mu sync.Mutex
	ds []time.Duration
}

func (s *sleepLog) sleep(ctx context.Context, d time.Duration) bool {
	s.mu.Lock()
	s.ds = append(s.ds, d)
	s.mu.Unlock()
	return ctx.Err() == nil
}

func (s *sleepLog) waits() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.ds...)
}

// listeningRig is de standaardopstelling van de STT-tests: een duif aan tafel
// met een nep-transcriber eronder en zonder wachttijd bij herstel.
func listeningRig(t *testing.T, tr audio.RealtimeTranscriber, sleep func(context.Context, time.Duration) bool) *meetingRig {
	t.Helper()
	return newMeetingRig(t, nil, func(e *meetingEngine) {
		e.transcriber = tr
		if sleep != nil {
			e.sttSleep = sleep
		} else {
			e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
		}
	})
}

// bridge haalt de STT-brug van een lopende sessie op.
func (rig *meetingRig) bridge(t *testing.T, sessionID string) *sttBridge {
	t.Helper()
	s, ok := rig.engine.session(sessionID, testUser.ID)
	if !ok {
		t.Fatalf("sessie %q bestaat niet", sessionID)
	}
	if s.stt == nil {
		t.Fatal("deze sessie draait zonder STT-brug")
	}
	return s.stt
}

// pushAudio stuurt één PCM16-blok de vergadersessie in.
func (rig *meetingRig) pushAudio(t *testing.T, sessionID string, frame []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		rig.srv.URL+"/v1/meeting/sessions/"+sessionID+"/audio", bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := rig.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("audio pushen: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

// --- de brug voedt het venster -----------------------------------------------

// Gecommitte segmenten zijn voer voor het venster; partials zijn alleen
// weergave. Niets start automatisch een antwoord.
func TestSTTBridge_CommittedFeedsTheWindow_PartialsAreDisplayOnly(t *testing.T) {
	tr := &fakeTranscriber{}
	rig := listeningRig(t, tr, nil)
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "duiven horen", IsFinal: false})
	partial := nextEvent(t, es.ch, EventTranscript)
	if partial.Final || partial.Text != "duiven horen" {
		t.Fatalf("een partial hoort als niet-definitief op het kanaal te staan: %+v", partial)
	}
	s, _ := rig.engine.session(id, testUser.ID)
	if n := s.segmentCount(); n != 0 {
		t.Fatalf("een partial hoort het venster niet te vullen, venster = %d", n)
	}

	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "duiven horen hier thuis", SpeakerID: "speaker_0", IsFinal: true})
	final := nextEvent(t, es.ch, EventTranscript)
	if !final.Final || final.Text != "duiven horen hier thuis" || final.Speaker != "speaker_0" {
		t.Fatalf("een commit hoort definitief het venster in te gaan: %+v", final)
	}
	waitFor(t, func() bool { return s.segmentCount() == 1 }, "het gecommitte segment bereikte het venster niet")

	// Een leeg commit-event is geen segment.
	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "   ", IsFinal: true})
	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "en verder nog iets", IsFinal: true})
	waitFor(t, func() bool { return s.segmentCount() == 2 }, "het tweede segment bereikte het venster niet")
}

// De sessietaal bereikt de STT-start.
func TestSTTBridge_StartsTheTranscriberInTheSessionLanguage(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{{"nl", "nl"}, {"en", "en"}, {"", "en"}} {
		t.Run("lang="+tc.lang, func(t *testing.T) {
			tr := &fakeTranscriber{}
			rig := listeningRig(t, tr, nil)
			id := rig.openSession(t, "individualist", tc.lang)
			tr.waitSession(t, 1)
			if got := tr.languages(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("STT gestart met %v, want [%s]", got, tc.want)
			}
			rig.do(t, http.MethodDelete, "/v1/meeting/sessions/"+id, nil)
		})
	}
}

// De STT blijft open terwijl de duif zelf aan het woord is:
// geen suspend, geen commit, geen resume-handshake. Wat er tijdens haar
// spreekbeurt binnenkomt, komt binnen en wordt alleen gemarkeerd als het op
// haar eigen woorden lijkt.
func TestSTTBridge_StaysOpenWhileThePigeonSpeaks(t *testing.T) {
	tr := &fakeTranscriber{}
	gllm := newGatedLLM("Roekoe. ", "Ik vind daar wat van.")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "wat vind jij hiervan", IsFinal: true})
	nextEvent(t, es.ch, EventTranscript)

	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)
	gllm.let(t) // de duif is nu aan het woord
	s, _ := rig.engine.session(id, testUser.ID)
	s.mu.Lock()
	gen := s.turnGen
	s.mu.Unlock()
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	// Wachten tot de eerste woorden van de duif geleverd zijn: pas dan heeft de
	// tekstpoort van de echo-zeef iets om tegen te vergelijken.
	nextEvent(t, es.ch, EventText)

	// De vergadering praat door; de STT-sessie is niet gesuspend. Deze zin is
	// niet wat de duif zegt, dus hij telt gewoon mee. Vroeger verdween
	// alles wat er tijdens haar beurt gezegd werd uit het venster, ook een
	// vraag die aan haar gericht was; het echo-geval staat in
	// TestMeeting_SegmentsDuringPigeonSpeech_AreMarkedAndExcluded.
	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "ze praat terwijl wij praten", IsFinal: true})
	during := nextEvent(t, es.ch, EventTranscript)
	if during.PigeonWindow {
		t.Error("een menselijke zin tijdens de spreekbeurt werd als echo weggezet")
	}
	gllm.let(t)
	collectSpokenText(t, es.ch)

	if n := tr.startCount(); n != 1 {
		t.Fatalf("de STT is %d keer gestart; een spreekbeurt hoort hem niet te herstarten", n)
	}
	if n := stt.commitCount(); n != 0 {
		t.Fatalf("de brug stuurde %d commits; continu meeluisteren kent geen push-to-talk-commit", n)
	}
}

// Resume regelt alleen automatische deelname; continu STT start niet opnieuw.
func TestMeetingProtocol_AutonomyResumeDoesNotRestartSTT(t *testing.T) {
	tr := &fakeTranscriber{}
	rig := listeningRig(t, tr, nil)
	id := rig.openSession(t, "individualist", "nl")
	waitFor(t, func() bool { return tr.startCount() == 1 }, "STT never started")
	if status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/resume", map[string]any{"turnId": "0"}); status != http.StatusAccepted {
		t.Fatalf("resume status=%d", status)
	}
	if tr.startCount() != 1 {
		t.Fatal("autonomy resume restarted STT")
	}
}

// --- zelfherstel -------------------------------------------------------------

func TestSTTBackoff_GrowsExponentiallyAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempt := range 8 {
		d := sttBackoff(attempt, 0)
		if d < sttBackoffMin || d > sttBackoffMax {
			t.Fatalf("backoff(%d) = %s, buiten de band %s–%s", attempt, d, sttBackoffMin, sttBackoffMax)
		}
		if attempt > 0 && d < prev {
			t.Fatalf("backoff(%d) = %s is kleiner dan de vorige %s", attempt, d, prev)
		}
		prev = d
	}
	// Het plafond is de bovengrens van de band, niet de vaste uitkomst: met
	// maximale jitter komt de backoff er precies op uit en geen milliseconde
	// erboven.
	if got := sttBackoff(99, 1); got != sttBackoffMax {
		t.Fatalf("met volle jitter hoort het plafond %s te zijn, kreeg %s", sttBackoffMax, got)
	}
	if got := sttBackoff(99, 0); got >= sttBackoffMax {
		t.Fatalf("zonder jitter hoort de backoff ónder het plafond te blijven, kreeg %s", got)
	}
	// En dát is waar de jitter voor bestaat: ook sessies die al lang aan het
	// herstellen zijn kloppen niet in gelijke tred bij de provider aan.
	if a, b := sttBackoff(99, 0), sttBackoff(99, 0.5); a == b {
		t.Fatalf("op het plafond spreidt de jitter niet meer: beide %s", a)
	}
	if a, b := sttBackoff(20, 0.2), sttBackoff(20, 0.9); !(a < b) {
		t.Fatalf("meer jitter hoort langer te wachten, ook op het plafond: %s vs %s", a, b)
	}
	// Jitter blijft binnen de band en verschuift het wachten hoorbaar.
	if a, b := sttBackoff(3, 0), sttBackoff(3, 0.99); a == b || b < a {
		t.Fatalf("jitter hoort de wachttijd te verlengen: %s vs %s", a, b)
	}
}

// De brug herstelt zichzelf: twee mislukte starts, de derde slaagt. De
// indicator vertelt de waarheid en audio uit het gat wordt weggegooid.
func TestSTTBridge_SelfHeals_AfterFailedStarts(t *testing.T) {
	tr := &fakeTranscriber{fails: 2}
	naps := &sleepLog{}
	// Elke backoff wacht op een token, zodat de test de brug midden in het gat
	// kan betrappen in plaats van erop te hopen.
	tokens := make(chan struct{})
	rig := listeningRig(t, tr, func(ctx context.Context, d time.Duration) bool {
		naps.sleep(ctx, d)
		select {
		case <-tokens:
			return ctx.Err() == nil
		case <-ctx.Done():
			return false
		}
	})
	id := rig.openSession(t, "individualist", "nl")

	// De eerste start is mislukt: de indicator zegt dat er niets meeluistert.
	es := rig.events(t, id)
	if first := es.recorded(); len(first) == 0 || first[0].Listening != ListenReconnecting {
		t.Fatalf("de indicator hoort het gat te tonen, kreeg %+v", first)
	}
	tokens <- struct{}{} // tweede poging: ook mis
	tokens <- struct{}{} // derde poging: raak

	stt := tr.waitSession(t, 1)
	if ev := nextEvent(t, es.ch, EventListening); ev.Listening != ListenActive {
		t.Fatalf("na herstel hoort de indicator weer op luisteren te staan: %+v", ev)
	}
	if n := tr.startCount(); n != 3 {
		t.Fatalf("de brug deed %d startpogingen, want 3 (twee mislukt, de derde raak)", n)
	}
	if w := naps.waits(); len(w) != 2 || w[0] > w[1] {
		t.Fatalf("er hoorden twee oplopende backoffs te zijn, kreeg %v", w)
	}

	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "en we gaan weer verder", IsFinal: true})
	if ev := nextEvent(t, es.ch, EventTranscript); ev.Text != "en we gaan weer verder" {
		t.Fatalf("na herstel hoort het transcript weer door te lopen: %+v", ev)
	}
}

// De teller loopt alleen op zolang het herstel niet lukt. Een
// STT-sessie die daadwerkelijk transcript leverde en dáárna wegvalt begint weer
// onderaan de band: een provider die het even begaf verdient geen straf van
// acht seconden.
func TestSTTBridge_AfterRealTranscript_RestartsAtTheBottomOfTheBand(t *testing.T) {
	tr := &fakeTranscriber{fails: 1}
	naps := &sleepLog{}
	tokens := make(chan struct{})
	rig := newMeetingRig(t, nil, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttJitter = func() float64 { return 0 } // de band, niet de spreiding, is hier het onderwerp
		e.sttSleep = func(ctx context.Context, d time.Duration) bool {
			naps.sleep(ctx, d)
			select {
			case <-tokens:
				return ctx.Err() == nil
			case <-ctx.Done():
				return false
			}
		}
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)

	tokens <- struct{}{} // de eerste start was mis; de tweede is raak
	first := tr.waitSession(t, 1)
	if ev := nextEvent(t, es.ch, EventListening); ev.Listening != ListenActive {
		t.Fatalf("de brug hoort te staan voor de test begint: %+v", ev)
	}

	// Deze sessie levert écht transcript en sterft daarna.
	first.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "de vergadering is begonnen", IsFinal: true})
	nextEvent(t, es.ch, EventTranscript)
	first.kill()

	waitFor(t, func() bool { return len(naps.waits()) == 2 }, "de brug ging niet opnieuw wachten")
	tokens <- struct{}{}
	tr.waitSession(t, 2)

	w := naps.waits()
	if w[1] != sttBackoff(0, 0) {
		t.Fatalf("na een sessie die transcript leverde hoort het herstel onderaan de band te beginnen (%s), kreeg %v",
			sttBackoff(0, 0), w)
	}
}

// Een sessie die middenin sterft wordt opnieuw opgezet, en de audio die
// tijdens dat gat binnenkomt wordt gedropt — nooit gebufferd.
func TestSTTBridge_DropsAudioDuringTheGap_AndReconnects(t *testing.T) {
	tr := &fakeTranscriber{}
	gate := make(chan struct{})
	naps := &sleepLog{}
	rig := listeningRig(t, tr, func(ctx context.Context, d time.Duration) bool {
		naps.sleep(ctx, d)
		select {
		case <-gate:
		case <-ctx.Done():
			return false
		}
		return ctx.Err() == nil
	})
	id := rig.openSession(t, "individualist", "nl")
	first := tr.waitSession(t, 1)

	if status, _ := rig.pushAudio(t, id, bytes.Repeat([]byte{1, 2}, 512)); status != http.StatusAccepted {
		t.Fatalf("audio pushen gaf status %d", status)
	}
	waitFor(t, func() bool { return first.frameCount() > 0 }, "de audio bereikte de STT-sessie niet")

	// De provider houdt ermee op; de brug wacht op zijn backoff.
	first.kill()
	s, _ := rig.engine.session(id, testUser.ID)
	waitFor(t, func() bool { return s.listenState() == ListenReconnecting }, "de indicator hoort het gat te melden")

	before := rig.bridge(t, id).droppedFrames()
	rig.pushAudio(t, id, bytes.Repeat([]byte{3, 4}, 512))
	if after := rig.bridge(t, id).droppedFrames(); after != before+1 {
		t.Fatalf("audio tijdens het gat hoort gedropt te worden (gedropt: %d → %d)", before, after)
	}

	close(gate)
	second := tr.waitSession(t, 2)
	if n := second.frameCount(); n != 0 {
		t.Fatalf("de nieuwe STT-sessie kreeg %d nagebufferde frames; het gat hoort verloren te gaan", n)
	}
	second.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "we zijn er weer", IsFinal: true})
	waitFor(t, func() bool { return s.segmentCount() == 1 }, "na herstel hoort het transcript weer door te lopen")
}

// Sessie dicht = brug dicht: geen goroutine, geen open provider-sessie.
func TestSTTBridge_SessionClose_StopsTheBridge(t *testing.T) {
	tr := &fakeTranscriber{}
	rig := listeningRig(t, tr, nil)
	id := rig.openSession(t, "individualist", "nl")
	stt := tr.waitSession(t, 1)

	rig.do(t, http.MethodDelete, "/v1/meeting/sessions/"+id, nil)
	select {
	case <-stt.done:
	case <-time.After(2 * time.Second):
		t.Fatal("de STT-sessie bleef open na het sluiten van de vergadersessie")
	}
	if status, _ := rig.pushAudio(t, id, []byte{1, 2, 3, 4}); status != http.StatusNotFound {
		t.Fatalf("audio naar een gesloten sessie hoort een 404 te zijn, kreeg %d", status)
	}
}

// Zonder gewirede transcriber is er niets om naartoe te streamen: dat is een
// nette 503, geen half werkende sessie (de indicator liever vals-negatief).
func TestMeetingAudio_WithoutTranscriber_IsAnHonestUnavailable(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")
	status, body := rig.pushAudio(t, id, []byte{1, 2, 3, 4})
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "stt_unavailable") {
		t.Fatalf("audio zonder STT-brug: status = %d, body = %s", status, body)
	}
}

// --- streaming-only voor de audioketen ---------------------------------------

// De harde producteis, nu op het pad waar de echte vergaderaudio loopt: een
// volledige nep-vergadering via de STT-brug laat geen spoor na in database,
// op schijf of in de logs — en de brug houdt zelf geen audio vast.
func TestSTTBridge_StreamingOnly_KeepsAudioAndTranscriptInMemoryOnly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	work, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tr := &fakeTranscriber{}
	rig := listeningRig(t, tr, nil)
	beforeTmp := snapshotTree(t, tmp)
	beforeWork := snapshotTree(t, work)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	for range 5 {
		rig.pushAudio(t, id, bytes.Repeat([]byte{7, 7}, 1024))
	}
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: sentinel + " (partial)", IsFinal: false})
	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: sentinel, IsFinal: true})
	waitFor(t, func() bool {
		s, _ := rig.engine.session(id, testUser.ID)
		return s.segmentCount() == 1
	}, "het gecommitte segment bereikte het venster niet")

	bridge := rig.bridge(t, id)
	if n := bridge.queuedFrames(); n != 0 {
		t.Errorf("de brug houdt %d audioframes vast; audio hoort door te stromen", n)
	}
	// De brug kán geen audio verzamelen: geen enkel veld kan frames bewaren.
	typ := reflect.TypeOf(bridge).Elem()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Slice {
			t.Errorf("sttBridge.%s (%s) kan audioframes verzamelen", f.Name, f.Type)
		}
		if f.Type.String() == "bytes.Buffer" || f.Type.String() == "*bytes.Buffer" {
			t.Errorf("sttBridge.%s is een buffer; vergaderaudio wordt niet gebufferd", f.Name)
		}
	}

	rig.do(t, http.MethodDelete, "/v1/meeting/sessions/"+id, nil)
	nextEvent(t, es.ch, EventSessionClosed)

	// 1. Database: niets.
	if len(rig.repo.sessions) != 0 || len(rig.repo.messages) != 0 {
		t.Errorf("de audioketen belandde in de database: %+v / %+v", rig.repo.sessions, rig.repo.messages)
	}
	// 2. Schijf: niets, niet in de tempdir en niet in de werkmap.
	for label, before := range map[string]map[string]int64{"tempdir": beforeTmp, "werkmap": beforeWork} {
		dir := tmp
		if label == "werkmap" {
			dir = work
		}
		for path, size := range snapshotTree(t, dir) {
			if old, ok := before[path]; !ok || old != size {
				t.Errorf("de audioketen schreef naar schijf (%s): %s", label, path)
			}
		}
	}
	// 3. Logs: tellers en tijden, nooit transcripttekst.
	if logs := rig.logs(); strings.Contains(logs, sentinel) {
		t.Errorf("transcripttekst staat in de logs:\n%s", logs)
	}
	// 4. En de sessie zelf is weg: er bestaat geen vollediger kopie.
	if _, ok := rig.engine.session(id, testUser.ID); ok {
		t.Error("de gesloten sessie staat nog in het geheugen van de engine")
	}
}

// --- de wacht- en sluisdetails ------------------------------------------------

func TestSleepCtx_WaitsUnlessTheSessionIsGone(t *testing.T) {
	live, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !sleepCtx(live, 0) {
		t.Error("zonder wachttijd hoort een levende sessie gewoon door te gaan")
	}
	if !sleepCtx(live, time.Millisecond) {
		t.Error("een korte wachttijd hoort af te lopen met true")
	}
	dead, stop := context.WithCancel(context.Background())
	stop()
	if sleepCtx(dead, 0) || sleepCtx(dead, time.Hour) {
		t.Error("een gesloten sessie hoort niet meer te wachten")
	}
}

// De sluis is een sluis: buiten een open STT-sessie, en bij een volle sluis,
// verdwijnt audio geteld — er groeit nooit een buffer (streaming-only).
func TestSTTBridge_Push_DropsWithoutASessionAndWhenFull(t *testing.T) {
	b := &sttBridge{frames: make(chan []byte, 2)}
	if b.push([]byte{1}) || b.droppedFrames() != 1 {
		t.Fatalf("zonder open sessie hoort audio te verdwijnen (gedropt: %d)", b.droppedFrames())
	}
	b.setLive(true)
	for i := range 2 {
		if !b.push([]byte{byte(i)}) {
			t.Fatalf("frame %d hoorde in de sluis te passen", i)
		}
	}
	if b.push([]byte{9}) || b.droppedFrames() != 2 {
		t.Fatalf("een volle sluis hoort te droppen, niet te groeien (gedropt: %d)", b.droppedFrames())
	}
	if n := b.queuedFrames(); n != 2 {
		t.Fatalf("er staan %d frames in de sluis, want 2", n)
	}
}

// Een provider die het niet bijhoudt krijgt geen groeiende buffer maar een gat.
func TestSTTBridge_Pump_DropsWhenTheProviderCannotKeepUp(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.close(CloseReasonUser) })
	_ = clock

	b := &sttBridge{engine: e, session: s, frames: make(chan []byte, 4)}
	b.frames <- []byte{1, 2}
	b.frames <- []byte{3, 4}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deaf := make(chan []byte) // niemand leest mee
	done := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		b.pump(ctx, &audio.RealtimeSTTSession{
			Audio: deaf, Events: make(chan audio.TranscriptionEvent), Done: done,
			Close: func() error { return nil },
		})
	}()
	waitFor(t, func() bool { return b.droppedFrames() == 2 },
		"een dove provider hoort tot gedropte frames te leiden")
	close(done)
	<-closed
}

// De supervisor stopt netjes: een gesloten sessie belt de provider niet meer,
// en een onderbroken backoff is het einde van de brug — geen wees-goroutine.
func TestSTTBridge_Run_StopsWithTheSession(t *testing.T) {
	e, _ := newTestEngine(t, nil, func(e *meetingEngine) { e.transcriber = &fakeTranscriber{fails: 99} })
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.close(CloseReasonUser) })

	t.Run("al gesloten context", func(t *testing.T) {
		tr := &fakeTranscriber{}
		b := &sttBridge{engine: e, session: s, tr: tr, frames: make(chan []byte, 1),
			sleep: sleepCtx, jitter: func() float64 { return 0 }}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		b.run(ctx)
		if n := tr.startCount(); n != 0 {
			t.Fatalf("een gesloten sessie belde de provider %d keer", n)
		}
	})

	t.Run("onderbroken backoff", func(t *testing.T) {
		tr := &fakeTranscriber{fails: 99}
		b := &sttBridge{engine: e, session: s, tr: tr, frames: make(chan []byte, 1),
			sleep:  func(context.Context, time.Duration) bool { return false },
			jitter: func() float64 { return 0 }}
		b.run(context.Background())
		if n := tr.startCount(); n != 1 {
			t.Fatalf("na een onderbroken backoff hoorde de brug te stoppen, kreeg %d pogingen", n)
		}
	})

	// Hetzelfde, maar één stap verder in de lus: de provider nam wél op en hing
	// meteen op. Ook dat gat mag de brug niet vasthouden als de sessie sluit.
	t.Run("sessie weg tijdens het gat ná een opgehangen provider", func(t *testing.T) {
		tr := &hangUpTranscriber{}
		b := &sttBridge{engine: e, session: s, tr: tr, frames: make(chan []byte, 1),
			sleep:  func(context.Context, time.Duration) bool { return false },
			jitter: func() float64 { return 0 }}
		b.run(context.Background())
		if n := tr.startCount(); n != 1 {
			t.Fatalf("de brug hoorde na één opgehangen sessie te stoppen, kreeg %d pogingen", n)
		}
	})
}

// hangUpTranscriber neemt op en hangt meteen op: de sessie is al dood voordat
// de pomp hem ziet.
type hangUpTranscriber struct {
	mu     sync.Mutex
	starts int
}

func (h *hangUpTranscriber) Start(context.Context, string) (*audio.RealtimeSTTSession, error) {
	h.mu.Lock()
	h.starts++
	h.mu.Unlock()
	done := make(chan struct{})
	close(done)
	return &audio.RealtimeSTTSession{
		Audio:  make(chan []byte, 1),
		Events: make(chan audio.TranscriptionEvent),
		Done:   done,
	}, nil
}

func (h *hangUpTranscriber) startCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.starts
}

// Een provider die zijn eventkanaal sluit is net zo goed weg als een dode
// sessie: de pomp stopt en de supervisor mag opnieuw beginnen.
func TestSTTBridge_Pump_ReturnsWhenTheProviderClosesItsEvents(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.close(CloseReasonUser) })

	b := &sttBridge{engine: e, session: s, frames: make(chan []byte, 1)}
	events := make(chan audio.TranscriptionEvent, 1)
	events <- audio.TranscriptionEvent{Type: "committed", Text: "een laatste zin", IsFinal: true}
	close(events)
	heard := b.pump(context.Background(), &audio.RealtimeSTTSession{
		Audio: make(chan []byte, 1), Events: events, Done: make(chan struct{}),
	})
	if !heard {
		t.Error("deze STT-sessie leverde wel degelijk transcript")
	}
	if n := s.segmentCount(); n != 1 {
		t.Fatalf("het laatste segment hoort nog binnen te komen, venster = %d", n)
	}
}

// Een segment dat aankomt nadat de vergadering al is opgestapt hoort nergens
// meer terecht te komen: de pomp stopt, het venster blijft dicht.
func TestSTTBridge_Pump_StopsWhenTheSessionIsAlreadyClosed(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	s.close(CloseReasonUser)

	b := &sttBridge{engine: e, session: s, frames: make(chan []byte, 1)}
	events := make(chan audio.TranscriptionEvent, 1)
	events <- audio.TranscriptionEvent{Type: "committed", Text: "een zin die te laat komt", IsFinal: true}
	heard := b.pump(context.Background(), &audio.RealtimeSTTSession{
		Audio: make(chan []byte, 1), Events: events, Done: make(chan struct{}),
	})
	if !heard {
		t.Error("de provider stuurde wel degelijk een segment")
	}
	if n := s.segmentCount(); n != 0 {
		t.Fatalf("een gesloten sessie hoort niets meer op te nemen, venster = %d", n)
	}
}

// Zonder geïnjecteerde seams valt de brug terug op de echte klok en een
// willekeurige jitter — nooit op nil.
func TestNewSTTBridge_FallsBackToRealWaiting(t *testing.T) {
	e, _ := newTestEngine(t, nil, func(e *meetingEngine) {
		e.transcriber = &fakeTranscriber{}
		e.sttSleep = nil
		e.sttJitter = nil
	})
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.close(CloseReasonUser) })
	b := newSTTBridge(e, s)
	if b.sleep == nil || b.jitter == nil {
		t.Fatal("de brug hoort altijd een wachtseam en een jitter te hebben")
	}
	if j := b.jitter(); j < 0 || j > 1 {
		t.Fatalf("jitter = %v, buiten [0,1]", j)
	}
}

// Een uiting die langer dan CommitAfter doorgroeit sluit de brug zelf af:
// zonder dat blijft een spreker die doorpraat grijs op het scherm en
// onhoorbaar voor de detector. Een commit van de provider zet de klok terug.
func TestSTTBridge_ForcesACommitOnALongUtterance(t *testing.T) {
	tr := &fakeTranscriber{}
	rig := listeningRig(t, tr, nil)
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)
	bridge := rig.bridge(t, id)

	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "we praten"})
	nextEvent(t, es.ch, EventTranscript)
	rig.clock.skip(defaultCommitAfter - time.Second)
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "we praten nog steeds"})
	nextEvent(t, es.ch, EventTranscript)
	if n := stt.commitCount(); n != 0 {
		t.Fatalf("vóór de grens hoort er geen commit te zijn, kreeg %d", n)
	}

	rig.clock.skip(2 * time.Second)
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "we praten nog steeds door"})
	nextEvent(t, es.ch, EventTranscript)
	waitFor(t, func() bool { return stt.commitCount() == 1 }, "de brug hoort na de grens om een commit te vragen")
	if bridge.forcedCommits() != 1 {
		t.Errorf("forcedCommits = %d", bridge.forcedCommits())
	}

	// De provider sluit af; de volgende uiting begint met een schone klok.
	stt.emit(t, audio.TranscriptionEvent{Type: "committed", Text: "we praten nog steeds door", IsFinal: true})
	nextEvent(t, es.ch, EventTranscript)
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "en verder"})
	nextEvent(t, es.ch, EventTranscript)
	rig.clock.skip(defaultCommitAfter / 2)
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "en verder nog"})
	nextEvent(t, es.ch, EventTranscript)
	if n := stt.commitCount(); n != 1 {
		t.Errorf("na een providercommit hoort de klok opnieuw te beginnen, commits = %d", n)
	}
}

// CommitAfter 0 (DUIF_STT_COMMIT_SECONDS=0) zet de geforceerde commit uit.
func TestSTTBridge_CommitAfterZero_NeverForces(t *testing.T) {
	tr := &fakeTranscriber{}
	rig := newMeetingRig(t, nil, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
		e.cfg.CommitAfter = 0
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "we praten"})
	nextEvent(t, es.ch, EventTranscript)
	rig.clock.skip(time.Hour)
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "we praten uren door"})
	nextEvent(t, es.ch, EventTranscript)
	if n := stt.commitCount(); n != 0 {
		t.Errorf("met CommitAfter 0 hoort er nooit een commit te komen, kreeg %d", n)
	}
}
