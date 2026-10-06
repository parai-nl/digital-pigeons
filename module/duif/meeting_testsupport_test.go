package duif

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/parai-nl/ai-maps-duiven/module/duif/ttsresult"
	"github.com/parai-nl/parai-core/audio"
	"github.com/parai-nl/parai-core/audio/audiotesting"
	"github.com/parai-nl/parai-core/llm"
)

// meetingEpoch is het vaste startmoment van de nepklok: vergadertests praten
// over "elf minuten later", nooit over echte wachttijd.
var meetingEpoch = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

// fakeClock is de injecteerbare klok van de vergadermodus (inactiviteitstimers
// testbaar zonder te wachten). Hij is tegelijk zijn eigen Ticker: elke
// advance zet de tijd vooruit en levert precies één tick af aan de sessielus.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	stops int

	ticks chan time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start, ticks: make(chan time.Time)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(time.Duration) Ticker { return c }

func (c *fakeClock) C() <-chan time.Time { return c.ticks }

func (c *fakeClock) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stops++
}

func (c *fakeClock) stopCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stops
}

// advance zet de klok d vooruit en wacht tot de sessielus de tick ophaalt.
func (c *fakeClock) advance(t *testing.T, d time.Duration) {
	t.Helper()
	if !c.tryAdvance(d) {
		t.Fatalf("de sessielus haalde de tick na +%s niet op", d)
	}
}

// advanceProcessed zet de klok vooruit en wacht tot de sessielus die tik ook
// werkelijk verwerkt heeft. advance() wacht alleen tot de lus de tik ophaalt;
// staat de lus er dan nog middenin, dan leest een test de stand van vóór de
// tik. De lus is één goroutine op één ongebufferd kanaal, dus de tweede tik
// komt pas aan als de eerste helemaal uit de lus is.
func (c *fakeClock) advanceProcessed(t *testing.T, d time.Duration) {
	t.Helper()
	c.advance(t, d)
	c.advance(t, 0)
}

// skip verzet de klok zonder tick: voor code die alleen Now() leest, zoals
// de commit-klok van de STT-brug.
func (c *fakeClock) skip(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) tryAdvance(d time.Duration) bool {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	select {
	case c.ticks <- now:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// errStreamBroken staat voor een provider die er middenin mee ophoudt.
var errStreamBroken = errors.New("test: stream brak af")

// gatedLLM is een llm.Service die zijn chunks pas vrijgeeft als de test dat
// zegt — nodig om de stopknop mídden in een lopende beurt te testen.
// llmtesting.MockService speelt zijn script in één keer af.
type gatedLLM struct {
	mu     sync.Mutex
	calls  []llm.CompletionRequest
	querys []llm.CompletionRequest
	chunks []llm.StreamChunk

	startErr error
	release  chan struct{} // nil = alle chunks direct

	cancelOnce sync.Once
	canceled   chan struct{}
}

func newGatedLLM(texts ...string) *gatedLLM {
	g := &gatedLLM{canceled: make(chan struct{})}
	for i, s := range texts {
		chunk := llm.StreamChunk{Content: s}
		if i == len(texts)-1 {
			chunk.FinishReason = "stop"
		}
		g.chunks = append(g.chunks, chunk)
	}
	return g
}

func (g *gatedLLM) gate() { g.release = make(chan struct{}) }

// let is de vrijgave van één chunk aan de wachtende stream.
func (g *gatedLLM) let(t *testing.T) {
	t.Helper()
	select {
	case g.release <- struct{}{}:
	case <-time.After(2 * time.Second):
		t.Fatal("de LLM-stream vroeg geen chunk op")
	}
}

func (g *gatedLLM) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, nil
}

func (g *gatedLLM) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if isMeetingQueryRequest(req) {
		// De queryherformulering staat vóór de beurt en heeft niets
		// met de poort te maken: zij levert meteen haar ene regel. Zij telt ook
		// niet mee in calls — anders zou elke bestaande asserting over "de
		// zoveelste beurtcall" een stap opschuiven zonder dat er iets aan die
		// beurt veranderde.
		g.mu.Lock()
		g.querys = append(g.querys, req)
		g.mu.Unlock()
		return fixedStream(testMeetingQueryLine), nil
	}
	g.mu.Lock()
	g.calls = append(g.calls, req)
	err := g.startErr
	g.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make(chan llm.StreamChunk)
	go func() {
		defer close(out)
		for _, c := range g.chunks {
			if g.release != nil {
				select {
				case <-g.release:
				case <-ctx.Done():
					g.markCanceled()
					return
				}
			}
			select {
			case out <- c:
			case <-ctx.Done():
				g.markCanceled()
				return
			}
		}
	}()
	return out, nil
}

