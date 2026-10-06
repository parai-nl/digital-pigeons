//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// budgetTrigger in een gebruikersbericht laat de stub de LiteLLM-budgetweigering
// nabootsen: een 4xx waarvan de tekst "budget" bevat.
const budgetTrigger = "TRIGGER_BUDGET_CAP"

// citationTrigger laat de stub een citatiemarkering in zijn antwoord zetten.
// De marker staat uitsluitend in de menselijke invoer; de bronfixture mag
// daaruit geen bron of citatie verzinnen.
const citationTrigger = "TRIGGER_CITATIE"

// embeddingDim is de vectordimensie van de stub-embedder.
//
// 3072 is geen vrije keuze: core's kennisgraaf neemt voor destillaat, quote
// en persona alleen vectoren van deze lengte aan. Een rij met een andere
// dimensie wordt bij het opslaan geweigerd ("expected 3072 dimensions, not N")
// en de hele import mislukt. Het is
// tegelijk de default van core's Knowledge-config; scripts/e2e.sh pint hem
// expliciet vast zodat de stub en de database niet uit elkaar kunnen lopen.
const embeddingDim = 3072

// embedMinTokenRunes is de ondergrens van een inhoudswoord. Lidwoorden,
// voorzetsels en voegwoorden ("de", "het", "van", "wat") dragen geen onderwerp
// en zouden twee volstrekt onverwante zinnen anders al op elkaar laten lijken.
const embedMinTokenRunes = 4

// stubLLM is een OpenAI-compatibele proxy-stub: hij spreekt hetzelfde
// SSE-contract als de LiteLLM-route waar de module in productie op praat,
// zonder een model of een euro te kosten. Nieuw geschreven testcode.
type stubLLM struct {
	*httptest.Server

	mu           sync.Mutex
	chatRequests []chatRequest
	embedCalls   int
	// embedFails laat /embeddings een 500 geven: de storing waarmee de e2e
	// aantoont dat een onbereikbare embedder terminaal kb_unavailable oplevert
	// en géén kennisgat.
	embedFails bool
}

