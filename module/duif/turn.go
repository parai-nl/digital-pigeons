package duif

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/parai-nl/parai-core/llm"
)

// turnRequest is de request-body van POST /v1/chat/turn.
type turnRequest struct {
	SessionID string `json:"sessionId"`
	PigeonID  string `json:"pigeonId"`
	Message   string `json:"message"`
	Lang      string `json:"lang"`
}

// turnError is een fout vóór de stream start: HTTP-status + contractbody.
type turnError struct {
	status int
	body   apiError
}

// badRequest bouwt de 400-body bij een verzoek dat niet klopt. De reden kiest
// de tweetalige zin uit badRequestMessages; een reden zonder zin valt terug op
// de algemene melding, zodat er nooit een leeg message-slot op de wire staat.
func badRequest(reason badRequestReason) *turnError {
	msg, ok := badRequestMessages[reason]
	if !ok {
		msg = errorMessages[ReasonInvalidRequest]
	}
	return &turnError{status: http.StatusBadRequest, body: apiError{Error: ReasonInvalidRequest, Message: msg}}
}

// preparedTurn is de uitkomst van de init→retrieval→prompt-stappen, klaar om
// te streamen.
type preparedTurn struct {
	session ChatSession
	lang    string
	status  KBStatus
	sources []Source
	// dropped meldt de gevonden kandidaten die alsnog geen bron werden
	// Hij hoort bij deze beurt en gaat niet de opslag in.
	dropped  *SourceDrops
	messages []llm.Message
	// Kept separately from every rendered prompt and model-generated summary.
	humanContext []string

	// persona is de duif van deze sessie; engine is haar motor (de
	// genormaliseerde Persona.Engine). De motor zelf zit in start.
	persona *Persona
	engine  string
	// exchanges zijn de eerdere vraag-en-antwoordparen van de duif uit deze
	// sessie. Alleen de agent-motor vult ze: hij geeft ze verbatim als
	// ambient aan de facilitator en als eigen historie aan de duif.
	exchanges []pigeonExchange
	// history is het historievenster van de agent-motor mét de stem erbij, en
	// messages is er de kale vorm van plus de nieuwe vraag. De duif-subprompt
	// leest history: alleen daar staat welke rij een mensbericht is en welke
	// het geciteerde antwoord van een andere duif.
	history []promptLine

	// start is de laatste stap vóór de headers, motorspecifiek: de pipeline
	// opent hier zijn LLM-stream (een weigering van de proxy is dan nog een
	// 503), de agent-motor heeft niets meer te doen. Wat hij teruggeeft is de
	// beurt zelf.
	start func(ctx context.Context) (turnRunner, *turnError)
}

// turnRunner streamt één beurt naar de client en sluit hem af. Hij is de
// enige vorm waarin een chatmotor buiten zijn eigen bestand bestaat: de
// handler kent geen motornamen meer.
type turnRunner func(ctx context.Context, emit func(any) error)

// chatEngine is één chatmotor: hoeveel historie hij nodig heeft, en hoe hij
// zich op de stream voorbereidt.
type chatEngine struct {
	// window is het aantal historieberichten dat deze motor meekrijgt.
	window int
	// prepare bouwt de berichtenlijst en zet preparedTurn.start.
	prepare func(pt *preparedTurn, in turnInput) *turnError
}

// turnInput is wat een motor nog nodig heeft bovenop de preparedTurn: het
// historievenster, de vraag van deze beurt en (alleen voor de pipeline) de
// gerenderde bronnensectie.
type turnInput struct {
	history    []ChatMessage
	question   string
	ragSection string
}

// engineFor is de enige plek waar de motoren uiteenlopen. De set is gesloten:
// een onbekende naam faalt hier zichtbaar in plaats van stilletjes in de
// pipeline te vallen. (LoadPersonas weigert hem al bij het booten; dit is de
// vangrail voor een waarde die daarna nog gezet wordt.)
func (m *Module) engineFor(engine string) (chatEngine, bool) {
	switch engine {
	case EnginePipeline:
		return chatEngine{window: m.historyWindow, prepare: m.preparePipeline}, true
	case EngineAgent:
		return chatEngine{window: m.agentHistoryWindow, prepare: m.prepareAgent}, true
	}
	return chatEngine{}, false
}

