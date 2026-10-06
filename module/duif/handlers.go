package duif

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/parai-nl/parai-core/auth"
)

// maxTurnBodyBytes begrenst de request-body van een turn (4096 tekens bericht
// + JSON-overhead, ruim genomen).
const maxTurnBodyBytes = 64 * 1024

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// requireUser haalt de ingelogde gebruiker uit de context. In productie dwingt
// het framework (RequireModuleAccess) dit al af vóór de module; deze check is
// de module-eigen vangrail (met een 401).
func requireUser(w http.ResponseWriter, r *http.Request) *auth.User {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, apiError{
			Error:   "unauthorized",
			Message: map[string]string{"en": "Login required.", "nl": "Inloggen vereist."},
		})
		return nil
	}
	return u
}

// handleHealth — GET /v1/health.
func (m *Module) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// duifDTO is de persona-lijst-DTO: gedrags- en productmatrix-metadata voor de
// frontend, zónder systemprompt (prompts verlaten de server nooit).
type duifDTO struct {
	ID            string     `json:"id"`
	Name          LocalName  `json:"name"`
	VoiceReady    bool       `json:"voiceReady"` // false zolang voice_id een placeholder of leeg is
	KnowledgeBase bool       `json:"knowledgeBase"`
	Chat          bool       `json:"chat"`
	Meeting       bool       `json:"meeting"`
	Topics        []TopicRef `json:"topics"` // thema's voor themabadges in de UI
	// Engine is de chatmotor van deze duif (EnginePipeline of EngineAgent).
	// De client leidt eruit af wie er in een beurt als eerste spreekt: op de
	// agent-motor opent de facilitator, op de pipeline de duif zelf. Dat staat
	// per duif vast, dus het hoort hier en niet in een frame per beurt.
	Engine string `json:"engine"`
	// AutonomousDefault is de startstand van zelfstandig meedoen voor deze
	// duif. De opzetkaart zet de schakelaar erop; wie hem verzet,
	// verzet hem voor die ene vergadering. `gedrag.interventieZelfstandig`
	// blijft ernaast staan als de gedeelde terugval.
	AutonomousDefault bool `json:"autonomousDefault"`
}

