package duif

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// newTestAgentTurn bouwt een beurttoestand zonder route eromheen, voor de
// tools die je los kunt bevragen.
func newTestAgentTurn(t *testing.T, lang string) (*agentTurn, *Module) {
	t.Helper()
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	p, ok := m.personas.Get("individualist")
	if !ok {
		t.Fatal("individualist ontbreekt")
	}
	pt := &preparedTurn{session: ChatSession{ID: "s1"}, lang: lang, status: KBPersonaOnly, persona: p, engine: EngineAgent}
	return m.newAgentTurn(pt), m
}

// Beide pratende duiven hebben dezelfde vier gepinde tools; het bronzoeken
// zit in ask_pigeon.
func TestAgentRegistry_ExactToolSet(t *testing.T) {
	at, _ := newTestAgentTurn(t, "en")
	reg, err := newAgentRegistry(at)
	if err != nil {
		t.Fatalf("newAgentRegistry: %v", err)
	}
	var namen []string
	for _, tool := range reg.Definitions() {
		namen = append(namen, tool.Function.Name)
		if !tool.Function.Strict {
			t.Errorf("tool %q hoort een strict schema te hebben", tool.Function.Name)
		}
		if strings.TrimSpace(tool.Function.Description) == "" {
			t.Errorf("tool %q heeft geen beschrijving; het model kiest dan blind", tool.Function.Name)
		}
	}
	want := []string{toolAskPigeon, toolPigeonProfile, toolClarify, toolRedirectToPigeon}
	if strings.Join(namen, ",") != strings.Join(want, ",") {
		t.Fatalf("toolset = %v, want %v", namen, want)
	}
}

// pigeon_profile komt uit personas.yaml en kost geen LLM-beurt: een model dat
// verzint wie de duif is, ondergraaft precies de rolscheiding van eis:F2.
func TestPigeonProfile_FromConfig_NoLLMCall(t *testing.T) {
	for _, lang := range []string{"en", "nl"} {
		at, m := newTestAgentTurn(t, lang)
		res, err := at.pigeonProfile(t.Context(), pigeonProfileArgs{})
		if err != nil {
			t.Fatalf("%s: pigeonProfile: %v", lang, err)
		}
		p, _ := m.personas.Get("individualist")
		if res.Name != pickLang(p.Naam, lang) {
			t.Errorf("%s: naam = %q", lang, res.Name)
		}
		for _, topic := range p.Topics {
			if !strings.Contains(res.Profile, topic.LabelFor(lang)) {
				t.Errorf("%s: profiel mist het thema %q:\n%s", lang, topic.LabelFor(lang), res.Profile)
			}
			if strings.Contains(res.Profile, topic.DescribeFor(lang)) {
				t.Errorf("%s: compact profiel bevat nog de hele beschrijving van %q", lang, topic.ID)
			}
		}
		if n := m.llm.(*llmtesting.MockService).CallCount(); n != 0 {
			t.Errorf("%s: pigeon_profile mag geen LLM-beurt kosten (%d aanroepen)", lang, n)
		}
	}
}

// Op de route: profiel geeft chips en een facilitator-zin, en geen duif-blok.
func TestAgentTurn_PigeonProfile_NoArtifact(t *testing.T) {
	m, _, mock := newAgentModule(t, []llmtesting.StreamStep{
		toolCallStep("call-1", toolPigeonProfile, `{}`),
		textStep("Zij is een eigenwijze stadsduif."),
	})

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "met wie praat ik?"})
	want := []string{"tool", "tool_result", "text", "done"}
	if got := frameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v, want %v", got, want)
	}
	if frames[0]["name"] != toolPigeonProfile {
		t.Errorf("tool = %v", frames[0]["name"])
	}
	if mock.CallCount() != 1 {
		t.Errorf("LLM-aanroepen = %d, want 1 (geen betaalde afronding of duif-beurt)", mock.CallCount())
	}
}

// Eén wedervraag per beurt (eis:F3). De tweede aanvraag is een tool-fout, geen
// Go-error: de facilitator mag zelf beslissen wat hij dan tegen de mens zegt.
func TestClarify_SecondCallInSameTurnIsAToolError(t *testing.T) {
	at, _ := newTestAgentTurn(t, "nl")

	eerste, err := at.clarify(t.Context(), clarifyArgs{Question: "Bedoel je de stoep of het verkeer?"})
	if err != nil {
		t.Fatalf("clarify: %v", err)
	}
	if eerste.Ask != "Bedoel je de stoep of het verkeer?" || eerste.Error != "" {
		t.Fatalf("eerste wedervraag = %+v", eerste)
	}
	tweede, err := at.clarify(t.Context(), clarifyArgs{Question: "En nog een vraag?"})
	if err != nil {
		t.Fatalf("clarify (tweede): %v", err)
	}
	if tweede.Error != ReasonClarifyAlreadyAsked {
		t.Errorf("tweede wedervraag = %+v, want error %s", tweede, ReasonClarifyAlreadyAsked)
	}
	if tweede.Ask != "" {
		t.Errorf("een geweigerde wedervraag levert geen vraag: %+v", tweede)
	}
}

