package duif

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// Persistentie en historie.
func TestTurn_SavesUserAndAssistantMessage(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "Roekoe!", FinishReason: "stop"}}}})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "Dag duif"}, false)
	frames := parseSSE(t, rec.Body.String())
	sid := frames[len(frames)-1]["sessionId"].(string)

	msgs, _ := repo.ListMessages(context.Background(), sid, 0)
	if len(msgs) != 2 {
		t.Fatalf("want 2 berichten (user+assistant), got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Dag duif" {
		t.Errorf("userbericht fout bewaard: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Roekoe!" || msgs[1].Partial {
		t.Errorf("assistantbericht fout bewaard: %+v", msgs[1])
	}
	if msgs[1].KBStatus != string(KBPersonaOnly) {
		t.Errorf("assistantbericht moet kbStatus dragen: %+v", msgs[1])
	}
	s, err := repo.GetSession(context.Background(), sid, testUser.ID)
	if err != nil || s.PigeonID != "individualist" {
		t.Errorf("sessie fout bewaard: %+v (%v)", s, err)
	}
}

func TestTurn_HistoryWindowFedToLLM(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "ok", FinishReason: "stop"}}}})
	m, repo := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.historyWindow = 4
	})
	usePipeline(t, m, "individualist")
	sid := "sess-hist"
	repo.CreateSession(context.Background(), ChatSession{ID: sid, UserID: testUser.ID, PigeonID: "individualist", CreatedAt: time.Now()})
	for i := 0; i < 6; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		repo.AppendMessage(context.Background(), ChatMessage{ID: newID(), SessionID: sid, Role: role, Content: "hist-" + string(rune('a'+i)), CreatedAt: time.Now()})
	}

	rec := postTurn(t, m, map[string]any{"sessionId": sid, "pigeonId": "individualist", "message": "nieuwste"}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := mock.Calls[0].Messages
	// system + 4 historie + nieuwste userbericht
	if len(got) != 6 {
		t.Fatalf("LLM kreeg %d messages, want 6 (system + venster 4 + nieuw)", len(got))
	}
	if got[1].Content != "hist-c" {
		t.Errorf("venster moet de láátste N dragen; eerste historie = %q, want hist-c", got[1].Content)
	}
	if got[5].Content != "nieuwste" || got[5].Role != "user" {
		t.Errorf("laatste message moet het nieuwe userbericht zijn: %+v", got[5])
	}
}

func TestSessions_UserScoped(t *testing.T) {
	mock := llmtesting.NewMockService()
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	repo.CreateSession(context.Background(), ChatSession{ID: "andermans", UserID: "user-2", PigeonID: "individualist"})

	// Turn tegen andermans sessie → 404.
	rec := postTurn(t, m, map[string]any{"sessionId": "andermans", "pigeonId": "individualist", "message": "hoi"}, false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("turn: status = %d, want 404", rec.Code)
	}
	// Berichten van andermans sessie → 404.
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/andermans/messages", nil)
	rec = serve(t, m, req, false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("messages: status = %d, want 404", rec.Code)
	}
	// Sessielijst bevat alleen eigen sessies.
	repo.CreateSession(context.Background(), ChatSession{ID: "eigen", UserID: testUser.ID, PigeonID: "individualist"})
	req = httptest.NewRequest(http.MethodGet, "/v1/chat/sessions", nil)
	rec = serve(t, m, req, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions: status = %d", rec.Code)
	}
	var out struct {
		Sessions []ChatSession `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].ID != "eigen" {
		t.Errorf("sessielijst moet user-scoped zijn: %+v", out.Sessions)
	}
	if strings.Contains(rec.Body.String(), "user-2") {
		t.Error("andermans data mag de wire niet bereiken")
	}
}

// Een sessie is aan één persona gebonden: een turn met een andere pigeonId
// zou de verkeerde persona de historie voeren en het gesprek in de opslag
// aan de verkeerde duif attribueren.
func TestTurn_PigeonMismatchWithSession_400(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "coo", FinishReason: "stop"}}}})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")
	repo.CreateSession(context.Background(), ChatSession{ID: "s-ind", UserID: testUser.ID, PigeonID: "individualist"})

	rec := postTurn(t, m, map[string]any{"sessionId": "s-ind", "pigeonId": "biological", "message": "hoi"}, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if msgs, _ := repo.ListMessages(context.Background(), "s-ind", 0); len(msgs) != 0 {
		t.Errorf("afgewezen turn mag niets aan de sessie toevoegen: %+v", msgs)
	}
	// Dezelfde persona als de sessie blijft gewoon werken.
	rec = postTurn(t, m, map[string]any{"sessionId": "s-ind", "pigeonId": "individualist", "message": "hoi"}, false)
	if rec.Code != http.StatusOK {
		t.Errorf("matchende pigeonId: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestSessionMessages_ReturnsHistory(t *testing.T) {
	m, repo := newTestModule(t, nil)
	repo.CreateSession(context.Background(), ChatSession{ID: "s1", UserID: testUser.ID, PigeonID: "individualist"})
	repo.AppendMessage(context.Background(), ChatMessage{ID: "m1", SessionID: "s1", Role: "user", Content: "vraag"})
	repo.AppendMessage(context.Background(), ChatMessage{ID: "m2", SessionID: "s1", Role: "assistant", Content: "antwoord", KBStatus: string(KBPersonaOnly)})

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/s1/messages", nil)
	rec := serve(t, m, req, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 2 || out.Messages[0].Content != "vraag" || out.Messages[1].Content != "antwoord" {
		t.Errorf("historie fout: %+v", out.Messages)
	}
}
