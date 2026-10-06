package soniox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/parai-nl/ai-maps-duiven/module/duif/ttsresult"
)

// collectTTSText levert de tekstberichten die de client stuurt, tot de socket
// dichtgaat.
func collectTTSText(conn *websocket.Conn) <-chan ttsText {
	out := make(chan ttsText)
	go func() {
		defer close(out)
		for {
			_, raw, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var msg ttsText
			if err := json.Unmarshal(raw, &msg); err == nil {
				out <- msg
			}
		}
	}()
	return out
}

// De handshake: EU-endpoint, MP3 (wat de vergaderclient afspeelt), de taal van
// de sessie en een Soniox-stemnaam.
func TestSynthesizer_HandshakeAsksForMP3InTheSessionLanguage(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, ttsResponse{Terminated: true})
	})
	sess, err := f.synthesizer(SynthConfig{Voice: "Mina"}).Synthesize(t.Context(), "elevenlabs-id", "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	<-sess.Done

	cfg := f.config()
	for veld, want := range map[string]any{
		"api_key":      "test-key",
		"model":        defaultTTSModel,
		"language":     "nl",
		"voice":        "Mina",
		"audio_format": ttsAudioFormat,
		"sample_rate":  float64(ttsSampleRate),
	} {
		if cfg[veld] != want {
			t.Errorf("configuratie[%s] = %v, verwacht %v", veld, cfg[veld], want)
		}
	}
	if id, _ := cfg["stream_id"].(string); !strings.HasPrefix(id, "duif-") {
		t.Errorf("stream_id = %v, verwacht een duif-id", cfg["stream_id"])
	}
}

// De stem-id uit personas.yaml is een ElevenLabs-id en betekent hier niets. De
// geconfigureerde Soniox-stem wint; zonder configuratie de default.
func TestSynthesizer_IgnoresTheElevenLabsVoiceIDAndUsesTheConfiguredVoice(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, ttsResponse{Terminated: true})
	})
	sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "21m00Tcm4TlvDq8ikWAM", "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	<-sess.Done
	if got := f.config()["voice"]; got != DefaultTTSVoice {
		t.Errorf("voice = %v, verwacht de default %q", got, DefaultTTSVoice)
	}
}

// De volle route: zinnen erin, MP3-frames eruit. Het sluiten van het
// tekstkanaal is het teken dat de uiting compleet is.
func TestSynthesizer_StreamsTextUpAndAudioDown(t *testing.T) {
	upstream := make(chan ttsText, 8)
	// De nepserver spreekt de echte volgorde af: pas als de client zijn uiting
	// afsluit (text_end) komt de audio terug, en daarna de terminatie.
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		defer close(upstream)
		for msg := range collectTTSText(conn) {
			upstream <- msg
			if !msg.TextEnd {
				continue
			}
			send(t, conn, ttsResponse{Audio: base64.StdEncoding.EncodeToString([]byte("mp3-een"))})
			send(t, conn, ttsResponse{Audio: base64.StdEncoding.EncodeToString([]byte("mp3-twee")), AudioEnd: true})
			send(t, conn, ttsResponse{Terminated: true})
			<-conn.CloseRead(context.Background()).Done()
			return
		}
	})
	sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "", "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck

	sess.Text <- "Roekoe. "
	close(sess.Text)

	var got []string
	for frame := range sess.Audio {
		got = append(got, string(frame))
	}
	if len(got) != 2 || got[0] != "mp3-een" || got[1] != "mp3-twee" {
		t.Fatalf("audioframes = %v, verwacht de twee gedecodeerde blokken", got)
	}

	// En de tekst ging er in twee berichten uit: de zin, en de afsluiting.
	var msgs []ttsText
	for msg := range upstream {
		msgs = append(msgs, msg)
	}
	if len(msgs) != 2 {
		t.Fatalf("%d tekstberichten, verwacht 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Text != "Roekoe. " || msgs[0].TextEnd {
		t.Errorf("eerste bericht = %+v", msgs[0])
	}
	if !msgs[1].TextEnd || msgs[1].Text != "" {
		t.Errorf("afsluiting = %+v, verwacht een leeg bericht met text_end", msgs[1])
	}
	if msgs[0].StreamID == "" || msgs[0].StreamID != msgs[1].StreamID {
		t.Errorf("de stream-id klopt niet: %q / %q", msgs[0].StreamID, msgs[1].StreamID)
	}
}

