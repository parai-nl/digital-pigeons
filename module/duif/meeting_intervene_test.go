package duif

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// --- zelfstandig ingrijpen ---------------------------------------------------

// settings stelt de sessieknoppen live bij (instelbaar tijdens de sessie).
func (rig *meetingRig) settings(t *testing.T, sessionID string, body map[string]any) (int, string) {
	t.Helper()
	return rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+sessionID+"/settings", body)
}

// De kandidaatprompt draagt het interventiekader, de spreeklat van déze duif en
// haar thema's. Dat is waar het oordeel valt: het model beslist of er iets te
// zeggen valt, niet een aparte classificatie.
func TestMeetingIntervention_CandidatePromptCarriesTheBarAndTheGuardrails(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "pest-framing", "Dat woord doet iets [1]."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true, "lang": "nl"})
	rig.post(t, id, "/transcript", map[string]any{"text": "Het zijn gewoon ratten met vleugels."})
	waitFor(t, func() bool { return len(svc.candidateCalls()) > 0 }, "er ging geen kandidaatcall uit")

	system := svc.candidateCalls()[0].Messages[0].Content
	set := testPersonas(t)
	interventie := PromptInput{Personas: set}.meetingInterventionSection("nl")
	for _, marker := range []string{
		interventie,                  // niemand vroeg haar iets
		"eis:M1",                     // de spreeklat van deze duif
		candidateProtocol["nl"][:40], // de kopregelafspraak
		"topicId: pest-framing",      // haar eigen thema's
	} {
		if !strings.Contains(system, marker) {
			t.Errorf("de kandidaatprompt mist %q", marker)
		}
	}
	// De spreeklat van de kennispartner hoort hier niet in te staan.
	bio := mustGet(t, set, "biological")
	if strings.Contains(system, bio.Spreeklat.Instruction("nl")) {
		t.Error("de individualist kreeg de spreeklat van de kennispartner")
	}
}

// Bij de pratende duiven staat zelfstandig meedoen standaard uit, en de knop
// is en blijft de gegarandeerde route.
func TestMeetingIntervention_IsOffByDefault_AndTheButtonStillWorks(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "iets"))
	rig := eagerRig(t, svc, nil)
	// Geen "autonomous" in het verzoek: de configuratie van déze duif beslist,
	// en die staat voor de stakeholder uit.
	id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "eagerness": "eager"})
	es := rig.events(t, id)
	d := rig.candidates(t, id)

	rig.say(t, id, "duiven horen hier thuis, vind ik")
	rig.clock.advance(t, 2*time.Second)
	if d.counters().Started != 0 || len(svc.candidateCalls()) != 0 {
		t.Fatal("met autonomie uit hoort er geen enkele call uit te gaan")
	}
	for _, ev := range es.recorded() {
		if ev.Type == EventIntervention || ev.Type == EventText {
			t.Fatalf("de duif greep ongevraagd in: %q", ev.Type)
		}
	}

	// En de knop doet het onverstoord.
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("de knop hoort altijd te werken: status %d, body %s", status, body)
	}
	if spoken, _ := collectSpokenText(t, es.ch); spoken == "" {
		t.Fatal("de knop leverde geen spreekbeurt op")
	}
}

// Live bijstellen werkt per direct: uitzetten smoort de eerstvolgende evaluatie.
func TestMeetingIntervention_SettingsOffMidSession_StopsTheNextEvaluation(t *testing.T) {
	svc := newCandidateLLM(speak(0.98, "pigeon-rights", "Duiven horen hier thuis [1]."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager", "cooldownSeconds": 1,
	})
	es := rig.events(t, id)
	d := rig.candidates(t, id)

	rig.say(t, id, "duiven horen hier thuis")
	nextEvent(t, es.ch, EventIntervention)
	collectSpokenText(t, es.ch)
	rig.clock.advance(t, 2*time.Second)

	status, body := rig.settings(t, id, map[string]any{"autonomous": false})
	if status != http.StatusAccepted || !strings.Contains(body, `"autonomous":false`) {
		t.Fatalf("instellingen bijstellen: status %d, body %s", status, body)
	}
	before := d.counters().Started
	rig.say(t, id, "en nog eens: duiven horen hier thuis")
	rig.clock.advance(t, 2*time.Second)
	if got := d.counters().Started; got != before {
		t.Errorf("na uitzetten gingen er alsnog %d call(s) uit", got-before)
	}
}

