package duif

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

var errModelPolicy = errors.New("duif: language-model requests require vertex/gemini-3.6-flash-eu")

func validateModelOverrides() error {
	for _, key := range []string{"DUIF_CHAT_MODEL", "DUIF_DETECT_MODEL"} {
		if value := os.Getenv(key); value != "" && value != defaultChatModel {
			return fmt.Errorf("duif: %s must be vertex/gemini-3.6-flash-eu", key)
		}
	}
	return nil
}

// Live inference goes to the OpenAI-compatible proxy that the deployment
// configures (PARAI_LLM_BASE_URL); the module carries no default host. A live
// endpoint must be plain HTTPS without credentials, query or fragment in the
// URL. Never print the supplied URL: it may contain credentials. Offline
// shipping tests use an explicit, non-secret sentinel key and a numeric
// loopback HTTP fixture; real credentials cannot be sent to that exception,
// and the sentinel cannot be sent to a remote host.
func validateModelEndpoint(raw, apiKey string) error {
	// The pinned SDK otherwise falls back to ambient OPENAI_API_KEY when
	// Parai does not supply an explicit key. Never inherit that credential.
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("duif: explicit Parai language-model API key is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return errors.New("duif: invalid language-model endpoint configuration")
	}
	if apiKey == syntheticLoopbackKey {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme == "http" && ip != nil && ip.IsLoopback() && u.Port() != "" && u.Path == "" {
			return nil
		}
		return errors.New("duif: the offline sentinel key only reaches a numeric loopback HTTP endpoint")
	}
	if u.Scheme == "https" && u.Hostname() != "" && cleanEndpointPath(u.Path) {
		return nil
	}
	return errors.New("duif: live language-model endpoint must be an HTTPS URL (PARAI_LLM_BASE_URL)")
}

// syntheticLoopbackKey is the non-secret key of the offline test fixtures.
const syntheticLoopbackKey = "synthetic-loopback-only"

// cleanEndpointPath admits an API base path such as "/v1" but no dot segments
// or empty segments that could make the effective request path ambiguous.
func cleanEndpointPath(p string) bool {
	if p == "" || p == "/" {
		return true
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for _, segment := range strings.Split(strings.TrimSuffix(p[1:], "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// endpointOrigin is the validated endpoint as runtime metadata: scheme, host
// and a non-default port, without path, credentials or query.
func endpointOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

// Read-only runtime identity lets live evaluators refuse an older server
// before any generation request. It contains no key, raw URL or prompt.
type languageModelPolicy struct {
	Model          string `json:"model"`
	DetectionModel string `json:"detectionModel"`
	Endpoint       string `json:"endpoint"`
}

func (m *Module) languageModelPolicy() languageModelPolicy {
	return languageModelPolicy{Model: m.model, DetectionModel: m.detectModel, Endpoint: m.modelEndpoint}
}
