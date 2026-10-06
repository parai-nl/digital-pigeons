package duif

import (
	"net/http"
	"regexp"
	"strconv"
	"time"
)

const (
	EventPlaybackState = "playback_state"
	playbackTimeout    = 2 * time.Minute
)

type playbackTracker struct {
	expected       bool
	started        bool
	complete       bool // alle serveraudio is verstuurd
	ended          bool
	generationDone bool
	failed         bool
	// answerFailed: de antwoordketen liep vast (kennisbank, taalmodel, een
	// paniek). Dat is iets anders dan "het geluid speelde niet af" en pauzeert
	// zelfstandig meedoen dus níét — de gebruiker ziet de fout via EventError,
	// en alleen de storingsreeks van de generator zelf degradeert de autonomie.
	// De vlag hoort hier omdat zij dezelfde levensduur heeft als de rest van de
	// beurtstand: elke reset van deze struct wist haar mee.
	answerFailed bool
	deadline     time.Time // vast vanaf eerste frame; acknowledgements verlengen niet
	startedAt    time.Time
}

func (s *meetingSession) turnIDLocked() string { return strconv.FormatUint(s.turnGen, 10) }
func (s *meetingSession) playbackPhaseLocked() string {
	if !s.playback.expected {
		return ""
	}
	if s.playback.ended {
		return "ended"
	}
	if s.playback.started {
		return "playing"
	}
	return "pending"
}

func (s *meetingSession) observeTurnLocked(gen uint64, ev *MeetingEvent) {
	ev.TurnID = strconv.FormatUint(gen, 10)
	now := s.clock.Now()
	switch ev.Type {
	case EventText:
		// De staart van wat de duif zegt is de maatstaf van de tekstpoort
		// (meeting_freshness.go). Hij wordt hier gevuld en niet uit
		// s.contributions gelezen: die krijgt haar bijdrage pas ná de laatste
		// audioframes, en de echo komt binnen terwijl zij nog praat.
		s.noteSpokenTextLocked(ev.Delta)
		if !s.turnTextSeen {
			s.turnTextSeen = true
			s.diagnostics.mark(now, "first_text", "", now.Sub(s.turnStartedAt))
		}
	case EventTTSAudio, EventSound:
		if !s.playback.expected {
			s.playback.expected = true
			s.playback.startedAt = now
			s.playback.deadline = now.Add(playbackTimeout)
			s.diagnostics.mark(now, "first_audio", "", now.Sub(s.turnStartedAt))
		}
		if ev.Type == EventSound {
			s.playback.complete = true
		}
	case EventTTSDone:
		s.playback.complete = ev.Error == ""
		if ev.Error != "" {
			s.playback.failed = true
		}
	case EventError:
		s.playback.answerFailed = true
	}
	if ev.Type == EventResponseDone || ev.Type == EventSound {
		ev.PlaybackTracking = s.settings.PlaybackTracking
		if s.settings.PlaybackTracking {
			ev.Playback = s.playbackPhaseLocked()
		}
	}
}

func (s *meetingSession) releaseTurnLocked(state string) {
	now := s.clock.Now()
	s.speaking = false
	s.cancelTurn = nil
	s.speakUntil = now.Add(s.cfg.SpeakMargin)
	s.lastUtterance = now
	if s.settings.PlaybackTracking && s.playback.expected {
		s.dispatchLocked(MeetingEvent{Type: EventPlaybackState, TurnID: s.turnIDLocked(), Playback: state, Speaking: false, AutonomyPaused: boolPointer(s.autonomyPaused)})
	}
}

