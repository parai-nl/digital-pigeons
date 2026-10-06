package duif

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	parai "github.com/parai-nl/parai-core"
)

// Omgevingsvariabelen van de import. Het bundelpad en FORCE bestaan bewust
// alleen als env: een vervangende import mag nooit via HTTP te starten zijn.
const (
	envImportKB      = "DUIF_IMPORT_KB"
	envImportKBForce = "DUIF_IMPORT_KB_FORCE"
	envImportWorkers = "DUIF_IMPORT_WORKERS"
)

// Reden-codes waarom er niets te importeren viel. De eerste twee halen de
// wire: ze staan als `reason` bij de importstatus, want een lege kennisbank
// zonder uitleg is niet van een nog-te-laden kennisbank te onderscheiden.
const (
	ReasonImportNoKnowledge = "no_knowledge_repo"
	ReasonImportNoEmbedder  = "no_embedder"
)

// reasonInvalidWorkers en reasonForceImport zijn log-only: ze zeggen iets over
// hoe deze installatie gestart is, niet over de stand van de kennisbank.
const (
	reasonInvalidWorkers = "invalid_workers"
	reasonForceImport    = "force_import"
)

// bootStatusTimeout begrenst de statuslezing bij het opstarten: twee
// begrensde queries mogen de boot nooit laten hangen.
const bootStatusTimeout = 10 * time.Second

// initKnowledgeImport wiret de kennisbank-import en bepaalt de stand bij boot.
// Zonder DUIF_IMPORT_KB gebeurt er niets meer dan die ene lezing: de status
// vertelt dan wat er al in de graaf staat (ready, incomplete of empty).
func (m *Module) initKnowledgeImport(core *parai.Core) {
	path := strings.TrimSpace(os.Getenv(envImportKB))
	if m.knowledge == nil {
		// Zonder kennisgraaf is er niets te importeren én niets te melden;
		// knowledgeStatus geeft dan "empty".
		if path != "" {
			m.log().Warn("kennisbank-import overgeslagen", "component", "duif", "reason", ReasonImportNoKnowledge)
		}
		return
	}

	// Het embeddingmodel en de dimensie komen uit de core-config en gaan mee in
	// de marker: een import met een andere embedder levert onvergelijkbare
	// vectoren en moet zichtbaar blokkeren in plaats van te mengen.
	var model string
	var dimension int
	if core != nil && core.Config != nil {
		model = core.Config.Knowledge.EmbeddingModel
		dimension = core.Config.Knowledge.EmbeddingDimension
	}

	force := os.Getenv(envImportKBForce) == "1"
	if force {
		// FORCE wist de hele kennisbank vóór hij hem opnieuw schrijft. Dat is
		// precies de bedoeling bij een nieuwe bundel, en een ongeluk bij een
		// herstart met een oude env — één regel in het opstartlog maakt het
		// verschil zichtbaar vóór de eerste Delete.
		m.log().Warn("kennisbank-import vervangt de bestaande kennisbank", "component", "duif", "reason", reasonForceImport)
	}
	imp := newKBImporter(m.knowledge, m.logger, m.importWorkers(), force, model, dimension)
	if m.graph != nil {
		// De graaf bewaart de kinderlijst van een ouder kort; een import
		// schrijft of wist juist die rijen. Init bouwt de graaf vóór deze
		// wiring, dus de haak zit er vanaf de eerste import op.
		imp.onImportDone = m.graph.invalidateChildCache
	}
	m.kbImport = imp

	ctx, cancel := context.WithTimeout(context.Background(), bootStatusTimeout)
	imp.loadBootStatus(ctx)
	cancel()

	if path == "" {
		return
	}
	if m.embedder == nil {
		// Zonder embedder schrijft CreateNodeWithID knopen zonder vector; de
		// standpunten zouden dan onvindbaar in de graaf staan.
		m.log().Warn("kennisbank-import overgeslagen", "component", "duif", "reason", ReasonImportNoEmbedder)
		return
	}
	imp.start(context.Background(), path)
}

// importWorkers leest DUIF_IMPORT_WORKERS. Een onbruikbare waarde is een
// waarschuwing met reden-code, geen bootfout: de import is een extra, niet de
// kern van de module.
func (m *Module) importWorkers() int {
	v := strings.TrimSpace(os.Getenv(envImportWorkers))
	if v == "" {
		return defaultImportWorkers
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 64 {
		m.log().Warn("DUIF_IMPORT_WORKERS genegeerd", "component", "duif",
			"reason", reasonInvalidWorkers, "default", defaultImportWorkers)
		return defaultImportWorkers
	}
	return n
}

// knowledgeRoutes mount de statusroute. Strikt GET: er is geen HTTP-weg om
// een import te starten of te forceren.
func (m *Module) knowledgeRoutes(r chi.Router) {
	r.Get("/v1/knowledge/status", m.handleKnowledgeStatus)
}

// shutdownKnowledgeImport stopt een lopende import en wacht tot de goroutine
// weg is. Zonder marker herhaalt de volgende boot hem gewoon, dus afbreken is
// veilig; wachten is het wél waard, want een schrijver die na Shutdown nog
// doorloopt schrijft in een database die het proces al opgeeft.
func (m *Module) shutdownKnowledgeImport(ctx context.Context) {
	if m.kbImport != nil {
		m.kbImport.stop(ctx)
	}
}
