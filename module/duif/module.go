package duif

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/audio"
	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/llm"
)

// One fixed model serves every language-model task: chat, facilitator and
// meeting detection. Different environment overrides fail at boot, so a
// deployment cannot drift to another model unnoticed.
//
// Lange stiltes vóór het eerste token komen door de vorm van de aanvraag, niet
// door de modelkeuze (vandaar tool_choice "auto"); wat overblijft, vangt de
// waakhond op het eerste token af.
const defaultChatModel = "vertex/gemini-3.6-flash-eu"
const defaultDetectModel = defaultChatModel

// chatReasoningEffort is het denkniveau van de drie chatroutes: facilitator,
// bemiddeling en het duif-antwoord. "low": meer denken kost in de chat vooral
// tijd tot het eerste woord, en de lezer ziet ondertussen waar de duif mee
// bezig is. De gedachtestroom zelf komt nergens terecht; geen enkele lezer in
// deze module kijkt naar StreamChunk.Reasoning of agent.ReasoningDelta.
const chatReasoningEffort = "low"

func chatReasoning() *string { effort := chatReasoningEffort; return &effort }

// meetingReasoningEffort is het denkniveau van de gesproken beurt aan de
// vergadertafel (routeMeeting). Daar schrijft de duif een inhoudelijke
// bijdrage, dus het denken is het werk en geen overhead; tegelijk moet de tafel
// realtime blijven. "low" legt het denkbudget vast, zodat een volgende
// modelstandaard de vergadertafel niet stilletjes naar medium of hoger trekt.
// Medium kan hier niet: daarmee sloot de gesproken beurt in metingen telkens
// op de tokengrens en werd de bijdrage kórter.
//
// De herformuleringen en de detectie hebben hun eigen knop
// (meetingQueryReasoningEffort, candidateReasoningEffort). Geen
// env-variabele: dit is een besluit, geen instelling.
const meetingReasoningEffort = "low"

func meetingReasoning() *string { effort := meetingReasoningEffort; return &effort }

// meetingQueryReasoningEffort is de uitzondering die de twee herformuleringen
// van de vergadertafel apart zet (routeMeetingQuery en routeCandidateQuery):
// zij denken niet. Hun uitvoer is één zoekregel die binnen meetingQueryTimeout
// klaar moet zijn, en op "low" ging bijna alle tijd naar redeneren over die
// ene regel. Verandert het model, meet dit dan opnieuw: een "none" dat
// stilletjes niets doet, kost hier meteen de hele route.
const meetingQueryReasoningEffort = "none"

func meetingQueryReasoning() *string { effort := meetingQueryReasoningEffort; return &effort }

// candidateReasoningEffort is het denkniveau van de detectie: de ene call van
// de kandidaatgenerator (routeCandidate, meeting_detect.go) die bepaalt óf en
// wanneer de zelfstandige duif iets inbrengt. Zij staat op "none", voor de
// snelheid.
//
// De naam volgt de route, net als meetingReasoningEffort (routeMeeting) en
// meetingQueryReasoningEffort (de twee query-routes). Niet "detect": het
// detectiemodel (detectModel, DUIF_DETECT_MODEL) draait ook de
// herformuleringen, en die hebben hun eigen knop.
//
// Let op wat deze call doet. Het is één model, één call: de kopregel
// PASS of SPEAK, en bij een pratende duif meteen de bijdrage die zij daarna
// letterlijk uitspreekt (speakCandidate, zonder nieuwe modelronde). Op "none"
// schrijft zij die zelfstandige bijdrage dus ook zonder te denken. De bijdrage
// via de knop (routeMeeting) denkt wel, op low. Op "none" gaf de detectie in
// een synthetische meting hetzelfde oordeel, veel sneller, met een bijdrage
// die niet korter werd.
const candidateReasoningEffort = "none"

func candidateReasoning() *string { effort := candidateReasoningEffort; return &effort }

// defaultMinSimilarity is de relevantiedrempel, gekalibreerd tegen de echte
// kennisbank met `make kb-eval` (eval/kb-eval.yaml). De embedder drukt alle
// scores samen, zodat beantwoordbare vragen en kennisgaten elkaar rond deze
// waarde raken; 0,73 is de onderkant van dat gelijkscorende bereik, omdat de
// facilitator zijn vraag tot de duif richt en die formulering iets lager
// scoort dan de mensvraag. Override: DUIF_MIN_SIMILARITY.
const defaultMinSimilarity = 0.73

