package duif

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
)

func feederWith(said *[]string) *sentenceFeeder {
	return &sentenceFeeder{say: func(chunk string) { *said = append(*said, chunk) }}
}

// De feeder levert een zin zodra hij af is, en houdt de zin die nog groeit
// vast — ook als die nog maar een paar woorden lang is.
func TestSentenceFeeder_DeliversFinishedSentencesAndKeepsTheGrowingOne(t *testing.T) {
	var said []string
	f := feederWith(&said)

	f.feed("Eerste zin die lang ")
	f.feed("genoeg is. Tweede zin die ")
	if strings.Join(said, "|") != "Eerste zin die lang genoeg is." {
		t.Fatalf("na de eerste zinsgrens gezegd = %q", said)
	}
	f.feed("ook lang genoeg is. Derde")
	if strings.Join(said, "|") != "Eerste zin die lang genoeg is.|Tweede zin die ook lang genoeg is." {
		t.Fatalf("na de tweede zinsgrens gezegd = %q", said)
	}
	f.feed(" zin.")
	f.finish()
	if strings.Join(said, "|") != "Eerste zin die lang genoeg is.|Tweede zin die ook lang genoeg is.|Derde zin." {
		t.Fatalf("na finish gezegd = %q", said)
	}
}

// Een citatie achter het zinsteken hoort bij de zin en houdt de grens niet
// tegen: de eerste zin van een onderbouwd antwoord bereikt de stem meteen.
func TestSentenceFeeder_CitationAfterThePunctuationIsStillABoundary(t *testing.T) {
	var said []string
	f := feederWith(&said)
	f.feed("Wacht even, vergiftigen? [3] Sommige steden doen dat inderdaad [1][2]. Maar")
	if strings.Join(said, "|") != "Wacht even, vergiftigen?|Sommige steden doen dat inderdaad." {
		t.Fatalf("gezegd = %q", said)
	}
	// Een blokhaak die géén citatie is verschuift niets.
	said = nil
	f = feederWith(&said)
	f.feed("Dat staat vast. [zie bijlage] en verder")
	if len(said) != 0 {
		t.Fatalf("gezegd = %q, want niets (geen hoofdletter na de blokhaak)", said)
	}
}

// Een afkorting of een getal met een punt is geen zinsgrens — dezelfde regel
// als de splitser, dus "bijv. Duiven" wordt niet in tweeën uitgesproken.
func TestSentenceFeeder_SharesTheBoundaryRuleWithTheSplitter(t *testing.T) {
	var said []string
	f := feederWith(&said)
	f.feed("Kijk bijv. Duiven op het plein, ongeveer 12. 5 procent van de stad. Volgende zin")
	if len(said) != 1 || said[0] != "Kijk bijv. Duiven op het plein, ongeveer 12. 5 procent van de stad." {
		t.Fatalf("gezegd = %q", said)
	}
}

// Een regelovergang maakt alles ervoor af, ook zonder zinsteken: kopjes en
// opsommingen komen zo op tijd bij de stem, zonder hekjes of streepjes.
func TestSentenceFeeder_FlushesLinesOnNewline_Stripped(t *testing.T) {
	var said []string
	f := feederWith(&said)
	f.feed("## Wat we eten\n- zaden\n- brood van")
	if len(said) != 1 || said[0] != "Wat we eten zaden" {
		t.Fatalf("na de regelovergangen gezegd = %q", said)
	}
	f.feed(" mensen [2].\n")
	f.finish()
	if strings.Join(said, "|") != "Wat we eten zaden|brood van mensen." {
		t.Fatalf("gezegd = %q", said)
	}
}

// Citaties en nadruk halen de stem nooit; een stuk dat alleen uit markup
// bestaat levert niets op.
func TestSentenceFeeder_NothingSpeakableSaysNothing(t *testing.T) {
	var said []string
	f := feederWith(&said)
	f.feed("[1] **__** ")
	f.finish()
	if len(said) != 0 {
		t.Fatalf("gezegd = %q, want niets", said)
	}
	f.feed("**Roekoe** [1]. Dat woord doet iets met ons allemaal hier. En")
	if len(said) != 1 || said[0] != "Roekoe. Dat woord doet iets met ons allemaal hier." {
		t.Fatalf("gezegd = %q", said)
	}
}

