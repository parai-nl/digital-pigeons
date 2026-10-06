package duif

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/parai-nl/parai-core/data"
)

// De HTTP-laag van de kennisgraaf. Drie GET's, geen schrijfpad: de graaf is
// een venster op de geïmporteerde kennisbank, niet een manier om hem te
// veranderen.

// graphRoutes mount de graafroutes; module.go roept hem aan vanuit Routes()
// (auth komt van het framework, net als bij de andere routes).
func (m *Module) graphRoutes(r chi.Router) {
	r.Get("/v1/graph/overview", m.handleGraphOverview)
	r.Get("/v1/graph/nodes/{id}", m.handleGraphNode)
	r.Get("/v1/graph/nodes/{id}/children", m.handleGraphChildren)
}

// newGraphService bouwt de leesdienst uit de gewirede kennisgraaf — één keer,
// bij Init. Zonder kennisgraaf, of met een adapter die geen edges kan
// opsommen, kan de graaf niets zinnigs tonen: dat is een 503 met een
// reden-code, niet een leeg scherm dat op een lege kennisbank lijkt. Die
// vaststelling verandert niet per verzoek, en de waarschuwing erover hoort dus
// één keer in het opstartlog te staan en niet bij elke klik opnieuw.
func (m *Module) newGraphService() *graphService {
	if m.knowledge == nil {
		m.log().Warn("kennisgraaf niet beschikbaar: graafroutes antwoorden 503",
			"component", "duif", "reason", ReasonImportNoKnowledge)
		return nil
	}
	// ListEdges is een optionele port van core (de Postgres-adapter heeft hem,
	// een uitgeklede repo niet).
	lister, ok := m.knowledge.(data.EdgeLister)
	if !ok {
		m.log().Warn("kennisgraaf zonder EdgeLister: graafroutes antwoorden 503",
			"component", "duif", "reason", reasonNoEdgeLister)
		return nil
	}
	return &graphService{nodes: m.knowledge, edges: lister, status: m.knowledgeStatus, logger: m.logger}
}

// reasonNoEdgeLister is een log-only code: de repo kan geen relaties opsommen,
// dus er valt geen graaf te tekenen. De client ziet graph_unavailable.
const reasonNoEdgeLister = "no_edge_lister"

// writeGraphError zet een graaffout op de wire in de foutvorm van de module.
// Een fout zonder reden-code (kan hier niet ontstaan, maar de wire moet
// sluitend zijn) valt terug op graph_unavailable.
func writeGraphError(w http.ResponseWriter, err error) {
	var f *graphFault
	if !errors.As(err, &f) {
		f = errGraphUnavailable
	}
	writeJSON(w, f.status, errorBody(f.reason))
}

// noStore: de graaf en de importstatus zijn live vensters op de kennisgraaf.
// Een import die afrondt moet meteen zichtbaar zijn, dus geen enkele laag mag
// deze antwoorden bewaren.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

// handleGraphOverview — GET /v1/graph/overview.
func (m *Module) handleGraphOverview(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	if m.graph == nil {
		writeGraphError(w, errGraphUnavailable)
		return
	}
	out, err := m.graph.overview(r.Context())
	if err != nil {
		writeGraphError(w, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, out)
}

// handleGraphChildren — GET /v1/graph/nodes/{id}/children?offset=0.
func (m *Module) handleGraphChildren(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	offset, ok := parseGraphOffset(r.URL.Query().Get("offset"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorBody(ReasonInvalidOffset))
		return
	}
	if m.graph == nil {
		writeGraphError(w, errGraphUnavailable)
		return
	}
	out, err := m.graph.children(r.Context(), graphIDParam(r), offset)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, out)
}

// handleGraphNode — GET /v1/graph/nodes/{id}.
func (m *Module) handleGraphNode(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	if m.graph == nil {
		writeGraphError(w, errGraphUnavailable)
		return
	}
	out, err := m.graph.node(r.Context(), graphIDParam(r))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, out)
}

// graphIDParam leest de node-id uit het pad. De frontend codeert de dubbele
// punt (`destillaat%3A…`) en chi geeft het segment nog gecodeerd terug; zonder
// decoderen struikelt de id-vorm en is élke bronkaart een 404. Een id die
// niet te decoderen is blijft ongeldig en valt verderop als node_not_found.
func graphIDParam(r *http.Request) string {
	raw := chi.URLParam(r, "id")
	if id, err := url.PathUnescape(raw); err == nil {
		return id
	}
	return raw
}

// parseGraphOffset leest ?offset. Afwezig of leeg is 0; alles wat geen
// niet-negatief geheel getal is, is een expliciete 400 — een stille 0 zou een
// tikfout in de frontend eindeloos dezelfde pagina laten tonen.
func parseGraphOffset(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