// SSE-frames. `type` is de discriminator.
type sourcesFrame struct {
	Type string `json:"type"`
	// ID is het artifact waar de bronnen bij horen. Leeg op de pipeline: daar
	// is er één antwoord en horen ze bij de beurt zelf.
	ID      string   `json:"id,omitempty"`
	Sources []Source `json:"sources"`
}

type textFrame struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
}

type errorFrame struct {
	Type    string            `json:"type"`
	Error   string            `json:"error"`
	Message map[string]string `json:"message"`
}

type doneFrame struct {
	Type      string   `json:"type"`
	SessionID string   `json:"sessionId"`
	KBStatus  KBStatus `json:"kbStatus"`
	Sources   []Source `json:"sources,omitempty"`
	// StopReason zegt waaróm de beurt eindigde (natural | max_iter |
	// tool_error | truncated | error | …, de gesloten set van pkg/agent).
	// De agent-motor vult hem altijd; de pipeline alleen als de beurt anders
	// dan normaal eindigde — een veld dat altijd "natural" zegt voegt niets
	// toe, "error" en "truncated" wel.
	StopReason string `json:"stopReason,omitempty"`
	// Dropped meldt dat er gevonden kandidaten wegvielen vóór deze bronnenlijst
	// Hij hangt aan de beurt en niet aan het opgeslagen bericht: bij
	// herladen zou hij een uitkomst beschrijven die niet meer te controleren is.
	Dropped *SourceDrops `json:"dropped,omitempty"`
}

