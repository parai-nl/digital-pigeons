package duif

import (
	"context"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

func routerToolBatch(names, args []string) llmtesting.StreamStep {
	var deltas []llm.ToolCallDelta
	for i, name := range names {
		deltas = append(deltas, llm.ToolCallDelta{Index: i, ID: name, Name: name, ArgsFrag: args[i]})
	}
	return llmtesting.StreamStep{Chunks: []llm.StreamChunk{{ToolCallDeltas: deltas, FinishReason: "tool_calls"}}}
}

func TestFacilitatorRouter_ProfileDoesNotSkipValidAnswerInSameBatch(t *testing.T) {
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{
		routerToolBatch([]string{toolPigeonProfile, toolAskPigeon}, []string{`{}`, `{"question":"Synthetic question","context":""}`}),
		textStep("Synthetic pigeon answer."),
	})
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Who are you, and what about this synthetic decision?"})
	if len(repo.messagesWithKind(KindPigeonAnswer)) != 1 || mock.CallCount() != 2 || frames[len(frames)-1]["stopReason"] != "natural" {
		t.Fatal("valid pigeon action in the same batch was skipped, repeated or left incomplete")
	}
	if mock.Calls[0].ToolChoice == nil || mock.Calls[0].ToolChoice.Type != "auto" || mock.Calls[1].ToolChoice != nil {
		t.Fatal("the tool choice must apply only to routing")
	}
}

func TestFacilitatorRouter_UnknownActionAfterProfileCannotFinishNaturally(t *testing.T) {
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{
		routerToolBatch([]string{toolPigeonProfile, "ask_pigeon_typo"}, []string{`{}`, `{"question":"Synthetic question","context":""}`}),
	})
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Who are you, and what about this synthetic decision?"})
	if mock.CallCount() != 1 || len(repo.messagesWithKind(KindPigeonAnswer)) != 0 {
		t.Fatal("fixture unexpectedly called a provider again or answered the question")
	}
	var hasError bool
	for _, frame := range frames {
		hasError = hasError || frame["type"] == "error"
	}
	if frames[len(frames)-1]["stopReason"] == "natural" || !hasError {
		t.Fatalf("unexecuted requested action became a complete profile-only answer: types=%v done=%v", frameTypes(frames), frames[len(frames)-1])
	}
}

func TestFacilitatorRouter_InvalidAnswerAfterProfileCannotReloadAsComplete(t *testing.T) {
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		routerToolBatch([]string{toolPigeonProfile, toolAskPigeon}, []string{`{}`, `{"question":17,"context":""}`}),
	})
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Who are you, and what about this synthetic decision?"})
	if len(repo.messagesWithKind(KindPigeonAnswer)) != 0 {
		t.Fatal("invalid argument fixture unexpectedly answered")
	}
	if frames[len(frames)-1]["stopReason"] == "natural" {
		t.Fatalf("invalid requested answer became a complete profile-only turn: types=%v stopReason=%v", frameTypes(frames), frames[len(frames)-1]["stopReason"])
	}
	for _, row := range repo.messagesWithKind(KindText) {
		if row.Persona == PersonaFacilitator && !row.Partial {
			t.Fatalf("failed multi-tool turn persisted a complete facilitator row: stopReason=%v partial=%v", frames[len(frames)-1]["stopReason"], row.Partial)
		}
	}
}

func TestFacilitatorRouter_StopAtProfileResultPreventsLocalCompletion(t *testing.T) {
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("profile", toolPigeonProfile, `{}`)})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pt, err := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "Who are you?"})
	if err != nil {
		t.Fatal("synthetic preparation failed")
	}
	var terminal, text bool
	m.runAgentTurn(ctx, pt, func(frame any) error {
		switch frame.(type) {
		case toolResultFrame:
			cancel()
		case doneFrame:
			terminal = true
		case textFrame:
			text = true
		}
		return nil
	})
	if terminal || text || mock.CallCount() != 1 {
		t.Fatal("Stop at terminal tool result leaked a completion or another provider call")
	}
	assertNoFacilitatorRow(t, repo)
}
