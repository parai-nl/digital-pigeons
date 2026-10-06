package duif

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// Vroeger meldde een beurt die de provider op de tokengrens afsloot zich als
// normaal beëindigd. De lezer zag een halve zin, de opslag bewaarde hem als
// een afgeronde beurt, en de bronaudit becommentarieerde een antwoord waarvan
// niemand wist dat het niet af was.
//
// Deze tests leggen per route vast wat er nu gebeurt: de tekst blijft
// letterlijk wat het model schreef (niets wordt ingetrokken, aangevuld of
// opnieuw gevraagd), en de beurt zegt erbij dát de provider zelf afsloot.
// Een normaal einde blijft zwijgen — een melding onder elk antwoord zou niets
// meer betekenen.

// closedStep is een gescripte iteratie die tekst streamt en dan afsluit met de
// opgegeven finishReason: "length" als het tokenplafond op is, "content_filter"
// als de provider zichzelf stopte, "stop" als de zin gewoon af was.
func closedStep(finishReason string, delen ...string) llmtesting.StreamStep {
	chunks := make([]llm.StreamChunk, 0, len(delen))
	for i, d := range delen {
		c := llm.StreamChunk{Content: d}
		if i == len(delen)-1 {
			c.FinishReason = finishReason
		}
		chunks = append(chunks, c)
	}
	return llmtesting.StreamStep{Chunks: chunks}
}

// lengthStep is de afsluiting waar deze tests om draaien.
func lengthStep(delen ...string) llmtesting.StreamStep {
	return closedStep("length", delen...)
}

// findFrame geeft het eerste frame van dat type, of nil.
func findFrame(frames []sseFrame, typ string) sseFrame {
	for _, f := range frames {
		if f["type"] == typ {
			return f
		}
	}
	return nil
}

// lastFrame geeft het laatste frame van dat type, of nil. Het artifact opent
// én sluit met hetzelfde type; het sluitframe is de laatste.
func lastFrame(frames []sseFrame, typ string) sseFrame {
	var out sseFrame
	for _, f := range frames {
		if f["type"] == typ {
			out = f
		}
	}
	return out
}

// --- pipelineroute ---------------------------------------------------------

func TestTruncation_PipelineReportsTheLengthCloseAndKeepsTheText(t *testing.T) {
	const half = "Higher numbers of humans on the square make the flock"
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{lengthStep("Higher numbers of ", "humans on the square make the flock")})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What happens at a busy event?", "lang": "en",
	})

	var text strings.Builder
	for _, f := range frames {
		if f["type"] == "text" {
			text.WriteString(f["delta"].(string))
		}
	}
	if text.String() != half {
		t.Errorf("de tekst is onderweg veranderd: %q, want %q", text.String(), half)
	}
	done := findFrame(frames, "done")
	if done == nil || done["stopReason"] != StopReasonTruncated {
		t.Fatalf("done = %+v, want stopReason %q", done, StopReasonTruncated)
	}
	if findFrame(frames, "error") != nil {
		t.Error("een afgekapt antwoord is geen fout: er hoort geen error-frame te staan")
	}

	stored := repo.messagesWithRole("assistant")
	if len(stored) != 1 {
		t.Fatalf("opgeslagen assistantberichten = %d, want 1", len(stored))
	}
	if stored[0].Content != half {
		t.Errorf("opgeslagen tekst = %q, want %q", stored[0].Content, half)
	}
	if stored[0].StopReason != StopReasonTruncated {
		t.Errorf("opgeslagen stopreden = %q, want %q", stored[0].StopReason, StopReasonTruncated)
	}
	// Partial hoort bij een gebroken stroom (fout, disconnect). De provider
	// maakte deze stream netjes af; alleen het antwoord is niet af.
	if stored[0].Partial {
		t.Error("een lengte-afsluiting is geen afgebroken stroom en hoort niet als partial te worden bewaard")
	}
}

func TestTruncation_PipelineNaturalStopStaysSilent(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{textStep("A complete answer.")})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What happens at a busy event?", "lang": "en",
	})
	done := findFrame(frames, "done")
	if done == nil {
		t.Fatal("geen done-frame")
	}
	if _, has := done["stopReason"]; has {
		t.Errorf("een normaal einde hoort geen stopreden te dragen: %+v", done)
	}
	stored := repo.messagesWithRole("assistant")
	if len(stored) != 1 || stored[0].StopReason != "" {
		t.Errorf("opgeslagen stopreden = %q, want leeg", stored[0].StopReason)
	}
}