// chatRequest is wat de stub van één completion-aanroep onthoudt.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	// Tools is de toolset die de aanroeper meestuurt. Hij is de enige manier
	// waarop de stub de router herkent. Geen enkele fase draagt een
	// JSON-schema: elke response_format is een onbekende fase.
	Tools          []json.RawMessage     `json:"tools"`
	ToolChoice     json.RawMessage       `json:"tool_choice"`
	MaxTokens      int                   `json:"max_tokens"`
	ResponseFormat fixtureResponseFormat `json:"response_format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCallID staat op de tool-rol: het antwoord van een tool aan het
	// model. Een nieuwe betaalde routercall na een toolresultaat is in deze
	// vaste één-actiefixture ongewenst en wordt geweigerd.
	ToolCallID string `json:"tool_call_id"`
}

// Scripted answer responses, never a semantic model judgement.
const fixturePositionNL = "Mijn standpunt is dat duivenbelangen moeten meetellen."
const fixturePositionEN = "My position is that pigeon interests deserve consideration."
const fixtureGapNL = "Dat kan ik niet vaststellen uit de beschikbare bronnen."
const fixtureGapEN = "I cannot establish that from the available sources."
const fixtureNestingAnswer = "De veldnotitie meldt dat de duiven na de netten op het balkon van de spreker zitten."
const fixtureRouterRewrite = "SYNTHETIC_ROUTER_REWRITE"
const fixtureRouterContext = "SYNTHETIC_ROUTER_CONTEXT"

// Wat de facilitator op zijn twee zichtbare momenten zegt. Het kader reist als
// argument van ask_pigeon mee; de bemiddeling is de afsluitende iteratie, van
// buiten herkenbaar aan tool_choice "none".
const fixtureRouterFraming = "SYNTHETIC_FACILITATOR_FRAMING"
const fixtureRouterMediation = "SYNTHETIC_FACILITATOR_MEDIATION"

type fixturePassage struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}
type fixtureSource struct {
	Number    int              `json:"number"`
	Paper     string           `json:"paper,omitempty"`
	Citation  string           `json:"citation,omitempty"`
	Grounding string           `json:"grounding"`
	Passages  []fixturePassage `json:"passages"`
}

// phase zegt welke aanroep dit is, van buiten te onderscheiden zonder één
// woord instructieprose te lezen: de facilitator draagt tools, de bemiddeling
// een tool_choice "none", de herformulering haar eigen opdracht, en wat
// overblijft is de duif die haar eigen antwoord schrijft — vrije tekst, geen
// schema. Een verzoek mét response_format is per definitie onbekend: er is
// geen gestructureerde modelcall meer in een beurt.
func (r chatRequest) phase() string {
	if len(r.Tools) > 0 {
		return "router"
	}
	// De afsluitende bemiddeling van de facilitator: geen tools meer, maar wel
	// een expliciete tool_choice "none". Dat onderscheidt haar van het
	// antwoord van de duif, dat helemaal geen tool_choice draagt.
	if string(r.ToolChoice) == fixtureNoneChoice {
		return "mediation"
	}
	if r.ResponseFormat.Type != "" || r.ResponseFormat.JSONSchema.Name != "" {
		return "unknown"
	}
	// De queryherformulering van de vergaderroutes: geen tools, geen
	// tool_choice en geen schema, dus zonder deze regel zou zij als antwoord
	// van de duif tellen. Zij is herkenbaar aan haar opdracht, en die opdracht
	// staat in geen enkele andere prompt van de module.
	if len(r.Messages) > 0 && isInstruction(r.Messages[0]) {
		if strings.Contains(r.Messages[0].Content, "one search query") ||
			strings.Contains(r.Messages[0].Content, "één zoekvraag") {
			return "query"
		}
	}
	return "answer"
}

// isInstruction zegt of dit bericht instructiegezag draagt.
func isInstruction(m chatMessage) bool { return m.Role == "system" || m.Role == "developer" }

// Markers the server itself writes into the pigeon's system prompt. The stub
// reads those, never a scrap of the persona's own instruction prose: an edited
// prompt file must not change which fixture answer comes back.
const (
	fixtureDutchDirective = "Antwoord in het Nederlands"
	fixtureRAGHeaderNL    = "Onderzoeksbronnen bij deze vraag"
	fixtureRAGHeaderEN    = "Research sources for this question"
	// De kop van de stakeholder-prompt. Die luidt in beide talen hetzelfde,
	// dus één constante volstaat.
	fixtureIndividual = "# Stakeholder"
)

// fixtureRouterChoice is the tool_choice the facilitator must send while it
// still has an action to take; fixtureNoneChoice is the one closing mediation
// iteration, which carries no tools at all.
//
// Het is "auto" en niet "required": de routeringsronde dwingt geen tool af,
// omdat juist die vorm op de vaste alias via de proxy minutenlang stil kan
// blijven. Dat de ronde tóch een tool moet opleveren
// bewaakt de server zelf (hasUsefulOutcome in handleDone), en dat is wat de
// fasecontroles hieronder blijven toetsen.
const fixtureRouterChoice = `"auto"`
const fixtureNoneChoice = `"none"`

const fixtureSupportedNL = "De beschikbare passage draagt deze bijdrage"
const fixtureSupportedEN = "The available passage supports this contribution"

// fixtureAnswerText is de duif die haar eigen antwoord schrijft: één vrije
// tekst, geen JSON. Wat erin komt hangt af van wat de server haar werkelijk
// meegaf — bronnen of een kennisgat, welke taal, welke duif.
func fixtureAnswerText(req chatRequest) (string, error) {
	// The pinned SDK may carry an instruction as system or as developer; both
	// are instruction authority, and neither is the human speaking.
	if len(req.Messages) < 2 || !isInstruction(req.Messages[0]) || req.Messages[len(req.Messages)-1].Role != "user" {
		return "", errors.New("stub_answer_shape")
	}
	system := req.Messages[0].Content
	var human string
	for _, m := range req.Messages {
		if m.Role == "user" {
			human = m.Content
		}
	}
	dutch := strings.Contains(system, fixtureDutchDirective)
	individual := strings.Contains(system, fixtureIndividual)
	grounded := strings.Contains(system, fixtureRAGHeaderNL) || strings.Contains(system, fixtureRAGHeaderEN)

	parts := []string{"Roekoe."}
	switch {
	case grounded && strings.Contains(system, kbQuoteNesting):
		parts = append(parts, strings.TrimSuffix(fixtureNestingAnswer, ".")+" [1].")
	case grounded:
		supported := fixtureSupportedEN
		if dutch {
			supported = fixtureSupportedNL
		}
		parts = append(parts, supported+" [1].")
	default:
		gap := fixtureGapEN
		if dutch {
			gap = fixtureGapNL
		}
		// The human asked for a citation marker. A pigeon without sources may
		// still produce one; the server must be the thing that removes it.
		if strings.Contains(human, citationTrigger) {
			gap = strings.TrimSuffix(gap, ".") + " [1]."
		}
		parts = append(parts, gap)
	}
	if individual {
		position := fixturePositionEN
		if dutch {
			position = fixturePositionNL
		}
		parts = append(parts, position)
	}
	return strings.Join(parts, " "), nil
}

func fixturePhaseResponse(req chatRequest) (string, error) {
	if req.phase() == "answer" {
		return fixtureAnswerText(req)
	}
	return "", errors.New("stub_unknown_phase")
}

// heeftToolAntwoord zegt of er al een toolresultaat in de historie staat.
func (r chatRequest) heeftToolAntwoord() bool {
	for _, m := range r.Messages {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

func startStubLLM(t *testing.T) *stubLLM {
	t.Helper()
	s := &stubLLM{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			s.handleChat(w, r)
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			s.handleEmbeddings(w, r)
		default:
			http.Error(w, `{"error":{"message":"stub: onbekende route"}}`, http.StatusNotFound)
		}
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// requests geeft een kopie van de tot nu toe ontvangen completion-aanroepen.
func (s *stubLLM) requests() []chatRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]chatRequest, len(s.chatRequests))
	copy(out, s.chatRequests)
	return out
}

func (s *stubLLM) embedCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.embedCalls
}

// failEmbeddings zet de embed-storing aan of uit. Aanroepen blijven meetellen:
// een mislukte aanroep is een aanroep.
func (s *stubLLM) failEmbeddings(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedFails = fail
}

func (s *stubLLM) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":{"message":"stub: onleesbare body"}}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.chatRequests = append(s.chatRequests, req)
	s.mu.Unlock()

	var laatste string
	for _, m := range req.Messages {
		if m.Role == "user" {
			laatste = m.Content
		}
	}

	// Capfout-pad: dezelfde vorm als de LiteLLM-proxy bij een overschreden
	// budget (4xx met "budget" in de tekst).
	if strings.Contains(laatste, budgetTrigger) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"litellm.BudgetExceededError: Budget has been exceeded! Current cost: 9.87654, Max budget: 9.0","type":"budget_exceeded","code":"400"}}`))
		return
	}

	if req.Model != "vertex/gemini-3.6-flash-eu" {
		http.Error(w, `{"error":{"message":"stub_model_policy"}}`, http.StatusBadRequest)
		return
	}
	if req.phase() == "router" {
		if req.heeftToolAntwoord() || string(req.ToolChoice) != fixtureRouterChoice {
			http.Error(w, `{"error":{"message":"stub_unexpected_router_round"}}`, http.StatusBadRequest)
			return
		}
		s.schrijfToolCall(w, req, laatste)
		return
	}
	if req.phase() == "mediation" {
		s.schrijfTekst(w, req.Model, []string{fixtureRouterMediation})
		return
	}
	answer, err := fixturePhaseResponse(req)
	if err != nil {
		http.Error(w, `{"error":{"message":"stub_unexpected_answer_phase"}}`, http.StatusBadRequest)
		return
	}
	// De duif stroomt in stukken, zoals een echte provider: de eerste zin is
	// af terwijl de rest nog komt, en de vergaderstem hoort daarop te voeden.
	delen := []string{answer}
	if req.phase() == "answer" {
		if cut := strings.Index(answer, ". "); cut > 0 {
			delen = []string{answer[:cut+2], answer[cut+2:]}
		}
	}

	s.schrijfTekst(w, req.Model, delen)
}

