package duif

import (
	"time"
	"unicode"
)

// DetectionSnapshot is het venster van één evaluatie: wat er nieuw is sinds de
// vorige evaluatie, plus een staart context op leeftijd, binnen het bestaande
// segmentplafond en een hard woordplafond.
func (r *RollingTranscript) DetectionSnapshot(now time.Time, after uint64, cfg detectConfig) transcriptWindow {
	filtered := &RollingTranscript{}
	for _, s := range r.segments {
		if cfg.ContextAge > 0 && now.Sub(s.At) >= cfg.ContextAge {
			continue
		}
		filtered.segments = append(filtered.segments, s)
	}
	win := filtered.Since(after, cfg.ContextSegments, cfg.MaxWindowWords)
	// The final segment may itself exceed the budget. Keep its literal suffix.
	if cfg.MaxWindowWords > 0 && win.Words > cfg.MaxWindowWords && len(win.Segments) == 1 {
		text := win.Segments[0].Text
		starts := []int{}
		inWord := false
		for i, c := range text {
			if unicode.IsSpace(c) {
				inWord = false
			} else if !inWord {
				starts = append(starts, i)
				inWord = true
			}
		}
		// Het sprekerlabel eet van hetzelfde budget; wat overblijft is voor de
		// tekst. Ten minste één woord, anders zou een lang label de enige
		// bijdrage in het venster wegsnijden. Het budget kan ook groter zijn
		// dan de tekst zelf — dan valt er niets af te kappen.
		budget := max(cfg.MaxWindowWords-countWords(win.Segments[0].Speaker), 1)
		if budget < len(starts) {
			win.Segments[0].Text = text[starts[len(starts)-budget]:]
		}
		win.Words = segmentWords(win.Segments[0])
	}
	return win
}

func (w transcriptWindow) through(seq uint64) transcriptWindow {
	out := transcriptWindow{}
	split := len(w.Segments) - w.Fresh
	for i, s := range w.Segments {
		if s.Seq <= seq {
			out.Segments = append(out.Segments, s)
			if i >= split {
				out.Fresh++
			}
		}
	}
	out.Words = countSegmentWords(out.Segments)
	return out
}
