package duif

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

func postTurn(t *testing.T, m *Module, body any, anonymous bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/turn", turnBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	return serve(t, m, req, anonymous)
}

// Happy path: de duif streamt tekst en de beurt sluit met done.
func TestTurn_StreamsTextAndDone(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{
		{Content: "Coo. "},
		{Content: "I have "},
		{Content: "opinions.", FinishReason: "stop"},
	}}})
	m, _ := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "What do you think of the square?"}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	frames := parseSSE(t, rec.Body.String())
	if want := []string{"text", "text", "text", "done"}; strings.Join(frameTypes(frames), ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v, want %v", frameTypes(frames), want)
	}
	var text strings.Builder
	for _, f := range frames[:3] {
		text.WriteString(f["delta"].(string))
	}
	if text.String() != "Coo. I have opinions." {
		t.Errorf("gestreamde tekst = %q", text.String())
	}
	done := frames[len(frames)-1]
	if done["kbStatus"] != string(KBPersonaOnly) {
		t.Errorf("done.kbStatus = %v, want persona_only", done["kbStatus"])
	}
	if sid, _ := done["sessionId"].(string); sid == "" {
		t.Error("done.sessionId moet de gemunte sessie-id dragen")
	}
	if _, has := done["sources"]; has {
		t.Error("persona-only done mag geen sources dragen")
	}

	// De systemprompt gaat als eerste message de LLM in, met de EN-variant.
	if mock.CallCount() != 1 {
		t.Fatalf("LLM-callcount = %d", mock.CallCount())
	}
	sys := mock.Calls[0].Messages[0]
	if sys.Role != "system" || !strings.Contains(sys.Content, "eis:P1") {
		t.Errorf("eerste message moet de persona-systemprompt zijn (role=%s)", sys.Role)
	}
	if !strings.Contains(sys.Content, langDirective["en"]) {
		t.Error("default-taal moet Engels zijn")
	}
}

func TestTurn_LangNL_UsesNLPrompt(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "Roekoe.", FinishReason: "stop"}}}})
	m, _ := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "Wat vind je van het plein?", "lang": "nl"}, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	sys := mock.Calls[0].Messages[0].Content
	if !strings.Contains(sys, langDirective["nl"]) {
		t.Error("lang=nl moet het NL-directief meesturen")
	}
}

func TestTurn_ValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{"leeg bericht", map[string]any{"pigeonId": "individualist", "message": ""}},
		{"onbekende pigeonId", map[string]any{"pigeonId": "flamingo", "message": "hoi"}},
		{"niet-chattende duif", map[string]any{"pigeonId": "nonverbal", "message": "hoi"}},
		{"te lang bericht", map[string]any{"pigeonId": "individualist", "message": strings.Repeat("a", 4097)}},
		{"ongeldige taal", map[string]any{"pigeonId": "individualist", "message": "hoi", "lang": "de"}},
		{"onbekend veld", map[string]any{"pigeonId": "individualist", "message": "hoi", "extra": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
			rec := postTurn(t, m, tc.body, false)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTurn_Unauthorized(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "hoi"}, true)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// Foutpad: een fout mídden in de stream wordt een error-frame
// gevolgd door done, nooit een kale disconnect.
func TestTurn_LLMError_EmitsErrorFrameThenDone(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{
		{Content: "Coo"},
		{Err: errStreamBroke},
	}}})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "hoi"}, false)
	frames := parseSSE(t, rec.Body.String())
	types := frameTypes(frames)
	if strings.Join(types, ",") != "text,error,done" {
		t.Fatalf("frames = %v, want [text error done]", types)
	}
	errFrame := frames[1]
	if errFrame["error"] != ReasonLLMUnavailable {
		t.Errorf("error-frame reason = %v, want %s", errFrame["error"], ReasonLLMUnavailable)
	}
	if strings.Contains(rec.Body.String(), errStreamBroke.Error()) {
		t.Error("upstream-fouttekst mag de wire nooit bereiken")
	}
	// Partieel antwoord is bewaard.
	sid := frames[2]["sessionId"].(string)
	msgs, _ := repo.ListMessages(t.Context(), sid, 0)
	if len(msgs) != 2 || msgs[1].Content != "Coo" || !msgs[1].Partial {
		t.Errorf("partieel assistantbericht moet bewaard zijn: %+v", msgs)
	}
}

