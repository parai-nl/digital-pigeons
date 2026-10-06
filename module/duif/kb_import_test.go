package duif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// kbSentinel staat in de fouttekst van de nep-repository. Duikt hij op in een
// logregel, dan lekt de importer de fouttekst van de provider — en die echoot
// soms de invoer, en de invoer is hier kennisbanktekst.
const kbSentinel = "duivenkorrel-4412-repofouttekst"

func importFixture(t *testing.T, imp *kbImporter, f *mosaicFixture) importStatus {
	t.Helper()
	imp.run(context.Background(), f.writeZip(t, "kb.zip"))
	return imp.snapshot()
}

func TestKBImport_WritesTheGraphAndClosesWithAMarker(t *testing.T) {
	imp, fake := newTestImporter(t)
	st := importFixture(t, imp, newMosaicFixture())

	if st.State != ImportReady || st.Skipped {
		t.Fatalf("status = %+v, wil ready zonder skipped", st)
	}
	if st.KBID != "duiven-proef" || st.ImportedAt == "" {
		t.Errorf("status mist kb-id of tijdstip: %+v", st)
	}
	if st.Counts[tableDestillaat] != 2 || st.Counts[tableQuote] != 3 || st.Counts[data.RelHasQuote] != 3 {
		t.Errorf("tellingen kloppen niet: %v", st.Counts)
	}

	ctx := context.Background()
	desID := derived(st.KBID, tableDestillaat, "des-1")
	node, err := fake.GetNode(ctx, desID)
	if err != nil || node == nil {
		t.Fatalf("standpunt niet geschreven: %v", err)
	}
	if node["kb_id"] != st.KBID || node["thema_titel"] != "Overlast" {
		t.Errorf("standpunt-props onvolledig: %v", node)
	}
	// Het perspectief staat in core's tabel: anders blijft Source.perspective
	// bij elke turn leeg.
	if p, _ := fake.GetPerspectief(ctx, derived(st.KBID, tablePerspective, "per-1")); p == nil {
		t.Error("het perspectief hoort in tabel `perspective` te staan")
	}
	edges, err := fake.ListEdges(ctx, data.RelBelongsToPerspectief, []data.NodeID{desID}, nil)
	if err != nil || len(edges) != 1 {
		t.Fatalf("belongs_to_perspective ontbreekt: %v / %v", edges, err)
	}
	if edges[0].Props["kb_id"] != st.KBID || edges[0].Props["uitleg"] != "meet en telt" {
		t.Errorf("relatieprops onvolledig: %v", edges[0].Props)
	}

	marker, err := fake.GetNode(ctx, derived(st.KBID, tableImportMarker, st.KBID))
	if err != nil || marker == nil {
		t.Fatalf("marker niet geschreven: %v", err)
	}
	for key, want := range map[string]any{
		"kb_id":               "duiven-proef",
		"mapper_version":      kbMapperVersion,
		"embedding_model":     "test-embedder",
		"embedding_dimension": 1536,
	} {
		if marker[key] != want {
			t.Errorf("marker[%s] = %v, wil %v", key, marker[key], want)
		}
	}
	if h, _ := marker["content_hash"].(string); len(h) != 64 {
		t.Errorf("marker mist de content-hash: %v", marker["content_hash"])
	}
	if counts, ok := marker["counts"].(map[string]any); !ok || counts[tableDestillaat] != 2 {
		t.Errorf("marker mist de tellingen: %v", marker["counts"])
	}
}

func TestKBImport_SecondBootWithTheSameBundleWritesNothing(t *testing.T) {
	f := newMosaicFixture()
	first, fake := newTestImporter(t)
	if st := importFixture(t, first, f); st.State != ImportReady {
		t.Fatalf("eerste import mislukte: %+v", st)
	}
	before := fake.writeCount()

	second := testImporter(fake, first.logger, false)
	st := importFixture(t, second, f)
	if st.State != ImportReady || !st.Skipped {
		t.Fatalf("status = %+v, wil ready met skipped", st)
	}
	if fake.writeCount() != before {
		t.Errorf("%d schrijfacties na de tweede boot, wil 0", fake.writeCount()-before)
	}
	if st.Counts[tableDestillaat] != 2 {
		t.Errorf("de overgeslagen boot hoort de tellingen uit de marker te tonen: %v", st.Counts)
	}
}

