package duif

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parai-nl/parai-core/auth"
)

// This acknowledges the product explanation, not a legal basis, interview
// consent or permission from everyone in a room. Change it when that explanation
// materially changes. Only the latest version and timestamp are retained.
const noticeVersion = "2026-09-22.1"

type noticeRecord struct {
	Version        string     `json:"version"`
	Acknowledged   bool       `json:"acknowledged"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt,omitempty"`
}

type accessPolicy struct {
	db      *pgxpool.Pool
	mu      sync.Mutex
	users   map[string]*requestAllowance
	active  int
	now     func() time.Time
	valid   func(*http.Request) bool
	recheck time.Duration
}

type requestAllowance struct {
	start         time.Time
	count, active int
}

func newAccessPolicy(db *pgxpool.Pool) *accessPolicy {
	return &accessPolicy{db: db, users: map[string]*requestAllowance{}, now: time.Now, recheck: 10 * time.Second}
}

// Re-run the existing framework gate with a fresh context: the cached user in
// the streaming request must not turn a revoked cookie/grant into permanent
// access. The probe captures no body and opens no second HTTP connection.
func (p *accessPolicy) wireAuthorizer(gate func(http.Handler) http.Handler) {
	p.valid = func(original *http.Request) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		allowed := false
		gate(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			allowed = auth.UserFromContext(r.Context()) != nil
		})).ServeHTTP(&accessProbe{header: http.Header{}}, original.Clone(ctx))
		return allowed
	}
}

type accessProbe struct{ header http.Header }

func (p *accessProbe) Header() http.Header       { return p.header }
func (*accessProbe) WriteHeader(int)             {}
func (*accessProbe) Write(b []byte) (int, error) { return len(b), nil }

func (m *Module) watchAccess(ctx context.Context, r *http.Request, cancel context.CancelFunc, user string) {
	ticker := time.NewTicker(m.access.recheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.access.valid(r) {
				continue
			}
			if m.meetings != nil && strings.HasSuffix(r.URL.Path, "/events") {
				parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/events"), "/")
				if s, ok := m.meetings.session(parts[len(parts)-1], user); ok {
					s.close("access_ended")
				}
			}
			cancel()
			return
		}
	}
}

// At most two simultaneous chat turns per account, sixteen per process, and
// twelve new costly requests/minute/account. No waiting queue. Comparison
// retries are already idempotent and do not call a provider; only new branches
// consume quota in handleComparison. Stop, playback, resume, reading and deletion never take a slot.
func (p *accessPolicy) acquire(user string, chat bool) (func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for id, a := range p.users {
		if a.active == 0 && now.Sub(a.start) >= time.Minute {
			delete(p.users, id)
		}
	}
	a := p.users[user]
	if a == nil {
		if len(p.users) >= 4096 {
			return nil, false
		}
		a = &requestAllowance{start: now}
		p.users[user] = a
	}
	if now.Sub(a.start) >= time.Minute {
		a.start = now
		a.count = 0
	}
	if a.count >= 12 || (chat && (a.active >= 2 || p.active >= 16)) {
		return nil, false
	}
	a.count++
	if chat {
		a.active++
		p.active++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if chat {
				a.active--
				p.active--
			}
		})
	}, true
}

// De twee grenzen van een verzoek. chatTurnBudget is ruim omdat een chatbeurt
// meerdere modelrondes doet; requestBudget geldt voor al het andere, dat snel
// klaar hoort te zijn.
//
// Variabel in plaats van constant zodat een test ze kan verkorten; productie
// verandert ze nooit.
var (
	chatTurnBudget = 2 * time.Minute
	requestBudget  = 15 * time.Second
)

