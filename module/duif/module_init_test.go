package duif

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/audio/audiotesting"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// idlePool levert een pgxpool zonder ooit te verbinden (pgxpool verbindt lui).
// Init raakt de database niet aan — het bouwt alleen de repo — dus dit is
// genoeg om de wiring te testen zonder Postgres.
func idlePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://duif@127.0.0.1:1/duif?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestInit_WiresCoreServicesAndPersonas(t *testing.T) {
	logger, _ := captureLogs()
	mock := llmtesting.NewMockService()
	embedder := &llmtesting.MockEmbedder{}
	knowledge := &mockKnowledge{}
	m := New()

	if err := m.Init(&parai.Core{
		DB:        idlePool(t),
		LLM:       mock,
		Embedder:  embedder,
		Knowledge: knowledge,
		Logger:    logger,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if m.personas == nil || len(m.personas.Personas) != 3 {
		t.Fatalf("personas niet geladen: %+v", m.personas)
	}
	if m.repo == nil {
		t.Error("repo moet op Postgres gewired zijn")
	}
	bounded, ok := m.llm.(*boundedLLM)
	if !ok || bounded.upstream != mock || m.embedder != embedder || m.knowledge != knowledge {
		t.Error("core-services niet doorgegeven aan de module")
	}
	if m.log() != logger {
		t.Error("Init moet de core-logger overnemen")
	}
	if m.model != defaultChatModel || m.minSimilarity != defaultMinSimilarity {
		t.Errorf("defaults gewijzigd: model=%q minSimilarity=%v", m.model, m.minSimilarity)
	}
}

func TestInit_WithoutDatabase_FailsFast(t *testing.T) {
	err := New().Init(&parai.Core{})
	if err == nil {
		t.Fatal("Init zonder database moet falen (chat-persistentie vereist Postgres)")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("foutmelding = %q, want een melding over de database", err)
	}
}

func TestInit_ModelPolicy(t *testing.T) {
	for _, key := range []string{"DUIF_CHAT_MODEL", "DUIF_DETECT_MODEL"} {
		for _, value := range []string{"", defaultChatModel, "other-provider/private-model", defaultChatModel + " "} {
			t.Run(key+"/"+value, func(t *testing.T) {
				t.Setenv("DUIF_CHAT_MODEL", "")
				t.Setenv("DUIF_DETECT_MODEL", "")
				t.Setenv(key, value)
				m := New()
				// A stale pre-Init mutation must not change the approved model.
				m.model, m.detectModel = "stale", "stale"
				err := m.Init(&parai.Core{DB: idlePool(t), LLM: llmtesting.NewMockService()})
				if value != "" && value != defaultChatModel {
					if err == nil || !strings.Contains(err.Error(), key) || strings.Contains(err.Error(), value) {
						t.Fatalf("expected safe %s configuration rejection", key)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if m.model != "vertex/gemini-3.6-flash-eu" || m.detectModel != m.model || m.meetings.model != m.model || m.meetings.detectModel != m.model {
					t.Fatal("chat and meeting models must all use the approved alias")
				}
				if guard, ok := m.llm.(*boundedLLM); !ok || guard.requiredModel != m.model {
					t.Fatal("all initialized completion routes must carry the model guard")
				}
			})
		}
	}
}

func TestInit_MinSimilarityOverrideFromEnv(t *testing.T) {
	t.Setenv("DUIF_MIN_SIMILARITY", "0.8")
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t)}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.minSimilarity != 0.8 {
		t.Errorf("minSimilarity = %v, want 0.8", m.minSimilarity)
	}
}

func TestInit_InvalidMinSimilarity_FailsFast(t *testing.T) {
	for _, v := range []string{"heel-hoog", "-0.1", "1.5"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("DUIF_MIN_SIMILARITY", v)
			err := New().Init(&parai.Core{DB: idlePool(t)})
			if err == nil {
				t.Fatalf("DUIF_MIN_SIMILARITY=%q moet een bootfout zijn", v)
			}
			if !strings.Contains(err.Error(), "DUIF_MIN_SIMILARITY") {
				t.Errorf("foutmelding = %q, moet de envvar benoemen", err)
			}
		})
	}
}

