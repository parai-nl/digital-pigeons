package duif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// De Postgres-repotests draaien tegen een echte database: `make e2e` start een
// wegwerp-Postgres (pgvector) via docker en zet DUIF_TEST_DB_URL. Zonder die
// envvar slaan ze over, zodat `make test` docker-vrij blijft. De fake-repo in
// testsupport_test.go dekt het gedrag van de pipeline; deze tests dekken het
// contract van de echte implementatie: user-scoping, jsonb-round-trip,
// historievenster en de transactie rond bericht + sessie-timestamp.

var pgSchemaOnce sync.Once

// pgTestRepo levert een postgresRepo op een vers duif-schema.
func pgTestRepo(t *testing.T) (*postgresRepo, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DUIF_TEST_DB_URL")
	if url == "" {
		t.Skip("DUIF_TEST_DB_URL niet gezet — draai `make e2e` voor de Postgres-repotests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	var setupErr error
	pgSchemaOnce.Do(func() { setupErr = applyChatSchema(context.Background(), pool) })
	if setupErr != nil {
		t.Fatalf("schema opzetten: %v", setupErr)
	}
	return newPostgresRepo(pool), pool
}

// applyChatSchema zet het duif-schema op uit dezelfde migratiebundel die de
// module aan het framework meegeeft — de tests mogen geen tweede waarheid over
// de tabellen hebben.
func applyChatSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS duif CASCADE; CREATE SCHEMA duif;`); err != nil {
		return err
	}
	// Alle migraties op volgorde: de tests mogen geen tweede waarheid over de
	// tabellen hebben, en 0002 (kind/persona/question/seq) hoort erbij.
	namen, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(namen)
	for _, naam := range namen {
		up, err := fs.ReadFile(migrationsFS, naam)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(up)); err != nil {
			return fmt.Errorf("%s: %w", naam, err)
		}
	}
	return nil
}

// closedRepo is een repo waarvan de pool dicht is: elke query faalt. Zo zijn de
// foutpaden te raken zonder de database moedwillig te slopen.
func closedRepo(t *testing.T) *postgresRepo {
	t.Helper()
	url := os.Getenv("DUIF_TEST_DB_URL")
	if url == "" {
		t.Skip("DUIF_TEST_DB_URL niet gezet — draai `make e2e` voor de Postgres-repotests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	pool.Close()
	return newPostgresRepo(pool)
}

func TestPostgresRepo_SessionRoundTripIsUserScoped(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s := ChatSession{ID: newID(), UserID: "user-scope-a", PigeonID: "individualist", Lang: "nl", CreatedAt: now, UpdatedAt: now}

	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := repo.GetSession(ctx, s.ID, s.UserID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PigeonID != "individualist" || got.Lang != "nl" || got.UserID != s.UserID {
		t.Errorf("sessie kwam verminkt terug: %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("createdAt = %v, want %v", got.CreatedAt, now)
	}

	// Sessies zijn user-scoped: een andere gebruiker ziet hem niet, en het
	// verschil met "bestaat niet" lekt niet.
	if _, err := repo.GetSession(ctx, s.ID, "user-scope-b"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession van andermans sessie = %v, want ErrSessionNotFound", err)
	}
	if _, err := repo.GetSession(ctx, "bestaat-niet", s.UserID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession van een onbekende sessie = %v, want ErrSessionNotFound", err)
	}
}

func TestPostgresRepo_CreateSession_DuplicateIDFails(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	s := ChatSession{ID: newID(), UserID: "user-dup", PigeonID: "individualist", Lang: "en", CreatedAt: now, UpdatedAt: now}

	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	err := repo.CreateSession(ctx, s)
	if err == nil {
		t.Fatal("een tweede sessie met hetzelfde id moet falen")
	}
	if !strings.Contains(err.Error(), "duif:") {
		t.Errorf("fout = %q, moet als duif-fout gewrapt zijn", err)
	}
}

func TestPostgresRepo_ListSessions_NewestFirstAndUserScoped(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	base := time.Now().UTC()
	oud := ChatSession{ID: newID(), UserID: "user-list", PigeonID: "individualist", Lang: "en", CreatedAt: base.Add(-time.Hour), UpdatedAt: base.Add(-time.Hour)}
	nieuw := ChatSession{ID: newID(), UserID: "user-list", PigeonID: "biological", Lang: "en", CreatedAt: base, UpdatedAt: base}
	ander := ChatSession{ID: newID(), UserID: "user-ander", PigeonID: "individualist", Lang: "en", CreatedAt: base, UpdatedAt: base}
	for _, s := range []ChatSession{oud, nieuw, ander} {
		if err := repo.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	got, err := repo.ListSessions(ctx, "user-list")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("aantal sessies = %d, want 2 (alleen die van user-list)", len(got))
	}
	if got[0].ID != nieuw.ID || got[1].ID != oud.ID {
		t.Errorf("volgorde = %v, want nieuwste eerst", []string{got[0].ID, got[1].ID})
	}
}

func TestPostgresRepo_ListSessions_NoneIsEmpty(t *testing.T) {
	repo, _ := pgTestRepo(t)
	got, err := repo.ListSessions(t.Context(), "user-zonder-sessies")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("aantal sessies = %d, want 0", len(got))
	}
}

func TestPostgresRepo_MessagesRoundTripWithSources(t *testing.T) {
	repo, pool := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s := ChatSession{ID: newID(), UserID: "user-msg", PigeonID: "biological", Lang: "en", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	vraag := ChatMessage{ID: newID(), SessionID: s.ID, Role: "user", Content: "Wat vinden duiven van de kade?", CreatedAt: now}
	antwoord := ChatMessage{
		ID: newID(), SessionID: s.ID, Role: "assistant", Content: "Roekoe [1].",
		KBStatus: string(KBSources), Partial: true, CreatedAt: now.Add(time.Second),
		Sources: []Source{{Number: 1, Type: "standpunt", Text: "duiven en kades",
			Ref: SourceRef{File: "destillaat/kade.md", DOI: "10.1234/duif"}, Perspective: "ecologie", Theme: "ruimte"}},
	}
	for _, m := range []ChatMessage{vraag, antwoord} {
		if err := repo.AppendMessage(ctx, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	got, err := repo.ListMessages(ctx, s.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("aantal berichten = %d, want 2", len(got))
	}
	if got[0].Role != "user" || got[1].Role != "assistant" {
		t.Errorf("berichten moeten oplopend op tijd staan, kreeg %q, %q", got[0].Role, got[1].Role)
	}
	if len(got[1].Sources) != 1 || got[1].Sources[0].Ref.DOI != "10.1234/duif" || got[1].Sources[0].Theme != "ruimte" {
		t.Errorf("bronnen kwamen niet heel terug: %+v", got[1].Sources)
	}
	if !got[1].Partial || got[1].KBStatus != string(KBSources) {
		t.Errorf("partial/kbStatus verloren: partial=%v kbStatus=%q", got[1].Partial, got[1].KBStatus)
	}
	if len(got[0].Sources) != 0 {
		t.Errorf("een userbericht heeft geen bronnen, kreeg %+v", got[0].Sources)
	}

	// AppendMessage tikt de sessie aan, zodat ListSessions op recentheid kan
	// sorteren.
	var updated time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM duif.chat_sessions WHERE id = $1`, s.ID).Scan(&updated); err != nil {
		t.Fatalf("updated_at lezen: %v", err)
	}
	if !updated.Equal(antwoord.CreatedAt) {
		t.Errorf("sessie.updated_at = %v, want %v (tijd van het laatste bericht)", updated, antwoord.CreatedAt)
	}
}