// turnWriteGrace is de marge waarmee de schrijfdeadline van de verbinding
// vóórloopt op de context van de beurt. Zonder haar verliepen beide op
// dezelfde seconde: de beurt brak af en de server kon geen byte meer schrijven,
// dus de lezer kreeg geen `error` en geen `done` — de stream viel gewoon stil
// en de statusregel bleef staan.
//
// De marge is zo ruim omdat er tussen het omvallen en het `done` nog één
// opslagronde zit: pumpTurn en agentRun.persist bewaren het afgebroken antwoord
// met een eigen begroting van tien seconden, en juist die beurt hoort bewaard
// te worden. Vijftien seconden dekt die tien plus de afronding erna.
const turnWriteGrace = 15 * time.Second

// applyTurnBudget geeft het verzoek zijn harde grens en legt de deadlines van
// de verbinding daaromheen. De twee horen bij elkaar en staan daarom op één
// plek: de context verloopt eerder dan de verbinding, zodat de motor zijn
// annulering nog zichtbaar kan afmaken. De teruggegeven functie zet beide
// deadlines weer open — de verbinding wordt hergebruikt voor het volgende
// verzoek.
func applyTurnBudget(w http.ResponseWriter, r *http.Request, budget time.Duration) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	controller := http.NewResponseController(w)
	// De leesdeadline begrenst het inlezen van het verzoek en krijgt dezelfde
	// marge: korter maakt hem geen betere vangrail, maar wel gevaarlijk. Staat
	// er al een achtergrondlezer van net/http op de verbinding — dat is zo bij
	// een verzoek zónder body — dan breekt een leesdeadline vóór de context die
	// lezer af, en op die leesfout annuleert net/http de request-context. De
	// beurt zou dan als "lezer weggelopen" eindigen in plaats van als
	// tijdgrens, en dus weer zwijgend sluiten.
	//
	// Op de chatroute zelf speelt hij geen rol: daar wist startBackgroundRead
	// hem zodra de request-body op EOF is. Eén regel voor beide kanten is
	// eerlijker dan een uitzondering die van de vorm van het verzoek afhangt.
	until := time.Now().Add(budget + turnWriteGrace)
	_ = controller.SetReadDeadline(until)
	_ = controller.SetWriteDeadline(until)
	return r.WithContext(ctx), func() {
		cancel()
		_ = controller.SetReadDeadline(time.Time{})
		_ = controller.SetWriteDeadline(time.Time{})
	}
}

func policyError(w http.ResponseWriter, status int, code, en, nl string) {
	writeJSON(w, status, apiError{Error: code, Message: map[string]string{"en": en, "nl": nl}})
}

