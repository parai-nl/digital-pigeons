package duif

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/parai-nl/parai-core/auth"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// fakeRepo is de in-memory sessie/bericht-repository voor tests (map-based).
type fakeRepo struct {
	mu       sync.Mutex
	sessions map[string]ChatSession
	messages map[string][]ChatMessage
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{sessions: map[string]ChatSession{}, messages: map[string][]ChatMessage{}}
}

func (f *fakeRepo) CreateSession(_ context.Context, s ChatSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeRepo) CreateComparison(_ context.Context, s ChatSession, selected []ChatMessage) (*ChatSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.sessions {
		if s.ComparisonRequestID != "" && existing.UserID == s.UserID && existing.ComparisonRequestID == s.ComparisonRequestID {
			if existing.ComparisonRequestHash != s.ComparisonRequestHash {
				return nil, ErrComparisonConflict
			}
			return &existing, nil
		}
	}
	f.sessions[s.ID] = s
	for i, m := range selected {
		m.Seq = int64(i + 1)
		m.Kind = normalizeKind(m.Kind)
		f.messages[s.ID] = append(f.messages[s.ID], m)
	}
	return &s, nil
}

func (f *fakeRepo) GetComparison(_ context.Context, userID, requestID, requestHash string) (*ChatSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.sessions {
		if s.UserID == userID && s.ComparisonRequestID == requestID && requestID != "" {
			if s.ComparisonRequestHash != requestHash {
				return nil, ErrComparisonConflict
			}
			return &s, nil
		}
	}
	return nil, ErrSessionNotFound
}

func (f *fakeRepo) GetSession(_ context.Context, id, userID string) (*ChatSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok || s.UserID != userID {
		return nil, ErrSessionNotFound
	}
	return &s, nil
}