// prepareTurn voert de stappen vóór de stream uit: retrieval
// (embed → rag → gate → serialize) → init (sessie + historie + usermsg
// opslaan) → prompt.
//
// De retrieval gaat vóór de eerste schrijfactie, en dat is de hele volgorde.
// Een kennisbank die wegvalt geeft 503 kb_unavailable; gebeurde dat ná het
// aanmaken van de sessie, dan bleef er een lege sessie met een onbeantwoord
// userbericht achter — en de client kreeg geen sessionId om hem ooit terug te
// vinden. De prijs is dat een verzoek met een onbekende sessionId één
// embed-call kost vóór de 404. Dat is de goedkopere fout van de twee.
func (m *Module) prepareTurn(ctx context.Context, userID string, req turnRequest) (*preparedTurn, *turnError) {
	persona, ok := m.personas.Get(req.PigeonID)
	if !ok || !persona.Chat {
		return nil, badRequest(reasonUnknownChatPigeon)
	}
	if req.Lang != "" && req.Lang != "en" && req.Lang != "nl" {
		return nil, badRequest(reasonUnsupportedLang)
	}
	n := len([]rune(req.Message))
	if n < 1 || n > 4096 {
		return nil, badRequest(reasonMessageLength)
	}
	if m.llm == nil {
		return nil, &turnError{status: http.StatusServiceUnavailable, body: errorBody(ReasonLLMUnavailable)}
	}
	lang := normLang(req.Lang)

	// retrieval: de gedeelde ingang van chat en vergadering, vóór elke write.
	pt := &preparedTurn{lang: lang, status: KBPersonaOnly, persona: persona, engine: persona.EngineOr()}
	engine, ok := m.engineFor(pt.engine)
	if !ok {
		return nil, m.internalError("chatmotor kiezen",
			fmt.Errorf("duif: persona %q heeft onbekende engine %q", persona.ID, pt.engine))
	}
	// De pipeline zoekt hier, op de vraag zoals de mens hem stelde. De
	// agent-motor zoekt pas in ask_pigeon, op de vraag zoals de facilitator
	// hem aanscherpt — dat is het hele punt van die motor voor een
	// kennisbank-duif. Een kennisbank die daar wegvalt is een tool-fout met
	// reden-code, geen 503: de stream is dan al open.
	var ragSection string
	if persona.Kennisbank && pt.engine == EnginePipeline {
		res, terr := m.runRetrieval(ctx, req.Message)
		if terr != nil {
			return nil, terr
		}
		pt.status, pt.sources, ragSection = res.Status, res.Sources, res.RAGSection
		pt.dropped = res.Dropped
	}

	// init: sessie
	now := time.Now()
	if req.SessionID == "" {
		pt.session = ChatSession{ID: newID(), UserID: userID, PigeonID: req.PigeonID, Lang: lang, CreatedAt: now, UpdatedAt: now}
		if err := m.repo.CreateSession(ctx, pt.session); err != nil {
			return nil, m.internalError("sessie aanmaken", err)
		}
	} else {
		s, err := m.repo.GetSession(ctx, req.SessionID, userID)
		if err != nil {
			return nil, &turnError{status: http.StatusNotFound, body: apiError{
				Error:   "session_not_found",
				Message: map[string]string{"en": "Chat session not found.", "nl": "Chatsessie niet gevonden."},
			}}
		}
		pt.session = *s
		// Een bestaande sessie is aan één persona gebonden: een turn met een
		// andere pigeonId zou de verkeerde persona de historie voeren en het
		// gesprek fout attribueren.
		if req.PigeonID != pt.session.PigeonID {
			return nil, badRequest(reasonPigeonSessionMismatch)
		}
		// De sessietaal is leidend voor de prompts. Wisselt de gebruiker
		// halverwege van UI-taal, dan wisselen de knoppen en de meldingen mee
		// maar blijft het gesprek in de taal waarin het begon: een duif die
		// midden in een gesprek van taal verspringt is geen persona meer.
		if pt.session.Lang != "" {
			pt.lang = normLang(pt.session.Lang)
		}
	}

	// init: historievenster (vóór het nieuwe userbericht wordt opgeslagen).
	// Het venster is motorafhankelijk: een agent-beurt schrijft twee tot vier
	// rijen waar de pipeline er twee schrijft, en zonder ruimer venster zou
	// het schema stilzwijgend bepalen hoeveel gesprek de facilitator ziet.
	history, err := m.repo.ListMessages(ctx, pt.session.ID, engine.window)
	if err != nil {
		return nil, m.internalError("historie laden", err)
	}
	if err := m.repo.AppendMessage(ctx, ChatMessage{
		ID: newID(), SessionID: pt.session.ID, Role: "user", Content: req.Message,
		Kind: KindText, CreatedAt: now,
	}); err != nil {
		return nil, m.internalError("userbericht opslaan", err)
	}

	// prompt + messages: motorspecifiek, inclusief wat er nog vóór de headers
	// mag falen.
	return pt, engine.prepare(pt, turnInput{history: history, question: req.Message, ragSection: ragSection})
}

// preparePipeline bouwt de directe spreekbeurt: systemprompt, historie en de
// nieuwe vraag, met een runner die de LLM-stream vlak vóór de headers opent —
// zo is een weigering van de proxy (budgetcap) daar nog een 503.
func (m *Module) preparePipeline(pt *preparedTurn, in turnInput) *turnError {
	pt.humanContext = chatHumanContext(in)
	system := BuildSystemPrompt(PromptInput{Persona: pt.persona, Lang: pt.lang, Status: pt.status, RAGSection: in.ragSection})
	history := pipelineHistory(in.history, m.voicesFor(pt))
	pt.messages = make([]llm.Message, 0, len(history)+2)
	pt.messages = append(pt.messages, llm.Message{Role: "system", Content: system})
	pt.messages = append(pt.messages, history...)
	pt.messages = append(pt.messages, llm.Message{Role: "user", Content: in.question})
	pt.start = func(ctx context.Context) (turnRunner, *turnError) {
		ch, terr := m.startLLMStream(ctx, pt)
		if terr != nil {
			return nil, terr
		}
		return func(ctx context.Context, emit func(any) error) { m.pumpTurn(ctx, pt, ch, emit) }, nil
	}
	return nil
}