// --- agentroute ------------------------------------------------------------

// De melding hoort bij het blok dat afbrak. De facilitator praat daarna gewoon
// door: zijn bemiddeling is niet afgekapt, en de beurt als geheel evenmin.
func TestTruncation_AgentMarksThePigeonBlockAndNotTheTurn(t *testing.T) {
	const half = "You want to stage your spectacle on my home turf, but the research cannot"
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		lengthStep("You want to stage your spectacle on my home turf, ", "but the research cannot"),
		textStep("That is the pigeon's own reading of the square."),
	})

	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What about the event?", "lang": "en",
	})

	var text strings.Builder
	for _, f := range frames {
		if f["type"] == "artifact_delta" {
			text.WriteString(f["delta"].(string))
		}
	}
	if text.String() != half {
		t.Errorf("de tekst is onderweg veranderd: %q, want %q", text.String(), half)
	}
	closed := lastFrame(frames, "artifact")
	if closed == nil || closed["done"] != true {
		t.Fatalf("geen sluitend artifact-frame: %+v", closed)
	}
	if closed["stopReason"] != StopReasonTruncated {
		t.Errorf("artifact-close = %+v, want stopReason %q", closed, StopReasonTruncated)
	}
	if _, fout := closed["error"]; fout {
		t.Errorf("een afgekapt antwoord is geen fout op het blok: %+v", closed)
	}
	done := findFrame(frames, "done")
	if done == nil || done["stopReason"] != "natural" {
		t.Errorf("done = %+v, want stopReason natural: de facilitator eindigde wél normaal", done)
	}

	answers := repo.messagesWithKind(KindPigeonAnswer)
	if len(answers) != 1 {
		t.Fatalf("opgeslagen duifantwoorden = %d, want 1", len(answers))
	}
	if answers[0].Content != half {
		t.Errorf("opgeslagen tekst = %q, want %q", answers[0].Content, half)
	}
	if answers[0].StopReason != StopReasonTruncated {
		t.Errorf("opgeslagen stopreden = %q, want %q", answers[0].StopReason, StopReasonTruncated)
	}
	if answers[0].Partial {
		t.Error("een lengte-afsluiting hoort niet als afgebroken stroom te worden bewaard")
	}
	// De facilitator-rij draagt geen stopreden: zijn eigen beurt liep af.
	for _, msg := range repo.messagesWithKind(KindText) {
		if msg.Persona == PersonaFacilitator && msg.StopReason != "" {
			t.Errorf("facilitator-rij draagt stopreden %q; de afkapping was van de duif", msg.StopReason)
		}
	}
}

func TestTruncation_AgentNaturalPigeonBlockCarriesNoStopReason(t *testing.T) {
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		textStep("That pavement is mine, and I am staying on it."),
		textStep("That is the pigeon's own reading of the square."),
	})
	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What about the event?", "lang": "en",
	})
	closed := lastFrame(frames, "artifact")
	if closed == nil {
		t.Fatal("geen sluitend artifact-frame")
	}
	if _, has := closed["stopReason"]; has {
		t.Errorf("een normaal afgerond blok hoort geen stopreden te dragen: %+v", closed)
	}
	answers := repo.messagesWithKind(KindPigeonAnswer)
	if len(answers) != 1 || answers[0].StopReason != "" {
		t.Errorf("opgeslagen stopreden = %q, want leeg", answers[0].StopReason)
	}
}

// Een afgekapt antwoord wordt niet ingetrokken en niet overgeslagen: het
// blijft staan zoals het geleverd is, precies zoals een heel antwoord, en er
// loopt geen tweede modelcall meer achter.
func TestTruncation_AgentStillAuditsTheDeliveredText(t *testing.T) {
	const half = "The ledges they counted are the ones I sleep on [1], and I will not"
	m, _, mock, _ := kbAgentModule(t,
		[]kbSeed{{id: "trunc", samenvatting: "Synthetic summary about ledges.", score: .9,
			quote: answerSourcesFixture[0].Ref.Passages[0].Text, paper: "synthetic-study"}},
		[]llmtesting.StreamStep{
			askBiological(),
			lengthStep(half),
			textStep("The pigeon spoke for itself there."),
		},
		func(m *Module) { m.answerStream = nil })

	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "biological", "message": "Where do you sleep?", "lang": "en",
	})
	if closed := lastFrame(frames, "artifact"); closed == nil || closed["stopReason"] != StopReasonTruncated {
		t.Fatalf("artifact-close = %+v, want stopReason %q", closed, StopReasonTruncated)
	}
	if findFrame(frames, "audit") != nil {
		t.Fatal("de beurt eindigt bij done; er komt geen audit-frame meer")
	}
	if mock.CallCount() != 2 {
		t.Fatalf("een afgekapt antwoord kost twee calls (facilitator, duif), niet %d", mock.CallCount())
	}
}

