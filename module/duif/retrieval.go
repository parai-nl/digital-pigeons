package duif

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"sync"
	"text/template"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/pkg/interaction"
	"github.com/parai-nl/parai-core/pkg/pipeline"
)

// De retrieval-stap van één beurt, gebouwd op core's gedeelde bouwstenen:
// pkg/pipeline (de DAG) en pkg/interaction (EmbedQuery, RAGRetrieve,
// NewSerialize). Chat en vergadering delen deze ene ingang, zodat "geen bron →
// geen antwoord" op beide paden dezelfde code is en niet dezelfde
// bedoeling.
//
// Het verschil met de vorige eigen mini-RAG zit in wat er zichtbaar wordt:
// core's stappen slikken fouten bewust (RAGRetrieve logt een zoekfout en gaat
// met een lege lijst verder, EmbedQuery geeft bij een embed-fout nil zonder
// error). Een storing zou zo als kennisgat op het scherm komen — precies de
// verwarring die die regel moet uitsluiten. Daarom registreert deze laag de
// zoekfouten zelf en controleert ze na afloop of er werkelijk een vector was;
// wat overblijft is errKBUnavailable, een 503 met eigen reason-code, nooit een
// gap.

const (
	// retrieveTopK is het aantal kandidaten per tabel dat de vectorzoekopdracht
	// ophaalt (vóór de drempelfilter van RAGRetrieve). Het is tegelijk de
	// ContextDepth: elke bron die de gate kan doorlaten heeft zijn citaat,
	// perspectief en bronpaper nodig, dus verrijken we er precies evenveel.
	//
	// Vierentwintig is het bronnenplafond (maxAnswerSources, 20) plus een
	// kleine marge voor kandidaten die de gate alsnog op een harde fout laat
	// vallen; minder ophalen maakt dat plafond een papieren getal. Verrijken
	// gebeurt pas ná de drempelfilter, dus de breedte kost alleen graafwerk
	// voor kandidaten die de lat al haalden. Chat en vergadering delen haar.
	retrieveTopK = 24
	// meetingMinSimilarity is de bronnendrempel van de vergadertafel: de
	// spreekknop (meeting_turn.go) en de kandidaatgenerator
	// (meeting_detect.go). De chat houdt defaultMinSimilarity (0,73,
	// DUIF_MIN_SIMILARITY).
	//
	// Waarom lager: 0,73 is gekalibreerd op een gestélde vraag ("hoe bouwen
	// jullie je nest?"), waar een lage score een kennisgat betekent en een
	// eerlijk "dat weet ik niet" het goede antwoord is. Aan de vergadertafel
	// stelt niemand een vraag aan de kennisbank. Daar ligt een zin over
	// vuurwerk op het plein, en de vraag is of er onderzoek is dat eráán
	// grenst. Een gespreksvenster van meerdere sprekersbeurten is bovendien
	// veel breder dan één vraag, en dat drukt elke cosinusscore.
	//
	// 0,65: daar komt aangrenzend onderzoek door en blijft een gesprek zonder
	// duiven leeg; op 0,73 zwijgt de tafel juist over de onderwerpen waarvoor
	// zij bedoeld is, en Nederlandstalige vensters scoren tegen de Engelstalige
	// kennisbank nog lager. De meting staat in
	// TestLive_MeetingCandidateThreshold (eval/meeting-conversations.v1.yaml);
	// de negatieve controle is daar het getal om in de gaten te houden bij een
	// volgende kennisbundel.
	meetingMinSimilarity = 0.65
	// relatedMinSimilarity is de lat van de verwante zoekactie ná een kennisgat
	// in de chat (agent_tools.go). Zelfde getal als de vergaderlat,
	// om een andere reden — en dat is waarom het twee constanten zijn: de tafel
	// ligt lager omdat een gespreksvenster elke cosinusscore drukt, deze
	// zoekactie omdat zij een andere vraag stelt. Niet "wat beantwoordt dit?"
	// maar "wat grenst hieraan?", en op de gewone lat zou zij per definitie
	// niets opleveren: precies dezelfde bronnen als de zoekopdracht die er
	// zojuist niets vond. Schuift de ene lat, dan hoeft de andere niet mee.
	// Het getal zelf leunt op dezelfde kalibratie als de vergaderlat.
	relatedMinSimilarity = 0.65
	// ragTemplatePath is het eigen RAG-sjabloon binnen de moduleconfig.
	ragTemplatePath = "config/prompts/rag.tmpl"
)