func (m *Module) accessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Init always wires this in the shipping module. Narrow handler unit
		// fixtures can still test their contracts without constructing Postgres.
		if m.access == nil {
			next.ServeHTTP(w, r)
			return
		}
		u := requireUser(w, r)
		if u == nil {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		path := r.URL.Path
		chat := r.Method == http.MethodPost && strings.HasSuffix(path, "/v1/chat/turn")
		meeting := r.Method == http.MethodPost && strings.HasSuffix(path, "/v1/meeting/sessions")
		comparison := r.Method == http.MethodPost && strings.HasSuffix(path, "/v1/chat/comparisons")
		if chat || meeting || comparison || (r.Method == http.MethodPost && strings.HasSuffix(path, "/v1/access/notice")) {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				// JSON prevents an arbitrary cross-origin HTML form/simple fetch
				// from spending a signed-in account's budget or acknowledging text.
				policyError(w, 415, "json_required", "This action requires a JSON request.", "Voor deze actie is een JSON-verzoek nodig.")
				return
			}
		}
		manual := r.Method == http.MethodPost && strings.Contains(path, "/v1/meeting/sessions/") && (strings.HasSuffix(path, "/respond") || strings.HasSuffix(path, "/intervene") || strings.HasSuffix(path, "/detector/retry"))
		duration := requestBudget
		if chat {
			duration = chatTurnBudget
		}
		var cancel context.CancelFunc
		if strings.HasSuffix(path, "/events") {
			var ctx context.Context
			ctx, cancel = context.WithCancel(r.Context())
			r = r.WithContext(ctx)
		} else {
			r, cancel = applyTurnBudget(w, r, duration)
		}
		defer cancel()
		ctx := r.Context()
		if m.access.valid != nil && (chat || strings.HasSuffix(path, "/events")) {
			// chi assigns Request.Pattern after this middleware. Snapshot before
			// spawning: cloning the live request inside the monitor would race.
			go m.watchAccess(ctx, r.Clone(ctx), cancel, u.ID)
		}
		if chat || meeting || comparison {
			record, err := m.readNotice(r.Context(), u.ID)
			if err != nil {
				m.writeAccessUnavailable(w)
				return
			}
			if !record.Acknowledged {
				policyError(w, 428, "notice_required", "Please read and confirm the introduction above before starting a conversation.", "Lees en bevestig eerst de uitleg hierboven voordat je een gesprek begint.")
				return
			}
		}
		if chat || meeting || manual {
			release, ok := m.access.acquire(u.ID, chat)
			if !ok {
				w.Header().Set("Retry-After", "60")
				policyError(w, 429, "request_limit", "This account has too many active or recent requests. Stop a conversation or wait a minute.", "Dit account heeft te veel lopende of recente verzoeken. Stop een gesprek of wacht een minuut.")
				return
			}
			defer release()
		}
		if meeting {
			r = m.authorizeMeetingLifetime(r)
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Module) readNotice(ctx context.Context, user string) (noticeRecord, error) {
	r := noticeRecord{Version: noticeVersion}
	err := m.access.db.QueryRow(ctx, `SELECT acknowledged_at FROM duif.access_notice WHERE user_id=$1 AND version=$2`, user, noticeVersion).Scan(&r.AcknowledgedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	r.Acknowledged = err == nil
	return r, err
}

func (m *Module) writeAccessUnavailable(w http.ResponseWriter) {
	policyError(w, 503, "access_unavailable", "The access information cannot be checked right now. Please try again.", "De toegangsinformatie kan nu niet gecontroleerd worden. Probeer het opnieuw.")
}

func (m *Module) handleNotice(w http.ResponseWriter, r *http.Request) {
	u := requireUser(w, r)
	if u == nil {
		return
	}
	if m.access == nil || m.access.db == nil {
		m.writeAccessUnavailable(w)
		return
	}
	record, err := m.readNotice(r.Context(), u.ID)
	if err != nil {
		m.writeAccessUnavailable(w)
		return
	}
	writeJSON(w, 200, record)
}

func (m *Module) handleAcknowledgeNotice(w http.ResponseWriter, r *http.Request) {
	u := requireUser(w, r)
	if u == nil {
		return
	}
	if m.access == nil || m.access.db == nil {
		m.writeAccessUnavailable(w)
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || body.Version != noticeVersion || d.Decode(&struct{}{}) != io.EOF {
		policyError(w, 409, "notice_changed", "The introduction has changed. Reload it and read it before confirming.", "De uitleg is gewijzigd. Laad deze opnieuw en lees hem voordat je bevestigt.")
		return
	}
	_, err := m.access.db.Exec(r.Context(), `INSERT INTO duif.access_notice(user_id,version) VALUES($1,$2)
ON CONFLICT(user_id) DO UPDATE SET version=EXCLUDED.version, acknowledged_at=now()
WHERE duif.access_notice.version <> EXCLUDED.version`, u.ID, body.Version)
	if err != nil {
		m.writeAccessUnavailable(w)
		return
	}
	m.handleNotice(w, r)
}

func (m *Module) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	u := requireUser(w, r)
	if u == nil {
		return
	}
	if m.access == nil || m.access.db == nil {
		m.writeAccessUnavailable(w)
		return
	}
	result, err := m.access.db.Exec(r.Context(), `DELETE FROM duif.chat_sessions WHERE id=$1 AND user_id=$2`, chi.URLParam(r, "id"), u.ID)
	if err != nil {
		m.writeAccessUnavailable(w)
		return
	}
	if result.RowsAffected() == 0 {
		policyError(w, 404, "session_not_found", "Conversation not found.", "Gesprek niet gevonden.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
