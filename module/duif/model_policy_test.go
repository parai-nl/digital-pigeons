package duif

import (
	"errors"
	"strings"
	"testing"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/litellm"
)

func TestModelPolicy_Endpoint(t *testing.T) {
	// The endpoint is deployment configuration: any plain HTTPS base URL is a
	// live endpoint, with or without an API base path.
	for _, raw := range []string{"https://llm.example.invalid", "https://llm.example.invalid/", "https://llm.example.invalid/v1", "https://llm.example.invalid:443/v1/", "https://llm.example.invalid:8443/api/v1", "https://[2001:db8::1]/v1"} {
		if err := validateModelEndpoint(raw, "PRIVATE_KEY"); err != nil {
			t.Fatalf("HTTPS endpoint rejected: %v", err)
		}
	}
	for _, raw := range []string{"", "llm.example.invalid", "http://llm.example.invalid", "ftp://llm.example.invalid", "https://", "https:///v1", "https://PRIVATE_KEY@llm.example.invalid", "https://user:PRIVATE_KEY@llm.example.invalid", "https://llm.example.invalid?token=PRIVATE_KEY", "https://llm.example.invalid?", "https://llm.example.invalid/#PRIVATE_KEY", "https://llm.example.invalid/v1/../admin", "https://llm.example.invalid//v1", "https://llm.example.invalid/v1%2Fadmin", "http://127.0.0.1:9999", "https:PRIVATE_KEY"} {
		err := validateModelEndpoint(raw, "PRIVATE_KEY")
		if err == nil || strings.Contains(err.Error(), "PRIVATE_KEY") {
			t.Fatalf("endpoint %q must be rejected without leaking input", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:1", "http://[::1]:1"} {
		if err := validateModelEndpoint(raw, "synthetic-loopback-only"); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"http://localhost:1", "http://192.0.2.1:1", "https://llm.example.invalid", "https://127.0.0.1:1", "http://127.0.0.1", "http://127.0.0.1:1/v1", "http://127.0.0.1:1?secret=yes"} {
		if validateModelEndpoint(raw, "synthetic-loopback-only") == nil {
			t.Fatalf("offline sentinel must not allow %q", raw)
		}
	}
	for _, key := range []string{"", "  "} {
		if validateModelEndpoint("https://llm.example.invalid", key) == nil {
			t.Fatal("an empty key must never reach the SDK, which would inherit an ambient key")
		}
	}
}

func TestModelPolicy_RuntimeEndpointIsTheConfiguredOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"https://llm.example.invalid":         "https://llm.example.invalid",
		"https://llm.example.invalid:443/v1/": "https://llm.example.invalid",
		"https://llm.example.invalid:8443/v1": "https://llm.example.invalid:8443",
		"https://[2001:db8::1]:8443/api/v1":   "https://[2001:db8::1]:8443",
	} {
		cfg := &parai.Config{}
		cfg.LLM.BaseURL, cfg.LLM.APIKey = raw, "PRIVATE_KEY"
		provider := litellm.New(litellm.Config{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey})
		m := New()
		if err := m.wirePrivateProviders(&parai.Core{Config: cfg, LLM: provider}); err != nil {
			t.Fatal(err)
		}
		if got := m.languageModelPolicy().Endpoint; got != want {
			t.Fatalf("runtime endpoint for %q = %q, want %q", raw, got, want)
		}
	}
}

func TestModelPolicy_ProviderWiringRejectsUnapprovedEndpoint(t *testing.T) {
	cfg := &parai.Config{}
	cfg.LLM.BaseURL, cfg.LLM.APIKey = "http://unapproved.invalid", "PRIVATE_KEY"
	provider := litellm.New(litellm.Config{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey})
	m := New()
	if err := m.wirePrivateProviders(&parai.Core{Config: cfg, LLM: provider}); err == nil || strings.Contains(err.Error(), "PRIVATE_KEY") {
		t.Fatal("shipped provider must validate its endpoint before wiring")
	}
}

func TestModelPolicy_RequestCannotSelectAnotherModel(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		fake := &limitProvider{stream: make(chan llm.StreamChunk)}
		close(fake.stream)
		guard := newBoundedLLM(fake)
		guard.requiredModel = defaultChatModel
		for _, model := range []string{"", "other-model", defaultChatModel + " "} {
			req := llm.CompletionRequest{Model: model}
			var err error
			if streaming {
				_, err = guard.CompleteStream(t.Context(), req)
			} else {
				_, err = guard.Complete(t.Context(), req)
			}
			if !errors.Is(err, errModelPolicy) || fake.calls != 0 {
				t.Fatal("disallowed model reached provider")
			}
		}
		req := llm.CompletionRequest{Model: defaultChatModel, MaxTokens: 50}
		if streaming {
			stream, err := guard.CompleteStream(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
		} else if _, err := guard.Complete(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		if fake.calls != 1 || fake.request.Model != defaultChatModel {
			t.Fatal("approved request did not retain exact model")
		}
	}
}

func TestModelPolicy_RuntimeMetadataUsesActualValues(t *testing.T) {
	m := New()
	m.model, m.detectModel, m.modelEndpoint = "old-chat", "old-detect", "synthetic-loopback"
	got := m.languageModelPolicy()
	if got.Model != "old-chat" || got.DetectionModel != "old-detect" || got.Endpoint != "synthetic-loopback" {
		t.Fatal("metadata must describe the actual wired values, not claim the new policy")
	}
}
