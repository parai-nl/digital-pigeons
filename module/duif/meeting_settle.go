package duif

import (
	"context"
	"time"
)

// De knop leest het venster pas als de lopende uiting is afgerond.
//
// De aanleiding, gemeten op 18 september 2026: wie op of vlak na zijn laatste
// woord op de knop drukt, krijgt "ik heb nog niets gehoord". De
// spraakherkenning doet het namelijk prima — elke zin komt 0,3–0,4 s na het
// laatste woord als definitief segment binnen — maar het venster bevat alleen
// definitieve segmenten, en op het moment van de knop staat die laatste zin nog
// als partial in de lucht. Hij landde 121 ms ná de knop; een druk op +0,3 s
// leverde wél een echte beurt. In de browser is dat raam nog groter, want de
// microfoonsluis mag tot drie seconden audio onderweg hebben.
//
// De oplossing hoort hier en niet in de wachttijd van de gebruiker: de knop
// geeft zijn HTTP-202 meteen terug, en de beurt-goroutine vraagt de STT-brug om
// een afronding (gemeten: 31 ms heen en terug) en wacht daar kort op. Ook als
// het venster al gevuld is — anders ontbreekt juist de laatste zin, en dat is
// meestal de vraag aan de duif.
//
// Wachten is altijd begrensd en valt altijd door: zonder brug (de getypte
// route, tests zonder STT) en zonder lopende uiting gebeurt er niets.

// defaultFinalizeWait is het plafond op dat wachten. Ruim boven de gemeten
// 31 ms finalize plus de 0,3–0,4 s die de provider zelf over een zin doet, en
// kort genoeg om als knopdruk nog direct te voelen. Loopt hij af, dan gaat de
// beurt door met wat er wél staat.
const defaultFinalizeWait = 1200 * time.Millisecond

// speechSettle is wat de beurt aantrof toen zij het venster ging lezen. Hij
// draagt geen tekst: alleen of er iets openstond, of er om afronding gevraagd
// is, of die landde en hoe lang dat duurde.
type speechSettle struct {
	pending   bool // er stond een uiting open toen de knop viel
	requested bool // en de brug is om een afronding gevraagd
	landed    bool // en er kwam een vers menselijk segment binnen
	waited    time.Duration
}

// settleSpeech laat een lopende uiting afronden vóór de beurt het venster
// leest. Hij hoort in de beurt-goroutine thuis: de knop zelf is al beantwoord.
// De context is die van de beurt, zodat de stopknop ook dit wachten afbreekt.
func (e *meetingEngine) settleSpeech(ctx context.Context, s *meetingSession) speechSettle {
	afterSeq, pending := s.speechPending()
	if !pending {
		return speechSettle{}
	}
	settle := speechSettle{pending: true}
	// Zonder brug is er niemand die kan afronden. Dat is geen storing maar een
	// stand: de getypte route kent geen provider, en een sessie in herstel
	// evenmin. De beurt gaat meteen door met wat er staat.
	if s.stt == nil || !s.stt.finalize() {
		return settle
	}
	settle.requested = true
	started := time.Now()
	settle.landed = s.waitForSegment(ctx, afterSeq, s.cfg.FinalizeWait)
	settle.waited = time.Since(started)
	e.log().Debug("vergaderbeurt wachtte op de afronding van de lopende uiting",
		"component", "duif", "sessie", s.id, "geland", settle.landed,
		"ms", settle.waited.Milliseconds())
	return settle
}

// speechPending zegt of er een uiting openstaat, en geeft de stand van de
// volgnummers op dat moment mee: alles wat daarna landt is nieuw.
func (s *meetingSession) speechPending() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.segSeq, !s.closed && s.uncommittedSpeech
}

// waitForSegment wacht tot er een menselijk segment landt met een hoger
// volgnummer dan afterSeq, of tot de tijd op is. True betekent: er is iets
// nieuws om op te antwoorden.
//
// "Menselijk" is hier hetzelfde als overal: een segment dat binnenkwam terwijl
// de duif hoorbaar was draagt de duif-venstermarkering en telt niet mee.
// Op zo'n echo wachten zou betekenen dat de duif op zichzelf antwoordt.
//
// De wachttijd loopt op de echte klok en niet op de sessieklok: dit is wachten
// op een provider die buiten onze tijdrekening leeft, en een test die de klok
// vooruitzet hoort deze wacht niet ongemerkt over te slaan.
func (s *meetingSession) waitForSegment(ctx context.Context, afterSeq uint64, timeout time.Duration) bool {
	if timeout <= 0 || ctx.Err() != nil {
		return false
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return false
		}
		if s.lastHumanSeq > afterSeq {
			s.mu.Unlock()
			return true
		}
		wait := s.segmentWaitLocked()
		s.mu.Unlock()

		// Het kanaal komt onder dezelfde lock vandaan als waaronder het
		// dichtgaat, dus een seintje tussen loslaten en wachten door bestaat
		// niet: dan is het kanaal al dicht en keert de select meteen terug.
		select {
		case <-wait:
		case <-s.done:
			return false
		case <-ctx.Done():
			// De stopknop, of de noodrem op de beurt: dan hoeft er op niets
			// meer gewacht te worden.
			return false
		case <-deadline.C:
			return false
		}
	}
}

// segmentWaitLocked geeft het kanaal dat dichtgaat zodra er een vers menselijk
// segment binnen is. Het wordt pas gemaakt als er werkelijk iemand wacht.
func (s *meetingSession) segmentWaitLocked() <-chan struct{} {
	if s.segmentWait == nil {
		s.segmentWait = make(chan struct{})
	}
	return s.segmentWait
}

// signalSegmentLocked wekt iedereen die op een vers segment wacht. Sluiten en
// weggooien: de volgende wachter krijgt een vers kanaal.
func (s *meetingSession) signalSegmentLocked() {
	if s.segmentWait != nil {
		close(s.segmentWait)
		s.segmentWait = nil
	}
}

// echoedInWindow telt de segmenten die binnen het beurtvenster vielen maar als
// duif-venster gemarkeerd staan — wat de duif van zichzelf terughoorde.
func (s *meetingSession) echoedInWindow() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript.EchoCount(s.clock.Now(), s.cfg.WindowDuration)
}

// De drie redenen waarom een venster leeg kan zijn. Ze staan in de logregel én
// in de zin die de duif uitspreekt, zodat de oorzaak aan tafel zichtbaar is
// zonder dat er iemand in de logs hoeft te kijken.
const (
	// emptyWindowSilent: er is werkelijk niets gezegd.
	emptyWindowSilent = "silent"
	// emptyWindowUnfinished: er stond een uiting open die ook na de afronding
	// niet als segment landde — de zin was nog onderweg.
	emptyWindowUnfinished = "unfinished"
	// emptyWindowEcho: er kwam wel geluid binnen, maar alleen terwijl de duif
	// zelf aan het woord was. Dat is haar eigen stem, geen vergaderinhoud.
	emptyWindowEcho = "echo"
)

// emptyWindowReason kiest tussen die drie. De openstaande uiting gaat voor: wie
// nog aan het praten was krijgt geen "ik hoorde alleen mezelf" te horen.
func (s *meetingSession) emptyWindowReason(settle speechSettle) string {
	switch {
	case settle.pending && !settle.landed:
		return emptyWindowUnfinished
	case s.echoedInWindow() > 0:
		return emptyWindowEcho
	default:
		return emptyWindowSilent
	}
}
