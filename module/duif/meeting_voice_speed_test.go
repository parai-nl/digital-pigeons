package duif

import (
	"context"
	"net/http"
	"testing"

	"github.com/parai-nl/parai-core/audio"
	"github.com/parai-nl/parai-core/audio/audiotesting"
)

// Het tempoverschil tussen de twee pratende duiven is een nuance op
// één gedeelde stem. Deze test bewaakt de kant die de gebruiker hoort: wat er
// in de geconfigureerde personas staat, is wat de spreekbeurt aan de
// TTS-provider meegeeft, en de stakeholder praat sneller dan de kennispartner.
func TestMeetingSpeaker_SendsThePersonaTempoToTheVoice(t *testing.T) {
	personas, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("personas laden: %v", err)
	}
	tempo := map[string]float64{}
	for _, id := range []string{"biological", "individualist"} {
		p, ok := personas.Get(id)
		if !ok {
			t.Fatalf("persona %q ontbreekt", id)
		}
		tempo[id] = p.SpeakingSpeed()
		if tempo[id] == 0 {
			t.Errorf("persona %q heeft geen spreektempo; dan valt er niets te horen", id)
		}
	}
	if tempo["individualist"] <= tempo["biological"] {
		t.Errorf("stakeholder %v moet sneller praten dan de kennispartner %v",
			tempo["individualist"], tempo["biological"])
	}

	for _, id := range []string{"biological", "individualist"} {
		t.Run(id, func(t *testing.T) {
			gllm := newGatedLLM("Roekoe. ", "Ik vind daar wat van.")
			voice := newFakeVoice("mp3-frame-1")
			rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

			session := rig.openSession(t, id, "nl")
			es := rig.events(t, session)
			rig.say(t, session, "We willen de duiven van het Stationsplein weren.")
			nextEvent(t, es.ch, EventTranscript)
			if status, body := rig.respond(t, session); status != http.StatusAccepted {
				t.Fatalf("respond: status = %d, body = %s", status, body)
			}
			collectSpokenText(t, es.ch)

			calls := voice.speakCalls()
			if len(calls) != 1 {
				t.Fatalf("TTS-aanroepen = %+v, verwacht er één", calls)
			}
			if calls[0].Speed != tempo[id] {
				t.Errorf("spreektempo = %v, verwacht %v uit personas.yaml", calls[0].Speed, tempo[id])
			}
		})
	}
}

// speedMock is een provider mét tempoknop: hij onthoudt waarmee hij is
// aangeroepen.
type speedMock struct {
	*audiotesting.MockSynthesizer
	speed      float64
	viaAtSpeed bool
}

func (m *speedMock) SynthesizeAtSpeed(ctx context.Context, voiceID, language string, speed float64) (*audio.SynthesizeSession, error) {
	m.speed = speed
	m.viaAtSpeed = true
	return m.MockSynthesizer.Synthesize(ctx, voiceID, language)
}

// De Voice-seam geeft het tempo door aan een provider die het kent.
func TestSynthesizerVoice_PassesTheTempoToAProviderThatKnowsIt(t *testing.T) {
	sess := newScriptedSession()
	mock := &speedMock{MockSynthesizer: sess.synthesizer()}
	v := newSynthesizerVoice(mock, AudioFormat{Codec: CodecPCM16, SampleRate: 24000}, nil)

	if _, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.",
		SpeechRequest{VoiceID: "stem-1", Lang: "nl", Speed: 1.10}); err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	if !mock.viaAtSpeed || mock.speed != 1.10 {
		t.Errorf("tempo-aanroep = (%v, %v), verwacht (true, 1.10)", mock.viaAtSpeed, mock.speed)
	}
}

// Zonder tempo blijft het de gewone aanroep, ook bij een provider die de
// tempoknop wél kent — dan kiest de dienst zelf.
func TestSynthesizerVoice_WithoutATempoUsesThePlainCall(t *testing.T) {
	sess := newScriptedSession()
	mock := &speedMock{MockSynthesizer: sess.synthesizer()}
	v := newSynthesizerVoice(mock, AudioFormat{Codec: CodecPCM16, SampleRate: 24000}, nil)

	if _, err := speakAll(context.Background(), v, "Een uiting die lang genoeg is om te blijven staan.",
		SpeechRequest{VoiceID: "stem-1", Lang: "nl"}); err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	if mock.viaAtSpeed {
		t.Errorf("zonder tempo hoort de gewone Synthesize te lopen")
	}
}
