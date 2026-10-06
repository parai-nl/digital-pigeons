package duif

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// Het arousal-model en de geluidsbanden van de non-verbale duif. Twee
// ontwerpregels:
//
//  1. Arousal is sessiestand, geen detectorstand. De meter hangt aan de sessie
//     en zakt op de klok van de sessie, zodat de handmatige knop en de
//     UI-meter blijven werken met een uitgeschakelde of kapotte detector
//     (de bediening hangt nooit van de detector af).
//  2. De bandkeuze is een pure functie van de meterstand en de geluidenset,
//     met een injecteerbare trekking — willekeurig in productie, voorspelbaar
//     in tests.

// arousalEpsilon is de kleinste verandering die het melden waard is; zonder
// deze drempel zou elke hartslag een arousal-frame kosten.
const arousalEpsilon = 0.01

// manualArousalImpulse is de zet die de handmatige "de duif grijpt in"-knop de
// meter geeft: merkbaar, maar geen alarm uit het niets.
const manualArousalImpulse = 0.5

// bandCeilings zijn de bovengrenzen van de banden: onder 0,34 rustig, onder
// 0,67 nadrukkelijk, daarboven alarm.
const (
	bandCalmCeiling      = 0.34
	bandInsistentCeiling = 0.67
)

// bandForArousal vertaalt de meterstand naar de band waaruit een geluid komt.
func bandForArousal(arousal float64) SoundBand {
	switch {
	case arousal < bandCalmCeiling:
		return BandCalm
	case arousal < bandInsistentCeiling:
		return BandInsistent
	default:
		return BandAlarm
	}
}

// decayArousal is de leaky integrator: halveringstijd halfLife, begrensd op
// [0,1]. Zonder verstreken tijd of halveringstijd verandert er niets.
func decayArousal(a float64, dt, halfLife time.Duration) float64 {
	if a <= 0 || dt <= 0 || halfLife <= 0 {
		return clampFloat(a, 0, 1)
	}
	return clampFloat(a*math.Pow(0.5, dt.Seconds()/halfLife.Seconds()), 0, 1)
}

// arousalMeter is de continue 0–1-waarde van één sessie.
type arousalMeter struct {
	mu       sync.Mutex
	value    float64
	at       time.Time
	halfLife time.Duration
}

func newArousalMeter(start time.Time, halfLife time.Duration) *arousalMeter {
	return &arousalMeter{at: start, halfLife: halfLife}
}

// bump is `arousal = max(verval(arousal, Δt), impuls)`.
func (a *arousalMeter) bump(now time.Time, impulse float64) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := decayArousal(a.value, now.Sub(a.at), a.halfLife)
	if impulse > v {
		v = impulse
	}
	a.value = clampFloat(v, 0, 1)
	a.at = now
	return a.value
}

// decay laat de meter zakken tot `now`. De tweede returnwaarde zegt of de
// meter zichtbaar bewoog — alleen dan is het een frame naar de UI waard.
func (a *arousalMeter) decay(now time.Time) (float64, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	prev := a.value
	a.value = decayArousal(prev, now.Sub(a.at), a.halfLife)
	a.at = now
	return a.value, math.Abs(a.value-prev) >= arousalEpsilon
}

// level is de meterstand nu, zonder hem bij te werken.
func (a *arousalMeter) level() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.value
}

// randomIndex is de standaardtrekking binnen een band.
func randomIndex(n int) int {
	if n <= 1 {
		return 0
	}
	return rand.IntN(n)
}

// pickSound kiest een geluid uit de band die bij `arousal` hoort. Is die band
// leeg, dan schuift de keuze naar de dichtstbijzijnde bezette band — een duif
// met een half gevulde set zwijgt niet, ze koert net iets anders.
func pickSound(sounds []Sound, arousal float64, pick func(int) int) (Sound, bool) {
	return pickSoundAvoid(sounds, arousal, pick, "")
}

// Avoid an immediate repeat when the selected technical variation band has
// another eligible asset. A one-file band remains usable; no emotion follows
// from a band or from the choice of a recording.
func pickSoundAvoid(sounds []Sound, arousal float64, pick func(int) int, previous string) (Sound, bool) {
	if len(sounds) == 0 {
		return Sound{}, false
	}
	want := bandForArousal(arousal)
	start := 0
	for i, b := range soundBands {
		if b == want {
			start = i
		}
	}
	for offset := range len(soundBands) {
		for _, sign := range [2]int{-1, 1} {
			i := start + sign*offset
			if offset == 0 && sign == 1 {
				continue // de eigen band is al bekeken
			}
			if i < 0 || i >= len(soundBands) {
				continue
			}
			var band []Sound
			for _, s := range sounds {
				if s.Band == soundBands[i] {
					band = append(band, s)
				}
			}
			if len(band) == 0 {
				continue
			}
			if len(band) > 1 && previous != "" {
				var alternatives []Sound
				for _, sound := range band {
					if sound.ID != previous {
						alternatives = append(alternatives, sound)
					}
				}
				if len(alternatives) > 0 {
					band = alternatives
				}
			}
			n := pick(len(band))
			if n < 0 || n >= len(band) {
				n = 0
			}
			return band[n], true
		}
	}
	return Sound{}, false
}