// prepareAgent bouwt de facilitator-beurt. Zijn berichtenlijst draagt geen
// systemprompt: pkg/agent zet die per iteratie zelf vooraan (Config.System).
// De duif-antwoorden zitten er evenmin in — die gaan verbatim als ambient mee.
//
// Anders dan de pipeline valt hier niets meer vóór de headers: RunStream geeft
// alleen configfouten synchroon terug, dus een budgetfout is op dit pad altijd
// een error-frame ná de 200.
func (m *Module) prepareAgent(pt *preparedTurn, in turnInput) *turnError {
	pt.humanContext = chatHumanContext(in)
	prose, exchanges := splitHistory(in.history, m.voicesFor(pt))
	pt.exchanges, pt.history = exchanges, prose
	pt.messages = make([]llm.Message, 0, len(prose)+1)
	for _, line := range prose {
		pt.messages = append(pt.messages, line.Message)
	}
	pt.messages = append(pt.messages, llm.Message{Role: "user", Content: in.question})
	pt.start = func(context.Context) (turnRunner, *turnError) {
		return func(ctx context.Context, emit func(any) error) { m.runAgentTurn(ctx, pt, emit) }, nil
	}
	return nil
}

// Chat history is already scoped to this owned conversation. A comparison
// branch also carries the first pigeon's answers, and those stay hers: only
// rows this person wrote are human input. Copied pigeon answers, assistant
// questions, facilitator summaries and system scaffolding never gain human
// authorship, so nothing downstream reads another model's words as if a
// person had said them.
func chatHumanContext(in turnInput) []string {
	var context []string
	for _, message := range in.history {
		if message.Role == "user" && message.Content != "" {
			context = append(context, message.Content)
		}
	}
	return append(context, in.question)
}

// promptVoices weet van wie de woorden in een historierij zijn wanneer de
// prompt van één duif wordt gebouwd. Sinds een vergelijking het hele
// brongesprek meeneemt, staan er antwoorden van een ándere duif in de
// historie; die mogen nooit als eigen eerdere beurt terugkomen.
type promptVoices struct {
	// self is de duif die nu aan het woord komt.
	self string
	lang string
	// name geeft de weergavenaam van een andere duif in de promptaal; nil of
	// een lege uitkomst laat de id staan in plaats van een naam te verzinnen.
	name func(id string) string
}

func (m *Module) voicesFor(pt *preparedTurn) promptVoices {
	lang := pt.lang
	return promptVoices{self: personaID(pt.persona), lang: lang, name: func(id string) string { return m.personaName(id, lang) }}
}

// personaName geeft de weergavenaam van een duif; een onbekende id houdt zijn
// eigen identifier.
func (m *Module) personaName(id, lang string) string {
	if p, ok := m.personas.Get(id); ok {
		if name := pickLang(p.Naam, lang); name != "" {
			return name
		}
	}
	return id
}

// other geeft de andere duif achter een assistant-rij, of "" wanneer de rij
// van deze duif zelf is. Een rij zonder persona komt uit een sessie van vóór
// de personakolom: dat was de duif van dat gesprek zelf, dus haar eigen beurt.
// Een gekopieerd antwoord uit een vergelijking draagt wél altijd een persona
// (comparisonContext stempelt hem), en die is per definitie een andere duif.
func (v promptVoices) other(h ChatMessage) string {
	if h.Role != "assistant" || isFacilitatorRow(h) || h.Persona == "" || h.Persona == v.self {
		return ""
	}
	return h.Persona
}

// quote zet het antwoord van een andere duif in de prompt als uitspraak van
// die duif: een mensbericht met de naam ervoor. Als assistant-rij zou het
// model het lezen als iets wat het zelf gezegd heeft, en dan neemt de
// individualist het standpunt van de kennispartner over als het hare.
func (v promptVoices) quote(persona, answer string) llm.Message {
	name := persona
	if v.name != nil {
		if resolved := v.name(persona); resolved != "" {
			name = resolved
		}
	}
	if normLang(v.lang) == "nl" {
		return llm.Message{Role: "user", Content: name + " antwoordde eerder: “" + answer + "”"}
	}
	return llm.Message{Role: "user", Content: name + " answered earlier: “" + answer + "”"}
}

