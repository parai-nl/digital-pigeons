package duif

import (
	"strings"
	"testing"

	"github.com/parai-nl/ai-maps-duiven/module/duif/soniox"
	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/audio/audiotesting"
)

// Zonder vlag verandert er niets: de vergadermodus praat tegen wat parai-core
// wired. Dat is de belofte van deze keuze — niets gaat om zonder opt-in.
func TestInit_WithoutProviderFlags_KeepsTheCoreWiring(t *testing.T) {
	tr := &audiotesting.MockRealtimeTranscriber{}
	synth := &audiotesting.MockSynthesizer{}
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), RealtimeTranscriber: tr, Synthesizer: synth}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.transcriber != tr || m.synthesizer != synth {
		t.Error("Init verving de core-seams zonder dat erom gevraagd is")
	}
	if stt, tts := m.audioProviders(); stt != providerElevenLabs || tts != providerElevenLabs {
		t.Errorf("providers = %s/%s, verwacht de default", stt, tts)
	}
}

// Met de vlag én een sleutel komt Soniox ervoor in de plaats — en alleen de
// seam waar de vlag over gaat.
func TestInit_STTProviderSoniox_ReplacesOnlyTheTranscriber(t *testing.T) {
	tr := &audiotesting.MockRealtimeTranscriber{}
	synth := &audiotesting.MockSynthesizer{}
	t.Setenv("DUIF_STT_PROVIDER", "soniox")
	t.Setenv("SONIOX_API_KEY", "test-key")

	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), RealtimeTranscriber: tr, Synthesizer: synth}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.transcriber == nil || m.transcriber == tr {
		t.Error("de STT-seam werd niet door Soniox vervangen")
	}
	if m.synthesizer != synth {
		t.Error("de TTS-seam hoorde ongemoeid te blijven")
	}
	if stt, tts := m.audioProviders(); stt != providerSoniox || tts != providerElevenLabs {
		t.Errorf("providers = %s/%s, verwacht soniox/elevenlabs", stt, tts)
	}
}

func TestInit_TTSProviderSoniox_ReplacesOnlyTheSynthesizer(t *testing.T) {
	tr := &audiotesting.MockRealtimeTranscriber{}
	synth := &audiotesting.MockSynthesizer{}
	t.Setenv("DUIF_TTS_PROVIDER", "soniox")
	t.Setenv("SONIOX_API_KEY", "test-key")
	t.Setenv("SONIOX_TTS_VOICE", "Mina")

	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), RealtimeTranscriber: tr, Synthesizer: synth}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.synthesizer == nil || m.synthesizer == synth {
		t.Error("de TTS-seam werd niet door Soniox vervangen")
	}
	if m.transcriber != tr {
		t.Error("de STT-seam hoorde ongemoeid te blijven")
	}
	if _, tts := m.audioProviders(); tts != providerSoniox {
		t.Errorf("tts-provider = %s", tts)
	}
}

// De vlag zonder sleutel is een configuratiefout, geen stille terugval naar
// ElevenLabs — dan zou vergaderaudio ongemerkt de EU verlaten.
func TestInit_SonioxWithoutKey_FailsFast(t *testing.T) {
	for _, env := range []string{"DUIF_STT_PROVIDER", "DUIF_TTS_PROVIDER"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "soniox")
			t.Setenv("SONIOX_API_KEY", "")
			err := New().Init(&parai.Core{DB: idlePool(t)})
			if err == nil {
				t.Fatal("Init hoort te falen zonder SONIOX_API_KEY")
			}
			if !strings.Contains(err.Error(), "SONIOX_API_KEY") {
				t.Errorf("foutmelding = %q, verwacht een melding over de sleutel", err)
			}
		})
	}
}

// Een tikfout in de providernaam is ook een fout: "sonix" mag niet stilletjes
// ElevenLabs betekenen.
func TestInit_UnknownProvider_FailsFast(t *testing.T) {
	t.Setenv("DUIF_STT_PROVIDER", "sonix")
	err := New().Init(&parai.Core{DB: idlePool(t)})
	if err == nil || !strings.Contains(err.Error(), "sonix") {
		t.Fatalf("verwacht een fout over de onbekende provider, kreeg %v", err)
	}
}

// De naam is hoofdletterongevoelig en verdraagt spaties: een .env-regel is
// handwerk.
func TestProvider_IsForgivingAboutCaseAndSpaces(t *testing.T) {
	t.Setenv("DUIF_STT_PROVIDER", "  Soniox ")
	got, err := provider("DUIF_STT_PROVIDER")
	if err != nil || got != providerSoniox {
		t.Fatalf("provider() = %q, %v", got, err)
	}
	t.Setenv("DUIF_STT_PROVIDER", "ElevenLabs")
	if got, err := provider("DUIF_STT_PROVIDER"); err != nil || got != providerElevenLabs {
		t.Fatalf("provider() = %q, %v", got, err)
	}
}