// KnowledgeReader is de enige seam naar de kennisgraaf voor een beurt:
// uitsluitend zoeken en lezen wat core's RAG-stap zelf nodig heeft.
// Interface-segregatie in plaats van discipline — de vergadermodus kán hiermee
// niet schrijven, en de reflectietoets in
// meeting_privacy_test.go bewaakt dat.
//
// Generic search/context remains the required seam. Corpus quote grounding
// additionally uses the optional public read-only quoteMetadataReader API; no
// private framework source or write methods are imported into the meeting path.
type KnowledgeReader interface {
	interaction.RAGSearcher
}

// turnState is de pipeline-state van één beurt: core's gedeelde velden plus
// wat deze module er zelf bij houdt. De gedeelde stappen bereiken hem via
// Interaction(); de eigen gate-stap type-asserteert op *turnState.
type turnState struct {
	interaction.InteractionState

	// status is de uitkomst van de gate: sources of gap.
	status KBStatus
	// sources is de wire-DTO, afgeleid van RAGSources ná de gate — dezelfde
	// nummering als de RAG-sectie in de prompt.
	sources []Source
	// drops telt de kandidaten die de gate liet vallen, per reden.
	// Vroeger verdwenen zij met hoogstens een Warn in het log.
	drops map[string]int
}

// Interaction maakt turnState bruikbaar voor core's gedeelde stappen.
func (s *turnState) Interaction() *interaction.InteractionState { return &s.InteractionState }

// reasonPartialSearch is een interne logcode: één van de twee zoektabellen viel
// om terwijl er bruikbare bronnen overbleven. De client merkt er niets van.
const reasonPartialSearch = "partial_search_failure"

// retrievalResult is wat één retrieval-run oplevert voor de beurt eromheen.
type retrievalResult struct {
	Status     KBStatus
	Sources    []Source
	RAGSection string
	Related    bool // alternatieve zoekkandidaten, geen dekking van de oorspronkelijke vraag
	// Dropped is wat er onderweg wegviel: gevonden kandidaten die alsnog geen
	// bron werden, plus een halve zoekopdracht. Nil betekent dat er
	// niets wegviel. Hij hoort bij déze beurt en wordt niet opgeslagen.
	Dropped *SourceDrops
}

// searchRecorder houdt vast wat de zoekopdracht van één beurt onderweg
// oplevert: de laatste fout, en de rauwe props van elke kandidaat. Die props
// zijn de reden dat de gate geen enkele extra query hoeft te doen — ze zijn al
// gelezen, embedding en al, en een tweede GetNode per bron zou dezelfde ± 25 KB
// nog eens over de lijn halen. RAGRetrieve zoekt in twee goroutines tegelijk
// (destillaat en quote), vandaar de mutex.
type searchRecorder struct {
	mu            sync.Mutex
	err           error
	props         map[data.NodeID]map[string]any
	failedContext map[data.NodeID]bool
}

func (r *searchRecorder) contextFailed(id data.NodeID) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failedContext[id]
}

func (r *searchRecorder) rejectContext(id data.NodeID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failedContext == nil {
		r.failedContext = map[data.NodeID]bool{}
	}
	r.failedContext[id] = true
	r.err = errGroundingUnavailable
}

func (r *searchRecorder) record(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *searchRecorder) last() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// keep onthoudt de props van de gevonden kandidaten.
func (r *searchRecorder) keep(results []data.SemanticResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.props == nil {
		r.props = make(map[data.NodeID]map[string]any, len(results))
	}
	for _, res := range results {
		if res.Node != nil {
			r.props[res.ID] = res.Node
		}
	}
}

// node geeft de props van één kandidaat, of nil. De nil-receiver hoort erbij:
// de gate draait ook zonder recorder in de context (een directe aanroep in een
// test), en dat is geen reden om om te vallen.
func (r *searchRecorder) node(id data.NodeID) map[string]any {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.props[id]
}