// promptLine is één historierij zoals de prompt hem draagt, mét de stem erbij.
// De facilitator ziet de rijen op hun plek in het gesprek; de duif-subprompt
// heeft het onderscheid nodig, want het geciteerde antwoord van een andere duif
// staat als mensbericht in de prompt zonder mensinvoer te zijn.
type promptLine struct {
	llm.Message
	// otherPigeon markeert het geciteerde antwoord van een andere duif.
	otherPigeon bool
}

// splitHistory verdeelt het historievenster over de stemmen van een gesprek:
// prose is wat de mens en de facilitator schreven (plus wat een andere duif
// zei), exchanges is wat déze duif zei met de vraag die erbij hoorde.
//
// De partitie gaat op stem (persona) en niet op soort (kind), en dat is het
// hele punt. Een assistant-rij zonder persona komt uit een sessie van vóór de
// facilitator: dat was de duif die direct antwoordde, en die woorden zijn van
// haar. Op kind filteren zou al die oude gesprekken aan de facilitator
// toeschrijven — precies de rolvermenging die eis:F2 moet bewaken — en de
// motor bovendien niet terugschakelbaar maken.
//
// Lege berichten (bv. een assistantbericht na disconnect vóór de eerste chunk)
// blijven overal buiten: providers achter de proxy weigeren messages met lege
// content.
func splitHistory(history []ChatMessage, voices promptVoices) (prose []promptLine, exchanges []pigeonExchange) {
	for _, h := range history {
		if h.Content == "" {
			continue
		}
		if other := voices.other(h); other != "" {
			prose = append(prose, promptLine{Message: voices.quote(other, historyAnswer(h.Content)), otherPigeon: true})
			continue
		}
		if h.Role == "assistant" && !isFacilitatorRow(h) {
			exchanges = append(exchanges, pigeonExchange{Question: h.Question, Answer: historyAnswer(h.Content)})
			continue
		}
		prose = append(prose, promptLine{Message: llm.Message{Role: h.Role, Content: h.Content}})
	}
	return prose, exchanges
}

// pipelineHistory geeft de historie zoals de duif zelf hem beleeft: de vragen
// van de mens en haar eigen antwoorden, oud (kind text, zonder persona) én uit
// een facilitator-beurt (kind pigeon_answer). Alleen de facilitator-tekst
// blijft eruit — dat zijn zijn woorden, en ze teruggeven zou de duif laten
// denken dat zij ze schreef. Zo is engine: agent veilig terug te zetten. Het
// antwoord van een andere duif gaat mee als haar uitspraak, niet als een
// eerdere beurt van deze duif.
func pipelineHistory(history []ChatMessage, voices promptVoices) []llm.Message {
	out := make([]llm.Message, 0, len(history))
	for _, h := range history {
		if h.Content == "" || isFacilitatorRow(h) {
			continue
		}
		if other := voices.other(h); other != "" {
			out = append(out, voices.quote(other, historyAnswer(h.Content)))
			continue
		}
		content := h.Content
		if h.Role == "assistant" {
			content = historyAnswer(content)
		}
		out = append(out, llm.Message{Role: h.Role, Content: content})
	}
	return out
}

// isFacilitatorRow zegt of een historie-rij van de facilitator is. Een
// assistant-rij zónder persona komt uit een sessie van vóór hem: dat was de
// duif die direct antwoordde, en die woorden zijn van haar.
func isFacilitatorRow(h ChatMessage) bool {
	return h.Role == "assistant" && h.Persona == PersonaFacilitator
}

// personaID geeft de id van een (mogelijk niet gezette) persona. pumpTurn
// draait ook in tests met een kale preparedTurn.
func personaID(p *Persona) string {
	if p == nil {
		return ""
	}
	return p.ID
}

