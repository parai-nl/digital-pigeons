package duif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/parai-nl/parai-core/audio/audiotesting"
)

// resolveVoiceID is de enige bron voor "heeft deze duif een stem": DTO,
// sessiestatus en spreekbeurt lezen hem alledrie.
func TestResolveVoiceID_OneSourceOfTruth(t *testing.T) {
	tests := []struct {
		name     string
		persona  *Persona
		devVoice string
		wantID   string
		wantOK   bool
	}{
		{"nonverbal pigeon never speaks", &Persona{Chat: false, VoiceID: "echte-id"}, "dev-id", "", false},
		{"real voice id wins", &Persona{Chat: true, VoiceID: "echte-id"}, "dev-id", "echte-id", true},
		{"placeholder with dev voice falls back", &Persona{Chat: true, VoiceID: voicePlaceholder}, "dev-id", "dev-id", true},
		{"placeholder without dev voice is silent", &Persona{Chat: true, VoiceID: voicePlaceholder}, "", "", false},
		{"empty voice id is never a dev fallback", &Persona{Chat: true, VoiceID: ""}, "dev-id", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := resolveVoiceID(tc.persona, tc.devVoice)
			if id != tc.wantID || ok != tc.wantOK {
				t.Errorf("resolveVoiceID = (%q, %v), want (%q, %v)", id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}

// De stemstatus wordt eenmalig in openWith berekend en staat op het
// state-event: de UI hoeft nooit te raden of er geluid komt.
func TestMeetingSession_VoiceState_IsComputedOnce(t *testing.T) {
	tests := []struct {
		name     string
		pigeon   string
		wired    bool
		voiceID  string
		devVoice string
		want     VoiceState
	}{
		{"no provider wired", "individualist", false, "echte-id", "", VoiceOff},
		{"placeholder without dev voice", "individualist", true, voicePlaceholder, "", VoiceOff},
		{"placeholder with dev voice", "individualist", true, voicePlaceholder, "dev-id", VoiceReady},
		{"real voice id", "individualist", true, "echte-id", "", VoiceReady},
		{"nonverbal pigeon", "nonverbal", true, "echte-id", "dev-id", VoiceOff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := newTestEngine(t, nil, func(e *meetingEngine) {
				if tc.wired {
					e.voice = newFakeVoice("frame")
				}
				e.devVoice = tc.devVoice
				p, ok := e.personas.Get(tc.pigeon)
				if !ok {
					t.Fatalf("persona %q ontbreekt in de testconfig", tc.pigeon)
				}
				p.VoiceID = tc.voiceID
			})
			s, err := e.open("user-1", tc.pigeon, "en")
			if err != nil {
				t.Fatalf("sessie openen: %v", err)
			}
			if s.voice != tc.want {
				t.Errorf("s.voice = %q, want %q", s.voice, tc.want)
			}
			ch, unsub := s.subscribe()
			defer unsub()
			if ev := nextEvent(t, ch, EventState); ev.Voice != tc.want {
				t.Errorf("state-event voice = %q, want %q", ev.Voice, tc.want)
			}
		})
	}
}

// Het open-antwoord draagt hetzelfde veld, zodat de UI al vóór het eerste
// event weet of de duif klinkt.
func TestMeetingOpen_ResponseCarriesVoiceField(t *testing.T) {
	rig := newMeetingRig(t, nil, func(e *meetingEngine) { e.voice = newFakeVoice("frame") })
	status, body := rig.do(t, "POST", "/v1/meeting/sessions", map[string]any{"pigeonId": "individualist", "lang": "en"})
	if status != 201 {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body = %s (%v)", body, err)
	}
	v, ok := out["voice"].(string)
	if !ok {
		t.Fatalf("open-antwoord mist het voice-veld: %s", body)
	}
	if VoiceState(v) != VoiceOff && VoiceState(v) != VoiceReady {
		t.Errorf("voice = %q, want ready of off", v)
	}
}

// speakAll bootst de oude "hele uiting in, frames uit"-aanroep na op de
// streaming seam: openen, de zinnen vanuit een eigen goroutine schrijven
// (zoals de beurt dat doet), afsluiten. Zo toetsen deze tests de adapter en
// niet de feeder — die heeft zijn eigen tests in meeting_speaker_test.go.
func speakAll(ctx context.Context, v *synthesizerVoice, text string, req SpeechRequest) (<-chan []byte, error) {
	sp, err := v.Open(ctx, req)
	if err != nil {
		return nil, err
	}
	go func() {
		for _, chunk := range splitSentences(stripForSpeech(text)) {
			sp.Say(chunk)
		}
		sp.End()
	}()
	return sp.Frames(), nil
}

// De provider krijgt zin voor zin binnen, elk met precies één afsluitende
// spatie (de ElevenLabs-regel), en het tekstkanaal gaat precies één keer dicht.
func TestSynthesizerVoice_WritesSentencesWithASingleTrailingSpace(t *testing.T) {
	sess := newScriptedSession()
	logger, logged := captureLogs()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)

	const tekst = "De duiven zitten op het plein en kijken toe. Ze wachten rustig op een besluit."
	out, err := speakAll(context.Background(), v, tekst, SpeechRequest{VoiceID: "stem-1", Lang: "nl"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	want := []string{
		"De duiven zitten op het plein en kijken toe. ",
		"Ze wachten rustig op een besluit. ",
	}
	// Eerst de schrijver zijn werk laten afmaken: sluit de sessie eerder, dan
	// stopt hij onderweg — dat is precies de bedoeling, maar niet wat deze
	// test meet.
	waitFor(t, func() bool { return len(sess.writtenText()) == len(want) },
		"de provider kreeg niet alle zinnen")
	sess.finishAudio()
	collectFrames(t, out)

	got := sess.writtenText()
	if len(got) != len(want) {
		t.Fatalf("geschreven chunks = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, got[i], want[i])
		}
	}
	waitFor(t, func() bool { _, textClosed := sess.counts(); return textClosed == 1 },
		"het tekstkanaal hoort precies één keer te sluiten")
	if strings.Contains(logged(), "plein") {
		t.Errorf("de uiting staat in de logs:\n%s", logged())
	}
}

// Kleine frames worden opgespaard tot een blok van betekenis: minder events,
// en elk blok is een verse buffer (geen hergebruikte slice die het vorige
// frame onder de client vandaan schrijft).
func TestSynthesizerVoice_CoalescesIntoFreshBuffers(t *testing.T) {
	sess := newScriptedSession()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)
	v.coalesce = 16 * 1024
	v.flush = time.Hour // deze test meet het opsparen, niet de vroege flush

	out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1", Lang: "nl"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}

	frames := [][]byte{bytes.Repeat([]byte{1}, 10*1024), bytes.Repeat([]byte{2}, 10*1024), bytes.Repeat([]byte{3}, 10*1024)}
	// De frames gaan vanuit een eigen goroutine de leiding in (de pomp leest
	// pas terwijl de test verzamelt); een fout komt terug naar de
	// testgoroutine in plaats van er een t.Fatal buiten af te vuren.
	delivered := make(chan error, 1)
	go func() {
		defer close(delivered)
		for _, f := range frames {
			if err := sess.deliver(f); err != nil {
				delivered <- err
				return
			}
		}
		sess.finishAudio()
	}()

	got := collectFrames(t, out)
	if err := <-delivered; err != nil {
		t.Fatalf("frames aanleveren: %v", err)
	}
	if len(got) != 2 || len(got[0]) != 20*1024 || len(got[1]) != 10*1024 {
		t.Fatalf("blokken = %v", frameSizes(got))
	}
	if !bytes.Equal(bytes.Join(got, nil), bytes.Join(frames, nil)) {
		t.Error("de audio kwam niet ongeschonden aan")
	}
	// Aliasing: het eerste blok mag niet meegeschreven zijn met het tweede.
	if got[0][0] != 1 || got[0][len(got[0])-1] != 2 {
		t.Error("het eerste blok is overschreven door een hergebruikte buffer")
	}
}

// Sluit de provider zijn audiokanaal, dan sluit de adapter zijn uitvoer én de
// sessie — precies één keer.
func TestSynthesizerVoice_AudioCloseClosesOutputAndSession(t *testing.T) {
	sess := newScriptedSession()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)

	out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	if err := sess.deliver([]byte("mp3")); err != nil {
		t.Fatal(err)
	}
	sess.finishAudio()

	if got := collectFrames(t, out); len(got) != 1 || string(got[0]) != "mp3" {
		t.Fatalf("frames = %v", got)
	}
	waitFor(t, func() bool { calls, _ := sess.counts(); return calls == 1 },
		"de sessie hoort precies één keer gesloten te worden")
}

// De stopknop: de beurt-context vervalt, de sessie gaat dicht, er komt
// geen frame meer en er blijft geen goroutine achter.
func TestSynthesizerVoice_ContextCancel_ClosesEverything(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sess := newScriptedSession()
		v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)
		ctx, cancel := context.WithCancel(context.Background())

		out, err := speakAll(ctx, v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
		if err != nil {
			t.Fatalf("speakAll: %v", err)
		}
		synctest.Wait()
		cancel()
		synctest.Wait()

		if _, ok := <-out; ok {
			t.Error("na de stopknop hoort er geen frame meer te komen")
		}
		if calls, _ := sess.counts(); calls != 1 {
			t.Errorf("Close-aanroepen = %d, want 1", calls)
		}
	})
}

