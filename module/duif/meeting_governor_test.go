package duif

import (
	"testing"
	"time"
)

// De governor is een klok: hij beslist wanneer een klaarliggende
// bijdrage mag klinken, nooit of de inhoud deugt. Deze tests leggen precies dat
// verschil vast — en de volgorde waarin hij weigert.

// testBar is de spreeklat van de kennispartner zoals hij in de configuratie
// staat: geen beleefdheidswachttijd.
func testBar() *SpeakBar {
	return &SpeakBar{
		Drempel: 0.55, CooldownSeconden: 45, CooldownGroeiSeconden: 15,
		CooldownMaxSeconden: 120, ThemaCooldownSeconden: 180, Beleefdheid: false,
	}
}

// politeBar is dezelfde lat met de wachtknop weer aan: hij bestaat nog voor
// wie het wachten terug wil zetten, en dat hoort te blijven werken.
func politeBar() *SpeakBar {
	bar := testBar()
	bar.Beleefdheid = true
	return bar
}

func testClock(bar *SpeakBar) *governor {
	if bar == nil {
		bar = testBar()
	}
	return newGovernor(bar, 1500*time.Millisecond)
}

func candidateAt(now time.Time, topicID string, score float64) *meetingCandidate {
	return &meetingCandidate{ID: newID(), TopicID: topicID, Score: score, Text: "een bijdrage", At: now}
}

func openState() governorState {
	return governorState{Autonomous: true, Eagerness: EagernessNormal}
}

// De lat van deze duif, verschoven door gretigheid en themagevoeligheid.
func TestClock_ThresholdComesFromThePigeonsOwnBar(t *testing.T) {
	bar := testBar()
	g := testClock(bar)
	now := time.Now()
	st := openState()

	if v := g.assess(now, candidateAt(now, "urban-ecology", 0.54), 0.5, st); v != verdictBelowThreshold {
		t.Errorf("net onder de lat = %q, wil %q", v, verdictBelowThreshold)
	}
	if v := g.assess(now, candidateAt(now, "urban-ecology", 0.56), 0.5, st); v != verdictAdmit {
		t.Errorf("net boven de lat = %q, wil %q", v, verdictAdmit)
	}
	// Een gevoelig thema haalt de lat omlaag.
	if v := g.assess(now, candidateAt(now, "urban-ecology", 0.40), 1.0, st); v != verdictAdmit {
		t.Errorf("gevoelig thema = %q, wil %q", v, verdictAdmit)
	}
	// Een terughoudende vergadering zet hem omhoog.
	reserved := openState()
	reserved.Eagerness = EagernessReserved
	if v := g.assess(now, candidateAt(now, "urban-ecology", 0.60), 0.5, reserved); v != verdictBelowThreshold {
		t.Errorf("terughoudend = %q, wil %q", v, verdictBelowThreshold)
	}
}

// Zonder zelfstandig meedoen of na een Stop komt er niets door.
func TestClock_AutonomyOffAndPaused_LetNothingThrough(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, governorState{}); v != verdictDisabled {
		t.Errorf("autonomie uit = %q, wil %q", v, verdictDisabled)
	}
	paused := openState()
	paused.Paused = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, paused); v != verdictPaused {
		t.Errorf("gepauzeerd = %q, wil %q", v, verdictPaused)
	}
	// Gepauzeerd wint van autonomie-uit: Stop is de sterkste barrière.
	both := governorState{Paused: true}
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, both); v != verdictPaused {
		t.Errorf("beide = %q, wil %q", v, verdictPaused)
	}
}

// Een duif die al aan het woord is krijgt geen tweede spreekslot.
func TestClock_WhileSpeaking_IsBusy(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	st.Speaking = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictBusy {
		t.Errorf("tijdens spreken = %q, wil %q", v, verdictBusy)
	}
}

// Zolang "Ben je er nog?" openstaat begint de duif niets nieuws, ook niet met
// een kandidaat ruim boven de lat of een directe vraag. Stop blijft
// de sterkste barrière.
func TestClock_PresenceCheck_HoldsEveryCandidate(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	st.PresencePending = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictPresence {
		t.Errorf("vraag open = %q, wil %q", v, verdictPresence)
	}
	addressed := candidateAt(now, "urban-ecology", 1)
	addressed.Addressed = true
	if v := g.assess(now, addressed, 0.5, st); v != verdictPresence {
		t.Errorf("aangesproken, vraag open = %q, wil %q", v, verdictPresence)
	}
	st.Paused = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictPaused {
		t.Errorf("gepauzeerd en vraag open = %q, wil %q", v, verdictPaused)
	}
}

