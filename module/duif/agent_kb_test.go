package duif

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De facilitator met een onderzoeksduif. ask_pigeon zoekt op de originele
// mensvraag en draagt zijn eigen vangnet: een herschrijving (rephrased_related)
// of een treffer van een eerdere vraag uit dezelfde beurt (search_related).
// De facilitator heeft geen eigen zoektool.
// Dit bestand toetst die routering met gescripte providers, geen live gedrag.

// kbAgentModule bouwt een module met een gevulde kennisgraaf, een gescripte
// LLM en de kennispartner zoals personas.yaml hem levert: op de facilitator.
func kbAgentModule(t *testing.T, seeds []kbSeed, script []llmtesting.StreamStep, tune func(m *Module)) (*Module, *fakeRepo, *llmtesting.MockService, *llmtesting.MockEmbedder) {
	t.Helper()
	mock := llmtesting.NewMockService()
	mock.SetStreamScript(script)
	f := &fakeKnowledge{}
	for _, s := range seeds {
		seedDestillaat(t, f, s)
	}
	embedder := &llmtesting.MockEmbedder{}
	m, repo := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = embedder
		m.knowledge = f
		if tune != nil {
			tune(m)
		}
	})
	if p, _ := m.personas.Get("biological"); p.EngineOr() != EngineAgent {
		t.Fatalf("de kennispartner hoort op de agent-motor te staan, kreeg %q", p.EngineOr())
	}
	return m, repo, mock, embedder
}

func askBiological() llmtesting.StreamStep {
	return toolCallStep("call-1", toolAskPigeon, `{"question":"Herkennen duiven individuele mensen?","context":""}`)
}