// Dezelfde cap op de wire: de tweede chip draagt de reden-code, de beurt zelf
// eindigt gewoon natuurlijk — een wedervraag is een gewone beurt.
func TestAgentTurn_ClarifyTwice_ChipCarriesReasonAndTurnEndsNatural(t *testing.T) {
	m, _, _ := newAgentModule(t, []llmtesting.StreamStep{
		{Chunks: []llm.StreamChunk{{ToolCallDeltas: []llm.ToolCallDelta{
			{Index: 0, ID: "call-1", Name: toolClarify, ArgsFrag: `{"question":"De stoep of het verkeer?"}`},
			{Index: 1, ID: "call-2", Name: toolClarify, ArgsFrag: `{"question":"Of toch iets anders?"}`},
		}, FinishReason: "tool_calls"}}},
		textStep("Bedoel je de stoep of het verkeer?"),
	})

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "en dat daar dan"})
	want := []string{"tool", "tool_result", "tool", "tool_result", "text", "done"}
	if got := frameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v, want %v", got, want)
	}
	if _, fout := frames[1]["error"]; fout {
		t.Errorf("de eerste wedervraag mag geen fout zijn: %+v", frames[1])
	}
	if frames[3]["error"] != ReasonClarifyAlreadyAsked {
		t.Errorf("tweede tool_result = %+v, want %s", frames[3], ReasonClarifyAlreadyAsked)
	}
	if frames[5]["stopReason"] != "natural" {
		t.Errorf("done.stopReason = %v, want natural (een wedervraag is een gewone beurt)", frames[5]["stopReason"])
	}
}

// De ambient meldt dat de wedervraag al gesteld is, zodat de cap niet alleen
// in de tool zit maar ook in wat het model ziet.
func TestAmbient_MentionsClarifyAlreadyAsked(t *testing.T) {
	at, _ := newTestAgentTurn(t, "nl")
	voor, err := at.ambient(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(voor, ambientClarified["nl"]) {
		t.Errorf("vóór een wedervraag hoort die melding er niet in te staan:\n%s", voor)
	}
	if _, err := at.clarify(t.Context(), clarifyArgs{Question: "welke?"}); err != nil {
		t.Fatal(err)
	}
	na, err := at.ambient(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(na, ambientClarified["nl"]) {
		t.Errorf("na een wedervraag moet de ambient de cap melden:\n%s", na)
	}
}

// Het model krijgt een korte gist terug, niet het hele antwoord: dat staat al
// bij de gebruiker op het scherm (eis:F5).
func TestAskPigeon_ReturnsAGistNotTheWholeAnswer(t *testing.T) {
	lang := strings.Repeat("duivenpraat ", 60) // ruim boven gistMaxRunes
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep(lang)})
	m, _ := newTestModule(t, func(m *Module) { m.llm = mock })
	p, _ := m.personas.Get("individualist")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p})

	res, err := at.askPigeon(t.Context(), askPigeonArgs{Question: "en?", Context: ""})
	if err != nil {
		t.Fatalf("askPigeon: %v", err)
	}
	if res.Error != "" || res.Gist == "" {
		t.Fatalf("resultaat = %+v", res)
	}
	if len([]rune(res.Gist)) > gistMaxRunes+1 {
		t.Errorf("gist = %d runen, want ≤ %d (+ het beletselteken)", len([]rune(res.Gist)), gistMaxRunes)
	}
	// Het resultaat draagt geen handle en geen lengte: de facilitator-prompt
	// kent ze niet en eis:F5 verbiedt hem artifacts te benoemen.
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, verboden := range []string{"artifactId", "words"} {
		if strings.Contains(string(raw), verboden) {
			t.Errorf("het tool-resultaat draagt %q: %s", verboden, raw)
		}
	}
}

