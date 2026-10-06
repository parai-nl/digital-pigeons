package soniox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/parai-nl/parai-core/audio"
)

// fakeSoniox is een nep-Soniox aan de andere kant van de socket. Geen enkele
// unittest in dit pakket raakt het netwerk: de dial-seam wijst hierheen.
type fakeSoniox struct {
	t *testing.T
	// handle krijgt de verbinding nadat de client zijn configuratie stuurde.
	handle func(t *testing.T, conn *websocket.Conn, cfg map[string]any)

	srv *httptest.Server

	mu      sync.Mutex
	frames  [][]byte
	control []string
	cfg     map[string]any
	closed  bool
}

func newFakeSoniox(t *testing.T, handle func(t *testing.T, conn *websocket.Conn, cfg map[string]any)) *fakeSoniox {
	t.Helper()
	f := &fakeSoniox{t: t, handle: handle}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow() //nolint:errcheck // de nepserver hoeft niets netjes te doen
		_, raw, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		cfg := map[string]any{}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return
		}
		f.mu.Lock()
		f.cfg = cfg
		f.mu.Unlock()
		if f.handle != nil {
			f.handle(t, conn, cfg)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// transcriber bouwt een transcriber die naar de nepserver dialt.
func (f *fakeSoniox) transcriber(cfg Config) *transcriber {
	if cfg.APIKey == "" {
		cfg.APIKey = "test-key"
	}
	return &transcriber{cfg: cfg, dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.srv.URL, "http"), nil)
		return conn, err
	}}
}

func (f *fakeSoniox) synthesizer(cfg SynthConfig) *synthesizer {
	if cfg.APIKey == "" {
		cfg.APIKey = "test-key"
	}
	return &synthesizer{cfg: cfg, dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.srv.URL, "http"), nil)
		return conn, err
	}}
}

func (f *fakeSoniox) config() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}

// readUpstream leest wat de client stuurt en sorteert het in audioframes en
// besturingsberichten, tot de socket dichtgaat.
func (f *fakeSoniox) readUpstream(conn *websocket.Conn) {
	for {
		typ, raw, err := conn.Read(context.Background())
		if err != nil {
			f.mu.Lock()
			f.closed = true
			f.mu.Unlock()
			return
		}
		f.mu.Lock()
		switch {
		case typ == websocket.MessageBinary:
			f.frames = append(f.frames, raw)
		case len(raw) == 0:
			// Het lege tekstframe is het einde-van-audio-signaal, geen
			// besturingsbericht.
			f.control = append(f.control, "end-of-stream")
		default:
			var c sttControl
			_ = json.Unmarshal(raw, &c)
			f.control = append(f.control, c.Type)
		}
		f.mu.Unlock()
	}
}

func (f *fakeSoniox) upstream() ([][]byte, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.frames...), append([]string(nil), f.control...)
}

// send stuurt één serverbericht.
func send(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, raw); err != nil {
		t.Logf("nepserver kon niet schrijven: %v", err)
	}
}

// collectEvents leest events tot het kanaal dicht is of de tijd op is.
func collectEvents(t *testing.T, sess *audio.RealtimeSTTSession, want int) []audio.TranscriptionEvent {
	t.Helper()
	var got []audio.TranscriptionEvent
	deadline := time.After(3 * time.Second)
	for len(got) < want {
		select {
		case ev, open := <-sess.Events:
			if !open {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("timeout na %d events, verwacht %d: %+v", len(got), want, got)
		}
	}
	return got
}

// ── De handshake ─────────────────────────────────────────────────────────────

// Het openingsbericht is het hele contract met Soniox. Het formaat moet exact
// matchen met wat de vergadermodus levert (PCM16 mono 16 kHz), en
// endpoint-detectie moet aan staan — zonder <end>-tokens vult het
// vergadervenster zich nooit.
func TestTranscriber_HandshakeDescribesTheMeetingsAudioAndAsksForEndpoints(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, sttResponse{Finished: true})
	})
	tr := f.transcriber(Config{Diarization: true})
	sess, err := tr.Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	<-sess.Done

	cfg := f.config()
	for veld, want := range map[string]any{
		"api_key":                        "test-key",
		"model":                          defaultSTTModel,
		"audio_format":                   audioFormatPCM16,
		"sample_rate":                    float64(sampleRate),
		"num_channels":                   float64(numChannels),
		"enable_endpoint_detection":      true,
		"enable_language_identification": true,
		"enable_speaker_diarization":     true,
	} {
		if cfg[veld] != want {
			t.Errorf("configuratie[%s] = %v, verwacht %v", veld, cfg[veld], want)
		}
	}
	hints, _ := cfg["language_hints"].([]any)
	if len(hints) != 1 || hints[0] != "nl" {
		t.Errorf("language_hints = %v, verwacht [nl]", cfg["language_hints"])
	}
}