// Het hart: ask_pigeon zoekt op de mensvraag, de duif krijgt
// bronnen en citatie-instructie, de bronnen gaan als eigen frame aan het
// artifact gebonden de wire op, en de facilitator hoort dat zij uit bronnen
// sprak.
func TestAgentKB_AskPigeonGroundsTheAnswerInSources(t *testing.T) {
	m, repo, mock, embedder := kbAgentModule(t,
		[]kbSeed{
			{id: "9", samenvatting: "Duiven herkennen gezichten.", score: 0.9, quote: "Ze kenden mij.", paper: "lewald1987", perspective: "De kritische wetenschapper", themaTitel: "Cognitie"},
			{id: "8", samenvatting: "Duiven navigeren op magneetvelden.", score: 0.7, quote: "Ik zag ze zo terugvliegen.", paper: "walcott1978"},
		},
		[]llmtesting.StreamStep{
			askBiological(),
			textStep("Wij herkennen jullie echt [1]."),
		}, nil)

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "biological", "message": "Kennen duiven mij?", "lang": "nl"})
	// De done sluit de beurt; er komt niets meer achteraan.
	want := []string{"tool", "artifact", "sources", "artifact_delta", "artifact", "tool_result", "done"}
	if got := frameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v,\nwant %v", got, want)
	}
	open, sources, done := frames[1], frames[2], frames[6]
	if open["kbStatus"] != string(KBSources) {
		t.Errorf("artifact-open.kbStatus = %v, want sources", open["kbStatus"])
	}
	if sources["id"] != open["id"] {
		t.Errorf("sources.id = %v, want het artifact-id %v", sources["id"], open["id"])
	}
	raw, _ := json.Marshal(sources["sources"])
	var list []Source
	if err := json.Unmarshal(raw, &list); err != nil || len(list) != 2 || list[0].Number != 1 || list[0].Ref.Paper != "lewald1987" {
		t.Fatalf("bronnenlijst = %s (%v)", raw, err)
	}
	if done["kbStatus"] != string(KBSources) {
		t.Errorf("done.kbStatus = %v, want sources", done["kbStatus"])
	}
	// De bronnen horen bij het duif-blok, niet bij de facilitator-rij: done
	// herhaalt ze hier niet (dat is het pipeline-contract, waar de beurt zelf
	// het antwoord is).
	if _, has := done["sources"]; has {
		t.Errorf("done hoort op de facilitator geen bronnen te dragen: %v", done["sources"])
	}
	if frames[3]["delta"] != "Wij herkennen jullie echt [1]." {
		t.Errorf("in bronnenmodus blijft de citatiemarkering staan: %v", frames[3]["delta"])
	}

	// De duif kreeg de bronnen en de citatie-instructie; de facilitator vier
	// tools — geen eigen zoektool, want ask_pigeon zoekt zelf.
	if len(mock.Calls[0].Tools) != 4 {
		t.Errorf("de facilitator van een kennisbank-duif hoort vier tools te krijgen, kreeg %d", len(mock.Calls[0].Tools))
	}
	duifPrompt := mock.Calls[1].Messages[0].Content
	for _, want := range []string{ragContextHeader["nl"], citationInstruction["nl"], "Ze kenden mij."} {
		if !strings.Contains(duifPrompt, want) {
			t.Errorf("de systemprompt van de duif mist %q", want)
		}
	}
	// Twee betaalde aanroepen: de facilitator en de duif. Een afgerond
	// duif-antwoord heeft geen betaalde facilitator-slotronde nodig, en ook geen
	// auditcall achteraf: de bronnen zijn bij de import al gecontroleerd.
	if mock.CallCount() != 2 {
		t.Fatalf("a completed pigeon answer needs no paid facilitator closing or audit call: %d", mock.CallCount())
	}
	// Eén embed: in ask_pigeon, niet nog eens vóór de stream.
	if embedder.CallCount() != 1 {
		t.Errorf("embed-aanroepen = %d, want 1 (alleen in ask_pigeon)", embedder.CallCount())
	}

	// Het duif-blok bewaart zijn bronnen, zodat herladen dezelfde kaarten toont.
	sid := done["sessionId"].(string)
	msgs, err := repo.ListMessages(t.Context(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Kind != KindPigeonAnswer {
		t.Fatalf("berichten = %+v", msgs)
	}
	if msgs[1].KBStatus != string(KBSources) || len(msgs[1].Sources) != 2 {
		t.Errorf("duif-rij = kbStatus %q, %d bronnen; want sources met 2", msgs[1].KBStatus, len(msgs[1].Sources))
	}
	// Biological support stays internal: no facilitator summary is stored.
}

// Een kennisgat blijft een kennisgat op de facilitator: geen bronnenframe, de
// duif krijgt het gap-directief, de stripper haalt verzonnen markeringen uit
// stream én opslag, en de facilitator hoort dat het een gat was.
func TestAgentKB_GapStripsCitationsAndSaysSo(t *testing.T) {
	m, repo, mock, _ := kbAgentModule(t,
		[]kbSeed{{id: "1", samenvatting: "Te vage match.", score: 0.3, paper: "p1"}},
		[]llmtesting.StreamStep{
			askBiological(),
			textStep("Daar is geen onderzoek over [1]. Echt", " niet [2, 3]."),
			textStep("Het onderzoek zwijgt hier."),
		}, nil)

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "biological", "message": "Dromen duiven?", "lang": "nl"})
	want := []string{"tool", "artifact", "artifact_delta", "artifact_delta", "artifact", "tool_result", "done"}
	if got := frameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v,\nwant %v", got, want)
	}
	if frames[1]["kbStatus"] != string(KBGap) {
		t.Errorf("artifact-open.kbStatus = %v, want gap", frames[1]["kbStatus"])
	}
	var duif strings.Builder
	for _, f := range frames[2:4] {
		duif.WriteString(f["delta"].(string))
	}
	if duif.String() != "Daar is geen onderzoek over. Echt niet." {
		t.Errorf("duif-antwoord = %q, want zonder markeringen", duif.String())
	}
	if frames[6]["kbStatus"] != string(KBGap) {
		t.Errorf("done.kbStatus = %v, want gap", frames[6]["kbStatus"])
	}
	if !strings.Contains(mock.Calls[1].Messages[0].Content, gapDirective["nl"]) {
		t.Error("de duif hoort het kennisgat-directief te krijgen")
	}
	if mock.CallCount() != 2 {
		t.Fatalf("a completed gap answer needs no paid facilitator closing call: %d", mock.CallCount())
	}
	// De herinnering staat naast de vraag zelf, niet alleen in de systemprompt.
	if last := mock.Calls[1].Messages[len(mock.Calls[1].Messages)-1]; last.Role != "user" || !strings.Contains(last.Content, gapReminder["nl"]) {
		t.Errorf("de vraag aan de duif hoort in gap-modus de herinnering te dragen: %+v", last)
	}
	sid := frames[6]["sessionId"].(string)
	msgs, _ := repo.ListMessages(t.Context(), sid, 0)
	if len(msgs) != 2 || msgs[1].Content != "Daar is geen onderzoek over. Echt niet." || msgs[1].KBStatus != string(KBGap) {
		t.Errorf("opgeslagen duif-rij = %+v", msgs[1])
	}
}