func (f *fakeRepo) ListSessions(_ context.Context, userID string) ([]ChatSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ChatSession
	for _, s := range f.sessions {
		if s.UserID == userID {
			if s.Comparison != nil {
				s.Title = sessionTitle(s.Comparison.OriginalQuestion)
			} else {
				for _, m := range f.messages[s.ID] {
					if m.Role == "user" {
						s.Title = sessionTitle(m.Content)
						break
					}
				}
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) AppendMessage(ctx context.Context, m ChatMessage) error {
	m = stampAnswer(ctx, m)
	f.mu.Lock()
	defer f.mu.Unlock()
	// Dezelfde normalisatie als de kolomdefault in Postgres: een bericht
	// zonder kind is gewone tekst. Zonder dit zou de fake een lege kind
	// teruggeven waar de echte repo "text" geeft.
	m.Kind = normalizeKind(m.Kind)
	m.Seq = int64(len(f.messages[m.SessionID]) + 1)
	f.messages[m.SessionID] = append(f.messages[m.SessionID], m)
	return nil
}

// messagesWithKind geeft een kopie van alle opgeslagen berichten met dat kind.
// De volgorde is die van Seq, over alle sessies heen — de fake bewaart per
// sessie en de map-iteratie is willekeurig, dus zonder die sortering zou een
// test met meer dan één sessie wisselend slagen.
func (f *fakeRepo) messagesWithKind(kind string) []ChatMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ChatMessage
	for _, msgs := range f.messages {
		for _, m := range msgs {
			if m.Kind == kind {
				out = append(out, m)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b ChatMessage) int { return cmp.Compare(a.Seq, b.Seq) })
	return out
}

func (f *fakeRepo) ListMessages(_ context.Context, sessionID string, limit int) ([]ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msgs := f.messages[sessionID]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	if len(msgs) == 0 {
		return nil, nil // net als de Postgres-repo: leeg = nil-slice
	}
	out := make([]ChatMessage, len(msgs))
	copy(out, msgs)
	return out, nil
}

// messagesWithRole geeft een kopie van alle opgeslagen berichten met die rol.
func (f *fakeRepo) messagesWithRole(role string) []ChatMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ChatMessage
	for _, msgs := range f.messages {
		for _, m := range msgs {
			if m.Role == role {
				out = append(out, m)
			}
		}
	}
	return out
}

// failingEmbedder faalt op elke Embed-aanroep (llmtesting.MockEmbedder kent
// geen foutmodus).
type failingEmbedder struct{ err error }

func (e failingEmbedder) Embed(context.Context, string) ([]float32, error) { return nil, e.err }

// flakyEmbedder laat de eerste aanroepen gewoon door en laat er daarna één
// vallen. Eén beurt doet meerdere zoekacties achter elkaar en die hebben niet
// allemaal hetzelfde gewicht: de zoekopdracht op de mensvraag draagt het
// antwoord, de verwante zoekactie erna is een extraatje. failingEmbedder laat
// ze allebei vallen en kan dus niet laten zien dat alleen de tweede fout wordt
// opgevangen.
type flakyEmbedder struct {
	inner *llmtesting.MockEmbedder
	// failFrom is de eerste aanroep die faalt, 1-based; 0 = nooit. Zelfde
	// vorm als errRepo.appendErrFrom hierboven.
	failFrom int
	err      error

	mu    sync.Mutex
	calls int
}

func (e *flakyEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	e.calls++
	n := e.calls
	e.mu.Unlock()
	if e.failFrom > 0 && n >= e.failFrom {
		return nil, e.err
	}
	return e.inner.Embed(ctx, text)
}

func (e *flakyEmbedder) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// errRepo decoreert een ChatRepo met injecteerbare fouten, zodat de
// foutpaden van pipeline en handlers gedragsgericht getest worden zonder een
// half-kapotte Postgres na te bouwen. Nul-waarde = gedraagt zich als de inner
// repo.
type errRepo struct {
	ChatRepo

	createErr       error
	getErr          error
	listSessionsErr error
	listMsgErr      error

	// appendErr faalt vanaf de appendErrFrom-de AppendMessage-aanroep (1-based;
	// 0 = nooit). Zo is het userbericht (1e) los te laten falen van het
	// assistantbericht (2e).
	appendErr     error
	appendErrFrom int

	mu          sync.Mutex
	appendCalls int
}

func newErrRepo() *errRepo { return &errRepo{ChatRepo: newFakeRepo()} }

func (e *errRepo) CreateSession(ctx context.Context, s ChatSession) error {
	if e.createErr != nil {
		return e.createErr
	}
	return e.ChatRepo.CreateSession(ctx, s)
}

func (e *errRepo) GetSession(ctx context.Context, id, userID string) (*ChatSession, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	return e.ChatRepo.GetSession(ctx, id, userID)
}

func (e *errRepo) ListSessions(ctx context.Context, userID string) ([]ChatSession, error) {
	if e.listSessionsErr != nil {
		return nil, e.listSessionsErr
	}
	return e.ChatRepo.ListSessions(ctx, userID)
}

func (e *errRepo) ListMessages(ctx context.Context, sessionID string, limit int) ([]ChatMessage, error) {
	if e.listMsgErr != nil {
		return nil, e.listMsgErr
	}
	return e.ChatRepo.ListMessages(ctx, sessionID, limit)
}

func (e *errRepo) AppendMessage(ctx context.Context, m ChatMessage) error {
	e.mu.Lock()
	e.appendCalls++
	n := e.appendCalls
	e.mu.Unlock()
	if e.appendErr != nil && e.appendErrFrom > 0 && n >= e.appendErrFrom {
		return e.appendErr
	}
	return e.ChatRepo.AppendMessage(ctx, m)
}

func (e *errRepo) appendCallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.appendCalls
}

// errWriteFailed staat voor "de client verbrak de verbinding halverwege".
var errWriteFailed = errors.New("test: schrijven naar de client faalde")

// failWriter is een ResponseWriter die na okWrites geslaagde writes gaat
// falen. Dat is het enige realistische middel om het schrijffoutpad van de
// SSE-emitter te raken zonder een echte socket half dicht te trekken.
type failWriter struct {
	mu       sync.Mutex
	hdr      http.Header
	status   int
	body     strings.Builder
	okWrites int
	writes   int
	flushes  int
}

func newFailWriter(okWrites int) *failWriter {
	return &failWriter{hdr: http.Header{}, okWrites: okWrites}
}

func (w *failWriter) Header() http.Header { return w.hdr }

func (w *failWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	if w.writes > w.okWrites {
		return 0, errWriteFailed
	}
	return w.body.Write(p)
}

func (w *failWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushes++
}

func (w *failWriter) bodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

// failingLLM is een llm.Service die synchroon faalt op elke aanroep — voor
// het capfout-pad vóór de stream (llmtesting.MockService geeft StreamStep.Err
// niet synchroon terug; zie turn_test.go).
type failingLLM struct{ err error }

func (f failingLLM) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, f.err
}

