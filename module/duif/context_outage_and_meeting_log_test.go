package duif

import (
	"context"
	"errors"
	"github.com/parai-nl/parai-core/data"
	"strings"
	"testing"
)

type contextOutageKnowledge struct{ *fakeKnowledge }

func (b contextOutageKnowledge) GetWithContext(context.Context, data.NodeID) (*data.DestillaatContext, error) {
	return nil, errors.New("synthetic context read outage")
}

func TestRetrieval_ContextOutageCannotRestoreRejectedSummary(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "bad", samenvatting: "SENTINEL unchecked universal claim.", quote: "Rejected extraction.", score: .95, paper: "synthetic"})
	kg.nodes["quote:bad"]["grounded"] = false
	res, err := newTestRetrieval(t, contextOutageKnowledge{kg}, nil).run(t.Context(), "What is supported?")
	if !errors.Is(err, errKBUnavailable) || len(res.Sources) != 0 || strings.Contains(res.RAGSection, "SENTINEL") {
		t.Fatalf("context outage returned err=%v, status=%s, sources=%+v, summaryInPrompt=%v", err, res.Status, res.Sources, strings.Contains(res.RAGSection, "SENTINEL"))
	}
}

func TestMeetingLog_PreAudioCorrectionIsHumanContent(t *testing.T) {
	rig := eagerRig(t, newCandidateLLM(pass()), nil)
	s := rig.session(t, rig.openMeeting(t, map[string]any{"pigeonId": "individualist"}))
	s.appendSegment("We propose to remove the pigeons.", "")
	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("begin")
	}
	defer cancel()
	defer s.endTurn(gen)
	s.appendSegment("No, we withdraw that proposal.", "")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playback.expected || s.playback.started {
		t.Fatal("fixture unexpectedly emitted audio")
	}
	if s.transcript.segments[len(s.transcript.segments)-1].PigeonWindow || s.lastHumanSeq != 2 {
		t.Fatalf("pre-audio human correction suppressed: pigeonWindow=%v lastHumanSeq=%d", s.transcript.segments[len(s.transcript.segments)-1].PigeonWindow, s.lastHumanSeq)
	}
}

func TestMeetingLog_AutomaticTopicDoesNotPersistInLog(t *testing.T) {
	rig := eagerRig(t, newCandidateLLM(pass()), nil)
	s := rig.session(t, rig.openMeeting(t, map[string]any{"pigeonId": "individualist"}))
	s.appendSegment("An ordinary synthetic proposal.", "")
	snapshot := s.detectionWindow(0)
	gen, ctx, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("begin")
	}
	rig.engine.speakCandidate(s, gen, ctx, cancel, &meetingCandidate{
		ID: newID(), Text: "A synthetic contribution.", TopicID: "city-space",
		Status: KBPersonaOnly, Window: snapshot.Segments,
	})
	if strings.Contains(rig.logs(), "city-space") || strings.Contains(rig.logs(), "synthetic proposal") {
		t.Fatal("automatic content category persisted in completion log")
	}
}
