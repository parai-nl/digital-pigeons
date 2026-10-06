package duif

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// maxMeetingBodyBytes begrenst de besturingsberichten van de vergaderroutes.
const maxMeetingBodyBytes = 32 * 1024

// maxSegmentRunes begrenst één transcriptsegment.
const maxSegmentRunes = 4096

// maxSpeakerRunes begrenst het sprekerlabel van een segment. Het is een naam
// en geen bijdrage, dus het plafond mag laag. Zonder plafond reed een
// sprekerveld van 32 KiB (alles wat maxMeetingBodyBytes toelaat) het
// promptvenster in, en het telde daar bovendien niet mee in het woordbudget.
const maxSpeakerRunes = 128

// maxAudioBodyBytes en audioChunkBytes begrenzen de microfoonstroom: één
// verzoek draagt hooguit een seconde of wat geluid, in blokken die meteen de
// STT-brug in gaan. Er wordt niets van vastgehouden.
const (
	maxAudioBodyBytes = 1 << 20
	audioChunkBytes   = 4096
)

// routes mount de vergadermodus onder de module-naamruimte (alles onder
// /api/duif/v1/…). Auth komt van het framework; requireUser is de module-eigen
// vangrail.
//
// Bewuste afwijking van het oorspronkelijke ontwerp, dat één duplex-WebSocket
// per sessie voorschreef (`/api/duif/v1/meeting/{sessionId}/ws`). Deze module
// levert in plaats daarvan SSE (server → client) plus POST .../audio
// (client → server).
//
// Waarom: de SSE-kant dekt de knop, de stopknop, de inactiviteitsbewaking en de
// luisterindicator volledig en is met httptest als échte stroom te testen, waar
// een WS-upgrade eigen middleware en een eigen testharnas vergt. De STT-brug is
// inmiddels geland (meeting_stt.go), dus de oorspronkelijke voorwaarde "de WS
// komt erbij zodra de brug landt" is verlopen; dit is nu een staande keuze,
// geen tussenstand.
//
// Wat het kost: de microfoonkant is halfduplex. De browser houdt één POST in de
// lucht en zet nieuwe blokken zolang in een begrensde sluis (meeting.js,
// MAX_SLUISBLOKKEN) die bij een trage lijn overloopt en dan zichtbaar geluid
// laat vallen. Dat werkt tot ruwweg twee seconden round-trip; daarboven hoort
// de duif gaten. Een duplex-WS haalt die grens weg en is de aangewezen stap
// zodra de vergadermodus buiten een lokaal netwerk gaat draaien.
//
// Sessiebeheer, venster en spreekbeurt zijn transportonafhankelijk gebouwd, dus
// die stap raakt alleen dit bestand en meeting.js.
func (e *meetingEngine) routes(r chi.Router) {
	r.Post("/v1/meeting/sessions", e.handleOpen)
	r.Get("/v1/meeting/sessions/{id}/events", e.handleEvents)
	r.Post("/v1/meeting/sessions/{id}/audio", e.handleAudio)
	r.Post("/v1/meeting/sessions/{id}/transcript", e.handleTranscript)
	r.Post("/v1/meeting/sessions/{id}/respond", e.handleRespondNow)
	r.Post("/v1/meeting/sessions/{id}/intervene", e.handleIntervene)
	r.Post("/v1/meeting/sessions/{id}/settings", e.handleSettings)
	r.Post("/v1/meeting/sessions/{id}/stop", e.handleStop)
	r.Post("/v1/meeting/sessions/{id}/resume", e.handleResume)
	r.Post("/v1/meeting/sessions/{id}/playback", e.handlePlayback)
	r.Post("/v1/meeting/sessions/{id}/detector/retry", e.handleDetectorRetry)
	r.Post("/v1/meeting/sessions/{id}/candidate/speak", e.handleCandidateSpeak)
	r.Get("/v1/meeting/sessions/{id}/diagnostics", e.handleDiagnostics)
	r.Post("/v1/meeting/sessions/{id}/presence", e.handlePresence)
	r.Delete("/v1/meeting/sessions/{id}", e.handleEnd)
}

// decodeMeetingBody leest een besturingsbericht; onbekende velden zijn een
// 400 (het protocol is klein en expliciet).
func decodeMeetingBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMeetingBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		te := badRequest(reasonMalformedBody)
		writeJSON(w, te.status, te.body)
		return false
	}
	return true
}

