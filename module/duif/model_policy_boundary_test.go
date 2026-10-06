package duif

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/litellm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// The shipping SDK uses only the explicit configuration. Only invented inputs
// and an owned numeric loopback HTTP fixture are used. No live model, database
// or global transport.
func TestModelPolicyBoundary_ShippingSDKUsesExplicitConfiguration(t *testing.T) {
	t.Setenv("DUIF_CHAT_MODEL", "")
	t.Setenv("DUIF_DETECT_MODEL", "")
	t.Setenv("OPENAI_BASE_URL", "https://unrelated.invalid")
	t.Setenv("OPENAI_API_KEY", "SYNTHETIC_AMBIENT_KEY")
	t.Setenv("OPENAI_ORG_ID", "")
	t.Setenv("OPENAI_PROJECT_ID", "")
	t.Setenv("OPENAI_WEBHOOK_SECRET", "")
	type captured struct {
		Model, Path, Authorization string
		Streaming                  bool
	}
	var mu sync.Mutex
	var calls []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid synthetic fixture", http.StatusBadRequest)
			return
		}
		mu.Lock()
		calls = append(calls, captured{request.Model, r.URL.Path, r.Header.Get("Authorization"), request.Stream})
		mu.Unlock()
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+`{"id":"synthetic","object":"chat.completion.chunk","created":0,"model":"vertex/gemini-3.6-flash-eu","choices":[{"index":0,"delta":{"content":"Synthetic reply."},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"synthetic","object":"chat.completion","created":0,"model":"vertex/gemini-3.6-flash-eu","choices":[{"index":0,"message":{"role":"assistant","content":"Synthetic reply."},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	cfg := &parai.Config{}
	cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.DefaultModel = srv.URL, "synthetic-loopback-only", "SYNTHETIC_IGNORED_CORE_DEFAULT"
	provider := litellm.New(litellm.Config{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey})
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Config: cfg, LLM: provider}); err != nil {
		t.Fatal("synthetic provider did not initialize")
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
	if m.meetings.llm != m.llm || m.model != defaultChatModel || m.meetings.model != defaultChatModel || m.meetings.detectModel != defaultChatModel {
		t.Fatal("chat and meeting do not share the initialized model/service")
	}
	for _, service := range []llm.Service{m.llm, m.meetings.llm} {
		for _, streaming := range []bool{false, true} {
			if streaming {
				if _, err := service.CompleteStream(t.Context(), llm.CompletionRequest{Model: "SYNTHETIC_OTHER_MODEL"}); !errors.Is(err, errModelPolicy) {
					t.Fatal("internal streaming model drift reached the provider")
				}
			} else if _, err := service.Complete(t.Context(), llm.CompletionRequest{Model: "SYNTHETIC_OTHER_MODEL"}); !errors.Is(err, errModelPolicy) {
				t.Fatal("internal classifier model drift reached the provider")
			}
		}
	}
	req := llm.CompletionRequest{Model: defaultChatModel, MaxTokens: 20, Messages: []llm.Message{{Role: "user", Content: "SYNTHETIC_REQUEST"}}}
	if _, err := m.llm.Complete(t.Context(), req); err != nil {
		t.Fatal("approved non-streaming fixture failed")
	}
	stream, err := m.meetings.llm.CompleteStream(t.Context(), req)
	if err != nil {
		t.Fatal("approved streaming fixture failed")
	}
	if got, err := collectAnswer(t.Context(), stream, 1024); err != nil || got != "Synthetic reply." {
		t.Fatal("synthetic stream did not complete")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatal("a refused request or provider retry reached the HTTP fixture")
	}
	for _, call := range calls {
		if call.Model != "vertex/gemini-3.6-flash-eu" || call.Authorization != "Bearer synthetic-loopback-only" || !strings.HasSuffix(call.Path, "/chat/completions") {
			t.Fatal("SDK changed explicit model, credential or completion path")
		}
	}
	if calls[0].Streaming || !calls[1].Streaming || m.languageModelPolicy().Endpoint != "synthetic-loopback" {
		t.Fatal("wire mode or endpoint metadata mismatches fixture")
	}
}

// De enige betaalde call van een antwoord — de duif zelf — gaat naar het ene
// goedgekeurde model, in chat en vergadering, in beide talen en voor beide
// sprekende duiven. Er volgt geen tweede call meer achteraan.
func TestModelPolicyBoundary_AnswerKeepsGuardedModel(t *testing.T) {
	const answer = "In the northern enclosure, the authors proposed a possible pattern [1]."
	for _, persona := range []string{"biological", "individualist"} {
		for _, lang := range []string{"nl", "en"} {
			for _, meeting := range []bool{false, true} {
				t.Run(persona+"/"+lang+map[bool]string{false: "/chat", true: "/meeting"}[meeting], func(t *testing.T) {
					t.Setenv("DUIF_CHAT_MODEL", "")
					t.Setenv("DUIF_DETECT_MODEL", "")
					mock := llmtesting.NewMockService()
					mock.SetStreamScript([]llmtesting.StreamStep{textStep(answer)})
					m := New()
					if err := m.Init(&parai.Core{DB: idlePool(t), LLM: mock}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
					policy := answerPolicy{Persona: mustGet(t, testPersonas(t), persona), Lang: lang, Meeting: meeting, HumanContext: []string{"Synthetic human question."}}
					service, model := m.llm, m.model
					if meeting {
						service, model = m.meetings.llm, m.meetings.model
					}
					stream, err := openAnswerStream(t.Context(), service, llm.CompletionRequest{Model: model, Stream: true}, answerSourcesFixture, policy, nil)
					if err != nil {
						t.Fatal(err)
					}
					if got, err := collectAnswer(t.Context(), stream, 4*1024); err != nil || got != answer {
						t.Fatalf("answer stream changed: %q %v", got, err)
					}
					if mock.CallCount() != 1 {
						t.Fatalf("een antwoord is één call, geen %d", mock.CallCount())
					}
					for _, call := range mock.Calls {
						if call.Model != defaultChatModel {
							t.Fatal("one actual call changed model")
						}
					}
					if got := m.chatProvenance(t.Context(), persona); got == nil || got.Model != defaultChatModel {
						t.Fatal("new answer provenance does not retain actual selected model")
					}
				})
			}
		}
	}
}

func TestModelPolicyBoundary_RemoteSentinelCannotClaimLoopback(t *testing.T) {
	cfg := &parai.Config{}
	cfg.LLM.BaseURL, cfg.LLM.APIKey = "https://llm.example.invalid", "synthetic-loopback-only"
	m := New()
	provider := litellm.New(litellm.Config{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey})
	if err := m.wirePrivateProviders(&parai.Core{Config: cfg, LLM: provider}); err != nil {
		return // refusing an offline-only sentinel on a remote endpoint is safe
	}
	// No HTTP call: inspect only metadata from the actual wired config.
	if m.languageModelPolicy().Endpoint == "synthetic-loopback" {
		t.Fatal("remote configured provider is falsely labelled as loopback")
	}
}

func TestModelPolicyBoundary_EmptyConfiguredKeyCannotInheritAmbientKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "SYNTHETIC_UNRELATED_CREDENTIAL")
	cfg := &parai.Config{}
	cfg.LLM.BaseURL = "https://llm.example.invalid"
	m := New()
	provider := litellm.New(litellm.Config{BaseURL: cfg.LLM.BaseURL})
	// The pinned SDK reads OPENAI_API_KEY when Config.APIKey is empty. A
	// fail-closed configuration check is required before constructing it here.
	// This negative test deliberately performs no request to the remote URL.
	if err := m.wirePrivateProviders(&parai.Core{Config: cfg, LLM: provider}); err == nil {
		t.Fatal("missing Parai key silently leaves the SDK ambient credential fallback active")
	}
}