func (g *gatedLLM) markCanceled() { g.cancelOnce.Do(func() { close(g.canceled) }) }

// queryCalls zijn de herformuleringsaanroepen van deze rig.
func (g *gatedLLM) queryCalls() []llm.CompletionRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]llm.CompletionRequest(nil), g.querys...)
}

func (g *gatedLLM) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

// lastCall geeft de laatste spreekbeurt-aanroep.
func (g *gatedLLM) lastCall(t *testing.T) llm.CompletionRequest {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.calls) == 0 {
		t.Fatal("de LLM kreeg geen enkele spreekbeurt-aanroep")
		return llm.CompletionRequest{}
	}
	return g.calls[len(g.calls)-1]
}

// fakeVoice is de TTS-hook-seam in tests: hij levert audioframes, eventueel
// per stuk vrijgegeven, onthoudt wat er gezegd werd en of de beurt-context
// hem afbrak (stopknop).
type fakeVoice struct {
	mu     sync.Mutex
	calls  []SpeechRequest
	said   []string
	ends   int
	frames [][]byte
	format AudioFormat

	startErr  error
	resultErr error
	release   chan struct{}
	// opening en openGate maken het dialen zichtbaar en tegenhoudbaar: opening
	// gaat dicht zodra Open binnen is, openGate houdt hem daar vast tot de test
	// hem loslaat. Zo staat een trage stemhost model in een test.
	opening  chan struct{}
	openGate chan struct{}

	cancelOnce sync.Once
	canceled   chan struct{}
	openOnce   sync.Once
}

func newFakeVoice(frames ...string) *fakeVoice {
	v := &fakeVoice{canceled: make(chan struct{}), format: AudioFormat{Codec: CodecMP3, SampleRate: 44100}}
	for _, f := range frames {
		v.frames = append(v.frames, []byte(f))
	}
	return v
}

func (v *fakeVoice) gate() { v.release = make(chan struct{}) }

// blockOpen laat Open hangen tot de teruggegeven functie hem loslaat — of, als
// die nooit komt, tot de context omvalt.
func (v *fakeVoice) blockOpen() func() {
	v.opening = make(chan struct{})
	v.openGate = make(chan struct{})
	return sync.OnceFunc(func() { close(v.openGate) })
}

// waitForOpen wacht tot de beurt daadwerkelijk aan het dialen is.
func (v *fakeVoice) waitForOpen(t *testing.T) {
	t.Helper()
	select {
	case <-v.opening:
	case <-time.After(2 * time.Second):
		t.Fatal("de beurt begon niet aan het openen van de stem")
	}
}

func (v *fakeVoice) let(t *testing.T) {
	t.Helper()
	select {
	case v.release <- struct{}{}:
	case <-time.After(2 * time.Second):
		t.Fatal("de TTS-stream vroeg geen frame op")
	}
}

func (v *fakeVoice) Format() AudioFormat { return v.format }

