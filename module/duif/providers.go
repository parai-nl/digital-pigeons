package duif

import (
	"fmt"
	"os"
	"strings"

	"github.com/parai-nl/ai-maps-duiven/module/duif/soniox"
)

// De providerkeuze voor de twee audio-seams.
//
// parai-core wired ElevenLabs; dit bestand mag die keuze per installatie
// omzetten naar Soniox, dat een EU-regio met eigen endpoints heeft. De default
// is en blijft wat de core levert: wie niets instelt merkt hier niets van.
//
// De keuze staat bewust in een eigen bestand en niet in module.go. Ze raakt
// alleen de wiring, en zo blijft in één oogopslag te zien waar de vergaderaudio
// naartoe stroomt — en daar draait deze keuze om.

const (
	providerElevenLabs = "elevenlabs"
	providerSoniox     = "soniox"
)

// sttProvider/ttsProvider lezen de keuze uit de omgeving. Leeg of onbekend is
// geen stille terugval maar een harde fout: "ik dacht dat het op Soniox stond"
// is precies het misverstand dat deze vlag moet uitsluiten.
func provider(env string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(env)))
	switch v {
	case "", providerElevenLabs:
		return providerElevenLabs, nil
	case providerSoniox:
		return providerSoniox, nil
	default:
		return "", fmt.Errorf("duif: %s %q is geen bekende provider (%s of %s)", env, v, providerElevenLabs, providerSoniox)
	}
}

// sonioxConfig bouwt de gedeelde STT-configuratie uit de omgeving. De host
// blijft leeg wanneer er niets is ingesteld: het pakket kiest dan zelf de
// EU-regio.
func sonioxSTTConfig(m *Module) (soniox.Config, error) {
	key := os.Getenv("SONIOX_API_KEY")
	if strings.TrimSpace(key) == "" {
		return soniox.Config{}, fmt.Errorf("duif: DUIF_STT_PROVIDER=soniox maar SONIOX_API_KEY ontbreekt")
	}
	return soniox.Config{
		APIKey:      key,
		Host:        os.Getenv("SONIOX_STT_HOST"),
		Model:       os.Getenv("SONIOX_STT_MODEL"),
		Diarization: os.Getenv("SONIOX_DIARIZATION") == "1",
		Logger:      privateProviderLogger(m.logger),
	}, nil
}

func sonioxTTSConfig(m *Module) (soniox.SynthConfig, error) {
	key := os.Getenv("SONIOX_API_KEY")
	if strings.TrimSpace(key) == "" {
		return soniox.SynthConfig{}, fmt.Errorf("duif: DUIF_TTS_PROVIDER=soniox maar SONIOX_API_KEY ontbreekt")
	}
	// Eén stem voor beide pratende duiven, uit één variabele: dat is een
	// bewuste keuze en niet een plek waar nog iets per duif gekozen wordt.
	return soniox.SynthConfig{
		APIKey: key,
		Host:   os.Getenv("SONIOX_TTS_HOST"),
		Model:  os.Getenv("SONIOX_TTS_MODEL"),
		Voice:  strings.TrimSpace(os.Getenv("SONIOX_TTS_VOICE")),
		Logger: privateProviderLogger(m.logger),
	}, nil
}

// applyAudioProviders zet de providerkeuze om in de twee seams. Hij draait ná
// het overnemen van de core-services, zodat hij die kan vervangen.
func (m *Module) applyAudioProviders() error {
	stt, err := provider("DUIF_STT_PROVIDER")
	if err != nil {
		return err
	}
	if stt == providerSoniox {
		cfg, err := sonioxSTTConfig(m)
		if err != nil {
			return err
		}
		tr, err := soniox.NewRealtimeTranscriber(cfg)
		if err != nil {
			return fmt.Errorf("duif: soniox-STT opzetten: %w", err)
		}
		m.transcriber = tr
		m.sttProvider = providerSoniox
	}

	tts, err := provider("DUIF_TTS_PROVIDER")
	if err != nil {
		return err
	}
	if tts == providerSoniox {
		cfg, err := sonioxTTSConfig(m)
		if err != nil {
			return err
		}
		synth, err := soniox.NewSynthesizer(cfg)
		if err != nil {
			return fmt.Errorf("duif: soniox-TTS opzetten: %w", err)
		}
		m.synthesizer = synth
		m.ttsProvider = providerSoniox
		m.ttsVoice = cfg.Voice
	}
	return nil
}

// audioProviders geeft de twee gekozen providers terug, voor het opstartlog en
// de tests. Het is de enige plek die dat paar als geheel benoemt.
func (m *Module) audioProviders() (string, string) {
	stt, tts := m.sttProvider, m.ttsProvider
	if stt == "" {
		stt = providerElevenLabs
	}
	if tts == "" {
		tts = providerElevenLabs
	}
	return stt, tts
}

// personasWithTempo geeft de id's van de duiven met een eigen spreektempo, in
// configuratievolgorde. Het opstartlog gebruikt ze om te melden dat de gekozen
// TTS-route er niets mee doet.
func (m *Module) personasWithTempo() []string {
	if m.personas == nil {
		return nil
	}
	var ids []string
	for _, p := range m.personas.Personas {
		if p.SpeakingSpeed() > 0 {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// audioVoice is de stem in het opstartlog, plus of dat het vangnet is. Op de
// Soniox-route is er precies één stem voor beide pratende duiven, dus die is
// als naam te noemen; op de ElevenLabs-route staat de stem per duif in
// personas.yaml en is er geen enkele naam. De naam zelf is publieke
// Soniox-catalogus, geen providerinhoud en geen sleutel.
func (m *Module) audioVoice() (naam string, vangnet bool) {
	if m.ttsProvider != providerSoniox {
		return "per duif uit personas.yaml", false
	}
	if m.ttsVoice == "" {
		return soniox.DefaultTTSVoice, true
	}
	return m.ttsVoice, false
}

// ttsFormat is wat de gekozen TTS-provider levert. parai-core's
// ElevenLabs-synthesizer staat vast op MP3 (44,1 kHz, 128 kbps); Soniox
// levert PCM16 op 24 kHz, en dat is wat streaming afspelen mogelijk maakt.
func (m *Module) ttsFormat() AudioFormat {
	if m.ttsProvider == providerSoniox {
		return AudioFormat{Codec: CodecPCM16, SampleRate: soniox.OutputSampleRate}
	}
	return AudioFormat{Codec: CodecMP3, SampleRate: 44100}
}