func (f failingLLM) CompleteStream(context.Context, llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	return nil, f.err
}

// sseFrame is één geparsed SSE-frame.
type sseFrame map[string]any

// parseSSE parset een opgenomen text/event-stream-body naar de frames die de
// lezer werkelijk iets te lezen geven.
//
// De `phase`-frames zitten er bewust niet bij. Zij zijn de stand
// van de beurt en geen inhoud: ze dragen geen tekst, geen bron en geen fout, en
// ze kunnen in aantal veranderen zonder dat er iets aan het gesprek verandert.
// Zouden ze hier wél in staan, dan moest elke volgorde-assertie in dit pakket
// dezelfde vijf statusframes meetellen en verschoof elke index — terwijl die
// tests over de stemmen van de beurt gaan. Hun eigen volgorde staat
// onverkort in agent_phase_test.go (parseSSEAll), inclusief hun plaats
// tussen tool, artifact en done.
func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	for _, f := range parseSSEAll(t, body) {
		if f["type"] == "phase" {
			continue
		}
		frames = append(frames, f)
	}
	return frames
}

// parseSSEAll geeft élk frame, inclusief de stand-frames.
func parseSSEAll(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var f sseFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
			t.Fatalf("ongeldig SSE-frame %q: %v", line, err)
		}
		frames = append(frames, f)
	}
	return frames
}

func frameTypes(frames []sseFrame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i], _ = f["type"].(string)
	}
	return out
}

// testUser is de standaard ingelogde testgebruiker.
var testUser = &auth.User{ID: "user-1", Email: "test@example.org", Role: auth.RoleUser}

// newTestModule bouwt een Module met geïnjecteerde mocks en een router zoals
// het framework die mount (auth-user in context; de framework-middleware
// RequireModuleAccess zelf valt buiten module-tests).
func newTestModule(t *testing.T, mutate func(m *Module)) (*Module, *fakeRepo) {
	t.Helper()
	personas, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	repo := newFakeRepo()
	m := &Module{
		personas: personas,
		// Existing transport fixtures explicitly replace the editorial model
		// seam. AnswerReview integration tests reset this to nil and exercise
		// the real default guard through chat and meeting routes.
		answerStream: fixtureAnswerStream,
		// Standaardfixture: bereikbare lege KB; nil is onbeschikbaarheid.
		retrieval:     newTestRetrieval(t, &fakeKnowledge{}, nil),
		repo:          repo,
		model:         "test-model",
		detectModel:   "test-detect-model",
		minSimilarity: 0.55,
		historyWindow: 20,
		agentMaxIter:  defaultAgentMaxIter,
	}
	if mutate != nil {
		mutate(m)
	}
	// Init wired de retrieval-DAG zodra embedder én kennisbank er zijn; een
	// test die beide injecteert hoort dezelfde duif te krijgen als productie.
	//
	// De importstand hoort daarbij: `empty` is een storing en geen
	// kennisgat. Een fixture zonder eigen importer heeft geen stand, en
	// knowledgeStatus maakt daar `empty` van — dan zou élke fixtureturn
	// kb_unavailable geven en zou geen enkele test nog een gat of een bron
	// kunnen meten. De geïnjecteerde fakeKnowledge staat voor een geladen bank:
	// geïmporteerd, en wat de duif er niet in vindt is een echt kennisgat. Een
	// test die de storing zélf wil zien zet een eigen importer (zie
	// kb_update_retrieval_test.go).
	if m.embedder != nil && m.knowledge != nil {
		status := m.knowledgeStatus
		if m.kbImport == nil {
			status = func() importStatus { return importStatus{State: ImportReady} }
		}
		r, err := newRetrieval(m.embedder, m.knowledge, m.minSimilarity, status, m.logger)
		if err != nil {
			t.Fatalf("newRetrieval: %v", err)
		}
		m.retrieval = r
	} else if m.embedder != nil || m.knowledge != nil {
		m.retrieval = nil
	}
	// Idem voor de graaf: Init bouwt hem één keer, dus een test met een
	// gewirede kennisgraaf hoort dezelfde leesdienst te krijgen.
	if m.graph == nil && m.knowledge != nil {
		m.graph = m.newGraphService()
	}
	return m, repo
}

