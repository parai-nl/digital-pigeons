//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/ai-maps-duiven/module/duif"
)

// assertAnswerRequests checks the actual requests captured at the local stub,
// not a role inferred from the number of calls. It never prints model or
// system request content.
//
// One turn is at most two calls now: the facilitator (chat only) or the
// meeting's query rewrite, and the pigeon's own single streaming answer. The
// source-check call that used to follow a grounded answer is gone.
func assertAnswerRequests(t *testing.T, reqs []chatRequest, persona, lang string, meeting, grounded bool, human []string, rendered string) {
	t.Helper()
	phases := []string{"answer"}
	if meeting {
		// Op beide vergaderroutes gaat er één korte
		// herformuleringscall aan de retrieval vooraf: het venster wordt eerst
		// tot één zoekvraag herleid. Hij staat vóór het antwoord en draagt
		// hetzelfde verplichte model.
		phases = append([]string{"query"}, phases...)
	} else {
		phases = append([]string{"router"}, phases...)
	}
	// De individualist bemiddelt in de chat: na een geslaagd antwoord volgt
	// één afsluitende iteratie zonder tools (personas.yaml, blok facilitator).
	// De kennispartner houdt de facilitator intern en heeft die ronde niet.
	if !meeting && persona == "individualist" {
		phases = append(phases, "mediation")
	}
	if len(reqs) != len(phases) {
		t.Fatalf("completion calls = %d, want exact phases %v", len(reqs), phases)
	}
	question := human[len(human)-1]
	for i, req := range reqs {
		if req.Model != "vertex/gemini-3.6-flash-eu" || req.phase() != phases[i] || !req.Stream {
			t.Fatalf("request %d violates model/phase/stream contract (phase %s)", i, req.phase())
		}
		switch phases[i] {
		case "query":
			assertMeetingQueryRequest(t, req, human)
		case "router":
			assertRouterRequest(t, req, lang)
		case "answer":
			assertPigeonAnswerRequest(t, req, lang, meeting, human, question)
		case "mediation":
			assertMediationRequest(t, req, rendered)
		}
	}
}

// assertMeetingQueryRequest toetst de herformuleringscall: hij krijgt het
// venster als gebruikersbericht mee, één instructie ervoor, en niets anders.
func assertMeetingQueryRequest(t *testing.T, req chatRequest, human []string) {
	t.Helper()
	if len(req.Messages) != 2 || !isInstruction(req.Messages[0]) || req.Messages[1].Role != "user" {
		t.Fatalf("query call has the wrong shape: %+v", req.Messages)
	}
	if len(human) > 0 && !strings.Contains(req.Messages[1].Content, human[len(human)-1]) {
		t.Fatal("query call did not receive the transcript window")
	}
}

func assertRouterRequest(t *testing.T, req chatRequest, lang string) {
	t.Helper()
	if len(req.Messages) < 2 || req.Messages[0].Role == "user" {
		t.Fatal("router instruction lost its system/developer role")
	}
	if lang == "nl" && !strings.Contains(req.Messages[0].Content, fixtureDutchDirective) {
		t.Fatal("router lost the Dutch language directive")
	}
	var names []string
	for _, raw := range req.Tools {
		var tool struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(raw, &tool) != nil {
			t.Fatal("invalid tool schema")
		}
		names = append(names, tool.Function.Name)
	}
	sort.Strings(names)
	// De facilitator zoekt niet zelf: ask_pigeon doet de
	// retrieval op de mensvraag, dus de router heeft geen zoektool meer.
	want := []string{"ask_pigeon", "clarify", "pigeon_profile", "redirect_to_pigeon"}
	if !reflect.DeepEqual(names, want) || string(req.ToolChoice) != fixtureRouterChoice || req.heeftToolAntwoord() {
		t.Fatal("router must request exactly the controlled action tools, before any result")
	}
}