// Valt de kennisbank weg terwijl de facilitator de duif vraagt, dan is dat een
// chip met reden en geen duif-blok — de stream is al open, dus geen 503. De
// facilitator praat gewoon verder.
func TestAgentKB_KBUnavailable_IsAToolErrorNotA503(t *testing.T) {
	m, _, mock, _ := kbAgentModule(t, nil,
		[]llmtesting.StreamStep{
			askBiological(),
			textStep("Haar kennisbank is even onbereikbaar."),
		},
		func(m *Module) { m.knowledge = &fakeKnowledge{err: errors.New("test: kb plat")} })

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Kennen duiven mij?", "lang": "nl"}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (de stream is al open)", rec.Code)
	}
	frames := parseSSE(t, rec.Body.String())
	want := []string{"tool", "tool_result", "error", "done"}
	if got := frameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v,\nwant %v (geen artifact)", got, want)
	}
	if frames[1]["error"] != ReasonKBUnavailable {
		t.Errorf("tool_result.error = %v, want %s", frames[1]["error"], ReasonKBUnavailable)
	}
	if mock.CallCount() != 1 {
		t.Errorf("LLM-aanroepen = %d, want 1 (geen duif of betaalde slotcall)", mock.CallCount())
	}
	if frames[2]["error"] != ReasonKBUnavailable || frames[3]["stopReason"] != "error" || frames[3]["kbStatus"] != string(KBUnavailable) {
		t.Errorf("de storing moet terminaal zichtbaar zijn: %+v", frames)
	}
}

// De kennisbank verandert de toolset niet: ook de onderzoeksduif krijgt de
// vier gepinde tools. Het zoeken zit volledig in ask_pigeon, dus
// de facilitator kan de beurt geen extra modelronde laten kosten om vooraf te
// kijken wat er in de kennisbank ligt.
func TestAgentRegistry_KBPigeonHasNoSearchTool(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	reg, err := newAgentRegistry(at)
	if err != nil {
		t.Fatalf("newAgentRegistry: %v", err)
	}
	var namen []string
	for _, tool := range reg.Definitions() {
		namen = append(namen, tool.Function.Name)
	}
	want := []string{toolAskPigeon, toolPigeonProfile, toolClarify, toolRedirectToPigeon}
	if strings.Join(namen, ",") != strings.Join(want, ",") {
		t.Fatalf("toolset = %v, want %v", namen, want)
	}
}

// Het biologische profiel legt uit dat de duif uit beschikbare bronnen spreekt.
func TestPigeonProfile_KBPigeonSpeaksFromResearch(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	ch := make(chan any, 8)
	res, err := at.pigeonProfile(withEmitter(t.Context(), ch), pigeonProfileArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Profile, "onderzoekskennisbank") || strings.Contains(res.Profile, "geen onderzoekskennisbank") {
		t.Errorf("profiel = %q", res.Profile)
	}
	if !strings.Contains(res.Profile, "Populatiebeheer") {
		t.Errorf("het profiel hoort de thema's te dragen: %q", res.Profile)
	}
}

// Op de facilitator zoekt de beurt níét vooraf op de vraag van de mens: dat
// zou een embed-call kosten voor een vraag die de facilitator toch aanscherpt.
func TestAgentKB_NoRetrievalBeforeTheStream(t *testing.T) {
	m, _, _, embedder := kbAgentModule(t,
		[]kbSeed{{id: "9", samenvatting: "Duiven herkennen gezichten.", score: 0.9, paper: "p"}},
		nil, nil)
	pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: "biological", Message: "Kennen duiven mij?"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	if embedder.CallCount() != 0 {
		t.Errorf("embed-aanroepen vóór de stream = %d, want 0", embedder.CallCount())
	}
	if pt.status != KBPersonaOnly || len(pt.sources) != 0 {
		t.Errorf("de beurt begint zonder stand: %q, %d bronnen", pt.status, len(pt.sources))
	}
}