// schrijfTekst streamt vrije tekst in het OpenAI-chunkformaat: de stukken, dan
// de afsluitende chunk met finish_reason.
func (s *stubLLM) schrijfTekst(w http.ResponseWriter, model string, delen []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	write := func(payload string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	for _, d := range delen {
		write(fmt.Sprintf(
			`{"id":"stub-1","object":"chat.completion.chunk","created":0,"model":%q,"choices":[{"index":0,"delta":{"role":"assistant","content":%q},"finish_reason":null}]}`,
			model, d))
	}
	write(fmt.Sprintf(
		`{"id":"stub-1","object":"chat.completion.chunk","created":0,"model":%q,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`,
		model))
	write("[DONE]")
}

// schrijfToolCall streamt één ask_pigeon-aanroep in het OpenAI-chunkformaat:
// eerst het tool_call-delta, dan de afsluitende chunk met finish_reason.
func (s *stubLLM) schrijfToolCall(w http.ResponseWriter, req chatRequest, _ string) {
	args, err := json.Marshal(map[string]string{
		"question": fixtureRouterRewrite,
		"context":  fixtureRouterContext,
		"framing":  fixtureRouterFraming,
	})
	if err != nil {
		http.Error(w, `{"error":{"message":"stub: argumenten"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	write := func(payload string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	call, err := json.Marshal(map[string]any{
		"index": 0, "id": "call-stub-1", "type": "function",
		"function": map[string]string{"name": "ask_pigeon", "arguments": string(args)},
	})
	if err != nil {
		http.Error(w, `{"error":{"message":"stub: toolcall"}}`, http.StatusInternalServerError)
		return
	}
	write(fmt.Sprintf(
		`{"id":"stub-1","object":"chat.completion.chunk","created":0,"model":%q,"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[%s]},"finish_reason":null}]}`,
		req.Model, call))
	write(fmt.Sprintf(
		`{"id":"stub-1","object":"chat.completion.chunk","created":0,"model":%q,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		req.Model))
	write("[DONE]")
}

// handleEmbeddings levert een deterministische, tekstafhankelijke vector.
func (s *stubLLM) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input any    `json:"input"`
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	s.embedCalls++
	fails := s.embedFails
	s.mu.Unlock()
	if fails {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"stub: embedder tijdelijk weg","type":"server_error"}}`))
		return
	}

	out := map[string]any{
		"object": "list",
		"data": []any{map[string]any{
			"object":    "embedding",
			"index":     0,
			"embedding": stubEmbedding(embedInputText(req.Input)),
		}},
		"model": req.Model,
		"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// embedInputText leest het `input`-veld van de embed-aanroep. De client van
// core stuurt één string; de arrayvorm van het OpenAI-contract wordt hier
// samengevoegd in plaats van geweigerd, zodat de stub niet omvalt als core
// ooit batcht.
func embedInputText(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// stubEmbedding beeldt tekst af op een vaste, genormaliseerde vector: een
// bag-of-words met hashing (het "hashing trick"). Elk inhoudswoord kiest via
// een FNV-hash één as en een teken; het resultaat wordt op lengte 1 gebracht.
//
// Wat dat oplevert, en waarom de e2e het nodig heeft:
//   - dezelfde tekst geeft dezelfde vector, dus cosinus 1;
//   - een tekst die de ander bevat (samenvatting versus samenvatting + citaat,
//     de embed-tekst van de import) scoort hoog, ruim boven de drempel van 0,55;
//   - twee teksten zonder gedeelde inhoudswoorden staan loodrecht op elkaar:
//     cosinus 0.
//
// Zonder die eigenschappen zou "vraag vindt precies dit standpunt" niet te
// toetsen zijn: een constante vector maakt elk standpunt even relevant.
func stubEmbedding(text string) []float64 {
	vec := make([]float64, embeddingDim)
	var norm float64
	for _, token := range embedTokens(text) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(token))
		sum := h.Sum64()
		idx := int(sum % uint64(embeddingDim))
		// Het tekenbit maakt botsingen minder gunstig dan bij een
		// alleen-optellende hash: twee verschillende woorden op dezelfde as
		// versterken elkaar niet stelselmatig.
		if sum&(1<<63) != 0 {
			vec[idx]--
		} else {
			vec[idx]++
		}
	}
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		// Tekst zonder inhoudswoorden. Een nulvector zou pgvector's
		// cosinus-afstand op NaN zetten; één vaste as is het veilige antwoord.
		vec[0] = 1
		return vec
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

// embedTokens haalt de inhoudswoorden uit een tekst: kleine letters, ontdaan
// van leestekens, korte functiewoorden weggelaten, elk woord één keer.
func embedTokens(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) < embedMinTokenRunes || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// cosine is de maat waarop de kennisgraaf zoekt; de test hieronder gebruikt
// hem om de eigenschappen van de stub-embedder vast te leggen.
func cosine(a, b []float64) float64 {
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

// TestStubEmbedding legt vast wat de rest van de e2e van de stub-embedder
// aanneemt. Gaat deze test stuk, dan zeggen de bron- en kennisgat-scenario's
// niets meer: die leunen erop dat gelijke tekst dicht bij elkaar ligt en
// onverwante tekst niet.
func TestStubEmbedding(t *testing.T) {
	unit := func(v []float64) float64 {
		var n float64
		for _, x := range v {
			n += x * x
		}
		return math.Sqrt(n)
	}

	samenvatting := stubEmbedding(kbSummaryNesting)
	if len(samenvatting) != embeddingDim {
		t.Fatalf("dimensie = %d, want %d", len(samenvatting), embeddingDim)
	}
	if n := unit(samenvatting); math.Abs(n-1) > 1e-9 {
		t.Errorf("lengte = %v, want 1 (L2-genormaliseerd)", n)
	}

	if got := cosine(samenvatting, stubEmbedding(kbSummaryNesting)); math.Abs(got-1) > 1e-9 {
		t.Errorf("cosinus met zichzelf = %v, want 1", got)
	}

	// De import embedt samenvatting + gegrond citaat; de vraag is alleen de
	// samenvatting. Die twee moeten ruim boven de drempel (0,55) liggen.
	metCitaat := stubEmbedding(kbSummaryNesting + "\n\n" + kbQuoteNesting)
	if got := cosine(samenvatting, metCitaat); got < 0.7 {
		t.Errorf("cosinus samenvatting↔embed-tekst = %v, want ≥ 0,7", got)
	}

	// Onverwante tekst: de systemische vraag deelt geen inhoudswoord met welk
	// standpunt dan ook en hoort ver onder de drempel te blijven.
	systemisch := stubEmbedding("Wat is de zin van het bestaan van de gemeente?")
	for naam, tekst := range map[string]string{
		"nestelen": kbSummaryNesting,
		"voeren":   kbSummaryFeeding,
	} {
		if got := cosine(systemisch, stubEmbedding(tekst)); got >= 0.3 {
			t.Errorf("cosinus systemische vraag↔%s = %v, want < 0,3", naam, got)
		}
	}

	// De drie standpunten zijn onderling geschreven om niet op elkaar te
	// lijken: anders zou één vraag ze alle drie als bron opleveren.
	if got := cosine(stubEmbedding(kbSummaryNesting), stubEmbedding(kbSummaryFeeding)); got >= 0.3 {
		t.Errorf("cosinus tussen twee standpunten = %v, want < 0,3", got)
	}

	// Tekst zonder inhoudswoorden levert een geldige (niet-nul) vector op.
	if n := unit(stubEmbedding("de en op")); math.Abs(n-1) > 1e-9 {
		t.Errorf("lengte van een lege-tokenvector = %v, want 1", n)
	}
}
