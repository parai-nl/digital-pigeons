package duif

import (
	"testing"

	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// All evidence below is invented test material, not a copy of corpus text.
// The sentinel strings mark editorial fields (summary, theme, perspective)
// that organise the corpus but are never evidence: a test can assert that they
// stay out of a request by looking for the sentinel.
var answerSourcesFixture = []Source{{
	Number:      1,
	Text:        "EDITORIAL_SENTINEL: all animals always choose blue shelters.",
	Theme:       "THEME_SENTINEL",
	Perspective: "PERSPECTIVE_SENTINEL",
	Ref: SourceRef{
		Paper:    "synthetic-study",
		Passages: []SourcePassage{{ID: "synthetic:a", Text: "In the northern enclosure, the authors proposed that animals might visit blue shelters."}},
	},
}}

// doneFrameOf finds the turn's done frame.
func doneFrameOf(t *testing.T, frames []sseFrame) sseFrame {
	t.Helper()
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i]["type"] == "done" {
			return frames[i]
		}
	}
	t.Fatalf("no done frame in %v", frames)
	return nil
}

// groundedMeetingRig is a meeting whose knowledge base actually answers, so a
// turn reaches kbStatus=sources and speaks with a source list.
func groundedMeetingRig(t *testing.T, script []llmtesting.StreamStep) (*meetingRig, *llmtesting.MockService) {
	t.Helper()
	mock := llmtesting.NewMockService()
	mock.SetStreamScript(script)
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{
		id: "meeting", samenvatting: "Remove pigeon nests from balconies and ledges.", score: .9,
		quote: answerSourcesFixture[0].Ref.Passages[0].Text, paper: "synthetic-study",
	})
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = withQueryAnswers(mock)
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = knowledge
		m.answerStream = nil
	}, func(e *meetingEngine) {
		e.voice = newFakeVoice("synthetic-meeting-voice")
		e.intervene = nil
	})
	return rig, mock
}