// defaultHistoryWindow is het aantal historieberichten dat een turn meekrijgt.
const defaultHistoryWindow = 20

// defaultAgentHistoryWindow is hetzelfde venster voor de facilitator-motor.
// Ruimer, want een agent-beurt schrijft twee tot vier rijen (vraag, nul of
// meer duif-antwoorden, facilitator-tekst) waar de pipeline er twee schrijft:
// op twintig rijen zou pigeonHistoryWindow onbereikbaar zijn en zou het
// databaseschema stilzwijgend bepalen hoeveel gesprek de facilitator ziet.
const defaultAgentHistoryWindow = 3 * defaultHistoryWindow

// Module is de parai-core-module "duif". Construeer via New(); het framework
// roept Init/Routes/Assets aan.
type Module struct {
	access       *accessPolicy
	personas     *PersonaSet
	repo         ChatRepo
	llm          llm.Service
	answerStream answerStreamFunc // testseam; nil is altijd de echte modelcall
	embedder     llm.Embedder
	knowledge    data.KnowledgeRepo
	logger       *slog.Logger
	// transcriber is de streaming-STT van parai-core. Nil zonder
	// providersleutel: de vergadermodus draait dan zonder microfoonstroom.
	transcriber audio.RealtimeTranscriber
	// synthesizer is de streaming-TTS van parai-core. Nil zonder
	// providersleutel: de vergadering blijft dan tekst-only.
	synthesizer audio.Synthesizer
	// devVoice is de dev-only stem-id (DUIF_DEV_VOICE_ID) voor duiven die nog
	// de placeholder uit personas.yaml dragen.
	devVoice string
	// sttProvider/ttsProvider leggen vast welke aanbieder de vergaderaudio
	// verwerkt (providers.go). Leeg betekent: wat parai-core wired.
	sttProvider string
	ttsProvider string
	// ttsVoice is de stem waarmee beide pratende duiven op de Soniox-route
	// spreken — één stem, bewust, zonder keuze per duif. Alleen voor het
	// opstartlog: het is een publieke Soniox-stemnaam, geen providerinhoud en
	// geen sleutel.
	ttsVoice string
	// commitAfter is de DUIF_STT_COMMIT_SECONDS-override voor de brug; Set
	// onderscheidt "0 = uit" van "niet gezet".
	commitAfter    time.Duration
	commitAfterSet bool

	// meetings is de vergadermodus: eigen in-memory sessiebeheer,
	// bewust zónder repository — vergaderinhoud wordt nooit opgeslagen.
	meetings *meetingEngine

	// kbImport is de kennisbank-import: CAS-gate, status en de
	// achtergrond-goroutine. Nil zolang Init hem niet heeft gezet.
	kbImport *kbImporter

	// graph is de leesdienst van de kennisgraaf, één keer bij Init gebouwd.
	// Nil wanneer er geen kennisgraaf (met EdgeLister) gewired is: de
	// graafroutes antwoorden dan 503, na één waarschuwing bij het opstarten.
	graph *graphService

	// retrieval is de gedeelde embed → rag → gate → serialize-DAG van chat en
	// vergadering. Nil wanneer er geen embedder of geen kennisbank gewired is:
	// de pratende duiven melden dan kb_unavailable.
	retrieval *retrieval

	model         string
	modelEndpoint string // canonical, validated endpoint; never credentials
	detectModel   string
	minSimilarity float32
	historyWindow int
	// agentHistoryWindow is het historievenster van de facilitator-motor; zie
	// defaultAgentHistoryWindow voor waarom het ruimer is dan historyWindow.
	agentHistoryWindow int
	// agentMaxIter begrenst de tool-lus van de facilitator (injecteerbaar in
	// tests; zie defaultAgentMaxIter).
	agentMaxIter int
}

// New maakt een Module zonder gewirede dependencies (die zet Init).
func New() *Module {
	return &Module{
		model:         defaultChatModel,
		detectModel:   defaultDetectModel,
		minSimilarity: defaultMinSimilarity,
		historyWindow: defaultHistoryWindow,

		agentHistoryWindow: defaultAgentHistoryWindow,
		agentMaxIter:       defaultAgentMaxIter,
	}
}

func (m *Module) log() *slog.Logger { return logOr(m.logger) }