// Budget-fout op stream-start (proxy weigert de completion): het REST-contract
// geeft HTTP 503 met stabiele reason-code, vóór er een stream is.
// NB: llmtesting.MockService geeft StreamStep.Err níet synchroon terug;
// daarom een eigen minimale failing llm.Service voor dit pad.
func TestTurn_BudgetExceeded_503Contract(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = failingLLM{err: errBudgetUpstream} })
	usePipeline(t, m, "individualist")

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "hoi"}, false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body parsen: %v", err)
	}
	if body.Error != ReasonBudgetExceeded {
		t.Errorf("error = %q, want %q", body.Error, ReasonBudgetExceeded)
	}
	if body.Message["en"] == "" || body.Message["nl"] == "" {
		t.Errorf("message moet en+nl dragen: %+v", body.Message)
	}
	if strings.Contains(rec.Body.String(), "ExceededBudget") {
		t.Error("upstream-fouttekst mag de wire nooit bereiken")
	}
}

func TestTurn_NilLLM_503(t *testing.T) {
	m, _ := newTestModule(t, nil) // m.llm blijft nil
	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "hoi"}, false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (nil service → nette 503)", rec.Code)
	}
}

var (
	errStreamBroke    = errors.New("mock: upstream stream broke")
	errBudgetUpstream = errors.New("litellm: 400 ExceededBudget: Budget has been exceeded")
)

// Een duif zonder kennisbank raakt de retrieval niet aan.
func TestTurn_NoKB_SkipsRetrieval(t *testing.T) {
	embedder := &llmtesting.MockEmbedder{}
	knowledge := &fakeKnowledge{}
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "coo", FinishReason: "stop"}}}})
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = embedder
		m.knowledge = knowledge
	})
	usePipeline(t, m, "individualist")

	pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: "individualist", Message: "hoi"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	if pt.status != KBPersonaOnly {
		t.Errorf("status = %q, want persona_only", pt.status)
	}
	if embedder.CallCount() != 0 {
		t.Errorf("embedder mag niet aangeroepen worden zonder KB (calls=%d)", embedder.CallCount())
	}
	if knowledge.callCount() != 0 {
		t.Errorf("knowledge mag niet aangeroepen worden zonder KB (calls=%d)", knowledge.callCount())
	}
}

// Zonder embedder is niets gezocht. De gebruiker ziet onbeschikbaarheid,
// geen ongefundeerde conclusie over wat de kennisbank wel of niet bevat.
func TestTurn_NoRetrievalWired_IsUnavailable(t *testing.T) {
	knowledge := &fakeKnowledge{}
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = llmtesting.NewMockService()
		m.knowledge = knowledge // embedder blijft nil, dus geen retrieval
	})
	if m.retrieval != nil {
		t.Fatal("zonder embedder hoort er geen retrieval gewired te zijn")
	}
	usePipeline(t, m, "biological")

	pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: "biological", Message: "Wat eten duiven?"})
	if pt != nil || terr == nil || terr.body.Error != ReasonKBUnavailable {
		t.Fatalf("geen retrieval is onbeschikbaarheid: pt=%+v, err=%+v", pt, terr)
	}
	if knowledge.callCount() != 0 {
		t.Errorf("er valt niets te zoeken zonder embedder (calls=%d)", knowledge.callCount())
	}
}

func TestTurn_KBPersona_EmbedsAndRetrieves(t *testing.T) {
	embedder := &llmtesting.MockEmbedder{}
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{id: "1", samenvatting: "Duiven herkennen gezichten.", score: 0.91, paper: "p1"})
	seedDestillaat(t, knowledge, kbSeed{id: "2", samenvatting: "Te vage match.", score: 0.31, paper: "p2"})
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = llmtesting.NewMockService()
		m.embedder = embedder
		m.knowledge = knowledge
	})
	usePipeline(t, m, "biological")

	pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: "biological", Message: "Herkennen duiven mensen?"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	if pt.status != KBSources {
		t.Fatalf("status = %q, want sources", pt.status)
	}
	if len(pt.sources) != 1 || pt.sources[0].Number != 1 {
		t.Fatalf("drempelfilter (0.55) moet één bron overhouden, 1-based genummerd: %+v", pt.sources)
	}
	if embedder.CallCount() != 1 {
		t.Errorf("embed hoort precies één keer te draaien (%d)", embedder.CallCount())
	}
	// De systemprompt draagt dezelfde bron, met hetzelfde nummer.
	if sys := pt.messages[0].Content; !strings.Contains(sys, "Duiven herkennen gezichten.") || !strings.Contains(sys, `<source id="1"`) {
		t.Errorf("systemprompt mist de genummerde bron:\n%s", sys)
	}
}