// Ontbrekende optionele services zijn geen bootfout maar een waarschuwing: de
// module degradeert naar 503 respectievelijk gap-modus, nooit naar fabricatie.
func TestInit_MissingLLMAndKB_WarnsButBoots(t *testing.T) {
	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	out := logged()
	if !strings.Contains(out, "geen LLM-service gewired") {
		t.Errorf("ontbrekende LLM moet gelogd worden, log = %q", out)
	}
	if !strings.Contains(out, "geen embedder/kb gewired") {
		t.Errorf("ontbrekende embedder/kb moet gelogd worden, log = %q", out)
	}
}

// Zonder gewirede logger valt de module terug op de default-logger.
func TestLog_FallsBackToDefault(t *testing.T) {
	if New().log() == nil {
		t.Fatal("log() mag nooit nil teruggeven")
	}
}

// Assets() levert de gebouwde module-frontend: één bundle die de core-shell
// mount onder /modules/duif/. Zonder gebouwde bundle (geen bun) valt er niets
// te toetsen — dan slaat deze controle zichzelf over, net als de e2e.
func TestAssets_ServesModuleFrontend(t *testing.T) {
	// De dev-variant van frontendFS leest van schijf relatief aan de
	// module-root (zoals `make dev` draait), niet aan het package.
	t.Chdir("..")

	assets := New().Assets()
	if assets == nil {
		t.Fatal("Assets() = nil; de module moet zijn frontend serveren")
	}
	if _, err := fs.ReadDir(assets, "."); err != nil {
		t.Fatalf("frontend/dist niet leesbaar via Assets(): %v", err)
	}
	if _, err := fs.Stat(assets, "bundle.js"); err != nil {
		t.Skip("geen gebouwde bundle in duif/frontend/dist: draai `make bundle`")
	}
}

// Een niet-gebouwde module-frontend is geen bootfout — de API draait gewoon —
// maar wel iets wat je bij het opstarten wilt zien: de shell mount anders een
// module zonder scherm, en dat is in de browser een stille 404.
func TestInit_WithoutBundle_WarnsAtBoot(t *testing.T) {
	// De dev-variant van frontendFS leest van schijf (relatief aan de
	// module-root), dus een lege werkmap is een module zonder bundle.
	t.Chdir(t.TempDir())
	if _, err := fs.Stat(New().Assets(), "bundle.js"); err == nil {
		t.Skip("de bundle zit in deze build embedded; deze tak bestaat alleen in dev")
	}

	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger}); err != nil {
		t.Fatalf("een ontbrekende bundle mag geen bootfout zijn: %v", err)
	}
	if !strings.Contains(logged(), "make bundle") {
		t.Errorf("een ontbrekende bundle hoort gemeld te worden:\n%s", logged())
	}
}

// En met bundle blijft het opstartlog er stil over.
func TestInit_WithBundle_SaysNothing(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "duif", "frontend", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "bundle.js"), []byte("export default {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	// Onder `-tags dev` leest frontendFS de aangemaakte dist hierboven; in een
	// shipping-build zit de bundle embedded en is hij er alleen na `make
	// bundle`. Zonder die bundle toetst deze test niets en slaat hij over, net
	// als zijn zustertest hierboven.
	if _, err := fs.Stat(New().Assets(), "bundle.js"); err != nil {
		t.Skip("geen gebouwde bundle embedded of in duif/frontend/dist: draai `make bundle`")
	}

	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if strings.Contains(logged(), "make bundle") {
		t.Errorf("met een gebouwde bundle hoort er geen waarschuwing te komen:\n%s", logged())
	}
}

// De twee Init-toetsen hierboven hangen aan de buildtag: onder `-tags dev`
// leest frontendFS van schijf, in een shipping-build zit de bundle embedded en
// slaan ze zichzelf over. Deze tabel toetst het besluit zelf, los van die tag —
// inclusief de tak die in geen van beide builds voorkomt: helemaal geen assets.
func TestWarnOnMissingBundle_DecidesPerAssetTree(t *testing.T) {
	tests := []struct {
		name   string
		assets fs.FS
		warns  bool
	}{
		{"met bundle", fstest.MapFS{"bundle.js": &fstest.MapFile{Data: []byte("export default {}\n")}}, false},
		{"lege dist", fstest.MapFS{}, true},
		{"map zonder bundle", fstest.MapFS{"index.html": &fstest.MapFile{}}, true},
		// Assets() geeft nil als fs.Sub struikelt; dan is er niets te mounten.
		{"geen assets", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, logged := captureLogs()
			m := New()
			m.logger = logger
			m.warnOnMissingBundle(tc.assets)
			if got := strings.Contains(logged(), "make bundle"); got != tc.warns {
				t.Errorf("waarschuwing = %v, want %v:\n%s", got, tc.warns, logged())
			}
		})
	}
}