// logOr geeft de gewirede logger, of de default zolang die er niet is.
func logOr(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.Default()
}

// newMeetingEngine bouwt het vergadersessiebeheer uit de gewirede services.
// Merk op wat er niet in gaat: m.repo. De vergadermodus krijgt uitsluitend
// leesseams mee (niet-kunnen-persisteren als compile-eigenschap).
func (m *Module) newMeetingEngine() *meetingEngine {
	cfg := meetingConfigFromGedrag(m.personas.Gedrag)
	if m.commitAfterSet {
		cfg.CommitAfter = m.commitAfter
	}
	e := &meetingEngine{
		personas:     m.personas,
		llm:          m.llm,
		answerStream: m.answerStream,
		retrieval:    m.retrieval,
		clock:        systemClock{},
		logger:       m.logger,
		model:        m.model,
		detectModel:  m.detectModel,
		cfg:          cfg,
		transcriber:  m.transcriber,
		devVoice:     m.devVoice,
		sttSleep:     sleepCtx,
		sessions:     map[string]*meetingSession{},
	}
	// De TTS-seam wordt alleen gewired als er werkelijk een provider is; zonder
	// provider blijft e.voice nil en is elke sessie tekst-only (voice: "off").
	// De engine leest zelf geen omgeving: de dev-stem komt via de module mee.
	if m.synthesizer != nil {
		e.voice = newSynthesizerVoice(m.synthesizer, m.ttsFormat(), m.logger)
	}
	// De seam waar een doorgelaten TopicHit op landt: een spreekbeurt
	// met interventieframe voor de pratende duiven, een veldopname voor de
	// non-verbale duif.
	e.intervene = e.interveneNow
	return e
}

// internalError logt de echte fout en geeft een generieke 500-body terug
// (upstream-/interne fouttekst bereikt de wire nooit).
func (m *Module) internalError(wat string, err error) *turnError {
	m.log().Error("interne fout", "component", "duif", "operatie", wat, "error", err)
	return &turnError{status: http.StatusInternalServerError, body: apiError{
		Error:   "internal_error",
		Message: map[string]string{"en": "Something went wrong on our side.", "nl": "Er ging iets mis aan onze kant."},
	}}
}

// Name is de module-identifier; het framework mount routes onder /api/duif/.
func (m *Module) Name() string { return "duif" }