// The closing mediation is the facilitator's only free-prose turn. It carries
// no tools at all, so nothing it produces can be dispatched, and it must not
// arrive before the pigeon has actually answered.
func assertMediationRequest(t *testing.T, req chatRequest, rendered string) {
	t.Helper()
	if len(req.Tools) != 0 || string(req.ToolChoice) != fixtureNoneChoice {
		t.Fatalf("the closing mediation must carry no tools: tools=%d choice=%s", len(req.Tools), req.ToolChoice)
	}
	if !req.heeftToolAntwoord() {
		t.Fatal("mediation ran before the pigeon's tool result was in the history")
	}
	if rendered == "" {
		t.Fatal("mediation ran without a delivered pigeon answer")
	}
}

// The pigeon speaks once, with her real context: her persona prompt as system
// authority, her own history, and the human's original question last. No tool,
// no schema and no rewrite stands between her and the reader.
func assertPigeonAnswerRequest(t *testing.T, req chatRequest, lang string, meeting bool, human []string, question string) {
	t.Helper()
	if len(req.Tools) != 0 || req.ResponseFormat.Type != "" || req.ResponseFormat.JSONSchema.Name != "" {
		t.Fatal("the pigeon's answer acquired tools or a response schema")
	}
	// Provider budgets include reasoning tokens; the persona prompt still
	// controls the length of the answer shown to the user. The ceiling comes
	// from the server's own constant: a number copied into this test proves
	// only that someone updated two places at once.
	bound := duif.AnswerChatMaxTokens
	if meeting {
		bound = duif.AnswerMeetingMaxTokens
	}
	if req.MaxTokens != bound {
		t.Fatalf("answer token limit = %d, want %d", req.MaxTokens, bound)
	}
	if len(req.Messages) < 2 || !isInstruction(req.Messages[0]) || req.Messages[len(req.Messages)-1].Role != "user" {
		t.Fatal("the answer call lost its system prompt or its human question")
	}
	if lang == "nl" && !strings.Contains(req.Messages[0].Content, fixtureDutchDirective) {
		t.Fatal("the pigeon lost the session language")
	}
	if !strings.Contains(req.Messages[len(req.Messages)-1].Content, question) {
		t.Fatal("the original human question did not reach the pigeon")
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, fixtureRouterRewrite) {
			t.Fatal("a router rewrite replaced the original question")
		}
		if !isInstruction(m) {
			continue
		}
		for _, utterance := range human {
			if utterance != "" && strings.Contains(m.Content, utterance) {
				t.Fatal("human utterance acquired system authority")
			}
		}
	}
}

// waitForRequests waits until the stub has seen at least n calls.
func (s *duifServer) waitForRequests(t *testing.T, n int) []chatRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		reqs := s.llm.requests()
		if len(reqs) >= n || time.Now().After(deadline) {
			return reqs
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertProvenance(t *testing.T, p map[string]string, versioned bool) {
	t.Helper()
	if p["model"] != "vertex/gemini-3.6-flash-eu" || p["contractVersion"] == "" || len(p["personaVersion"]) != 64 || len(p["promptVersion"]) != 64 {
		t.Fatal("answer provenance lacks model/contract/persona/prompt identity")
	}
	if versioned && (p["kbState"] != "versioned" || p["kbId"] != kbFixtureID || len(p["kbVersion"]) != 64) {
		t.Fatal("answer provenance lacks the imported corpus version")
	}
}

// Always clean this subtest's own session, even when a later assertion fails.
// A failed answer check must not cause the next case's capacity failure.
func (s *duifServer) cleanupMeeting(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.baseURL+"/api/duif/v1/meeting/sessions/"+id, nil)
		if err != nil {
			t.Error("could not prepare own meeting cleanup")
			return
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error("own meeting cleanup failed")
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
			t.Errorf("own meeting cleanup status = %d", response.StatusCode)
		}
	})
}

