package duif

import (
	"net/http"
	"sync"
	"time"
)

const maxTimingMarks = 96

type timingMark struct {
	Stage      string `json:"stage"`
	AtMS       int64  `json:"atMs"`
	DurationMS int64  `json:"durationMs,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type timingSnapshot struct {
	Totals map[string]int `json:"totals"`
	Recent []timingMark   `json:"recent"`
}

// Alleen in code gekozen fasen en reason-codes; nooit vrij model-/usertekst.
// De vaste ring begrenst geheugen ook tijdens lange workshops.
type meetingDiagnostics struct {
	mu     sync.Mutex
	start  time.Time
	closed bool
	totals map[string]int
	recent []timingMark
}

func newMeetingDiagnostics(start time.Time) *meetingDiagnostics {
	return &meetingDiagnostics{start: start, totals: map[string]int{}}
}
func (d *meetingDiagnostics) mark(now time.Time, stage, reason string, duration time.Duration) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.totals[stage]++
	d.recent = append(d.recent, timingMark{Stage: stage, AtMS: max(now.Sub(d.start).Milliseconds(), 0), DurationMS: max(duration.Milliseconds(), 0), Reason: reason})
	if len(d.recent) > maxTimingMarks {
		d.recent = append([]timingMark(nil), d.recent[len(d.recent)-maxTimingMarks:]...)
	}
}
func (d *meetingDiagnostics) snapshot() timingSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := timingSnapshot{Totals: map[string]int{}, Recent: append([]timingMark(nil), d.recent...)}
	for k, v := range d.totals {
		out.Totals[k] = v
	}
	return out
}
func (d *meetingDiagnostics) clear() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.totals = map[string]int{}
	d.recent = nil
}

// Eigenaarschap wordt identiek aan de andere sessieroutes gecontroleerd.
// Geen downloadbestand, inhoud, bron-ID of signaleringsverklaring.
func (e *meetingEngine) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	s, ok := e.resolve(w, r)
	if !ok {
		return
	}
	bar := s.persona.Spreeklat
	out := map[string]any{"schemaVersion": 2, "candidateModel": e.detectModel, "timing": s.diagnostics.snapshot(), "parameters": map[string]any{
		"minimumWords": s.cfg.Detect.MinWords, "intervalMs": s.cfg.Detect.Interval.Milliseconds(),
		"contextAgeMs": s.cfg.Detect.ContextAge.Milliseconds(), "windowWords": s.cfg.Detect.MaxWindowWords,
		"candidateTimeoutMs": s.cfg.Detect.Timeout.Milliseconds(), "quietMs": s.cfg.Detect.Quiet.Milliseconds(),
		"pauseCheckMs": 250, "playbackTimeoutMs": playbackTimeout.Milliseconds(),
		"maximumFailures": maxDetectorFailures,
		"speakBar": map[string]any{
			"threshold": bar.Drempel, "cooldownMs": bar.Cooldown(1).Milliseconds(),
			"cooldownGrowthMs": time.Duration(bar.CooldownGroeiSeconden) * time.Second / time.Millisecond,
			"cooldownMaxMs":    time.Duration(bar.CooldownMaxSeconden) * time.Second / time.Millisecond,
			"topicCooldownMs":  bar.TopicCooldown().Milliseconds(), "polite": bar.Polite(),
			"withoutSources": bar.WithoutSources(),
		},
	}}
	if s.candidates != nil {
		out["detector"] = s.candidates.status()
		out["evaluations"] = s.candidates.counters()
		out["governor"] = s.candidates.gov.verdictCounts()
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}
