package soniox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/parai-nl/ai-maps-duiven/module/duif/ttsresult"
	"github.com/parai-nl/parai-core/audio"
)

// De TTS-kant van Soniox, achter audio.Synthesizer.
//
// Waarom Soniox ook de TTS doet: Soniox draait niet alleen de
// spraakherkenning maar ook de synthese in de EU-regio, op dezelfde sleutel en
// dezelfde regio-endpoint. Zo verwerkt één leverancier de spraak in beide
// richtingen.
//
// De Voice-seam ontvangt tekst, audio, Done en Close plus een terminale
// Result-melding. Alleen een bevestigde providerafsluiting levert nil op;
// een gesloten socket of audiokanaal alleen bewijst geen volledige synthese.

const (
	// defaultTTSHost is opnieuw de EU-regio, om dezelfde reden als bij STT.
	defaultTTSHost = "tts-rt.eu.soniox.com"
	ttsPath        = "/tts-websocket"

	defaultTTSModel = "tts-rt-v2"

	// ttsAudioFormat en ttsSampleRate: rauwe PCM16, zodat de vergaderclient
	// blok voor blok kan afspelen terwijl de rest nog gesynthetiseerd wordt.
	// MP3 was compacter (een derde van de bytes) maar losse MP3-chunks zijn
	// geen bestand: de client moest de hele beurt afwachten vóór hij kon
	// decoderen. Gemeten 2026-09-07: eerste PCM-frame 0,37 s na de tekst.
	ttsAudioFormat = "pcm_s16le"
	ttsSampleRate  = OutputSampleRate
)

// DefaultTTSVoice is het vangnet, niet de keuze. De gekozen stem is Logan
// en die komt uit SONIOX_TTS_VOICE, zodat het
// stembesluit in configuratie staat en daar te wijzigen is. Draait een
// installatie zonder die variabele, dan spreekt de duif met deze ingebouwde
// stem — hoorbaar iets anders dan wat er is gekozen. Het opstartlog
// waarschuwt daarvoor (module/duif/providers.go).
const DefaultTTSVoice = "Adrian"

// OutputSampleRate is de bemonsteringsfrequentie van de PCM16-frames die de
// synthesizer levert (mono, little-endian). De vergadermodus geeft dit door
// aan de client op elk audioframe.
const OutputSampleRate = 24000

// Het spreektempo van de Soniox-TTS: `speed` in het openingsbericht, 1,0 is
// het normale tempo en het bereik is 0,7–1,3. Een waarde daarbuiten weigert de
// dienst met invalid_request, en dat zou de hele beurt laten mislukken — dus
// knijpt de synthesizer een doorgegeven tempo af in plaats van het te sturen.
// Bron: Soniox-documentatie "Speech speed"
// (https://soniox.com/docs/tts/concepts/speech-speed), geraadpleegd
// 22 september 2026.
const (
	MinSpeed = 0.7
	MaxSpeed = 1.3
)

// ttsConfig is het openingsbericht van de TTS-socket.
type ttsConfig struct {
	APIKey      string  `json:"api_key"`
	Model       string  `json:"model"`
	Language    string  `json:"language,omitempty"`
	Voice       string  `json:"voice"`
	AudioFormat string  `json:"audio_format"`
	SampleRate  int     `json:"sample_rate,omitempty"`
	StreamID    string  `json:"stream_id"`
	Speed       float64 `json:"speed,omitempty"`
}

// ttsText is één stuk tekst; TextEnd sluit de uiting af.
type ttsText struct {
	StreamID string `json:"stream_id"`
	Text     string `json:"text"`
	TextEnd  bool   `json:"text_end"`
}