// De kern van de latentiewinst: de eerste zin bereikt de stem terwijl de LLM
// nog aan de tweede schrijft — de beurt wacht niet op het hele antwoord.
func TestMeetingRespondNow_SpeaksWhileTheLLMIsStillWriting(t *testing.T) {
	gllm := newGatedLLM("Eerste zin die lang genoeg is. Tweede zin die ", "ook lang genoeg is.")
	gllm.gate()
	voice := newFakeVoice("frame-1")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: status = %d, body = %s", status, body)
	}

	gllm.let(t) // de eerste chunk is eruit; de LLM houdt de tweede nog vast
	waitFor(t, func() bool { return len(voice.spokenChunks()) == 1 },
		"de eerste zin hoort de stem te bereiken vóór de LLM klaar is")
	if got := voice.spokenChunks()[0]; got != "Eerste zin die lang genoeg is." {
		t.Errorf("eerste zin bij de stem = %q", got)
	}
	if voice.endCount() != 0 {
		t.Error("de beurt sloot de stem al af terwijl de LLM nog schreef")
	}

	gllm.let(t)
	text, evs := collectSpokenText(t, es.ch)
	if voice.spokenText() != text {
		t.Errorf("uitgesproken = %q, geschreven = %q", voice.spokenText(), text)
	}
	if voice.endCount() != 1 || !hasEvent(evs, EventTTSDone) {
		t.Errorf("End = %d, events = %v", voice.endCount(), eventTypes(evs))
	}
}

// Elk audioframe draagt het formaat van de stem, zodat de client vóór het
// eerste frame weet of hij blok voor blok mag afspelen.
func TestMeetingRespondNow_FramesCarryTheAudioFormat(t *testing.T) {
	gllm := newGatedLLM("Roekoe. Ik vind daar wat van.")
	voice := newFakeVoice("pcm-1", "pcm-2")
	voice.format = AudioFormat{Codec: CodecPCM16, SampleRate: 24000}
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	var frames int
	for _, ev := range evs {
		if ev.Type != EventTTSAudio {
			continue
		}
		frames++
		if ev.Format != CodecPCM16 || ev.SampleRate != 24000 {
			t.Errorf("frame %d: format = %q, sampleRate = %d", ev.Seq, ev.Format, ev.SampleRate)
		}
	}
	if frames != 2 {
		t.Errorf("frames = %d, want 2", frames)
	}
}

// Het openen van een stemsessie is een handshake over het netwerk. Stond die in
// het spoor van de tekstdelta's, dan hield een trage stemhost het zichtbare
// antwoord vast vanaf de eerste afgemaakte zin. De stem gaat nu naast de
// antwoordgoroutine open en de tekst loopt door: het antwoord bereikt de lezer
// altijd.
func TestMeetingTurn_SlowVoiceOpen_KeepsTheTextFlowing(t *testing.T) {
	const answer = "Eerste zin is af. Tweede zin is ook af. Derde zin ook al. Vierde idem. En de vijfde."
	voice := newFakeVoice("frame-1")
	release := voice.blockOpen()
	gllm := newGatedLLM("Eerste zin is af. ", "Tweede zin is ook af. ",
		"Derde zin ook al. ", "Vierde idem. ", "En de vijfde.")
	gllm.gate()
	e, _ := newTestEngine(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })
	s, _ := e.open("user-1", "individualist", "en")
	s.appendSegment("er wordt iets gevraagd", "")
	ch, unsub := s.subscribe()
	defer unsub()

	if err := e.respondNow(s); err != nil {
		t.Fatalf("respondNow: %v", err)
	}
	// De eerste afgemaakte zin zet de stem aan — een zin is pas af als er tekst
	// achter staat, dus dat is de tweede chunk. De stem blijft in Open hangen.
	gllm.let(t)
	gllm.let(t)
	voice.waitForOpen(t)
	// let() faalt zodra de stroom niet meer gelezen wordt: dát was de klem.
	gllm.let(t)
	gllm.let(t)
	gllm.let(t)

	var text strings.Builder
	for text.Len() < len(answer) {
		text.WriteString(nextEvent(t, ch, EventText).Delta)
	}
	if got := text.String(); got != answer {
		t.Fatalf("zichtbare tekst = %q, wil %q", got, answer)
	}
	release()
	nextEvent(t, ch, EventResponseDone)
}