// Zonder TTS-provider blijft de vergadering tekst-only. Dat is een keuze van
// de omgeving, geen bootfout — maar wel eentje die zichtbaar moet zijn.
func TestInit_WithoutSynthesizer_WarnsAndStaysTextOnly(t *testing.T) {
	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.synthesizer != nil {
		t.Error("zonder core-synthesizer hoort de seam leeg te blijven")
	}
	if m.meetings.voice != nil {
		t.Error("zonder synthesizer hoort de vergader-engine geen stem te krijgen")
	}
	if !strings.Contains(logged(), "geen TTS gewired") {
		t.Errorf("een ontbrekende TTS hoort gelogd te worden:\n%s", logged())
	}
}

// Met een provider hangt de echte adapter in de Voice-seam.
func TestInit_WithSynthesizer_WiresTheAdapter(t *testing.T) {
	logger, _ := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger, Synthesizer: &audiotesting.MockSynthesizer{}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, ok := m.meetings.voice.(*synthesizerVoice); !ok {
		t.Errorf("Voice-seam = %T, want de synthesizer-adapter", m.meetings.voice)
	}
}

// De dev-stem is een grote knop: hij geldt voor élke pratende duif. Het
// opstartlog meldt hem daarom, zonder de id zelf te tonen.
func TestInit_DevVoiceOverride_WarnsWithoutShowingTheID(t *testing.T) {
	const devID = "dev-stem-id-7731"
	t.Setenv("DUIF_DEV_VOICE_ID", devID)
	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger, Synthesizer: &audiotesting.MockSynthesizer{}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.devVoice != devID || m.meetings.devVoice != devID {
		t.Errorf("dev-stem = %q / %q, want %q", m.devVoice, m.meetings.devVoice, devID)
	}
	if !strings.Contains(logged(), "DUIF_DEV_VOICE_ID actief") {
		t.Errorf("de dev-stem hoort gemeld te worden:\n%s", logged())
	}
	if strings.Contains(logged(), devID) {
		t.Errorf("de stem-id hoort niet in het log:\n%s", logged())
	}
}

// Het spreektempo per duif is een Soniox-knop. Draait de installatie
// op ElevenLabs, dan doet de configuratie stilzwijgend niets; dat hoort één
// keer in het opstartlog, anders wacht iemand op een verschil dat nooit komt.
func TestInit_TempoOnARouteWithoutSpeed_WarnsAtBoot(t *testing.T) {
	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger, Synthesizer: &audiotesting.MockSynthesizer{}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	out := logged()
	if !strings.Contains(out, "spreektempo genegeerd") {
		t.Errorf("het opstartlog zwijgt over het genegeerde spreektempo:\n%s", out)
	}
	// En het zegt welke duiven het betreft, zodat het na te lopen is.
	if !strings.Contains(out, "individualist") {
		t.Errorf("het opstartlog noemt de duiven niet:\n%s", out)
	}
}

// Op de Soniox-route klopt het tempo wél, dus daar hoort die waarschuwing niet.
func TestInit_TempoOnSoniox_StaysSilent(t *testing.T) {
	t.Setenv("DUIF_TTS_PROVIDER", "soniox")
	t.Setenv("SONIOX_API_KEY", "test-key")
	logger, logged := captureLogs()
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), Logger: logger}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if strings.Contains(logged(), "spreektempo genegeerd") {
		t.Errorf("de Soniox-route kent het tempo wél:\n%s", logged())
	}
}

// Shutdown breekt een lopende kennisbank-import af en geeft geen fout terug,
// ook zonder gewirede importer.
func TestShutdown_StopsTheKnowledgeImport(t *testing.T) {
	m, _ := newTestModule(t, nil)
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