func fixtureAnswerStream(ctx context.Context, service llm.Service, req llm.CompletionRequest, _ []Source, _ answerPolicy) (<-chan llm.StreamChunk, error) {
	return service.CompleteStream(ctx, req)
}

// withTestUser zet de ingelogde testgebruiker in de context, zoals de
// framework-middleware dat in productie doet. Eén plek: elke test-router die
// zijn eigen kopie meebracht, bewees daarmee alleen zijn eigen kopie.
func withTestUser(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
			next.ServeHTTP(w, rq.WithContext(auth.ContextWithUser(rq.Context(), testUser)))
		})
	})
}

// serveOn draait één request door een router die uitsluitend de meegegeven
// routes mount, met (tenzij anonymous) de testgebruiker in de context.
func serveOn(t *testing.T, mount func(chi.Router), req *http.Request, anonymous bool) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	serveOnTo(t, mount, rec, req, anonymous)
	return rec
}

// serveOnTo schrijft naar een zelfgekozen ResponseWriter (voor het
// schrijffoutpad).
func serveOnTo(t *testing.T, mount func(chi.Router), w http.ResponseWriter, req *http.Request, anonymous bool) {
	t.Helper()
	r := chi.NewRouter()
	if !anonymous {
		withTestUser(r)
	}
	mount(r)
	r.ServeHTTP(w, req)
}

// serve draait één request door de volledige module-router.
func serve(t *testing.T, m *Module, req *http.Request, anonymous bool) *httptest.ResponseRecorder {
	t.Helper()
	return serveOn(t, func(r chi.Router) { m.Routes(r, nil, nil) }, req, anonymous)
}

// serveTo draait één request door de volledige module-router naar een
// zelfgekozen ResponseWriter, met de testgebruiker in context.
func serveTo(t *testing.T, m *Module, w http.ResponseWriter, req *http.Request) {
	t.Helper()
	serveOnTo(t, func(r chi.Router) { m.Routes(r, nil, nil) }, w, req, false)
}

// usePipeline zet een duif terug op de pipeline-motor. personas.yaml zet de
// stakeholder op de facilitator (engine: agent), dus deze stand
// bestaat in productie niet meer: de tests die hem gebruiken zijn de
// regressiesuite van het pipeline-contract zelf (retrieval → prompt → stream,
// citatiestripper, disconnect-gedrag), met de enige andere persona-only duif
// als proefkonijn. Elke test laadt zijn eigen PersonaSet, dus de wijziging
// blijft lokaal.
func usePipeline(t *testing.T, m *Module, id string) {
	t.Helper()
	p, ok := m.personas.Get(id)
	if !ok {
		t.Fatalf("persona %q ontbreekt", id)
	}
	p.Engine = EnginePipeline
	// Deze helper bewaart de historische persona-only pipelinefixture.
	// Producttests gebruiken de echte config en vragen deze helper niet.
	if id == "individualist" {
		p.Kennisbank = false
	}
}

func turnBody(t *testing.T, v any) *strings.Reader {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(string(raw))
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// originalExcerpts telt de eigen tekstfragmenten van een bron: de passages uit
// het stuk zelf, met de losse quote als terugval. Dat is de vorm waar
// brongronding op rust — een bron zonder eigen fragment draagt geen claim.
func originalExcerpts(s Source) int {
	n := 0
	for _, p := range s.Ref.Passages {
		if strings.TrimSpace(p.Text) != "" {
			n++
		}
	}
	if n == 0 && strings.TrimSpace(s.Ref.Quote) != "" {
		n = 1
	}
	return n
}