// --- vergaderroute ---------------------------------------------------------

func TestTruncation_MeetingResponseDoneCarriesTheLengthClose(t *testing.T) {
	const half = "The counted ledges are where we roost, and what a festival does"
	rig, _ := groundedMeetingRig(t, []llmtesting.StreamStep{
		{Chunks: []llm.StreamChunk{
			{Content: "The counted ledges are where we roost, "},
			{Content: "and what a festival does", FinishReason: "length"},
		}},
	})
	id := rig.openSession(t, "biological", "en")
	events := rig.events(t, id)
	rig.say(t, id, "What does a festival do to you?")
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: %d %s", status, body)
	}

	var spoken strings.Builder
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events.ch:
			switch ev.Type {
			case EventText:
				spoken.WriteString(ev.Delta)
			case EventError:
				t.Fatalf("een lengte-afsluiting is geen storing: %+v", ev)
			case EventResponseDone:
				if ev.StopReason != StopReasonTruncated {
					t.Fatalf("response_done = %+v, want stopReason %q", ev, StopReasonTruncated)
				}
				if spoken.String() != half {
					t.Fatalf("de uitgesproken tekst is veranderd: %q, want %q", spoken.String(), half)
				}
				return
			}
		case <-deadline:
			t.Fatal("geen response_done op het vergaderkanaal")
		}
	}
}

func TestTruncation_MeetingNaturalStopStaysSilent(t *testing.T) {
	rig, _ := groundedMeetingRig(t, []llmtesting.StreamStep{
		{Chunks: []llm.StreamChunk{{Content: "A complete spoken turn.", FinishReason: "stop"}}},
	})
	id := rig.openSession(t, "biological", "en")
	events := rig.events(t, id)
	rig.say(t, id, "What does a festival do to you?")
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: %d %s", status, body)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events.ch:
			if ev.Type == EventResponseDone {
				if ev.StopReason != "" {
					t.Fatalf("een normaal einde hoort geen stopreden te dragen: %+v", ev)
				}
				return
			}
		case <-deadline:
			t.Fatal("geen response_done op het vergaderkanaal")
		}
	}
}

// --- het inhoudsfilter van de provider --------------------------------------

// Dezelfde machinerie, tweede afsluiting: sluit de provider op zijn eigen
// inhoudsfilter, dan heet dat "filtered" — het woord dat de gedeelde
// agent-lus en de client er al voor hebben. De tekst die al binnenkwam blijft
// staan; er wordt niets alsnog ingetrokken.
func TestProviderStop_PipelineReportsAContentFilterClose(t *testing.T) {
	const half = "The counted ledges are where"
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{closedStep("content_filter", half)})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock })
	usePipeline(t, m, "individualist")

	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What happens at a busy event?", "lang": "en",
	})
	done := findFrame(frames, "done")
	if done == nil || done["stopReason"] != StopReasonFiltered {
		t.Fatalf("done = %+v, want stopReason %q", done, StopReasonFiltered)
	}
	stored := repo.messagesWithRole("assistant")
	if len(stored) != 1 || stored[0].Content != half || stored[0].StopReason != StopReasonFiltered {
		t.Errorf("opgeslagen = %+v, want tekst %q met stopreden %q", stored, half, StopReasonFiltered)
	}
}

func TestProviderStop_AgentMarksAContentFilterCloseOnThePigeonBlock(t *testing.T) {
	const half = "That pavement is where"
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		closedStep("content_filter", half),
		textStep("That is the pigeon's own reading of the square."),
	})
	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What about the event?", "lang": "en",
	})
	closed := lastFrame(frames, "artifact")
	if closed == nil || closed["stopReason"] != StopReasonFiltered {
		t.Fatalf("artifact-close = %+v, want stopReason %q", closed, StopReasonFiltered)
	}
	answers := repo.messagesWithKind(KindPigeonAnswer)
	if len(answers) != 1 || answers[0].Content != half || answers[0].StopReason != StopReasonFiltered {
		t.Errorf("opgeslagen duifantwoord = %+v, want tekst %q met stopreden %q", answers, half, StopReasonFiltered)
	}
}

