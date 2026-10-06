package duif

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

var errRepoDown = errors.New("test: repository onbereikbaar")

// decodeError leest een foutbody van de wire.
func decodeError(t *testing.T, body string) apiError {
	t.Helper()
	var out apiError
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("foutbody %q is geen JSON: %v", body, err)
	}
	return out
}

// Contractgarantie: interne fouttekst bereikt de wire nooit — de client krijgt
// een generieke, tweetalige 500.
func TestSessions_RepoError_500_WithoutLeakingDetail(t *testing.T) {
	repo := newErrRepo()
	repo.listSessionsErr = errRepoDown
	m, _ := newTestModule(t, func(m *Module) { m.repo = repo })

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/chat/sessions", nil), false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	got := decodeError(t, rec.Body.String())
	if got.Error != "internal_error" {
		t.Errorf("error = %q, want internal_error", got.Error)
	}
	if got.Message["nl"] == "" || got.Message["en"] == "" {
		t.Errorf("melding moet tweetalig zijn, kreeg %+v", got.Message)
	}
	if strings.Contains(rec.Body.String(), "onbereikbaar") {
		t.Errorf("interne fouttekst lekt naar de wire: %s", rec.Body.String())
	}
}

func TestSessions_None_ReturnsEmptyArrayNotNull(t *testing.T) {
	m, _ := newTestModule(t, nil)

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/chat/sessions", nil), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"sessions":[]`) {
		t.Errorf("lege lijst moet [] zijn (niet null), kreeg %s", rec.Body.String())
	}
}

func TestSessions_Unauthorized(t *testing.T) {
	m, _ := newTestModule(t, nil)

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/chat/sessions", nil), true)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSessionMessages_Unauthorized(t *testing.T) {
	m, _ := newTestModule(t, nil)

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/abc/messages", nil), true)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSessionMessages_None_ReturnsEmptyArrayNotNull(t *testing.T) {
	m, repo := newTestModule(t, nil)
	if err := repo.CreateSession(t.Context(), ChatSession{ID: "s1", UserID: testUser.ID, PigeonID: "individualist", Lang: "en"}); err != nil {
		t.Fatal(err)
	}

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/s1/messages", nil), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"messages":[]`) {
		t.Errorf("lege lijst moet [] zijn (niet null), kreeg %s", rec.Body.String())
	}
}