func TestKBImport_ChangedBundleOrMapperImportsAgain(t *testing.T) {
	f := newMosaicFixture()
	first, fake := newTestImporter(t)
	importFixture(t, first, f)
	before := fake.writeCount()

	f.Nodes["Domein"][0]["titel"] = "Dorp"
	second := testImporter(fake, first.logger, false)
	st := importFixture(t, second, f)
	if st.State != ImportReady || st.Skipped {
		t.Fatalf("een gewijzigde bundel hoort opnieuw te importeren: %+v", st)
	}
	if fake.writeCount() == before {
		t.Error("er hoorde geschreven te worden")
	}
}

func TestKBImport_AnotherKnowledgeBaseBlocks(t *testing.T) {
	imp, fake := newTestImporter(t)
	if _, err := fake.CreateNodeWithID(context.Background(), "duif_import:vreemd", map[string]any{"kb_id": "andere-kb"}, ""); err != nil {
		t.Fatal(err)
	}
	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportFailed || st.Reason != ReasonImportKBIDMismatch {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportKBIDMismatch)
	}
}

func TestKBImport_AnotherEmbedderBlocks(t *testing.T) {
	f := newMosaicFixture()
	first, fake := newTestImporter(t)
	importFixture(t, first, f)

	second := newKBImporter(fake, first.logger, 4, false, "andere-embedder", 3072)
	st := importFixture(t, second, f)
	if st.State != ImportFailed || st.Reason != ReasonImportEmbeddingMismatch {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportEmbeddingMismatch)
	}
}

