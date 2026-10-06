package duif

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"
)

// ComparisonOrigin describes a deliberate copy of this user's chat context.
// It never accepts free-form context or meeting identifiers.
//
// ContextMessageIDs lists, in reading order, every source row that travelled
// along: this user's own messages, the question, and the answers the first
// pigeon gave to it. The question is copied and then asked again as a live
// turn, so the reader sees what was answered before and the second pigeon's
// answer streams straight away.
type ComparisonOrigin struct {
	GroupID           string   `json:"groupId"`
	SessionID         string   `json:"sessionId"`
	PigeonID          string   `json:"pigeonId"`
	QuestionID        string   `json:"questionId"`
	OriginalQuestion  string   `json:"originalQuestion"`
	ContextMessageIDs []string `json:"contextMessageIds"`
}

// A comparison carries the whole conversation, so the reader chooses nothing
// beyond the question and the pigeon who has not answered it yet.
type comparisonRequest struct {
	RequestID       string `json:"requestId"`
	SourceSessionID string `json:"sourceSessionId"`
	QuestionID      string `json:"questionId"`
	PigeonID        string `json:"pigeonId"`
}

var comparisonRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

// comparisonMaxContextRunes bounds one copy. It is far above a real
// conversation — the turn's own history window is much smaller — and exists so
// a single request cannot write an unbounded transaction.
const comparisonMaxContextRunes = 262144

// A key names one explicit intent; the intent behind it is checked as well, so
// a reused request ID with a different question or pigeon is a conflict rather
// than a silent second branch.
func (r comparisonRequest) fingerprint() string {
	r.RequestID = ""
	return contentVersion(r)
}

func (m *Module) handleComparison(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTurnBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req comparisonRequest
	if err := dec.Decode(&req); err != nil || !comparisonRequestIDPattern.MatchString(req.RequestID) {
		writeJSON(w, http.StatusBadRequest, badRequest(reasonComparisonUnreadable).body)
		return
	}
	hash := req.fingerprint()
	if existing, err := m.repo.GetComparison(r.Context(), user.ID, req.RequestID, hash); err == nil {
		writeJSON(w, http.StatusCreated, map[string]any{"session": existing})
		return
	} else if !errors.Is(err, ErrSessionNotFound) {
		m.writeComparisonError(w, err)
		return
	}
	// A retry of an already committed request returned above. Only genuinely
	// new branches consume a request allowance, preventing unbounded copies.
	if m.access != nil {
		release, ok := m.access.acquire(user.ID, false)
		if !ok {
			w.Header().Set("Retry-After", "60")
			policyError(w, 429, "request_limit", "Please wait a minute before creating another comparison.", "Wacht een minuut voordat je nog een vergelijking aanmaakt.")
			return
		}
		defer release()
	}
	target, ok := m.personas.Get(req.PigeonID)
	if !ok || !target.Chat {
		writeJSON(w, http.StatusBadRequest, badRequest(reasonComparisonTargetNoChat).body)
		return
	}
	source, err := m.repo.GetSession(r.Context(), req.SourceSessionID, user.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "session_not_found", Message: map[string]string{"en": "Chat session not found.", "nl": "Chatsessie niet gevonden."}})
		return
	}
	if source.PigeonID == target.ID {
		writeJSON(w, http.StatusBadRequest, badRequest(reasonComparisonSamePigeon).body)
		return
	}
	messages, err := m.repo.ListMessages(r.Context(), source.ID, 0)
	if err != nil {
		te := m.internalError("vergelijkcontext laden", err)
		writeJSON(w, te.status, te.body)
		return
	}
	question, context, ids := comparisonContext(messages, req.QuestionID, source.PigeonID)
	if question == nil {
		writeJSON(w, http.StatusBadRequest, badRequest(reasonComparisonQuestionGone).body)
		return
	}
	now := time.Now()
	session := ChatSession{
		ID: newID(), UserID: user.ID, PigeonID: target.ID, Lang: normLang(source.Lang), CreatedAt: now, UpdatedAt: now,
		Title: sessionTitle(question.Content), ComparisonRequestID: req.RequestID, ComparisonRequestHash: hash,
		Comparison: &ComparisonOrigin{GroupID: question.ID, SessionID: source.ID, PigeonID: source.PigeonID,
			QuestionID: question.ID, OriginalQuestion: question.Content, ContextMessageIDs: ids},
	}
	total := 0
	for i, message := range context {
		total += len([]rune(message.Content))
		message.ID, message.SessionID, message.CreatedAt = newID(), session.ID, now
		context[i] = message
	}
	if total > comparisonMaxContextRunes {
		writeJSON(w, http.StatusBadRequest, badRequest(reasonComparisonContextTooBig).body)
		return
	}
	created, err := m.repo.CreateComparison(r.Context(), session, context)
	if err != nil {
		m.writeComparisonError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": created})
}

