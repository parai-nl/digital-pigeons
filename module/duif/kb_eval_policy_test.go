//go:build live

package duif

import "testing"

// These tests exercise only configuration. They never create a provider,
// connect to a database, or execute the tagged live calibration entrypoints.
func kbEvalPolicyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DUIF_CHAT_MODEL", "DUIF_DETECT_MODEL", "PARAI_LLM_BASE_URL", "PARAI_LLM_API_KEY", "PARAI_KNOWLEDGE_EMBEDDING_MODEL", "PARAI_DB_URL"} {
		t.Setenv(name, "")
	}
}

func TestKBEvalPolicyRejectsModelOverrideBeforeMissingLiveConfiguration(t *testing.T) {
	for _, variable := range []string{"DUIF_CHAT_MODEL", "DUIF_DETECT_MODEL"} {
		t.Run(variable, func(t *testing.T) {
			kbEvalPolicyEnvironment(t)
			t.Setenv(variable, "synthetic-disallowed-language-model")
			if _, err := kbEvalProviderConfig(); err == nil {
				t.Fatal("standalone calibration accepted a disallowed language model without live configuration")
			}
		})
	}
}

func TestKBEvalPolicyRejectsOtherEndpointsWithoutCreatingProvider(t *testing.T) {
	for _, endpoint := range []string{"http://llm.example.invalid/v1", "https://user:secret@llm.example.invalid/v1", "https://llm.example.invalid/v1?key=secret", "http://127.0.0.1:1"} {
		t.Run(endpoint, func(t *testing.T) {
			kbEvalPolicyEnvironment(t)
			t.Setenv("PARAI_LLM_BASE_URL", endpoint)
			t.Setenv("PARAI_LLM_API_KEY", "synthetic-invalid-for-loopback")
			if _, err := kbEvalProviderConfig(); err == nil {
				t.Fatal("standalone calibration accepted a disallowed model endpoint")
			}
		})
	}
}

func TestKBEvalPolicyPreservesEmbeddingModelAtConfiguredProxy(t *testing.T) {
	for _, embedding := range []string{"", "synthetic-existing-vector-model"} {
		t.Run(embedding, func(t *testing.T) {
			kbEvalPolicyEnvironment(t)
			t.Setenv("DUIF_CHAT_MODEL", "vertex/gemini-3.6-flash-eu")
			t.Setenv("DUIF_DETECT_MODEL", "vertex/gemini-3.6-flash-eu")
			t.Setenv("PARAI_LLM_BASE_URL", "https://llm.example.invalid/v1")
			t.Setenv("PARAI_LLM_API_KEY", "synthetic-configuration-only")
			t.Setenv("PARAI_KNOWLEDGE_EMBEDDING_MODEL", embedding)
			cfg, err := kbEvalProviderConfig()
			if err != nil {
				t.Fatal(err)
			}
			want := embedding
			if want == "" {
				want = kbEvalDefaultEmbeddingModel
			}
			if cfg.EmbeddingModel != want || cfg.BaseURL != "https://llm.example.invalid/v1" || cfg.APIKey != "synthetic-configuration-only" {
				t.Fatal("configured proxy changed the existing vector model or credentials")
			}
		})
	}
}

func TestKBEvalPolicyMissingConfigurationRemainsUnconfigured(t *testing.T) {
	kbEvalPolicyEnvironment(t)
	cfg, err := kbEvalProviderConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "" || cfg.APIKey != "" || cfg.EmbeddingModel != kbEvalDefaultEmbeddingModel {
		t.Fatal("missing proxy configuration gained a hidden endpoint or changed the vector model")
	}
}
