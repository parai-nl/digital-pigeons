package soniox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/parai-nl/parai-core/audio"
)

// De streaming-STT-kant van Soniox, achter audio.RealtimeTranscriber.
//
// Het contract van de brug in duif/meeting_stt.go is smal en dat is maar goed
// ook: Start levert vier kanalen, de brug pompt frames en leest events, en
// wanneer de socket wegvalt is dat gewoon het einde van deze sessie — de brug
// is haar eigen supervisor en start opnieuw. Dit pakket doet dus géén
// reconnect: het sterft netjes en snel.

const (
	// defaultSTTHost is de EU-regio. Bewust de default en niet de
	// wereldwijde stt-rt.soniox.com: de hele reden dat dit pakket bestaat is
	// dat vergaderaudio de EU niet verlaat. Een sleutel van een US-project
	// werkt hier niet — regio en sleutel horen bij elkaar.
	defaultSTTHost = "stt-rt.eu.soniox.com"
	sttPath        = "/transcribe-websocket"

	// defaultSTTModel is het realtime-model; stt-rt-v5 is het actieve model.
	defaultSTTModel = "stt-rt-v5"

	// De vergadermodus levert PCM16 mono op 16 kHz (meeting_handlers.go).
	audioFormatPCM16 = "pcm_s16le"
	sampleRate       = 16000
	numChannels      = 1

	// keepaliveInterval houdt de socket open terwijl er geen audio loopt.
	// Soniox sluit na twintig seconden zonder audio of keepalive; de helft
	// daarvan geeft ruimte voor één gemiste tik.
	keepaliveInterval = 10 * time.Second

	// readLimit begrenst één serverbericht. Tokens zijn klein; dit is een
	// vangrail tegen een provider die ineens iets enorms stuurt.
	readLimit = 1 << 20

	// audioQueue/eventQueue zijn doorstroomruimte, geen opslag: de brug erboven
	// laat liever een frame vallen dan dat er audio blijft staan, want
	// vergaderinhoud blijft vluchtig.
	audioQueue = 4
	eventQueue = 8
)

// Config is wat dit pakket nodig heeft om te kunnen praten. Alles behalve de
// sleutel heeft een zinnige default.
type Config struct {
	// APIKey is de regio-gebonden projectsleutel.
	APIKey string
	// Host is de regio-endpoint zonder schema, bv. "stt-rt.eu.soniox.com".
	// Leeg is de EU.
	Host string
	// Model overschrijft het realtime-model.
	Model string
	// Diarization zet sprekerherkenning aan (de vergadermodus toont labels).
	Diarization bool
	// Logger is optioneel; nil is slog.Default().
	Logger *slog.Logger
}

// ErrNoAPIKey is de configuratiefout: zonder sleutel is er geen provider.
var ErrNoAPIKey = errors.New("soniox: API-sleutel ontbreekt")

type transcriber struct {
	cfg Config
	// dial is de netwerkseam. In tests wijst hij naar een nep-WebSocket-server;
	// in productie naar websocket.Dial.
	dial func(ctx context.Context, url string) (*websocket.Conn, error)
	// keepalive is de tikseam; 0 betekent keepaliveInterval. Een test kan hem
	// niet tien seconden lang afwachten.
	keepalive time.Duration
}

var _ audio.RealtimeTranscriber = (*transcriber)(nil)

// NewRealtimeTranscriber bouwt de Soniox-STT achter de core-interface.
func NewRealtimeTranscriber(cfg Config) (audio.RealtimeTranscriber, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ErrNoAPIKey
	}
	return &transcriber{cfg: cfg, dial: dialWebSocket}, nil
}

func dialWebSocket(ctx context.Context, url string) (*websocket.Conn, error) {
	conn, resp, err := websocket.Dial(ctx, url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn, err
}

func (t *transcriber) log() *slog.Logger {
	if t.cfg.Logger != nil {
		return t.cfg.Logger
	}
	return slog.Default()
}

func (t *transcriber) url() string {
	host := t.cfg.Host
	if host == "" {
		host = defaultSTTHost
	}
	// De sleutel rijdt mee in het eerste bericht, niet in de URL: zo kan hij
	// niet in een proxylog of een foutmelding belanden.
	return "wss://" + host + sttPath
}

func (t *transcriber) model() string {
	if t.cfg.Model != "" {
		return t.cfg.Model
	}
	return defaultSTTModel
}

// Start opent één transcriptiesessie. De sessie sterft bij ctx-annulering, bij
// een providerfout en bij het sluiten van Audio; de brug erboven merkt dat aan
// Done en begint opnieuw.
func (t *transcriber) Start(ctx context.Context, language string) (*audio.RealtimeSTTSession, error) {
	conn, err := t.dial(ctx, t.url())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", audio.ErrDialFailed, err)
	}
	conn.SetReadLimit(readLimit)

	sessionCtx, cancel := context.WithCancel(ctx)
	if err := writeJSON(sessionCtx, conn, t.config(language)); err != nil {
		cancel()
		_ = conn.Close(websocket.StatusInternalError, "config")
		return nil, fmt.Errorf("soniox: configuratie versturen: %w", err)
	}

	audioCh := make(chan []byte, audioQueue)
	commitCh := make(chan struct{}, 1)
	eventsCh := make(chan audio.TranscriptionEvent, eventQueue)
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
		if err := t.writeLoop(sessionCtx, conn, audioCh, commitCh); err != nil && sessionCtx.Err() == nil {
			t.log().Warn("sonioxsessie: schrijfkant viel weg", "component", "soniox", "error", err)
		}
	}()
	go func() {
		defer wg.Done()
		defer close(eventsCh)
		if err := t.readLoop(sessionCtx, conn, eventsCh); err != nil && sessionCtx.Err() == nil {
			t.log().Warn("sonioxsessie: leeskant viel weg", "component", "soniox", "error", err)
		}
		// De leeskant is de bron van waarheid: is die klaar, dan is de sessie
		// klaar en moet de schrijfkant meteen mee ophouden.
		cancel()
	}()
	go func() {
		defer close(doneCh)
		wg.Wait()
		_ = closeFn()
	}()

	return &audio.RealtimeSTTSession{
		Audio: audioCh, Commit: commitCh, Events: eventsCh, Done: doneCh, Close: closeFn,
	}, nil
}