// Offline fixture checks use ResponseRecorder; no server, socket, DB or model.
// They pin what the stub itself promises, so a boot failure can be read as a
// product failure rather than a fixture surprise.
func TestStubAnswerContract(t *testing.T) {
	run := func(req chatRequest) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(req)
		rr := httptest.NewRecorder()
		(&stubLLM{}).handleChat(rr, httptest.NewRequest(http.MethodPost, "http://fixture.invalid/chat/completions", bytes.NewReader(raw)))
		return rr
	}
	answerRequest := func(system, human string) chatRequest {
		return chatRequest{Model: "vertex/gemini-3.6-flash-eu", Stream: true, Messages: []chatMessage{
			{Role: "system", Content: system}, {Role: "user", Content: human},
		}}
	}

	// A gap answer in Dutch: an honest limit, no invented evidence, and the
	// designed position only when the individualist is speaking.
	gap := answerRequest(fixtureDutchDirective, "SYNTHETIC_HUMAN")
	text, err := fixtureAnswerText(gap)
	if err != nil || !strings.Contains(text, fixtureGapNL) || strings.Contains(text, fixturePositionNL) || strings.ContainsAny(text, "[]") {
		t.Fatalf("gap fixture invented evidence, a position or a citation: %q %v", text, err)
	}
	individual := answerRequest(fixtureDutchDirective+"\n"+fixtureIndividual, "SYNTHETIC_HUMAN")
	if text, err = fixtureAnswerText(individual); err != nil || !strings.Contains(text, fixturePositionNL) {
		t.Fatalf("individualist fixture lost its designed position: %q %v", text, err)
	}

	// A human who writes a citation marker gets no source out of it: the
	// fixture emits the marker, and the server is what must remove it.
	marked := answerRequest(fixtureDutchDirective, "SYNTHETIC_HUMAN "+citationTrigger)
	if text, err = fixtureAnswerText(marked); err != nil || !strings.Contains(text, "[1]") {
		t.Fatalf("citation trigger fixture changed: %q %v", text, err)
	}

	// The retrieved passage, and only the retrieved passage, produces the
	// grounded sentence. A human sentence that merely quotes it does not.
	grounded := answerRequest(fixtureDutchDirective+"\n"+fixtureRAGHeaderNL+"\n"+kbQuoteNesting, "SYNTHETIC_HUMAN")
	if text, err = fixtureAnswerText(grounded); err != nil || !strings.Contains(text, strings.TrimSuffix(fixtureNestingAnswer, ".")+" [1]") {
		t.Fatalf("grounded fixture lost its original passage relationship: %q %v", text, err)
	}
	humanQuote := answerRequest(fixtureDutchDirective, kbQuoteNesting)
	if text, err = fixtureAnswerText(humanQuote); err != nil || strings.Contains(text, fixtureNestingAnswer) {
		t.Fatalf("a human sentence became evidence: %q %v", text, err)
	}

	// Complete SSE framing for the remaining phases, and nothing for a shape
	// the stub does not recognise.
	for _, req := range []chatRequest{gap, grounded} {
		rr := run(req)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"finish_reason":"stop"`) || !strings.HasSuffix(rr.Body.String(), "data: [DONE]\n\n") {
			t.Fatal("recognized phase lost complete SSE framing")
		}
	}
	// The pigeon streams in pieces, so the meeting voice can start speaking
	// the first sentence while the rest is still being written.
	if strings.Count(run(grounded).Body.String(), `"content":`) < 2 {
		t.Fatal("the answer fixture stopped streaming in pieces")
	}
	// Elk gestructureerd verzoek is een onbekende fase: er is
	// geen modelcall in een beurt meer die JSON terugvraagt.
	unknownSchema := gap
	unknownSchema.ResponseFormat = fixtureSchemaFormat("duiven_source_answer")
	for _, req := range []chatRequest{unknownSchema, {Model: "old-model", Messages: gap.Messages, Stream: true}} {
		if run(req).Code != http.StatusBadRequest {
			t.Fatal("an unknown phase or model became free-prose fallback")
		}
	}

	// The facilitator route: untrusted tool arguments once, then at most the
	// one toolless mediation — never a second paid round that still has tools.
	router := chatRequest{Model: "vertex/gemini-3.6-flash-eu", Stream: true, Messages: []chatMessage{{Role: "system", Content: "SYNTHETIC_ROUTER"}, {Role: "user", Content: "SYNTHETIC_HUMAN"}}, Tools: []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"ask_pigeon"}}`)}, ToolChoice: json.RawMessage(fixtureRouterChoice)}
	if rr := run(router); rr.Code != 200 || !strings.Contains(rr.Body.String(), fixtureRouterRewrite) || !strings.Contains(rr.Body.String(), fixtureRouterContext) || !strings.Contains(rr.Body.String(), fixtureRouterFraming) || !strings.Contains(rr.Body.String(), `"finish_reason":"tool_calls"`) {
		t.Fatal("router fixture did not exercise untrusted tool arguments and a framing sentence")
	}
	router.Messages = append(router.Messages, chatMessage{Role: "tool", Content: "SYNTHETIC_RESULT", ToolCallID: "call-stub-1"})
	if run(router).Code != http.StatusBadRequest {
		t.Fatal("obsolete paid conclusion became successful fixture prose")
	}
	// The closing mediation is the one exception: it comes after the tool
	// result, carries no tools and asks for none, and streams free prose.
	mediation := router
	mediation.Tools = nil
	mediation.ToolChoice = json.RawMessage(fixtureNoneChoice)
	if rr := run(mediation); rr.Code != 200 || !strings.Contains(rr.Body.String(), fixtureRouterMediation) || !strings.Contains(rr.Body.String(), `"finish_reason":"stop"`) {
		t.Fatalf("mediation fixture lost its toolless closing prose: %d %s", rr.Code, rr.Body.String())
	}
}