// Init captured de core-services en laadt + valideert de persona-config
// (fail-fast). Nil-services worden gelogd en geven operationele fouten,
// nooit een kennislacune of fabricatie.
func (m *Module) Init(core *parai.Core) error {
	if err := validateModelOverrides(); err != nil {
		return err
	}
	m.model, m.detectModel = defaultChatModel, defaultDetectModel
	personas, err := LoadEmbeddedPersonas()
	if err != nil {
		return fmt.Errorf("duif: persona-config: %w", err)
	}
	m.personas = personas
	m.llm = core.LLM
	m.embedder = core.Embedder
	m.knowledge = core.Knowledge
	m.logger = core.Logger
	// Symmetrisch met de synthesizer: een typed nil uit de core is géén
	// provider, en moet als nil binnenkomen — anders praat de module tegen
	// een lege interface aan.
	if !audio.IsRealtimeTranscriberNil(core.RealtimeTranscriber) {
		m.transcriber = core.RealtimeTranscriber
	}
	if !audio.IsSynthesizerNil(core.Synthesizer) {
		m.synthesizer = core.Synthesizer
	}
	if err := m.wirePrivateProviders(core); err != nil {
		return err
	}
	// De providerkeuze mag de gewirede seams vervangen (providers.go). Ze staat
	// hier, ná de core-services en vóór alle waarschuwingen hieronder, zodat
	// het opstartlog de werkelijk gekozen aanbieder meldt.
	if err := m.applyAudioProviders(); err != nil {
		return err
	}
	m.devVoice = os.Getenv("DUIF_DEV_VOICE_ID")

	if v := os.Getenv("DUIF_STT_COMMIT_SECONDS"); v != "" {
		// 0 zet de geforceerde commit uit; anders het aantal seconden dat één
		// uiting mag doorgroeien vóór de brug hem afsluit (meeting_stt.go).
		secs, err := strconv.Atoi(v)
		if err != nil || secs < 0 {
			return fmt.Errorf("duif: DUIF_STT_COMMIT_SECONDS=%q is geen geheel getal ≥ 0", v)
		}
		m.commitAfter = time.Duration(secs) * time.Second
		m.commitAfterSet = true
	}
	if v := os.Getenv("DUIF_MIN_SIMILARITY"); v != "" {
		f, err := strconv.ParseFloat(v, 32)
		if err != nil || f < 0 || f > 1 {
			return fmt.Errorf("duif: DUIF_MIN_SIMILARITY %q is geen waarde in [0,1]", v)
		}
		m.minSimilarity = float32(f)
	}

	if core.DB != nil {
		m.repo = newPostgresRepo(core.DB)
		m.access = newAccessPolicy(core.DB)
		if core.ModuleAuth != nil && recheckCookieAccess {
			m.access.wireAuthorizer(core.ModuleAuth.RequireModuleAccess("duif"))
		}
		if m.llm != nil {
			bounded := newBoundedLLM(m.llm)
			bounded.requiredModel = defaultChatModel
			// De duurmetriek van elke modelcall. core.Analytics is
			// altijd gezet — noop of persistent — dus dit is geen optie maar
			// de gewone wiring.
			bounded.tracker = core.Analytics
			m.llm = bounded
		}
	} else {
		return fmt.Errorf("duif: geen database gewired (chat-persistentie vereist Postgres)")
	}
	if m.llm == nil {
		m.log().Warn("geen LLM-service gewired: chat antwoordt 503", "component", "duif")
	}
	if m.transcriber == nil {
		m.log().Warn("geen streaming-STT gewired: de vergadermodus luistert niet mee", "component", "duif")
	}
	if m.synthesizer == nil {
		m.log().Warn("geen TTS gewired: vergadering tekst-only", "component", "duif")
	}
	// Waar de vergaderaudio naartoe stroomt hoort in het opstartlog te staan:
	// vergaderaudio wordt nergens bewaard, dus wie hem verwerkt is de vraag die
	// telt, en het antwoord hangt aan twee omgevingsvariabelen die niemand ziet.
	sttNaam, ttsNaam := m.audioProviders()
	// De stem hoort in dezelfde regel: hij is de tweede onzichtbare
	// omgevingsvariabele, en nu er één stem voor de pratende duiven gekozen is,
	// is "welke stem staat er aan" een vraag die beantwoord moet zijn voordat
	// iemand de duiven hoort praten.
	stemNaam, vangnet := m.audioVoice()
	m.log().Info("audioproviders gekozen", "component", "duif", "stt", sttNaam, "tts", ttsNaam, "stem", stemNaam)
	if vangnet {
		m.log().Warn("SONIOX_TTS_VOICE niet gezet: beide pratende duiven gebruiken de ingebouwde vangnetstem, niet de gekozen stem", "component", "duif", "stem", stemNaam)
	}
	if m.devVoice != "" {
		// Bewust zonder de id: het log mag hem niet dragen, en "dev" is hier
		// alleen een naam — gezet in productie praat élke duif met deze stem.
		m.log().Warn("DUIF_DEV_VOICE_ID actief: alle pratende duiven gebruiken de dev-stem", "component", "duif")
	}
	// Het spreektempo per duif is een Soniox-knop (`speed` in het
	// openingsbericht). Op de ElevenLabs-route kent audio.Synthesizer geen
	// tempo, dus daar doet `spreektempo` stilzwijgend niets: de configuratie
	// blijft staan, en wie hem las verwacht een verschil dat niet klinkt.
	if tempo := m.personasWithTempo(); len(tempo) > 0 && ttsNaam != providerSoniox {
		m.log().Warn("spreektempo genegeerd: deze TTS-route kent geen tempo per aanroep",
			"component", "duif", "tts", ttsNaam, "duiven", strings.Join(tempo, ","))
	}
	// Eén gedeelde retrieval-DAG voor beide pratende duiven. Ontbrekende
	// configuratie blijft nil en geeft kb_unavailable, nooit een corpusgat.
	if m.embedder != nil && m.knowledge != nil {
		r, err := newRetrieval(m.embedder, m.knowledge, m.minSimilarity, m.knowledgeStatus, m.logger)
		if err != nil {
			return err
		}
		m.retrieval = r
	} else {
		m.log().Warn("geen embedder/kb gewired: onderzoeksantwoorden onbeschikbaar", "component", "duif")
	}
	m.graph = m.newGraphService()
	m.warnOnMissingBundle(m.Assets())
	m.meetings = m.newMeetingEngine()
	m.meetings.maxSessions = 12
	m.meetings.maxUserSessions = 1
	// De kennisbank-import start als laatste: hij leunt op de
	// gewirede repo en embedder en loopt daarna op de achtergrond.
	m.initKnowledgeImport(core)
	return nil
}