// comparisonContext returns the question and everything up to and including the
// answer turn on it. What travels is fixed, so the reader always knows what the
// second pigeon was given: this user's own messages, the first pigeon's earlier
// answers, the question itself, and the answer that question already got — in
// their original order. The next human message ends the copy; nothing from a
// later turn goes along.
//
// Two kinds of row stay behind. The facilitator's framing and mediation belong
// to the turn they were spoken in — they address the pigeon who answered
// there, and the prompt of a comparison would never replay them anyway. Empty
// rows (a disconnect before the first chunk) are nothing to carry; skipping
// them keeps ContextMessageIDs aligned with what was actually written.
//
// A copied answer keeps its author, its sources, its provenance and the reason
// its own stream ended, so it stays the first pigeon's answer in the new
// conversation — half a sentence included. Each copy also records the original
// row it came from, always the first one and never an intermediate copy, so a
// copy of a copy still names where the words were written.
//
// What a copy does NOT take along is the source-coverage label an older row may
// still carry. That label was written by a check that no longer runs (the
// sources are verified on import instead); a copy is a row created today, and
// today no row is born with one. The original keeps its own — it is the answer
// that was actually checked back then. An answer from before personas existed
// is stamped with the pigeon of its own conversation: without that, the reading
// view would fall back to the pigeon on screen and hand her someone else's
// words. The facilitator question a pigeon answer belongs to stays behind with
// its own turn — the second pigeon reads a copied answer as another pigeon's
// statement, never as her own beat in an exchange (splitHistory).
func comparisonContext(messages []ChatMessage, questionID, sourcePigeonID string) (*ChatMessage, []ChatMessage, []string) {
	var question *ChatMessage
	context, ids := []ChatMessage{}, []string{}
	for i, message := range messages {
		if question != nil && message.Role == "user" {
			break
		}
		if question == nil && message.ID == questionID {
			if message.Role != "user" || message.Content == "" {
				return nil, nil, nil
			}
			question = &messages[i]
		}
		if message.Content == "" || isFacilitatorRow(message) {
			continue
		}
		copied := ChatMessage{Role: message.Role, Kind: normalizeKind(message.Kind), Content: message.Content}
		// Waar deze rij vandaan komt, altijd de oorspronkelijke rij en nooit
		// een tussenkopie.
		copied.SourceMessageID = message.SourceMessageID
		if copied.SourceMessageID == "" {
			copied.SourceMessageID = message.ID
		}
		if message.Role == "assistant" {
			copied.Persona = message.Persona
			if copied.Persona == "" {
				copied.Persona = sourcePigeonID
			}
			// Ook de herkomst van de bronnen: een kopie van een antwoord uit
			// grenzend onderzoek is nog steeds een antwoord uit grenzend
			// onderzoek, en zonder deze regel droeg alleen het brongesprek de
			// markering.
			copied.KBStatus, copied.Sources, copied.Related = message.KBStatus, message.Sources, message.Related
			copied.Provenance, copied.Partial = message.Provenance, message.Partial
			// En de stopreden. Een antwoord dat halverwege een zin stopte is
			// in de kopie nog steeds datzelfde halve antwoord; zonder deze
			// regel droeg het brongesprek de melding wél en de kopie niet, en
			// zag de lezer de tweede duif een afgekapt antwoord van de eerste
			// weerspreken alsof dat afgerond was.
			copied.StopReason = message.StopReason
		}
		context, ids = append(context, copied), append(ids, message.ID)
	}
	if question == nil {
		return nil, nil, nil
	}
	return question, context, ids
}

func (m *Module) writeComparisonError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrComparisonConflict) {
		writeJSON(w, http.StatusConflict, apiError{Error: "comparison_conflict", Message: map[string]string{
			"en": "This comparison request belongs to a different question. Start a new comparison.",
			"nl": "Dit vergelijkverzoek hoort bij een andere vraag. Begin een nieuwe vergelijking.",
		}})
		return
	}
	te := m.internalError("vergelijkgesprek aanmaken", err)
	writeJSON(w, te.status, te.body)
}
