package duif

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Herstel van de kandidaatgenerator. Er is geen venster meer om te
// bewaren: elke evaluatie bouwt haar eigen venster over het gesprek van dat
// moment, dus een mislukte call kost hoogstens één ronde. Wat overblijft is de
// vraag hoe vaak een storing zich mag herhalen voordat de vergadering het te
// horen krijgt.
const (
	// maxDetectorFailures is het aantal opeenvolgende storingen waarna de
	// generator zichzelf zichtbaar uitzet. Eén geslaagde evaluatie zet de
	// teller terug: een provider die af en toe hikt hoeft de duif niet de rest
	// van de vergadering het zwijgen op te leggen.
	maxDetectorFailures         = 3
	reasonDetectorConfiguration = "detector_configuration"
)

var detectorHTTPStatus = regexp.MustCompile(`(?:^|[\s:])([45][0-9]{2})(?:$|[\s:])`)

func classifyDetectorFailure(err error) string {
	if classifyLLMError(err) == ReasonBudgetExceeded {
		return ReasonBudgetExceeded
	}
	message := strings.ToLower(err.Error())
	// The provider adapter wraps HTTP failures in text. Match a complete status,
	// never digits embedded in a request ID. 408/409/429 and 5xx stay transient.
	if match := detectorHTTPStatus.FindStringSubmatch(message); len(match) > 1 {
		code, _ := strconv.Atoi(match[1])
		switch code {
		case 400, 401, 402, 403, 404, 405, 406, 410, 415, 422:
			return reasonDetectorConfiguration
		}
	}
	for _, part := range []string{"unauthorized", "invalid api key", "permission denied", "model_not_found", "invalid_request_error", "unsupported parameter", "unknown model", "unprocessable entity"} {
		if strings.Contains(message, part) {
			return reasonDetectorConfiguration
		}
	}
	return ReasonLLMUnavailable
}

// noteFailure boekt één mislukte evaluatie. Een kostenlimiet of een verkeerde
// configuratie is meteen einde verhaal; al het andere is een hik met een
// oplopende wachttijd, tot het er te veel achter elkaar zijn.
func (d *candidateEngine) noteFailure(now time.Time, reason string) {
	d.engine.log().Warn("kandidaatevaluatie niet beschikbaar", "component", "duif",
		"sessie", d.session.id, "reason", reason)
	d.skip(reason)
	if reason == ReasonBudgetExceeded || reason == reasonDetectorConfiguration {
		d.disable(reason)
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.failures++
	if d.failures >= maxDetectorFailures {
		d.mu.Unlock()
		d.disable(reason)
		return
	}
	d.reason = reason
	d.retryAt = now.Add(time.Duration(d.failures*d.failures) * time.Second)
	status := d.statusLocked(now)
	d.mu.Unlock()
	d.session.diagnostics.mark(now, "detector_retry", reason, 0)
	d.session.publish(MeetingEvent{Type: EventDetectorStatus, Detector: &status, Reason: reason})
}

// noteSuccess wist een eerdere storing en meldt het herstel, maar alleen als er
// iets te herstellen viel — anders zou elke geslaagde evaluatie een bericht
// kosten.
func (d *candidateEngine) noteSuccess() {
	d.mu.Lock()
	if d.closed || (d.failures == 0 && d.reason == "") {
		d.mu.Unlock()
		return
	}
	d.failures = 0
	d.reason = ""
	d.retryAt = time.Time{}
	status := d.statusLocked(d.engine.clock.Now())
	d.mu.Unlock()
	d.session.publish(MeetingEvent{Type: EventDetectorStatus, Detector: &status})
}

func (d *candidateEngine) statusLocked(now time.Time) DetectorStatus {
	status := DetectorStatus{Enabled: d.enabled, Reason: d.reason, State: "ready"}
	if d.reason == "" {
		return status
	}
	status.Retryable = d.reason != ReasonBudgetExceeded && d.reason != reasonSignalResearchUnavailable
	if !d.enabled {
		status.State = "unavailable"
	} else {
		status.State = "retrying"
	}
	if d.reason == ReasonBudgetExceeded {
		status.State = "budget_stopped"
	}
	if d.enabled {
		status.RetryAfterMS = max(d.retryAt.Sub(now).Milliseconds(), 0)
	}
	return status
}

// retryNow is de handmatige herstelknop onder een degradatiemelding.
func (d *candidateEngine) retryNow() string {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return reasonDetectorConfiguration
	}
	switch {
	case d.reason == reasonSignalResearchUnavailable:
		d.mu.Unlock()
		return reasonSignalResearchUnavailable
	case d.reason == ReasonBudgetExceeded:
		d.mu.Unlock()
		return ReasonBudgetExceeded
	case d.engine.llm == nil:
		d.mu.Unlock()
		return ReasonLLMUnavailable
	}
	d.enabled = true
	d.failures = 0
	d.retryAt = time.Time{}
	// Een handmatige poging mag niet op de cadans van de vorige call blijven
	// wachten: de gebruiker vraagt er nú om.
	d.lastEval = d.engine.clock.Now().Add(-d.cfg.Interval)
	status := d.statusLocked(d.engine.clock.Now())
	d.mu.Unlock()
	d.session.publish(MeetingEvent{Type: EventDetectorStatus, Detector: &status})
	d.maybeEvaluate(d.engine.clock.Now())
	return ""
}

func (e *meetingEngine) handleDetectorRetry(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	if s.candidates == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": reasonDetectorConfiguration})
		return
	}
	if reason := s.candidates.retryNow(); reason != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": reason})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "detector": s.candidates.status()})
}

// handleCandidateSpeak — POST .../candidate/speak: de knop "laat horen" onder
// het kandidaatblok. Hij gaat langs de stilte-eis, de lat en de cooldown: de
// gebruiker vraagt er zelf om. Zonder klaarliggende kandidaat is er niets te
// laten horen, en dat is een 409, geen stille 202.
func (e *meetingEngine) handleCandidateSpeak(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	// Een gepauzeerde of uitgezette modus geeft een eigen antwoord: anders
	// leest "de duif heeft nu niets klaarliggen" als een lege duif, terwijl de
	// bijdrage er wel degelijk ligt en alleen niet mag klinken.
	if st := s.governorState(); !st.Autonomous || st.Paused {
		writeJSON(w, http.StatusConflict, apiError{
			Error: "autonomy_paused",
			Message: map[string]string{
				"en": "Joining in on its own is paused. Resume it, or use the button.",
				"nl": "Zelfstandig meedoen staat stil. Hervat het, of gebruik de knop.",
			},
		})
		return
	}
	if s.candidates == nil || !s.candidates.speakNow() {
		writeJSON(w, http.StatusConflict, apiError{
			Error: "no_candidate",
			Message: map[string]string{
				"en": "The pigeon has nothing ready to say right now.",
				"nl": "De duif heeft nu niets klaarliggen om te zeggen.",
			},
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func (d *candidateEngine) close() {
	d.mu.Lock()
	d.closed = true
	d.enabled = false
	d.pending = 0
	d.mu.Unlock()
}
