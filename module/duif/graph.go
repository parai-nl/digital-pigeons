package duif

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/parai-nl/parai-core/data"
)

// De kennisgraaf-view. Drie read-only routes op de gedeelde
// kennisgraaf van core: een overzicht van het structuurniveau, één niveau
// dieper per klik, en één node met zijn voorouderspoor.
//
// Twee regels beheersen dit bestand:
//
//  1. Nooit unbounded lezen. Een kennisknoop draagt de embedding (~25 KB per
//     standpunt) mee in elke rij; de graaf leest daarom in pagina's van 200 en
//     haalt kinderen per klik op, met een cap.
//  2. Nooit meer op de wire dan een allowlist. De kennisgraaf is gedeeld met
//     andere modules en draagt props die er niet uit mogen (`storage_key`,
//     `embedding`, `content_hash`, `import_hash`). Alleen wat in
//     graphMetadata staat gaat de deur uit — een nieuwe prop lekt dus niet
//     vanzelf mee.

// graphFault is een graaffout onderweg: status + reden-code, meer niet. De
// onderliggende fout wordt gelogd waar hij ontstaat, nooit meegedragen.
type graphFault struct {
	status int
	reason string
}

func (f *graphFault) Error() string { return f.reason }

func graphErr(status int, reason string) *graphFault {
	return &graphFault{status: status, reason: reason}
}

var (
	errGraphNotFound    = graphErr(http.StatusNotFound, ReasonNodeNotFound)
	errGraphUnavailable = graphErr(http.StatusServiceUnavailable, ReasonGraphUnavailable)
)

// graphTables is de tabel-allowlist: een id met een andere tabel is een 404,
// nog vóór er een query loopt.
//
// Het perspectief en de redactionele persona staan er bewust niet in.
// Een perspectief is een redactionele groepering van standpunten, geen bron en
// geen ordening die de lezer iets vertelt; op de kaart maakte het elk thema
// drukker zonder er iets aan toe te voegen. De persona hangt alleen via
// perspectieven aan de graaf en zou zonder hen een lege wortel zijn. Beide
// blijven in de kennisbank staan — alleen de graaf toont ze niet.
var graphTables = map[string]bool{
	tableDomein:     true,
	tableThema:      true,
	tableSubthema:   true,
	tableInterview:  true,
	tableDestillaat: true,
	tableQuote:      true,
}

// graphStructureTables is het structuurniveau van de overview, in de volgorde
// waarin de nodes worden opgehaald (het antwoord zelf sorteert op id).
var graphStructureTables = []string{tableDomein, tableThema, tableSubthema}

// graphStructureTable is dezelfde verzameling als lidmaatschapstoets: is dit
// id een node uit de structuurlaag, en dus een node die de overview zelf al
// gelezen heeft?
var graphStructureTable = func() map[string]bool {
	m := make(map[string]bool, len(graphStructureTables))
	for _, t := range graphStructureTables {
		m[t] = true
	}
	return m
}()

// graphLabelProps zegt welke prop het label van een node draagt. Destillaat en
// quote staan er niet in: hun label is een afgekapte tekst (zie graphLabel).
var graphLabelProps = map[string]string{
	tableDomein:    "titel",
	tableThema:     "titel",
	tableSubthema:  "titel",
	tableInterview: "title",
}

// metaField koppelt een wire-veld aan de prop waar het uit komt. De twee namen
// verschillen alleen waar de wire duidelijker is dan de kolom (destillaat:
// `text` is de volledige samenvatting, waarvan het label de afgekapte vorm is).
type metaField struct {
	wire, prop string
	// localized zegt dat dit veld in duiven/3 twee talen draagt en dus als
	// taalobject de wire op gaat in plaats van als kale string. Een citaat, een
	// papertitel en een opsommingssleutel staan er bewust niet op.
	localized bool
}

// graphMetadata is de veld-allowlist per tabel: dit en niets anders komt in
// `metadata`. `storage_key`, `embedding`, `content_hash` en `import_hash`
// staan er bewust niet in en kunnen er ook niet in glippen — graphNodeOf kopieert
// uitsluitend deze sleutels.
var graphMetadata = map[string][]metaField{
	tableDomein:   {{wire: "beschrijving", prop: "beschrijving", localized: true}},
	tableThema:    {{wire: "beschrijving", prop: "beschrijving", localized: true}},
	tableSubthema: {{wire: "beschrijving", prop: "beschrijving", localized: true}},
	tableInterview: {
		{wire: "interview_id", prop: "interview_id"},
		{wire: "citation", prop: "citatie"},
		{wire: "doi", prop: "doi"},
		{wire: "citationConfidence", prop: "citatie_confidence"},
		{wire: "citationMissing", prop: "citatie_ontbreekt"},
	},
	tableDestillaat: {
		// De persona van een standpunt staat er niet bij: die laag toont de
		// graaf niet.
		{wire: "text", prop: "samenvatting", localized: true},
		{wire: "domain", prop: "domain"},
		{wire: "scope", prop: "scope"},
		{wire: "thema_titel", prop: "thema_titel", localized: true},
	},
	tableQuote: {{wire: "text", prop: "text"}, {wire: "speaker", prop: "speaker"}},
}