// Open levert de gescripte frames zodra de beurt eindigt (End) — of, met een
// gate, één per let(). De frames komen dus pas na de tekst, zoals bij een
// echte provider die op de laatste zin wacht.
func (v *fakeVoice) Open(ctx context.Context, req SpeechRequest) (Speech, error) {
	v.mu.Lock()
	v.calls = append(v.calls, req)
	err := v.startErr
	v.mu.Unlock()
	if v.openGate != nil {
		if v.opening != nil {
			v.openOnce.Do(func() { close(v.opening) })
		}
		select {
		case <-v.openGate:
		case <-ctx.Done():
			v.markCanceled()
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	out := make(chan []byte)
	ended := make(chan struct{})
	go func() {
		defer close(out)
		select {
		case <-ended:
		case <-ctx.Done():
			v.markCanceled()
			return
		}
		for _, f := range v.frames {
			if v.release != nil {
				select {
				case <-v.release:
				case <-ctx.Done():
					v.markCanceled()
					return
				}
			}
			select {
			case out <- f:
			case <-ctx.Done():
				v.markCanceled()
				return
			}
		}
	}()
	return &fakeSpeech{v: v, out: out, ended: ended}, nil
}

type fakeSpeech struct {
	v     *fakeVoice
	out   chan []byte
	ended chan struct{}
	once  sync.Once
}

func (sp *fakeSpeech) Say(text string) {
	sp.v.mu.Lock()
	sp.v.said = append(sp.v.said, text)
	sp.v.mu.Unlock()
}

func (sp *fakeSpeech) End() {
	sp.v.mu.Lock()
	sp.v.ends++
	sp.v.mu.Unlock()
	sp.once.Do(func() { close(sp.ended) })
}

func (sp *fakeSpeech) Frames() <-chan []byte { return sp.out }
func (sp *fakeSpeech) Result() <-chan error {
	ch := make(chan error, 1)
	ch <- sp.v.resultErr
	close(ch)
	return ch
}

func (v *fakeVoice) markCanceled() { v.cancelOnce.Do(func() { close(v.canceled) }) }

func (v *fakeVoice) speakCalls() []SpeechRequest {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]SpeechRequest, len(v.calls))
	copy(out, v.calls)
	return out
}

// spokenChunks is wat de beurt zin voor zin aan de stem gaf.
func (v *fakeVoice) spokenChunks() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, len(v.said))
	copy(out, v.said)
	return out
}

// spokenText is alles wat de stem te horen kreeg, als één uiting.
func (v *fakeVoice) spokenText() string { return strings.Join(v.spokenChunks(), " ") }

func (v *fakeVoice) endCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.ends
}

// scriptedSession is een gescripte TTS-providersessie: de test bepaalt welke
// audio eruit komt en leest terug wat de adapter erin schreef. Bewust alleen
// de publieke sessievelden; finishResult bevestigt of weigert voltooiing.
// De compatibiliteitsseam houdt oude API-checkouts compileerbaar.
type scriptedSession struct {
	textCh     chan string
	audioCh    chan []byte
	done       chan struct{}
	result     chan error
	resultOnce sync.Once

	mu          sync.Mutex
	written     []string
	textClosed  int
	closeCalls  int
	closeErr    error
	closePanics bool

	doneOnce  sync.Once
	audioOnce sync.Once
}

// newScriptedSession leest mee met wat er geschreven wordt (zoals de echte
// provider: buffer 16 op Text).
func newScriptedSession() *scriptedSession { return newSessionScript(16, true) }

// newStuckScriptedSession leest níets: de schrijver blijft op de eerste zin
// hangen tot de sessie eindigt.
func newStuckScriptedSession() *scriptedSession { return newSessionScript(0, false) }

func newSessionScript(textBuffer int, collect bool) *scriptedSession {
	s := &scriptedSession{
		textCh:  make(chan string, textBuffer),
		audioCh: make(chan []byte, 8),
		done:    make(chan struct{}),
		result:  make(chan error, 1),
	}
	if collect {
		go func() {
			for chunk := range s.textCh {
				s.mu.Lock()
				s.written = append(s.written, chunk)
				s.mu.Unlock()
			}
			s.mu.Lock()
			s.textClosed++
			s.mu.Unlock()
		}()
	}
	return s
}

// session is de sessie zoals de provider hem teruggeeft.
func (s *scriptedSession) session() *audio.SynthesizeSession {
	ss := &audio.SynthesizeSession{
		Text:  s.textCh,
		Audio: s.audioCh,
		Done:  s.done,
		Close: s.close,
	}
	ttsresult.Set(ss, s.result)
	return ss
}

// synthesizer verpakt deze sessie in de mock van core.
func (s *scriptedSession) synthesizer() *audiotesting.MockSynthesizer {
	return &audiotesting.MockSynthesizer{Session: s.session()}
}