// phaseSequence leest de stand-frames uit een ruwe turn-body: de
// stappen van de beurt in de volgorde waarin de lezer ze te zien kreeg.
//
// Ze worden hier uit de body gehaald en niet uit de framelijst, omdat postTurn
// ze er bewust uit laat: het zijn statusframes, geen inhoud. Ze dragen niets
// dan hun naam — een fase met een extra veld (een zoekopdracht, een vraag) is
// hier meteen een fout.
func phaseSequence(t *testing.T, raw string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var f sseFrame
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f) != nil {
			continue
		}
		if f["type"] != "phase" {
			continue
		}
		if len(f) != 2 {
			t.Fatalf("a phase frame carries more than its stand: %+v", f)
		}
		phase, _ := f["phase"].(string)
		out = append(out, phase)
	}
	return out
}

// assertPhaseOrder pint de stappen van een chatbeurt: eerst de vraag
// formuleren, dan de bronnen, dan het antwoord, en bij een bemiddelende duif
// de afronding. Elke stap staat vóór het werk dat hij aankondigt; een stand
// die pas ná het wachten komt vertelt de lezer niets.
func assertPhaseOrder(t *testing.T, raw string, mediation bool) {
	t.Helper()
	phases := phaseSequence(t, raw)
	if len(phases) == 0 {
		t.Fatal("the turn never said which step it was on")
	}
	if phases[0] != "facilitator" {
		t.Fatalf("first step = %q, want facilitator", phases[0])
	}
	// De brede zoekactie na een kennisgat is optioneel; de rest ligt vast.
	var kern []string
	for _, phase := range phases {
		switch phase {
		case "facilitator", "pigeon_grounding", "pigeon", "mediation":
			kern = append(kern, phase)
		case "pigeon_related_search":
		default:
			t.Fatalf("unknown step %q on the wire", phase)
		}
	}
	want := []string{"facilitator", "pigeon_grounding", "pigeon"}
	if mediation {
		want = append(want, "mediation")
	}
	if !reflect.DeepEqual(kern, want) {
		t.Fatalf("steps = %v, want %v", kern, want)
	}
}

func coalescedFrameTypes(frames []sseFrame) []string {
	var phases []string
	for _, typ := range frameTypes(frames) {
		if len(phases) == 0 || phases[len(phases)-1] != typ {
			phases = append(phases, typ)
		}
	}
	return phases
}
