package soniox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio"
)

// De live-meting van de spraakherkenning. Hij draait niet in de gewone suite: zonder
// DUIF_LIVE_SONIOX=1 slaat hij over, en hij kost echte API-tijd.
//
//	DUIF_LIVE_SONIOX=1 SONIOX_LIVE_PCM=/pad/naar/spraak.pcm go test -run TestLive ./duif/soniox/
//
// Wat hij meet is precies wat de vergadermodus voelt: hoe lang duurt het
// voordat er iets op het scherm staat (eerste partial), en hoe lang voordat er
// een segment het vergadervenster in mag (eerste final). Beide providers gaan
// door dezelfde interface en krijgen dezelfde audio in hetzelfde tempo, dus de
// getallen zijn onderling vergelijkbaar.

// frameBytes is 120 ms PCM16 op 16 kHz — het tempo dat de browser aanhoudt.
const frameBytes = 3840

func liveGate(t *testing.T) string {
	t.Helper()
	if os.Getenv("DUIF_LIVE_SONIOX") != "1" {
		t.Skip("live-meting alleen met DUIF_LIVE_SONIOX=1")
	}
	key := os.Getenv("SONIOX_API_KEY")
	if key == "" {
		t.Skip("SONIOX_API_KEY vereist")
	}
	return key
}

func livePCM(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("SONIOX_LIVE_PCM")
	if path == "" {
		t.Skip("SONIOX_LIVE_PCM vereist (PCM16 16 kHz mono)")
	}
	pcm, err := os.ReadFile(path) //nolint:gosec // testpad uit de omgeving
	if err != nil {
		t.Fatalf("testaudio lezen: %v", err)
	}
	return pcm
}

// meting is wat één providerrun oplevert.
type meting struct {
	provider     string
	firstPartial time.Duration
	firstFinal   time.Duration
	partials     int
	finals       int
	transcript   string
	audioLen     time.Duration
}

func (m meting) String() string {
	return fmt.Sprintf("%-11s audio=%v  eerste partial=%v  eerste final=%v  partials=%d  finals=%d\n  transcript: %q",
		m.provider, m.audioLen.Round(time.Millisecond),
		m.firstPartial.Round(time.Millisecond), m.firstFinal.Round(time.Millisecond),
		m.partials, m.finals, m.transcript)
}

// stream duwt de audio in realtime door een transcriber en meet mee. tail is
// hoelang hij na de laatste frame nog op nakomende segmenten wacht.
func stream(t *testing.T, tr audio.RealtimeTranscriber, naam, lang string, pcm []byte, tail time.Duration) meting {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sess, err := tr.Start(ctx, lang)
	if err != nil {
		t.Fatalf("%s: sessie starten: %v", naam, err)
	}
	defer sess.Close() //nolint:errcheck

	m := meting{provider: naam, audioLen: time.Duration(len(pcm)/2) * time.Second / 16000}
	var segments []string
	start := time.Now()

	// De leeskant loopt mee met de schrijfkant: zo meten we werkelijke latency
	// en niet de tijd tot we toevallig gingen lezen.
	gelezen := make(chan struct{})
	go func() {
		defer close(gelezen)
		for ev := range sess.Events {
			switch {
			case ev.IsFinal:
				if m.firstFinal == 0 {
					m.firstFinal = time.Since(start)
				}
				m.finals++
				segments = append(segments, ev.Text)
			default:
				if m.firstPartial == 0 {
					m.firstPartial = time.Since(start)
				}
				m.partials++
			}
		}
	}()

	// De twee seconden stilte achteraan horen bij de meting, niet erbuiten: een
	// spreker die uitgesproken is laat een pauze vallen, en dát is wat de
	// endpoint-detectie van Soniox de uiting laat afsluiten. Zonder die stilte
	// meet je een monoloog die nooit ophoudt.
	pcm = append(append([]byte{}, pcm...), make([]byte, 2*2*16000)...)
	for off := 0; off < len(pcm); off += frameBytes {
		end := min(off+frameBytes, len(pcm))
		frame := make([]byte, end-off)
		copy(frame, pcm[off:end])
		select {
		case sess.Audio <- frame:
		case <-sess.Done:
			t.Fatalf("%s: de sessie viel weg tijdens het streamen", naam)
		}
		time.Sleep(120 * time.Millisecond)
	}
	close(sess.Audio)

	select {
	case <-gelezen:
	case <-time.After(tail):
	}
	m.transcript = strings.Join(segments, " ")
	return m
}