func TestProviderStop_MeetingResponseDoneCarriesAContentFilterClose(t *testing.T) {
	const half = "The counted ledges are where"
	rig, _ := groundedMeetingRig(t, []llmtesting.StreamStep{
		closedStep("content_filter", half),
	})
	id := rig.openSession(t, "biological", "en")
	events := rig.events(t, id)
	rig.say(t, id, "What does a festival do to you?")
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: %d %s", status, body)
	}
	var spoken strings.Builder
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events.ch:
			switch ev.Type {
			case EventText:
				spoken.WriteString(ev.Delta)
			case EventError:
				t.Fatalf("een inhoudsfilter is geen storing: %+v", ev)
			case EventResponseDone:
				if ev.StopReason != StopReasonFiltered {
					t.Fatalf("response_done = %+v, want stopReason %q", ev, StopReasonFiltered)
				}
				if spoken.String() != half {
					t.Fatalf("de uitgesproken tekst is veranderd: %q, want %q", spoken.String(), half)
				}
				return
			}
		case <-deadline:
			t.Fatal("geen response_done op het vergaderkanaal")
		}
	}
}

// Wat de provider verder ook meldt: als het geen normale stop is en wij het
// woord niet kennen, zwijgt de beurt erover. Een reden die de client niet aan
// de lezer kan uitleggen is erger dan geen reden.
func TestProviderStop_OnlyNamesTheTwoClosesWeCanExplain(t *testing.T) {
	for reason, want := range map[string]string{
		"length":         StopReasonTruncated,
		"content_filter": StopReasonFiltered,
		"stop":           "",
		"":               "",
		"tool_calls":     "",
		"LENGTH":         "",
		"something_new":  "",
	} {
		if got := providerStop(reason); got != want {
			t.Errorf("providerStop(%q) = %q, want %q", reason, got, want)
		}
	}
}

// --- de budgetten zelf -----------------------------------------------------

// Het plafond is geen lengtemaat maar een som: wat het model vóór de eerste
// zichtbare letter aan redeneren besteedt, plus de zinnen zelf. De gemeten
// slechtste redenering kostte 1220 respectievelijk 1145 tokens — zie de
// constanten hieronder. Blijft het plafond bij die gemeten waarde in de buurt,
// dan is de stille afkapping terug.
func TestTruncation_TokenBudgetsLeaveRoomForReasoningPlusTheAnswer(t *testing.T) {
	// Beide getallen zijn rédeneertokens alleen, gemeten in vier ronden om en
	// om, per route de echte aanvraag, niet-gestreamd afgevuurd zodat de
	// respons usage draagt (een gestreamde call doet dat niet).
	//
	// 1220 hoort bij de routes die op meetingReasoningEffort ("low") denken.
	// De duurste waarneming was 1362 completion-tokens op de detectie en 1325
	// op de gesproken beurt; daar gaat de zichtbare staart vanaf (ruim 490
	// tekens, zo'n 145 tokens). Hoeveel "low" kost, verschilt sterk per
	// modelversie. De detectie staat nu op "none" (candidateReasoningEffort)
	// en houdt dezelfde ruimte als vangnet.
	//
	// 1145 hoort bij de chat, gemeten toen die nog op "medium" dacht:
	// 1249 completion-tokens min ruim 400 tekens zichtbare tekst. Inmiddels
	// denkt de chat op "low" (chatReasoningEffort); een kleine meting op het
	// huidige model (alleen het duif-antwoord op synthetische invoer, n=3)
	// kwam op 908-1145 redeneertokens, dus niet boven dit getal.
	const measuredReasoningWorstCase = 1220
	const measuredChatReasoningWorstCase = 1145
	// boundTokens (provider_limits.go) kapt alles boven dit getal af op 8192;
	// een plafond daarboven is een stille no-op.
	const providerBound = 8192
	for _, tc := range []struct {
		naam    string
		budget  int
		zicht   int
		ruimte  int
		gemeten int
	}{
		{"chat", answerChatMaxTokens, answerChatVisibleTokens, answerChatReasoningHeadroom, measuredChatReasoningWorstCase},
		{"vergadering", answerMeetingMaxTokens, answerMeetingVisibleTokens, answerReasoningHeadroom, measuredReasoningWorstCase},
	} {
		if tc.ruimte <= tc.gemeten {
			t.Errorf("%s: redeneerruimte %d ligt niet boven de gemeten %d",
				tc.naam, tc.ruimte, tc.gemeten)
		}
		if tc.budget != tc.ruimte+tc.zicht {
			t.Errorf("%s: budget %d is niet de som van redeneerruimte en zichtbare tekst", tc.naam, tc.budget)
		}
		if tc.budget > providerBound {
			t.Errorf("%s: budget %d ligt boven de providergrens %d", tc.naam, tc.budget, providerBound)
		}
	}
	// Een vergaderbeurt blijft korter dan een chatantwoord: 2-4 zinnen tegen
	// 3-5, en elke extra zin is extra synthesetijd.
	if answerMeetingMaxTokens >= answerChatMaxTokens {
		t.Errorf("vergaderbudget %d hoort onder het chatbudget %d te blijven", answerMeetingMaxTokens, answerChatMaxTokens)
	}
}

