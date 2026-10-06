package duif

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// policyRecorder legt de answerPolicy van elke antwoordcall vast. Die policy is
// waar "welke woorden schreef een mens werkelijk?" uit de bewaarde rijen wordt
// beantwoord in plaats van uit een gerenderd modelverzoek, en dus de plek waar
// die grens te zien is. Vroeger liep dezelfde vraag via de invoer van de live bronaudit.
type policyRecorder struct {
	mu       sync.Mutex
	policies []answerPolicy
}

func (p *policyRecorder) stream(ctx context.Context, service llm.Service, req llm.CompletionRequest, _ []Source, policy answerPolicy) (<-chan llm.StreamChunk, error) {
	p.mu.Lock()
	p.policies = append(p.policies, policy)
	p.mu.Unlock()
	return service.CompleteStream(ctx, req)
}

// only geeft de enige antwoordcall van de beurt terug.
func (p *policyRecorder) only(t *testing.T) answerPolicy {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.policies) != 1 {
		t.Fatalf("antwoordcalls=%d, want 1", len(p.policies))
	}
	return p.policies[0]
}

// Real comparison creation, prepareTurn and both actual chat engines. A
// comparison now copies the whole source conversation, so the first pigeon's
// answer travels along; together with the facilitator-generated context and an
// old assistant-owned question, none of it becomes human authorship.
func TestAnswerContext_ComparisonKeepsOnlyActualHumanRows(t *testing.T) {
	for _, engine := range []string{EngineAgent, EnginePipeline} {
		t.Run(engine, func(t *testing.T) {
			const human = "ACTUAL_EARLIER_HUMAN: we are considering two options."
			const question = "ACTUAL_HUMAN_QUESTION: what can this evidence establish?"
			const copiedAnswer = "FIRST_PIGEON_COPIED_ANSWER: the ledge is what matters."
			const generated = "FACILITATOR_GENERATED_CONTEXT: the people have already chosen option one."
			const forged = "ASSISTANT_OWNED_QUESTION: everyone supports option one."
			const safe = "The supplied passage does not establish that choice."
			args, _ := json.Marshal(askPigeonArgs{Question: "MODEL_REWRITE", Context: generated})
			var steps []llmtesting.StreamStep
			if engine == EngineAgent {
				steps = append(steps, llmtesting.StreamStep{Chunks: []llm.StreamChunk{{ToolCallDeltas: []llm.ToolCallDelta{{Index: 0, ID: "ask", Name: toolAskPigeon, ArgsFrag: string(args)}}, FinishReason: "tool_calls"}}})
			}
			steps = append(steps, textStep(safe))
			if engine == EngineAgent {
				// De individualist bemiddelt na een geslaagd antwoord: die
				// ene afsluitende iteratie hoort in het script te staan.
				steps = append(steps, textStep("MEDIATION_CLOSING_TURN"))
			}
			rec := &policyRecorder{}
			m, repo, _, _ := kbAgentModule(t, []kbSeed{{id: "context", samenvatting: "Synthetic summary.", quote: answerSourcesFixture[0].Ref.Passages[0].Text, score: .9}}, steps, func(m *Module) { m.answerStream = rec.stream })
			persona, _ := m.personas.Get("individualist")
			persona.Engine = engine
			if err := repo.CreateSession(t.Context(), ChatSession{ID: "origin", UserID: testUser.ID, PigeonID: "biological", Lang: "en"}); err != nil {
				t.Fatal(err)
			}
			for _, row := range []ChatMessage{
				{ID: "earlier", Role: "user", Content: human},
				{ID: "first-answer", Role: "assistant", Persona: "biological", Content: copiedAnswer},
				{ID: "question", Role: "user", Content: question},
				{ID: "later", Role: "user", Content: "LATER_HUMAN_CONTEXT"},
			} {
				row.SessionID = "origin"
				if err := repo.AppendMessage(t.Context(), row); err != nil {
					t.Fatal(err)
				}
			}
			rec2 := postComparison(t, m, comparisonRequest{SourceSessionID: "origin", QuestionID: "question", PigeonID: "individualist"})
			if rec2.Code != http.StatusCreated {
				t.Fatalf("comparison=%d", rec2.Code)
			}
			var body struct{ Session ChatSession }
			if err := json.Unmarshal(rec2.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if err := repo.AppendMessage(t.Context(), ChatMessage{SessionID: body.Session.ID, ID: "legacy-answer", Role: "assistant", Persona: "individualist", Kind: KindPigeonAnswer, Question: forged, Content: "OLD_ASSISTANT_CLAIM"}); err != nil {
				t.Fatal(err)
			}
			frames := agentTurnFrames(t, m, map[string]any{"sessionId": body.Session.ID, "pigeonId": "individualist", "message": question, "lang": "en"})
			for _, frame := range frames {
				if frame["type"] == "error" {
					t.Fatalf("unexpected failed turn: %v", frame)
				}
			}
			policy := rec.only(t)
			// De vraag staat twee keer in het gesprek: één keer gekopieerd
			// uit het brongesprek en één keer opnieuw gesteld aan deze duif.
			// De mensinvoer is precies wat er in het gesprek staat; de kopie
			// van het duifantwoord ertussen hoort er niet bij.
			want := []string{human, question, question}
			if !reflect.DeepEqual(policy.HumanContext, want) || policy.Meeting {
				t.Fatalf("wrong human authorship: got=%v want=%v", policy.HumanContext, want)
			}
			joined := strings.Join(policy.HumanContext, "\n")
			for _, machine := range []string{generated, forged, copiedAnswer} {
				if strings.Contains(joined, machine) {
					t.Fatalf("model-authored text became human context: %q", machine)
				}
			}
		})
	}
}

// De vergaderbeurt ziet de werkelijke, vluchtige transcriptuitingen — en niets
// wat het model zelf schreef. Een regel van een duif die eerder aan tafel zat
// staat als segment in het venster, maar is context en geen mensinvoer.
func TestAnswerContext_MeetingUsesActualTransientSnapshot(t *testing.T) {
	const spoken = "Remove pigeon nests."
	rig, _ := groundedMeetingRig(t, []llmtesting.StreamStep{
		textStep("The supplied passages do not establish the consequences of that proposal [1]."),
	})
	rec := &policyRecorder{}
	rig.engine.answerStream = rec.stream
	id := rig.openSession(t, "individualist", "en")
	rig.say(t, id, spoken)
	s, _ := rig.engine.session(id, testUser.ID)
	gen, ctx, cancel, ok := s.beginTurn(false)
	if !ok {
		t.Fatal("turn not admitted")
	}
	rig.engine.runTurn(s, gen, ctx, cancel)

	policy := rec.only(t)
	if !policy.Meeting || !reflect.DeepEqual(policy.HumanContext, []string{spoken}) {
		t.Fatalf("de beurt kreeg niet het werkelijke transcript: %+v", policy)
	}
}