// Regressie: disconnect vóór de eerste chunk → géén leeg assistantbericht in
// de opslag, en de volgende turn in dezelfde sessie voedt de LLM geen lege
// message (providers achter de proxy weigeren die).
func TestTurn_DisconnectBeforeFirstChunk_NextTurnSucceeds(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{
		{Chunks: []llm.StreamChunk{{Content: "komt nooit aan", FinishReason: "stop"}}},
		{Chunks: []llm.StreamChunk{{Content: "ok", FinishReason: "stop"}}},
	})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	ctx, cancel := context.WithCancel(t.Context())
	pt, terr := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "hoi"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	ch, terr := m.startLLMStream(ctx, pt)
	if terr != nil {
		t.Fatalf("startLLMStream: %+v", terr)
	}
	cancel() // client weg vóór de eerste chunk
	m.pumpTurn(ctx, pt, ch, func(any) error { return nil })

	msgs, _ := repo.ListMessages(context.Background(), pt.session.ID, 0)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("nul gestreamde content mag geen assistantbericht opleveren: %+v", msgs)
	}

	// Vervolg-turn in dezelfde sessie slaagt.
	rec := postTurn(t, m, map[string]any{"sessionId": pt.session.ID, "pigeonId": "individualist", "message": "nog eens"}, false)
	if rec.Code != 200 {
		t.Fatalf("vervolg-turn: status = %d (body %s)", rec.Code, rec.Body.String())
	}
	last := mock.Calls[len(mock.Calls)-1].Messages
	for i, msg := range last {
		if msg.Content == "" {
			t.Errorf("message %d (role=%s) heeft lege content", i, msg.Role)
		}
	}
}

// Client-disconnect: partieel antwoord bewaard, geen zombie.
func TestTurn_ClientDisconnect_PersistsPartial(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{
		{Content: "Eerste"},
		{Content: " tweede"},
		{Content: " derde", FinishReason: "stop"},
	}}})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	ctx, cancel := context.WithCancel(t.Context())
	pt, terr := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "hoi"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	ch, terr := m.startLLMStream(ctx, pt)
	if terr != nil {
		t.Fatalf("startLLMStream: %+v", terr)
	}
	frames := 0
	m.pumpTurn(ctx, pt, ch, func(any) error {
		frames++
		if frames == 1 {
			cancel() // client weg na het eerste frame
		}
		return nil
	})

	waitFor(t, func() bool {
		msgs, _ := repo.ListMessages(context.Background(), pt.session.ID, 0)
		return len(msgs) == 2
	}, "partieel assistantbericht moet ook na cancel persisteren (detached context)")
	msgs, _ := repo.ListMessages(context.Background(), pt.session.ID, 0)
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || !last.Partial || last.Content == "" {
		t.Errorf("partieel bericht fout bewaard: %+v", last)
	}
}