// Zonder sessietaal geen hint: dan doet Soniox zijn eigen taalherkenning.
func TestTranscriber_WithoutASessionLanguageItSendsNoHint(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, sttResponse{Finished: true})
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	<-sess.Done
	if _, ok := f.config()["language_hints"]; ok {
		t.Errorf("er ging een taalhint mee terwijl de sessie er geen had: %v", f.config())
	}
}

// ── De stroom ────────────────────────────────────────────────────────────────

// De volle route: audio erin als binaire frames, partials en een gecommit
// segment eruit. Dit is de vertaling waar de hele brug op leunt.
func TestTranscriber_StreamsPCMUpAndSegmentsDown(t *testing.T) {
	var f *fakeSoniox
	f = newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		go func() {
			send(t, conn, sttResponse{Tokens: []sttToken{tok("Goedemiddag", false)}})
			send(t, conn, sttResponse{Tokens: []sttToken{tok("Goedemiddag", true), tok(" allemaal.", true)}})
			send(t, conn, sttResponse{Tokens: []sttToken{tok(markerEnd, true)}})
		}()
		// Eén lezer op de socket: readUpstream blokkeert tot de client sluit.
		// (conn.CloseRead zou een tweede lezer starten en de frames wegvangen.)
		f.readUpstream(conn)
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck

	sess.Audio <- []byte{1, 2, 3, 4}
	evs := collectEvents(t, sess, 3)
	want := []audio.TranscriptionEvent{
		{Type: "partial", Text: "Goedemiddag", IsFinal: false},
		{Type: "partial", Text: "Goedemiddag allemaal.", IsFinal: false},
		{Type: "committed", Text: "Goedemiddag allemaal.", IsFinal: true},
	}
	for i := range want {
		if evs[i] != want[i] {
			t.Errorf("event %d = %+v, verwacht %+v", i, evs[i], want[i])
		}
	}

	// En de audio ging er rauw en binair in — geen base64, geen JSON.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if frames, _ := f.upstream(); len(frames) == 1 && string(frames[0]) == "\x01\x02\x03\x04" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	frames, _ := f.upstream()
	t.Errorf("het audioframe kwam niet ongewijzigd aan: %v", frames)
}

// Het sluiten van het audiokanaal is het einde-van-stroom-signaal: Soniox wil
// daarvoor een leeg frame zien.
func TestTranscriber_ClosingAudioSendsTheEndOfStreamFrame(t *testing.T) {
	var f *fakeSoniox
	f = newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		f.readUpstream(conn)
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	close(sess.Audio)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ctl := f.upstream(); len(ctl) == 1 && ctl[0] == "end-of-stream" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	frames, ctl := f.upstream()
	t.Errorf("verwacht één leeg tekstframe als einde-van-stroom, kreeg frames=%v control=%v", frames, ctl)
}