// config bouwt het openingsbericht. language is de sessietaal van de
// vergadering ("nl" of "en"); hij gaat mee als hint, niet als dwang, zodat een
// tafel die halverwege omschakelt niet stilvalt.
func (t *transcriber) config(language string) sttConfig {
	cfg := sttConfig{
		APIKey:      t.cfg.APIKey,
		Model:       t.model(),
		AudioFormat: audioFormatPCM16,
		SampleRate:  sampleRate,
		NumChannels: numChannels,
		// Endpoint-detectie is wat de tokenstroom in uitingen knipt; zonder
		// dit levert Soniox nooit een <end> en vult het vergadervenster zich
		// niet. Het is de tegenhanger van commit_strategy=vad bij ElevenLabs.
		EnableEndpointDetection:      true,
		EnableLanguageIdentification: true,
		EnableSpeakerDiarization:     t.cfg.Diarization,
	}
	if language != "" {
		cfg.LanguageHints = []string{language}
	}
	return cfg
}

// writeLoop is de enige schrijver op de socket. Hij eindigt zodra Audio dicht
// gaat (dan meldt hij het einde van de stroom) of de sessie omvalt.
func (t *transcriber) writeLoop(ctx context.Context, conn *websocket.Conn, audioCh <-chan []byte, commitCh <-chan struct{}) error {
	every := t.keepalive
	if every <= 0 {
		every = keepaliveInterval
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if err := writeJSON(ctx, conn, sttControl{Type: controlKeepalive}); err != nil {
				return fmt.Errorf("soniox: keepalive: %w", err)
			}
		case <-commitCh:
			// De vergadermodus commit nooit; push-to-talk-gebruikers
			// van dezelfde interface wel. Soniox antwoordt met <fin>.
			if err := writeJSON(ctx, conn, sttControl{Type: controlFinalize}); err != nil {
				return fmt.Errorf("soniox: finalize: %w", err)
			}
		case frame, open := <-audioCh:
			if !open {
				// Het einde-van-audio-signaal is een leeg TEKSTframe. De
				// documentatie noemt "binair of tekst"; live blijkt een leeg
				// binair frame genegeerd te worden — de sessie loopt dan door
				// tot de inactiviteitstimer hem met 408 afkapt en het laatste
				// segment nooit gecommit wordt (gemeten 2026-09-04).
				return conn.Write(ctx, websocket.MessageText, []byte{})
			}
			// Rauwe PCM als binair frame — geen base64, geen JSON-omhulsel.
			if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
				return fmt.Errorf("soniox: audioframe: %w", err)
			}
			tick.Reset(every)
		}
	}
}

// readLoop vertaalt de tokenstroom naar transcriptie-events. Wat hier
// langskomt gaat het kanaal op en verder nergens heen.
func (t *transcriber) readLoop(ctx context.Context, conn *websocket.Conn, out chan<- audio.TranscriptionEvent) error {
	agg := newAggregator()
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			// Een gesloten socket is het normale einde van een sessie, geen
			// storing: de brug erboven start gewoon opnieuw.
			if ctx.Err() != nil || isClosed(err) {
				return nil
			}
			return fmt.Errorf("soniox: lezen: %w", err)
		}
		var resp sttResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			// Een onleesbaar bericht is geen reden om de sessie te slopen; wat
			// erin stond weten we niet en loggen we dus ook niet.
			t.log().Warn("sonioxsessie: onleesbaar serverbericht overgeslagen",
				"component", "soniox", "bytes", len(raw))
			continue
		}
		if resp.ErrorCode != 0 {
			// De foutmelding van de provider kan geen inhoud dragen (het is een
			// protocolfout), maar de code alleen is genoeg om op te sturen.
			return fmt.Errorf("soniox: providerfout %d", resp.ErrorCode)
		}
		for _, ev := range agg.consume(resp.Tokens) {
			if !emit(ctx, out, ev) {
				return nil
			}
		}
		if resp.Finished {
			// Wat er nog in de lopende zin stond gaat alsnog het venster in.
			if ev, ok := agg.close(); ok {
				emit(ctx, out, ev)
			}
			return nil
		}
	}
}

// emit zet één event op het kanaal; false betekent dat de sessie voorbij is.
func emit(ctx context.Context, out chan<- audio.TranscriptionEvent, ev transcriptEvent) bool {
	typ := "partial"
	if ev.Final {
		typ = "committed"
	}
	select {
	case <-ctx.Done():
		return false
	case out <- audio.TranscriptionEvent{Type: typ, Text: ev.Text, SpeakerID: ev.Speaker, IsFinal: ev.Final}:
		return true
	}
}

// isClosed herkent een nette of half-nette socketafsluiting.
func isClosed(err error) bool {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("soniox: JSON opbouwen: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