func TestSessionMessages_RepoError_500(t *testing.T) {
	repo := newErrRepo()
	m, _ := newTestModule(t, func(m *Module) { m.repo = repo })
	if err := repo.CreateSession(t.Context(), ChatSession{ID: "s1", UserID: testUser.ID, PigeonID: "individualist", Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	repo.listMsgErr = errRepoDown

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/s1/messages", nil), false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec.Body.String()); got.Error != "internal_error" {
		t.Errorf("error = %q, want internal_error", got.Error)
	}
}

func TestTurn_InvalidJSONBody_400(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/turn", strings.NewReader(`{"pigeonId":`))
	rec := serve(t, m, req, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	got := decodeError(t, rec.Body.String())
	if got.Error != "invalid_request" {
		t.Errorf("error = %q, want invalid_request", got.Error)
	}
	// De Nederlandse UI toont deze zin ongewijzigd. Vroeger stond hier de
	// Engelse ontwikkelaarszin in beide slots.
	if want := badRequestMessages[reasonMalformedBody]; got.Message["nl"] != want["nl"] || got.Message["en"] != want["en"] {
		t.Errorf("message = %+v, want %+v", got.Message, want)
	}
}

func TestTurn_UnknownField_400(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/turn",
		strings.NewReader(`{"pigeonId":"individualist","message":"hoi","stiekem":"veld"}`))
	rec := serve(t, m, req, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("onbekende velden moeten geweigerd worden, status = %d", rec.Code)
	}
}

// Repo-fouten vóór de stream zijn REST-fouten, geen half-geopende SSE-stream.
func TestTurn_CreateSessionFails_500BeforeStream(t *testing.T) {
	repo := newErrRepo()
	repo.createErr = errRepoDown
	m, _ := newTestModule(t, func(m *Module) {
		m.repo = repo
		m.llm = llmtesting.NewMockService()
	})

	rec := postTurn(t, m, turnRequest{PigeonID: "individualist", Message: "hoi"}, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "event-stream") {
		t.Errorf("fout vóór de stream mag geen SSE-respons zijn (content-type %q)", ct)
	}
}

func TestTurn_HistoryLoadFails_500BeforeStream(t *testing.T) {
	repo := newErrRepo()
	repo.listMsgErr = errRepoDown
	m, _ := newTestModule(t, func(m *Module) {
		m.repo = repo
		m.llm = llmtesting.NewMockService()
	})

	rec := postTurn(t, m, turnRequest{PigeonID: "individualist", Message: "hoi"}, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTurn_UserMessagePersistFails_500BeforeStream(t *testing.T) {
	repo := newErrRepo()
	repo.appendErr = errRepoDown
	repo.appendErrFrom = 1
	m, _ := newTestModule(t, func(m *Module) {
		m.repo = repo
		m.llm = llmtesting.NewMockService()
	})

	rec := postTurn(t, m, turnRequest{PigeonID: "individualist", Message: "hoi"}, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

// Faalt het opslaan van het assistantbericht, dan mag de client daar niets van
// merken: de stream is al geleverd en sluit netjes af met done.
func TestTurn_AssistantPersistFails_StreamStillCompletes(t *testing.T) {
	repo := newErrRepo()
	repo.appendErr = errRepoDown
	repo.appendErrFrom = 2
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "roekoe", FinishReason: "stop"}}}})
	m, _ := newTestModule(t, func(m *Module) {
		m.repo = repo
		m.llm = mock
	})
	usePipeline(t, m, "individualist")

	rec := postTurn(t, m, turnRequest{PigeonID: "individualist", Message: "hoi"}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	types := frameTypes(parseSSE(t, rec.Body.String()))
	if len(types) == 0 || types[len(types)-1] != "done" {
		t.Errorf("frames = %v, want afgesloten met done", types)
	}
	if repo.appendCallCount() != 2 {
		t.Errorf("AppendMessage-aanroepen = %d, want 2 (user + assistant)", repo.appendCallCount())
	}
}

// Contractgarantie: breekt de verbinding halverwege het schrijven, dan stopt
// de stream én is wat al gestreamd was als partial opgeslagen.
func TestTurn_WriteFailsMidStream_PersistsPartial(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{
		{Content: "eerste"},
		{Content: "tweede"},
		{Content: "derde", FinishReason: "stop"},
	}}})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	w := newFailWriter(1) // alleen het eerste tekstframe komt door
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/turn",
		turnBody(t, turnRequest{PigeonID: "individualist", Message: "hoi"}))
	serveTo(t, m, w, req)

	if !strings.Contains(w.bodyString(), "eerste") {
		t.Fatalf("eerste frame ontbreekt in %q", w.bodyString())
	}
	if strings.Contains(w.bodyString(), "tweede") {
		t.Errorf("na de schrijffout mag er niets meer geschreven zijn: %q", w.bodyString())
	}

	stored := repo.messagesWithRole("assistant")
	if len(stored) != 1 {
		t.Fatalf("opgeslagen assistantberichten = %d, want 1 (partial mag niet verloren gaan)", len(stored))
	}
	assistant := stored[0]
	if !assistant.Partial {
		t.Errorf("afgebroken stream moet als partial opgeslagen worden")
	}
	// Alles wat het model tot de breuk produceerde blijft bewaard (de duif
	// heeft het gezegd, ook al haalde het laatste stuk de client niet meer);
	// wat na de breuk zou komen is nooit opgehaald.
	if assistant.Content != "eerstetweede" {
		t.Errorf("opgeslagen content = %q, want %q", assistant.Content, "eerstetweede")
	}
	if strings.Contains(assistant.Content, "derde") {
		t.Errorf("na de breuk mag er niets meer uit de stream gelezen zijn: %q", assistant.Content)
	}
}