// De provider kan er middenin mee ophouden terwijl de schrijver nog zinnen
// aanbiedt: die stopt dan via Done in plaats van eeuwig te blijven staan.
func TestSynthesizerVoice_ProviderEndsWhileFeedIsWriting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sess := newStuckScriptedSession()
		v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)

		out, err := speakAll(context.Background(), v, "Eerste zin die lang genoeg is om te blijven staan. Tweede zin die ook lang genoeg is.", SpeechRequest{VoiceID: "stem-1"})
		if err != nil {
			t.Fatalf("speakAll: %v", err)
		}
		synctest.Wait() // de schrijver hangt nu op de eerste zin
		_ = sess.close()
		synctest.Wait()

		if _, ok := <-out; ok {
			t.Error("na het einde van de sessie hoort er niets meer te komen")
		}
	})
}

// Een provider die zwijgt is een fout die de vergadering moet kunnen zien in
// de logs — als reden, zonder een letter van de uiting.
func TestSynthesizerVoice_ZeroAudio_WarnsWithReasonOnly(t *testing.T) {
	sess := newScriptedSession()
	logger, logged := captureLogs()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)

	out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	sess.finishAudio()
	if got := collectFrames(t, out); len(got) != 0 {
		t.Fatalf("frames = %v", got)
	}
	waitFor(t, func() bool { return strings.Contains(logged(), "no_audio") },
		"een stille provider hoort een waarschuwing op te leveren")
	if strings.Contains(logged(), "uiting die lang genoeg") {
		t.Errorf("de uiting staat in de logs:\n%s", logged())
	}
}

