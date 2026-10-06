package duif

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

// De stem van één spreekbeurt, streamend (voor de latentie). Twee stukken:
//
//   - sentenceFeeder knipt de LLM-stroom in uitspreekbare zinnen zodra ze af
//     zijn: markup eraf (stripForSpeech), zinsgrenzen via splitSentences.
//   - speaker opent de TTS-sessie pas bij de eerste uitspreekbare zin, voert
//     de zinnen door en pompt de audioframes intussen naar het kanaal.
//
// Zonder provider, zonder stem-id of met een gevallen provider is de speaker
// inert: de beurt blijft tekst-only, precies zoals vóór het streamen.
// Streaming-only geldt onverkort: tekst gaat naar de provider en het kanaal,
// nooit naar een logregel.

// sentenceFeeder verzamelt tekstdelta's en levert afgemaakte zinnen af.
//
// Twee grenzen tellen: een regelovergang (alles ervoor is af, inclusief
// kopjes en opsommingen die zelf geen zinsteken hebben) en een zinsgrens
// binnen de lopende regel (alle zinnen behalve de laatste, want die groeit
// nog). De opmaakstripper werkt per regel; een zin die middenin een regel
// begint heeft geen regelmarkering meer, dus dat klopt. Een codeblok dat over
// meerdere flushes loopt wordt niet als blok herkend — de duiven schrijven
// geen code, en de stripper haalt de hekjes er sowieso af.
type sentenceFeeder struct {
	pending string
	say     func(string)
}

func (f *sentenceFeeder) feed(delta string) {
	if delta == "" {
		return
	}
	f.pending += delta
	if i := strings.LastIndexByte(f.pending, '\n'); i >= 0 {
		f.flush(f.pending[:i+1])
		f.pending = f.pending[i+1:]
	}
	// Binnen de lopende regel: alles tot de laatste zinsgrens is af, de rest
	// groeit nog. Dezelfde grensregel als de splitser (sentenceCuts), zodat
	// een afkorting of "12. 5 procent" hier niet halverwege afgeknipt wordt.
	rs := []rune(f.pending)
	cuts := sentenceCuts(rs)
	if len(cuts) == 0 {
		return
	}
	cut := cuts[len(cuts)-1]
	f.flush(string(rs[:cut]))
	f.pending = string(rs[cut:])
}

// finish levert wat er nog staat.
func (f *sentenceFeeder) finish() {
	f.flush(f.pending)
	f.pending = ""
}

func (f *sentenceFeeder) flush(text string) {
	spoken := stripForSpeech(text)
	if spoken == "" {
		return
	}
	for _, chunk := range splitSentences(spoken) {
		f.say(chunk)
	}
}

// speaker is de stem van één beurt. feed en finish komen van de
// beurt-goroutine; run bezit de stemsessie en pump is de enige die frames telt
// en tts_done stuurt.
//
// Waarom run een eigen goroutine is: het openen van de stemsessie is een DNS-,
// TCP-, TLS- en upgrade-handshake. Stond die in het spoor van de tekstdelta's,
// dan hield een trage stemhost de zichtbare tekst tegen vanaf de eerste
// afgemaakte zin, en een doodlopende host hield hem tegen tot de beurtklok
// afliep. Dat is precies wat het antwoordcontract uitsluit: het antwoord
// bereikt de lezer altijd, en geen schakel tussen het model en de lezer houdt
// tekst vast. De zinnen gaan nu naar een gebufferd kanaal en de
// tekst loopt door; valt de stem om, dan blijft de beurt tekst-only.
type speaker struct {
	e      *meetingEngine
	s      *meetingSession
	gen    uint64
	ctx    context.Context
	req    SpeechRequest
	format AudioFormat
	feeder sentenceFeeder

	ttsStartedAt time.Time
	inert        bool // geen provider of geen stem: de seam wordt niet aangeroepen
	fed          bool // er kwam tekst binnen (een lege LLM-beurt is geen "niets te zeggen")
	// chunks draagt de afgemaakte zinnen naar de stemgoroutine; nil tot de
	// eerste zin. done gaat dicht zodra die goroutine klaar is — ook als de
	// stem nooit openging, zodat feed en finish nooit blijven hangen.
	chunks chan string
	done   chan struct{}
	// closeOnce bewaakt het dichtgaan van chunks: de afgebroken route sluit
	// de stem via close(), de geslaagde via finish(), en beide mogen elkaar
	// tegenkomen zonder een tweede close() op hetzelfde kanaal.
	closeOnce sync.Once
}

