package duif

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/parai-nl/parai-core/analytics"
	"github.com/parai-nl/parai-core/llm"
)

// The money ledger remains in the configured proxy. This process-local brake
// stops all model paths for five minutes after a budget rejection; it cannot
// bypass the proxy, estimate spend or promise a provider's billing period.
type boundedLLM struct {
	upstream      llm.Service
	requiredModel string // set by Init; empty only on injected unit-test adapters
	mu            sync.Mutex
	blockedUntil  time.Time
	now           func() time.Time
	timeout       time.Duration
	slots         chan struct{}
	// tracker krijgt per call de duurmetriek (llm_metrics.go). Dit is de enige
	// plek waar álle modelpaden langskomen, dus de enige plek waar "hoe lang
	// duurde dit" één keer geschreven hoeft te worden. Nil in unit-tests.
	tracker analytics.Tracker
}

func newBoundedLLM(upstream llm.Service) *boundedLLM {
	return &boundedLLM{upstream: upstream, now: time.Now, timeout: 2 * time.Minute, slots: make(chan struct{}, 32)}
}

func (b *boundedLLM) enter() (func(), error) {
	b.mu.Lock()
	blocked := b.now().Before(b.blockedUntil)
	b.mu.Unlock()
	if blocked {
		return nil, ErrBudgetExceeded
	}
	select {
	case b.slots <- struct{}{}:
		return func() { <-b.slots }, nil
	default:
		return nil, errors.New("provider capacity reached")
	}
}

// Never hand provider response bodies, prompts or credentials to module logs.
// Preserve only classifications used by the visible recovery protocol.
func (b *boundedLLM) safeError(err error) error {
	if err == nil {
		return nil
	}
	if classifyLLMError(err) == ReasonBudgetExceeded {
		b.mu.Lock()
		b.blockedUntil = b.now().Add(5 * time.Minute)
		b.mu.Unlock()
		return ErrBudgetExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if classifyDetectorFailure(err) == reasonDetectorConfiguration {
		return errors.New("provider configuration error: status 400")
	}
	return errors.New("provider unavailable")
}

func boundTokens(req llm.CompletionRequest) llm.CompletionRequest {
	if req.MaxTokens <= 0 || req.MaxTokens > 8192 {
		req.MaxTokens = 8192
	}
	return req
}

func (b *boundedLLM) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	if b.requiredModel != "" && req.Model != b.requiredModel {
		return llm.CompletionResponse{}, errModelPolicy
	}
	release, err := b.enter()
	if err != nil {
		return llm.CompletionResponse{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	started := time.Now()
	result, err := b.upstream.Complete(ctx, boundTokens(req))
	b.trackLLMDuration(ctx, req.Model, started)
	return result, b.safeError(err)
}

func (b *boundedLLM) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if b.requiredModel != "" && req.Model != b.requiredModel {
		return nil, errModelPolicy
	}
	release, err := b.enter()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	started := time.Now()
	in, err := b.upstream.CompleteStream(ctx, boundTokens(req))
	if err != nil {
		b.trackLLMDuration(ctx, req.Model, started)
		cancel()
		release()
		return nil, b.safeError(err)
	}
	// One pending chunk lets a terminal error survive even when the reader has
	// momentarily stopped receiving. A timeout must never look like a normal
	// channel close: callers would otherwise save the partial answer as done.
	out := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(out)
		defer cancel()
		defer release()
		// De duur van een stream is de duur tot hij eindigt — normaal, met een
		// fout, of doordat de lezer wegviel. Alle drie tellen mee: juist het
		// verschil tussen die gevallen is wat een trage proxy zichtbaar maakt.
		defer b.trackLLMDuration(ctx, req.Model, started)
		fail := func(err error) {
			if err == nil {
				return
			}
			terminal := llm.StreamChunk{Err: b.safeError(err)}
			select {
			case out <- terminal:
			default:
				// Prefer an explicit failure over one not-yet-consumed text
				// chunk. There is only one producer; this cannot block.
				select {
				case <-out:
				default:
				}
				out <- terminal
			}
		}
		for {
			select {
			case <-ctx.Done():
				fail(ctx.Err())
				return
			case chunk, ok := <-in:
				if !ok {
					fail(ctx.Err())
					return
				}
				chunk.Err = b.safeError(chunk.Err)
				select {
				case out <- chunk:
				case <-ctx.Done():
					fail(ctx.Err())
					return
				}
				if chunk.Err != nil {
					return
				}
			}
		}
	}()
	return out, nil
}