// Blijft de provider hangen, dan wacht de duif niet eindeloos: de sessie gaat
// dicht en de reden staat in het log.
func TestSynthesizerVoice_IdleTimeout_ClosesTheSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sess := newScriptedSession()
		logger, logged := captureLogs()
		v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)
		// Bewust níet de default: zo toetst deze test de override-knop en
		// niet toevallig defaultIdleTimeout.
		v.idle = 3 * time.Second

		out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
		if err != nil {
			t.Fatalf("speakAll: %v", err)
		}
		if err := sess.deliver([]byte("een frame")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()

		if _, ok := <-out; ok {
			// het eerste blok mag er nog uit komen; daarna hoort het dicht te zijn
			if _, nog := <-out; nog {
				t.Error("na de stiltegrens hoort de uitvoer dicht te zijn")
			}
		}
		if calls, _ := sess.counts(); calls == 0 {
			t.Error("een stilgevallen sessie hoort gesloten te worden")
		}
		if !strings.Contains(logged(), "tts_stalled") {
			t.Errorf("de reden hoort in het log te staan:\n%s", logged())
		}
	})
}

// Een fout bij het sluiten is geen incident voor de vergadering: hij hoort in
// het debuglog, niet in beeld.
func TestSynthesizerVoice_CloseError_IsDebugOnly(t *testing.T) {
	sess := newScriptedSession()
	sess.closeErr = errors.New(sentinel)
	logger, logged := captureLogs()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)

	out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	sess.finishAudio()
	collectFrames(t, out)
	waitFor(t, func() bool { return strings.Contains(logged(), "level=DEBUG") },
		"een sluitfout hoort als debugregel gelogd te worden")
	if strings.Contains(logged(), sentinel) {
		t.Fatal("provider close error leaked content into debug logs")
	}
}

// Een provider die niet opengaat levert een fout op — zonder ook maar een
// woord van de uiting erin.
func TestSynthesizerVoice_SynthesizeError_CarriesNoText(t *testing.T) {
	mock := &audiotesting.MockSynthesizer{Err: errStreamBroken}
	v := newSynthesizerVoice(mock, AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)

	_, err := speakAll(context.Background(), v, "Geheime vergaderzin die nergens mag opduiken.", SpeechRequest{VoiceID: "stem-1"})
	if err == nil {
		t.Fatal("een gefaalde Synthesize hoort een fout te geven")
	}
	if !errors.Is(err, errStreamBroken) {
		t.Errorf("fout = %v, want de providerfout eronder", err)
	}
	if strings.Contains(err.Error(), "vergaderzin") {
		t.Errorf("de uiting staat in de fout: %v", err)
	}
}

