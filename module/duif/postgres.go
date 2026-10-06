package duif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepo struct{ db *pgxpool.Pool }

func newPostgresRepo(db *pgxpool.Pool) *postgresRepo { return &postgresRepo{db: db} }

type chatExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertChatSession(ctx context.Context, db chatExecer, s ChatSession, idempotent bool) (bool, error) {
	var origin any
	if s.Comparison != nil {
		origin = s.Comparison.SessionID
	}
	comparison, err := json.Marshal(s.Comparison)
	if err != nil {
		return false, err
	}
	query := `INSERT INTO duif.chat_sessions
   (id,user_id,pigeon_id,lang,created_at,updated_at,comparison,origin_session_id,comparison_request_id,comparison_request_hash)
   VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''))`
	if idempotent {
		query += ` ON CONFLICT (user_id,comparison_request_id) WHERE comparison_request_id IS NOT NULL DO NOTHING`
	}
	tag, err := db.Exec(ctx, query, s.ID, s.UserID, s.PigeonID, s.Lang, s.CreatedAt, s.UpdatedAt, comparison, origin, s.ComparisonRequestID, s.ComparisonRequestHash)
	return tag.RowsAffected() == 1, err
}

func (r *postgresRepo) CreateSession(ctx context.Context, s ChatSession) error {
	if _, err := insertChatSession(ctx, r.db, s, false); err != nil {
		return fmt.Errorf("duif: sessie aanmaken: %w", err)
	}
	return nil
}