// Leverde een eerdere vraag in dezelfde beurt bronnen op en geeft een volgende
// vraag aan de duif een gat (de tweede vraagzin ligt anders in de
// vectorruimte), dan krijgt de duif dat eerder gevonden onderzoek mee — mét
// bronnen en citaties, in plaats van een kennisgat over onderzoek dat er wel
// is. Vroeger vulde een aparte zoektool `search_sources` dit vangnet; nu vult
// ask_pigeon het met zijn eigen treffers, zodat het vangnet blijft zonder extra
// modelronde.
func TestAgentKB_AskPigeonFallsBackToTheEarlierHitsOfTheSameTurn(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{
		textStep("Wij dragen zelden iets over [1]."),
		textStep("Wij dragen soms iets bij ons [1]."),
	})
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "9", samenvatting: "Duiven dragen zelden ziekten over op mensen.", score: 0.9, paper: "haag2004", themaTitel: "Gezondheid"})
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = f
	})
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	ch := make(chan any, 64)
	ctx := withEmitter(t.Context(), ch)

	first, err := at.askPigeon(ctx, askPigeonArgs{Question: "Dragen duiven ziekten over op mensen?"})
	if err != nil || first.KBStatus != string(KBSources) || first.Grounding != "" {
		t.Fatalf("eerste vraag = %+v (%v), want rechtstreekse bronnen", first, err)
	}
	// Daarna scoort niets meer boven de drempel: de tweede vraag aan de duif
	// zou een gat geven.
	f.mu.Lock()
	for _, props := range f.nodes {
		props["score"] = 0.1
	}
	f.mu.Unlock()

	res, err := at.askPigeon(ctx, askPigeonArgs{Question: "Brengen jullie ziektes over op mensen, en wat betekent dat voor beleid?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.KBStatus != string(KBSources) || res.Sources != 1 || res.Grounding != "search_related" {
		t.Fatalf("resultaat = %+v, want sources uit de eerdere vraag", res)
	}
	close(ch)
	var sawSources bool
	for frame := range ch {
		switch fr := frame.(type) {
		case sourcesFrame:
			sawSources = len(fr.Sources) == 1 && fr.Sources[0].Ref.Paper == "haag2004"
		case artifactFrame:
			if !fr.Done && fr.KBStatus != KBSources {
				t.Errorf("artifact-open.kbStatus = %q, want sources", fr.KBStatus)
			}
		}
	}
	if !sawSources {
		t.Error("de eerder gevonden bronnen horen bij het duif-blok te staan")
	}
	// De eerste vraag kreeg dekking en dus geen verwante-bronnenkader; de
	// tweede wel.
	if strings.Contains(mock.Calls[0].Messages[0].Content, relatedSourcesDirective["nl"]) {
		t.Error("een rechtstreekse treffer hoort geen verwante-bronnenkader te krijgen")
	}
	if !strings.Contains(mock.Calls[1].Messages[0].Content, relatedSourcesDirective["nl"]) {
		t.Error("verwante kennis mag geen volledige dekking suggereren")
	}
	if !strings.Contains(mock.Calls[1].Messages[0].Content, "Duiven dragen zelden ziekten over op mensen.") {
		t.Error("de duif hoort het gevonden onderzoek in haar systemprompt te krijgen")
	}
}