// graphStep is één stap door de graaf: een relatie en de richting waarin hij
// vanaf een node gelezen wordt. `incoming` betekent dat de edge naar die node
// toe wijst (een standpunt wijst naar zijn subthema, een interview naar zijn
// standpunten). Omhoog en omlaag is dezelfde vraag met een andere regel.
type graphStep struct {
	relation string
	incoming bool
}

// graphChildRules is de laag-voor-laag-structuur van de graaf: per tabel wat
// "één niveau dieper" betekent. Een quote staat er niet in — dat is het blad.
var graphChildRules = map[string][]graphStep{
	tableDomein:    {{relation: relContainsThema}},
	tableThema:     {{relation: relContainsSubthema}},
	tableSubthema:  {{relation: relBelongsToSubthema, incoming: true}},
	tableInterview: {{relation: data.RelContains}},
	// Het standpunt heeft twee soorten kinderen: zijn citaten, en het
	// bronpaper waar het uit komt (dat naar hém wijst).
	tableDestillaat: {{relation: data.RelHasQuote}, {relation: data.RelContains, incoming: true}},
	tableQuote:      {},
}

// graphParentRules bouwt het spoor terug naar de wortel: citaat → standpunt →
// subthema → thema → domein.
var graphParentRules = map[string]graphStep{
	tableQuote:      {relation: data.RelHasQuote, incoming: true},
	tableDestillaat: {relation: relBelongsToSubthema},
	tableSubthema:   {relation: relContainsSubthema, incoming: true},
	tableThema:      {relation: relContainsThema, incoming: true},
}

// graphMaxTrailHops begrenst de klim naar de wortel. Het diepste echte spoor
// is citaat → standpunt → subthema → thema → domein (vier stappen); de vijfde
// is speling, en tegelijk de rem als de data ooit een lus bevat.
const graphMaxTrailHops = 5

// graphChildCap is het maximum aantal kinderen per antwoord. Een subthema of
// bronpaper kan honderden standpunten hebben; de rest komt via ?offset.
const graphChildCap = 150

// graphPageSize is de paginagrootte van ListNodes — gelijk aan de cap van
// core's ListOpts, zodat een pagina nooit stilletjes kleiner uitvalt dan
// gevraagd en "korte pagina = einde" een geldig stopcriterium is.
const graphPageSize = 200

// graphFetchWorkers is de parallelliteit van de GetNode-fan-out.
const graphFetchWorkers = 8

// graphLabelMaxRunes kapt lange teksten (samenvatting, citaat) af tot een
// leesbaar label; de volledige tekst zit in metadata.text.
const graphLabelMaxRunes = 140

// graphIDPattern is de id-vorm van core: tabel + uuid. Elke route toetst hem
// vóór er een query loopt, zodat een verzonnen id nooit de database raakt.
var graphIDPattern = regexp.MustCompile(`^[a-z_]+:[0-9a-f-]{36}$`)

// --- wire-vormen ------------------------------------------------------------
//
// Bewust snake_case in relationships (`source_id`, `target_id`,
// `relationship_type`): dat is de vorm die de graafrenderer leest. De rest
// van de module is camelCase; deze uitzondering staat ook in de README.