// Een gevallen embedder of kennisbank is een storing, geen kennisgat: de duif
// doet geen LLM-beurt en de client krijgt het 503-contract met de eigen
// reason-code. Vermomd als gap zou de gebruiker denken dat het onderzoek
// ontbreekt, terwijl er alleen niet gezocht is.
func TestTurn_KBUnavailable_503WithReasonCode(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m *Module)
	}{
		{"embedder plat", func(m *Module) {
			m.embedder = failingEmbedder{err: errors.New("test: embedder plat")}
			m.knowledge = &fakeKnowledge{}
		}},
		{"kennisbank plat", func(m *Module) {
			m.embedder = &llmtesting.MockEmbedder{}
			m.knowledge = &fakeKnowledge{err: errors.New("test: kb plat")}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := llmtesting.NewMockService()
			m, _ := newTestModule(t, func(m *Module) {
				m.llm = mock
				tc.mutate(m)
			})
			// De 503-vóór-de-stream is het pipeline-contract: daar zoekt de
			// beurt vóór de headers. Op de facilitator zoekt ask_pigeon en is
			// een gevallen kennisbank een tool-fout (agent_kb_test.go).
			usePipeline(t, m, "biological")

			rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Herkennen duiven mensen?"}, false)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "event-stream") {
				t.Errorf("een storing vóór de stream mag geen SSE-respons zijn (content-type %q)", ct)
			}
			got := decodeError(t, rec.Body.String())
			if got.Error != ReasonKBUnavailable {
				t.Errorf("error = %q, want %q", got.Error, ReasonKBUnavailable)
			}
			if got.Message["en"] == "" || got.Message["nl"] == "" {
				t.Errorf("melding moet tweetalig zijn: %+v", got.Message)
			}
			if strings.Contains(rec.Body.String(), "plat") {
				t.Errorf("interne fouttekst lekt naar de wire: %s", rec.Body.String())
			}
			if len(mock.Calls) != 0 {
				t.Errorf("zonder kennisbank hoort er geen LLM-beurt te zijn (%d aanroepen)", len(mock.Calls))
			}
		})
	}
}

// Lege berichten uit de historie gaan niet mee de LLM in: providers achter de
// proxy weigeren messages zonder content.
func TestPrepareTurn_SkipsEmptyHistoryMessages(t *testing.T) {
	m, repo := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	usePipeline(t, m, "individualist")
	if err := repo.CreateSession(t.Context(), ChatSession{ID: "s1", UserID: testUser.ID, PigeonID: "individualist", Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []ChatMessage{
		{ID: "m1", SessionID: "s1", Role: "user", Content: "eerdere vraag"},
		{ID: "m2", SessionID: "s1", Role: "assistant", Content: ""},
	} {
		if err := repo.AppendMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}

	pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{SessionID: "s1", PigeonID: "individualist", Message: "nieuw"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	for _, msg := range pt.messages {
		if msg.Content == "" {
			t.Fatalf("lege message in de LLM-context: %+v", pt.messages)
		}
	}
	if len(pt.messages) != 3 { // system + eerdere vraag + nieuw
		t.Errorf("messages = %d, want 3 (system, historie, nieuw)", len(pt.messages))
	}
}

// Contractgarantie, buffer-randgeval: staan er na een client-disconnect nog
// chunks in de kanaalbuffer, dan stopt de pomp alsnog direct — hij mag ze niet
// leegdraaien en zo een zombie-stream in stand houden. Het `select` in pumpTurn
// kiest bij twee klaarstaande takken willekeurig, dus deze garantie wordt
// herhaald gecontroleerd.
func TestPumpTurn_DisconnectWithBufferedChunks_StopsImmediately(t *testing.T) {
	const herhalingen = 16
	m, repo := newTestModule(t, nil)
	pt := &preparedTurn{session: ChatSession{ID: "s1"}, status: KBPersonaOnly}

	for i := range herhalingen {
		ctx, cancel := context.WithCancel(t.Context())
		ch := make(chan llm.StreamChunk, 8)
		for j := 0; j < 8; j++ {
			ch <- llm.StreamChunk{Content: "roekoe"}
		}
		close(ch)
		cancel() // client weg vóór de eerste pomp-iteratie

		var emitted []string
		m.pumpTurn(ctx, pt, ch, func(frame any) error {
			if tf, ok := frame.(textFrame); ok {
				emitted = append(emitted, tf.Delta)
			}
			return nil
		})
		if len(emitted) != 0 {
			t.Fatalf("herhaling %d: %d tekstframes gestreamd na disconnect, want 0", i, len(emitted))
		}
	}
	if got := len(repo.messagesWithRole("assistant")); got != 0 {
		t.Errorf("er is niets gestreamd, dus er mag geen (leeg) assistantbericht zijn; kreeg %d", got)
	}
}