func TestKBImport_ForceReplacesEveryKnowledgeBaseInTheseTables(t *testing.T) {
	f := newMosaicFixture()
	first, fake := newTestImporter(t)
	importFixture(t, first, f)

	ctx := context.Background()
	// Een verweesd standpunt van dezelfde kb (stond in de vorige bundel), en
	// een standpunt plus marker van een tweede kennisbank. Dat laatste is het
	// gevaarlijke geval: mét embedding blijft het doorzoekbaar, terwijl de
	// graaf het als 404 verbergt.
	stale := data.NodeID("destillaat:00000000-0000-0000-0000-0000000000ff")
	foreign := data.NodeID("destillaat:00000000-0000-0000-0000-0000000000aa")
	if _, err := fake.CreateNodeWithID(ctx, stale, map[string]any{"kb_id": "duiven-proef"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.CreateNodeWithID(ctx, foreign, map[string]any{"kb_id": "andere-kb", "samenvatting": "Uit de vorige bank."}, "embed"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.CreateNodeWithID(ctx, "duif_import:vreemd", map[string]any{"kb_id": "andere-kb"}, ""); err != nil {
		t.Fatal(err)
	}

	forced := testImporter(fake, first.logger, true)
	st := importFixture(t, forced, f)
	if st.State != ImportReady || st.Skipped {
		t.Fatalf("FORCE hoort te vervangen, kreeg %+v", st)
	}
	if node, _ := fake.GetNode(ctx, stale); node != nil {
		t.Error("FORCE liet een verweesde knoop van deze kb staan")
	}
	if node, _ := fake.GetNode(ctx, foreign); node != nil {
		t.Error("FORCE liet een doorzoekbaar standpunt van een andere kb staan")
	}
	if node, _ := fake.GetNode(ctx, "duif_import:vreemd"); node != nil {
		t.Error("FORCE liet de marker van een andere kb staan")
	}
	if node, _ := fake.GetNode(ctx, derived(st.KBID, tableDestillaat, "des-1")); node == nil {
		t.Error("na FORCE hoort de bundel er weer volledig in te staan")
	}
}

func TestKBImport_CountMismatchLeavesNoMarker(t *testing.T) {
	fake := &fakeKnowledge{}
	logger, logged := captureLogs()
	imp := testImporter(&dropRelateWriter{fakeKnowledge: fake, drop: data.RelHasQuote}, logger, false)

	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportFailed || st.Reason != ReasonImportCountMismatch {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportCountMismatch)
	}
	if marker, _ := fake.GetNode(context.Background(), derived("duiven-proef", tableImportMarker, "duiven-proef")); marker != nil {
		t.Error("een half gevulde graaf hoort geen marker te krijgen")
	}
	if !strings.Contains(logged(), ReasonImportCountMismatch) {
		t.Error("de afwijkende telling hoort met reden-code gelogd te worden")
	}
}

func TestKBImport_WithoutEdgeListerOnlyNodesAreCounted(t *testing.T) {
	fake := &fakeKnowledge{}
	logger, _ := captureLogs()
	writer := listerOnlyWriter{inner: &dropRelateWriter{fakeKnowledge: fake, drop: data.RelHasQuote}}
	imp := testImporter(writer, logger, false)

	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportReady {
		t.Fatalf("zonder EdgeLister blijft de check bij de knopen: %+v", st)
	}
}

func TestKBImport_RetriesATransientWriteFailure(t *testing.T) {
	failer := &countingFailer{op: "create", fails: 2, err: errors.New(kbSentinel)}
	imp, fake := newTestImporter(t)
	fake.failOn = failer.hook

	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportReady {
		t.Fatalf("twee mislukte pogingen horen opgevangen te worden: %+v", st)
	}
	if failer.count() <= 2 {
		t.Errorf("er hoorden herhaalpogingen te zijn, kreeg %d", failer.count())
	}
}

func TestKBImport_GivesUpAfterThreeAttempts(t *testing.T) {
	logger, logged := captureLogs()
	fake := &fakeKnowledge{}
	fake.failOn = func(op string, _ data.NodeID) error {
		if op == "create" {
			return errors.New(kbSentinel)
		}
		return nil
	}
	imp := testImporter(fake, logger, false)

	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportFailed || st.Reason != ReasonImportWriteFailed {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportWriteFailed)
	}
	if marker, _ := fake.GetNode(context.Background(), derived("duiven-proef", tableImportMarker, "duiven-proef")); marker != nil {
		t.Error("een mislukte import hoort geen marker achter te laten")
	}
	// De harde privacy-eis geldt op elk niveau, ook Debug.
	logs := aboveDebug(logged())
	if strings.Contains(logs, kbSentinel) {
		t.Errorf("de fouttekst van de repository staat in het log:\n%s", logs)
	}
	if strings.Contains(logs, "Duiven voeren vergroot de overlast") || strings.Contains(logs, "broodkorsten") {
		t.Errorf("kennisbanktekst staat in het log:\n%s", logs)
	}
	if !strings.Contains(logs, ReasonImportWriteFailed) {
		t.Errorf("de reden-code hoort wél in het log te staan:\n%s", logs)
	}
	if strings.Contains(logged(), kbSentinel) {
		t.Errorf("Debug bevat een private providerfout:\n%s", logged())
	}
}

func TestKBImport_ShutdownStopsWithoutAMarker(t *testing.T) {
	logger, _ := captureLogs()
	fake := &fakeKnowledge{}
	imp := testImporter(fake, logger, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	imp.run(ctx, newMosaicFixture().writeZip(t, "kb.zip"))

	st := imp.snapshot()
	if st.State != ImportFailed || st.Reason != ReasonImportShutdown {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportShutdown)
	}
	if marker, _ := fake.GetNode(context.Background(), derived("duiven-proef", tableImportMarker, "duiven-proef")); marker != nil {
		t.Error("een afgebroken import hoort geen marker te schrijven")
	}
}

func TestKBImport_StopCancelsARunningImport(t *testing.T) {
	imp, fake := newTestImporter(t)
	fake.failOn = func(op string, _ data.NodeID) error {
		if op == "create" {
			return errors.New(kbSentinel)
		}
		return nil
	}
	// De wachtseam tussen twee pogingen is de plek waar een lopende import
	// onderbreekbaar stilstaat — precies waar Shutdown in productie invalt.
	var once sync.Once
	imp.sleep = func(ctx context.Context, _ time.Duration) bool {
		once.Do(func() { go imp.stop(context.Background()) })
		<-ctx.Done()
		return false
	}

	imp.start(context.Background(), newMosaicFixture().writeZip(t, "kb.zip"))
	waitFor(t, func() bool { return imp.snapshot().State == ImportFailed }, "de import stopte niet")
	if st := imp.snapshot(); st.Reason != ReasonImportShutdown {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportShutdown)
	}
	if marker, _ := fake.GetNode(context.Background(), derived("duiven-proef", tableImportMarker, "duiven-proef")); marker != nil {
		t.Error("een afgebroken import hoort geen marker te schrijven")
	}
}

