package soniox

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio"
)

// De live tempo-meting. Net als de andere live-tests draait hij niet
// in de gewone suite: zonder DUIF_LIVE_SONIOX=1 slaat hij over en hij kost
// echte API-tijd.
//
//	DUIF_LIVE_SONIOX=1 SONIOX_API_KEY=… SONIOX_TTS_VOICE=Logan \
//	  SONIOX_TTS_OUT=/pad/naar/map go test -run TestLive_Soniox_TTSSpeed ./duif/soniox/
//
// Wat hij meet is het enige dat telt voor de tempokeuze: dezelfde zin, dezelfde
// stem, drie tempo's — hoeveel korter wordt de beurt werkelijk? De duur volgt
// uit de PCM16-bytes (mono, 24 kHz), dus dit is de lengte van de audio zelf en
// niet de tijd die de meting toevallig kostte. Met SONIOX_TTS_OUT gezet komen
// de WAV's in die map te staan, zodat ze te beluisteren zijn.
func TestLive_Soniox_TTSSpeed(t *testing.T) {
	key := liveGate(t)
	synth, err := NewSynthesizer(SynthConfig{APIKey: key, Voice: os.Getenv("SONIOX_TTS_VOICE")})
	if err != nil {
		t.Fatal(err)
	}
	speedy, ok := synth.(interface {
		SynthesizeAtSpeed(context.Context, string, string, float64) (*audio.SynthesizeSession, error)
	})
	if !ok {
		t.Fatal("de Soniox-synthesizer kent geen tempo meer")
	}
	dir := os.Getenv("SONIOX_TTS_OUT")

	// Eén zin per taal, voor alle tempo's dezelfde: alleen zo is het verschil
	// in duur het tempo en niet de tekst.
	zinnen := map[string]string{
		"nl": "Netten en pinnen kosten ons onze landingsplekken, en dat lost de overlast op het plein niet op.",
		"en": "Nets and spikes cost us our landing spots, and that does not solve the nuisance on the square.",
	}
	for _, lang := range []string{"nl", "en"} {
		for _, speed := range []float64{0.97, 1.0, 1.10} {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			sess, err := speedy.SynthesizeAtSpeed(ctx, "", lang, speed)
			if err != nil {
				cancel()
				t.Fatalf("%s @ %.2f: sessie openen: %v", lang, speed, err)
			}
			sess.Text <- zinnen[lang] + " "
			close(sess.Text)
			var pcm []byte
			for frame := range sess.Audio {
				pcm = append(pcm, frame...)
			}
			<-sess.Done
			_ = sess.Close()
			cancel()

			duur := time.Duration(len(pcm)/2) * time.Second / OutputSampleRate
			t.Logf("%s @ %.2f: %v (%d bytes)", lang, speed, duur.Round(10*time.Millisecond), len(pcm))
			if len(pcm) == 0 {
				t.Errorf("%s @ %.2f: geen audio", lang, speed)
				continue
			}
			if dir == "" {
				continue
			}
			naam := filepath.Join(dir, fmt.Sprintf("tempo-%s-%.2f.wav", lang, speed))
			if err := os.WriteFile(naam, wav(pcm, OutputSampleRate), 0o600); err != nil {
				t.Fatalf("%s schrijven: %v", naam, err)
			}
		}
	}
}

// wav plakt een RIFF-kop voor PCM16 mono, zodat een speler het bestand opent.
func wav(pcm []byte, rate int) []byte {
	out := make([]byte, 44, 44+len(pcm))
	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+len(pcm))) //nolint:gosec // testaudio, lengte past
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], uint32(rate))   //nolint:gosec // vaste 24 kHz
	binary.LittleEndian.PutUint32(out[28:], uint32(rate*2)) //nolint:gosec // vaste 24 kHz
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(pcm))) //nolint:gosec // testaudio, lengte past
	return append(out, pcm...)
}