// resolve haalt de sessie van de ingelogde gebruiker op. Onbekend en
// andermans sessie zijn allebei een kale 404: eigenaarschap lekt niet.
func (e *meetingEngine) resolve(w http.ResponseWriter, r *http.Request) (*meetingSession, bool) {
	u := requireUser(w, r)
	if u == nil {
		return nil, false
	}
	s, ok := e.session(chi.URLParam(r, "id"), u.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{
			Error:   "session_not_found",
			Message: map[string]string{"en": "Meeting session not found.", "nl": "Vergadersessie niet gevonden."},
		})
		return nil, false
	}
	return s, true
}

// resolveControl is resolve voor de bedieningsroutes: de knoppen die een mens
// zelf indrukt. Zo'n verzoek is het teken van leven waar de
// inactiviteitsbewaking op telt, ook als de actie zelf daarna
// geweigerd wordt — er zat iemand aan het scherm. De routes die de browser
// zelf aanroept (audio, transcript, afspeelmeldingen, het kanaal) gaan via
// resolve en tellen niet.
func (e *meetingEngine) resolveControl(w http.ResponseWriter, r *http.Request) (*meetingSession, bool) {
	s, ok := e.resolve(w, r)
	if ok {
		s.noteUserActivity()
	}
	return s, ok
}

// handleOpen — POST /v1/meeting/sessions: zet een duif aan tafel.
func (e *meetingEngine) handleOpen(w http.ResponseWriter, r *http.Request) {
	u := requireUser(w, r)
	if u == nil {
		return
	}
	var req struct {
		PigeonID         string `json:"pigeonId"`
		PlaybackTracking bool   `json:"playbackTracking"`
		Lang             string `json:"lang"`
		// Instellingen voor zelfstandig meedoen; sessie-metadata, geen inhoud.
		// Autonomous is een pointer: afwezig betekent "wat de gedragsconfig
		// zegt", niet "uit".
		Autonomous      *bool  `json:"autonomous"`
		Eagerness       string `json:"eagerness"`
		CooldownSeconds int    `json:"cooldownSeconds"`
		// ReplaceExisting is de uitweg uit "er is al een vergadering open":
		// sluit de open vergadering(en) van dít account en ga hier
		// verder. De gebruiker bevestigt dat in de UI; nooit andermans plaats.
		ReplaceExisting bool `json:"replaceExisting"`
		// HandoverFrom is de duifwissel: neem de vergadercontext van je
		// eigen lopende sessie over. Vervangt die sessie, dus de wissel loopt
		// nooit tegen de capaciteitsregel aan.
		HandoverFrom string `json:"handoverFrom"`
	}
	if !decodeMeetingBody(w, r, &req) {
		return
	}
	if req.Lang != "" && req.Lang != "en" && req.Lang != "nl" {
		te := badRequest(reasonUnsupportedLang)
		writeJSON(w, te.status, te.body)
		return
	}
	if req.Eagerness != "" {
		if _, ok := eagernessShift[req.Eagerness]; !ok {
			te := badRequest(reasonEagerness)
			writeJSON(w, te.status, te.body)
			return
		}
	}
	if req.CooldownSeconds < 0 || req.CooldownSeconds > 3600 {
		te := badRequest(reasonCooldown)
		writeJSON(w, te.status, te.body)
		return
	}
	// De standaardstand van zelfstandig meedoen hangt aan de duif:
	// de signalerende duif doet vanzelf mee, de pratende twee wachten op een
	// uitnodiging. Kent de config deze duif niet, dan valt het verzoek verderop
	// bij openSession om; hier blijft de gedeelde terugval staan.
	autonomous := e.personas.Gedrag.InterventieZelfstandig
	if persona, ok := e.personas.Get(req.PigeonID); ok {
		autonomous = persona.AutonomousDefault(autonomous)
	}
	settings := meetingSettings{
		PlaybackTracking: req.PlaybackTracking,
		Autonomous:       autonomous,
		Eagerness:        req.Eagerness,
		Cooldown:         time.Duration(req.CooldownSeconds) * time.Second,
	}
	if req.Autonomous != nil {
		settings.Autonomous = *req.Autonomous
	}
	if settings.Eagerness == "" {
		settings.Eagerness = e.personas.Gedrag.DetectieGretigheid
	}
	s, err := e.openSession(u.ID, req.PigeonID, req.Lang, settings, openRequest{
		ReplaceOwn: req.ReplaceExisting,
		Handover:   req.HandoverFrom,
	})
	if err != nil {
		// Twee soorten "vol", twee meldingen: bij een eigen open vergadering
		// heeft de gebruiker een knop om zichzelf los te maken, bij bezette
		// plaatsen van anderen valt er niets te bevrijden.
		if errors.Is(err, errMeetingCapacitySelf) {
			w.Header().Set("Retry-After", "30")
			policyError(w, 429, "meeting_capacity_self", "A meeting from this account is still open somewhere else — another tab, another window, or a tab that was closed without ending it.", "Er is nog een vergadering van dit account open — in een ander tabblad, een ander venster, of een tabblad dat is weggeklikt zonder te beëindigen.")
			return
		}
		if errors.Is(err, errMeetingCapacity) {
			w.Header().Set("Retry-After", "30")
			policyError(w, 429, "meeting_capacity", "All places at the table are occupied right now. Try again shortly.", "Alle plaatsen aan tafel zijn op dit moment bezet. Probeer het zo opnieuw.")
			return
		}
		te := badRequest(reasonUnknownMeetingPigeon)
		writeJSON(w, te.status, te.body)
		return
	}
	bindMeetingAuthorization(r, s)
	// Eén snapshot voor het hele antwoord: twee losse lezingen konden een
	// halve stand opleveren als er tussendoor een settings-verzoek langskwam.
	snap := s.settingsSnapshot()
	writeJSON(w, http.StatusCreated, map[string]any{
		"sessionId": s.id,
		"pigeonId":  s.pigeonID,
		"lang":      s.lang,
		"listening": s.listenState(),
		"voice":     s.voice,
		// settings is de volledige stand (ook cooldownSeconds), zodat een
		// client die opnieuw verbindt niets hoeft te raden; autonomous en
		// eagerness blijven los staan voor de bestaande lezers.
		"settings":         snap,
		"autonomyPaused":   snap.AutonomyPaused,
		"playbackTracking": snap.PlaybackTracking,
		"autonomous":       snap.Autonomous,
		"eagerness":        snap.Eagerness,
	})
}