func TestKBImport_StopAfterTheImportIsHarmless(t *testing.T) {
	imp, fake := newTestImporter(t)
	imp.start(context.Background(), newMosaicFixture().writeZip(t, "kb.zip"))
	waitFor(t, func() bool { return imp.snapshot().State == ImportReady }, "de import werd niet klaar")
	imp.stop(context.Background()) // na afloop: mag geen paniek geven
	if fake.writeCount() == 0 {
		t.Error("de achtergrondimport schreef niets")
	}
}

func TestKBImport_StopWithoutAStartedImportIsHarmless(t *testing.T) {
	imp, _ := newTestImporter(t)
	imp.stop(context.Background())
}

func TestKBImport_RunIsSingleFlight(t *testing.T) {
	imp, fake := newTestImporter(t)
	imp.running.Store(true)
	imp.run(context.Background(), newMosaicFixture().writeZip(t, "kb.zip"))
	if fake.writeCount() != 0 {
		t.Error("een tweede import naast een lopende hoort niets te doen")
	}
	if imp.snapshot().State != ImportEmpty {
		t.Errorf("de status van de lopende import hoort ongemoeid te blijven: %+v", imp.snapshot())
	}
}

func TestKBImport_ProgressIsLoggedAsCounts(t *testing.T) {
	logger, logged := captureLogs()
	fake := &fakeKnowledge{}
	imp := testImporter(fake, logger, false)
	imp.progressEvery = 2 // de echte drempel (500) haalt een mini-bundel niet

	if st := importFixture(t, imp, newMosaicFixture()); st.State != ImportReady {
		t.Fatalf("import mislukte: %+v", st)
	}
	logs := logged()
	if !strings.Contains(logs, "voortgang") || !strings.Contains(logs, "nodes=") {
		t.Errorf("de voortgang hoort als telling gelogd te worden:\n%s", logs)
	}
}

func TestKBImport_UnreadableBundleFailsWithoutLeakingTheReason(t *testing.T) {
	logger, logged := captureLogs()
	imp := testImporter(&fakeKnowledge{}, logger, false)
	imp.run(context.Background(), "/deze/bundel/bestaat/niet.zip")

	st := imp.snapshot()
	if st.State != ImportFailed || st.Reason != ReasonImportBundleInvalid {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportBundleInvalid)
	}
	if strings.Contains(aboveDebug(logged()), "/deze/bundel/bestaat/niet.zip") {
		t.Error("het bundelpad hoort niet in het log te staan boven Debug")
	}
}

