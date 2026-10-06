package duif

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// These tests exercise the real public chat handler and pinned agent loop
// with synthetic provider streams. They do not prove a live model selects
// the right tool or produces a semantically necessary clarification.
func TestFacilitatorAction_IdentityProfilesNeedOneRoutingCallAcrossLanguages(t *testing.T) {
	for _, persona := range []string{"biological", "individualist"} {
		for _, lang := range []string{"en", "nl"} {
			t.Run(persona+"/"+lang, func(t *testing.T) {
				m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("profile", toolPigeonProfile, `{}`)})
				frames := agentTurnFrames(t, m, map[string]any{"pigeonId": persona, "lang": lang, "message": "Are you an actual pigeon?"})
				if strings.Join(frameTypes(frames), ",") != "tool,tool_result,text,done" || frames[3]["stopReason"] != "natural" {
					t.Fatalf("profile route did not complete: %+v", frames)
				}
				if mock.CallCount() != 1 || mock.Calls[0].ToolChoice == nil || mock.Calls[0].ToolChoice.Type != "auto" {
					t.Fatal("one routing call on auto expected; no provider closing call")
				}
				if !strings.Contains(frames[2]["delta"].(string), map[string]string{"en": "constructed AI", "nl": "geconstrueerde AI"}[lang]) {
					t.Fatal("scripted truthful profile is missing")
				}
				profile := frames[2]["delta"].(string)
				if len([]rune(profile)) > 650 || strings.ContainsAny(profile, "\n\r") || strings.Count(profile, ".") > 3 {
					t.Fatalf("identity response must remain one compact paragraph: %d characters", len([]rune(profile)))
				}
				p, _ := m.personas.Get(persona)
				for _, topic := range p.Topics {
					if !strings.Contains(profile, topic.LabelFor(lang)) || strings.Contains(profile, topic.DescribeFor(lang)) {
						t.Fatal("theme overview must retain names without long descriptions")
					}
				}
				t.Logf("profile characters=%d; sentences=%d", len([]rune(profile)), strings.Count(profile, "."))
				row := facilitatorRow(t, repo)
				if row.Content != frames[2]["delta"] || row.Partial || len(row.Sources) != 0 {
					t.Fatal("profile must persist exactly, without borrowed evidence")
				}
			})
		}
	}
}

// De vangrail achter de routeringsronde, en de enige die er nog is: de
// aanvraag vraagt om een tool met "auto" en dwingt niets meer af, dus
// een ronde die met vrije tekst, leeg of afgekapt sluit is precies wat er in
// productie kan gebeuren. Zij mag nooit een antwoord worden of opgeslagen hulp.
func TestFacilitatorAction_ARoundWithoutActionCannotBecomeSuccessOrStoredHelp(t *testing.T) {
	for _, lang := range []string{"en", "nl"} {
		for name, step := range map[string]llmtesting.StreamStep{
			"free_prose": textStep("SYNTHETIC_NOT_AN_ANSWER"),
			"empty":      {Chunks: []llm.StreamChunk{{FinishReason: "stop"}}},
			"truncated":  {Chunks: []llm.StreamChunk{{Content: "SYNTHETIC_NOT_AN_ANSWER", FinishReason: "length"}}},
		} {
			t.Run(lang+"/"+name, func(t *testing.T) {
				m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{step})
				frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "lang": lang, "message": "What are you?"})
				if strings.Join(frameTypes(frames), ",") != "error,done" || frames[0]["error"] != ReasonInternal || frames[1]["stopReason"] != "error" {
					t.Fatalf("non-action incorrectly completed: %+v", frames)
				}
				if mock.CallCount() != 1 || mock.Calls[0].ToolChoice.Type != "auto" {
					t.Fatal("no retries or paid completion for a round without action")
				}
				assertNoFacilitatorRow(t, repo)
				if len(repo.messagesWithKind(KindPigeonAnswer)) != 0 {
					t.Fatal("non-answer must not be stored")
				}
			})
		}
	}
}

// De toolset kent geen tussenstap meer (de aparte zoektool is weggehaald):
// elke tool van de facilitator levert een uitkomst of een storing op. De twee tests die het
// tussenstap-gedrag van `search_sources` vastlegden (een tweede routeringsronde
// mocht, en de iteratiegrens mocht daar geen antwoord van maken) zijn daarmee
// vervallen. Het onderliggende slot — een beurt zonder antwoordactie eindigt in
// een fout — staat nog in de twee tests hieronder en in de vrije-prozatest.