// searchRecorderKey draagt de opvang van één beurt door de DAG heen. De
// pipeline wordt één keer bij Init gebouwd en door alle gelijktijdige beurten
// gedeeld; de opvang is per beurt, en de context is de enige weg die de
// gedeelde RAGRetrieve-stap openlaat. Zonder deze scheiding zou de KB-storing
// van de ene gebruiker de beurt van de andere als onbereikbaar markeren.
//
// De afweging: dit kan weg door de DAG per beurt te bouwen in plaats van bij
// Init. Dan is elke stap een gesloten waarde en verdwijnt de contexttruc — maar
// dan verhuizen ook de sjabloonfout en de DAG-validatie van het opstarten naar
// de eerste vraag van een gebruiker, en dat is de slechtere ruil: een bouwfout
// hoort de boot te laten falen, niet één beurt.
type searchRecorderKey struct{}

// recordingSearcher remembers search and evidence-context failures per turn.
// A failed context read must not be mistaken for readable legacy evidence.
type recordingSearcher struct {
	inner KnowledgeReader
}

func (s recordingSearcher) SemanticSearch(ctx context.Context, table string, vector []float32, k int) ([]data.SemanticResult, error) {
	res, err := s.inner.SemanticSearch(ctx, table, vector, k)
	rec, _ := ctx.Value(searchRecorderKey{}).(*searchRecorder)
	if rec == nil {
		return res, err
	}
	if err != nil {
		rec.record(err)
		return res, err
	}
	rec.keep(res)
	return res, err
}

// De typed reads krijgen er één vangrail bij. Core's RAGRetrieve leest de
// teruggegeven pointer zodra de fout nil is; een verdwenen node (nil, nil —
// wat de repo teruggeeft als de rij er niet meer is) laat die stap dan op een
// nil-pointer lopen. Tussen de zoekopdracht en de verrijking kan een node
// weggeschreven of verwijderd zijn, bijvoorbeeld door een herimport. Een nette
// fout laat core netjes degraderen; het is niet aan de gate om een paniek op te
// vangen.
var errNodeGone = errors.New("duif: node verdween tussen zoeken en lezen")

func (s recordingSearcher) GetDestillaat(ctx context.Context, id data.NodeID) (*data.Destillaat, error) {
	d, err := s.inner.GetDestillaat(ctx, id)
	if err == nil && d == nil {
		return nil, errNodeGone
	}
	return d, err
}

func (s recordingSearcher) GetQuote(ctx context.Context, id data.NodeID) (*data.Quote, error) {
	q, err := s.inner.GetQuote(ctx, id)
	if err == nil && q == nil {
		return nil, errNodeGone
	}
	return q, err
}

func (s recordingSearcher) GetWithContext(ctx context.Context, id data.NodeID) (*data.DestillaatContext, error) {
	c, err := s.inner.GetWithContext(ctx, id)
	if err == nil && c == nil {
		err = errNodeGone
	}
	if err != nil {
		// Core can still return the editorial summary after this failure. It
		// is not a legacy passage: this candidate could not be checked at all.
		rec, _ := ctx.Value(searchRecorderKey{}).(*searchRecorder)
		rec.rejectContext(id)
	}
	return c, err
}

// Core's logs may contain echoed input, error details or arbitrary attributes.
// Keep only a fixed diagnostic event, at every level including Debug. Neither
// WithAttrs nor WithGroup may sneak content around the record redaction.
type redactedStepHandler struct{ inner slog.Handler }

func (h redactedStepHandler) Enabled(ctx context.Context, _ slog.Level) bool {
	return h.inner.Enabled(ctx, slog.LevelDebug)
}
func (h redactedStepHandler) Handle(ctx context.Context, r slog.Record) error {
	safe := slog.NewRecord(r.Time, slog.LevelDebug, "retrieval step reported a diagnostic", 0)
	safe.AddAttrs(slog.String("component", "duif"), slog.String("reason", "retrieval_step_diagnostic"))
	return h.inner.Handle(ctx, safe)
}
func (h redactedStepHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h redactedStepHandler) WithGroup(string) slog.Handler      { return h }
func stepLogger(l *slog.Logger) *slog.Logger {
	return slog.New(redactedStepHandler{inner: logOr(l).Handler()})
}