const (
	// speakerOpenTimeout is de eigen deadline van het openen van de stem. De
	// beurtklok staat op twee minuten en is daarmee geen rem op een stemhost
	// die niet antwoordt; deze wel. Komt de stem niet binnen deze tijd op gang,
	// dan degradeert de beurt naar tekst-only.
	speakerOpenTimeout = 10 * time.Second
	// speakerQueue is hoeveel afgemaakte zinnen er mogen wachten terwijl de
	// stemsessie nog opengaat. Een vergaderbijdrage is een handvol zinnen, dus
	// de tekst loopt in de praktijk nooit tegen deze grens aan — en als hij dat
	// wél doet, wacht hij hoogstens tot de deadline hierboven.
	speakerQueue = 64
)

// newSpeaker zet de stem van een beurt klaar. Dezelfde bron als het
// state-event bepaalt of er geluid komt: wat de UI als `voice` te zien kreeg,
// is wat hier gebeurt. Zonder bruikbare stem-id houdt het hier op — de seam
// wordt niet eens aangeroepen.
func (e *meetingEngine) newSpeaker(ctx context.Context, s *meetingSession, gen uint64) *speaker {
	sp := &speaker{e: e, s: s, gen: gen, ctx: ctx, inert: true}
	if e.voice == nil {
		return sp
	}
	voiceID, ok := resolveVoiceID(s.persona, e.devVoice)
	if !ok {
		e.logSpeechFailure(s, errNoVoice)
		return sp
	}
	sp.inert = false
	sp.req = SpeechRequest{VoiceID: voiceID, Lang: s.lang, Speed: s.persona.SpeakingSpeed()}
	sp.format = e.voice.Format()
	sp.feeder = sentenceFeeder{say: sp.say}
	return sp
}

// feed krijgt elke tekstdelta van de beurt, in volgorde.
func (sp *speaker) feed(delta string) {
	if sp.inert || delta == "" {
		return
	}
	sp.fed = true
	sp.feeder.feed(delta)
}

// say is de afnemer van de feeder: de eerste zin zet de stemgoroutine aan, de
// volgende schuiven in de rij. Blokkeren doet hij alleen als die rij vol staat,
// en dan niet langer dan de stem erover mag doen om open te gaan.
func (sp *speaker) say(chunk string) {
	if sp.inert {
		return
	}
	if sp.chunks == nil {
		sp.ttsStartedAt = sp.e.clock.Now()
		sp.s.diagnostics.mark(sp.ttsStartedAt, "tts_requested", "", 0)
		sp.chunks = make(chan string, speakerQueue)
		sp.done = make(chan struct{})
		go sp.run()
	}
	select {
	case sp.chunks <- chunk:
	case <-sp.done: // de stem is weg; de tekst loopt gewoon door
	case <-sp.ctx.Done():
	}
}

// run bezit de stemsessie: hij opent haar, voert haar de zinnen die de beurt
// aanlevert en wacht op de laatste frames.
func (sp *speaker) run() {
	defer close(sp.done)
	// De sessie hangt aan deze context en leeft dus zo lang als de beurt.
	// Alleen het openen krijgt een eigen, kortere deadline: de timer knipt de
	// context door als de host niet op tijd antwoordt, en wordt gestopt zodra
	// Open terug is — een geopende stem wordt hier nooit alsnog afgekapt.
	ctx, cancel := context.WithCancel(sp.ctx)
	defer cancel()
	timer := time.AfterFunc(sp.e.speechOpenTimeout(), cancel)
	speech, err := sp.e.voice.Open(ctx, sp.req)
	timer.Stop()
	if ctx.Err() != nil && sp.ctx.Err() != nil {
		// De beurt zelf is afgebroken (stopknop): dat is geen stemstoring.
		return
	}
	if ctx.Err() != nil {
		if err == nil {
			speech.End() // wat er nog terugkwam hangt aan een dode context
		}
		speech, err = nil, errSpeechOpenTimeout
	}
	if err != nil {
		sp.e.logSpeechFailure(sp.s, err)
		sp.emitTTSUnavailable()
		return
	}
	pumped := make(chan struct{})
	go sp.pump(speech, pumped)
	// Twee einden, niet één. Het zinnenkanaal gaat dicht als de beurt is
	// uitgesproken; de beurt-context valt om als zij wordt afgebroken —
	// stopknop, weggelopen lezer, beurtklok, foutchunk. Alleen op het eerste
	// wachten liet deze goroutine op elke afgebroken beurt achter in de
	// leeslus, met sessie en engine er nog aan vast, en sloot de stemsessie
	// nooit af.
	for open := true; open; {
		select {
		case chunk, more := <-sp.chunks:
			if !more {
				open = false
				break
			}
			speech.Say(chunk)
		case <-sp.ctx.Done():
			open = false
		}
	}
	speech.End()
	select {
	case <-pumped:
	case <-sp.ctx.Done():
	}
}

