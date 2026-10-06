package duif

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func comparisonFixture(t *testing.T) (*Module, *fakeRepo) {
	t.Helper()
	m, repo := newTestModule(t, nil)
	ctx := t.Context()
	_ = repo.CreateSession(ctx, ChatSession{ID: "origin", UserID: testUser.ID, PigeonID: "biological", Lang: "nl"})
	_ = repo.CreateSession(ctx, ChatSession{ID: "private", UserID: "another-user", PigeonID: "biological", Lang: "nl"})
	// Het brongesprek is de gewone gang van zaken: een eerdere beurt, dan de
	// vraag met haar eigen antwoordbeurt (kader, duif, bemiddeling), daarna nog
	// een beurt die niet meer meereist, en tot slot een vraag die nog geen
	// antwoord kreeg.
	//
	// "earlier-answer" is een rij van vóór de personakolom: geen persona, geen
	// kind. Zo'n antwoord moet de kopie stempelen met de duif van het
	// brongesprek, anders leest het nieuwe gesprek het als woorden van de duif
	// die daar op het scherm staat.
	for _, message := range []ChatMessage{
		{ID: "earlier", SessionID: "origin", Role: "user", Content: "We bespreken een stationsplein."},
		{ID: "framing", SessionID: "origin", Role: "assistant", Persona: PersonaFacilitator, Content: "Ik geef de duif het woord."},
		{ID: "earlier-answer", SessionID: "origin", Role: "assistant", Content: "Een eerder duifantwoord."},
		{ID: "question", SessionID: "origin", Role: "user", Content: "Wat betekent een nieuwe nestplek?"},
		{ID: "framing-2", SessionID: "origin", Role: "assistant", Persona: PersonaFacilitator, Content: "Ik leg haar de nestplek voor."},
		// Related staat aan: dit antwoord kwam uit de brede zoekactie ná een
		// kennisgat. Een kopie daarvan is nog steeds een antwoord uit
		// grenzend onderzoek, dus de markering hoort mee te reizen — anders
		// droeg alleen het brongesprek haar en las de kopie als volle dekking.
		{ID: "answer", SessionID: "origin", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer,
			Content: "Het antwoord op de vraag [1].", KBStatus: string(KBSources), Sources: comparisonSources,
			Related: true, Audit: &AnswerAudit{Status: "verified"}},
		{ID: "mediation", SessionID: "origin", Role: "assistant", Persona: PersonaFacilitator, Content: "Dit raakt de mensen aan tafel zo."},
		{ID: "later", SessionID: "origin", Role: "user", Content: "Een latere vraag."},
		{ID: "later-answer", SessionID: "origin", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer, Content: "Een later antwoord."},
		{ID: "unanswered", SessionID: "origin", Role: "user", Content: "En hoe zit dat met het dak?"},
		{ID: "private-question", SessionID: "private", Role: "user", Content: "Privé."},
	} {
		_ = repo.AppendMessage(ctx, message)
	}
	return m, repo
}

// comparisonSources is de bronnenset van het duifantwoord in de bron: hij moet
// het kopiëren ongewijzigd overleven, want een citatienummer zonder zijn bron
// is geen onderbouwing meer.
var comparisonSources = []Source{{Number: 1, Type: "destillaat", ID: "destillaat:9f1c", Text: "Duiven nestelen op richels."}}

func postComparison(t *testing.T, m *Module, req comparisonRequest) *httptest.ResponseRecorder {
	t.Helper()
	if req.RequestID == "" {
		req.RequestID = newID()
	}
	body, _ := json.Marshal(req)
	return serve(t, m, httptest.NewRequest(http.MethodPost, "/v1/chat/comparisons", bytes.NewReader(body)), false)
}