// Commit is voor push-to-talk-gebruikers van dezelfde interface; de
// vergadermodus gebruikt hem niet. Soniox noemt het finalize.
func TestTranscriber_CommitSendsFinalize(t *testing.T) {
	var f *fakeSoniox
	f = newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		f.readUpstream(conn)
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	sess.Commit <- struct{}{}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ctl := f.upstream(); len(ctl) == 1 && ctl[0] == controlFinalize {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, ctl := f.upstream()
	t.Errorf("verwacht een finalize-bericht, kreeg %v", ctl)
}

// De keepalive houdt de socket open in de stiltes tussen twee sprekers: Soniox
// sluit na twintig seconden zonder audio én zonder keepalive.
func TestTranscriber_KeepaliveIsSentWhileTheRoomIsSilent(t *testing.T) {
	var f *fakeSoniox
	f = newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		f.readUpstream(conn)
	})
	tr := f.transcriber(Config{})
	tr.keepalive = 10 * time.Millisecond // de echte tien seconden heeft een test niet
	sess, err := tr.Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ctl := f.upstream(); len(ctl) >= 2 && ctl[0] == controlKeepalive && ctl[1] == controlKeepalive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, ctl := f.upstream()
	t.Errorf("verwacht herhaalde keepalives in de stilte, kreeg %v", ctl)
}

// Zodra er wél audio loopt is de keepalive overbodig: audio houdt de socket
// zelf open, en een keepalive erbovenop kost alleen berichten.
func TestTranscriber_AudioResetsTheKeepaliveTimer(t *testing.T) {
	var f *fakeSoniox
	f = newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		f.readUpstream(conn)
	})
	tr := f.transcriber(Config{})
	tr.keepalive = 200 * time.Millisecond
	sess, err := tr.Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	for range 10 {
		sess.Audio <- []byte{0, 0}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ctl := f.upstream(); len(ctl) != 0 {
		t.Errorf("er ging een keepalive mee terwijl er audio liep: %v", ctl)
	}
}

// De keepalive-interval moet ruim onder de providertimeout van twintig
// seconden blijven, met ruimte voor één gemiste tik.
func TestKeepaliveInterval_StaysUnderTheProviderTimeout(t *testing.T) {
	if keepaliveInterval*2 > 20*time.Second {
		t.Errorf("keepalive elke %v laat geen ruimte voor een gemiste tik binnen de 20s-timeout", keepaliveInterval)
	}
}

// ── Falen ────────────────────────────────────────────────────────────────────

// Een providerfout beëindigt de sessie; de brug erboven ziet Done dichtgaan en
// begint met backoff opnieuw. De foutmelding draagt de code, geen inhoud.
func TestTranscriber_ProviderErrorEndsTheSession(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, sttResponse{ErrorCode: 401, ErrorMessage: "unauthenticated"})
		<-conn.CloseRead(context.Background()).Done()
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("de sessie bleef hangen op een providerfout")
	}
}

// Een onleesbaar bericht is geen reden om de sessie te slopen: de volgende
// tokens komen gewoon door.
func TestTranscriber_UnparseableMessageIsSkippedNotFatal(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		if err := conn.Write(context.Background(), websocket.MessageText, []byte("{niet-json")); err != nil {
			return
		}
		send(t, conn, sttResponse{Tokens: []sttToken{tok("Toch", false)}})
		<-conn.CloseRead(context.Background()).Done()
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	if evs := collectEvents(t, sess, 1); evs[0].Text != "Toch" {
		t.Errorf("na een onleesbaar bericht kwam er niets meer door: %+v", evs)
	}
}

// Valt de socket zomaar weg — dat is het scenario waar de brug haar backoff
// voor heeft — dan sluit de sessie netjes af in plaats van te blijven hangen.
func TestTranscriber_DroppedSocketClosesTheSession(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		conn.CloseNow() //nolint:errcheck
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("een weggevallen socket liet de sessie hangen")
	}
}

// Wat er nog in de lopende zin stond bij een nette afsluiting gaat alsnog het
// venster in — anders verdwijnt de laatste uiting van de vergadering.
func TestTranscriber_FinishedFlushesThePendingSegment(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, sttResponse{Tokens: []sttToken{tok("Laatste woord", true)}})
		send(t, conn, sttResponse{Finished: true})
		<-conn.CloseRead(context.Background()).Done()
	})
	sess, err := f.transcriber(Config{}).Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	evs := collectEvents(t, sess, 2)
	if !evs[1].IsFinal || evs[1].Text != "Laatste woord" {
		t.Errorf("de staart werd niet gecommit: %+v", evs)
	}
}