// De artifact-id's van één beurt lopen op: de client koppelt delta's eraan.
func TestAskPigeon_ArtifactIDsAreUniquePerTurn(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("een"), textStep("twee")})
	m, _ := newTestModule(t, func(m *Module) { m.llm = mock })
	p, _ := m.personas.Get("individualist")
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s1"}, lang: "nl", status: KBPersonaOnly, persona: p})

	ch := make(chan any, 32)
	ctx := withEmitter(t.Context(), ch)
	if _, err := at.askPigeon(ctx, askPigeonArgs{Question: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := at.askPigeon(ctx, askPigeonArgs{Question: "b"}); err != nil {
		t.Fatal(err)
	}
	close(ch)
	ids := map[string]bool{}
	for frame := range ch {
		if af, ok := frame.(artifactFrame); ok && !af.Done {
			ids[af.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Fatalf("artifact-id's = %v, want twee verschillende", ids)
	}
	// Het tweede duif-antwoord ziet het eerste als eigen historie terug.
	tweedeCall := mock.Calls[1].Messages
	if len(tweedeCall) != 4 || tweedeCall[2].Content != "een" {
		t.Errorf("de duif hoort haar eerste antwoord terug te zien: %+v", tweedeCall)
	}
}

// Zonder context blijft de vraag aan de duif de kale vraag: een lege aanhef
// zou ruis zijn in haar prompt.
func TestPigeonQuestion_WithoutContext(t *testing.T) {
	if got := pigeonQuestion(askPigeonArgs{Question: " en het plein? ", Context: "  "}, "nl", "token", KBPersonaOnly); got != "en het plein?" {
		t.Errorf("vraag = %q", got)
	}
}

// De handlers werken ook zonder stream: de emitter uit de context is dan een
// no-op, en de tools blijven los aanroepbaar.
func TestEmitter_WithoutContextValue_IsANoOp(t *testing.T) {
	em := emitterFrom(context.Background())
	if em == nil {
		t.Fatal("emitterFrom mag nooit nil geven")
	}
	if !em.emit(context.Background(), toolFrame{Type: "tool"}) {
		t.Error("een no-op emitter hoort te slagen")
	}
}

// De brug levert af zolang de context leeft, en geeft op zodra hij afloopt —
// anders blijft een handler hangen op een lezer die er niet meer is.
func TestEmitter_DeliversAndGivesUpOnCancel(t *testing.T) {
	ch := make(chan any)
	ctx := withEmitter(t.Context(), ch)
	em := emitterFrom(ctx)

	go func() { <-ch }()
	if !em.emit(ctx, toolFrame{Type: "tool", ID: "t1"}) {
		t.Fatal("emit hoort te slagen zolang er een lezer is")
	}

	dood, cancel := context.WithCancel(t.Context())
	cancel()
	if em.emit(dood, toolFrame{Type: "tool", ID: "t2"}) {
		t.Error("emit hoort op te geven zodra de context afloopt")
	}
}

// Vangrail: een duif zonder facilitator-motor komt niet per ongeluk op het
// agent-pad terecht, en andersom.
func TestPrepareTurn_ChoosesEnginePerPigeon(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	for _, tc := range []struct{ id, engine string }{
		{"individualist", EngineAgent},
		{"biological", EngineAgent},
	} {
		pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: tc.id, Message: "hoi"})
		if terr != nil {
			t.Fatalf("%s: prepareTurn: %+v", tc.id, terr)
		}
		if pt.engine != tc.engine {
			t.Errorf("%s: engine = %q, want %q", tc.id, pt.engine, tc.engine)
		}
	}
}

// De facilitator-historie draagt geen systemprompt: pkg/agent zet die per
// iteratie zelf vooraan (Config.System). Een tweede zou hem verdubbelen.
func TestPrepareTurn_AgentMessagesCarryNoSystemPrompt(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
	pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: "individualist", Message: "hoi"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	if len(pt.messages) != 1 || pt.messages[0].Role != "user" || pt.messages[0].Content != "hoi" {
		t.Fatalf("messages = %+v, want alleen het userbericht", pt.messages)
	}
}

// De vergadermodus blijft de directe spreekbeurt: aan tafel spreekt de
// stakeholder zelf, zonder facilitator, zonder tools en zonder artifact —
// `engine` heeft daar geen effect.
func TestMeeting_IgnoresEngineField(t *testing.T) {
	gllm := newGatedLLM("Roekoe. ", "Dat plein is van mij.")
	voice := newFakeVoice("mp3-1")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })
	p, _ := rig.m.personas.Get("individualist")
	if p.EngineOr() != EngineAgent {
		t.Fatalf("aanname: de individualist staat in de chat op de agent-motor")
	}

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
	nextEvent(t, es.ch, EventTranscript)
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: status = %d, body = %s", status, body)
	}
	if persona := nextEvent(t, es.ch, EventPersona); persona.PigeonID != "individualist" {
		t.Errorf("persona-event = %+v, want de duif zelf (geen facilitator)", persona)
	}
	if text, _ := collectSpokenText(t, es.ch); text != "Roekoe. Dat plein is van mij." {
		t.Errorf("gesproken tekst = %q", text)
	}
	if n := gllm.callCount(); n != 1 {
		t.Errorf("LLM-aanroepen = %d, want 1 (één directe spreekbeurt, geen tool-lus)", n)
	}
}