// --- de drie overdrachten -------------------------------------------------

// De stopreden wordt met de hand doorgegeven, en drie routes misten elk een
// overstap. Niet drie los gevonden gebreken: één contract dat op drie plekken
// overgeschreven moet worden en dat op drie plekken vergeten was.

// De zelfstandige vergaderroute schrijft haar bijdrage vooruit, lang vóór zij
// klinkt. Sloot de provider die stroom op het tokenplafond, dan draagt de
// afsluitende chunk dat woord en geen letter tekst meer — en sprak de duif tot
// nu toe een halve zin uit alsof zij was uitgesproken.
func TestTruncation_MeetingCandidateRouteReportsTheLengthClose(t *testing.T) {
	const half = "Noise changes how they use rooftops [1], and what a festival does"
	svc := newCandidateLLM(closedSpeak("length", 0.99, "pigeon-rights", half))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "biological", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	rig.say(t, id, "wat doet vuurwerk met de daken hier")
	waitForCandidate(t, s)

	if status, body := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Fatalf("laat horen werd geweigerd: status = %d, body = %s", status, body)
	}
	nextEvent(t, es.ch, EventPersona)
	spoken, evs := collectSpokenText(t, es.ch)
	if spoken != half {
		t.Errorf("de uitgesproken tekst is veranderd: %q, want %q", spoken, half)
	}
	if hasEvent(evs, EventError) {
		t.Errorf("een lengte-afsluiting is geen storing: %v", eventTypes(evs))
	}
	textDone := indexOfEvent(evs, EventTextDone)
	done := indexOfEvent(evs, EventResponseDone)
	if textDone < 0 || done < 0 {
		t.Fatalf("de zelfstandige bijdrage mist een sluitend event: %v", eventTypes(evs))
	}
	// Beide events, want de client leest de melding van allebei: text_done
	// zet haar neer zodra de tekst compleet is, response_done bevestigt haar
	// als de stem klaar is.
	if evs[textDone].StopReason != StopReasonTruncated {
		t.Errorf("text_done = %+v, want stopReason %q", evs[textDone], StopReasonTruncated)
	}
	if evs[done].StopReason != StopReasonTruncated {
		t.Errorf("response_done = %+v, want stopReason %q", evs[done], StopReasonTruncated)
	}
}

// En het spiegelbeeld: een kandidaat die het model gewoon afmaakte zwijgt.
func TestTruncation_MeetingCandidateNaturalStopStaysSilent(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Noise changes how they use rooftops [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "biological", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	rig.say(t, id, "wat doet vuurwerk met de daken hier")
	waitForCandidate(t, s)
	if status, body := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Fatalf("laat horen werd geweigerd: status = %d, body = %s", status, body)
	}
	nextEvent(t, es.ch, EventPersona)
	_, evs := collectSpokenText(t, es.ch)
	for _, ev := range evs {
		if ev.StopReason != "" {
			t.Errorf("een normaal einde hoort geen stopreden te dragen: %+v", ev)
		}
	}
}

