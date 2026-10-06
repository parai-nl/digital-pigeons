package duif

import (
	"context"
	"time"
)

// Zelfstandig ingrijpen en de non-verbale reactie: wat er gebeurt zodra de klok
// een klaarliggende kandidaat doorlaat.
//
// De grondregel staat nog steeds in de vorm van deze code: de knop is de
// gegarandeerde basis en deelt geen enkel pad met de kandidaatgenerator. Wat
// wél veranderd is, is wat er hier gebeurt. Vroeger begon op dit punt een hele
// nieuwe beurt: retrieval, prompt, generatie, en pas daarna geluid — seconden
// nadat het moment voorbij was. Nu ligt de bijdrage er al. Wat rest is haar
// uitspreken.

// soundRoute is het pad waar een veldopname op te halen valt; de frontend
// hangt hem in een <audio>-element.
const soundRoute = "/api/duif/v1/sounds/"

// SoundRef is één gekozen veldopname van de non-verbale duif op de wire.
type SoundRef struct {
	ID   string    `json:"id"`
	Band SoundBand `json:"band"`
	File string    `json:"file"`
	URL  string    `json:"url"`
}

// SessionSettings zijn de sessieknoppen van zelfstandig meedoen op de wire. Ze
// zijn sessie-metadata en geen inhoud — instellingen mogen wél gedeeld worden.
type SessionSettings struct {
	TurnID           string `json:"turnId"`
	AutonomyPaused   bool   `json:"autonomyPaused"`
	PlaybackTracking bool   `json:"playbackTracking"`
	Autonomous       bool   `json:"autonomous"`
	Eagerness        string `json:"eagerness"`
	CooldownSeconds  int    `json:"cooldownSeconds"`
}

// interveneNow is de seam waar een doorgelaten kandidaat op landt. De pratende
// duif spreekt haar klaarliggende bijdrage uit; de non-verbale duif laat een
// geluid horen.
func (e *meetingEngine) interveneNow(s *meetingSession, c *meetingCandidate) {
	// Deze seam wordt aangeroepen vanuit twee goroutines die geen van beide de
	// sessielus zijn. Een paniek hier is een zijtak die omvalt, geen reden om
	// de sessie of het proces mee te nemen; de knop moet blijven werken.
	defer e.recoverIntervention(s)
	if !s.persona.Chat {
		e.playSound(s, c.At, 0, true)
		return
	}
	gen, ctx, cancel, ok := s.beginTurn(true)
	if !ok {
		// Geen wachtrij: de vergadering is dan toch al verder.
		return
	}
	// Alleen operationele codes gaan in het log, geen onderwerp of bijdrage.
	e.log().Info("duif grijpt zelfstandig in", "component", "duif", "sessie", s.id,
		"duif", s.pigeonID)
	go e.speakCandidate(s, gen, ctx, cancel, c)
}

// speakCandidate spreekt een al geschreven bijdrage uit. Het is dezelfde
// machinerie als een gewone beurt — persona, tekst, stem, response_done — met
// één verschil: er komt geen taalmodel meer aan te pas. De duif heeft deze
// woorden geschreven toen zij ze bedacht, en spreekt ze nu uit.
func (e *meetingEngine) speakCandidate(s *meetingSession, gen uint64, ctx context.Context, cancel context.CancelFunc, c *meetingCandidate) {
	defer cancel()
	defer s.endTurn(gen)
	defer e.recoverTurn(s, gen)

	if !s.emitTurn(gen, MeetingEvent{Type: EventPersona, PigeonID: s.pigeonID, Lang: s.lang}) {
		return
	}
	// Dezelfde volgorde als een gewone beurt (runTurn): eerst de bronnen, dan
	// de tekst, dan text_done. De client heeft de bronnen nodig vóór de tekst
	// om "[1]" als verwijzing te kunnen tekenen in plaats van als ruwe tekens,
	// en text_done haalt de typ-cursor weg zodra de tekst compleet is — anders
	// blijft die de hele spreekduur staan.
	if !s.emitTurn(gen, MeetingEvent{Type: EventSources, KBStatus: c.Status, Sources: c.Sources, Dropped: c.Dropped}) {
		return
	}
	sp := e.newSpeaker(ctx, s, gen)
	// Ook hier gaat de stem mee dicht welke route de beurt ook verlaat, zodat
	// er geen stemgoroutine achterblijft.
	defer sp.close()
	if !s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: c.Text}) {
		return
	}
	sp.feed(c.Text)
	// De stopreden reist mee met de bijdrage: zij is geschreven op het moment
	// dat de kandidaat ontstond, niet nu. Zonder haar op deze twee events zou
	// de kaart een op het tokenplafond afgekapte halve zin als een afgeronde
	// bijdrage tonen — de client leest de melding van precies hier
	// (meetingStore: text_done en response_done).
	s.emitTurn(gen, MeetingEvent{Type: EventTextDone, KBStatus: c.Status, Sources: c.Sources, StopReason: c.StopReason})
	sp.finish()
	s.noteContribution(c.Text, lastHumanSeq(c.Window), ownGrounding{Status: c.Status, Sources: c.Sources, RAGSection: c.RAGSection})
	s.finishTurn(gen, MeetingEvent{Type: EventResponseDone, KBStatus: c.Status, Sources: c.Sources, StopReason: c.StopReason})

	// Ná finishTurn, net als in runTurn: daar houdt TestMeetingRespondNow_ResponseDoneMeansTheSlotIsFree
	// de beurt op dit bericht vast, en beide routes horen het slot op dezelfde plek vrij te geven.
	e.log().Info("vergaderbeurt klaar", "component", "duif", "sessie", s.id,
		"duif", s.pigeonID, "aanleiding", "automatic", "segmenten", len(c.Window),
		"bronnen", len(c.Sources), "tekens", len(c.Text), "stop", c.StopReason)
}