// Valt de mensvraag net buiten het corpus, dan kijkt ask_pigeon nog één keer
// breder: wat grenst hieraan? Wat die zoekactie
// oplevert gaat als vérwante kennis naar de duif — mét bronnen, mét het
// verwante-bronnenkader, en met `related` op het artifact-frame, zodat het
// scherm kan zeggen dat dit geen dekking van de vraag is.
func TestAgentKB_GapFallsBackToABroaderRelatedSearch(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("Dat kan ik niet vaststellen. Verwant is wel [1].")})
	f := &fakeKnowledge{}
	// 0,68 ligt onder de chatdrempel (0,73) en boven de brede lat (0,65): de
	// vraag zelf levert een gat, de brede zoekactie vindt wél iets.
	seedDestillaat(t, f, kbSeed{id: "9", samenvatting: "Duiven eten in de winter vaker straatafval.", score: 0.68, paper: "haag2004", themaTitel: "Gezondheid"})
	m, repo := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = f
		m.minSimilarity = 0.73
	})
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	ch := make(chan any, 64)

	res, err := at.askPigeon(withEmitter(t.Context(), ch), askPigeonArgs{Question: "Hoeveel duiven leven er in Reykjavik?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.KBStatus != string(KBSources) || res.Sources != 1 || res.Grounding != "search_related" {
		t.Fatalf("resultaat = %+v, want verwante bronnen uit de brede zoekactie", res)
	}
	close(ch)
	var sawRelatedOpen, sawSources bool
	for frame := range ch {
		switch fr := frame.(type) {
		case artifactFrame:
			if !fr.Done {
				sawRelatedOpen = fr.Related && fr.KBStatus == KBSources
			}
		case sourcesFrame:
			sawSources = len(fr.Sources) == 1
		}
	}
	if !sawRelatedOpen {
		t.Error("het openframe hoort related te dragen: anders leest het scherm dit als dekking")
	}
	if !sawSources {
		t.Error("de verwante bronnen horen als eigen frame bij het duif-blok te staan")
	}
	if !strings.Contains(mock.Calls[0].Messages[0].Content, relatedSourcesDirective["nl"]) {
		t.Error("verwante kennis mag geen volledige dekking suggereren")
	}
	if !strings.Contains(mock.Calls[0].Messages[0].Content, "Duiven eten in de winter vaker straatafval.") {
		t.Error("de duif hoort het grenzende onderzoek in haar systemprompt te krijgen")
	}
	// En de markering gaat mee de opslag in. Zonder deze regel las dezelfde rij
	// na herladen als een gewoon, volledig gedekt antwoord met één bron: de
	// duif benoemt de grens alleen als de bronnen haar vraag écht niet dragen.
	stored := repo.messagesWithKind(KindPigeonAnswer)
	if len(stored) != 1 || !stored[0].Related {
		t.Errorf("bewaarde rijen = %+v, want één duif-blok met related", stored)
	}
}

// Ligt er ook breder niets, dan blijft het gat een gat: de brede zoekactie mag
// geen bron uit de kast praten die er niet is.
func TestAgentKB_GapStaysAGapWhenNothingIsEvenRelated(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("Daar zwijgt het onderzoek over.")})
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "1", samenvatting: "Te ver weg.", score: 0.5, paper: "p"})
	embedder := &llmtesting.MockEmbedder{}
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = embedder
		m.knowledge = f
		m.minSimilarity = 0.73
	})
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	ch := make(chan any, 64)

	res, err := at.askPigeon(withEmitter(t.Context(), ch), askPigeonArgs{Question: "Welke duiven broeden op Spitsbergen?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.KBStatus != string(KBGap) || res.Grounding != "" {
		t.Fatalf("resultaat = %+v, want een kaal kennisgat", res)
	}
	close(ch)
	for frame := range ch {
		if fr, ok := frame.(artifactFrame); ok && fr.Related {
			t.Error("een gat draagt geen verwante-markering")
		}
	}
	// Twee embeds: de vraag zelf en de brede zoekactie. De herformulering komt
	// er niet aan te pas — de facilitator gaf dezelfde vraag door.
	if embedder.CallCount() != 2 {
		t.Errorf("embed-aanroepen = %d, want 2 (de vraag en de brede zoekactie)", embedder.CallCount())
	}
}

