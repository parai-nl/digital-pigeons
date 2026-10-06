package duif

import (
	"context"
	"testing"

	"github.com/parai-nl/parai-core/llm/llmtesting"
	"github.com/parai-nl/parai-core/pkg/agent"
)

func TestFacilitatorCancellationBufferedDoneCannotPublishCanceledProfile(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		name := "live_control"
		if stopped {
			name = "already_canceled"
		}
		t.Run(name, func(t *testing.T) {
			m, repo, mock := newAgentModule(t, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pt, err := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "Who are you?"})
			if err != nil {
				t.Fatal("synthetic preparation failed")
			}
			at := m.newAgentTurn(pt)
			at.profileText = "Synthetic configured profile."
			publications := 0
			r := &agentRun{m: m, at: at, pt: pt, cancel: cancel, emit: func(any) error { publications++; return nil }}
			// Deterministic version of the reported race: the producer's local
			// terminal event is already buffered when the consumer observes Stop.
			events := make(chan agent.Event, 1)
			events <- agent.Done{Reason: agent.StopReasonNatural}
			close(events)
			if stopped {
				cancel()
			}
			r.pump(ctx, events, make(chan any))
			r.persist(ctx)
			if mock.CallCount() != 0 {
				t.Fatal("local completion unexpectedly called a provider")
			}
			if stopped {
				if publications != 0 || !r.aborted {
					t.Fatal("queued Done published a profile after cancellation")
				}
				assertNoFacilitatorRow(t, repo)
			} else if publications != 1 || r.aborted || facilitatorRow(t, repo).Partial {
				t.Fatal("positive control failed to publish the same ready profile")
			}
		})
	}
}

func TestFacilitatorCancellationInsideTextWriterPreventsFollowingDone(t *testing.T) {
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{toolCallStep("profile", toolPigeonProfile, `{}`)})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pt, err := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "Who are you?"})
	if err != nil {
		t.Fatal("synthetic preparation failed")
	}
	texts, terminals := 0, 0
	var acceptedText string
	m.runAgentTurn(ctx, pt, func(frame any) error {
		switch f := frame.(type) {
		case textFrame:
			texts++
			acceptedText = f.Delta
			cancel()
		case doneFrame:
			terminals++
		}
		return nil
	})
	if texts != 1 || terminals != 0 || mock.CallCount() != 1 {
		t.Fatal("cancellation at the text publication boundary leaked completion or another call")
	}
	row := facilitatorRow(t, repo)
	if !row.Partial || row.Content != acceptedText {
		t.Fatal("only the actually attempted text may be retained as partial")
	}
}