func (s *scriptedSession) close() error {
	s.finishResult(context.Canceled)
	s.mu.Lock()
	s.closeCalls++
	err, boem := s.closeErr, s.closePanics
	s.mu.Unlock()
	s.doneOnce.Do(func() { close(s.done) })
	s.audioOnce.Do(func() { close(s.audioCh) })
	if boem {
		panic("test: Close() ontplofte")
	}
	return err
}

// deliver zet één audioframe klaar. Hij meldt een vastloper als fout in
// plaats van via t.Fatal: hij wordt ook vanuit een hulp-goroutine aangeroepen,
// en daar mag alleen de testgoroutine zelf stoppen.
func (s *scriptedSession) deliver(frame []byte) error {
	select {
	case s.audioCh <- frame:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("de TTS-pomp nam het audioframe niet aan")
	}
}

// finishAudio is de provider die klaar is met spreken.
func (s *scriptedSession) finishAudio() {
	s.finishResult(nil)
	s.audioOnce.Do(func() { close(s.audioCh) })
}
func (s *scriptedSession) finishResult(err error) {
	s.resultOnce.Do(func() { s.result <- err; close(s.result) })
}

// endSession sluit alleen Done: de sessie is voorbij terwijl er nog frames in
// het audiokanaal staan.
func (s *scriptedSession) endSession() { s.finishResult(nil); s.doneOnce.Do(func() { close(s.done) }) }

func (s *scriptedSession) writtenText() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.written...)
}

func (s *scriptedSession) counts() (closeCalls, textClosed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCalls, s.textClosed
}

// collectFrames leest de uitvoer van de adapter tot het kanaal sluit.
func collectFrames(t *testing.T, out <-chan []byte) [][]byte {
	t.Helper()
	var got [][]byte
	for {
		select {
		case frame, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, frame)
		case <-time.After(3 * time.Second):
			t.Fatalf("de TTS-uitvoer sloot niet (kreeg %d frames)", len(got))
			return got
		}
	}
}

// nextEvent wacht op het eerstvolgende event van dit type en slaat andere
// events over.
func nextEvent(t *testing.T, ch <-chan MeetingEvent, want string) MeetingEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("eventkanaal ging dicht vóór een %q-event", want)
			}
			if ev.Type == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("geen %q-event binnen de deadline", want)
		}
	}
}

// tickAndCollect zet de klok vooruit en verzamelt alles wat die ene tick
// oplevert, tot en met de hartslag (of de sluiting). Zo is "er kwam géén
// presence_check" een harde assert in plaats van een race.
func tickAndCollect(t *testing.T, c *fakeClock, ch <-chan MeetingEvent, d time.Duration) []MeetingEvent {
	t.Helper()
	c.advance(t, d)
	var out []MeetingEvent
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
			if ev.Type == EventListening || ev.Type == EventSessionClosed {
				return out
			}
		case <-deadline:
			t.Fatalf("tick +%s leverde geen hartslag op (kreeg %v)", d, eventTypes(out))
			return out
		}
	}
}

func eventTypes(evs []MeetingEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

// indexOfEvent geeft de plek van het eerste event van dit type in de beurt, of
// -1. Tests over de volgorde van een beurt vergelijken die plekken.
func indexOfEvent(evs []MeetingEvent, typ string) int {
	for i, e := range evs {
		if e.Type == typ {
			return i
		}
	}
	return -1
}

func hasEvent(evs []MeetingEvent, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// lockedBuffer is een logsenke die vanuit meerdere goroutines beschreven mag
// worden (de vergadermodus logt uit sessielus én beurt-goroutine).
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs geeft een logger plus een leesfunctie op wat erin geschreven is.
// De senke is goroutine-veilig: de vergaderlagen loggen vanuit sessielus,
// beurt-goroutine en TTS-pomp, dus de test leest mee terwijl er geschreven
// wordt. Dit is de enige logsenke van het pakket.
func captureLogs() (*slog.Logger, func() string) {
	sink := &lockedBuffer{}
	return slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})), sink.String
}