// recoverIntervention vangt een paniek op het interventiepad. Er wordt niets
// gemeld op het sessiekanaal: de vergadering heeft geen interventie gevraagd en
// hoeft dus ook geen foutmelding over een uitgebleven interventie. De generator
// die deze aanroep deed meldt zijn eigen degradatie (`guard`).
func (e *meetingEngine) recoverIntervention(s *meetingSession) {
	if r := recover(); r != nil {
		e.log().Error("zelfstandige interventie paniekte, sessie loopt door",
			"component", "duif", "sessie", s.id, "duif", s.pigeonID)
	}
}

// playSound is de reactie van de non-verbale duif: een mp3 uit de band van
// de actuele meterstand, plus een impuls op die meter. Geen LLM, geen TTS. Hij
// geeft terug wat er gespeeld is, zodat de knop-route dat kan melden.
func (e *meetingEngine) playSound(s *meetingSession, now time.Time, impulse float64, automatic ...bool) (SoundRef, float64, bool) {
	// De band hoort bij de stand van nú: op de kandidaatroute is die net
	// meegestegen met de score, op de knop-route staat hij nog waar hij stond.
	level := s.arousal.level()
	var ref SoundRef
	if len(s.persona.Geluiden) == 0 {
		return ref, level, false
	}
	gen, _, cancel, admitted := s.beginTurn(automatic...)
	if !admitted {
		return ref, level, false
	}
	defer cancel()
	defer s.endTurn(gen)
	s.mu.Lock()
	previous := s.lastSoundID
	s.mu.Unlock()
	sound, ok := pickSoundAvoid(s.persona.Geluiden, level, e.pick, previous)
	if ok {
		ref = SoundRef{ID: sound.ID, Band: sound.Band, File: sound.Bestand, URL: soundRoute + sound.ID}
		if !s.emitTurn(gen, MeetingEvent{Type: EventSound, Sound: &ref, EventID: newID(), DurationMS: sound.DurationMs}) {
			return SoundRef{}, level, false
		}
	}
	if impulse > 0 {
		// Alleen de knop-route zet de meter zelf een zet; de kandidaatroute
		// heeft de stand al gemeld en zou hem hier dubbel sturen.
		level = s.arousal.bump(now, impulse)
		if s.persona.Chat {
			s.publish(MeetingEvent{Type: EventArousal, Arousal: &level})
		}
	}
	if !ok {
		return SoundRef{}, level, false
	}
	s.noteUtterance()
	e.log().Info("non-verbale duif laat zich horen", "component", "duif", "sessie", s.id,
		"event", "sound")
	return ref, level, true
}

// manualIntervene is de handmatige "de duif grijpt in"-knop. Hij werkt
// altijd — ook met een uitgeschakelde of kapotte generator — en is daarmee voor
// deze duif wat de spreekknop is voor de pratende duiven.
func (e *meetingEngine) manualIntervene(s *meetingSession) (SoundRef, float64, error) {
	if s.persona.Chat {
		return SoundRef{}, 0, errPigeonDoesNotSound
	}
	ref, level, ok := e.playSound(s, s.clock.Now(), manualArousalImpulse)
	if !ok {
		if s.isSpeaking() {
			return SoundRef{}, level, errPigeonBusy
		}
		return SoundRef{}, level, errNoSoundAvailable
	}
	return ref, level, nil
}

// pick is de trekking binnen een band: willekeurig, tenzij een test hem vastzet.
func (e *meetingEngine) pick(n int) int {
	if e.pickIndex != nil {
		return e.pickIndex(n)
	}
	return randomIndex(n)
}
