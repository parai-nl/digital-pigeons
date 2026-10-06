package duif

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestComparison_RetryAfterLostResponseIsIdempotentAndQuestionBound(t *testing.T) {
	m, repo := comparisonFixture(t)
	req := comparisonRequest{RequestID: newID(), SourceSessionID: "origin", QuestionID: "question", PigeonID: "individualist"}
	first := postComparison(t, m, req) // The client may never receive this successful response.
	if first.Code != 201 {
		t.Fatal(first.Body.String())
	}
	retry := postComparison(t, m, req)
	var a, b struct {
		Session ChatSession `json:"session"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if retry.Code != 201 || a.Session.ID != b.Session.ID {
		t.Fatalf("retry created another target: %s / %s", first.Body.String(), retry.Body.String())
	}
	messages, _ := repo.ListMessages(t.Context(), b.Session.ID, 0)
	if len(messages) != 4 || messages[0].Content != "We bespreken een stationsplein." {
		t.Fatalf("copied context duplicated: %+v", messages)
	}
	sessions, _ := repo.ListSessions(t.Context(), testUser.ID)
	if len(sessions) != 2 {
		t.Fatalf("expected source and one target: %+v", sessions)
	}
	req.QuestionID = "earlier"
	conflict := postComparison(t, m, req)
	if conflict.Code != 409 {
		t.Fatalf("same request key accepted another question: %d %s", conflict.Code, conflict.Body.String())
	}
	if _, err := repo.GetComparison(t.Context(), "another-user", req.RequestID, a.Session.ComparisonRequestHash); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("request key escaped owner scope")
	}
	for _, private := range []string{"comparisonRequestHash", "comparisonRequestId", "userId", "user_id"} {
		if strings.Contains(first.Body.String(), private) {
			t.Fatalf("internal identity on wire: %s", private)
		}
	}
}

func TestComparison_RequiresRequestIDBeforeCreatingAnything(t *testing.T) {
	m, repo := comparisonFixture(t)
	rec := serve(t, m, httptest.NewRequest(http.MethodPost, "/v1/chat/comparisons", strings.NewReader(`{"sourceSessionId":"origin","questionId":"question","pigeonId":"individualist"}`)), false)
	if rec.Code != 400 {
		t.Fatalf("missing idempotency identity accepted: %d", rec.Code)
	}
	sessions, _ := repo.ListSessions(t.Context(), testUser.ID)
	if len(sessions) != 1 {
		t.Fatalf("invalid request created target: %+v", sessions)
	}
}

func TestPostgresComparison_ConcurrentRetriesCommitOneCompleteTarget(t *testing.T) {
	repo, pool := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	source := ChatSession{ID: newID(), UserID: "retry-owner", PigeonID: "biological", Lang: "nl", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, source); err != nil {
		t.Fatal(err)
	}
	requestID := newID()
	const workers = 8
	type result struct {
		session *ChatSession
		err     error
	}
	results := make(chan result, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			target := ChatSession{ID: newID(), UserID: source.UserID, PigeonID: "individualist", Lang: "nl", CreatedAt: now, UpdatedAt: now, ComparisonRequestID: requestID, ComparisonRequestHash: "one-selection", Comparison: &ComparisonOrigin{SessionID: source.ID, OriginalQuestion: "Dezelfde vraag", ContextMessageIDs: []string{"earlier"}}}
			got, err := repo.CreateComparison(ctx, target, []ChatMessage{{ID: newID(), SessionID: target.ID, Role: "user", Content: "Selected own context", CreatedAt: now}})
			results <- result{got, err}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	id := ""
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if id == "" {
			id = result.session.ID
		}
		if result.session.ID != id {
			t.Fatalf("concurrent retry created multiple targets: %q/%q", id, result.session.ID)
		}
	}
	rows, err := repo.ListMessages(ctx, id, 0)
	if err != nil || len(rows) != 1 || rows[0].Content != "Selected own context" {
		t.Fatalf("partial or duplicate context: %+v %v", rows, err)
	}
	if _, err := repo.GetComparison(ctx, "foreign-owner", requestID, "one-selection"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("request lookup not user scoped")
	}
	if _, err := repo.GetComparison(ctx, source.UserID, requestID, "different-selection"); !errors.Is(err, ErrComparisonConflict) {
		t.Fatal("request lookup accepted changed selection")
	}
	// Deleting the origin clears private links but does not destroy retry identity.
	if _, err := pool.Exec(ctx, `DELETE FROM duif.chat_sessions WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.GetComparison(ctx, source.UserID, requestID, "one-selection")
	if err != nil || recovered.ID != id || recovered.Comparison != nil {
		t.Fatalf("deleted origin retry: %+v %v", recovered, err)
	}
}

func TestPostgresSessionTitle_UsesFirstHumanQuestionAndComparisonQuestion(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	source := ChatSession{ID: newID(), UserID: "title-owner", PigeonID: "biological", Lang: "nl", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, source); err != nil {
		t.Fatal(err)
	}
	for _, m := range []ChatMessage{
		{ID: newID(), SessionID: source.ID, Role: "assistant", Content: "Never use this generated label", CreatedAt: now},
		{ID: newID(), SessionID: source.ID, Role: "user", Content: "  Een plein\n met  duiven 🕊️  ", CreatedAt: now},
		{ID: newID(), SessionID: source.ID, Role: "user", Content: "A different later question", CreatedAt: now},
	} {
		if err := repo.AppendMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := repo.ListSessions(ctx, source.UserID)
	if err != nil || len(sessions) != 1 || sessions[0].Title != "Een plein met duiven 🕊️" {
		t.Fatalf("first own question not preserved as label: %+v %v", sessions, err)
	}
	target := ChatSession{ID: newID(), UserID: source.UserID, PigeonID: "individualist", Lang: "nl", CreatedAt: now, UpdatedAt: now, ComparisonRequestID: newID(), ComparisonRequestHash: "title", Comparison: &ComparisonOrigin{SessionID: source.ID, OriginalQuestion: "Waarom juist daar?"}}
	if _, err := repo.CreateComparison(ctx, target, []ChatMessage{{ID: newID(), SessionID: target.ID, Role: "user", Content: "Earlier context must not replace the actual question title", CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSession(ctx, target.ID, target.UserID)
	if err != nil || got.Title != "Waarom juist daar?" {
		t.Fatalf("comparison title lost: %+v %v", got, err)
	}
}

func TestAnswerProvenance_EffectiveBehaviorChangesVersion(t *testing.T) {
	m, _ := newTestModule(t, nil)
	for _, directives := range []map[string]string{gapDirective, langDirective, citationInstruction, historyEvidenceDirective} {
		before := m.chatProvenance(t.Context(), "biological")
		original := directives["en"]
		directives["en"] = original + " An intentionally changed contract instruction."
		after := m.chatProvenance(t.Context(), "biological")
		directives["en"] = original
		if before.PromptVersion == after.PromptVersion {
			t.Fatal("effective instruction change retained prompt version")
		}
		if before.PromptVersion != m.chatProvenance(t.Context(), "biological").PromptVersion {
			t.Fatal("unchanged contract is not deterministic")
		}
	}
}

type sessionAckRecorder struct {
	*httptest.ResponseRecorder
	firstFlush func()
}

func (w *sessionAckRecorder) Flush() {
	if w.firstFlush != nil {
		f := w.firstFlush
		w.firstFlush = nil
		f()
	}
	w.ResponseRecorder.Flush()
}

func TestTurn_SessionAcknowledgedBeforeFirstModelRun(t *testing.T) {
	m, repo, mock := newAgentModule(t, nil)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/turn", strings.NewReader(`{"pigeonId":"individualist","message":"Keep this context","lang":"en"}`))
	// The standard test router attaches the authenticated user. Capture the first
	// flush at the HTTP seam, before any model output can be generated.
	recorder := &sessionAckRecorder{ResponseRecorder: httptest.NewRecorder()}
	flushed := false
	recorder.firstFlush = func() {
		flushed = true
		id := recorder.Header().Get("X-Duif-Session-ID")
		if id == "" {
			t.Fatal("first headers lack session acknowledgement")
		}
		rows, err := repo.ListMessages(t.Context(), id, 0)
		if err != nil || len(rows) != 1 || rows[0].Content != "Keep this context" {
			t.Fatalf("acknowledged before human message committed: %+v %v", rows, err)
		}
		if mock.CallCount() != 0 {
			t.Fatal("model ran before acknowledgement became available")
		}
	}
	serveTo(t, m, recorder, request)
	if !flushed {
		t.Fatal("stream headers were not flushed")
	}
}
