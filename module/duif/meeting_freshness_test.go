package duif

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Wat een nog onhoorbare zelfstandige bijdrage mag afbreken, en wat niet.
//
// De regel is één regel: alleen een écht gecommit menselijk segment
// vervangt de aanleiding. Niet een partial, niet een klok. Dat verschil is het
// verschil tussen een duif die haar zin afmaakt en een duif die bij elk kuchje
// stopt.

func freshAutomaticFixture(t *testing.T) (*meetingRig, *meetingSession) {
	t.Helper()
	rig := eagerRig(t, newCandidateLLM(pass()), nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	s := rig.session(t, id)
	s.appendSegment("Remove pigeon nests.", "")
	rig.clock.skip(2 * time.Second)
	return rig, s
}

func lastHumanSeqOf(s *meetingSession) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHumanSeq
}

func TestMeetingFreshness_ConcurrentUserStopAlwaysKeepsItsPauseBarrier(t *testing.T) {
	for range 20 {
		_, s := freshAutomaticFixture(t)
		gen, ctx, cancel, ok := s.beginTurn(true)
		if !ok {
			t.Fatal("begin")
		}
		var wg sync.WaitGroup
		gate := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-gate; s.stopSpeaking() }()
		go func() { defer wg.Done(); <-gate; s.appendSegment("Keep those nests.", "") }()
		close(gate)
		wg.Wait()
		cancel()
		if ctx.Err() != context.Canceled || s.isSpeaking() || !s.settingsSnapshot().AutonomyPaused || s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "late"}) {
			t.Fatal("supersession released a concurrent user stop")
		}
	}
}

// Een gecommitte correctie breekt een nog onhoorbare zelfstandige bijdrage af;
// een partial doet dat uitdrukkelijk níét meer.
func TestMeetingFreshness_OnlyACommitCancelsAnUnheardAutomaticTurn(t *testing.T) {
	t.Run("committed", func(t *testing.T) {
		_, s := freshAutomaticFixture(t)
		ch, unsub := s.subscribe()
		defer unsub()
		nextEvent(t, ch, EventState)
		gen, ctx, cancel, ok := s.beginTurn(true)
		if !ok {
			t.Fatal("begin")
		}
		defer cancel()
		s.emitTurn(gen, MeetingEvent{Type: EventPersona, PigeonID: s.pigeonID})
		s.appendSegment("Keep those nests.", "")
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("obsolete provider context kept running")
		}
		stopped := nextEvent(t, ch, EventStopped)
		settings := nextEvent(t, ch, EventSettings)
		if stopped.Reason != "context_changed" || stopped.AutonomyPaused == nil || *stopped.AutonomyPaused ||
			settings.Settings == nil || settings.Settings.TurnID != stopped.TurnID || settings.Settings.AutonomyPaused {
			t.Fatalf("wrong barrier: %+v / %+v", stopped, settings)
		}
		if s.isSpeaking() || s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "late", Seq: 1}) {
			t.Fatal("canceled generator revived")
		}
		s.endTurn(gen)
		if s.settingsSnapshot().AutonomyPaused {
			t.Fatal("old completion introduced pause")
		}
		if lastHumanSeqOf(s) != 2 {
			t.Fatal("correction was not retained as human")
		}
	})
	t.Run("partial", func(t *testing.T) {
		_, s := freshAutomaticFixture(t)
		gen, ctx, cancel, ok := s.beginTurn(true)
		if !ok {
			t.Fatal("begin")
		}
		defer cancel()
		s.emitTurn(gen, MeetingEvent{Type: EventPersona, PigeonID: s.pigeonID})
		s.publishPartial("eh, wacht, ik bedoel", "")
		if ctx.Err() != nil {
			t.Fatal("een partial brak de nog onhoorbare bijdrage af")
		}
		if !s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: "de duif praat door"}) {
			t.Fatal("de beurt werd door een partial ongeldig gemaakt")
		}
		if !s.governorState().UncommittedSpeech {
			t.Error("de partial telde niet mee voor de stilte-eis")
		}
	})
}

func TestMeetingFreshness_ActualAudioRemainsEchoProtected(t *testing.T) {
	_, s := freshAutomaticFixture(t)
	gen, ctx, cancel, ok := s.beginTurn(true)
	if !ok {
		t.Fatal("begin")
	}
	defer cancel()
	defer s.endTurn(gen)
	if !s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1}) {
		t.Fatal("audio")
	}
	s.publishPartial("Pigeon speaker echo", "")
	s.appendSegment("Pigeon speaker echo.", "")
	if ctx.Err() != nil || lastHumanSeqOf(s) != 1 || s.governorState().UncommittedSpeech {
		t.Fatal("audio echo superseded its own turn")
	}
}

func TestMeetingFreshness_ManualIntentSurvivesHumanCorrectionWithoutFalseEcho(t *testing.T) {
	_, s := freshAutomaticFixture(t)
	gen, ctx, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("begin")
	}
	defer cancel()
	s.appendSegment("Keep those nests.", "")
	if ctx.Err() != nil || lastHumanSeqOf(s) != 2 || !s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: "An explicitly requested answer."}) {
		t.Fatal("manual intent canceled or human correction lost")
	}
	s.endTurn(gen)
	s.appendSegment("Another human line.", "")
	if lastHumanSeqOf(s) != 3 {
		t.Fatal("text-only completion invented an echo margin")
	}
}

// Een bijdrage die al klinkt wordt niet meer afgebroken: wie halverwege een zin
// wordt afgekapt klinkt kapotter dan wie zijn zin afmaakt.
func TestMeetingFreshness_AudibleContributionIsNotSuperseded(t *testing.T) {
	_, s := freshAutomaticFixture(t)
	gen, ctx, cancel, ok := s.beginTurn(true)
	if !ok {
		t.Fatal("begin")
	}
	defer cancel()
	defer s.endTurn(gen)
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	s.mu.Lock()
	s.playback.expected = true
	s.mu.Unlock()
	s.appendSegment("Iemand praat er doorheen.", "")
	if ctx.Err() != nil {
		t.Fatal("een hoorbare bijdrage werd alsnog afgebroken")
	}
}