// Zonder stem-id wordt de provider niet eens aangeroepen: geen sessie, geen
// kosten, geen tekst bij de leverancier. (De "niets uitspreekbaars"-poort zit
// in de speaker: zie meeting_speaker_test.go.)
func TestSynthesizerVoice_NoVoice_NeverCallsTheProvider(t *testing.T) {
	mock := &audiotesting.MockSynthesizer{}
	v := newSynthesizerVoice(mock, AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)
	sp, err := v.Open(context.Background(), SpeechRequest{VoiceID: ""})
	if !errors.Is(err, errNoVoice) {
		t.Fatalf("fout = %v, want %v", err, errNoVoice)
	}
	if sp != nil {
		t.Error("zonder synthese hoort er geen beurt te zijn")
	}
	if mock.CallCount() != 0 {
		t.Errorf("de provider werd %d keer aangeroepen", mock.CallCount())
	}
}

// Format is wat de module meegaf: de seam verzint zelf geen formaat.
func TestSynthesizerVoice_FormatIsWhatTheModuleWired(t *testing.T) {
	v := newSynthesizerVoice(&audiotesting.MockSynthesizer{}, AudioFormat{Codec: CodecPCM16, SampleRate: 24000}, nil)
	if f := v.Format(); f.Codec != CodecPCM16 || f.SampleRate != 24000 {
		t.Errorf("Format() = %+v", f)
	}
}

// Een paniek in een van de twee goroutines legt hooguit de beurt om: de
// uitvoer sluit netjes en het proces leeft door.
func TestSynthesizerVoice_PanicIsContained(t *testing.T) {
	t.Run("in the pump", func(t *testing.T) {
		sess := newScriptedSession()
		sess.closePanics = true
		logger, logged := captureLogs()
		v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)

		out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
		if err != nil {
			t.Fatalf("speakAll: %v", err)
		}
		sess.finishAudio()
		collectFrames(t, out) // sluit het kanaal ondanks de paniek
		waitFor(t, func() bool { return strings.Contains(logged(), "tts_pump_panic") },
			"een paniek in de pomp hoort als reden gelogd te worden")
	})

	t.Run("in the writer", func(t *testing.T) {
		sess := newScriptedSession()
		close(sess.textCh) // elke schrijfpoging paniekt nu
		logger, logged := captureLogs()
		v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)

		out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
		if err != nil {
			t.Fatalf("speakAll: %v", err)
		}
		sess.finishAudio()
		collectFrames(t, out)
		waitFor(t, func() bool { return strings.Contains(logged(), "tts_feed_panic") },
			"een paniek in de schrijver hoort als reden gelogd te worden")
	})
}

// De provider mag ophouden terwijl er nog frames in de leiding staan: die
// gaan alsnog mee naar de vergadering.
func TestSynthesizerVoice_SessionEndsWithFramesStillQueued(t *testing.T) {
	sess := newScriptedSession()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)

	out, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.", SpeechRequest{VoiceID: "stem-1"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	if err := sess.deliver([]byte("laatste")); err != nil {
		t.Fatal(err)
	}
	sess.endSession()

	got := collectFrames(t, out)
	if len(got) != 1 || string(got[0]) != "laatste" {
		t.Errorf("frames = %v", got)
	}
}

func frameSizes(frames [][]byte) []int {
	out := make([]int, len(frames))
	for i, f := range frames {
		out[i] = len(f)
	}
	return out
}

// De stopknop terwijl de provider de zinnen niet eens aanneemt: ook dan blijft
// er geen schrijver achter.
func TestSynthesizerVoice_ContextCancelWhileFeedIsBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sess := newStuckScriptedSession()
		v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecMP3, SampleRate: 44100}, nil)
		ctx, cancel := context.WithCancel(context.Background())

		out, err := speakAll(ctx, v, "Eerste zin die lang genoeg is om te blijven staan. Tweede zin die ook lang genoeg is.", SpeechRequest{VoiceID: "stem-1"})
		if err != nil {
			t.Fatalf("speakAll: %v", err)
		}
		synctest.Wait() // de schrijver hangt op de eerste zin
		cancel()
		synctest.Wait()

		if _, ok := <-out; ok {
			t.Error("na de stopknop hoort er niets meer te komen")
		}
	})
}