// De bemiddeling van de facilitator kan zelf op het tokenplafond stuklopen.
// Live klopt de melding al — de `done` van de beurt draagt zijn stopreden —
// maar zij stond niet op zijn rij, dus bij het eerste herladen verdween zij en
// las een halve bemiddeling als een hele.
func TestTruncation_FacilitatorRowKeepsItsOwnStopReason(t *testing.T) {
	const half = "That is the pigeon's own reading, and what it means for the people here is"
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		textStep("That pavement is mine, and I am staying on it."),
		lengthStep(half),
	})

	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What about the event?", "lang": "en",
	})
	done := findFrame(frames, "done")
	if done == nil || done["stopReason"] != StopReasonTruncated {
		t.Fatalf("done = %+v, want stopReason %q", done, StopReasonTruncated)
	}

	rows := repo.messagesWithKind(KindText)
	var facilitator []ChatMessage
	for _, msg := range rows {
		if msg.Persona == PersonaFacilitator {
			facilitator = append(facilitator, msg)
		}
	}
	if len(facilitator) != 1 {
		t.Fatalf("opgeslagen facilitator-rijen = %d, want 1 (%+v)", len(facilitator), rows)
	}
	if facilitator[0].Content != half {
		t.Errorf("opgeslagen tekst = %q, want %q", facilitator[0].Content, half)
	}
	if facilitator[0].StopReason != StopReasonTruncated {
		t.Errorf("opgeslagen stopreden = %q, want %q: na herladen hoort dezelfde melding er te staan",
			facilitator[0].StopReason, StopReasonTruncated)
	}
	// Het blok van de duif eindigde wél netjes en blijft dus zwijgen.
	for _, answer := range repo.messagesWithKind(KindPigeonAnswer) {
		if answer.StopReason != "" {
			t.Errorf("duif-blok draagt stopreden %q; de afkapping was van de facilitator", answer.StopReason)
		}
	}
}

// Een beurt die gewoon eindigde laat de facilitator-rij leeg: "natural" is
// beurtboekhouding en zou na herladen een melding tonen die live niet stond.
func TestTruncation_FacilitatorRowStaysSilentOnANaturalTurn(t *testing.T) {
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		textStep("That pavement is mine, and I am staying on it."),
		textStep("That is the pigeon's own reading of the square."),
	})
	agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "What about the event?", "lang": "en",
	})
	for _, msg := range repo.messagesWithKind(KindText) {
		if msg.Persona == PersonaFacilitator && msg.StopReason != "" {
			t.Errorf("facilitator-rij draagt stopreden %q na een normale beurt", msg.StopReason)
		}
	}
}

// Vergelijken kopieert het antwoord van de eerste duif naar het gesprek van de
// tweede. Bleef de stopreden achter, dan droeg het brongesprek de melding wél
// en de kopie niet: dezelfde woorden, in het ene gesprek half en in het andere
// af.
func TestTruncation_ComparisonCopyKeepsTheStopReason(t *testing.T) {
	const half = "De richels die zij telden zijn waar wij slapen [1], en wat een festival"
	m, repo := newTestModule(t, nil)
	ctx := t.Context()
	_ = repo.CreateSession(ctx, ChatSession{ID: "origin", UserID: testUser.ID, PigeonID: "biological", Lang: "nl"})
	for _, message := range []ChatMessage{
		{ID: "question", SessionID: "origin", Role: "user", Content: "Wat betekent een nieuwe nestplek?"},
		{ID: "answer", SessionID: "origin", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer,
			Content: half, KBStatus: string(KBSources), Sources: comparisonSources,
			StopReason: StopReasonTruncated},
	} {
		_ = repo.AppendMessage(ctx, message)
	}

	session := comparisonIn(t, m, repo, "origin", "question", "individualist")
	messages, err := repo.ListMessages(t.Context(), session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var copied *ChatMessage
	for i, message := range messages {
		if message.Role == "assistant" {
			copied = &messages[i]
		}
	}
	if copied == nil {
		t.Fatalf("het antwoord reisde niet mee: %+v", messages)
	}
	if copied.Content != half {
		t.Errorf("gekopieerde tekst = %q, want %q", copied.Content, half)
	}
	if copied.StopReason != StopReasonTruncated {
		t.Errorf("gekopieerde stopreden = %q, want %q", copied.StopReason, StopReasonTruncated)
	}
}

// Een antwoord dat netjes eindigde reist zonder melding mee.
func TestTruncation_ComparisonCopyStaysSilentOnANaturalAnswer(t *testing.T) {
	m, repo := comparisonFixture(t)
	session := comparisonIn(t, m, repo, "origin", "question", "individualist")
	messages, err := repo.ListMessages(t.Context(), session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.StopReason != "" {
			t.Errorf("een normaal afgerond antwoord kreeg in de kopie stopreden %q", message.StopReason)
		}
	}
}