func TestReadImportStatus_ReflectsWhatIsInTheGraph(t *testing.T) {
	ctx := context.Background()
	fake := &fakeKnowledge{}

	st, err := readImportStatus(ctx, fake)
	if err != nil || st.State != ImportEmpty {
		t.Fatalf("lege graaf = %+v (%v), wil empty", st, err)
	}

	// Een standpunt zonder import_hash komt niet van deze import: het zegt niets
	// over een afgebroken poging, en "incomplete" zou een volgende boot aan het
	// werk zetten voor een kennisbank die er niet is.
	if _, err := fake.CreateNodeWithID(ctx, "destillaat:vreemd", map[string]any{"kb_id": "andere-module"}, ""); err != nil {
		t.Fatal(err)
	}
	if st, _ = readImportStatus(ctx, fake); st.State != ImportEmpty {
		t.Errorf("standpunt zonder import_hash = %+v, wil empty", st)
	}

	if _, err := fake.CreateNodeWithID(ctx, "destillaat:1", map[string]any{"kb_id": "duiven-proef", "import_hash": "0f0f"}, ""); err != nil {
		t.Fatal(err)
	}
	if st, _ = readImportStatus(ctx, fake); st.State != ImportIncomplete {
		t.Errorf("standpunten zonder marker = %+v, wil incomplete", st)
	}

	props := map[string]any{
		"kb_id": "duiven-proef", "imported_at": "2026-09-03T10:00:00Z",
		"counts": map[string]any{tableDestillaat: 2},
	}
	if _, err := fake.CreateNodeWithID(ctx, "duif_import:1", props, ""); err != nil {
		t.Fatal(err)
	}
	st, _ = readImportStatus(ctx, fake)
	if st.State != ImportReady || st.KBID != "duiven-proef" || st.Counts[tableDestillaat] != 2 {
		t.Errorf("met marker = %+v, wil ready met kb-id en tellingen", st)
	}
}

func TestLoadBootStatus_UnreadableGraphIsAFailedState(t *testing.T) {
	logger, logged := captureLogs()
	fake := &fakeKnowledge{}
	imp := testImporter(failingKnowledge{fakeKnowledge: fake, listErr: errors.New(kbSentinel)}, logger, false)
	imp.loadBootStatus(context.Background())

	if st := imp.snapshot(); st.State != ImportFailed || st.Reason != ReasonImportMarkerUnreadable {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportMarkerUnreadable)
	}
	if strings.Contains(logged(), kbSentinel) {
		t.Error("de fouttekst van de repository hoort niet in het log")
	}
}

func TestKBImport_MarkerUnreadableFailsBeforeWriting(t *testing.T) {
	logger, _ := captureLogs()
	fake := &fakeKnowledge{}
	imp := testImporter(failingKnowledge{fakeKnowledge: fake, listErr: errors.New(kbSentinel)}, logger, false)

	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportFailed || st.Reason != ReasonImportMarkerUnreadable {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportMarkerUnreadable)
	}
	if fake.writeCount() != 0 {
		t.Error("zonder leesbare marker hoort er niets geschreven te worden")
	}
}

func TestAsIntAndCountsFromProps_SurviveJSONRoundTrips(t *testing.T) {
	if asInt(float64(7)) != 7 || asInt(int64(7)) != 7 || asInt(7) != 7 || asInt("zeven") != 0 {
		t.Error("asInt hoort jsonb-getallen én in-memory getallen te lezen")
	}
	if countsFromProps("geen map") != nil {
		t.Error("een niet-map hoort nil te geven")
	}
	got := countsFromProps(map[string]any{"destillaat": float64(4067)})
	if got["destillaat"] != 4067 {
		t.Errorf("counts = %v", got)
	}
}

// --- wiring (kb_module.go) --------------------------------------------------

func TestInitKnowledgeImport_WithoutKnowledgeRepoDoesNothing(t *testing.T) {
	logger, logged := captureLogs()
	m, _ := newTestModule(t, func(m *Module) { m.logger = logger })
	t.Setenv(envImportKB, "/pad/naar/kb.zip")

	m.initKnowledgeImport(nil)
	if m.kbImport != nil {
		t.Error("zonder kennisgraaf hoort er geen importer te zijn")
	}
	if !strings.Contains(logged(), ReasonImportNoKnowledge) {
		t.Errorf("de reden hoort gelogd te worden:\n%s", logged())
	}
	if st := m.knowledgeStatus(); st.State != ImportEmpty {
		t.Errorf("status = %+v, wil empty", st)
	}
}

func TestInitKnowledgeImport_WithoutEmbedderRefusesToImport(t *testing.T) {
	logger, logged := captureLogs()
	fake := &fakeKnowledge{}
	m, _ := newTestModule(t, func(m *Module) {
		m.logger = logger
		m.knowledge = fake
	})
	t.Setenv(envImportKB, newMosaicFixture().writeZip(t, "kb.zip"))

	m.initKnowledgeImport(nil)
	if !strings.Contains(logged(), ReasonImportNoEmbedder) {
		t.Errorf("de reden hoort gelogd te worden:\n%s", logged())
	}
	if fake.writeCount() != 0 {
		t.Error("zonder embedder zouden standpunten zonder vector in de graaf komen")
	}
}

