package duif

import (
	"sync"
	"time"
)

// De governor is een klok en niets meer. Hij beslist wannéér een
// klaarliggende kandidaat mag klinken, nooit óf de inhoud deugt: dat oordeel
// heeft het model al geveld toen het `SPEAK` of `PASS` schreef.
//
// Wat er daarmee weg is, is precies wat de duif in een echt gesprek het zwijgen
// oplegde: de verstheidspoorten (er mocht na de aanleiding niets meer gezegd
// zijn), de letterlijk-citaateis en het sessieplafond. Een kandidaat veroudert
// nu niet — hij wordt vervángen zodra het gesprek verder is. Ook de
// beleefdheidswachttijd is standaard weg: een duif die op een gaatje wachtte, zweeg. Wat
// overblijft is de eigen lat van de duif en haar eigen rust na spreken. Wordt
// `beleefdheid` in de config weer aangezet, dan komt de stilte-eis terug —
// staat hij uit, dan valt zij midden in de zin in.
//
// De klok is een pure beslisser over tijd en tellers: geen transcript, geen
// LLM, geen opslag.

// governorVerdict is de uitkomst van één weging. Alles behalve verdictAdmit
// betekent: de duif zwijgt nog even, en de vergadering merkt er niets van.
type governorVerdict string

const (
	verdictNone           governorVerdict = ""      // er lag geen kandidaat
	verdictAdmit          governorVerdict = "admit" // laat maar horen
	verdictPaused         governorVerdict = "paused"
	verdictPresence       governorVerdict = "presence_check"  // "Ben je er nog?" staat open
	verdictDisabled       governorVerdict = "disabled"        // zelfstandig meedoen staat uit
	verdictBusy           governorVerdict = "busy"            // de duif is al aan het woord
	verdictBelowThreshold governorVerdict = "below_threshold" // onder de spreeklat van deze duif
	verdictCooldown       governorVerdict = "cooldown"        // haar eigen rust loopt nog
	verdictTopicCooldown  governorVerdict = "topic_cooldown"  // dit thema had zij net
	verdictWaitQuiet      governorVerdict = "wait_quiet"      // er wordt nog gepraat
)

// sensitivitySpread bepaalt hoe ver de gevoeligheid van een thema de spreeklat
// verschuift: gevoeligheid 1,0 haalt hem 0,2 omlaag, 0,0 zet hem 0,2 omhoog.
const sensitivitySpread = 0.4

// thresholdFloor/Ceiling houden de lat binnen een zinnige band, ook bij
// extreme combinaties van drempel, gretigheid en gevoeligheid.
const (
	thresholdFloor   = 0.05
	thresholdCeiling = 0.95
)

func clampFloat(v, lo, hi float64) float64 {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

// governorState is de sessiestand op het moment van wegen. Hij komt van de
// sessie, niet uit de governor: de governor houdt alleen zijn eigen tellers bij.
type governorState struct {
	Autonomous bool
	Paused     bool
	Eagerness  string
	// Cooldown is een expliciete sessie-override op de rust van deze duif;
	// 0 betekent: gebruik haar eigen spreeklat.
	Cooldown             time.Duration
	Speaking             bool
	PresencePending      bool      // "Ben je er nog?" staat open: niets nieuws beginnen
	LastUtterance        time.Time // laatste duif-uiting, ook via de knop
	LastSpeechActivity   time.Time // laatste teken van menselijke spraak
	UncommittedSpeech    bool      // er loopt een onafgemaakte uiting
	ListeningUnavailable bool
}

type governorCounters struct {
	admitted, blocked int
}

// governor is de klok van één sessie.
type governor struct {
	bar   *SpeakBar
	quiet time.Duration

	mu        sync.Mutex
	admitted  int
	blocked   int
	byVerdict map[string]int
	lastAdmit time.Time
	lastTopic map[string]time.Time
}

func newGovernor(bar *SpeakBar, quiet time.Duration) *governor {
	return &governor{bar: bar, quiet: quiet, lastTopic: map[string]time.Time{}, byVerdict: map[string]int{}}
}

func (g *governor) counters() governorCounters {
	g.mu.Lock()
	defer g.mu.Unlock()
	return governorCounters{admitted: g.admitted, blocked: g.blocked}
}

// spokenCount is het aantal doorgelaten bijdragen in deze sessie; de oplopende
// cooldown rekent ermee.
func (g *governor) spokenCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.admitted
}