// De cooldown is een sessieknop: korter zetten laat de duif eerder weer in.
func TestMeetingIntervention_SettingsCooldown_ReachesTheClock(t *testing.T) {
	// Twee thema's, zodat de themacooldown de meting van de algemene rust niet
	// vertroebelt.
	svc := newCandidateLLM(
		speak(0.98, "pigeon-rights", "Duiven horen hier thuis [1]."),
		speak(0.98, "city-space", "Het plein is ook van ons [1]."),
	)
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager", "cooldownSeconds": 600,
	})
	es := rig.events(t, id)
	d := rig.candidates(t, id)

	rig.say(t, id, "duiven horen hier thuis")
	nextEvent(t, es.ch, EventIntervention)
	collectSpokenText(t, es.ch)
	rig.clock.advance(t, 2*time.Second)

	rig.say(t, id, "nog eens: duiven horen hier thuis")
	waitForCandidate(t, rig.session(t, id))
	rig.clock.advance(t, 3*time.Second)
	if got := d.gov.counters().admitted; got != 1 {
		t.Fatalf("binnen de cooldown van tien minuten hoort de duif te zwijgen: %d bijdragen", got)
	}

	if status, _ := rig.settings(t, id, map[string]any{"cooldownSeconds": 1}); status != http.StatusAccepted {
		t.Fatal("cooldown bijstellen faalde")
	}
	for range 12 {
		if d.gov.counters().admitted == 2 {
			return
		}
		rig.clock.advance(t, time.Second)
	}
	t.Fatal("met een kortere cooldown hoort de duif weer in te mogen vallen")
}

func TestMeetingSettings_RejectsNonsense(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")
	tests := []struct {
		name string
		body map[string]any
	}{
		{"onbekende gretigheid", map[string]any{"eagerness": "hysterisch"}},
		{"negatieve cooldown", map[string]any{"cooldownSeconds": -1}},
		{"absurde cooldown", map[string]any{"cooldownSeconds": 99999}},
		{"onbekend veld", map[string]any{"volume": 11}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := rig.settings(t, id, tc.body); status != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", status, body)
			}
		})
	}
}

// Een kandidaat die binnenvalt terwijl de duif spreekt komt niet in een
// wachtrij: hij blijft liggen tot zij klaar is, en de vergadering krijgt geen
// tweede pipeline.
func TestMeetingIntervention_CandidateWhileSpeaking_DoesNotQueue(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Een bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	// De rust van de duif loopt nog een uur: de kandidaat blijft liggen in
	// plaats van meteen te klinken, zodat de knop-beurt en de kandidaat
	// tegelijk in beeld zijn.
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)

	rig.say(t, id, "wij vinden er dit van")
	waitForCandidate(t, s)
	// De duif is via de knop aan het woord; de klaarliggende kandidaat blijft
	// liggen en krijgt geen tweede spreekslot.
	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)
	collectSpokenText(t, es.ch)
	if s.heldCandidate() == nil {
		t.Fatal("de kandidaat verdween tijdens de knop-beurt")
	}
	var personas int
	for _, ev := range es.recorded() {
		if ev.Type == EventPersona {
			personas++
		}
	}
	if personas != 1 {
		t.Fatalf("er begonnen %d spreekbeurten, want 1", personas)
	}
}

// Stop geldt ook voor "laat horen". De kandidaat blijft liggen — hij is niet
// verlopen, hij mag alleen niet klinken — en de melding zegt dat ook, in plaats
// van te doen alsof de duif niets heeft.
func TestMeetingIntervention_LetItBeHeard_RespectsStop(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Een bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	rig.say(t, id, "wij willen ze weren van het plein")
	held := waitForCandidate(t, s)

	if status, _ := rig.post(t, id, "/stop", nil); status != http.StatusAccepted && status != http.StatusOK {
		t.Fatalf("stoppen gaf status %d", status)
	}
	status, body := rig.post(t, id, "/candidate/speak", nil)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, body = %s (want 409)", status, body)
	}
	if !strings.Contains(body, "autonomy_paused") {
		t.Errorf("body = %s, want de reden dat het meedoen stilstaat", body)
	}
	if got := s.heldCandidate(); got == nil || got.ID != held.ID {
		t.Errorf("de kandidaat werd opgegeten door een geweigerd verzoek: %+v", got)
	}
	if s.isSpeaking() {
		t.Error("de duif kwam alsnog aan het woord na Stop")
	}
}