func TestInitKnowledgeImport_ImportsAndServesTheStatus(t *testing.T) {
	logger, _ := captureLogs()
	fake := &fakeKnowledge{}
	m, _ := newTestModule(t, func(m *Module) {
		m.logger = logger
		m.knowledge = fake
		m.embedder = &llmtesting.MockEmbedder{}
	})
	t.Setenv(envImportKB, newMosaicFixture().writeZip(t, "kb.zip"))
	t.Setenv(envImportWorkers, "2")

	m.initKnowledgeImport(&parai.Core{Config: &parai.Config{
		Knowledge: parai.KnowledgeConfig{EmbeddingModel: "vertex/test", EmbeddingDimension: 1536},
	}})
	waitFor(t, func() bool { return m.knowledgeStatus().State == ImportReady }, "de import werd niet ready")

	st := m.knowledgeStatus()
	if st.KBID != "duiven-proef" || st.Counts[tableDestillaat] != 2 {
		t.Errorf("status = %+v", st)
	}
	marker, _ := fake.GetNode(context.Background(), derived("duiven-proef", tableImportMarker, "duiven-proef"))
	if marker == nil || marker["embedding_model"] != "vertex/test" {
		t.Errorf("het embeddingmodel uit de core-config hoort in de marker: %v", marker)
	}
	m.shutdownKnowledgeImport(context.Background())
}

func TestInitKnowledgeImport_WithoutEnvOnlyReadsTheStand(t *testing.T) {
	logger, _ := captureLogs()
	fake := &fakeKnowledge{}
	if _, err := fake.CreateNodeWithID(context.Background(), "destillaat:1", map[string]any{"kb_id": "duiven-proef", "import_hash": "0f0f"}, ""); err != nil {
		t.Fatal(err)
	}
	m, _ := newTestModule(t, func(m *Module) {
		m.logger = logger
		m.knowledge = fake
	})
	t.Setenv(envImportKB, "")

	m.initKnowledgeImport(nil)
	if st := m.knowledgeStatus(); st.State != ImportIncomplete {
		t.Errorf("status = %+v, wil incomplete (knopen zonder marker)", st)
	}
}

func TestImportWorkers_FallsBackOnAnUnusableValue(t *testing.T) {
	logger, logged := captureLogs()
	m, _ := newTestModule(t, func(m *Module) { m.logger = logger })

	t.Setenv(envImportWorkers, "")
	if got := m.importWorkers(); got != defaultImportWorkers {
		t.Errorf("leeg = %d, wil de default", got)
	}
	t.Setenv(envImportWorkers, "8")
	if got := m.importWorkers(); got != 8 {
		t.Errorf("8 = %d", got)
	}
	for _, waarde := range []string{"nul", "0", "-3", "999"} {
		t.Setenv(envImportWorkers, waarde)
		if got := m.importWorkers(); got != defaultImportWorkers {
			t.Errorf("%q = %d, wil de default", waarde, got)
		}
	}
	if !strings.Contains(logged(), reasonInvalidWorkers) {
		t.Errorf("een onbruikbare waarde hoort een waarschuwing met reden-code te geven:\n%s", logged())
	}
}

func TestShutdownKnowledgeImport_WithoutImporterIsHarmless(t *testing.T) {
	m, _ := newTestModule(t, nil)
	m.shutdownKnowledgeImport(context.Background())
}

func TestNewKBImporter_FallsBackToTheDefaultWorkerCount(t *testing.T) {
	imp := newKBImporter(&fakeKnowledge{}, nil, 0, false, "", 0)
	if imp.workers != defaultImportWorkers {
		t.Errorf("workers = %d, wil %d", imp.workers, defaultImportWorkers)
	}
	if imp.log() == nil {
		t.Error("zonder gewirede logger hoort de default-logger te gelden")
	}
}