// aboveDebug houdt alleen de logregels bóven Debug over. De afspraak in deze
// module is dat ruwe fouttekst, kennisbanktekst en transcript hooguit op Debug
// terechtkomen; een privacytoets kijkt dus naar de rest, en niet naar het
// diagnosespoor dat bewust op Debug staat.
func aboveDebug(logs string) string {
	var b strings.Builder
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "level=DEBUG") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// testVoiceID is de stem-id die de testomgeving meegeeft. De meegeleverde
// personas dragen nog de placeholder uit personas.yaml (het stembesluit staat
// open), en zonder bruikbare stem-id roept de spreekbeurt de TTS-seam bewust
// niet meer aan — dus zetten de rigs hier de dev-stem, net als `make dev`.
const testVoiceID = "test-stem-1"

// newTestEngine bouwt een vergader-engine met nepklok, los van HTTP.
func newTestEngine(t *testing.T, mutate func(m *Module), tune func(e *meetingEngine)) (*meetingEngine, *fakeClock) {
	t.Helper()
	m, _ := newTestModule(t, mutate)
	clock := newFakeClock(meetingEpoch)
	e := m.newMeetingEngine()
	e.clock = clock
	e.devVoice = testVoiceID
	if tune != nil {
		tune(e)
	}
	m.meetings = e
	t.Cleanup(e.shutdown)
	return e, clock
}

// meetingRig draait de vergaderroutes over een echte HTTP-server, zodat de
// SSE-stream als stream getest wordt (volgorde van frames is de kern van de
// stopknop).
type meetingRig struct {
	t      *testing.T
	m      *Module
	repo   *fakeRepo
	engine *meetingEngine
	clock  *fakeClock
	srv    *httptest.Server
	logs   func() string
}

func newMeetingRig(t *testing.T, mutate func(m *Module), tune func(e *meetingEngine)) *meetingRig {
	t.Helper()
	logger, logs := captureLogs()

	m, repo := newTestModule(t, func(m *Module) {
		m.logger = logger
		if mutate != nil {
			mutate(m)
		}
	})
	clock := newFakeClock(meetingEpoch)
	e := m.newMeetingEngine()
	e.clock = clock
	e.devVoice = testVoiceID
	if tune != nil {
		tune(e)
	}
	m.meetings = e
	t.Cleanup(e.shutdown)

	r := chi.NewRouter()
	withTestUser(r)
	m.Routes(r, nil, nil)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &meetingRig{t: t, m: m, repo: repo, engine: e, clock: clock, srv: srv, logs: logs}
}

func (rig *meetingRig) do(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, rig.srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rig.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s body lezen: %v", method, path, err)
	}
	return resp.StatusCode, string(raw)
}

// post is de korte vorm van een besturingsverzoek op één sessie.
func (rig *meetingRig) post(t *testing.T, sessionID, route string, body any) (int, string) {
	t.Helper()
	return rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+sessionID+route, body)
}

// sessionIDOf leest de sessie-id uit een open-antwoord.
func sessionIDOf(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.SessionID == "" {
		t.Fatalf("sessie-body = %s (%v)", body, err)
	}
	return out.SessionID
}

// textOf plakt de tekstdelta's van een reeks events aan elkaar.
func textOf(evs []MeetingEvent) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Type == EventText {
			b.WriteString(e.Delta)
		}
	}
	return b.String()
}

// openSession maakt een vergadersessie via de API en geeft de id terug.
func (rig *meetingRig) openSession(t *testing.T, pigeonID, lang string) string {
	t.Helper()
	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{"pigeonId": pigeonID, "lang": lang})
	if status != http.StatusCreated {
		t.Fatalf("sessie openen: status = %d, body = %s", status, body)
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.SessionID == "" {
		t.Fatalf("sessie-body = %s (%v)", body, err)
	}
	return out.SessionID
}

// eventStream leest de SSE-kanaalstroom van één vergadersessie mee.
type eventStream struct {
	ch chan MeetingEvent

	mu   sync.Mutex
	seen []MeetingEvent
}