// Wie de duif aanspreekt terwijl zij praat, wordt gehoord.
//
// Vóór deze fix kreeg élk gecommit segment de echo-markering zolang de duif
// hoorbaar was, ook een getypte bijdrage. Zo'n segment valt uit het venster én
// uit de cadans: de vraag was daarna nergens meer. Zo verdwenen er achttien
// seconden gesprek, inclusief een directe vraag aan de duif. Nu
// blijft de vraag staan en is hij ná haar beurt de aanleiding.
func TestMeetingIntervention_QuestionDuringHerSpeech_IsEvaluatedAfterwards(t *testing.T) {
	svc := newCandidateLLM(
		addressed(0.99, "pigeon-rights", "Ja, daar vind ik dit van [1]."),
	)
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	s := rig.session(t, id)

	// De duif is hoorbaar aan het woord: een beurt loopt en er is audio
	// onderweg. Precies de stand waarin vroeger alles wat binnenkwam als
	// haar eigen echo werd weggezet.
	gen, _, cancel, ok := s.beginTurn(true)
	if !ok {
		t.Fatal("de beurt begon niet")
	}
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	s.mu.Lock()
	s.playback.expected = true
	audible := s.echoPossibleLocked(s.clock.Now())
	s.mu.Unlock()
	if !audible {
		t.Fatal("de opstelling meet niets: de duif geldt niet als hoorbaar")
	}

	// Iemand spreekt haar rechtstreeks aan terwijl zij praat.
	rig.say(t, id, "duif, wat vind jij daar zelf van")
	win := s.window()
	if len(win) == 0 || win[len(win)-1].Text != "duif, wat vind jij daar zelf van" {
		t.Fatalf("de vraag verdween uit het venster: %+v", win)
	}

	// Zij spreekt uit; de klok laat de kandidaat daarna door.
	s.endTurn(gen)
	cancel()
	rig.clock.advance(t, 2*time.Second)

	nextEvent(t, es.ch, EventPersona)
	answer, _ := collectSpokenText(t, es.ch)
	if answer != "Ja, daar vind ik dit van [1]." {
		t.Errorf("bijdrage = %q, want het antwoord op de directe vraag", answer)
	}
	calls := svc.candidateCalls()
	if len(calls) == 0 {
		t.Fatal("er ging geen enkele evaluatie uit")
	}
	if w := calls[len(calls)-1].Messages[1].Content; !strings.Contains(w, "duif, wat vind jij daar zelf van") {
		t.Errorf("de evaluatie zag de vraag niet:\n%s", w)
	}
}

// De zelfstandige route is voor de kaart dezelfde beurt als de knop-route: de
// bronnen staan er vóór de eerste tekst (anders blijft "[1]" ruwe tekst tot de
// duif is uitgesproken) en text_done komt vóór response_done (anders blijft de
// typcursor de hele spreekduur staan). Zelfde bewaking als
// TestMeetingRespondNow_EventOrder_..., nu voor de kandidaat.
func TestMeetingIntervention_CandidateEventOrder_SourcesThenTextThenTextDoneThenResponseDone(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Noise changes how they use rooftops [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "biological", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	rig.say(t, id, "wat doet vuurwerk met de daken hier")
	waitForCandidate(t, s)

	if status, _ := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Fatal("laat horen werd geweigerd")
	}
	nextEvent(t, es.ch, EventPersona)
	_, evs := collectSpokenText(t, es.ch)
	sources := indexOfEvent(evs, EventSources)
	text := indexOfEvent(evs, EventText)
	textDone := indexOfEvent(evs, EventTextDone)
	done := indexOfEvent(evs, EventResponseDone)
	if sources < 0 || text < 0 || textDone < 0 || done < 0 {
		t.Fatalf("de zelfstandige bijdrage mist een event: %v", eventTypes(evs))
	}
	if !(sources < text && text < textDone && textDone < done) {
		t.Errorf("volgorde = %v, want sources(%d) < text(%d) < text_done(%d) < response_done(%d)",
			eventTypes(evs), sources, text, textDone, done)
	}
	// Wat de kaart bij text_done leest moet bij response_done nog kloppen,
	// anders springt de voetregel alsnog na het uitspreken.
	if got, want := evs[textDone].KBStatus, evs[done].KBStatus; got != want {
		t.Errorf("kbStatus: text_done = %q, response_done = %q", got, want)
	}
	if len(evs[sources].Sources) == 0 || len(evs[textDone].Sources) != len(evs[sources].Sources) {
		t.Errorf("de bronnen lopen niet mee: sources=%d text_done=%d",
			len(evs[sources].Sources), len(evs[textDone].Sources))
	}
}

// Een mislukte zelfstandige bijdrage is geen kapotte sessie: het spreekslot
// komt vrij en de knop blijft bruikbaar: geen stille storing.
func TestMeetingIntervention_TurnFailure_ReleasesTheSpeakingSlot(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Een bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	rig.say(t, id, "er is wel degelijk iets gezegd")
	waitForCandidate(t, s)

	// De stem valt om; de beurt loopt door als tekst en geeft het slot terug.
	rig.engine.voice = failingVoice{}
	if status, _ := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Fatal("laat horen werd geweigerd")
	}
	nextEvent(t, es.ch, EventResponseDone)
	waitFor(t, func() bool { return !s.isSpeaking() }, "het spreekslot bleef hangen")

	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("na een mislukte bijdrage hoort de knop gewoon te werken")
	}
}

// failingVoice is een stem die niet opengaat: de beurt blijft tekst-only en
// het spreekslot hoort gewoon vrij te komen.
type failingVoice struct{}

func (failingVoice) Open(context.Context, SpeechRequest) (Speech, error) {
	return nil, errors.New("test: de stem gaat niet open")
}

func (failingVoice) Format() AudioFormat { return testMP3Format }