// runRetrieval draait de retrieval voor een kennisbank-duif. Zonder gewirede
// retrieval (geen embedder of geen kennisbank bij het opstarten) blijft de duif
// in gap-modus: dat is een configuratiestand, geen storing — zie Init, en de
// nil-receiver van run. Een storing tijdens de beurt is wél een storing: 503
// met kb_unavailable, geen LLM-beurt.
func (m *Module) runRetrieval(ctx context.Context, query string) (retrievalResult, *turnError) {
	res, err := m.retrieval.run(ctx, query)
	if err != nil {
		return retrievalResult{}, &turnError{
			status: http.StatusServiceUnavailable,
			body:   errorBody(ReasonKBUnavailable),
		}
	}
	return res, nil
}

// startLLMStream opent de completion-stream. Een synchrone fout (bv. de
// budgetweigering van de proxy) is een fout vóór de stream: de handler antwoordt
// dan met de foutbody van het REST-contract (apiError).
func (m *Module) startLLMStream(ctx context.Context, pt *preparedTurn) (<-chan llm.StreamChunk, *turnError) {
	ch, err := openAnswerStream(ctx, m.llm, llm.CompletionRequest{
		Model:           m.model,
		Messages:        pt.messages,
		Stream:          true,
		MaxTokens:       answerChatMaxTokens,
		ReasoningEffort: chatReasoning(),
	}, pt.sources, answerPolicy{Persona: pt.persona, Lang: pt.lang, HumanContext: pt.humanContext}, m.answerStream)
	if err != nil {
		reason := classifyLLMError(err)
		m.log().Error("llm-stream start faalde", "component", "duif", "reason", reason, "error", err)
		return nil, &turnError{status: http.StatusServiceUnavailable, body: errorBody(reason)}
	}
	return ch, nil
}