// close sluit het zinnenkanaal en laat de stemgoroutine zo uit haar leeslus
// lopen. Hij is idempotent en wacht nergens op: elke route die een beurt
// verlaat stelt hem uit, ook de routes die nooit bij finish() uitkomen.
func (sp *speaker) close() {
	if sp.chunks == nil {
		return
	}
	sp.closeOnce.Do(func() { close(sp.chunks) })
}

// speechOpenTimeout is hoe lang het openen van een stemsessie mag duren.
func (e *meetingEngine) speechOpenTimeout() time.Duration {
	if e.openTimeout > 0 {
		return e.openTimeout
	}
	return speakerOpenTimeout
}

// finish sluit de beurt af: de laatste zin mee, geen tekst meer, en wachten
// tot de pomp het laatste frame en tts_done heeft afgeleverd — of tot de
// stopknop de beurt afbreekt. Het is de variant van close() die ook wacht.
func (sp *speaker) finish() {
	if sp.inert {
		return
	}
	sp.feeder.finish()
	if sp.chunks == nil {
		if sp.fed {
			sp.e.logSpeechFailure(sp.s, errNothingToSay)
		}
		return
	}
	sp.close()
	select {
	case <-sp.done:
	case <-sp.ctx.Done():
	}
}

// pump zet de audioframes op het kanaal zodra ze er zijn, genummerd, met het
// formaat erbij zodat de client bij het eerste frame al weet wat hij speelt.
func (sp *speaker) pump(speech Speech, pumped chan struct{}) {
	defer close(pumped)
	var count int
	frames := speech.Frames()
	for {
		select {
		case <-sp.ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				var terminal error = errSpeechUnconfirmed
				if reported, ok := speech.(interface{ Result() <-chan error }); ok {
					result := reported.Result()
					if result != nil {
						select {
						case err, received := <-result:
							if received {
								terminal = err
							}
						case <-sp.ctx.Done():
							return
						case <-time.After(defaultIdleTimeout):
							terminal = errSpeechStalled
						}
					}
				}
				sp.emitTTSResult(count, terminal)
				sp.e.log().Debug("vergaderbeurt: tts klaar", "component", "duif",
					"sessie", sp.s.id, "frames", count)
				return
			}
			if count == 0 {
				now := sp.e.clock.Now()
				sp.s.diagnostics.mark(now, "tts_first_frame", "", now.Sub(sp.ttsStartedAt))
			}
			count++
			if !sp.s.emitTurn(sp.gen, MeetingEvent{
				Type: EventTTSAudio, Seq: count,
				Audio:  base64.StdEncoding.EncodeToString(frame),
				Format: sp.format.Codec, SampleRate: sp.format.SampleRate,
			}) {
				return
			}
		}
	}
}

// ttsUnavailable is de code van een stem die nooit begon: het openen faalde of
// duurde te lang. Dat is iets anders dan tts_incomplete, waar er al geluid
// klonk en het halverwege stokte. Hier speelde er niets, dus "het geluid speelde
// niet volledig af" klopte niet; de tekst komt gewoon binnen.
const ttsUnavailable = "tts_unavailable"

// emitTTSUnavailable meldt dat deze beurt zonder stem blijft. De client toont
// daarop zijn eigen melding (meeting.notice.voiceUnavailable); de zin hier is
// de terugval voor een client die de code niet kent.
func (sp *speaker) emitTTSUnavailable() {
	if sp.ctx.Err() != nil {
		return
	}
	sp.s.emitTurn(sp.gen, MeetingEvent{Type: EventTTSDone, Error: ttsUnavailable, Message: map[string]string{
		"en": "The voice is not available right now. The text is on screen.",
		"nl": "De stem is nu niet beschikbaar. De tekst staat wel op het scherm.",
	}})
}

// TTS completion is separate from response_done, which describes the written
// answer. A speech error must stop playback without discarding later text.
func (sp *speaker) emitTTSResult(frames int, err error) {
	if sp.ctx.Err() != nil {
		return
	}
	ev := MeetingEvent{Type: EventTTSDone, Frames: frames}
	if err != nil {
		ev.Error = "tts_incomplete"
		ev.Message = map[string]string{
			"en": "The voice stopped before completion. The written answer remains available.",
			"nl": "De stem is niet volledig afgerond. Het geschreven antwoord blijft beschikbaar.",
		}
		sp.e.log().Warn("speech did not complete", "component", "duif", "reason", "tts_incomplete")
	}
	sp.s.emitTurn(sp.gen, ev)
}
