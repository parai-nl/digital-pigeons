package duif

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// Real public handler + pinned loop with synthetic routing, not live classification.
func TestFacilitatorRedirect_FixedGuidanceNeverBorrowsOldAnswer(t *testing.T) {
	for _, persona := range []string{"biological", "individualist"} {
		for _, lang := range []string{"en", "nl"} {
			for _, reason := range []string{"outside_project", "override_attempt"} {
				t.Run(persona+"/"+lang+"/"+reason, func(t *testing.T) {
					args, _ := json.Marshal(redirectToPigeonArgs{Reason: reason})
					m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("r1", toolRedirectToPigeon, string(args))})
					sid := "redirect-history"
					if err := repo.CreateSession(t.Context(), ChatSession{ID: sid, UserID: testUser.ID, PigeonID: persona, Lang: lang}); err != nil {
						t.Fatal(err)
					}
					old := ChatMessage{ID: "prior-answer", SessionID: sid, Role: "assistant", Kind: KindPigeonAnswer, Persona: persona, Question: "Earlier synthetic question", Content: "SYNTHETIC_OLD_SOURCE_CLAIM [1]", KBStatus: string(KBSources), Sources: []Source{{Number: 1, ID: "synthetic-old-source"}}}
					if err := repo.AppendMessage(t.Context(), old); err != nil {
						t.Fatal(err)
					}
					frames := agentTurnFrames(t, m, map[string]any{"sessionId": sid, "pigeonId": persona, "lang": lang, "message": "Synthetic request"})
					if strings.Join(frameTypes(frames), ",") != "tool,tool_result,text,done" || frames[3]["stopReason"] != "natural" {
						t.Fatalf("redirect did not finish: %+v", frames)
					}
					want := redirectOutsideProject[lang]
					if reason == "override_attempt" {
						want = redirectOverrideAttempt[lang]
					}
					if frames[2]["delta"] != want || mock.CallCount() != 1 || mock.Calls[0].ToolChoice == nil || mock.Calls[0].ToolChoice.Type != "auto" {
						t.Fatal("expected fixed guidance and one routing call on auto")
					}
					row := facilitatorRow(t, repo)
					if row.Content != want || row.Persona != PersonaFacilitator || row.Kind != KindText || len(row.Sources) != 0 || row.Partial {
						t.Fatal("incorrect persisted guidance/authorship")
					}
					if got := repo.messagesWithKind(KindPigeonAnswer); len(got) != 1 || got[0].Content != old.Content {
						t.Fatal("redirect altered pigeon answers")
					}
					encoded, _ := json.Marshal(frames)
					for _, leak := range []string{"SYNTHETIC_OLD_SOURCE_CLAIM", "synthetic-old-source", reason, `"sources"`, `"arguments"`} {
						if strings.Contains(string(encoded), leak) {
							t.Fatalf("wire leaked %s", leak)
						}
					}
				})
			}
		}
	}
}

func TestFacilitatorRedirect_InvalidReasonCannotSupplyText(t *testing.T) {
	for _, reason := range []string{"", "outside_project [1]", "SYNTHETIC_ARBITRARY_PROSE"} {
		t.Run(reason, func(t *testing.T) {
			args, _ := json.Marshal(redirectToPigeonArgs{Reason: reason})
			m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("r1", toolRedirectToPigeon, string(args))})
			frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Synthetic request"})
			if strings.Join(frameTypes(frames), ",") != "tool,tool_result,error,done" || frames[2]["error"] != ReasonInternal || frames[3]["stopReason"] != "error" || mock.CallCount() != 1 {
				t.Fatalf("invalid reason became success: %+v", frames)
			}
			assertNoFacilitatorRow(t, repo)
		})
	}
}

func TestFacilitatorRedirect_ConflictingBatchCannotAnswerOrComplete(t *testing.T) {
	for _, other := range []struct{ name, args string }{{toolPigeonProfile, `{}`}, {toolClarify, `{"question":"Which place?"}`}, {toolAskPigeon, `{"question":"Synthetic question","context":""}`}, {toolRedirectToPigeon, `{"reason":"override_attempt"}`}} {
		t.Run(other.name, func(t *testing.T) {
			step := llmtesting.StreamStep{Chunks: []llm.StreamChunk{{ToolCallDeltas: []llm.ToolCallDelta{
				{Index: 0, ID: "r1", Name: toolRedirectToPigeon, ArgsFrag: `{"reason":"outside_project"}`},
				{Index: 1, ID: "other", Name: other.name, ArgsFrag: other.args},
			}, FinishReason: "tool_calls"}}}
			m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{step})
			frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Synthetic request"})
			if frames[len(frames)-1]["stopReason"] != "error" || mock.CallCount() != 1 {
				t.Fatalf("conflicting route completed or paid: %+v", frames)
			}
			for _, f := range frames {
				if f["type"] == "artifact" || f["type"] == "text" || f["type"] == "sources" {
					t.Fatal("mixed redirect emitted answer")
				}
			}
			assertNoFacilitatorRow(t, repo)
		})
	}
	// A useful action preceding redirect is also a conflicting decision.
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{ToolCallDeltas: []llm.ToolCallDelta{
		{Index: 0, ID: "p1", Name: toolPigeonProfile, ArgsFrag: `{}`},
		{Index: 1, ID: "r1", Name: toolRedirectToPigeon, ArgsFrag: `{"reason":"outside_project"}`},
	}, FinishReason: "tool_calls"}}}})
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Synthetic request"})
	if frames[len(frames)-1]["stopReason"] != "error" || mock.CallCount() != 1 {
		t.Fatal("late redirect hid conflict")
	}
	assertNoFacilitatorRow(t, repo)
}

// De test die vastlegde dat een zoekopdracht aan een redirect vooraf mocht
// gaan, is met de aparte zoektool `search_sources` vervallen: de toolset kent
// geen tussenstap meer, dus elke tool vóór een redirect is per definitie een
// botsend besluit — dat blijft hierboven getoetst.

func TestFacilitatorRedirect_StopDoesNotPublishOrPay(t *testing.T) {
	for _, stage := range []string{"before_tool", "tool", "tool_result"} {
		t.Run(stage, func(t *testing.T) {
			if stage == "before_tool" {
				at, m := newTestAgentTurn(t, "en")
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, err := at.redirectToPigeon(ctx, redirectToPigeonArgs{Reason: "outside_project"})
				if !errors.Is(err, context.Canceled) || at.hasUsefulOutcome() || m.llm.(*llmtesting.MockService).CallCount() != 0 {
					t.Fatal("canceled tool did work")
				}
				return
			}
			m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("r1", toolRedirectToPigeon, `{"reason":"outside_project"}`)})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pt, terr := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "Synthetic request"})
			if terr != nil {
				t.Fatal(terr)
			}
			var seen []string
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				m.runAgentTurn(ctx, pt, func(frame any) error {
					switch frame.(type) {
					case toolFrame:
						if stage == "tool" {
							cancel()
							return context.Canceled
						}
					case toolResultFrame:
						if stage == "tool_result" {
							cancel()
							return context.Canceled
						}
					case textFrame:
						seen = append(seen, "text")
					case doneFrame:
						seen = append(seen, "done")
					}
					return nil
				})
			}()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("Stop left producer running")
			}
			if len(seen) != 0 || mock.CallCount() != 1 {
				t.Fatal("Stop published completion or paid")
			}
			assertNoFacilitatorRow(t, repo)
		})
	}
}