// Een providerfout beëindigt de beurt; de Voice-seam erboven meldt "geen
// audio" en de vergadering gaat tekst-only verder.
func TestSynthesizer_ProviderErrorEndsTheTurn(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, ttsResponse{ErrorCode: 402, ErrorMessage: "payment required"})
		<-conn.CloseRead(context.Background()).Done()
	})
	sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "", "nl")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("de beurt bleef hangen op een providerfout")
	}
}

// Kapotte base64 is een harde fout: half afgespeelde ruis is erger dan stilte.
func TestSynthesizer_UndecodableAudioEndsTheTurn(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, ttsResponse{Audio: "!!geen base64!!"})
		<-conn.CloseRead(context.Background()).Done()
	})
	sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "", "nl")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("de beurt bleef hangen op onleesbare audio")
	}
}

// Een onleesbaar TTS-bericht kan audioverlies verbergen en beëindigt met fout.
func TestSynthesizer_UnparseableMessageIsFailure(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		if err := conn.Write(context.Background(), websocket.MessageText, []byte("{stuk")); err != nil {
			return
		}
		send(t, conn, ttsResponse{Audio: base64.StdEncoding.EncodeToString([]byte("mp3"))})
		send(t, conn, ttsResponse{Terminated: true})
		<-conn.CloseRead(context.Background()).Done()
	})
	sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "", "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	for range sess.Audio {
	}
	result := ttsresult.Read(sess)
	if result == nil {
		t.Fatal("terminal result missing")
	}
	if err := <-result; err == nil {
		t.Fatal("invalid provider frame reported successful completion")
	}
}

// Een dial die faalt is een startfout.
func TestSynthesizer_DialFailureIsAStartError(t *testing.T) {
	s := &synthesizer{cfg: SynthConfig{APIKey: "k"}, dial: func(context.Context, string) (*websocket.Conn, error) {
		return nil, context.DeadlineExceeded
	}}
	if _, err := s.Synthesize(t.Context(), "", "nl"); err == nil {
		t.Fatal("verwacht een fout bij een mislukte dial")
	}
}

// Ook aan de TTS-kant is een onverzendbare handshake een startfout.
func TestSynthesizer_ConfigWriteFailureIsAStartError(t *testing.T) {
	f := newFakeSoniox(t, nil)
	s := f.synthesizer(SynthConfig{})
	s.dial = func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.srv.URL, "http"), nil)
		if err != nil {
			return nil, err
		}
		conn.CloseNow() //nolint:errcheck
		return conn, nil
	}
	if _, err := s.Synthesize(t.Context(), "", "nl"); err == nil {
		t.Fatal("verwacht een fout wanneer de configuratie niet verstuurd kan worden")
	}
}

func TestNewSynthesizer_RequiresAKey(t *testing.T) {
	if _, err := NewSynthesizer(SynthConfig{APIKey: " "}); err == nil {
		t.Fatal("verwacht ErrNoAPIKey")
	}
	if _, err := NewSynthesizer(SynthConfig{APIKey: "k"}); err != nil {
		t.Fatalf("met sleutel hoort het te lukken: %v", err)
	}
}

// Ook de TTS-endpoint wijst standaard naar de EU-regio.
func TestSynthesizer_DefaultsToTheEURegion(t *testing.T) {
	s := &synthesizer{cfg: SynthConfig{APIKey: "k"}}
	if got := s.url(); got != "wss://tts-rt.eu.soniox.com/tts-websocket" {
		t.Errorf("default-endpoint = %q, verwacht de EU-regio", got)
	}
	if s.model() != defaultTTSModel || s.voice() != DefaultTTSVoice || s.log() == nil {
		t.Errorf("defaults kloppen niet: %s / %s", s.model(), s.voice())
	}
	other := &synthesizer{cfg: SynthConfig{APIKey: "k", Host: "tts-rt.soniox.com", Model: "tts-rt-v1"}}
	if other.url() != "wss://tts-rt.soniox.com/tts-websocket" || other.model() != "tts-rt-v1" {
		t.Errorf("host/model-override werkt niet: %s %s", other.url(), other.model())
	}
}