type graphNode struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Label is een kale string voor onvertaalde tekst (een citaat, een
	// papertitel) en een taalobject voor tekst die wél vertaald is. De client
	// kiest zijn UI-taal en valt terug op de andere taal.
	Label    any            `json:"label"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// SourceLanguage is de taal waarin deze knoop geschreven is. Hij is leeg
	// voor een eentalig corpus; alleen mét die taal kan de client zeggen dat
	// wat er staat een vertaling is en niet het origineel.
	SourceLanguage string `json:"sourceLanguage,omitempty"`
}

type graphRelationship struct {
	SourceID         string `json:"source_id"`
	TargetID         string `json:"target_id"`
	RelationshipType string `json:"relationship_type"`
}

// graphRef is een node zonder metadata: genoeg voor een broodkruimel.
type graphRef struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label any    `json:"label"`
}

// graphKB is de importstand die bij het antwoord zit, zodat een client (of
// agent) niet apart hoeft te pollen om te weten waarom de graaf leeg is. Het
// veld draagt de volledige ImportState-enum (empty|importing|ready|incomplete|
// failed), gelijk aan `state` op GET /v1/knowledge/status: een graaf die leeg
// oogt omdat de import nog loopt, is iets anders dan een lege kennisbank.
type graphKB struct {
	State string `json:"state"`
}

// graphOverviewResponse draagt naast het structuurniveau ook childCounts: per
// node hoeveel kinderen `children` voor hem zou tellen. De kaart tekent daarop
// het "+"-teken, en zonder die telling zou dat teken een belofte zijn die de
// data niet hoeft na te komen. Nul telt niet mee — een node zonder kinderen
// staat er domweg niet in, zodat de wire klein blijft.
type graphOverviewResponse struct {
	Nodes         []graphNode         `json:"nodes"`
	Relationships []graphRelationship `json:"relationships"`
	Totals        map[string]int      `json:"totals"`
	ChildCounts   map[string]int      `json:"childCounts"`
	KB            graphKB             `json:"kb"`
}

// graphChildrenResponse: `truncated` is afleidbaar uit `nextOffset` (die staat
// er precies dan) en blijft toch op de wire. Een client die alleen "is dit
// alles?" wil weten, hoeft dan geen conclusie te trekken uit de afwezigheid van
// een veld — en `total` is een bovengrens: kinderen die tussen twee pagina's
// door verdwijnen tellen mee in het totaal en niet in de nodes.
type graphChildrenResponse struct {
	Nodes         []graphNode         `json:"nodes"`
	Relationships []graphRelationship `json:"relationships"`
	Total         int                 `json:"total"`
	Truncated     bool                `json:"truncated"`
	NextOffset    *int                `json:"nextOffset,omitempty"`
}

type graphNodeResponse struct {
	Node  graphNode  `json:"node"`
	Trail []graphRef `json:"trail"`
}

// --- seams ------------------------------------------------------------------

// graphNodeReader is de leesseam van de graaf op core's kennisgraaf: alleen
// lezen, en alleen de twee generieke reads. De typed reads zitten er bewust
// niet in — die laten props vallen die de graaf juist toont.
type graphNodeReader interface {
	GetNode(ctx context.Context, id data.NodeID) (map[string]any, error)
	ListNodes(ctx context.Context, table string, opts data.ListOpts) ([]map[string]any, error)
}

// graphService beantwoordt de drie graafvragen. Hij wordt één keer bij Init
// gebouwd. De importstand wordt per verzoek gelezen, zodat een import die
// tijdens het bladeren afrondt meteen zichtbaar is; de enige staat die hij over
// verzoeken heen houdt is childCache, en die leegt diezelfde import.
type graphService struct {
	nodes graphNodeReader
	edges data.EdgeLister
	// childCache bewaart de afgebakende kinderlijst per (kennisbank, ouder),
	// zodat een bladertocht de volledige leesronde één keer
	// betaalt in plaats van per pagina. Zie graph_child_cache.go.
	childCache graphChildCache
	// status geeft de importstand van de module. De marker beantwoordt alleen
	// "klaar of niet"; dat een lege graaf midden in een import zit, of op een
	// mislukte poging, weet alleen de importer — en zonder dat verschil leest
	// elke storing als een lege kennisbank.
	status func() importStatus
	logger *slog.Logger
}

func (g *graphService) log() *slog.Logger { return logOr(g.logger) }

// fault logt een repo-fout zoals de rest van de module dat doet: de reden-code
// op Error, de fouttekst hooguit op Debug. De kennisgraaf draagt
// kennisbanktekst, en een adapter die zijn invoer in een foutmelding echoot zou
// die tekst anders in een gewone logregel zetten.
func (g *graphService) fault(msg, operatie string, err error, attrs ...any) {
	base := append([]any{"component", "duif", "operatie", operatie}, attrs...)
	g.log().Error(msg, append(base, "reason", ReasonGraphUnavailable)...)
	g.log().Debug(msg, append(base, "error", err)...)
}

// noScope logt dat een leesroute zonder actieve kennisbank is dichtgeklapt.
// Sinds readKB geen `ready` zonder kb_id meer teruggeeft hoort dit niet te
// kunnen; komt het er tóch uit (een vreemde of beschadigde marker), dan is een
// lege graaf het antwoord en zegt het log waarom.
func (g *graphService) noScope(operatie string) {
	g.log().Error("kennisgraaf: leesverzoek zonder actieve kennisbank geweigerd",
		"component", "duif", "operatie", operatie, "reason", ReasonNodeNotFound)
}

// --- de actieve kennisbank --------------------------------------------------

// readKB zegt welke kennisbank de graaf mag tonen, en in welke stand hij is.
// De marker beantwoordt alleen de eerste vraag; voor de tweede is de importer
// de bron. Een leesfout is een 503: doen alsof de KB leeg is zou een storing
// als een lege bank vermommen.
func (g *graphService) readKB(ctx context.Context) (importStatus, error) {
	st, err := readImportStatus(ctx, g.nodes)
	if err != nil {
		g.fault("kennisgraaf: importstand niet leesbaar", "graph_marker", err)
		return importStatus{}, errGraphUnavailable
	}
	if st.State != ImportReady {
		st.State = g.importState()
	}
	if st.State == ImportReady && st.KBID == "" {
		// `ready` zonder kb_id kan niets afbakenen: elke rij zonder kb_id-prop
		// zou dan matchen en de graaf toonde alle kennisbanken tegelijk. De
		// guard staat ná het afleiden van de stand en niet ervóór, want de
		// afleiding gaf het weggehaalde `ready` anders meteen terug — mét een
		// lege kb_id. Een stand die niet `ready` is blijft staan zoals hij is:
		// "er wordt geïmporteerd" is geen lege kennisbank.
		st = importStatus{State: ImportEmpty}
	}
	return st, nil
}

// importState is de stand van de module-importer, of `empty` zolang die er niet
// is: een graaf zonder gewirede import kan alleen leeg zijn.
func (g *graphService) importState() ImportState {
	if g.status == nil {
		return ImportEmpty
	}
	if st := g.status().State; st != "" {
		return st
	}
	return ImportEmpty
}

// --- projectie naar de wire -------------------------------------------------

// validGraphID toetst id-vorm én tabel-allowlist. Beide fouten geven dezelfde
// 404: van buiten mag niet te zien zijn welke tabellen er nog meer bestaan.
func validGraphID(id string) bool {
	return graphIDPattern.MatchString(id) && graphTables[tableOf(id)]
}

// truncateLabel kapt af op een woordgrens en zet er een beletselteken achter.
// Witruimte wordt eerst genormaliseerd: een label met harde regeleindes leest
// in de graaf als een gat.
func truncateLabel(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}

// graphLabelLocalized zegt welke tabellen een vertaald label dragen. Het
// bronpaper staat er niet in: zijn titel wordt niet vertaald. Het citaat
// evenmin — dat is het origineel.
var graphLabelLocalized = map[string]bool{
	tableDomein:     true,
	tableThema:      true,
	tableSubthema:   true,
	tableDestillaat: true,
}

// graphLabelProp is de prop waar het label uit komt, ook voor de twee tabellen
// waarvan het label een afgekapte tekst is.
func graphLabelProp(table string) string {
	switch table {
	case tableDestillaat:
		return "samenvatting"
	case tableQuote:
		return "text"
	}
	return graphLabelProps[table]
}

// graphLabel bepaalt het label van een node: een titel of naam waar die er is,
// en anders de afgekapte tekst van het standpunt of citaat. Een vertaald label
// komt als taalobject terug, een onvertaald label als kale string.
func graphLabel(table string, props map[string]any) any {
	key := graphLabelProp(table)
	if key == "" {
		return ""
	}
	shorten := func(s string) string {
		if table == tableDestillaat || table == tableQuote {
			return truncateLabel(s, graphLabelMaxRunes)
		}
		return strings.TrimSpace(s)
	}
	if graphLabelLocalized[table] {
		if label := bilingualProp(props, key); label != nil {
			return label.mapText(shorten)
		}
		return ""
	}
	s, _ := props[key].(string)
	return shorten(s)
}

// graphNodeOf projecteert een ruwe props-map op de wire-node. Dit is de enige
// plek waar props naar buiten gaan: alles buiten de allowlist blijft achter.
func graphNodeOf(id string, props map[string]any) graphNode {
	table := tableOf(id)
	n := graphNode{ID: id, Type: table, Label: graphLabel(table, props)}
	if lang, ok := props["source_language"].(string); ok {
		n.SourceLanguage = lang
	}
	for _, f := range graphMetadata[table] {
		v, ok := props[f.prop].(string)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if f.wire == "citationConfidence" && bibliographyConfidence(v) == "" {
			continue
		}
		if n.Metadata == nil {
			n.Metadata = map[string]any{}
		}
		if f.localized {
			n.Metadata[f.wire] = bilingualProp(props, f.prop)
			continue
		}
		n.Metadata[f.wire] = v
	}
	// Only an explicit corpus boolean becomes this code-owned text enum.
	// Missing legacy data is unknown; true is not expert validation.
	if table == tableQuote {
		if grounded, present := props["grounded"].(bool); present {
			if n.Metadata == nil {
				n.Metadata = map[string]any{}
			}
			status := groundingRejected
			if grounded {
				status = groundingVerified
			}
			n.Metadata["grounding"] = status
		}
	}
	return n
}

// graphRefOf is de broodkruimelvorm van een node (geen metadata).
func graphRefOf(id string, props map[string]any) graphRef {
	table := tableOf(id)
	return graphRef{ID: id, Type: table, Label: graphLabel(table, props)}
}

// sortGraphNodes en sortRelationships maken elk antwoord stabiel: dezelfde
// data geeft byte voor byte dezelfde JSON, zodat de frontend kan diffen en de
// tests niet op map-volgorde leunen.
func sortGraphNodes(nodes []graphNode) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
}

func sortRelationships(rels []graphRelationship) {
	sort.Slice(rels, func(i, j int) bool {
		if rels[i].SourceID != rels[j].SourceID {
			return rels[i].SourceID < rels[j].SourceID
		}
		if rels[i].TargetID != rels[j].TargetID {
			return rels[i].TargetID < rels[j].TargetID
		}
		return rels[i].RelationshipType < rels[j].RelationshipType
	})
}

// --- lezen met kb-filter ----------------------------------------------------

// getScopedNode leest één node en toetst hem tegen de actieve kennisbank. Alle
// drie de uitkomsten "bestaat niet", "hoort bij een andere kb" en "verkeerde
// tabel" geven dezelfde 404.
func (g *graphService) getScopedNode(ctx context.Context, kbID string, id string) (map[string]any, error) {
	if kbID == "" {
		g.noScope("graph_get_node")
		return nil, errGraphNotFound
	}
	props, err := g.nodes.GetNode(ctx, data.NodeID(id))
	if err != nil {
		g.fault("kennisgraaf: node niet leesbaar", "graph_get_node", err)
		return nil, errGraphUnavailable
	}
	if props == nil {
		return nil, errGraphNotFound
	}
	if got, _ := props["kb_id"].(string); got != kbID {
		return nil, errGraphNotFound
	}
	return props, nil
}

// listScopedTable paginieert één tabel en houdt alleen de nodes van de actieve
// kennisbank over. Nooit unbounded: elke pagina is er één van 200, en een korte
// pagina is het einde.
func (g *graphService) listScopedTable(ctx context.Context, kbID, table string) ([]graphNode, error) {
	if kbID == "" {
		g.noScope("graph_list_nodes")
		return nil, nil
	}
	var out []graphNode
	for offset := 0; ; offset += graphPageSize {
		rows, err := g.nodes.ListNodes(ctx, table, data.ListOpts{Limit: graphPageSize, Offset: offset})
		if err != nil {
			g.fault("kennisgraaf: tabel niet leesbaar", "graph_list_nodes", err, "tabel", table)
			return nil, errGraphUnavailable
		}
		for _, row := range rows {
			if got, _ := row["kb_id"].(string); got != kbID {
				continue
			}
			id := rowID(row)
			if id == "" {
				continue
			}
			out = append(out, graphNodeOf(id, row))
		}
		if len(rows) < graphPageSize {
			return out, nil
		}
	}
}

// listEdges is de enkele plek waar de EdgeLister wordt aangesproken, zodat de
// foutafhandeling (503, geen fouttekst) niet op vier plekken herhaald wordt.
func (g *graphService) listEdges(ctx context.Context, relation string, fromIDs, toIDs []data.NodeID) ([]data.Edge, error) {
	edges, err := g.edges.ListEdges(ctx, relation, fromIDs, toIDs)
	if err != nil {
		g.fault("kennisgraaf: relaties niet leesbaar", "graph_list_edges", err, "relatie", relation)
		return nil, errGraphUnavailable
	}
	return edges, nil
}

// fetchNodes haalt een lijst nodes parallel op (errgroup met een vaste
// limiet). De uitkomst houdt de volgorde van `ids` aan; een node die er niet
// meer is, of van een andere kennisbank, valt stil weg — een datafout mag de
// hele view niet omgooien.
func (g *graphService) fetchNodes(ctx context.Context, kbID string, ids []string) ([]graphNode, error) {
	if kbID == "" {
		g.noScope("graph_children")
		return nil, nil
	}
	found := make([]*graphNode, len(ids))
	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(graphFetchWorkers)
	for i, id := range ids {
		grp.Go(func() error {
			props, err := g.nodes.GetNode(gctx, data.NodeID(id))
			if err != nil {
				g.fault("kennisgraaf: kind niet leesbaar", "graph_children", err)
				return errGraphUnavailable
			}
			if props == nil {
				return nil
			}
			if got, _ := props["kb_id"].(string); got != kbID {
				return nil
			}
			n := graphNodeOf(id, props)
			found[i] = &n
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		return nil, err
	}
	out := make([]graphNode, 0, len(ids))
	for _, n := range found {
		if n != nil {
			out = append(out, *n)
		}
	}
	return out, nil
}

// --- kinderen verzamelen ----------------------------------------------------

// graphChildEdge is één kandidaat-kind: het id van het kind plus de edge
// waarlangs het gevonden is, zodat children hem ook als relatie op de wire kan
// zetten.
type graphChildEdge struct {
	childID string
	rel     graphRelationship
}

// collectChildEdges verzamelt de kandidaat-kinderen van een groep nodes uit
// dezelfde tabel: per regel uit graphChildRules één ListEdges, begrensd op die
// ids. Dit is de enige plek waar "één niveau dieper" wordt uitgerekend —
// overview telt ermee en children bladert erdoorheen, zodat beide van dezelfde
// kandidatenlijst uitgaan.
//
// De volgorde per ouder is regelvolgorde, en binnen een regel op id: dat is wat
// de paginering van children over verzoeken heen stabiel houdt. Dedup gebeurt
// per ouder en over de regels heen, zodat een kind dat langs twee regels
// binnenkomt één keer telt.
//
// Deze functie kent de kennisbank niet: zij levert kandidaten, en de afbakening
// gebeurt erna — op de kaart met inScopeChildren, in de lijst achter de klik
// met scopedChildren. Die twee zijn bewust niet hetzelfde: scopedChildren leest
// elk kind en is dus exact, inScopeChildren toetst alleen de structuurlaag en
// geeft daaronder een bovengrens. De kindertelling op de kaart mag daar dus
// hoger uitvallen dan de lijst achter de klik; de beperking hieronder zegt
// waarom die prijs bewust betaald is.
//
// Beperking, na de afbakening: van de structuurlaag (domein, thema, subthema)
// wordt elk kind werkelijk getoetst — dat is een handvol nodes. Standpunten en
// citaten gaan ongetoetst mee, want ze stuk voor stuk lezen zou de telling op
// de kaart een volledige scan van ±8300 standpunten kosten. Een telling op een
// subthema is daarmee een bovengrens; op de structuurlaag is zij exact.
func (g *graphService) collectChildEdges(ctx context.Context, table string, ids []data.NodeID) (map[string][]graphChildEdge, error) {
	out := map[string][]graphChildEdge{}
	if len(ids) == 0 {
		// Zonder bronlijst zou ListEdges de hele graaf lezen; dat nooit.
		return out, nil
	}
	owners := make(map[string]bool, len(ids))
	for _, id := range ids {
		owners[string(id)] = true
	}
	seen := make(map[string]map[string]bool, len(ids))
	for _, rule := range graphChildRules[table] {
		var edges []data.Edge
		var err error
		if rule.incoming {
			edges, err = g.listEdges(ctx, rule.relation, nil, ids)
		} else {
			edges, err = g.listEdges(ctx, rule.relation, ids, nil)
		}
		if err != nil {
			return nil, err
		}
		batch := map[string][]graphChildEdge{}
		for _, e := range edges {
			owner, child := string(e.From), string(e.To)
			if rule.incoming {
				owner, child = child, owner
			}
			// De adapter filtert al op de meegegeven ids; deze toets houdt de
			// telling ook goed als een edge langs de andere kant binnenkomt.
			if !owners[owner] || !validGraphID(child) {
				continue
			}
			if seen[owner] == nil {
				seen[owner] = map[string]bool{}
			}
			if seen[owner][child] {
				continue
			}
			seen[owner][child] = true
			batch[owner] = append(batch[owner], graphChildEdge{childID: child, rel: graphRelationship{
				SourceID:         string(e.From),
				TargetID:         string(e.To),
				RelationshipType: e.Relation,
			}})
		}
		for owner, kinderen := range batch {
			sort.Slice(kinderen, func(i, j int) bool { return kinderen[i].childID < kinderen[j].childID })
			out[owner] = append(out[owner], kinderen...)
		}
	}
	return out, nil
}

// inScopeChildren houdt de kinderen over die bij de actieve kennisbank horen,
// voor de telling op de kaart. `scoped` zegt van de structuurlaag welke ids dat
// zijn; standpunten en citaten gaan daar ongetoetst mee (zie de beperking bij
// collectChildEdges), dus childCounts is op een subthema een bovengrens. De lijst achter de klik toetst ze wél stuk voor stuk, met
// scopedChildren.
func inScopeChildren(kinderen []graphChildEdge, scoped map[string]bool) []graphChildEdge {
	out := make([]graphChildEdge, 0, len(kinderen))
	for _, k := range kinderen {
		if graphStructureTable[tableOf(k.childID)] && !scoped[k.childID] {
			continue
		}
		out = append(out, k)
	}
	return out
}

// scopedChildren houdt alleen de kinderen over die werkelijk gelezen zijn en
// bij deze kennisbank horen — elke laag, zonder uitzondering. Hier hangt de
// hele paginering van children aan: total, truncated en nextOffset tellen deze
// lijst, dus wat de lezer beloofd wordt is precies wat hij krijgt.
func scopedChildren(kinderen []graphChildEdge, scoped map[string]bool) []graphChildEdge {
	out := make([]graphChildEdge, 0, len(kinderen))
	for _, k := range kinderen {
		if !scoped[k.childID] {
			continue
		}
		out = append(out, k)
	}
	return out
}

// scopedChildNodes leest de meegegeven kandidaten en geeft terug welke bij deze
// kennisbank horen, mét de node erbij zodat de pagina ze niet een tweede keer
// hoeft te lezen.
//
// De leeskosten zijn begrensd door de kinderlijst van die ene ouder, niet door
// een tabel: een klik op een subthema leest de standpunten van dát subthema.
// Dat is precies de prijs die de kaart niet wil betalen — daar zou het om alle
// ± 8 300 standpunten tegelijk gaan — en die de lijst achter de klik wel kan
// betalen, omdat zij er toch al een pagina van ophaalde.
//
// Die volle ronde hoort ook maar één keer per bladertocht betaald te worden: de
// seam kent geen batch-read, dus hij is één GetNode per id. childCandidates
// bewaart de uitkomst kort, en de pagina's daarna geven hier alleen nog hun
// eigen pagina mee.
func (g *graphService) scopedChildNodes(ctx context.Context, kbID string, kinderen []graphChildEdge) (map[string]graphNode, error) {
	if len(kinderen) == 0 {
		return map[string]graphNode{}, nil
	}
	ids := make([]string, 0, len(kinderen))
	for _, k := range kinderen {
		ids = append(ids, k.childID)
	}
	nodes, err := g.fetchNodes(ctx, kbID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]graphNode, len(nodes))
	for _, n := range nodes {
		out[n.ID] = n
	}
	return out, nil
}

// --- overview ---------------------------------------------------------------

// overview levert het structuurniveau: domeinen, thema's en subthema's van de
// actieve kennisbank, plus de relaties daartussen. De standpunten en citaten (± 8 300 nodes) blijven eruit; die
// komen per klik via children.
func (g *graphService) overview(ctx context.Context) (graphOverviewResponse, error) {
	out := graphOverviewResponse{
		Nodes:         []graphNode{},
		Relationships: []graphRelationship{},
		Totals:        map[string]int{},
		ChildCounts:   map[string]int{},
	}
	kb, err := g.readKB(ctx)
	if err != nil {
		return out, err
	}
	out.KB = graphKB{State: string(kb.State)}
	if kb.State != ImportReady {
		return out, nil
	}
	for table, n := range kb.Counts {
		// Alleen tabellen: de marker telt ook relaties, en die horen niet in
		// `totals` per tabel thuis.
		if graphTables[table] {
			out.Totals[table] = n
		}
	}

	byTable := map[string][]data.NodeID{}
	known := map[string]bool{}
	for _, table := range graphStructureTables {
		nodes, err := g.listScopedTable(ctx, kb.KBID, table)
		if err != nil {
			return graphOverviewResponse{}, err
		}
		for _, n := range nodes {
			byTable[table] = append(byTable[table], data.NodeID(n.ID))
			known[n.ID] = true
		}
		out.Nodes = append(out.Nodes, nodes...)
	}

	// De relaties worden per relatie opgehaald en begrensd op de bronnen die we
	// net gelezen hebben — nooit een ongefilterde ListEdges over de hele graaf.
	for _, rule := range graphOverviewEdgeRules {
		from := byTable[rule.fromTable]
		if len(from) == 0 {
			continue
		}
		edges, err := g.listEdges(ctx, rule.relation, from, nil)
		if err != nil {
			return graphOverviewResponse{}, err
		}
		for _, e := range edges {
			if !known[string(e.To)] {
				continue
			}
			out.Relationships = append(out.Relationships, graphRelationship{
				SourceID:         string(e.From),
				TargetID:         string(e.To),
				RelationshipType: e.Relation,
			})
		}
	}

	// De kindertelling per node: dezelfde regels en dezelfde dedup als
	// children, per tabel begrensd op de ids die we hierboven al gelezen
	// hebben. Eén ListEdges per (tabel, regel), geen enkele node-read erbij.
	for _, table := range graphStructureTables {
		byParent, err := g.collectChildEdges(ctx, table, byTable[table])
		if err != nil {
			return graphOverviewResponse{}, err
		}
		for parent, kinderen := range byParent {
			// De overview heeft de hele structuurlaag van deze kennisbank al
			// gelezen: `known` is dus precies de afbakening, en de toets kost
			// geen enkele extra read.
			if n := len(inScopeChildren(kinderen, known)); n > 0 {
				out.ChildCounts[parent] = n
			}
		}
	}

	sortGraphNodes(out.Nodes)
	sortRelationships(out.Relationships)
	return out, nil
}

// graphOverviewEdgeRule koppelt een relatie aan de tabel waar hij vandaan komt,
// zodat ListEdges altijd een begrensde bronlijst meekrijgt.
type graphOverviewEdgeRule struct {
	relation  string
	fromTable string
}

var graphOverviewEdgeRules = []graphOverviewEdgeRule{
	{relation: relContainsThema, fromTable: tableDomein},
	{relation: relContainsSubthema, fromTable: tableThema},
}

// --- children ---------------------------------------------------------------

// children levert één niveau dieper vanaf een node. De relaties in het
// antwoord zijn de echte edges: meestal ouder → kind, maar bij een standpunt
// onder zijn subthema of een bronpaper onder zijn standpunt wijst de edge de
// andere kant op, en dat blijft zo op de wire.
func (g *graphService) children(ctx context.Context, id string, offset int) (graphChildrenResponse, error) {
	out := graphChildrenResponse{Nodes: []graphNode{}, Relationships: []graphRelationship{}}
	if !validGraphID(id) {
		return out, errGraphNotFound
	}
	kb, err := g.readKB(ctx)
	if err != nil {
		return out, err
	}
	if _, err := g.getScopedNode(ctx, kb.KBID, id); err != nil {
		return out, err
	}

	// Twee ronden, hooguit: de tweede alleen als de bewaarde lijst een kind
	// noemt dat er niet meer is. Dan is de belofte van total en nextOffset niet
	// meer waar te maken en telt de graaf liever opnieuw dan te gokken.
	for poging := 0; ; poging++ {
		candidates, gelezen, err := g.childCandidates(ctx, kb.KBID, id)
		if err != nil {
			return graphChildrenResponse{}, err
		}

		out.Total = len(candidates)
		if offset > len(candidates) {
			offset = len(candidates)
		}
		page := candidates[offset:]
		if len(page) > graphChildCap {
			page = page[:graphChildCap]
		}
		if end := offset + len(page); end < len(candidates) {
			out.Truncated = true
			next := end
			out.NextOffset = &next
		} else {
			out.Truncated, out.NextOffset = false, nil
		}

		if gelezen == nil {
			// De lijst kwam uit de cache, dus alleen deze pagina hoeft nog
			// gelezen te worden — niet opnieuw alle kinderen van de ouder.
			gelezen, err = g.scopedChildNodes(ctx, kb.KBID, page)
			if err != nil {
				return graphChildrenResponse{}, err
			}
			if len(gelezen) != len(page) && poging == 0 {
				g.childCache.drop(graphChildCacheKey(kb.KBID, id))
				out = graphChildrenResponse{Nodes: []graphNode{}, Relationships: []graphRelationship{}}
				continue
			}
		}

		// De kinderen staan gelezen klaar: elke node op deze pagina kwam door
		// dezelfde toets als de telling, dus er is geen tweede waarheid.
		out.Nodes = make([]graphNode, 0, len(page))
		out.Relationships = make([]graphRelationship, 0, len(page))
		for _, c := range page {
			n, ok := gelezen[c.childID]
			if !ok {
				continue
			}
			out.Nodes = append(out.Nodes, n)
			out.Relationships = append(out.Relationships, c.rel)
		}
		sortGraphNodes(out.Nodes)
		sortRelationships(out.Relationships)
		return out, nil
	}
}

// childCandidates geeft de afgebakende kinderlijst van één ouder, in de vaste
// volgorde waarop de paginering leunt. Kwam hij vers uit de graaf, dan komen de
// gelezen nodes mee: de pagina hoeft ze dan niet nóg eens te lezen. Kwam hij uit
// de cache, dan is de nodemap nil en leest de aanroeper alleen zijn eigen
// pagina.
//
// Eerst de afbakening, dan pas tellen en pagineren. Andersom beschrijven total,
// truncated en nextOffset rijen die de lezer nooit te zien krijgt: een vreemd
// thema onder een eigen domein gaf total=3 bij twee nodes, en de laatste pagina
// kwam er één te kort uit.
//
// De toets loopt over álle kandidaten van deze ene ouder, ook de standpunten en
// citaten. Alleen op de structuurlaag toetsen liet dezelfde belofte staan op de
// laag eronder: een subthema met 150 eigen en 5 vreemde standpunten gaf
// total=155 en een tweede pagina die leeg bleek.
func (g *graphService) childCandidates(ctx context.Context, kbID, id string) ([]graphChildEdge, map[string]graphNode, error) {
	key := graphChildCacheKey(kbID, id)
	if cached, ok := g.childCache.get(key); ok {
		return cached, nil, nil
	}
	// Kandidaten in regelvolgorde, per regel op id gesorteerd: de paginering
	// moet over verzoeken heen dezelfde volgorde houden. Dezelfde helper die de
	// overview zijn childCounts geeft, hier voor één ouder.
	byParent, err := g.collectChildEdges(ctx, tableOf(id), []data.NodeID{data.NodeID(id)})
	if err != nil {
		return nil, nil, err
	}
	gelezen, err := g.scopedChildNodes(ctx, kbID, byParent[id])
	if err != nil {
		return nil, nil, err
	}
	scoped := make(map[string]bool, len(gelezen))
	for childID := range gelezen {
		scoped[childID] = true
	}
	candidates := scopedChildren(byParent[id], scoped)
	g.childCache.put(key, candidates)
	return candidates, gelezen, nil
}

// --- node + trail -----------------------------------------------------------

// node levert één node met zijn voorouderspoor, zodat "open in de kennisgraaf"
// vanuit een bronkaart de hele tak kan openklappen. Ontbreekt een voorouder
// (datafout), dan is het spoor korter — nooit een fout.
func (g *graphService) node(ctx context.Context, id string) (graphNodeResponse, error) {
	var out graphNodeResponse
	if !validGraphID(id) {
		return out, errGraphNotFound
	}
	kb, err := g.readKB(ctx)
	if err != nil {
		return out, err
	}
	props, err := g.getScopedNode(ctx, kb.KBID, id)
	if err != nil {
		return out, err
	}
	out.Node = graphNodeOf(id, props)
	out.Trail = []graphRef{}

	// Klim naar de wortel. `visited` is de lusrem: de data komt uit een
	// gedeelde graaf en mag deze handler niet laten rondtollen.
	visited := map[string]bool{id: true}
	curID := id
	var ancestors []graphRef
	for hop := 0; hop < graphMaxTrailHops; hop++ {
		rule, ok := graphParentRules[tableOf(curID)]
		if !ok {
			break
		}
		parentID, err := g.parentOf(ctx, curID, rule)
		if err != nil {
			return graphNodeResponse{}, err
		}
		if parentID == "" || visited[parentID] {
			break
		}
		parentProps, err := g.getScopedNode(ctx, kb.KBID, parentID)
		if err != nil {
			if errors.Is(err, errGraphNotFound) {
				break // ontbrekende ouder: korter spoor, geen fout
			}
			return graphNodeResponse{}, err
		}
		visited[parentID] = true
		ancestors = append(ancestors, graphRefOf(parentID, parentProps))
		curID = parentID
	}

	// Van wortel naar ouder: dat leest als een broodkruimel.
	for i := len(ancestors) - 1; i >= 0; i-- {
		out.Trail = append(out.Trail, ancestors[i])
	}
	return out, nil
}

// parentOf zoekt de ene ouder van een node volgens de regel van zijn tabel.
// Meerdere ouders (een datafout in deze bundel) → de laagste id, zodat het
// antwoord stabiel is.
func (g *graphService) parentOf(ctx context.Context, id string, rule graphStep) (string, error) {
	var edges []data.Edge
	var err error
	if rule.incoming {
		edges, err = g.listEdges(ctx, rule.relation, nil, []data.NodeID{data.NodeID(id)})
	} else {
		edges, err = g.listEdges(ctx, rule.relation, []data.NodeID{data.NodeID(id)}, nil)
	}
	if err != nil {
		return "", err
	}
	best := ""
	for _, e := range edges {
		cand := string(e.To)
		if rule.incoming {
			cand = string(e.From)
		}
		if !validGraphID(cand) {
			continue
		}
		if best == "" || cand < best {
			best = cand
		}
	}
	return best, nil
}