// Het historievenster levert de laatste N berichten, maar wél oplopend: de
// prompt-assemblage rekent op chronologie.
func TestPostgresRepo_ListMessages_LimitReturnsLastNAscending(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	s := ChatSession{ID: newID(), UserID: "user-window", PigeonID: "individualist", Lang: "en", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := range 5 {
		m := ChatMessage{ID: newID(), SessionID: s.ID, Role: "user", Content: string(rune('a' + i)), CreatedAt: now.Add(time.Duration(i) * time.Second)}
		if err := repo.AppendMessage(ctx, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	got, err := repo.ListMessages(ctx, s.ID, 2)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("aantal berichten = %d, want 2", len(got))
	}
	if got[0].Content != "d" || got[1].Content != "e" {
		t.Errorf("venster = %q,%q; want de laatste twee oplopend (d,e)", got[0].Content, got[1].Content)
	}
}

func TestPostgresRepo_AppendMessage_UnknownSessionFails(t *testing.T) {
	repo, _ := pgTestRepo(t)
	err := repo.AppendMessage(t.Context(), ChatMessage{
		ID: newID(), SessionID: "bestaat-niet", Role: "user", Content: "hoi", CreatedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("een bericht in een onbestaande sessie moet falen (foreign key)")
	}
	if !strings.Contains(err.Error(), "duif:") {
		t.Errorf("fout = %q, moet als duif-fout gewrapt zijn", err)
	}
}

// Corrupte bronnen-JSON is een leesfout, geen stilzwijgend leeg bronnenlijstje:
// anders zou een antwoord zijn onderbouwing verliezen zonder dat iemand het merkt.
func TestPostgresRepo_ListMessages_CorruptSourcesIsAnError(t *testing.T) {
	repo, pool := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	s := ChatSession{ID: newID(), UserID: "user-corrupt", PigeonID: "biological", Lang: "en", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO duif.chat_messages (id, session_id, role, content, kb_status, sources, partial, created_at)
		 VALUES ($1, $2, 'assistant', 'roekoe', 'sources', '{"geen":"lijst"}'::jsonb, false, $3)`,
		newID(), s.ID, now); err != nil {
		t.Fatalf("corrupte rij plaatsen: %v", err)
	}

	if _, err := repo.ListMessages(ctx, s.ID, 0); err == nil {
		t.Fatal("onleesbare bronnen moeten een fout geven")
	}
}

// Een onbereikbare database geeft nette, gewrapte fouten — geen paniek en geen
// half-gevulde resultaten.
func TestPostgresRepo_DatabaseUnavailable_WrapsEveryOperation(t *testing.T) {
	repo := closedRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()

	if err := repo.CreateSession(ctx, ChatSession{ID: "x", UserID: "u", PigeonID: "individualist", Lang: "en", CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Error("CreateSession moet falen op een dichte pool")
	}
	if _, err := repo.GetSession(ctx, "x", "u"); err == nil || errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession op een dichte pool = %v, want een echte fout (geen not-found)", err)
	}
	if _, err := repo.ListSessions(ctx, "u"); err == nil {
		t.Error("ListSessions moet falen op een dichte pool")
	}
	if err := repo.AppendMessage(ctx, ChatMessage{ID: "m", SessionID: "x", Role: "user", Content: "hoi", CreatedAt: now}); err == nil {
		t.Error("AppendMessage moet falen op een dichte pool")
	}
	if _, err := repo.ListMessages(ctx, "x", 0); err == nil {
		t.Error("ListMessages moet falen op een dichte pool")
	}
}

// Migratie 0002 — beide stemmen in één sessie, elk met hun eigen kind en
// persona, en de facilitator-vraag bij het duif-antwoord.
func TestPostgresRepo_MessageKindsAndPersonaRoundTrip(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	s := ChatSession{ID: newID(), UserID: "user-kinds", PigeonID: "individualist", Lang: "nl",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Alle drie op dezelfde tijdstempel: `created_at, id` zou hier een andere
	// volgorde kunnen geven dan de schrijfvolgorde — precies waarvoor seq er is.
	now := time.Now().UTC()
	for _, m := range []ChatMessage{
		{ID: newID(), SessionID: s.ID, Role: "user", Content: "Wat vindt zij van de Coolsingel?", CreatedAt: now},
		{ID: newID(), SessionID: s.ID, Role: "assistant", Content: "Die stoep is van mij.", CreatedAt: now,
			Kind: KindPigeonAnswer, Persona: "individualist", Question: "Wat vind jij van de Coolsingel?", KBStatus: string(KBPersonaOnly)},
		{ID: newID(), SessionID: s.ID, Role: "assistant", Content: "Zo denkt zij erover.", CreatedAt: now,
			Kind: KindText, Persona: PersonaFacilitator, KBStatus: string(KBPersonaOnly)},
	} {
		if err := repo.AppendMessage(ctx, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	got, err := repo.ListMessages(ctx, s.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("berichten = %d, want 3", len(got))
	}
	// Een bericht zonder kind komt als 'text' terug (kolomdefault).
	if got[0].Kind != KindText || got[0].Persona != "" || got[0].Question != "" {
		t.Errorf("userbericht = %+v", got[0])
	}
	if got[1].Kind != KindPigeonAnswer || got[1].Persona != "individualist" || got[1].Question != "Wat vind jij van de Coolsingel?" {
		t.Errorf("duif-bericht = %+v", got[1])
	}
	if got[2].Kind != KindText || got[2].Persona != PersonaFacilitator {
		t.Errorf("facilitator-bericht = %+v", got[2])
	}
	// seq is monotoon en volgt de schrijfvolgorde, ook bij gelijke tijdstempel.
	if !(got[0].Seq < got[1].Seq && got[1].Seq < got[2].Seq) {
		t.Errorf("seq = %d, %d, %d — want oplopend in schrijfvolgorde", got[0].Seq, got[1].Seq, got[2].Seq)
	}
	// Het historievenster leest dezelfde volgorde terug.
	venster, err := repo.ListMessages(ctx, s.ID, 2)
	if err != nil {
		t.Fatalf("ListMessages (venster): %v", err)
	}
	if len(venster) != 2 || venster[0].Kind != KindPigeonAnswer || venster[1].Persona != PersonaFacilitator {
		t.Errorf("venster = %+v, want de laatste twee in volgorde", venster)
	}
}

// De CHECK op kind is er om een typefout in de code hard te laten vallen in
// plaats van een onbekende soort in de tabel te laten glijden.
func TestPostgresRepo_UnknownKindIsRejected(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	s := ChatSession{ID: newID(), UserID: "user-kindcheck", PigeonID: "individualist", Lang: "en",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	err := repo.AppendMessage(ctx, ChatMessage{
		ID: newID(), SessionID: s.ID, Role: "assistant", Content: "x", CreatedAt: time.Now().UTC(), Kind: "gekwetter"})
	if err == nil {
		t.Fatal("een onbekend kind moet door de CHECK geweigerd worden")
	}
}

// Migration 0006 is additive: it adds the audit label column and nothing else.
// Rolling it back and forward around an existing answer must leave that
// answer's text untouched and its label NULL — "no audit applies", never "this
// answer failed a check".
func TestPostgresAuditMigration_IsAdditiveAndKeepsLegacyAnswers(t *testing.T) {
	_, pool := pgTestRepo(t)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	down, err := fs.ReadFile(migrationsFS, "migrations/0006_chat_audit.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("down: %v", err)
	}
	sid, mid := newID(), newID()
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_sessions(id,user_id,pigeon_id,lang) VALUES($1,'legacy-owner','biological','en')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_messages(id,session_id,role,content) VALUES($1,$2,'assistant','The unchanged original answer [1].')`, mid, sid); err != nil {
		t.Fatal(err)
	}
	up, err := fs.ReadFile(migrationsFS, "migrations/0006_chat_audit.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("up: %v", err)
	}

	var content string
	var audit []byte
	if err = tx.QueryRow(ctx, `SELECT content,audit FROM duif.chat_messages WHERE id=$1`, mid).Scan(&content, &audit); err != nil {
		t.Fatal(err)
	}
	if content != "The unchanged original answer [1]." || audit != nil {
		t.Fatalf("migration fabricated a label or changed legacy content: %q %s", content, audit)
	}
}

// De kolom blijft bestaan voor oudere rijen: een antwoord dat vroeger een
// brondekkingslabel kreeg leest nog steeds terug zoals het bewaard staat.
// Er wordt er geen meer geschreven — de repo heeft daar geen schrijver meer
// voor — maar historie mag niet stuklopen op een veld dat niemand meer vult.
//
// Terugleest, en daar houdt het op: de lezer krijgt het niet te zien en de
// client krijgt het niet toegestuurd. Het slot van deze test kijkt daarom ook
// naar de JSON die de REST-route van deze rij maakt.
func TestPostgresRepo_LegacyAuditLabelStillReadsBack(t *testing.T) {
	repo, pool := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s := ChatSession{ID: newID(), UserID: "user-audit", PigeonID: "biological", Lang: "en", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	msg := ChatMessage{ID: newID(), SessionID: s.ID, Role: "assistant", Content: "An answer with a marker [1].",
		KBStatus: string(KBSources), Sources: []Source{{Number: 1, Text: "Bron"}}, CreatedAt: now, Kind: KindPigeonAnswer, Persona: "biological"}
	if err := repo.AppendMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.ListMessages(ctx, s.ID, 0)
	if err != nil || len(stored) != 1 || stored[0].Audit != nil {
		t.Fatalf("een nieuw antwoord krijgt geen label meer: %+v %v", stored, err)
	}
	// Een rij zoals hij met brondekkingslabel in de database belandde.
	legacy := `{"status":"flagged","findings":[{"sentence":1,"text":"An answer with a marker [1].","reason":"unsupported_fact"}]}`
	if _, err := pool.Exec(ctx, `UPDATE duif.chat_messages SET audit=$2 WHERE id=$1`, msg.ID, []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.ListMessages(ctx, s.ID, 0)
	if err != nil || len(stored) != 1 {
		t.Fatalf("ListMessages: %+v %v", stored, err)
	}
	got := stored[0]
	if got.Content != msg.Content || got.KBStatus != msg.KBStatus || len(got.Sources) != 1 {
		t.Fatalf("het oude label veranderde het antwoord: %+v", got)
	}
	if got.Audit == nil || got.Audit.Status != AuditFlagged || len(got.Audit.Findings) != 1 || got.Audit.Findings[0].Reason != "unsupported_fact" {
		t.Fatalf("label round trip: %+v", got.Audit)
	}
	assertAuditStaysOffTheWire(t, got)
}

// assertAuditStaysOffTheWire toetst wat `json:"-"` op ChatMessage.Audit belooft:
// het bewaarde label blijft serverzijdig. De rij zelf reist gewoon.
func assertAuditStaysOffTheWire(t *testing.T, row ChatMessage) {
	t.Helper()
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if _, present := wire["audit"]; present {
		t.Fatalf("het bewaarde label ging alsnog de REST-lijn op: %s", raw)
	}
	if _, present := wire["findings"]; present {
		t.Fatalf("een bevinding ging alsnog de REST-lijn op: %s", raw)
	}
	if wire["content"] != row.Content || wire["kbStatus"] != row.KBStatus {
		t.Fatalf("de rij zelf reist niet meer mee: %s", raw)
	}
}

// Dezelfde belofte zonder database, zodat zij ook meeloopt in de gewone
// testronde: pgTestRepo slaat over zonder DUIF_TEST_DB_URL, en juist een veld
// dat niemand meer leest is er een dat ongemerkt terug de wire op glijdt.
func TestChatMessage_LegacyAuditLabelStaysOffTheWire(t *testing.T) {
	assertAuditStaysOffTheWire(t, ChatMessage{
		ID: "row", SessionID: "session", Role: "assistant", Kind: KindPigeonAnswer, Persona: "biological",
		Content: "An answer with a marker [1].", KBStatus: string(KBSources),
		Audit: &AnswerAudit{Status: AuditFlagged, Findings: []AuditFinding{
			{Sentence: 1, Text: "An answer with a marker [1].", Reason: "unsupported_fact"},
		}},
	})
}

// De verwante-markering moet de opslag heel doorkomen: zij is het
// enige wat na herladen nog vertelt dat deze bronnen uit de brede zoekactie
// kwamen en niet uit de gewone zoekopdracht. En zij moet standaard uit staan —
// een rij zonder markering is een rij met gewone treffers.
func TestPostgresRepo_RelatedMarkingRoundTrips(t *testing.T) {
	repo, _ := pgTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s := ChatSession{ID: newID(), UserID: "user-related", PigeonID: "biological", Lang: "nl", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	grenzend := ChatMessage{
		ID: newID(), SessionID: s.ID, Role: "assistant", Kind: KindPigeonAnswer, Persona: "biological",
		Content: "Dat kan ik niet vaststellen; verwant is wel [1].", KBStatus: string(KBSources),
		Related: true, CreatedAt: now,
	}
	gewoon := ChatMessage{
		ID: newID(), SessionID: s.ID, Role: "assistant", Kind: KindPigeonAnswer, Persona: "biological",
		Content: "Ja, dat staat in het onderzoek [1].", KBStatus: string(KBSources), CreatedAt: now.Add(time.Second),
	}
	for _, m := range []ChatMessage{grenzend, gewoon} {
		if err := repo.AppendMessage(ctx, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	got, err := repo.ListMessages(ctx, s.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("aantal berichten = %d, want 2", len(got))
	}
	if !got[0].Related {
		t.Error("de verwante-markering ging verloren; na herladen leest dit als volledige dekking")
	}
	if got[1].Related {
		t.Error("een gewoon antwoord kreeg een verwante-markering")
	}
}

// Rolling the related column back and forward around an existing answer must
// leave that answer untouched, and every row from before it must read as an
// ordinary search hit — which is what those rows were.
func TestPostgresRelatedMigration_IsAdditiveAndKeepsLegacyAnswers(t *testing.T) {
	_, pool := pgTestRepo(t)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	down, err := fs.ReadFile(migrationsFS, "migrations/0009_chat_related.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("down: %v", err)
	}
	sid, mid := newID(), newID()
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_sessions(id,user_id,pigeon_id,lang) VALUES($1,'legacy-owner','biological','en')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_messages(id,session_id,role,content) VALUES($1,$2,'assistant','The unchanged original answer [1].')`, mid, sid); err != nil {
		t.Fatal(err)
	}
	up, err := fs.ReadFile(migrationsFS, "migrations/0009_chat_related.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("up: %v", err)
	}

	var content string
	var related bool
	if err = tx.QueryRow(ctx, `SELECT content,related FROM duif.chat_messages WHERE id=$1`, mid).Scan(&content, &related); err != nil {
		t.Fatal(err)
	}
	if content != "The unchanged original answer [1]." || related {
		t.Fatalf("migration changed a legacy answer or marked it related: %q %v", content, related)
	}
}

// Rolling the comparison-origin column back and forward around an existing
// answer must leave that answer untouched: the column is only a trail from a
// copy to the answer it came from.
func TestPostgresSourceMessageMigration_IsAdditiveAndKeepsLegacyAnswers(t *testing.T) {
	_, pool := pgTestRepo(t)
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	down, err := fs.ReadFile(migrationsFS, "migrations/0008_comparison_source_message.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("down: %v", err)
	}
	sid, mid := newID(), newID()
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_sessions(id,user_id,pigeon_id,lang) VALUES($1,'legacy-owner','biological','en')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO duif.chat_messages(id,session_id,role,content) VALUES($1,$2,'assistant','The unchanged original answer [1].')`, mid, sid); err != nil {
		t.Fatal(err)
	}
	up, err := fs.ReadFile(migrationsFS, "migrations/0008_comparison_source_message.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("up: %v", err)
	}

	var content, origin string
	if err = tx.QueryRow(ctx, `SELECT content,source_message_id FROM duif.chat_messages WHERE id=$1`, mid).Scan(&content, &origin); err != nil {
		t.Fatal(err)
	}
	if content != "The unchanged original answer [1]." || origin != "" {
		t.Fatalf("migration changed a legacy answer or invented an origin: %q %q", content, origin)
	}
}

// De index op de uitwaaierkolom moet bruikbaar zijn voor de enige query die
// hem leest: `WHERE id=$1 OR source_message_id=$1`. Een gebonden parameter is
// voor de planner onbekend, dus onder een generiek plan kan hij een
// voorwaarde als "source_message_id ongelijk aan de lege string" niet
// bewijzen — hij laat zo'n partiële index links liggen en scant de hele
// tabel. Deze test legt beide
// helften vast: de index draagt geen voorwaarde, en het generieke plan van
// precies die update raakt hem ook werkelijk aan.
func TestPostgresSourceMessageIndex_IsUsableForABoundParameter(t *testing.T) {
	_, pool := pgTestRepo(t)
	ctx := t.Context()

	var predicate *string
	if err := pool.QueryRow(ctx, `SELECT pg_get_expr(i.indpred, i.indrelid)
                FROM pg_index i
                JOIN pg_class c ON c.oid = i.indexrelid
                JOIN pg_namespace n ON n.oid = c.relnamespace
                WHERE n.nspname = 'duif' AND c.relname = 'chat_messages_source_message'`).Scan(&predicate); err != nil {
		t.Fatalf("index chat_messages_source_message niet gevonden: %v", err)
	}
	if predicate != nil {
		t.Fatalf("de index draagt een voorwaarde (%s); die kan de planner voor een parameter niet bewijzen", *predicate)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// force_generic_plan is de kern van de zaak: zonder die stand plant
	// Postgres de eerste uitvoeringen mét de meegegeven waarde, en dán kan hij
	// `source_message_id <> ''` wél bewijzen — een partiële index lijkt dan
	// gewoon te werken. Het generieke plan is wat een server na een handvol
	// beurten hergebruikt, en daar viel de index uit. enable_seqscan=off
	// beprijst de volledige scan weg zonder hem te verbieden: bestaat er geen
	// bruikbare index, dan kiest de planner hem alsnog en valt deze test om.
	// EXPLAIN zonder ANALYZE voert niets uit.
	if _, err := tx.Exec(ctx, `SET LOCAL plan_cache_mode = force_generic_plan`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	// PREPARE/EXECUTE en niet een parameter aan deze Query meegeven: een
	// los verzonden statement plant Postgres altijd mét de waarde erbij, hoe
	// plan_cache_mode ook staat. Alleen een echt voorbereid statement gaat
	// door de plancache heen, en dat is de weg waarlangs een server zijn
	// generieke plan opbouwt.
	if _, err := tx.Exec(ctx, `PREPARE auditfanout(text) AS
                UPDATE duif.chat_messages SET audit=NULL WHERE id=$1 OR source_message_id=$1`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF) EXECUTE auditfanout('een-antwoord-dat-hier-niet-staat')`)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "chat_messages_source_message") {
		t.Fatalf("het plan raakt de index niet aan:\n%s", plan.String())
	}
}