// De echte dial-functie moet ook werken; hij is de enige regel die in
// productie een andere weg loopt dan in de tests. httptest praat lokaal, dus
// dit raakt geen netwerk buiten de machine.
func TestDialWebSocket_ConnectsAndReportsFailure(t *testing.T) {
	f := newFakeSoniox(t, nil)
	conn, err := dialWebSocket(t.Context(), "ws"+strings.TrimPrefix(f.srv.URL, "http"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.CloseNow() //nolint:errcheck

	if _, err := dialWebSocket(t.Context(), "ws://127.0.0.1:1/nergens"); err == nil {
		t.Error("een dial naar een dichte poort hoort te falen")
	}
}

// Een handshake die niet weggeschreven kan worden — de socket is al dicht — is
// een startfout, geen sessie die stilletjes niets doet.
func TestTranscriber_ConfigWriteFailureIsAStartError(t *testing.T) {
	f := newFakeSoniox(t, nil)
	tr := f.transcriber(Config{})
	tr.dial = func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.srv.URL, "http"), nil)
		if err != nil {
			return nil, err
		}
		conn.CloseNow() //nolint:errcheck
		return conn, nil
	}
	if _, err := tr.Start(t.Context(), "nl"); err == nil {
		t.Fatal("verwacht een fout wanneer de configuratie niet verstuurd kan worden")
	}
}

// Een dial die faalt is een startfout, geen halve sessie.
func TestTranscriber_DialFailureIsAStartError(t *testing.T) {
	tr := &transcriber{cfg: Config{APIKey: "k"}, dial: func(context.Context, string) (*websocket.Conn, error) {
		return nil, context.DeadlineExceeded
	}}
	if _, err := tr.Start(t.Context(), "nl"); err == nil {
		t.Fatal("verwacht een fout bij een mislukte dial")
	}
}

// ── Configuratie ─────────────────────────────────────────────────────────────

// Zonder sleutel is er geen provider — dat is een configuratiefout, geen
// stille degradatie.
func TestNewRealtimeTranscriber_RequiresAKey(t *testing.T) {
	if _, err := NewRealtimeTranscriber(Config{APIKey: "  "}); err == nil {
		t.Fatal("verwacht ErrNoAPIKey")
	}
	if _, err := NewRealtimeTranscriber(Config{APIKey: "k"}); err != nil {
		t.Fatalf("met sleutel hoort het te lukken: %v", err)
	}
}

// De default-endpoint is de EU-regio. Dat is de hele reden dat dit pakket
// bestaat: een tikfout die stilletjes naar de VS zou wijzen moet hier stuk.
func TestTranscriber_DefaultsToTheEURegion(t *testing.T) {
	tr := &transcriber{cfg: Config{APIKey: "k"}}
	if got := tr.url(); got != "wss://stt-rt.eu.soniox.com/transcribe-websocket" {
		t.Errorf("default-endpoint = %q, verwacht de EU-regio", got)
	}
	if !strings.Contains(tr.url(), ".eu.") {
		t.Error("de default-endpoint zit niet in de EU-regio")
	}
	// En de sleutel staat nooit in de URL.
	if strings.Contains(tr.url(), "k") && strings.Contains(tr.url(), "api_key") {
		t.Error("de sleutel lekt via de URL")
	}
	other := &transcriber{cfg: Config{APIKey: "k", Host: "stt-rt.soniox.com", Model: "stt-rt-v4"}}
	if other.url() != "wss://stt-rt.soniox.com/transcribe-websocket" || other.model() != "stt-rt-v4" {
		t.Errorf("host/model-override werkt niet: %s %s", other.url(), other.model())
	}
	if tr.model() != defaultSTTModel {
		t.Errorf("default-model = %q", tr.model())
	}
	if tr.log() == nil {
		t.Error("log() hoort nooit nil te zijn")
	}
}