// De Nederlandse meting: dit is de zin die de vergadermodus moet kunnen horen.
func TestLive_Soniox_Dutch(t *testing.T) {
	key := liveGate(t)
	pcm := livePCM(t)
	tr, err := NewRealtimeTranscriber(Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	m := stream(t, tr, "soniox", "nl", pcm, 10*time.Second)
	t.Log("\n" + m.String())
	if m.finals == 0 {
		t.Error("geen enkel gecommit segment: het vergadervenster zou leeg blijven")
	}
	if !strings.Contains(strings.ToLower(m.transcript), "duiv") {
		t.Errorf("het woord 'duiven' zit niet in het transcript: %q", m.transcript)
	}
}

// Dezelfde audio langs ElevenLabs, door dezelfde interface: dit is de
// vergelijking waar de providerkeuze op leunt.
func TestLive_ElevenLabs_Dutch(t *testing.T) {
	liveGate(t)
	key := os.Getenv("PARAI_AUDIO_ELEVENLABS_API_KEY")
	if key == "" {
		t.Skip("PARAI_AUDIO_ELEVENLABS_API_KEY vereist voor de vergelijking")
	}
	pcm := livePCM(t)
	tr, err := audio.NewRealtimeTranscriber(audio.Config{ElevenLabsAPIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	m := stream(t, tr, "elevenlabs", "nl", pcm, 10*time.Second)
	t.Log("\n" + m.String())
}

// De Engelse meting draait op hetzelfde pad; de audio komt uit
// SONIOX_LIVE_PCM_EN.
func TestLive_Soniox_English(t *testing.T) {
	key := liveGate(t)
	path := os.Getenv("SONIOX_LIVE_PCM_EN")
	if path == "" {
		t.Skip("SONIOX_LIVE_PCM_EN vereist")
	}
	pcm, err := os.ReadFile(path) //nolint:gosec // testpad uit de omgeving
	if err != nil {
		t.Fatal(err)
	}
	tr, err := NewRealtimeTranscriber(Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	m := stream(t, tr, "soniox-en", "en", pcm, 10*time.Second)
	t.Log("\n" + m.String())
	if m.finals == 0 {
		t.Error("geen enkel gecommit segment")
	}
}

// Taalwissel middenin: de sessie opent op Nederlands, maar er komt Engels
// doorheen. Met language_hints als hint (niet als dwang) plus
// enable_language_identification hoort dat gewoon getranscribeerd te worden.
func TestLive_Soniox_LanguageSwitchMidSession(t *testing.T) {
	key := liveGate(t)
	nl := livePCM(t)
	path := os.Getenv("SONIOX_LIVE_PCM_EN")
	if path == "" {
		t.Skip("SONIOX_LIVE_PCM_EN vereist")
	}
	en, err := os.ReadFile(path) //nolint:gosec // testpad uit de omgeving
	if err != nil {
		t.Fatal(err)
	}
	tr, err := NewRealtimeTranscriber(Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	m := stream(t, tr, "soniox-nl→en", "nl", append(append([]byte{}, nl...), en...), 12*time.Second)
	t.Log("\n" + m.String())
	// De taalhint is een hint, geen dwang: allebei de talen horen leesbaar uit
	// dezelfde sessie te komen.
	low := strings.ToLower(m.transcript)
	if !strings.Contains(low, "duiven") {
		t.Errorf("het Nederlandse deel ontbreekt: %q", m.transcript)
	}
	if !strings.Contains(low, "pigeons") {
		t.Errorf("het Engelse deel ontbreekt: %q", m.transcript)
	}
}

// Stilte: een halve minuut niets. Zonder keepalive sluit Soniox na twintig
// seconden; deze test bewijst dat de sessie blijft staan.
func TestLive_Soniox_SurvivesThirtySecondsOfSilence(t *testing.T) {
	key := liveGate(t)
	tr, err := NewRealtimeTranscriber(Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := tr.Start(t.Context(), "nl")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close() //nolint:errcheck

	select {
	case <-sess.Done:
		t.Fatal("de sessie viel weg tijdens de stilte — de keepalive werkt niet")
	case <-time.After(30 * time.Second):
	}

	// En hij luistert daarna nog: één frame moet er nog in kunnen.
	select {
	case sess.Audio <- make([]byte, frameBytes):
	case <-sess.Done:
		t.Fatal("de sessie nam na de stilte geen audio meer aan")
	}
}

// De TTS-kant: levert de EU-regio hoorbare Nederlandse spraak?
func TestLive_Soniox_TTS(t *testing.T) {
	key := liveGate(t)
	synth, err := NewSynthesizer(SynthConfig{APIKey: key, Voice: os.Getenv("SONIOX_TTS_VOICE")})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := synth.Synthesize(t.Context(), "", "nl")
	if err != nil {
		t.Fatalf("tts-sessie starten: %v", err)
	}
	defer sess.Close() //nolint:errcheck

	start := time.Now()
	sess.Text <- "Roekoe. Ik zit hier op het stationsplein en ik hoor jullie praten over voeren. "
	close(sess.Text)

	var total int
	var first time.Duration
	for frame := range sess.Audio {
		if first == 0 {
			first = time.Since(start)
		}
		total += len(frame)
	}
	t.Logf("soniox-tts: eerste frame na %v, %d bytes MP3", first.Round(time.Millisecond), total)
	if total == 0 {
		t.Error("de TTS leverde geen audio")
	}
}
