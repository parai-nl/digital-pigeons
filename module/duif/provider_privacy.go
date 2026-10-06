package duif

import (
	"context"
	"log/slog"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/audio"
	"github.com/parai-nl/parai-core/llm/litellm"
)

// Private provider implementations log raw transcripts/errors before returning
// through our seams. Their existing logger options are the supported boundary.
// Discard even the message and pre-bound attributes: upstream messages are not
// a stable, audited vocabulary. Token metrics still use core.Analytics.
type providerLogHandler struct{ sink slog.Handler }

func (h providerLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.sink.Enabled(ctx, level)
}
func (h providerLogHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, "provider event", 0)
	clean.AddAttrs(slog.String("component", "duif-provider"))
	return h.sink.Handle(ctx, clean)
}
func (h providerLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h providerLogHandler) WithGroup(string) slog.Handler      { return h }
func privateProviderLogger(logger *slog.Logger) *slog.Logger {
	return slog.New(providerLogHandler{sink: logOr(logger).Handler()})
}

func (m *Module) wirePrivateProviders(core *parai.Core) error {
	if core.Config == nil {
		return nil
	} // injected test adapters remain injected
	logger := privateProviderLogger(core.Logger)
	// Only replace the known shipped provider, never a custom injected service.
	if _, ok := core.LLM.(*litellm.Provider); ok {
		cfg := core.Config
		if err := validateModelEndpoint(cfg.LLM.BaseURL, cfg.LLM.APIKey); err != nil {
			return err
		}
		m.modelEndpoint = endpointOrigin(cfg.LLM.BaseURL)
		if cfg.LLM.APIKey == syntheticLoopbackKey {
			m.modelEndpoint = "synthetic-loopback"
		}
		provider := litellm.New(litellm.Config{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, EmbeddingModel: cfg.Knowledge.EmbeddingModel, TranscriptionModel: cfg.LLM.TranscriptionModel, ImageModel: cfg.LLM.ImageModel}, litellm.WithLogger(logger), litellm.WithAnalytics(core.Analytics))
		m.llm = provider
		if _, ok := core.Embedder.(*litellm.Provider); ok {
			m.embedder = provider
		}
	}
	cfg := core.Config.Audio
	if cfg.ElevenLabsAPIKey != "" {
		config := audio.Config{ElevenLabsAPIKey: cfg.ElevenLabsAPIKey, STTModel: cfg.STTModel, TTSModel: cfg.TTSModel, TTSDefaultVoice: cfg.TTSDefaultVoice, BaseURL: cfg.BaseURL}
		var err error
		m.transcriber, err = audio.NewRealtimeTranscriber(config, audio.WithLogger(logger))
		if err != nil {
			return err
		}
		m.synthesizer, err = audio.NewSynthesizer(config, audio.WithLogger(logger))
		if err != nil {
			return err
		}
	}
	return nil
}