// EmbedQuery uses the global logger instead of the supplied step logger. Bound
// its error before calling that shared step; the original input/vector stays
// transient, and the retrieval result still reports kb_unavailable.
type redactedRetrievalEmbedder struct{ inner llm.Embedder }

func (e redactedRetrievalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	v, err := e.inner.Embed(ctx, text)
	if err != nil {
		return nil, errKBUnavailable
	}
	return v, nil
}

// retrieval is de gebouwde DAG plus wat de eigen gate-stap nodig heeft. Hij
// bestaat alleen wanneer embedder én kennisbank gewired zijn; ontbreekt er een,
// dan is dit veld nil en geeft run kb_unavailable.
type retrieval struct {
	// reader is de leesseam waarop de DAG gebouwd is. Hij wordt na newRetrieval
	// niet meer aangeroepen — de gedeelde RAG-stap houdt hem vast — maar blijft
	// als veld staan: dit is de plek waar zwart op wit staat dat een beurt de
	// kennisgraaf alleen léést, en de reflectietoets in meeting_privacy_test.go
	// loopt er langs om te bewijzen dat er geen schrijfmethode achter zit.
	reader KnowledgeReader
	// meeting is dezelfde DAG op de lat van de vergadertafel
	// (meetingMinSimilarity), voor de spreekknop en de kandidaatgenerator.
	meeting *pipeline.Pipeline
	// related is dezelfde DAG op relatedMinSimilarity, voor de verwante
	// zoekactie ná een kennisgat.
	related *pipeline.Pipeline
	// status geeft de importstand, zodat de gate weet welke kennisbank actief
	// is: standpunten van een vórige kennisbank staan er met embedding en al
	// nog in, terwijl de graaf ze als 404 verbergt.
	status func() importStatus
	logger *slog.Logger
	pipe   *pipeline.Pipeline
}

func (r *retrieval) log() *slog.Logger { return logOr(r.logger) }