func TestKBImport_LogsSkippedBundlePartsAsCounts(t *testing.T) {
	logger, logged := captureLogs()
	fake := &fakeKnowledge{}
	imp := testImporter(fake, logger, false)

	f := newMosaicFixture()
	f.Nodes["Sausage"] = []map[string]any{{"id": "s-1"}}
	if st := importFixture(t, imp, f); st.State != ImportReady {
		t.Fatalf("een onbekend label hoort de import niet te breken: %+v", st)
	}
	if !strings.Contains(logged(), ReasonKBUnknownLabel) {
		t.Errorf("het overgeslagen label hoort met reden-code gelogd te worden:\n%s", logged())
	}
}

func TestReadImportStatus_PropagatesAFailingDestillaatQuery(t *testing.T) {
	fake := &fakeKnowledge{}
	writer := failingKnowledge{fakeKnowledge: fake, listErr: errors.New(kbSentinel), listTable: tableDestillaat}
	if _, err := readImportStatus(context.Background(), writer); err == nil {
		t.Fatal("een onbereikbare graaf hoort een fout te geven")
	}
}

func TestKBImport_ReplaceFailuresAreReported(t *testing.T) {
	logger, _ := captureLogs()
	// staleGraph geeft een graaf met één knoop van deze kb, zodat replace
	// werkelijk iets te lezen en te verwijderen heeft.
	staleGraph := func(t *testing.T) *fakeKnowledge {
		t.Helper()
		fake := &fakeKnowledge{}
		if _, err := fake.CreateNodeWithID(context.Background(), "destillaat:1", map[string]any{"kb_id": "duiven-proef"}, ""); err != nil {
			t.Fatal(err)
		}
		return fake
	}

	t.Run("lezen faalt", func(t *testing.T) {
		writer := failingKnowledge{fakeKnowledge: staleGraph(t), listErr: errors.New(kbSentinel), listTable: tableQuote}
		imp := testImporter(writer, logger, true)
		if st := importFixture(t, imp, newMosaicFixture()); st.Reason != ReasonImportReplaceFailed {
			t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportReplaceFailed)
		}
	})

	t.Run("verwijderen faalt", func(t *testing.T) {
		writer := failingKnowledge{fakeKnowledge: staleGraph(t), deleteErr: errors.New(kbSentinel)}
		imp := testImporter(writer, logger, true)
		if st := importFixture(t, imp, newMosaicFixture()); st.Reason != ReasonImportReplaceFailed {
			t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportReplaceFailed)
		}
	})
}

// Elke schrijffase heeft dezelfde afloop: de fase stopt, de import faalt met
// write_failed en er komt geen marker. De vier gevallen verschillen alleen in
// wélke schrijfactie omvalt.
func TestKBImport_AFailingWriteStopsTheImport(t *testing.T) {
	cases := map[string]struct{ op, table string }{
		"marker":           {op: "create", table: tableImportMarker},
		"standpunt":        {op: "create", table: tableDestillaat},
		"standpuntrelatie": {op: "relate", table: tableDestillaat},
		"structuurrelatie": {op: "relate", table: tableDomein},
	}
	for naam, tc := range cases {
		t.Run(naam, func(t *testing.T) {
			imp, fake := newTestImporter(t)
			fake.failOn = func(op string, id data.NodeID) error {
				if op == tc.op && tableOf(string(id)) == tc.table {
					return errors.New(kbSentinel)
				}
				return nil
			}
			st := importFixture(t, imp, newMosaicFixture())
			if st.State != ImportFailed || st.Reason != ReasonImportWriteFailed {
				t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportWriteFailed)
			}
			if marker, _ := fake.GetNode(context.Background(), derived("duiven-proef", tableImportMarker, "duiven-proef")); marker != nil {
				t.Error("een mislukte import hoort geen marker achter te laten")
			}
		})
	}
}