// ttsResponse is één serverbericht: base64-audio, of de afsluiting.
type ttsResponse struct {
	StreamID     string `json:"stream_id"`
	Audio        string `json:"audio"`
	AudioEnd     bool   `json:"audio_end"`
	Terminated   bool   `json:"terminated"`
	ErrorCode    int    `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// SynthConfig is de TTS-tegenhanger van Config.
type SynthConfig struct {
	// APIKey is dezelfde regio-gebonden projectsleutel als bij STT.
	APIKey string
	// Host is de regio-endpoint zonder schema; leeg is de EU.
	Host string
	// Model overschrijft het TTS-model.
	Model string
	// Voice is de Soniox-stemnaam. Hij overschrijft de stem-id die de
	// vergadermodus meegeeft, want dat is een ElevenLabs-id en die betekent
	// hier niets. Dat beide pratende duiven op Soniox met dezelfde stem praten
	// is een ontwerpkeuze, geen beperking: bewust één stem (Logan) voor
	// allebei, zonder stemmenmenu en zonder
	// stemmachinerie per duif. Wie dat wil wijzigen, wijzigt SONIOX_TTS_VOICE.
	Voice  string
	Logger *slog.Logger
}

type synthesizer struct {
	cfg  SynthConfig
	dial func(ctx context.Context, url string) (*websocket.Conn, error)
}

var _ audio.Synthesizer = (*synthesizer)(nil)

// NewSynthesizer bouwt de Soniox-TTS achter de core-interface.
func NewSynthesizer(cfg SynthConfig) (audio.Synthesizer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ErrNoAPIKey
	}
	return &synthesizer{cfg: cfg, dial: dialWebSocket}, nil
}

func (s *synthesizer) log() *slog.Logger {
	if s.cfg.Logger != nil {
		return s.cfg.Logger
	}
	return slog.Default()
}

func (s *synthesizer) url() string {
	host := s.cfg.Host
	if host == "" {
		host = defaultTTSHost
	}
	return "wss://" + host + ttsPath
}

// voice kiest de stem: de geconfigureerde Soniox-naam wint, anders het
// vangnet. De voiceID die de vergadermodus meegeeft is een ElevenLabs-id en
// wordt bewust genegeerd in plaats van doorgestuurd — een onbekende stemnaam
// is bij Soniox een harde fout, en dat zou elke beurt laten mislukken. Er is
// hier niets per duif te kiezen: één stem voor beide pratende duiven is het
// besluit, dus de meegegeven id heeft ook niets te betekenen.
func (s *synthesizer) voice() string {
	if s.cfg.Voice != "" {
		return s.cfg.Voice
	}
	return DefaultTTSVoice
}

// clampSpeed houdt het tempo binnen wat de dienst accepteert. 0 (of minder)
// betekent "niets meesturen": het veld valt weg en Soniox spreekt op 1,0.
func clampSpeed(speed float64) float64 {
	switch {
	case speed <= 0:
		return 0
	case speed < MinSpeed:
		return MinSpeed
	case speed > MaxSpeed:
		return MaxSpeed
	default:
		return speed
	}
}

func (s *synthesizer) model() string {
	if s.cfg.Model != "" {
		return s.cfg.Model
	}
	return defaultTTSModel
}

// streamID is het correlatie-id dat elk TTS-bericht draagt.
func streamID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "duif-stream"
	}
	return "duif-" + hex.EncodeToString(b[:])
}

// Synthesize opent één spreekbeurt op het normale tempo van de dienst.
// voiceID komt uit de personaconfig en wordt hier genegeerd (zie voice());
// language is "nl" of "en".
func (s *synthesizer) Synthesize(ctx context.Context, voiceID, language string) (*audio.SynthesizeSession, error) {
	return s.SynthesizeAtSpeed(ctx, voiceID, language, 0)
}

// SynthesizeAtSpeed is dezelfde spreekbeurt met een tempo erbij: de uitbreiding
// op audio.Synthesizer waarmee de vergadermodus per duif iets sneller of
// rustiger laat praten. De stem blijft er één voor beide pratende
// duiven; alleen het tempo verschilt. speed 0 laat het veld weg, zodat de
// dienst zijn eigen 1,0 gebruikt.
func (s *synthesizer) SynthesizeAtSpeed(ctx context.Context, _, language string, speed float64) (*audio.SynthesizeSession, error) {
	conn, err := s.dial(ctx, s.url())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", audio.ErrDialFailed, err)
	}
	conn.SetReadLimit(readLimit)

	sessionCtx, cancelCause := context.WithCancelCause(ctx)
	cancel := func() { cancelCause(context.Canceled) }
	id := streamID()
	cfg := ttsConfig{
		APIKey: s.cfg.APIKey, Model: s.model(), Language: language,
		Voice: s.voice(), AudioFormat: ttsAudioFormat, SampleRate: ttsSampleRate, StreamID: id,
		Speed: clampSpeed(speed),
	}
	if err := writeJSON(sessionCtx, conn, cfg); err != nil {
		cancel()
		_ = conn.Close(websocket.StatusInternalError, "config")
		return nil, fmt.Errorf("soniox: tts-configuratie versturen: %w", err)
	}

	textCh := make(chan string, 4)
	audioCh := make(chan []byte, eventQueue)
	doneCh := make(chan struct{})

	var once sync.Once
	closeFn := func() error {
		var err error
		once.Do(func() {
			cancel()
			err = conn.Close(websocket.StatusNormalClosure, "done")
		})
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := s.writeLoop(sessionCtx, conn, id, textCh); err != nil {
			cancelCause(err)
		}
	}()
	go func() {
		defer wg.Done()
		defer close(audioCh)
		err := s.readLoop(sessionCtx, conn, audioCh)
		if err == nil {
			cancelCause(errProviderComplete)
		} else {
			cancelCause(err)
		}
	}()
	result := make(chan error, 1)
	go func() {
		defer close(doneCh)
		wg.Wait()
		cause := context.Cause(sessionCtx)
		if cause == errProviderComplete {
			cause = nil
		}
		result <- cause
		close(result)
		_ = closeFn()
	}()
	sess := &audio.SynthesizeSession{Text: textCh, Audio: audioCh, Done: doneCh, Close: closeFn}
	ttsresult.Set(sess, result)
	return sess, nil
}

// writeLoop stuurt de zinnen door. Het sluiten van het tekstkanaal is het
// teken dat de uiting compleet is; dan gaat er één bericht met text_end.
func (s *synthesizer) writeLoop(ctx context.Context, conn *websocket.Conn, id string, textCh <-chan string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, open := <-textCh:
			if !open {
				return writeJSON(ctx, conn, ttsText{StreamID: id, TextEnd: true})
			}
			if err := writeJSON(ctx, conn, ttsText{StreamID: id, Text: chunk}); err != nil {
				return fmt.Errorf("soniox: tekst versturen: %w", err)
			}
		}
	}
}

var errProviderComplete = errors.New("soniox: provider confirmed completion")

// readLoop decodeert de audioframes en zet ze op het kanaal.
func (s *synthesizer) readLoop(ctx context.Context, conn *websocket.Conn, out chan<- []byte) error {
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isClosed(err) {
				return errors.New("soniox: stream closed without completion")
			}
			return fmt.Errorf("soniox: tts lezen: %w", err)
		}
		var resp ttsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return errors.New("soniox: invalid TTS response")
		}
		if resp.ErrorCode != 0 {
			// Never propagate provider prose; it may echo supplied text or credentials.
			return errors.New("soniox: TTS provider rejected request")
		}
		if resp.Audio != "" {
			frame, err := base64.StdEncoding.DecodeString(resp.Audio)
			if err != nil {
				return fmt.Errorf("soniox: audioframe decoderen: %w", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- frame:
			}
		}
		if resp.Terminated {
			return nil
		}
	}
}