// De cooldown telt ook de beurten van de knop mee, en loopt op.
func TestClock_CooldownCountsButtonTurnsAndRises(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	st.LastUtterance = now.Add(-30 * time.Second)
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictCooldown {
		t.Fatalf("30 s na een beurt = %q, wil %q", v, verdictCooldown)
	}
	st.LastUtterance = now.Add(-50 * time.Second)
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Fatalf("50 s na een beurt = %q, wil %q", v, verdictAdmit)
	}

	// Na elke eigen bijdrage wordt de rust langer: de rem die in de plaats
	// kwam van het sessieplafond.
	g.admit(now, candidateAt(now, "urban-ecology", 1))
	st.LastUtterance = now.Add(-50 * time.Second)
	if v := g.assess(now, candidateAt(now, "nesting-and-shelter", 1), 0.5, st); v != verdictCooldown {
		t.Errorf("na één bijdrage is 50 s te kort: %q", v)
	}
}

// Een expliciete sessie-cooldown vervangt de eigen rust van de duif.
func TestClock_SessionCooldownOverridesTheBar(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	st.LastUtterance = now.Add(-10 * time.Second)
	st.Cooldown = 5 * time.Second
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("met een korte sessie-cooldown = %q, wil %q", v, verdictAdmit)
	}
}

// De themacooldown houdt de duif van haar stokpaardje af; een duif zonder
// themacooldown (de signalerende) merkt er niets van.
func TestClock_TopicCooldownIsPerPigeon(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	g.admit(now.Add(-time.Minute), candidateAt(now, "urban-ecology", 1))
	st.LastUtterance = now.Add(-10 * time.Minute)
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictTopicCooldown {
		t.Errorf("hetzelfde thema binnen drie minuten = %q, wil %q", v, verdictTopicCooldown)
	}
	if v := g.assess(now, candidateAt(now, "nesting-and-shelter", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("een ander thema = %q, wil %q", v, verdictAdmit)
	}

	free := testClock(&SpeakBar{Drempel: 0.3, CooldownSeconden: 20})
	free.admit(now.Add(-30*time.Second), candidateAt(now, "nesting", 1))
	loose := openState()
	loose.LastUtterance = now.Add(-time.Minute)
	if v := free.assess(now, candidateAt(now, "nesting", 1), 0.5, loose); v != verdictAdmit {
		t.Errorf("zonder themacooldown = %q, wil %q", v, verdictAdmit)
	}
}

// De duif wacht niet meer op een gaatje. Een kandidaat boven de lat en
// buiten de cooldown klinkt direct: terwijl er nog gepraat wordt, midden in een
// onafgemaakte zin, en ook als er helemaal niet meegeluisterd kan worden.
func TestClock_NoPolitenessWait_BreaksInWhileOthersSpeak(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	st.LastSpeechActivity = now
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("er wordt nog gepraat = %q, wil %q", v, verdictAdmit)
	}
	st.LastSpeechActivity = now.Add(-500 * time.Millisecond)
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("nog geen 1,5 s stil = %q, wil %q", v, verdictAdmit)
	}
	st.UncommittedSpeech = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("met een onafgemaakte uiting = %q, wil %q", v, verdictAdmit)
	}
	st.UncommittedSpeech = false
	st.ListeningUnavailable = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("zonder luisterstand = %q, wil %q", v, verdictAdmit)
	}
}

// De wachtknop is niet gesloopt, alleen uitgezet: een duif die in de config wél
// op beleefdheid staat, wacht nog steeds op stilte en op een afgemaakte zin.
func TestClock_PolitenessKnobStillWaitsWhenSwitchedOn(t *testing.T) {
	g := testClock(politeBar())
	now := time.Now()
	st := openState()
	st.LastSpeechActivity = now.Add(-500 * time.Millisecond)
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictWaitQuiet {
		t.Errorf("nog geen 1,5 s stil = %q, wil %q", v, verdictWaitQuiet)
	}
	st.LastSpeechActivity = now.Add(-2 * time.Second)
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictAdmit {
		t.Errorf("na 2 s stilte = %q, wil %q", v, verdictAdmit)
	}
	st.UncommittedSpeech = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictWaitQuiet {
		t.Errorf("met een onafgemaakte uiting = %q, wil %q", v, verdictWaitQuiet)
	}
	st.UncommittedSpeech = false
	st.ListeningUnavailable = true
	if v := g.assess(now, candidateAt(now, "urban-ecology", 1), 0.5, st); v != verdictWaitQuiet {
		t.Errorf("zonder luisterstand = %q, wil %q", v, verdictWaitQuiet)
	}
}