// newRetrieval bouwt de DAG embed → rag → gate → serialize. Alle
// constructiefouten (sjabloon, DAG-validatie) komen hier naar boven — één keer
// bij Init, niet bij de eerste vraag van een gebruiker.
func newRetrieval(embedder llm.Embedder, reader KnowledgeReader, minSimilarity float32, status func() importStatus, logger *slog.Logger) (*retrieval, error) {
	if embedder == nil || reader == nil {
		return nil, fmt.Errorf("duif: retrieval vereist een embedder en een kennisbank")
	}
	ragTmpl, err := loadRAGTemplate()
	if err != nil {
		return nil, err
	}
	// De persona-sectie blijft leeg: deze module kiest zijn duif zelf uit
	// personas.yaml en zet nooit een SelectedPersona in de state. NewSerialize
	// eist het sjabloon wel, dus het is er — en rendert niets.
	personaTmpl, err := template.New("persona").Parse("")
	if err != nil {
		return nil, fmt.Errorf("duif: persona-sjabloon: %w", err)
	}
	serialize, err := interaction.NewSerialize(interaction.SerializeConfig{
		PersonaTemplate: personaTmpl,
		RAGTemplate:     ragTmpl,
	})
	if err != nil {
		return nil, fmt.Errorf("duif: serialize-stap: %w", err)
	}

	r := &retrieval{reader: reader, status: status, logger: logger}
	// topK is hoeveel kandidaten de vectorzoekopdracht ophaalt; minSim is de
	// lat waar ze overheen moeten. De verrijkingsdiepte volgt topK, want zij
	// bepaalt niet hoeveel bronnen er doorkomen maar wélke er kúnnen
	// doorkomen: core geeft alleen de eerste ContextDepth kandidaten hun
	// citaat, perspectief en bronpaper, en de gate laat een v2-bron zónder
	// oorspronkelijke passages niet door (gate.go). Stond de diepte vast op
	// acht, dan kon kandidaat negen tot en met vierentwintig van de brede
	// zoekopdracht nooit bron worden, hoe hoog hij ook scoorde — de bredere
	// lat leverde dan wel meer kandidaten en niet meer bronnen. Core verrijkt
	// pas ná de lat (filterBySimilarity), dus dit kost alleen graafwerk voor
	// kandidaten die de lat al haalden.
	build := func(topK int, minSim float64) (*pipeline.Pipeline, error) {
		return pipeline.New(
			pipeline.Node{Name: "embed", Execute: interaction.EmbedQuery(redactedRetrievalEmbedder{inner: embedder})},
			pipeline.Node{Name: "rag", Deps: []string{"embed"}, Execute: interaction.RAGRetrieve(interaction.RAGConfig{
				Knowledge:     recordingSearcher{inner: reader},
				TopK:          topK,
				ContextDepth:  topK,
				MinSimilarity: minSim,
				Logger:        stepLogger(logger),
			})},
			pipeline.Node{Name: "gate", Deps: []string{"rag"}, Execute: r.gateStep},
			pipeline.Node{Name: "serialize", Deps: []string{"gate"}, Execute: serialize},
		)
	}
	pipe, err := build(retrieveTopK, float64(minSimilarity))
	if err != nil {
		return nil, fmt.Errorf("duif: retrieval-pipeline: %w", err)
	}
	// Twee routes met een eigen lat, om twee redenen (zie de constanten): de
	// vergadertafel en de verwante zoekactie ná een kennisgat. Dat het nu
	// hetzelfde getal is, maakt er geen één zoekopdracht van — schuift de
	// kalibratie van de tafel, dan blijft "wat grenst hieraan" staan.
	//
	// De min() houdt beide nooit stríkter dan de gewone zoekopdracht: zet
	// iemand DUIF_MIN_SIMILARITY op 0,60, dan is de tafel 0,60 en niet 0,65,
	// en zou "verwant" anders minder opleveren dan de zoekopdracht die er net
	// niets vond.
	meeting, err := build(retrieveTopK, math.Min(float64(minSimilarity), meetingMinSimilarity))
	if err != nil {
		return nil, fmt.Errorf("duif: retrieval-pipeline voor de vergadertafel: %w", err)
	}
	related, err := build(retrieveTopK, math.Min(float64(minSimilarity), relatedMinSimilarity))
	if err != nil {
		return nil, fmt.Errorf("duif: retrieval-pipeline voor verwante bronnen: %w", err)
	}
	r.pipe, r.meeting, r.related = pipe, meeting, related
	return r, nil
}

// loadRAGTemplate leest het eigen RAG-sjabloon uit de embedded moduleconfig.
func loadRAGTemplate() (*template.Template, error) {
	raw, err := fs.ReadFile(configFS, ragTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("duif: rag-sjabloon laden: %w", err)
	}
	tmpl, err := template.New("rag").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("duif: rag-sjabloon parsen: %w", err)
	}
	return tmpl, nil
}

// run draait de DAG voor één vraag en levert de gate-uitkomst.
//
// Drie takken geven errKBUnavailable — de kennisbank was er niet, dus er is
// geen grond om iets over te zeggen én geen kennisgat om te melden:
//  1. na de embed-stap is er geen vector (de embedder faalde; core's stap
//     meldt dat niet als fout);
//  2. er is een zoekfout gezien én er kwam geen enkele bron terug (met bronnen
//     is één gevallen tabel geen storing maar een gedeeltelijke uitkomst);
//  3. de DAG zelf viel om.
func (r *retrieval) run(ctx context.Context, query string) (retrievalResult, error) {
	return r.runWith(ctx, query, laneDefault)
}

// runMeeting is dezelfde DAG op de lat van de vergadertafel
// (meetingMinSimilarity): een gespreksvenster is geen gestelde vraag.
func (r *retrieval) runMeeting(ctx context.Context, query string) (retrievalResult, error) {
	return r.runWith(ctx, query, laneMeeting)
}

// runRelated is dezelfde DAG op relatedMinSimilarity: niet "wat beantwoordt
// dit?" maar "wat grenst hieraan?".
func (r *retrieval) runRelated(ctx context.Context, query string) (retrievalResult, error) {
	return r.runWith(ctx, query, laneRelated)
}