// cooldownFor is de rust die nú geldt: de sessie-override als die er is,
// anders de oplopende eigen rust van deze duif.
func (g *governor) cooldownFor(st governorState) time.Duration {
	if st.Cooldown > 0 {
		return st.Cooldown
	}
	return g.bar.Cooldown(g.admitted + 1)
}

// assess weegt een kandidaat zonder iets te veranderen. De sessielus roept hem
// elke tik aan; pas als hij verdictAdmit zegt, boekt admit de beslissing.
func (g *governor) assess(now time.Time, c *meetingCandidate, sensitivity float64, st governorState) governorVerdict {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.assessLocked(now, c, sensitivity, st)
}

func (g *governor) assessLocked(now time.Time, c *meetingCandidate, sensitivity float64, st governorState) governorVerdict {
	if c == nil {
		return verdictNone
	}
	switch {
	case st.Paused:
		return verdictPaused
	case !st.Autonomous:
		return verdictDisabled
	case st.PresencePending:
		// De kandidaat blijft liggen: wie "ja, ga door" zegt, kan haar alsnog
		// horen als het gesprek er nog over gaat.
		return verdictPresence
	case st.Speaking:
		return verdictBusy
	}
	// Aangesproken worden is geen interruptie: de duif wacht op het zinseinde
	// en gaat dan langs haar eigen rust en lat. Wie haar iets vraagt hoort
	// geen stilte omdat zij drie minuten geleden ook al iets zei.
	if c.Addressed {
		if st.UncommittedSpeech || st.ListeningUnavailable {
			return verdictWaitQuiet
		}
		return verdictAdmit
	}
	if c.Score < g.bar.Threshold(st.Eagerness, sensitivity) {
		return verdictBelowThreshold
	}
	last := st.LastUtterance
	if g.lastAdmit.After(last) {
		last = g.lastAdmit
	}
	if !last.IsZero() && now.Sub(last) < g.cooldownFor(st) {
		return verdictCooldown
	}
	if topicCooldown := g.bar.TopicCooldown(); topicCooldown > 0 && c.TopicID != "" {
		if at, ok := g.lastTopic[c.TopicID]; ok && now.Sub(at) < topicCooldown {
			return verdictTopicCooldown
		}
	}
	// Alleen een duif die in de config nog op beleefdheid staat, wacht hier
	// op een gaatje. Standaard staat dat uit en breekt zij direct in.
	if g.bar.Polite() {
		if st.UncommittedSpeech || st.ListeningUnavailable {
			return verdictWaitQuiet
		}
		if !st.LastSpeechActivity.IsZero() && now.Sub(st.LastSpeechActivity) < g.quiet {
			return verdictWaitQuiet
		}
	}
	return verdictAdmit
}

// admit boekt één doorgelaten kandidaat: de rust begint te lopen en het thema
// gaat op zijn eigen klok.
func (g *governor) admit(now time.Time, c *meetingCandidate) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admitted++
	g.lastAdmit = now
	g.byVerdict[string(verdictAdmit)]++
	if c != nil && c.TopicID != "" {
		g.lastTopic[c.TopicID] = now
	}
}

// note telt een geblokkeerde weging. De sessielus weegt elke 250 ms opnieuw;
// alleen een verandering van uitkomst is het tellen waard, anders staat er na
// een minuut wachten 240 keer "wait_quiet" in de diagnostiek.
func (g *governor) note(v governorVerdict) {
	if v == verdictNone || v == verdictAdmit {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.blocked++
	g.byVerdict[string(v)]++
}

func (g *governor) verdictCounts() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]int{}
	for k, v := range g.byVerdict {
		out[k] = v
	}
	return out
}