// De opstartregel vertelt waar de vergaderaudio naartoe gaat. Zonder die regel
// is de providerkeuze onzichtbaar voor wie de server draait.
func TestInit_LogsTheChosenAudioProviders(t *testing.T) {
	logger, logged := captureLogs()
	t.Setenv("DUIF_STT_PROVIDER", "soniox")
	t.Setenv("SONIOX_API_KEY", "test-key")
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	out := logged()
	if !strings.Contains(out, "audioproviders gekozen") || !strings.Contains(out, "stt=soniox") {
		t.Errorf("het opstartlog noemt de providerkeuze niet:\n%s", out)
	}
	// En de sleutel staat er niet in.
	if strings.Contains(out, "test-key") {
		t.Error("de API-sleutel belandde in het opstartlog")
	}
}

// De hostoverride bestaat voor een test tegen een andere regio, maar de
// default blijft de EU. Dit is de toets waar de providerkeuze op rust.
func TestSonioxConfig_DefaultsToTheEUEndpointAndCarriesTheOverrides(t *testing.T) {
	t.Setenv("SONIOX_API_KEY", "test-key")
	m := New()

	cfg, err := sonioxSTTConfig(m)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "" {
		t.Errorf("Host = %q, verwacht leeg zodat het pakket de EU-regio kiest", cfg.Host)
	}
	if cfg.Diarization {
		t.Error("diarisatie hoort standaard uit te staan (gelijk aan het huidige gedrag)")
	}

	t.Setenv("SONIOX_STT_HOST", "stt-rt.soniox.com")
	t.Setenv("SONIOX_STT_MODEL", "stt-rt-v4")
	t.Setenv("SONIOX_DIARIZATION", "1")
	cfg, err = sonioxSTTConfig(m)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "stt-rt.soniox.com" || cfg.Model != "stt-rt-v4" || !cfg.Diarization {
		t.Errorf("overrides kwamen niet door: %+v", cfg)
	}

	t.Setenv("SONIOX_TTS_HOST", "tts-rt.soniox.com")
	t.Setenv("SONIOX_TTS_MODEL", "tts-rt-v1")
	t.Setenv("SONIOX_TTS_VOICE", "Mina")
	tcfg, err := sonioxTTSConfig(m)
	if err != nil {
		t.Fatal(err)
	}
	if tcfg.Host != "tts-rt.soniox.com" || tcfg.Model != "tts-rt-v1" || tcfg.Voice != "Mina" {
		t.Errorf("tts-overrides kwamen niet door: %+v", tcfg)
	}
}

// De stem hoort in het opstartlog: één stem voor beide pratende duiven is een
// bewuste keuze, en een installatie die die keuze mist hoort dat te zien. Deze
// test bewaakt beide kanten — de gekozen stem én het vangnet.
func TestAudioVoice_NamesTheSingleSonioxVoiceAndFlagsTheFallback(t *testing.T) {
	t.Setenv("SONIOX_API_KEY", "test-key")
	t.Setenv("DUIF_TTS_PROVIDER", "soniox")
	t.Setenv("SONIOX_TTS_VOICE", "Logan")

	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t)}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if naam, vangnet := m.audioVoice(); naam != "Logan" || vangnet {
		t.Errorf("stem = %q (vangnet=%v), verwacht de geconfigureerde Logan", naam, vangnet)
	}

	t.Setenv("SONIOX_TTS_VOICE", "  ")
	m = New()
	if err := m.Init(&parai.Core{DB: idlePool(t)}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	naam, vangnet := m.audioVoice()
	if naam != soniox.DefaultTTSVoice || !vangnet {
		t.Errorf("stem = %q (vangnet=%v), verwacht het gemarkeerde vangnet %q", naam, vangnet, soniox.DefaultTTSVoice)
	}
}

// Op de ElevenLabs-route is er geen enkele stem te noemen: die staat per duif
// in personas.yaml. Het log mag daar dus geen stemnaam suggereren.
func TestAudioVoice_ElevenLabsRouteNamesNoSingleVoice(t *testing.T) {
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t)}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	naam, vangnet := m.audioVoice()
	if vangnet || !strings.Contains(naam, "personas.yaml") {
		t.Errorf("stem = %q (vangnet=%v), verwacht een verwijzing naar personas.yaml", naam, vangnet)
	}
}