// Twee sessies delen nooit een stream-id: Soniox correleert er zijn audio mee.
func TestStreamID_IsUniquePerSession(t *testing.T) {
	if streamID() == streamID() {
		t.Error("twee sessies kregen dezelfde stream-id")
	}
}

func TestSynthesizer_TerminalResultDistinguishesCompletionFromPartialFailure(t *testing.T) {
	for _, mode := range []string{"complete", "partial-error", "closed-without-terminal", "audio-end-only"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
				send(t, conn, ttsResponse{Audio: base64.StdEncoding.EncodeToString([]byte{1, 0}), AudioEnd: mode == "audio-end-only"})
				switch mode {
				case "complete":
					send(t, conn, ttsResponse{Terminated: true})
				case "partial-error":
					send(t, conn, ttsResponse{ErrorCode: 402, ErrorMessage: "private-provider-echo"})
				default:
					conn.Close(websocket.StatusNormalClosure, "done")
					return
				}
				<-conn.CloseRead(context.Background()).Done()
			})
			sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "", "nl")
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			var total int
			for frame := range sess.Audio {
				total += len(frame)
			}
			result := ttsresult.Read(sess)
			if result == nil {
				t.Fatal("result contract missing")
			}
			terminal := <-result
			if mode == "complete" && (terminal != nil || total != 2) {
				t.Fatalf("completion=%v bytes=%d", terminal, total)
			}
			if mode != "complete" && terminal == nil {
				t.Fatal("unconfirmed output reported success")
			}
			if terminal != nil && strings.Contains(terminal.Error(), "private-") {
				t.Fatal("provider payload escaped")
			}
		})
	}
}

// Het spreektempo gaat als `speed` mee in het openingsbericht. Eén
// gedeelde stem, per duif een ander tempo — dus dit veld moet echt de draad op.
func TestSynthesizer_SendsTheRequestedSpeed(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, ttsResponse{Terminated: true})
	})
	sess, err := f.synthesizer(SynthConfig{}).SynthesizeAtSpeed(t.Context(), "", "nl", 1.10)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	<-sess.Done
	if got := f.config()["speed"]; got != 1.10 {
		t.Errorf("speed = %v, verwacht 1.10", got)
	}
}

// Zonder tempo blijft het veld weg: dan spreekt de dienst op zijn eigen 1,0.
// Een 0 meesturen zou Soniox met invalid_request weigeren.
func TestSynthesizer_OmitsSpeedWhenTheVoiceHasNoTempo(t *testing.T) {
	f := newFakeSoniox(t, func(t *testing.T, conn *websocket.Conn, _ map[string]any) {
		send(t, conn, ttsResponse{Terminated: true})
	})
	sess, err := f.synthesizer(SynthConfig{}).Synthesize(t.Context(), "", "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck
	<-sess.Done
	if _, aanwezig := f.config()["speed"]; aanwezig {
		t.Errorf("speed = %v, verwacht dat het veld wegblijft", f.config()["speed"])
	}
}

// Een tempo buiten 0,7-1,3 weigert Soniox met invalid_request, en dat zou de
// hele beurt kosten. De synthesizer knijpt het daarom af in plaats van het te
// sturen; de configuratie weigert zo'n waarde al bij het booten.
func TestSynthesizer_ClampsSpeedToTheSupportedRange(t *testing.T) {
	for _, tc := range []struct {
		naam string
		in   float64
		want float64
	}{
		{"te traag", 0.2, MinSpeed},
		{"te snel", 2.5, MaxSpeed},
		{"binnen bereik", 1.05, 1.05},
	} {
		t.Run(tc.naam, func(t *testing.T) {
			if got := clampSpeed(tc.in); got != tc.want {
				t.Errorf("clampSpeed(%v) = %v, verwacht %v", tc.in, got, tc.want)
			}
		})
	}
}