// Eén klik neemt het gesprek mee tot en met de antwoordbeurt op de vraag: de
// eigen berichten, de eerdere duifantwoorden, de vraag zelf en het antwoord dat
// zij al kreeg. De facilitator en alles van latere beurten blijven achter, en
// het gekopieerde antwoord houdt zijn auteur, bronnen en dekkingslabel.
func TestComparison_CopiesTheConversationUpToAndIncludingTheAnswerTurn(t *testing.T) {
	m, repo := comparisonFixture(t)
	rec := postComparison(t, m, comparisonRequest{SourceSessionID: "origin", QuestionID: "question", PigeonID: "individualist"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comparison: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session ChatSession `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	session, err := repo.GetSession(t.Context(), out.Session.ID, testUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Lang != "nl" || session.PigeonID != "individualist" || session.Comparison.QuestionID != "question" ||
		session.Comparison.PigeonID != "biological" || session.Comparison.OriginalQuestion != "Wat betekent een nieuwe nestplek?" {
		t.Fatalf("origin/language/author lost: %+v", session)
	}
	// De herkomst noemt precies wat er meereisde, in leesvolgorde.
	want := []string{"earlier", "earlier-answer", "question", "answer"}
	if !reflect.DeepEqual(session.Comparison.ContextMessageIDs, want) {
		t.Fatalf("origin does not name what travelled: %+v", session.Comparison.ContextMessageIDs)
	}
	messages, _ := repo.ListMessages(t.Context(), session.ID, 0)
	if len(messages) != len(want) {
		t.Fatalf("copied conversation: %+v", messages)
	}
	for i, message := range messages {
		if message.ID == want[i] {
			t.Fatalf("copied row kept the source identity: %+v", message)
		}
	}
	if messages[0].Role != "user" || messages[0].Content != "We bespreken een stationsplein." || messages[0].Persona != "" {
		t.Fatalf("own message not copied cleanly: %+v", messages[0])
	}
	// Het antwoord van vóór de personakolom draagt in de kopie de duif van het
	// brongesprek: zonder dat stempel valt de weergave terug op de duif van dít
	// gesprek en krijgt zij andermans woorden toegeschreven.
	if messages[1].Role != "assistant" || messages[1].Persona != "biological" || messages[1].Content != "Een eerder duifantwoord." {
		t.Fatalf("answer from before the persona column lost its pigeon: %+v", messages[1])
	}
	if messages[2].Role != "user" || messages[2].Content != "Wat betekent een nieuwe nestplek?" {
		t.Fatalf("the question itself did not travel: %+v", messages[2])
	}
	answer := messages[3]
	if answer.Role != "assistant" || answer.Persona != "biological" || answer.Kind != KindPigeonAnswer ||
		answer.Content != "Het antwoord op de vraag [1]." || answer.KBStatus != string(KBSources) ||
		!reflect.DeepEqual(answer.Sources, comparisonSources) {
		t.Fatalf("copied answer lost its author or sources: %+v", answer)
	}
	if !answer.Related {
		t.Fatalf("de kopie verloor de herkomst van haar bronnen: %+v", answer)
	}
	// Het brondekkingslabel reist niet mee. Het bronantwoord in de fixture
	// draagt er een, zoals een oudere rij; de kopie is een rij van vandaag, en
	// die wordt niet meer gelabeld.
	if answer.Audit != nil {
		t.Fatalf("de kopie kreeg een label dat vandaag niemand meer maakt: %+v", answer.Audit)
	}
	if answer.Question != "" {
		t.Fatalf("copied answer kept a foreign facilitator question: %+v", answer)
	}
	for _, message := range messages {
		if message.Persona == PersonaFacilitator || strings.Contains(message.Content, "later") || strings.Contains(message.Content, "Een later") {
			t.Fatalf("facilitator or a later turn travelled along: %+v", message)
		}
	}
	if _, err := repo.GetSession(t.Context(), session.ID, "another-user"); err != ErrSessionNotFound {
		t.Fatal("comparison not user scoped")
	}
	original, _ := repo.ListMessages(t.Context(), "origin", 0)
	if len(original) != 10 {
		t.Fatal("source conversation changed")
	}
}

// Vergelijken op een latere vraag neemt het hele gesprek ervóór mee: de eerdere
// beurten, die vraag zelf en het antwoord dat zij al kreeg.
func TestComparison_CopiesEveryEarlierTurnBeforeALaterQuestion(t *testing.T) {
	session := comparisonOf(t, "origin", "later", "individualist")
	want := []string{"earlier", "earlier-answer", "question", "answer", "later", "later-answer"}
	if !reflect.DeepEqual(session.Comparison.ContextMessageIDs, want) {
		t.Fatalf("wrong scope for a later question: %+v", session.Comparison.ContextMessageIDs)
	}
}

// Een vraag die nog geen antwoord kreeg reist zonder antwoord mee: de kopie
// eindigt bij de vraag zelf, en de tweede duif is de eerste die haar
// beantwoordt.
func TestComparison_CopiesAQuestionThatWasNeverAnswered(t *testing.T) {
	m, repo := comparisonFixture(t)
	session := comparisonIn(t, m, repo, "origin", "unanswered", "individualist")
	want := []string{"earlier", "earlier-answer", "question", "answer", "later", "later-answer", "unanswered"}
	if !reflect.DeepEqual(session.Comparison.ContextMessageIDs, want) {
		t.Fatalf("wrong scope for an unanswered question: %+v", session.Comparison.ContextMessageIDs)
	}
	messages, _ := repo.ListMessages(t.Context(), session.ID, 0)
	last := messages[len(messages)-1]
	if len(messages) != len(want) || last.Role != "user" || last.Content != "En hoe zit dat met het dak?" {
		t.Fatalf("the copy did not end at the unanswered question: %+v", messages)
	}
}

// comparisonOf voert één vergelijking uit op het standaardbrongesprek en geeft
// het opgeslagen doelgesprek terug; comparisonIn doet hetzelfde op een fixture
// die de test zelf al in handen heeft.
func comparisonOf(t *testing.T, source, question, target string) *ChatSession {
	t.Helper()
	m, repo := comparisonFixture(t)
	return comparisonIn(t, m, repo, source, question, target)
}

func comparisonIn(t *testing.T, m *Module, repo *fakeRepo, source, question, target string) *ChatSession {
	t.Helper()
	rec := postComparison(t, m, comparisonRequest{SourceSessionID: source, QuestionID: question, PigeonID: target})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comparison: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session ChatSession `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	session, err := repo.GetSession(t.Context(), out.Session.ID, testUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// Een ketting: het brongesprek was zelf al een vergelijking, dus er staat een
// antwoord van een derde duif in. Dat antwoord houdt zíjn persona en wordt niet
// alsnog aan de duif van dit gesprek toegeschreven.
func TestComparison_ChainedCopyKeepsEveryPigeonWithItsOwnAnswer(t *testing.T) {
	m, repo := newTestModule(t, nil)
	ctx := t.Context()
	_ = repo.CreateSession(ctx, ChatSession{ID: "branch", UserID: testUser.ID, PigeonID: "individualist", Lang: "nl"})
	for _, message := range []ChatMessage{
		{ID: "copied-question", SessionID: "branch", Role: "user", Content: "De vraag."},
		{ID: "third-voice", SessionID: "branch", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer, Content: "Woorden van de kennispartner."},
		{ID: "own-question", SessionID: "branch", Role: "user", Content: "De vraag."},
		{ID: "own-answer", SessionID: "branch", Role: "assistant", Persona: "individualist", Kind: KindPigeonAnswer, Content: "Woorden van de individualist."},
	} {
		_ = repo.AppendMessage(ctx, message)
	}
	rec := postComparison(t, m, comparisonRequest{SourceSessionID: "branch", QuestionID: "own-question", PigeonID: "biological"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comparison: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session ChatSession `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	messages, _ := repo.ListMessages(t.Context(), out.Session.ID, 0)
	if len(messages) != 4 || messages[1].Persona != "biological" || messages[3].Persona != "individualist" {
		t.Fatalf("chained copy mixed the voices: %+v", messages)
	}
}

// Een lege rij (disconnect vóór de eerste chunk) reist niet mee, en de
// herkomstlijst blijft één-op-één met de gekopieerde rijen.
func TestComparison_SkipsEmptyRowsAndKeepsTheOriginListAligned(t *testing.T) {
	m, repo := newTestModule(t, nil)
	ctx := t.Context()
	_ = repo.CreateSession(ctx, ChatSession{ID: "gaps", UserID: testUser.ID, PigeonID: "biological", Lang: "en"})
	for _, message := range []ChatMessage{
		{ID: "kept-1", SessionID: "gaps", Role: "user", Content: "First."},
		{ID: "empty-answer", SessionID: "gaps", Role: "assistant", Persona: "biological", Content: ""},
		{ID: "kept-2", SessionID: "gaps", Role: "user", Content: "The question."},
		{ID: "empty-again", SessionID: "gaps", Role: "assistant", Persona: "biological", Content: ""},
		{ID: "kept-3", SessionID: "gaps", Role: "assistant", Persona: "biological", Content: "An answer."},
	} {
		_ = repo.AppendMessage(ctx, message)
	}
	rec := postComparison(t, m, comparisonRequest{SourceSessionID: "gaps", QuestionID: "kept-2", PigeonID: "individualist"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comparison: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session ChatSession `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	session, _ := repo.GetSession(t.Context(), out.Session.ID, testUser.ID)
	ids := session.Comparison.ContextMessageIDs
	messages, _ := repo.ListMessages(t.Context(), out.Session.ID, 0)
	if !reflect.DeepEqual(ids, []string{"kept-1", "kept-2", "kept-3"}) || len(messages) != len(ids) {
		t.Fatalf("empty rows broke the alignment: ids=%+v rows=%+v", ids, messages)
	}
	for i, want := range []string{"First.", "The question.", "An answer."} {
		if messages[i].Content != want {
			t.Fatalf("row %d: %+v", i, messages[i])
		}
	}
}

func TestComparison_RejectsPrivateAssistantOrUnknownQuestions(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		source, question, target string
		status                   int
	}{
		{"private source", "private", "private-question", "individualist", 404},
		{"meeting id", "meeting-session", "question", "individualist", 404},
		{"private question", "origin", "private-question", "individualist", 400},
		{"assistant question", "origin", "answer", "individualist", 400},
		{"facilitator question", "origin", "framing", "individualist", 400},
		{"same pigeon", "origin", "question", "biological", 400},
		{"signaling", "origin", "question", "nonverbal", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, repo := comparisonFixture(t)
			rec := postComparison(t, m, comparisonRequest{SourceSessionID: tc.source, QuestionID: tc.question, PigeonID: tc.target})
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			sessions, _ := repo.ListSessions(t.Context(), testUser.ID)
			if len(sessions) != 1 {
				t.Fatalf("rejected comparison created session: %+v", sessions)
			}
		})
	}
}

// Een onbekend veld in de body is een oude client, geen stille selectie: de
// server accepteert hem niet en maakt niets aan.
func TestComparison_RejectsAnUnknownSelectionField(t *testing.T) {
	m, repo := comparisonFixture(t)
	body := `{"requestId":"` + newID() + `","sourceSessionId":"origin","questionId":"question","pigeonId":"individualist","contextMessageIds":["earlier"]}`
	rec := serve(t, m, httptest.NewRequest(http.MethodPost, "/v1/chat/comparisons", strings.NewReader(body)), false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stale selection accepted: %d %s", rec.Code, rec.Body.String())
	}
	sessions, _ := repo.ListSessions(t.Context(), testUser.ID)
	if len(sessions) != 1 {
		t.Fatalf("rejected comparison created session: %+v", sessions)
	}
}

func TestAnswerProvenance_OnlyNewAnswersReceiveSnapshot(t *testing.T) {
	m, repo := newTestModule(t, nil)
	p, _ := m.personas.Get("biological")
	v := m.chatProvenance(t.Context(), p.ID)
	if v.ContractVersion == "" || v.PromptVersion == "" || v.PersonaVersion == "" {
		t.Fatalf("missing version: %+v", v)
	}
	first := v.PromptVersion
	p.prompts["en"] += "\nA changed instruction."
	if m.chatProvenance(t.Context(), p.ID).PromptVersion == first {
		t.Fatal("prompt edits need a new content hash")
	}
	ctx := context.WithValue(t.Context(), provenanceContextKey{}, v)
	_ = repo.AppendMessage(t.Context(), ChatMessage{ID: "legacy", SessionID: "s", Role: "assistant", Content: "Unchanged old answer"})
	_ = repo.AppendMessage(ctx, ChatMessage{ID: "user", SessionID: "s", Role: "user", Content: "Question"})
	_ = repo.AppendMessage(ctx, ChatMessage{ID: "new", SessionID: "s", Role: "assistant", Content: "New answer"})
	rows, _ := repo.ListMessages(t.Context(), "s", 0)
	if rows[0].Provenance != nil || rows[1].Provenance != nil || rows[2].Provenance == nil {
		t.Fatalf("incorrect provenance stamping: %+v", rows)
	}
	if rows[0].Content != "Unchanged old answer" {
		t.Fatal("legacy content changed")
	}
}

func TestPostgresComparison_AtomicCopyAndDeletedOrigin(t *testing.T) {
	repo, pool := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	source := ChatSession{ID: newID(), UserID: "comparison-owner", PigeonID: "biological", Lang: "en", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, source); err != nil {
		t.Fatal(err)
	}
	question := ChatMessage{ID: newID(), SessionID: source.ID, Role: "user", Content: "Original", CreatedAt: now}
	if err := repo.AppendMessage(ctx, question); err != nil {
		t.Fatal(err)
	}
	target := ChatSession{ID: newID(), UserID: source.UserID, PigeonID: "individualist", Lang: "en", CreatedAt: now, UpdatedAt: now, Comparison: &ComparisonOrigin{SessionID: source.ID, QuestionID: question.ID, OriginalQuestion: question.Content}}
	bad := ChatMessage{ID: question.ID, SessionID: target.ID, Role: "user", Content: "duplicate id", CreatedAt: now}
	if _, err := repo.CreateComparison(ctx, target, []ChatMessage{bad}); err == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err := repo.GetSession(ctx, target.ID, target.UserID); err != ErrSessionNotFound {
		t.Fatal("failed comparison left a session")
	}
	good := bad
	good.ID = newID()
	good.Content = "Copied user context"
	// De kopie draagt ook het antwoord van de eerste duif: persona en bronnen
	// moeten de jsonb-heenreis overleven.
	answer := ChatMessage{ID: newID(), SessionID: target.ID, Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer,
		Content: "Copied pigeon answer [1].", KBStatus: string(KBSources), Sources: comparisonSources, CreatedAt: now}
	if _, err := repo.CreateComparison(ctx, target, []ChatMessage{good, answer}); err != nil {
		t.Fatal(err)
	}
	copied, err := repo.ListMessages(ctx, target.ID, 0)
	if err != nil || len(copied) != 2 {
		t.Fatalf("copied conversation: %+v %v", copied, err)
	}
	if copied[1].Persona != "biological" || copied[1].Kind != KindPigeonAnswer || copied[1].KBStatus != string(KBSources) ||
		!reflect.DeepEqual(copied[1].Sources, comparisonSources) {
		t.Fatalf("copied answer lost its author or sources: %+v", copied[1])
	}
	if copied[1].Audit != nil {
		t.Fatalf("een nieuw geschreven rij kreeg een brondekkingslabel: %+v", copied[1].Audit)
	}
	v := &AnswerProvenance{ContractVersion: "test-contract", PromptVersion: "test-prompt", KBState: "versioned", KBVersion: "test-kb"}
	if err := repo.AppendMessage(context.WithValue(ctx, provenanceContextKey{}, v), ChatMessage{ID: newID(), SessionID: target.ID, Role: "assistant", Content: "Answer", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListMessages(ctx, target.ID, 0)
	if err != nil || len(rows) != 3 || rows[2].Provenance == nil || rows[2].Provenance.KBVersion != "test-kb" {
		t.Fatalf("provenance roundtrip: %+v %v", rows, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM duif.chat_sessions WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSession(ctx, target.ID, target.UserID)
	if err != nil || got.Comparison != nil {
		t.Fatalf("deleted origin still exposed: %+v %v", got, err)
	}
	rows, err = repo.ListMessages(ctx, target.ID, 0)
	if err != nil || len(rows) != 3 || rows[0].Content != "Copied user context" || rows[1].Persona != "biological" {
		t.Fatal("explicit target content was changed by origin deletion")
	}
}

func TestPostgresProvenanceMigration_PreservesLegacyAnswer(t *testing.T) {
	_, pool := pgTestRepo(t)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	down, err := fs.ReadFile(migrationsFS, "migrations/0003_chat_provenance.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	sid, mid := newID(), newID()
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_sessions(id,user_id,pigeon_id,lang) VALUES($1,'legacy-owner','unknown-legacy-pigeon','en')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_messages(id,session_id,role,content) VALUES($1,$2,'assistant','The unchanged original answer [1].')`, mid, sid); err != nil {
		t.Fatal(err)
	}
	up, err := fs.ReadFile(migrationsFS, "migrations/0003_chat_provenance.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var content string
	var provenance []byte
	if err = tx.QueryRow(ctx, `SELECT content,provenance FROM duif.chat_messages WHERE id=$1`, mid).Scan(&content, &provenance); err != nil {
		t.Fatal(err)
	}
	if content != "The unchanged original answer [1]." || provenance != nil {
		t.Fatalf("migration fabricated provenance or changed legacy content: %q %s", content, provenance)
	}
}