func TestFacilitatorAction_EmptyClarificationDoesNotComplete(t *testing.T) {
	for _, question := range []string{"   ", "[99]"} {
		t.Run(question, func(t *testing.T) {
			args, _ := json.Marshal(clarifyArgs{Question: question})
			m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("clarify", toolClarify, string(args))})
			frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Which place?"})
			if strings.Join(frameTypes(frames), ",") != "tool,tool_result,error,done" || frames[3]["stopReason"] != "error" || mock.CallCount() != 1 {
				t.Fatalf("empty clarification was an answer: %+v", frames)
			}
			assertNoFacilitatorRow(t, repo)
		})
	}
}

func TestFacilitatorAction_DoesNotAffectPigeonRequestsOrPayAfterFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name, answer := "answer", textStep("Synthetic bounded answer.")
		if failed {
			name, answer = "budget", llmtesting.StreamStep{Chunks: []llm.StreamChunk{{Err: errBudgetUpstream}}}
		}
		t.Run(name, func(t *testing.T) {
			m, _, mock := newAgentModule(t, []llmtesting.StreamStep{askCoolsingel(), answer})
			frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "A synthetic question."})
			if mock.CallCount() != 2 || mock.Calls[1].ToolChoice != nil || len(mock.Calls[1].Tools) != 0 {
				t.Fatal("only facilitator routing carries a tool choice; no third provider call")
			}
			want := "natural"
			if failed {
				want = "error"
				if frames[len(frames)-2]["error"] != ReasonBudgetExceeded {
					t.Fatal("budget failure must remain visible")
				}
			}
			if frames[len(frames)-1]["stopReason"] != want {
				t.Fatalf("wrong terminal result: %+v", frames)
			}
		})
	}
}

func TestFacilitatorAction_OldExchangesAndAttemptsAreNotCurrentAnswers(t *testing.T) {
	at, _ := newTestAgentTurn(t, "en")
	at.recordExchange("Earlier question", "Earlier answer")
	at.takePigeonCall()
	if at.hasUsefulOutcome() || at.facilitatorText() != "" {
		t.Fatal("old exchanges or an attempted call cannot manufacture a current answer")
	}
}

// De vorm van de routeringsronde, apart vastgelegd. Zij vraagt om
// een tool met "auto" en dwingt er nooit meer een af: met de echte
// facilitatorprompt en deze toolset bleef de "required"-vorm op de vaste alias
// via de proxy 8 van de 12 keer 124-126 s stil vóór het eerste token, waar
// dezelfde aanvraag op "auto" 14 van de 14 keer binnen 7 s terugkwam — telkens
// met een toolcall. De andere tests hierboven lezen deze waarde mee; deze staat
// er zodat een terugdraai naar "required" een besluit is en geen typefout.
func TestFacilitatorAction_RoutingAsksForAToolButNeverForcesOne(t *testing.T) {
	at, _ := newTestAgentTurn(t, "nl")
	mock := llmtesting.NewMockService()
	svc := &facilitatorActionService{Service: mock, turn: at}
	if _, err := svc.CompleteStream(t.Context(), llm.CompletionRequest{}); err != nil {
		t.Fatalf("routeringsronde gaf een fout: %v", err)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("routeringsronde deed %d providercalls, want 1", mock.CallCount())
	}
	if tc := mock.Calls[0].ToolChoice; tc == nil || tc.Type != "auto" {
		t.Fatalf("routeringsronde stuurde tool_choice %+v mee, want \"auto\"", tc)
	}
}

func TestFacilitatorAction_CancellationPrecedesEvenLocalCompletion(t *testing.T) {
	at, _ := newTestAgentTurn(t, "en")
	at.profileText = "Synthetic profile"
	mock := llmtesting.NewMockService()
	svc := &facilitatorActionService{Service: mock, turn: at}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ch, err := svc.CompleteStream(ctx, llm.CompletionRequest{})
	if ch != nil || !errors.Is(err, context.Canceled) || mock.CallCount() != 0 {
		t.Fatal("Stop must not become a local natural completion or start a provider call")
	}
}

func TestFacilitatorAction_EmptyPigeonAttemptCannotCreateAnsweredNotice(t *testing.T) {
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{askCoolsingel(), textStep("")})
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "A synthetic question."})
	if frames[len(frames)-1]["stopReason"] != "error" || mock.CallCount() != 2 {
		t.Fatalf("empty pigeon attempt completed: %+v", frames)
	}
	for _, f := range frames {
		if f["type"] == "text" || f["type"] == "artifact_delta" {
			t.Fatal("empty answer got an answered notice")
		}
	}
	assertNoFacilitatorRow(t, repo)
	if len(repo.messagesWithKind(KindPigeonAnswer)) != 0 {
		t.Fatal("empty answer persisted")
	}
}