// Een stemhost die helemaal niet antwoordt kost de beurt haar stem, niet haar
// tekst: binnen haar eigen deadline valt zij terug op tekst-only.
func TestMeetingTurn_VoiceThatNeverOpens_DegradesToTextOnly(t *testing.T) {
	voice := newFakeVoice("frame-1")
	voice.blockOpen() // en nooit loslaten
	gllm := newGatedLLM("Eerste zin is af. En de tweede.")
	e, _ := newTestEngine(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.voice = voice
		e.openTimeout = 40 * time.Millisecond
	})
	s, _ := e.open("user-1", "individualist", "en")
	s.appendSegment("er wordt iets gevraagd", "")
	ch, unsub := s.subscribe()
	defer unsub()

	if err := e.respondNow(s); err != nil {
		t.Fatalf("respondNow: %v", err)
	}
	// Er klonk nooit iets, dus geen tts_incomplete ("niet volledig afgespeeld")
	// maar tts_unavailable: de stem kwam er niet, de tekst wel.
	if ev := nextEvent(t, ch, EventTTSDone); ev.Error != ttsUnavailable || ev.Frames != 0 {
		t.Errorf("tts_done = %q met %d frames, wil %s zonder frames", ev.Error, ev.Frames, ttsUnavailable)
	}
	nextEvent(t, ch, EventResponseDone)
	if text := collectText(ch); text != "" {
		t.Logf("resterende tekst: %q", text)
	}
}

// collectText leegt wat er nog op het kanaal staat; alleen als hulp bij een
// falende assert, nooit als eis.
func collectText(ch <-chan MeetingEvent) string {
	var out strings.Builder
	for {
		select {
		case ev := <-ch:
			if ev.Type == EventText {
				out.WriteString(ev.Delta)
			}
		default:
			return out.String()
		}
	}
}

// speakerRunGoroutines telt de stemgoroutines die nu ergens in run() staan.
// Een afgebroken beurt hoort er geen achter te laten.
func speakerRunGoroutines() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), "(*speaker).run(")
		}
		buf = make([]byte, 2*len(buf))
	}
}

// De stopknop laat geen stemgoroutine achter. run() hing in
// `for chunk := range sp.chunks`, en dat kanaal ging alleen dicht op de
// geslaagde route (finish). Een beurt die vóór die route eindigde — stopknop,
// weggelopen lezer, beurtklok, foutchunk — liet de goroutine voor de rest van
// het proces staan, mét verwijzingen naar sessie en engine, en speech.End()
// kwam er nooit.
func TestMeetingStop_ClosesTheSpeakerGoroutine(t *testing.T) {
	before := speakerRunGoroutines()
	gllm := newGatedLLM("Eerste zin die lang genoeg is. Tweede zin die ", "ook lang genoeg is.")
	gllm.gate()
	voice := newFakeVoice("frame-1")
	voice.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Iemand stelt de duif een vraag.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)

	gllm.let(t) // de eerste afgemaakte zin zet de stemgoroutine aan
	waitFor(t, func() bool { return len(voice.spokenChunks()) == 1 },
		"de stem kreeg de eerste zin niet")

	rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/stop", nil)
	nextEvent(t, es.ch, EventStopped)

	waitFor(t, func() bool { return speakerRunGoroutines() <= before },
		"de stemgoroutine bleef na de stopknop hangen")
	waitFor(t, func() bool { return voice.endCount() == 1 },
		"de stemsessie is nooit afgesloten (End)")
}

// Hetzelfde lek op de foutroute: een foutchunk mídden in de stream
// eindigt de beurt zonder finish(), dus ook hier moet de stem dichtgaan.
func TestMeetingMidStreamError_ClosesTheSpeakerGoroutine(t *testing.T) {
	before := speakerRunGoroutines()
	gllm := newGatedLLM()
	gllm.chunks = []llm.StreamChunk{{Content: "Eerste zin die lang genoeg is. Tweede zin die "}, {Err: errStreamBroken}}
	gllm.gate()
	voice := newFakeVoice("frame-1")
	voice.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Iemand stelt de duif een vraag.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)

	gllm.let(t)
	waitFor(t, func() bool { return len(voice.spokenChunks()) == 1 },
		"de stem kreeg de eerste zin niet")
	gllm.let(t) // en nu breekt de provider af

	_, evs := collectSpokenText(t, es.ch)
	if !hasEvent(evs, EventError) {
		t.Fatalf("foutframe ontbreekt: %v", eventTypes(evs))
	}
	waitFor(t, func() bool { return speakerRunGoroutines() <= before },
		"de stemgoroutine bleef na de foutchunk hangen")
	waitFor(t, func() bool { return voice.endCount() == 1 },
		"de stemsessie is nooit afgesloten (End)")
}