// handleEvents — GET .../events: het sessiekanaal (luisterindicator,
// transcript, spreekbeurt, aanwezigheidsvragen). De stroom eindigt als de
// sessie sluit of de client weggaat.
func (e *meetingEngine) handleEvents(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	ch, unsubscribe := s.subscribe()
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Een SSE-stroom die niet doorgespoeld kan worden is geen stroom: kan de
	// keten het niet, dan stopt hij hier in plaats van stilletjes te bufferen.
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			raw, err := json.Marshal(ev)
			if err != nil {
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := w.Write(append(append([]byte("data: "), raw...), '\n', '\n')); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// handleAudio — POST .../audio: microfoonaudio (PCM16) de STT-brug in.
//
// De body wordt in blokken gelezen en meteen doorgegeven; er wordt niets
// verzameld, niets geretourneerd en niets bewaard. Wat de brug niet kwijt kan
// — omdat de STT-sessie net weg is — verdwijnt geteld; vergaderaudio wordt
// nergens bewaard.
func (e *meetingEngine) handleAudio(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	if s.stt == nil {
		writeJSON(w, http.StatusServiceUnavailable, apiError{
			Error: "stt_unavailable",
			Message: map[string]string{
				"en": "Live listening is not available: no speech-to-text provider is configured.",
				"nl": "Live meeluisteren is niet beschikbaar: er is geen spraak-naar-tekstdienst geconfigureerd.",
			},
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAudioBodyBytes)
	var streamed, dropped int
	splitAudioFrames(r.Body, func(frame []byte) {
		if s.stt.push(frame) {
			streamed++
		} else {
			dropped++
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"streamed": streamed, "dropped": dropped, "listening": s.listenState(),
	})
}

// handleTranscript — POST .../transcript: één gecommit segment het venster in.
//
// De STT-brug (meeting_stt.go) voedt exact dezelfde sessie-API; deze route is
// de tweede bron ernaast, voor een vergadering met een eigen transcriptbron en
// voor de e2e. Het segment gaat het geheugen in en het kanaal op; nergens
// anders heen — en het raakt de luisterindicator niet aan (appendSegment).
func (e *meetingEngine) handleTranscript(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	var req struct {
		Text    string `json:"text"`
		Speaker string `json:"speaker"`
	}
	if !decodeMeetingBody(w, r, &req) {
		return
	}
	n := len([]rune(req.Text))
	if n < 1 || n > maxSegmentRunes {
		te := badRequest(reasonMeetingTextLength)
		writeJSON(w, te.status, te.body)
		return
	}
	if len([]rune(req.Speaker)) > maxSpeakerRunes {
		te := badRequest(reasonMeetingSpeakerLength)
		writeJSON(w, te.status, te.body)
		return
	}
	if !s.appendTypedSegment(req.Text, req.Speaker) {
		writeJSON(w, http.StatusNotFound, apiError{
			Error:   "session_closed",
			Message: map[string]string{"en": "Meeting session is closed.", "nl": "Vergadersessie is gesloten."},
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "segments": s.segmentCount()})
}

// handleRespondNow — POST .../respond: de knop.
func (e *meetingEngine) handleRespondNow(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	switch err := e.respondNow(s); {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
	case errors.Is(err, errPigeonBusy):
		writeJSON(w, http.StatusConflict, apiError{
			Error:   "pigeon_busy",
			Message: busyMessage,
		})
	default:
		te := badRequest(reasonPigeonDoesNotSpeak)
		writeJSON(w, te.status, te.body)
	}
}

// handleIntervene — POST .../intervene: de handmatige "de duif grijpt in"-knop
// van de non-verbale duif. Werkt altijd, ook met detector uit of kapot.
func (e *meetingEngine) handleIntervene(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	sound, level, err := e.manualIntervene(s)
	switch {
	case errors.Is(err, errPigeonBusy):
		writeJSON(w, http.StatusConflict, apiError{Error: "pigeon_busy", Message: busyMessage})
	case errors.Is(err, errPigeonDoesNotSound):
		te := badRequest(reasonPigeonSpeaks)
		writeJSON(w, te.status, te.body)
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, apiError{
			Error: "no_sound_available",
			Message: map[string]string{
				"en": "This pigeon has no recordings to play.",
				"nl": "Deze duif heeft geen opnames om af te spelen.",
			},
		})
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"sound": sound, "arousal": level})
	}
}

// handleSettings — POST .../settings: de knoppen voor zelfstandig meedoen live
// bijstellen.
// Afwezige velden blijven staan; uitzetten werkt per direct.
func (e *meetingEngine) handleSettings(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	var req struct {
		Autonomous      *bool  `json:"autonomous"`
		Eagerness       string `json:"eagerness"`
		CooldownSeconds *int   `json:"cooldownSeconds"`
	}
	if !decodeMeetingBody(w, r, &req) {
		return
	}
	if req.Eagerness != "" {
		if _, known := eagernessShift[req.Eagerness]; !known {
			te := badRequest(reasonEagerness)
			writeJSON(w, te.status, te.body)
			return
		}
	}
	var cooldown *time.Duration
	if req.CooldownSeconds != nil {
		if *req.CooldownSeconds < 0 || *req.CooldownSeconds > 3600 {
			te := badRequest(reasonCooldown)
			writeJSON(w, te.status, te.body)
			return
		}
		d := time.Duration(*req.CooldownSeconds) * time.Second
		cooldown = &d
	}
	writeJSON(w, http.StatusAccepted, s.applySettings(req.Autonomous, req.Eagerness, cooldown))
}

// handleStop — POST .../stop: de stopknop. Stoppen is geen sessie-einde:
// de duif zwijgt, het meeluisteren gaat door.
func (e *meetingEngine) handleStop(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	was := s.stopSpeaking()
	writeJSON(w, http.StatusAccepted, map[string]any{"stopped": true, "wasSpeaking": was, "autonomyPaused": true, "settings": s.settingsSnapshot()})
}

// handlePresence — POST .../presence: "ja, we zijn er nog". Het is ook de
// route waarlangs de browser de klikken en toetsen tussen de knoppen door meldt
// (gedrosseld): beide zijn hetzelfde teken van leven.
func (e *meetingEngine) handlePresence(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"acknowledged": true, "pendingCheck": s.noteUserActivity()})
}

// handleEnd — DELETE .../{id}: de vergadering is klaar.
func (e *meetingEngine) handleEnd(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	s.close(CloseReasonUser)
	w.WriteHeader(http.StatusNoContent)
}

// splitAudioFrames knipt een PCM16-body in frames van audioChunkBytes en geeft
// ze meteen door; er wordt niets vastgehouden dan het restant tot de volgende
// framegrens. De body komt in willekeurige stukken binnen (de HTTP-lezer knipt
// waar zijn buffer toevallig vol is), maar een frame moet op een monstergrens
// eindigen: een oneven aantal bytes is voor de STT-provider geen audio
// ("Failed to decode audio chunk") en verschuift elk volgend monster een byte
// op. Live gaf dat per browserblok frames van 4096, 4095 en 1 byte, waarvan er
// twee werden geweigerd. Het laatste restant gaat met een even lengte mee; een
// enkele halve byte aan het eind vervalt.
func splitAudioFrames(body io.Reader, push func([]byte)) {
	buf := make([]byte, audioChunkBytes)
	var pending []byte
	for {
		n, err := body.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for len(pending) >= audioChunkBytes {
				frame := make([]byte, audioChunkBytes)
				copy(frame, pending[:audioChunkBytes])
				pending = pending[audioChunkBytes:]
				push(frame)
			}
		}
		if err != nil {
			break
		}
	}
	if rest := len(pending) &^ 1; rest > 0 {
		frame := make([]byte, rest)
		copy(frame, pending[:rest])
		push(frame)
	}
}
