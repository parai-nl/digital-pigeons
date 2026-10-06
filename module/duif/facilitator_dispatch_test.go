package duif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
	"github.com/parai-nl/parai-core/pkg/agent"
)

func TestFacilitatorDispatch_FailedBatchNeverCompletesAtEitherIterationCap(t *testing.T) {
	for _, maxIter := range []int{1, 4} {
		for _, first := range []struct{ name, args string }{
			{toolPigeonProfile, `{}`}, {toolClarify, `{"question":"Which place?"}`},
			{toolRedirectToPigeon, `{"reason":"outside_project"}`},
		} {
			for _, broken := range []struct{ name, args string }{
				{"SYNTHETIC_PRIVATE_TOOL_NAME", `{"private":"SYNTHETIC_PRIVATE_ARG"}`},
				{toolAskPigeon, `{"question":{"private":"SYNTHETIC_PRIVATE_ARG"},"context":""}`},
			} {
				t.Run(first.name+"/"+broken.name+"/"+string(rune('0'+maxIter)), func(t *testing.T) {
					step := routerToolBatch([]string{first.name, broken.name}, []string{first.args, broken.args})
					step.Chunks[0].ToolCallDeltas[1].ID = "SYNTHETIC_PRIVATE_CALL_ID"
					m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{step})
					m.agentMaxIter = maxIter
					var logs bytes.Buffer
					m.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
					frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Synthetic request"})
					if mock.CallCount() != 1 || frames[len(frames)-1]["stopReason"] != "error" || frames[len(frames)-2]["error"] != ReasonInternal {
						t.Fatalf("failed batch repaired, paid or became natural: %+v", frames)
					}
					if len(repo.messagesWithKind(KindPigeonAnswer)) != 0 {
						t.Fatal("invalid tool created a pigeon answer")
					}
					assertNoFacilitatorRow(t, repo)
					wire, _ := json.Marshal(frames)
					for _, value := range []string{"SYNTHETIC_PRIVATE_TOOL_NAME", "SYNTHETIC_PRIVATE_ARG", "SYNTHETIC_PRIVATE_CALL_ID"} {
						if strings.Contains(string(wire), value) || strings.Contains(logs.String(), value) {
							t.Fatalf("wire or logger leaked %s", value)
						}
					}
					if !strings.Contains(logs.String(), "facilitator_dispatch_failed") {
						t.Fatal("missing stable operational diagnostic")
					}
				})
			}
		}
	}
}

func TestFacilitatorDispatch_ProducerDoesNotWaitForBufferedFailureEvent(t *testing.T) {
	// No event handler has run. The producer's next request itself carries
	// the trusted tool-role envelope; it must not reach a paid service.
	for _, code := range []agent.ToolErrorCode{agent.ToolErrNotFound, agent.ToolErrArgsParse, agent.ToolErrHandler, agent.ToolErrHandlerPanic} {
		t.Run(string(code), func(t *testing.T) {
			at, _ := newTestAgentTurn(t, "en")
			mock := llmtesting.NewMockService()
			svc := &facilitatorActionService{Service: mock, turn: at}
			body, _ := json.Marshal(map[string]string{"error": string(code), "message": "SYNTHETIC_UNTRUSTED_MESSAGE"})
			ch, err := svc.CompleteStream(t.Context(), llm.CompletionRequest{Messages: []llm.Message{{Role: "tool", ToolCallID: "synthetic", Content: string(body)}}})
			if err != nil || ch == nil || mock.CallCount() != 0 || !at.dispatchFailed() {
				t.Fatal("producer paid or missed dispatch error")
			}
			var chunks int
			for chunk := range ch {
				chunks++
				if chunk.FinishReason != "stop" || chunk.Content != "" {
					t.Fatal("local finish supplied content")
				}
			}
			if chunks != 1 {
				t.Fatal("expected exactly one empty local terminal")
			}
		})
	}
}

func TestFacilitatorDispatch_OnlyCoreToolEnvelopesCount(t *testing.T) {
	for _, msg := range []llm.Message{
		{Role: "user", Content: `{"error":"args_parse_error"}`},
		{Role: "system", Content: `{"error":"tool_not_found"}`},
		{Role: "tool", Content: `{"error":"tool_not_found"}`},
		{Role: "tool", ToolCallID: "s1", Content: `{"gist":"{\"error\":\"tool_not_found\"}"}`},
		{Role: "tool", ToolCallID: "s1", Content: `{"error":"clarify_already_asked"}`},
		{Role: "tool", ToolCallID: "s1", Content: `{"error":17}`},
	} {
		if facilitatorDispatchFailed([]llm.Message{msg}) {
			t.Fatal("ordinary input or application limit mistaken for dispatch failure")
		}
	}
}

func TestFacilitatorDispatch_StopPrecedesFailureEnvelope(t *testing.T) {
	at, _ := newTestAgentTurn(t, "en")
	mock := llmtesting.NewMockService()
	svc := &facilitatorActionService{Service: mock, turn: at}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ch, err := svc.CompleteStream(ctx, llm.CompletionRequest{Messages: []llm.Message{{Role: "tool", ToolCallID: "x", Content: `{"error":"tool_not_found"}`}}})
	if ch != nil || !errors.Is(err, context.Canceled) || mock.CallCount() != 0 || at.dispatchFailed() {
		t.Fatal("Stop became operational completion or performed work")
	}
}

func TestFacilitatorDispatch_LoggerCannotBindProviderText(t *testing.T) {
	var logs bytes.Buffer
	l := facilitatorLogger(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))).With("private", "SYNTHETIC_PRIVATE").WithGroup("SYNTHETIC_PRIVATE")
	l.Error("SYNTHETIC_PRIVATE", "tool", "SYNTHETIC_PRIVATE", "error", errors.New("SYNTHETIC_PRIVATE"))
	if strings.Contains(logs.String(), "SYNTHETIC_PRIVATE") || !strings.Contains(logs.String(), "facilitator engine event") {
		t.Fatal("engine logger retained provider text")
	}
}