// handleDuiven — GET /v1/duiven.
func (m *Module) handleDuiven(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	out := struct {
		Duiven      []duifDTO           `json:"duiven"`
		Gedrag      Gedrag              `json:"gedrag"`
		ModelPolicy languageModelPolicy `json:"modelPolicy"`
	}{Gedrag: m.personas.Gedrag, ModelPolicy: m.languageModelPolicy()}
	for _, p := range m.personas.Personas {
		// Alleen id en label gaan de deur uit; de themabeschrijving is
		// classifier-invoer en blijft — net als de systemprompt — op de server.
		topics := make([]TopicRef, 0, len(p.Topics))
		for _, t := range p.Topics {
			topics = append(topics, TopicRef{ID: t.ID, Label: t.Label})
		}
		// Eén bron voor "heeft deze duif een stem" (zie resolveVoiceID); de
		// dev-stem blijft er bewust buiten, die hoort bij de sessie.
		_, voiceReady := resolveVoiceID(p, "")
		out.Duiven = append(out.Duiven, duifDTO{
			ID:            p.ID,
			Name:          p.Naam,
			VoiceReady:    voiceReady,
			KnowledgeBase: p.Kennisbank,
			Chat:          p.Chat,
			Meeting:       p.Vergadering,
			Topics:        topics,
			Engine:        p.EngineOr(),

			AutonomousDefault: p.AutonomousDefault(m.personas.Gedrag.InterventieZelfstandig),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSound — GET /v1/sounds/{id}: één veldopname van de non-verbale duif
// Dit zijn vooraf opgenomen statische assets uit de moduleconfig, geen
// vergaderaudio: de regel dat vergaderinhoud niet bewaard wordt gaat over wat
// er in de vergaderzaal klinkt, niet over wat de duif meebrengt.
func (m *Module) handleSound(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	f, err := m.personas.OpenSound(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{
			Error:   "sound_not_found",
			Message: map[string]string{"en": "Recording not found.", "nl": "Opname niet gevonden."},
		})
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "audio/mpeg")
	// Privé: de veldopnames horen bij één ingelogde gebruiker en gaan niet in
	// een gedeelde cache.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// Kan het bestand zoeken (embed en os.DirFS kunnen dat), dan doet
	// ServeContent het werk: Range-verzoeken en HEAD erbij, zodat een
	// audiospeler mag terugspoelen. De modtijd blijft nul — deze opnames
	// horen bij de build, niet bij een klok.
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, rs)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		m.log().Debug("geluid streamen afgebroken", "component", "duif", "error", err)
	}
}

// handleTurn — POST /v1/chat/turn (SSE).
func (m *Module) handleTurn(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTurnBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req turnRequest
	if err := dec.Decode(&req); err != nil {
		te := badRequest(reasonMalformedBody)
		writeJSON(w, te.status, te.body)
		return
	}

	r = r.WithContext(context.WithValue(r.Context(), provenanceContextKey{}, m.chatProvenance(r.Context(), req.PigeonID)))
	pt, terr := m.prepareTurn(r.Context(), user.ID, req)
	if terr != nil {
		writeJSON(w, terr.status, terr.body)
		return
	}
	// prepareTurn has durably created the session and stored the human message.
	// Return its identity even if starting the provider fails or the user stops
	// before a done frame. Never expose IDs for rejected or foreign sessions.
	w.Header().Set("X-Duif-Session-ID", pt.session.ID)
	// De laatste stap die nog een REST-fout mag zijn: de pipeline opent hier
	// zijn LLM-stream, zodat een weigering van de proxy (budgetcap) nog een
	// 503 kan zijn. Wélke motor dat is weet de handler niet — dat is precies
	// de bedoeling; zie engineFor in turn.go.
	run, terr := pt.start(r.Context())
	if terr != nil {
		writeJSON(w, terr.status, terr.body)
		return
	}

	// Vanaf hier is het antwoord een stream; fouten worden error-frames.
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		return
	}

	emit := func(frame any) error {
		if done, ok := frame.(doneFrame); ok {
			provenance, _ := r.Context().Value(provenanceContextKey{}).(*AnswerProvenance)
			frame = struct {
				doneFrame
				Provenance *AnswerProvenance `json:"provenance,omitempty"`
			}{done, provenance}
		}
		raw, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(append([]byte("data: "), raw...), '\n', '\n')); err != nil {
			return err
		}
		// Een mislukte flush is het einde van de stroom, niet iets om over
		// heen te lezen: pumpTurn stopt op de eerste fout.
		return rc.Flush()
	}
	run(r.Context(), emit)
}

// handleSessions — GET /v1/chat/sessions (minimaal, voor UI-herstel).
func (m *Module) handleSessions(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	sessions, err := m.repo.ListSessions(r.Context(), user.ID)
	if err != nil {
		te := m.internalError("sessies laden", err)
		writeJSON(w, te.status, te.body)
		return
	}
	if sessions == nil {
		sessions = []ChatSession{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// handleSessionMessages — GET /v1/chat/sessions/{id}/messages.
func (m *Module) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := m.repo.GetSession(r.Context(), id, user.ID); err != nil {
		writeJSON(w, http.StatusNotFound, apiError{
			Error:   "session_not_found",
			Message: map[string]string{"en": "Chat session not found.", "nl": "Chatsessie niet gevonden."},
		})
		return
	}
	msgs, err := m.repo.ListMessages(r.Context(), id, 0)
	if err != nil {
		te := m.internalError("berichten laden", err)
		writeJSON(w, te.status, te.body)
		return
	}
	if msgs == nil {
		msgs = []ChatMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}