// Shutdown (parai.ShutdownModule) breekt een lopende kennisbank-import af en
// wacht tot hij werkelijk gestopt is; een halve import laat geen marker achter
// en wordt bij de volgende boot hervat. De ctx begrenst het wachten: een
// importer die blijft hangen mag het afsluiten niet gijzelen.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.meetings != nil {
		m.meetings.shutdown()
	}
	m.shutdownKnowledgeImport(ctx)
	return nil
}

// warnOnMissingBundle meldt bij het opstarten dat de module-frontend niet
// gebouwd is. Zonder bundle mount de core-shell een leeg script en blijft de
// module onzichtbaar — een stille 404 in de browser is een slechtere melding
// dan een regel in het opstartlog. `make release` bouwt de bundle daarom vóór
// de binary.
//
// De assets komen als argument binnen in plaats van uit m.Assets(): welk
// bestandssysteem dit is hangt af van de buildtag (embed of os.DirFS), en die
// keuze hoort geen invloed te hebben op wat deze regel toetst.
func (m *Module) warnOnMissingBundle(assets fs.FS) {
	if assets != nil {
		if _, err := fs.Stat(assets, "bundle.js"); err == nil {
			return
		}
	}
	m.log().Warn("module-frontend niet gebouwd: /modules/duif/bundle.js ontbreekt (draai make bundle)", "component", "duif")
}

// Routes mount de API onder /api/duif/ (alles onder /v1). Auth komt van het
// framework: de server wrapt deze subrouter in RequireModuleAccess("duif") — de
// module schrijft geen middleware.
func (m *Module) Routes(r chi.Router, _ huma.API, _ huma.API) {
	r.Use(m.accessMiddleware)
	r.Get("/v1/access/notice", m.handleNotice)
	r.Post("/v1/access/notice", m.handleAcknowledgeNotice)
	r.Delete("/v1/chat/sessions/{id}", m.handleDeleteChat)
	r.Get("/v1/health", m.handleHealth)
	r.Get("/v1/duiven", m.handleDuiven)
	r.Get("/v1/sounds/{id}", m.handleSound)
	// De veldopnames zijn statische bestanden: een audiospeler mag ze eerst
	// met HEAD aftasten (ServeContent beantwoordt dat zonder body).
	r.Head("/v1/sounds/{id}", m.handleSound)
	r.Post("/v1/chat/turn", m.handleTurn)
	r.Post("/v1/chat/comparisons", m.handleComparison)
	r.Get("/v1/chat/sessions", m.handleSessions)
	r.Get("/v1/chat/sessions/{id}/messages", m.handleSessionMessages)

	if m.meetings == nil {
		m.meetings = m.newMeetingEngine()
	}
	m.knowledgeRoutes(r)
	m.graphRoutes(r)
	m.meetings.routes(r)
}

// Assets levert de embedded module-frontend.
func (m *Module) Assets() fs.FS {
	sub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		return nil
	}
	return sub
}

// Metadata stuurt de dashboard-weergave in de core-shell.
func (m *Module) Metadata() parai.ModuleMetadata {
	return parai.ModuleMetadata{
		Label:       map[string]string{"en": "Digital Pigeons", "nl": "Digitale Duiven"},
		Description: map[string]string{"en": "Three AI pigeon personas that give the city pigeon a voice.", "nl": "Drie AI-duifpersona's die de stadsduif een stem geven."},
		Icon:        "bird",
		Category:    parai.ModuleCategoryInteract,
	}
}

// PostgresMigrations levert de embedded migratiebundel voor chat-sessies en
// -berichten (chatdata; de regel "niets opslaan" gaat exclusief over
// vergaderaudio/-transcript en blijft onverkort gelden).
func (m *Module) PostgresMigrations() data.ModuleMigrations {
	return data.ModuleMigrations{Module: "duif", Schema: "duif", FS: migrationsFS, FSRoot: "migrations"}
}