// Valt de verwante zoekactie zélf om, dan blijft het kennisgat staan: de
// mensvraag ís gezocht en gaf een geldig gat, en dat gat weggooien voor een
// mislukt extraatje zou de lezer een antwoord kosten dat er al was.
// Deze regel staat bewust in relatedSearch en wordt hier bewaakt: de embedder
// laat alleen de tweede aanroep vallen, zodat de eerste zoekopdracht gewoon
// slaagt.
func TestAgentKB_FailingRelatedSearchKeepsTheGapInsteadOfFailingTheTurn(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("Daar zwijgt het onderzoek over.")})
	f := &fakeKnowledge{}
	// 0,68 ligt onder de chatdrempel en boven de brede lat: zonder de storing
	// zou de verwante zoekactie hier bronnen vinden. Dat maakt het verschil
	// zichtbaar — wat hier wegvalt is een treffer, geen leeg resultaat.
	seedDestillaat(t, f, kbSeed{id: "9", samenvatting: "Duiven eten in de winter vaker straatafval.", score: 0.68, paper: "haag2004"})
	embedder := &flakyEmbedder{inner: &llmtesting.MockEmbedder{}, failFrom: 2, err: errors.New("test: embedder plat bij de tweede aanroep")}
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = embedder
		m.knowledge = f
		m.minSimilarity = 0.73
	})
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	ch := make(chan any, 64)

	res, err := at.askPigeon(withEmitter(t.Context(), ch), askPigeonArgs{Question: "Hoeveel duiven leven er in Reykjavik?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("resultaat = %+v, want geen fout: een mislukt extraatje mag de beurt niet kosten", res)
	}
	if res.KBStatus != string(KBGap) || res.Grounding != "" {
		t.Fatalf("resultaat = %+v, want een kaal kennisgat", res)
	}
	if at.pt.status == KBUnavailable {
		t.Error("de beurtstatus staat op unavailable; de mensvraag is wél gezocht")
	}
	if embedder.callCount() != 2 {
		t.Errorf("embed-aanroepen = %d, want 2 (de vraag en de mislukte brede zoekactie)", embedder.callCount())
	}
	close(ch)
	var sawAnswer bool
	for frame := range ch {
		switch fr := frame.(type) {
		case artifactFrame:
			if fr.Related {
				t.Error("een mislukte verwante zoekactie draagt geen verwante-markering")
			}
		case artifactDeltaFrame:
			sawAnswer = sawAnswer || fr.Delta != ""
		case sourcesFrame:
			t.Error("zonder geslaagde zoekactie horen er geen bronnen bij het blok te staan")
		}
	}
	if !sawAnswer {
		t.Error("de duif hoort haar gat-antwoord gewoon te geven")
	}
}

// Zonder eerdere zoekopdracht blijft een gat een gat: er is niets om op terug
// te vallen, en een gat uit een vorige beurt telt niet (de toestand is per
// beurt).
func TestAgentKB_GapWithoutSearchStaysAGap(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("Daar weet ik niets van.")})
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "1", samenvatting: "Te vaag.", score: 0.1, paper: "p"})
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = f
	})
	p, _ := m.personas.Get("biological")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent})
	res, err := at.askPigeon(withEmitter(t.Context(), make(chan any, 64)), askPigeonArgs{Question: "Dromen duiven?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.KBStatus != string(KBGap) || res.Grounding != "" {
		t.Errorf("resultaat = %+v, want gap zonder grounding", res)
	}
}

// De mensvraag wordt altijd als eerste gezocht. Een alternatieve formulering
// krijgt een verwante-bronneninstructie en vervangt de mensvraag niet.
func TestAgentKB_OriginalQuestionFirst_AlternativeIsRelated(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("Wij nestelen hoog [1].")})
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "9", samenvatting: "Duiven nestelen bij voorkeur op grote hoogte.", score: 0.9, paper: "haag2004"})
	// De originele mensvraag levert niets boven de drempel; de tweede,
	// alternatieve zoekformulering levert mogelijke verwante bronnen.
	f.onSearch = func(call int, table string) ([]data.SemanticResult, bool) {
		if call <= 2 {
			return nil, true
		}
		return nil, false
	}
	embedder := &llmtesting.MockEmbedder{}
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = embedder
		m.knowledge = f
	})
	p, _ := m.personas.Get("biological")
	pt := &preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p, engine: EngineAgent,
		messages: []llm.Message{{Role: "user", Content: "hoe nestelen duiven?"}}}
	at := m.newAgentTurn(pt)

	res, err := at.askPigeon(withEmitter(t.Context(), make(chan any, 64)), askPigeonArgs{Question: "Hoe bouwen jullie je nest?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.KBStatus != string(KBSources) || res.Sources != 1 || res.Grounding != "rephrased_related" {
		t.Fatalf("resultaat = %+v, want sources via de mensvraag", res)
	}
	if n := len(embedder.Calls); n != 2 || embedder.Calls[0] != "hoe nestelen duiven?" || embedder.Calls[1] != "Hoe bouwen jullie je nest?" {
		t.Errorf("embed-aanroepen = %v, want eerst de mensvraag, daarna de alternatieve zoekvraag", embedder.Calls)
	}
}
