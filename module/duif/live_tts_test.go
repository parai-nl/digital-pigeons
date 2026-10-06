package duif

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio"
)

// TestLive_TTS is een handmatige diagnose tegen de echte ElevenLabs-TTS via de
// parai-core-synthesizer en de duif-adapter. Hij draait alleen met
// DUIF_LIVE_TTS=1 én een PARAI_AUDIO_ELEVENLABS_API_KEY in de omgeving; in de
// gewone suite slaat hij over. Hij logt aantallen en redenen, nooit sleutels.
func TestLive_TTS(t *testing.T) {
	if os.Getenv("DUIF_LIVE_TTS") != "1" {
		t.Skip("live TTS-diagnose alleen met DUIF_LIVE_TTS=1")
	}
	key := os.Getenv("PARAI_AUDIO_ELEVENLABS_API_KEY")
	voice := os.Getenv("DUIF_DEV_VOICE_ID")
	if key == "" || voice == "" {
		t.Skip("PARAI_AUDIO_ELEVENLABS_API_KEY en DUIF_DEV_VOICE_ID vereist")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	synth, err := audio.NewSynthesizer(audio.Config{ElevenLabsAPIKey: key}, audio.WithLogger(logger))
	if err != nil {
		t.Fatalf("NewSynthesizer: %v", err)
	}
	v := newSynthesizerVoice(synth, AudioFormat{Codec: CodecMP3, SampleRate: 44100}, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	frames, err := speakAll(ctx, v, "Coo. I have been listening, but I have not heard anything to respond to yet [1].", SpeechRequest{VoiceID: voice, Lang: "en"})
	if err != nil {
		t.Fatalf("speakAll: %v", err)
	}
	var n, bytes int
	for f := range frames {
		n++
		bytes += len(f)
	}
	t.Logf("frames=%d bytes=%d", n, bytes)
	if bytes == 0 {
		t.Fatal("geen audio ontvangen")
	}
}
