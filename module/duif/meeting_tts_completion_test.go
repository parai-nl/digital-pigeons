package duif

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/ai-maps-duiven/module/duif/ttsresult"
	"github.com/parai-nl/parai-core/audio/audiotesting"
)

func TestSpeechCompletion_PartialProviderFailureIsNotSuccess(t *testing.T) {
	sess := newScriptedSession()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{Codec: CodecPCM16, SampleRate: 24000}, nil)
	v.coalesce = 1
	sp, err := v.Open(t.Context(), SpeechRequest{VoiceID: "fixture", Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.deliver([]byte{1, 0}); err != nil {
		t.Fatal(err)
	}
	if got := <-sp.Frames(); len(got) != 2 {
		t.Fatalf("first frame=%v", got)
	}
	sess.finishResult(errors.New("private-provider-error"))
	sess.finishAudio()
	for range sp.Frames() {
	}
	if err := <-sp.(interface{ Result() <-chan error }).Result(); err == nil || strings.Contains(err.Error(), "private-") {
		t.Fatalf("terminal=%v", err)
	}
}
func TestSpeechCompletion_MissingTerminalContractIsRejected(t *testing.T) {
	sess := newScriptedSession()
	api := sess.session()
	ttsresult.Set(api, nil)
	v := newSynthesizerVoice(&audiotesting.MockSynthesizer{Session: api}, AudioFormat{}, nil)
	if _, err := v.Open(t.Context(), SpeechRequest{VoiceID: "fixture"}); !errors.Is(err, errSpeechUnconfirmed) {
		t.Fatalf("error=%v", err)
	}
	if closed, _ := sess.counts(); closed != 1 {
		t.Fatalf("cleanup=%d", closed)
	}
}
func TestSpeechCompletion_AudioEndWaitsForTerminalResult(t *testing.T) {
	sess := newScriptedSession()
	v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{}, nil)
	v.idle = 50 * time.Millisecond
	sp, err := v.Open(t.Context(), SpeechRequest{VoiceID: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	sess.deliver([]byte{1, 0})
	sess.audioOnce.Do(func() { close(sess.audioCh) })
	if frame := <-sp.Frames(); len(frame) == 0 {
		t.Fatal("tail lost")
	}
	select {
	case <-sp.Frames():
		t.Fatal("audio close was treated as confirmed completion")
	default:
	}
	sess.finishResult(nil)
	for range sp.Frames() {
	}
	if err := <-sp.(interface{ Result() <-chan error }).Result(); err != nil {
		t.Fatal(err)
	}
}
func TestSpeechCompletion_TerminalTimeoutAndCancellationAreBounded(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "cancel"}[cancelEarly], func(t *testing.T) {
			sess := newScriptedSession()
			v := newSynthesizerVoice(sess.synthesizer(), AudioFormat{}, nil)
			v.idle = 10 * time.Millisecond
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sp, err := v.Open(ctx, SpeechRequest{VoiceID: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			if cancelEarly {
				cancel()
			}
			for range sp.Frames() {
			}
			if err := <-sp.(interface{ Result() <-chan error }).Result(); err == nil {
				t.Fatal("interrupted speech reported success")
			}
			if closed, _ := sess.counts(); closed != 1 {
				t.Fatalf("cleanup=%d", closed)
			}
		})
	}
}
func TestSpeechCompletion_MeetingKeepsWrittenResponseAndReportsSpeechFailure(t *testing.T) {
	voice := newFakeVoice("partial-audio")
	voice.resultErr = errors.New("private-provider-echo")
	rig := newMeetingRig(t, nil, func(e *meetingEngine) { e.voice = voice })
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	status, _ := rig.do(t, "POST", "/v1/meeting/sessions/"+id+"/respond", nil)
	if status != 202 {
		t.Fatal(status)
	}
	text, events := collectSpokenText(t, es.ch)
	if text == "" {
		t.Fatal("written fallback lost")
	}
	var done, tts bool
	for _, ev := range events {
		if ev.Type == EventTTSDone {
			tts = ev.Error == "tts_incomplete" && ev.Frames == 1
		}
		if ev.Type == EventResponseDone {
			done = true
		}
		if ev.Type == EventError {
			t.Fatal("speech error incorrectly failed the written response")
		}
	}
	if !tts || !done {
		t.Fatalf("speech/response terminal missing: %v", eventTypes(events))
	}
	if strings.Contains(rig.logs(), "private-provider") {
		t.Fatal("provider payload leaked")
	}
}

type earlyFailedVoice struct{}

func (earlyFailedVoice) Format() AudioFormat {
	return AudioFormat{Codec: CodecPCM16, SampleRate: 24000}
}
func (earlyFailedVoice) Open(context.Context, SpeechRequest) (Speech, error) {
	return earlyFailedSpeech{}, nil
}

type earlyFailedSpeech struct{}

func (earlyFailedSpeech) Say(string)            {}
func (earlyFailedSpeech) End()                  {}
func (earlyFailedSpeech) Frames() <-chan []byte { ch := make(chan []byte); close(ch); return ch }
func (earlyFailedSpeech) Result() <-chan error {
	ch := make(chan error, 1)
	ch <- errSpeechUnconfirmed
	close(ch)
	return ch
}
func TestSpeechCompletion_FailureWaitsForWrittenEndBeforePauseAndBarrier(t *testing.T) {
	gllm := newGatedLLM("A complete first written sentence. The ", "final written sentence.")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = earlyFailedVoice{} })
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "A synthetic plan.")
	nextEvent(t, es.ch, EventTranscript)
	if status, _ := rig.respond(t, id); status != 202 {
		t.Fatal(status)
	}
	gllm.let(t)
	tts := nextEvent(t, es.ch, EventTTSDone)
	if tts.Error != "tts_incomplete" {
		t.Fatalf("tts=%+v", tts)
	}
	s, _ := rig.engine.session(id, testUser.ID)
	s.mu.Lock()
	gen := s.turnGen
	failed, complete, paused, speaking := s.playback.failed, s.playback.complete, s.autonomyPaused, s.speaking
	s.mu.Unlock()
	if !failed || complete || paused || !speaking {
		t.Fatalf("premature release: failed=%v complete=%v paused=%v speaking=%v", failed, complete, paused, speaking)
	}
	gllm.let(t)
	text, events := collectSpokenText(t, es.ch)
	if !strings.Contains(text, "final written sentence") {
		t.Fatal("later text discarded")
	}
	if hasEvent(events, EventError) {
		t.Fatal("text generation failed with speech")
	}
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.autonomyPaused && !s.speaking && s.turnGen == gen+1
	}, "server must pause and release only after response_done")
}

func TestSpeechCompletion_ExplicitStopCancelsWrittenFallbackImmediately(t *testing.T) {
	gllm := newGatedLLM("A complete first written sentence. The ", "final written sentence.")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = earlyFailedVoice{} })
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "A synthetic plan.")
	nextEvent(t, es.ch, EventTranscript)
	if status, _ := rig.respond(t, id); status != 202 {
		t.Fatal(status)
	}
	gllm.let(t)
	nextEvent(t, es.ch, EventTTSDone)
	if status, _ := rig.do(t, "POST", "/v1/meeting/sessions/"+id+"/stop", nil); status != 202 {
		t.Fatal(status)
	}
	nextEvent(t, es.ch, EventStopped)
	select {
	case <-gllm.canceled:
	case <-time.After(time.Second):
		t.Fatal("Stop left written fallback running")
	}
	s, _ := rig.engine.session(id, testUser.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.autonomyPaused || s.speaking {
		t.Fatal("Stop failed to pause and release")
	}
}