// The target and copied context are one transaction. A failed copy can never
// leave a partially selected conversation behind.
func (r *postgresRepo) CreateComparison(ctx context.Context, s ChatSession, selected []ChatMessage) (*ChatSession, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inserted, err := insertChatSession(ctx, tx, s, true)
	if err != nil {
		return nil, err
	}
	if !inserted {
		// INSERT waits for the competing transaction. At READ COMMITTED this
		// next query sees its complete target and context, never a partial copy.
		return getComparison(ctx, tx, s.UserID, s.ComparisonRequestID, s.ComparisonRequestHash)
	}
	for _, m := range selected {
		if err := insertChatMessage(ctx, tx, m); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &s, nil
}

const sessionColumns = `id,user_id,pigeon_id,lang,created_at,updated_at,comparison,
 COALESCE(comparison_request_id,''),COALESCE(comparison_request_hash,''),
 COALESCE(NULLIF(comparison->>'originalQuestion',''),
 (SELECT content FROM duif.chat_messages WHERE session_id=duif.chat_sessions.id AND role='user' ORDER BY seq LIMIT 1),'')`

type chatQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getComparison(ctx context.Context, db chatQuerier, userID, requestID, requestHash string) (*ChatSession, error) {
	if requestID == "" {
		return nil, ErrSessionNotFound
	}
	s, err := scanChatSession(db.QueryRow(ctx, `SELECT `+sessionColumns+` FROM duif.chat_sessions WHERE user_id=$1 AND comparison_request_id=$2`, userID, requestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if s.ComparisonRequestHash != requestHash {
		return nil, ErrComparisonConflict
	}
	return s, nil
}

func (r *postgresRepo) GetComparison(ctx context.Context, userID, requestID, requestHash string) (*ChatSession, error) {
	return getComparison(ctx, r.db, userID, requestID, requestHash)
}

func scanChatSession(row pgx.Row) (*ChatSession, error) {
	var s ChatSession
	var comparison []byte
	if err := row.Scan(&s.ID, &s.UserID, &s.PigeonID, &s.Lang, &s.CreatedAt, &s.UpdatedAt, &comparison, &s.ComparisonRequestID, &s.ComparisonRequestHash, &s.Title); err != nil {
		return nil, err
	}
	if len(comparison) > 0 {
		if err := json.Unmarshal(comparison, &s.Comparison); err != nil {
			return nil, fmt.Errorf("duif: vergelijkherkomst lezen: %w", err)
		}
	}
	s.Title = sessionTitle(s.Title)
	return &s, nil
}

func (r *postgresRepo) GetSession(ctx context.Context, id, userID string) (*ChatSession, error) {
	s, err := scanChatSession(r.db.QueryRow(ctx, `SELECT `+sessionColumns+` FROM duif.chat_sessions WHERE id=$1 AND user_id=$2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("duif: sessie laden: %w", err)
	}
	return s, nil
}

func (r *postgresRepo) ListSessions(ctx context.Context, userID string) ([]ChatSession, error) {
	rows, err := r.db.Query(ctx, `SELECT `+sessionColumns+` FROM duif.chat_sessions WHERE user_id=$1 ORDER BY updated_at DESC,id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("duif: sessies laden: %w", err)
	}
	defer rows.Close()
	var out []ChatSession
	for rows.Next() {
		s, err := scanChatSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func insertChatMessage(ctx context.Context, db chatExecer, m ChatMessage) error {
	m = stampAnswer(ctx, m)
	sources, err := json.Marshal(m.Sources)
	if err != nil {
		return err
	}
	provenance, err := json.Marshal(m.Provenance)
	if err != nil {
		return err
	}
	audit, err := json.Marshal(m.Audit)
	if err != nil {
		return err
	}
	// seq is deliberately omitted: the database sequence preserves write order.
	_, err = db.Exec(ctx, `INSERT INTO duif.chat_messages
 (id,session_id,role,content,kb_status,sources,partial,created_at,kind,persona,question,provenance,audit,stop_reason,source_message_id,related)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		m.ID, m.SessionID, m.Role, m.Content, m.KBStatus, sources, m.Partial, m.CreatedAt, normalizeKind(m.Kind), m.Persona, m.Question, provenance, audit, m.StopReason, m.SourceMessageID, m.Related)
	return err
}

func (r *postgresRepo) AppendMessage(ctx context.Context, m ChatMessage) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("duif: transactie: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertChatMessage(ctx, tx, m); err != nil {
		return fmt.Errorf("duif: bericht opslaan: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE duif.chat_sessions SET updated_at=$2 WHERE id=$1`, m.SessionID, m.CreatedAt); err != nil {
		return fmt.Errorf("duif: sessie bijwerken: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *postgresRepo) ListMessages(ctx context.Context, sessionID string, limit int) ([]ChatMessage, error) {
	const columns = `id,session_id,role,content,kb_status,sources,partial,created_at,kind,persona,question,seq,provenance,audit,stop_reason,source_message_id,related`
	q := `SELECT ` + columns + ` FROM duif.chat_messages WHERE session_id=$1 ORDER BY seq ASC`
	args := []any{sessionID}
	if limit > 0 {
		q = `SELECT ` + columns + ` FROM (SELECT * FROM duif.chat_messages WHERE session_id=$1 ORDER BY seq DESC LIMIT $2) laatste ORDER BY seq ASC`
		args = append(args, limit)
	}
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("duif: berichten laden: %w", err)
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var sources, provenance, audit []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.KBStatus, &sources, &m.Partial, &m.CreatedAt, &m.Kind, &m.Persona, &m.Question, &m.Seq, &provenance, &audit, &m.StopReason, &m.SourceMessageID, &m.Related); err != nil {
			return nil, err
		}
		if len(sources) > 0 {
			if err := json.Unmarshal(sources, &m.Sources); err != nil {
				return nil, fmt.Errorf("duif: bronnen parsen: %w", err)
			}
		}
		if len(provenance) > 0 {
			if err := json.Unmarshal(provenance, &m.Provenance); err != nil {
				return nil, fmt.Errorf("duif: antwoordversie lezen: %w", err)
			}
		}
		if len(audit) > 0 {
			if err := json.Unmarshal(audit, &m.Audit); err != nil {
				return nil, fmt.Errorf("duif: antwoordaudit lezen: %w", err)
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