// pumpTurn streamt chunks naar emit-frames en persisteert het (eventueel
// partiële) assistantbericht — óók bij client-disconnect (detached context:
// geen dataverlies, geen zombie-stream).
//
// Iedere delta wordt aan de bronlijst van deze uiting getoetst vóór emit en
// persist. Zonder bronnen blijven geen markeringen achter; verkeerde nummers
// worden ook bij een gedeeltelijk onderbouwd antwoord verwijderd.
func (m *Module) pumpTurn(ctx context.Context, pt *preparedTurn, ch <-chan llm.StreamChunk, emit func(any) error) {
	if pt.status == KBSources {
		_ = emit(sourcesFrame{Type: "sources", Sources: pt.sources})
	}
	strip := citationFilter(pt.sources)
	var full []byte
	partial := false
	// expired scheidt de harde tijdgrens van de server (chatTurnBudget) van een
	// weggelopen lezer. Alleen de eerste verdient een melding: die lezer zit er
	// nog en zou anders op een stille stream blijven wachten.
	expired := false
	// stopReason blijft leeg bij een normaal einde. Sluit de provider af op de
	// tokengrens of op zijn eigen inhoudsfilter, dan komt daar "truncated" of
	// "filtered" te staan: de tekst gaat ongewijzigd naar de lezer en de
	// opslag, maar de beurt meldt niet langer dat zij normaal eindigde.
	stopReason := ""
loop:
	for {
		select {
		case <-ctx.Done():
			partial, expired = true, errors.Is(ctx.Err(), context.DeadlineExceeded)
			break loop
		case chunk, ok := <-ch:
			// ctx-cancel heeft voorrang: een gebufferd kanaal kan de select
			// anders blijven winnen ná een client-disconnect.
			if ctx.Err() != nil {
				partial, expired = true, errors.Is(ctx.Err(), context.DeadlineExceeded)
				break loop
			}
			if !ok {
				break loop
			}
			if chunk.Err != nil {
				reason := classifyLLMError(chunk.Err)
				m.log().Error("llm-stream brak af", "component", "duif", "reason", reason, "error", chunk.Err)
				fr := errorFrame{Type: "error", Error: reason, Message: errorBody(reason).Message}
				_ = emit(fr)
				partial = true
				break loop
			}
			if reason := providerStop(chunk.FinishReason); reason != "" {
				stopReason = reason
			}
			delta := chunk.Content
			if strip != nil {
				delta = strip.write(delta)
			}
			if delta != "" {
				full = append(full, delta...)
				if err := emit(textFrame{Type: "text", Delta: delta}); err != nil {
					partial = true
					break loop
				}
			}
		}
	}
	// Wat de stripper nog vasthield (een blokhaak die nooit gesloten werd) is
	// gewone tekst en hoort er alsnog uit.
	if strip != nil {
		if tail := strip.flush(); tail != "" {
			full = append(full, tail...)
			if !partial {
				if err := emit(textFrame{Type: "text", Delta: tail}); err != nil {
					partial = true
				}
			}
		}
	}

	// De reden gaat de deur uit vóór de opslag: die heeft een eigen begroting
	// van tien seconden, en de schrijfmarge van de verbinding (turnWriteGrace)
	// is niet zo ruim dat een trage database hem mag opeten. Het done-frame
	// hieronder blijft ná de opslag staan — een bewaard antwoord eerst, zoals
	// bij elke andere beurt.
	if expired {
		m.log().Warn("beurt bereikte de tijdgrens", "component", "duif", "reason", ReasonTurnTimeout, "tekens", len(full))
		_ = emit(errorFrame{Type: "error", Error: ReasonTurnTimeout, Message: errorBody(ReasonTurnTimeout).Message})
	}

	// persist met detached context: client-disconnect verliest niets. Nul
	// gestreamde content (disconnect of error-frame vóór de eerste chunk)
	// wordt niet opgeslagen: een leeg assistantbericht zou bij de volgende
	// turn als lege message de LLM in gaan en de sessie stukmaken.
	if len(full) > 0 {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		msg := ChatMessage{
			ID: newID(), SessionID: pt.session.ID, Role: "assistant", Content: string(full),
			KBStatus: string(pt.status), Sources: pt.sources, Partial: partial, CreatedAt: time.Now(),
			Kind: KindText, Persona: personaID(pt.persona), StopReason: stopReason,
		}
		if err := m.repo.AppendMessage(persistCtx, msg); err != nil {
			m.log().Error("assistantbericht opslaan faalde", "component", "duif", "error", err)
		}
	}

	done := doneFrame{Type: "done", SessionID: pt.session.ID, KBStatus: pt.status, StopReason: stopReason}
	if partial {
		// Een gebroken stroom overschrijft de reden van de provider: de lezer
		// heeft dan een foutbanner nodig, niet de mededeling dat het antwoord
		// halverwege stopt.
		done.StopReason = "error"
	}
	if pt.status == KBSources {
		done.Sources = pt.sources // done herhaalt de bronnenlijst
	}
	// En wat er niet in die lijst kwam. Ook bij een gat: juist dan is "er was
	// wél iets gevonden, maar het viel af" wat de lezer mist.
	done.Dropped = pt.dropped
	_ = emit(done)
}

// citationStripper haalt citatiemarkeringen uit een tekststroom. Hij volgt
// dezelfde regel als reCitation in meeting_speech.go — blokhaken met cijfers,
// eventueel door komma's gescheiden — maar dan als toestandsmachine, omdat een
// markering over een chunkgrens heen geknipt kan zijn ("… erbij [1" + "2].").
//
// Witruimte vóór een markering wordt vastgehouden tot duidelijk is wat erna
// komt: zonder dat blijft er "erbij ." staan waar "erbij [1]." stond.
type citationStripper struct {
	allowed map[int]bool // nil verwijdert alle markers; anders uitsluitend echte bronnummers
	// space is de nog niet uitgegeven witruimte.
	space []rune
	// bracket is de inhoud vanaf een geopende blokhaak, inclusief de haak zelf.
	bracket []rune
	// dropped zegt dat de laatste uitgave een markering wegliet; alleen dan mag
	// de vastgehouden witruimte vóór een leesteken verdwijnen.
	dropped bool
}

// citationFilter bindt elke gestreamde marker aan de bronlijst van die uiting.
// Dit bewijst nummermapping, niet dat de bron de inhoudelijke claim draagt.
func citationFilter(sources []Source) *citationStripper {
	c := &citationStripper{allowed: make(map[int]bool, len(sources))}
	for _, source := range sources {
		if source.Number > 0 {
			c.allowed[source.Number] = true
		}
	}
	return c
}