// De signalerende duif kende de wachttijd al niet: een geluid onderbreekt
// niemand.
func TestClock_SignalingPigeonDoesNotWaitForQuiet(t *testing.T) {
	g := testClock(&SpeakBar{Drempel: 0.30, CooldownSeconden: 20})
	now := time.Now()
	st := openState()
	st.LastSpeechActivity = now
	if v := g.assess(now, candidateAt(now, "nesting", 0.4), 0.5, st); v != verdictAdmit {
		t.Errorf("de signalerende duif wachtte op stilte: %q", v)
	}
}

// Aangesproken worden gaat langs lat, rust en themacooldown, maar wacht wel op
// een afgemaakte zin.
func TestClock_AddressedBypassesTheBarButWaitsForTheSentence(t *testing.T) {
	g := testClock(nil)
	now := time.Now()
	st := openState()
	st.LastUtterance = now
	g.admit(now, candidateAt(now, "urban-ecology", 1))

	c := candidateAt(now, "urban-ecology", 0.05)
	c.Addressed = true
	if v := g.assess(now, c, 0.5, st); v != verdictAdmit {
		t.Errorf("aangesproken = %q, wil %q", v, verdictAdmit)
	}
	st.UncommittedSpeech = true
	if v := g.assess(now, c, 0.5, st); v != verdictWaitQuiet {
		t.Errorf("aangesproken midden in een zin = %q, wil %q", v, verdictWaitQuiet)
	}
}

// Er is geen sessieplafond meer: een lang gesprek loopt niet vast op een teller.
func TestClock_NoSessionCap(t *testing.T) {
	g := testClock(&SpeakBar{Drempel: 0.5, CooldownSeconden: 1})
	base := time.Now()
	for i := range 12 {
		now := base.Add(time.Duration(i) * time.Minute)
		st := openState()
		c := candidateAt(now, "urban-ecology", 1)
		if v := g.assess(now, c, 0.5, st); v != verdictAdmit {
			t.Fatalf("bijdrage %d werd geweigerd met %q", i+1, v)
		}
		g.admit(now, c)
	}
	if got := g.counters().admitted; got != 12 {
		t.Errorf("doorgelaten = %d, wil 12", got)
	}
}

// Zonder kandidaat is er niets te wegen.
func TestClock_WithoutCandidate_IsANoOp(t *testing.T) {
	g := testClock(nil)
	if v := g.assess(time.Now(), nil, 0.5, openState()); v != verdictNone {
		t.Errorf("zonder kandidaat = %q, wil leeg", v)
	}
}

// De non-verbale duif brengt haar eigen lat mee wanneer zij aan tafel zit.
func TestMeetingConfig_NonverbalPigeon_HasItsOwnSpeakBar(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	verbal, err := e.open("user-1", "individualist", "en")
	if err != nil {
		t.Fatal(err)
	}
	nonverbal, err := e.open("user-1", "nonverbal", "en")
	if err != nil {
		t.Fatal(err)
	}
	// Geen van de drie wacht nog op een stilte; het verschil zit in de
	// lat en de rust, niet in beleefdheid.
	if verbal.persona.Spreeklat.Polite() {
		t.Error("de pratende duif wacht nog op een gaatje")
	}
	if nonverbal.persona.Spreeklat.Polite() {
		t.Error("de non-verbale duif koert bewust dwars door mensen heen")
	}
	if nonverbal.persona.Spreeklat.Cooldown(1) >= verbal.persona.Spreeklat.Cooldown(1) {
		t.Errorf("geluid is minder disruptief: cooldown non-verbaal (%s) hoort korter dan pratend (%s)",
			nonverbal.persona.Spreeklat.Cooldown(1), verbal.persona.Spreeklat.Cooldown(1))
	}
}

// De sessie-instellingen komen mee bij het aan tafel zetten.
func TestMeetingSessions_CreateAcceptsDetectionSettings(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "lang": "nl",
		"autonomous": true, "eagerness": "reserved", "cooldownSeconds": 60,
	})
	s := rig.session(t, id)
	if !s.settings.Autonomous || s.settings.Eagerness != EagernessReserved || s.settings.Cooldown != time.Minute {
		t.Fatalf("sessie-instellingen = %+v", s.settings)
	}
}

func TestMeetingSessions_CreateRejectsUnknownEagerness(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	status, body := rig.do(t, "POST", "/v1/meeting/sessions", map[string]any{
		"pigeonId": "individualist", "eagerness": "razend",
	})
	if status != 400 {
		t.Fatalf("een onbekende gretigheidsstand hoort een 400 te zijn, kreeg %d: %s", status, body)
	}
}

func TestMeetingSessions_CreateRejectsNegativeCooldown(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	status, body := rig.do(t, "POST", "/v1/meeting/sessions", map[string]any{
		"pigeonId": "individualist", "cooldownSeconds": -5,
	})
	if status != 400 {
		t.Fatalf("een negatieve cooldown hoort een 400 te zijn, kreeg %d: %s", status, body)
	}
}