func (rig *meetingRig) events(t *testing.T, sessionID string) *eventStream {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rig.srv.URL+"/v1/meeting/sessions/"+sessionID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	resp, err := rig.srv.Client().Do(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("eventstroom openen: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("eventstroom status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("eventstroom Content-Type = %q", ct)
	}
	es := &eventStream{ch: make(chan MeetingEvent, 512)}
	go func() {
		defer resp.Body.Close()
		defer close(es.ch)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev MeetingEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				return
			}
			es.mu.Lock()
			es.seen = append(es.seen, ev)
			es.mu.Unlock()
			es.ch <- ev
		}
	}()
	// De eerste frame is altijd de sessiestatus: zo weet de test zeker dat de
	// abonnee bij de sessie staat vóórdat hij events uitlokt.
	first := nextEvent(t, es.ch, EventState)
	if first.SessionID != sessionID {
		t.Fatalf("statusframe hoort bij sessie %q, niet %q", first.SessionID, sessionID)
	}
	return es
}

// wait wacht op het eerstvolgende event van dit type.
func (es *eventStream) wait(t *testing.T, want string) MeetingEvent {
	t.Helper()
	return nextEvent(t, es.ch, want)
}

// collected is alles wat er tot nu toe binnenkwam.
func (es *eventStream) collected() []MeetingEvent { return es.recorded() }

func (es *eventStream) recorded() []MeetingEvent {
	es.mu.Lock()
	defer es.mu.Unlock()
	out := make([]MeetingEvent, len(es.seen))
	copy(out, es.seen)
	return out
}

// subscribersKeepUp zegt of elke abonnee van de sessie hoogstens `backlog`
// events achterloopt. Een test die sneller events uitlokt dan een vergadering
// ooit doet (de replay: duizenden tikken in een paar seconden), moet daar
// tussendoor op wachten. Anders loopt de buffer onder last vol, en kost het
// eerstvolgende besturingsevent de abonnee zijn abonnement
// (dispatchClassLocked): een browser verbindt dan opnieuw, deze test niet.
func (s *meetingSession) subscribersKeepUp(backlog int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		if len(ch) > backlog {
			return false
		}
	}
	return true
}

// recordedThrough is recorded() nadat de stroom de sessie heeft ingehaald. Het
// kanaal loopt via HTTP en dus achter op de sessie: wie direct na zijn laatste
// actie leest, mist soms het laatste event. Een merkteken achteraan
// lost dat op; de sessie levert per abonnee in volgorde af, dus alles wat
// ervóór gepubliceerd is, is dan binnen. Het merkteken zelf valt eraf.
func (es *eventStream) recordedThrough(t *testing.T, s *meetingSession) []MeetingEvent {
	t.Helper()
	const marker = "test-stroom-ingehaald"
	s.publish(MeetingEvent{Type: EventSettings, EventID: marker})
	var out []MeetingEvent
	waitFor(t, func() bool {
		evs := es.recorded()
		for i, ev := range evs {
			if ev.EventID == marker {
				out = evs[:i]
				return true
			}
		}
		return false
	}, "de eventstroom haalde de sessie niet in")
	return out
}

// testMP3Format is het formaat van de ElevenLabs-stem; de meeste tests
// kijken niet naar het formaat en nemen dit.
var testMP3Format = AudioFormat{Codec: CodecMP3, SampleRate: 44100}

// queryAnsweringLLM beantwoordt de korte queryherformulering zelf
// en laat al het andere aan de onderliggende dienst. Zonder deze wikkel zou die
// call de eerste stap van elk gescript antwoord opeten: de scriptmocks weten
// niet welk verzoek ze beantwoorden, alleen in welke volgorde ze gevraagd
// worden.
type queryAnsweringLLM struct {
	llm.Service
	mu     sync.Mutex
	querys []llm.CompletionRequest
}

func withQueryAnswers(inner llm.Service) *queryAnsweringLLM {
	return &queryAnsweringLLM{Service: inner}
}

func (q *queryAnsweringLLM) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if isMeetingQueryRequest(req) {
		q.mu.Lock()
		q.querys = append(q.querys, req)
		q.mu.Unlock()
		return fixedStream(testMeetingQueryLine), nil
	}
	return q.Service.CompleteStream(ctx, req)
}

func (q *queryAnsweringLLM) queryCalls() []llm.CompletionRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]llm.CompletionRequest(nil), q.querys...)
}