func (c *citationStripper) validMarker() string {
	var kept []string
	seen := map[int]bool{}
	for _, part := range strings.Split(string(c.bracket[1:]), ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && c.allowed[n] && !seen[n] {
			kept = append(kept, strconv.Itoa(n))
			seen[n] = true
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "[" + strings.Join(kept, ", ") + "]"
}

// citationPunct zijn de leestekens die direct achter een weggehaalde markering
// mogen aanschuiven.
var citationPunct = map[rune]bool{'.': true, ',': true, ';': true, ':': true, '!': true, '?': true, '…': true}

// write geeft terug wat er van deze delta uitgezonden mag worden.
func (c *citationStripper) write(s string) string {
	var b strings.Builder
	for _, r := range s {
		c.step(&b, r)
	}
	return b.String()
}

// step verwerkt één teken.
func (c *citationStripper) step(b *strings.Builder, r rune) {
	if len(c.bracket) > 0 {
		switch {
		case r == ']':
			if bracketIsCitation(c.bracket) {
				marker := c.validMarker()
				c.bracket = nil
				if marker != "" {
					c.emitSpace(b, '[')
					b.WriteString(marker)
					c.dropped = false
				} else {
					c.dropped = true
				}
				return
			}
			// Blokhaken zonder cijfer zijn gewone tekst ("[zie kader]").
			c.emitSpace(b, '[')
			b.WriteString(string(c.bracket))
			b.WriteRune(r)
			c.bracket, c.dropped = nil, false
			return
		case unicode.IsDigit(r), r == ',', unicode.IsSpace(r):
			c.bracket = append(c.bracket, r)
			return
		default:
			// Wat hier binnenkomt hoort niet in een markering: de haak was
			// gewone tekst en dit teken wordt hieronder opnieuw beoordeeld.
			c.emitSpace(b, '[')
			b.WriteString(string(c.bracket))
			c.bracket, c.dropped = nil, false
		}
	}
	switch {
	case r == '[':
		c.bracket = append(c.bracket, r)
	case unicode.IsSpace(r):
		// Na een weggehaalde markering blijft er hooguit één stuk witruimte
		// over; anders wordt "erbij [1] en" tot "erbij  en".
		if c.dropped && len(c.space) > 0 {
			return
		}
		c.space = append(c.space, r)
	default:
		c.emitSpace(b, r)
		b.WriteRune(r)
		c.dropped = false
	}
}

// emitSpace geeft de vastgehouden witruimte uit, tenzij zij tussen een
// weggehaalde markering en een leesteken staat.
func (c *citationStripper) emitSpace(b *strings.Builder, next rune) {
	if len(c.space) > 0 && !(c.dropped && citationPunct[next]) {
		b.WriteString(string(c.space))
	}
	c.space = nil
}

// bracketIsCitation zegt of het geopende haakje een citatienummer draagt. De
// inhoud kan hier alleen uit cijfers, komma's en spaties bestaan; er moet
// minstens één cijfer in zitten.
func bracketIsCitation(bracket []rune) bool {
	for _, r := range bracket {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// flush geeft gewone open blokhaaktekst uit. Een onafgeronde numerieke citatie
// verdwijnt: daarvoor bestaat geen bruikbare bronactie.
// Losse witruimte aan het slot verdwijnt: dat is óf de spatie vóór een
// weggehaalde markering, óf een spatie waar niets meer achter kwam.
func (c *citationStripper) flush() string {
	var b strings.Builder
	if len(c.bracket) > 0 && !bracketIsCitation(c.bracket) {
		b.WriteString(string(c.space))
		b.WriteString(string(c.bracket))
	}
	c.space, c.bracket, c.dropped = nil, nil, false
	return b.String()
}

// Historical citation numbers are local to their original answer. Never lend
// those numbers to a later turn's different source list; stored/UI text stays
// unchanged, while model history is conversational context, not fresh evidence.
func historyAnswer(text string) string {
	c := &citationStripper{}
	return c.write(text) + c.flush()
}