func TestKBImport_AnEmptyPhaseIsSimplySkipped(t *testing.T) {
	f := newMosaicFixture()
	f.Rels["RELATES_TO"] = nil
	imp, _ := newTestImporter(t)
	if st := importFixture(t, imp, f); st.State != ImportReady {
		t.Fatalf("een bundel zonder relates_to hoort gewoon te importeren: %+v", st)
	}
}

func TestKBImport_ACancelDuringTheBackoffStopsTheImport(t *testing.T) {
	logger, _ := captureLogs()
	fake := &fakeKnowledge{}
	fake.failOn = func(op string, _ data.NodeID) error {
		if op == "create" {
			return errors.New(kbSentinel)
		}
		return nil
	}
	imp := testImporter(fake, logger, false)
	// De wachtseam meldt "de context is weg" — precies wat sleepCtx doet als
	// Shutdown ertussen komt.
	imp.sleep = func(context.Context, time.Duration) bool { return false }

	st := importFixture(t, imp, newMosaicFixture())
	if st.State != ImportFailed || st.Reason != ReasonImportWriteFailed {
		t.Fatalf("status = %+v, wil failed:%s", st, ReasonImportWriteFailed)
	}
}

func TestCountsMatch_ReportsAMissingTable(t *testing.T) {
	logger, logged := captureLogs()
	imp := testImporter(&fakeKnowledge{}, logger, false)

	b := &kbBundle{KBID: "duiven-proef", Counts: map[string]int{tableThema: 3}}
	if imp.countsMatch(context.Background(), b) {
		t.Fatal("een tabel zonder geschreven knopen hoort een mismatch te zijn")
	}
	if !strings.Contains(logged(), tableThema) {
		t.Errorf("de afwijkende tabel hoort in het log te staan:\n%s", logged())
	}
}

func TestCountsMatch_UncountableEdgesCannotPublishCompleteMarker(t *testing.T) {
	logger, logged := captureLogs()
	fake := &fakeKnowledge{}
	writer := failingKnowledge{fakeKnowledge: fake, edgesErr: errors.New(kbSentinel)}
	imp := testImporter(writer, logger, false)

	if st := importFixture(t, imp, newMosaicFixture()); st.State != ImportFailed || st.Reason != ReasonImportCountMismatch {
		t.Fatalf("een mislukte controle mag geen complete marker opleveren: %+v", st)
	}
	if !strings.Contains(logged(), "niet te tellen") {
		t.Errorf("de mislukte telling hoort gelogd te worden:\n%s", logged())
	}
}

// Niet elke schrijffout is het proberen waard: een id dat al onder een andere
// tabel bestaat en een onleesbaar antwoord gaan bij poging drie precies zo mis,
// en kosten dan alleen tijd en nog twee embed-calls.
func TestRetryableWrite_OnlyRecoverableFailures(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"geen fout":       {nil, false},
		"provider 429":    {errors.New("429 too many requests"), true},
		"verbinding weg":  {errors.New("connection reset by peer"), true},
		"dubbele id":      {errors.New("fake: id already exists under a different table"), false},
		"onleesbare json": {&json.SyntaxError{}, false},
		"verkeerd type":   {&json.UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(0)}, false},
		"ingepakte json":  {fmt.Errorf("antwoord lezen: %w", &json.SyntaxError{}), false},
	}
	for naam, tc := range cases {
		t.Run(naam, func(t *testing.T) {
			if got := retryableWrite(tc.err); got != tc.want {
				t.Errorf("retryableWrite(%v) = %v, wil %v", tc.err, got, tc.want)
			}
		})
	}
}

// Een niet-herstelbare fout kost precies één poging.
func TestKBImport_DoesNotRetryAPermanentFailure(t *testing.T) {
	failer := &countingFailer{op: "create", fails: 99, err: errors.New("fake: id already exists under a different table")}
	imp, fake := newTestImporter(t)
	fake.failOn = failer.hook

	if st := importFixture(t, imp, newMosaicFixture()); st.State != ImportFailed {
		t.Fatalf("status = %+v, wil failed", st)
	}
	if failer.count() > imp.workers {
		t.Errorf("een blijvende fout hoort niet herhaald te worden, kreeg %d pogingen", failer.count())
	}
}