// retrievalLane kiest welke van de gebouwde DAG's een zoekopdracht draait. Ze
// verschillen alleen in hun lat; de breedte (retrieveTopK) en de stappen zijn
// overal gelijk.
type retrievalLane int

const (
	laneDefault retrievalLane = iota // chat en spreekknop-in-chat: DUIF_MIN_SIMILARITY
	laneMeeting                      // de vergadertafel: meetingMinSimilarity
	laneRelated                      // "wat grenst hieraan" ná een kennisgat: relatedMinSimilarity
)

// runWith draait de DAG van de gevraagde baan.
func (r *retrieval) runWith(ctx context.Context, query string, lane retrievalLane) (retrievalResult, error) {
	if r == nil {
		// Zonder retrieval is niets onderzocht. Dat is onbeschikbaarheid,
		// niet een inhoudelijke uitspraak over wat het corpus bevat.
		return retrievalResult{}, errKBUnavailable
	}
	if r.importUnavailable() {
		return retrievalResult{}, errKBUnavailable
	}
	rec := &searchRecorder{}
	ctx = context.WithValue(ctx, searchRecorderKey{}, rec)

	st := &turnState{}
	st.Content = query
	st.RequestCtx = ctx

	pipe := r.pipe
	switch {
	case lane == laneMeeting && r.meeting != nil:
		pipe = r.meeting
	case lane == laneRelated && r.related != nil:
		pipe = r.related
	}
	_, err := pipe.Run(ctx, st)
	if r.importUnavailable() {
		return retrievalResult{}, errKBUnavailable
	}
	searchErr := rec.last()
	log := r.log()
	switch {
	case err != nil:
		log.Error("retrieval-pipeline viel om", "component", "duif", "reason", reasonRetrievalFailed)
		return retrievalResult{}, errKBUnavailable
	case st.QueryEmbedding == nil:
		log.Error("geen query-embedding: kennisbank onbereikbaar", "component", "duif", "reason", ReasonKBUnavailable)
		return retrievalResult{}, errKBUnavailable
	case searchErr != nil && len(st.sources) == 0:
		log.Error("kb-zoekopdracht faalde zonder bronnen", "component", "duif", "reason", ReasonKBUnavailable)
		return retrievalResult{}, errKBUnavailable
	}
	if searchErr != nil {
		// Eén gevallen tabel naast bruikbare bronnen: gemeld, niet fataal — maar
		// wel zichtbaar. De lezer weet dan dat deze beurt op een
		// halve zoekopdracht staat, ook al staan er bronnen onder.
		log.Warn("kb-zoekopdracht deels gefaald", "component", "duif", "reason", reasonPartialSearch, "bronnen", len(st.sources))
		st.drop(dropPartialSearch)
	}
	return retrievalResult{
		Status: st.status, Sources: st.sources, RAGSection: st.RAGSection,
		Dropped: dropsOf(st.drops),
	}, nil
}

// Een half vervangen graaf kan geen antwoord dragen. Een bekende
// failed/incomplete/importing-stand is een storing, gelijk aan de
// beschikbaarheidspoort van de graaf.
//
// ImportEmpty hoort in dezelfde rij. Een lege of nooit geïmporteerde
// kennisbank is niets onderzocht, geen onderzocht niets: er is geen corpus
// waarover een kennisgat iets kan zeggen. Stond `empty` hier niet, dan kwam een
// installatie zonder import als inhoudelijk kennisgat op het scherm — precies
// wat niet mag: een onbereikbare kennisbank is geen inhoudelijke
// kennislacune.
//
// Het gevolg reikt verder dan de chat: meeting_turn.go en meeting_detect.go
// lezen dezelfde fout en melden ReasonKBUnavailable in plaats van een stille
// gap-beurt.
func (r *retrieval) importUnavailable() bool {
	if r.status == nil {
		return false
	}
	switch r.status().State {
	case ImportEmpty, ImportImporting, ImportIncomplete, ImportFailed:
		return true
	}
	return false
}