// De citatiestripper is de garantie onder het gap-directief. Hij moet over
// chunkgrenzen heen werken, want een markering kan in tweeën geknipt binnenkomen.
func TestCitationStripper_RemovesMarkersAcrossChunks(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"losse markering", []string{"Dat klopt [1]."}, "Dat klopt."},
		{"markering over chunkgrens", []string{"Dat klopt [", "1", "]."}, "Dat klopt."},
		{"meerdere nummers", []string{"Beide [1, 2] kloppen."}, "Beide kloppen."},
		{"opeenvolgend", []string{"Zo [1][2] dus."}, "Zo dus."},
		{"midden in de zin", []string{"Duiven [3] eten brood."}, "Duiven eten brood."},
		{"geen cijfers blijft staan", []string{"Zie [het kader] hierboven."}, "Zie [het kader] hierboven."},
		{"niet-gesloten haak blijft", []string{"Een lijst [1, 2 zonder slot"}, "Een lijst [1, 2 zonder slot"},
		{"lege haken blijven", []string{"Leeg [] hier."}, "Leeg [] hier."},
		{"zonder markering ongemoeid", []string{"Gewone ", "tekst met  dubbele spatie."}, "Gewone tekst met  dubbele spatie."},
		{"aan het eind", []string{"Klaar [7]"}, "Klaar"},
		{"open haak aan het slot", []string{"Klaar ["}, "Klaar ["},
		{"open citatie aan het slot verdwijnt", []string{"Klaar [1"}, "Klaar"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c citationStripper
			var got strings.Builder
			for _, chunk := range tc.chunks {
				got.WriteString(c.write(chunk))
			}
			got.WriteString(c.flush())
			if got.String() != tc.want {
				t.Errorf("stripper = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

// Met een plafond van twintig bronnen citeert de duif tweecijferige
// nummers. De toestandsmachine verzamelt cijfers tot de sluithaak, dus [20] is
// voor haar niet anders dan [2] — maar de bronlijst bepaalt nog steeds wat een
// geldige markering is, ook over een chunkgrens heen.
func TestCitationFilter_KeepsTwoDigitNumbersWithinTheSourceList(t *testing.T) {
	sources := make([]Source, 0, maxAnswerSources)
	for i := 1; i <= maxAnswerSources; i++ {
		sources = append(sources, Source{Number: i})
	}
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"hoogste nummer", []string{"Ook hoger op [20]."}, "Ook hoger op [20]."},
		{"tweecijferig over de chunkgrens", []string{"Ook hoger op [1", "9]."}, "Ook hoger op [19]."},
		{"gemengde lijst", []string{"Beide [3, 17] kloppen."}, "Beide [3, 17] kloppen."},
		{"boven het plafond verdwijnt", []string{"Verzonnen [21] bron."}, "Verzonnen bron."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := citationFilter(sources)
			var got strings.Builder
			for _, chunk := range tc.chunks {
				got.WriteString(c.write(chunk))
			}
			got.WriteString(c.flush())
			if got.String() != tc.want {
				t.Errorf("filter = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

// De stripper en de TTS-stripper horen dezelfde markeringen te herkennen:
// wat van het scherm verdwijnt, verdwijnt ook uit de stem.
func TestCitationStripper_AgreesWithSpeechStripper(t *testing.T) {
	zinnen := []string{
		"Dat klopt [1].",
		"Beide [1, 2] kloppen.",
		"Zo [1][2] dus.",
		"Duiven [12] eten brood.",
		"Niets te strippen hier.",
	}
	for _, zin := range zinnen {
		var c citationStripper
		got := c.write(zin) + c.flush()
		if want := stripForSpeech(zin); got != want {
			t.Errorf("stream-stripper %q vs TTS-stripper %q (invoer %q)", got, want, zin)
		}
	}
}

// Een vergelijking neemt het hele brongesprek mee, dus staat er een antwoord
// van een andere duif in de historie. Dat antwoord gaat de prompt in als háár
// uitspraak — een mensbericht met haar naam ervoor — en nooit als een eerdere
// beurt van de duif die nu aan het woord komt.
func TestTurn_ForeignPigeonAnswerEntersThePromptAsAnotherVoice(t *testing.T) {
	m, repo := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	usePipeline(t, m, "individualist")
	ctx := t.Context()
	if err := repo.CreateSession(ctx, ChatSession{ID: "branch", UserID: testUser.ID, PigeonID: "individualist", Lang: "nl"}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []ChatMessage{
		{ID: "own", SessionID: "branch", Role: "user", Content: "Wat betekent een nieuwe nestplek?"},
		{ID: "copied", SessionID: "branch", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer, Content: "Richels zijn schaars [1]."},
	} {
		if err := repo.AppendMessage(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	pt, terr := m.prepareTurn(ctx, testUser.ID, turnRequest{SessionID: "branch", PigeonID: "individualist", Message: "En jij?"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	other := m.personaName("biological", "nl")
	if other == "" || other == "biological" {
		t.Fatalf("de andere duif heeft geen naam in de prompt: %q", other)
	}
	var quoted bool
	for i, message := range pt.messages {
		if i > 0 && message.Role == "assistant" {
			t.Fatalf("andermans antwoord kwam terug als eigen beurt: %+v", message)
		}
		if strings.Contains(message.Content, "Richels zijn schaars") {
			quoted = true
			// Historische citatienummers horen niet bij de bronnen van deze
			// beurt en verdwijnen, precies als bij een eigen oud antwoord.
			if message.Role != "user" || !strings.Contains(message.Content, other+" antwoordde eerder:") || strings.Contains(message.Content, "[1]") {
				t.Fatalf("verkeerde vorm voor andermans antwoord: %+v", message)
			}
		}
	}
	if !quoted {
		t.Fatal("het gekopieerde antwoord ontbreekt in de prompt")
	}
	if want := []string{"Wat betekent een nieuwe nestplek?", "En jij?"}; !slices.Equal(pt.humanContext, want) {
		t.Fatalf("gekopieerd duifantwoord telde als mensinvoer: %+v", pt.humanContext)
	}
}

// De facilitator-motor maakt hetzelfde onderscheid: eigen antwoorden blijven
// exchanges (de duif krijgt ze terug als haar eigen woorden), andermans
// antwoord staat als benoemde uitspraak in de prose.
func TestSplitHistory_ForeignAnswerIsNeverAnOwnExchange(t *testing.T) {
	history := []ChatMessage{
		{Role: "user", Content: "Eerst dit."},
		{Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer, Content: "Haar antwoord [1].", Question: "Haar vraag"},
		{Role: "assistant", Persona: "individualist", Kind: KindPigeonAnswer, Content: "Mijn eigen antwoord.", Question: "Mijn vraag"},
		{Role: "assistant", Persona: PersonaFacilitator, Content: "Kader vooraf."},
		{Role: "assistant", Content: "Antwoord van vóór de personakolom."},
	}
	name := func(string) string { return "Kennispartner" }
	prose, exchanges := splitHistory(history, promptVoices{self: "individualist", lang: "nl", name: name})
	if len(exchanges) != 2 || exchanges[0].Answer != "Mijn eigen antwoord." || exchanges[0].Question != "Mijn vraag" ||
		exchanges[1].Answer != "Antwoord van vóór de personakolom." {
		t.Fatalf("eigen beurten verloren of geleend: %+v", exchanges)
	}
	if len(prose) != 3 || prose[0].Content != "Eerst dit." || prose[2].Content != "Kader vooraf." {
		t.Fatalf("prose: %+v", prose)
	}
	if prose[1].Role != "user" || prose[1].Content != "Kennispartner antwoordde eerder: “Haar antwoord.”" {
		t.Fatalf("andermans antwoord verkeerd gepresenteerd: %+v", prose[1])
	}
	english, _ := splitHistory(history[1:2], promptVoices{self: "individualist", lang: "en", name: name})
	if english[0].Content != "Kennispartner answered earlier: “Haar antwoord.”" {
		t.Fatalf("engelse vorm: %+v", english[0])
	}
}

// Dezelfde regel op de facilitator-motor, langs de echte prepareTurn: het
// gekopieerde antwoord van de andere duif komt in de duif-subprompt onder zijn
// eigen kop, en nooit onder "Eerdere mensberichten in dit gesprek".
func TestAgentTurn_ForeignPigeonAnswerIsNotOfferedAsHumanContext(t *testing.T) {
	m, repo := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	ctx := t.Context()
	if err := repo.CreateSession(ctx, ChatSession{ID: "branch", UserID: testUser.ID, PigeonID: "individualist", Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []ChatMessage{
		{ID: "own", SessionID: "branch", Role: "user", Content: "HUMAN_WROTE_THIS about the station square."},
		{ID: "copied", SessionID: "branch", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer, Content: "OTHER_PIGEON_SAID_THIS [1]."},
	} {
		if err := repo.AppendMessage(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	pt, terr := m.prepareTurn(ctx, testUser.ID, turnRequest{SessionID: "branch", PigeonID: "individualist", Message: "And you?"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	if pt.engine != EngineAgent {
		t.Fatalf("deze duif draait niet op de facilitator-motor: %q", pt.engine)
	}
	last := m.newAgentTurn(pt).pigeonMessages(askPigeonArgs{Question: "And you?"}, retrievalResult{Status: KBGap})
	prompt := last[len(last)-1].Content
	human, other := strings.Index(prompt, previousHumanHeader["en"]), strings.Index(prompt, otherPigeonHeader["en"])
	person, pigeon := strings.Index(prompt, "HUMAN_WROTE_THIS"), strings.Index(prompt, "OTHER_PIGEON_SAID_THIS")
	if human < 0 || other < 0 || person < 0 || pigeon < 0 {
		t.Fatalf("kop of context ontbreekt: %q", prompt)
	}
	// Elke stem staat achter zijn eigen kop: mens, dan duif.
	if !(human < person && person < other && other < pigeon) {
		t.Fatalf("andermans antwoord staat onder de menskop: %q", prompt)
	}
	if strings.Contains(prompt, "[1]") {
		t.Fatalf("historisch citatienummer meegereisd: %q", prompt)
	}
}