// Dezelfde beurt mag maximaal één vast venster spelen. Oudere of voortijdige
// eindmeldingen worden genegeerd; deze route reset geen governor-/kostentellers.
func (s *meetingSession) ackPlayback(turnID, state string) bool {
	s.mu.Lock()
	if s.closed || !s.settings.PlaybackTracking || turnID != s.turnIDLocked() {
		s.mu.Unlock()
		return false
	}
	// Een beurt zonder audio kent geen afspeelvenster: de tekst-only opstelling
	// (geen stemprovider gewired) verwacht nergens geluid. De client sluit zo'n
	// beurt netjes af met "ended" en die melding klopt. Weigeren zou in de
	// interface neerslaan als "het geluid speelde niet volledig af", over een
	// beurt waarin nooit geluid verwacht is. Er valt hier niets bij te
	// houden: endTurn heeft de beurt al vrijgegeven.
	if !s.playback.expected {
		accepted := state == "ended"
		s.mu.Unlock()
		return accepted
	}
	if !s.speaking {
		s.mu.Unlock()
		return false
	}
	var cancel func()
	now := s.clock.Now()
	switch state {
	case "started":
		if !s.playback.started && !s.playback.ended {
			s.playback.started = true
			s.diagnostics.mark(now, "playback_started", "", now.Sub(s.playback.startedAt))
			s.dispatchLocked(MeetingEvent{Type: EventPlaybackState, TurnID: turnID, Playback: "playing", Speaking: true})
		}
	case "ended":
		if !s.playback.complete || s.playback.ended {
			s.mu.Unlock()
			return false
		}
		s.playback.ended = true
		s.diagnostics.mark(now, "playback_ended", "", now.Sub(s.playback.startedAt))
		if s.playback.generationDone {
			s.releaseTurnLocked("ended")
		}
	case "aborted", "failed":
		cancel = s.cancelTurn
		s.autonomyPaused = true
		s.diagnostics.mark(now, "playback_ended", state, now.Sub(s.playback.startedAt))
		s.releaseTurnLocked(state)
		s.playback = playbackTracker{}
		s.turnGen++ // late generator/audioframes mogen niet terugkomen
		settings := s.settingsLocked()
		s.dispatchLocked(MeetingEvent{Type: EventSettings, Settings: &settings, AutonomyPaused: boolPointer(true)})
	default:
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (e *meetingEngine) handlePlayback(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	var req struct {
		TurnID string `json:"turnId"`
		State  string `json:"state"`
	}
	if !decodeMeetingBody(w, r, &req) {
		return
	}
	if req.TurnID == "" || len(req.TurnID) > 32 || (req.State != "started" && req.State != "ended" && req.State != "failed" && req.State != "aborted") {
		te := badRequest(reasonPlaybackState)
		writeJSON(w, te.status, te.body)
		return
	}
	accepted := s.ackPlayback(req.TurnID, req.State)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted, "speaking": s.isSpeaking(), "settings": s.settingsSnapshot()})
}

func boolPointer(v bool) *bool { return &v }

var controlTurnID = regexp.MustCompile(`^[0-9]{1,20}$`)

// Resume resolves exactly the observed control barrier. A request sent before
// a newer Stop must never release that Stop when it arrives late.
func (s *meetingSession) resumeAutonomy(expected string) (SessionSettings, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || expected != s.turnIDLocked() {
		return s.settingsLocked(), false
	}
	s.autonomyPaused = false
	settings := s.settingsLocked()
	s.dispatchLocked(MeetingEvent{Type: EventSettings, Settings: &settings, AutonomyPaused: boolPointer(false)})
	return settings, true
}
func (e *meetingEngine) handleResume(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolveControl(w, r)
	if !ok {
		return
	}
	var req struct {
		TurnID string `json:"turnId"`
	}
	if !decodeMeetingBody(w, r, &req) {
		return
	}
	if !controlTurnID.MatchString(req.TurnID) {
		te := badRequest(reasonResumeTurnID)
		writeJSON(w, te.status, te.body)
		return
	}
	settings, accepted := s.resumeAutonomy(req.TurnID)
	if !accepted {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "meeting_control_changed", "message": map[string]string{
			"en": "The meeting was stopped again or has moved on. Review its current state before resuming.",
			"nl": "De vergadering is opnieuw gestopt of heeft een nieuwe beurt. Bekijk de huidige toestand voordat je hervat.",
		}, "settings": settings})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"autonomyPaused": false, "settings": settings})
}
