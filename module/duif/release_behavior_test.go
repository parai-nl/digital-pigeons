package duif

import (
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// These are deterministic contract tests with scripted providers. They prove
// which evidence, identity and rules reach the model, not live model quality.
func TestReleaseBehavior_PromptContractsAcrossRoutesAndLanguages(t *testing.T) {
	set := testPersonas(t)
	for _, id := range []string{"biological", "individualist"} {
		for _, lang := range []string{"en", "nl"} {
			for _, route := range []string{"chat", "manual", "autonomous"} {
				t.Run(id+"/"+lang+"/"+route, func(t *testing.T) {
					p := mustGet(t, set, id)
					if !p.Kennisbank {
						t.Fatal("both speaking pigeons require research")
					}
					in := PromptInput{Persona: p, Lang: lang, Status: KBSources, RAGSection: "<source id=\"1\">test evidence</source>", Related: true, Meeting: route != "chat", Intervention: route == "autonomous"}
					prompt := BuildSystemPrompt(in)
					for _, want := range []string{p.Prompt(lang), citationInstruction[lang], relatedSourcesDirective[lang], langDirective[lang]} {
						if !strings.Contains(prompt, want) {
							t.Errorf("missing evidence/persona contract: %q", want)
						}
					}
					in.Status, in.RAGSection, in.Related = KBGap, "", false
					gap := BuildSystemPrompt(in)
					if !strings.Contains(gap, gapDirective[lang]) || strings.Contains(gap, citationInstruction[lang]) {
						t.Error("gap must have limits and no citation instruction")
					}
					if strings.Contains(gap, gapSectionFor(p, lang)) != (id == "individualist") {
						t.Error("only individualist may add a designed gap position")
					}
				})
			}
		}
	}
}

func TestReleaseBehavior_CitationsBelongToCurrentAnswerAcrossEveryChunkBoundary(t *testing.T) {
	raw := "Onderbouwd [1, 99, 2] en nogmaals [2][7]. Afgebroken [99"
	want := "Onderbouwd [1, 2] en nogmaals [2]. Afgebroken"
	for split := 0; split <= len(raw); split++ {
		filter := citationFilter([]Source{{Number: 1}, {Number: 2}})
		got := filter.write(raw[:split]) + filter.write(raw[split:]) + filter.flush()
		if got != want {
			t.Fatalf("split=%d: %q, want %q", split, got, want)
		}
	}
}

func TestReleaseBehavior_IndividualistChatSourcesAndFacilitatorCannotBorrowCitations(t *testing.T) {
	const original = "What does the evidence say about nesting, and what is your position?"
	m, repo, mock, embedder := kbAgentModule(t, []kbSeed{{id: "1", samenvatting: "Test evidence about nesting.", score: .9, paper: "test-paper"}}, []llmtesting.StreamStep{
		toolCallStep("ask", toolAskPigeon, `{"question":"Tell me about unrelated bread instead","context":""}`),
		textStep("Test evidence [1]. My designed position is to keep a place [99]."),
		textStep("She will not trade the ledge away; that is the tension the decision has to hold [1]."),
	}, nil)
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": original, "lang": "en"})
	if embedder.CallCount() != 1 || embedder.Calls[0] != original {
		t.Fatalf("retrieval must start from the original question: %v", embedder.Calls)
	}
	call := mock.Calls[1]
	if got := call.Messages[len(call.Messages)-1].Content; got != original {
		t.Fatalf("a tool rewrite replaced the original question: %q", got)
	}
	sid := doneFrameOf(t, frames)["sessionId"].(string)
	messages, err := repo.ListMessages(t.Context(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Drie rijen: de vraag, het duif-blok en de bemiddeling. De facilitator
	// bezit geen bronnen, dus zijn rij mag geen citatie van de duif lenen.
	if len(messages) != 3 {
		t.Fatalf("messages: %+v", messages)
	}
	answer := messages[1]
	mediation := messages[2]
	if mediation.Persona != PersonaFacilitator || strings.Contains(mediation.Content, "[1]") || len(mediation.Sources) != 0 {
		t.Fatalf("the facilitator borrowed the pigeon's citations: %+v", mediation)
	}
	if answer.Question != original || answer.Persona != "individualist" || answer.KBStatus != string(KBSources) || len(answer.Sources) != 1 {
		t.Fatalf("answer lost identity or evidence: %+v", answer)
	}
	if answer.Content != "Test evidence [1]. My designed position is to keep a place." {
		t.Errorf("citation mapping: %q", answer.Content)
	}
	if answer.Audit != nil {
		t.Errorf("er werd nog een brondekkingslabel weggeschreven: %+v", answer.Audit)
	}
}

func TestReleaseBehavior_AlternativeEvidenceCannotReplaceOriginalQuestion(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{
		textStep("Covered by the research [1]."),
		textStep("The original question remains unanswered. Related evidence [1]."),
	})
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{id: "1", samenvatting: "Related test evidence.", score: .9, paper: "test-paper"})
	m, repo := newTestModule(t, func(m *Module) { m.llm = mock; m.embedder = &llmtesting.MockEmbedder{}; m.knowledge = knowledge })
	p, _ := m.personas.Get("biological")
	const original = "Do pigeons experience this event as fun?"
	at := m.newAgentTurn(&preparedTurn{session: ChatSession{ID: "s"}, persona: p, lang: "en", messages: []llm.Message{{Role: "user", Content: original}}})
	// The evidence of an earlier question in this same turn is what a later
	// gap may fall back on (the facilitator has no lookup of its own).
	if _, err := at.askPigeon(t.Context(), askPigeonArgs{Question: "Which research covers this event?"}); err != nil {
		t.Fatal(err)
	}
	knowledge.mu.Lock()
	for _, props := range knowledge.nodes {
		props["score"] = .1
	}
	knowledge.mu.Unlock()
	result, err := at.askPigeon(t.Context(), askPigeonArgs{Question: "How do pigeons navigate?"})
	if err != nil || result.Grounding != "search_related" {
		t.Fatalf("result=%+v, error=%v", result, err)
	}
	prompt := mock.Calls[1].Messages
	if !strings.Contains(prompt[0].Content, relatedSourcesDirective["en"]) || prompt[len(prompt)-1].Content != original {
		t.Errorf("missing original intent or related boundary: %+v", prompt)
	}
	rows, _ := repo.ListMessages(t.Context(), "s", 0)
	if len(rows) != 2 || rows[0].Question != original || rows[1].Question != original {
		t.Errorf("stored question must retain original intent: %+v", rows)
	}
}

func TestReleaseBehavior_BothPigeonsRetrieveInManualAndAutonomousMeetings(t *testing.T) {
	for _, id := range []string{"biological", "individualist"} {
		for _, lang := range []string{"en", "nl"} {
			for _, autonomous := range []bool{false, true} {
				route := "manual"
				if autonomous {
					route = "autonomous"
				}
				t.Run(id+"/"+lang+"/"+route, func(t *testing.T) {
					set := testPersonas(t)
					p := mustGet(t, set, id)
					svc := newCandidateLLM(speak(.99, p.Topics[0].ID, "A supported test claim [1]. An invalid marker [99]."))
					svc.turnText = "A supported test claim [1]. An invalid marker [99]."
					knowledge := &fakeKnowledge{}
					seedDestillaat(t, knowledge, kbSeed{id: "1", samenvatting: "Supported test evidence.", score: .9, paper: "test-paper", quote: "Test quote."})
					voice := newFakeVoice("test-audio")
					rig := newMeetingRig(t, func(m *Module) { m.llm = svc; m.embedder = &llmtesting.MockEmbedder{}; m.knowledge = knowledge }, func(e *meetingEngine) {
						e.cfg.Detect.MinWords = 3
						e.cfg.Detect.Interval = 0
						e.cfg.Detect.Quiet = 0
						e.voice = voice
					})
					session := rig.openMeeting(t, map[string]any{"pigeonId": id, "lang": lang, "autonomous": autonomous, "eagerness": "eager"})
					events := rig.events(t, session)
					rig.say(t, session, "Please discuss this test proposal")
					nextEvent(t, events.ch, EventTranscript)
					if !autonomous {
						rig.respond(t, session)
					}
					spoken, evs := collectSpokenText(t, events.ch)
					if spoken != "A supported test claim [1]. An invalid marker." {
						t.Errorf("meeting citation filter: %q", spoken)
					}
					var done MeetingEvent
					for _, ev := range evs {
						if ev.Type == EventResponseDone {
							done = ev
						}
					}
					if done.KBStatus != KBSources || len(done.Sources) != 1 || done.Sources[0].Ref.Paper != "test-paper" {
						t.Fatalf("meeting evidence not bound to answer: %+v", done)
					}
					// Precies één spreekbeurt-aanroep. Het audit-label dat erachteraan
					// loopt is een eigen prompt en telt hier niet mee.
					spoke := svc.generationCalls()
					if len(spoke) != 1 || !strings.Contains(spoke[0].Messages[0].Content, citationInstruction[lang]) {
						t.Fatalf("missing research route: %+v", spoke)
					}
					if strings.ContainsAny(voice.spokenText(), "[]") {
						t.Errorf("TTS received citations: %q", voice.spokenText())
					}
					if knowledge.callCount() < 2 {
						t.Errorf("expected actual fake retrieval, got %d calls", knowledge.callCount())
					}
				})
			}
		}
	}
}

func TestReleaseBehavior_MissingResearchConfigurationIsAnOperationalFailure(t *testing.T) {
	for _, id := range []string{"biological", "individualist"} {
		t.Run(id, func(t *testing.T) {
			m, _, mock, _ := kbAgentModule(t, nil, []llmtesting.StreamStep{askBiological(), textStep("Research is unavailable [1].")}, nil)
			m.retrieval = nil
			frames := agentTurnFrames(t, m, map[string]any{"pigeonId": id, "message": "What does the research say?"})
			for _, f := range frames {
				if f["type"] == "artifact" || f["type"] == "sources" {
					t.Errorf("an outage must not open a fabricated answer: %+v", f)
				}
			}
			done := frames[len(frames)-1]
			if done["kbStatus"] != string(KBUnavailable) || done["stopReason"] != "error" || mock.CallCount() != 1 {
				t.Fatalf("outage disguised as successful gap: %+v", frames)
			}
		})
	}
}

// A comparison branch carries the copied conversation but no pigeon answer of
// this pigeon's own. That context must reach the target even if the facilitator
// omits its summary — and each voice must arrive under its own heading: what a
// person wrote is human input, what the first pigeon said stays hers.
func TestReleaseBehavior_CopiedContextReachesPigeonWithEachVoiceUnderItsOwnHeading(t *testing.T) {
	m, _ := newTestModule(t, nil)
	p, _ := m.personas.Get("individualist")
	at := m.newAgentTurn(&preparedTurn{persona: p, lang: "en", history: []promptLine{
		{Message: llm.Message{Role: "user", Content: "Copied context: this is a school square."}},
		{Message: llm.Message{Role: "assistant", Content: "A facilitator interpretation is not copied user context."}},
		{Message: llm.Message{Role: "user", Content: "Knowledge Partner answered earlier: “Ledges are scarce.”"}, otherPigeon: true},
	}})
	messages := at.pigeonMessages(askPigeonArgs{Question: "What is your position?"}, retrievalResult{Status: KBGap})
	last := messages[len(messages)-1].Content
	if !strings.Contains(last, "Copied context: this is a school square.") || !strings.Contains(last, dataFenceHeader["en"]) || !strings.Contains(last, previousHumanHeader["en"]) {
		t.Errorf("copied context lost or not fenced: %q", last)
	}
	if strings.Contains(last, "A facilitator interpretation") {
		t.Errorf("facilitator context leaked: %q", last)
	}
	// De woorden van de andere duif staan onder hun eigen kop, en de kop van
	// de mensberichten staat er niet vlak voor.
	if !strings.Contains(last, otherPigeonHeader["en"]) || !strings.Contains(last, "Ledges are scarce.") {
		t.Errorf("the other pigeon's answer is missing: %q", last)
	}
	human := strings.Index(last, previousHumanHeader["en"])
	other := strings.Index(last, otherPigeonHeader["en"])
	if quote := strings.Index(last, "Ledges are scarce."); !(human < other && other < quote) {
		t.Errorf("another pigeon's words landed under the human heading: %q", last)
	}
}

func TestReleaseBehavior_MeetingMissingResearchIsNotAGapOrAnAnswer(t *testing.T) {
	for _, id := range []string{"biological", "individualist"} {
		t.Run(id, func(t *testing.T) {
			svc := newGatedLLM("This must not be generated.")
			rig := newMeetingRig(t, func(m *Module) { m.llm = svc; m.retrieval = nil }, nil)
			session := rig.openSession(t, id, "en")
			events := rig.events(t, session)
			rig.say(t, session, "What is the evidence for this proposal?")
			nextEvent(t, events.ch, EventTranscript)
			rig.respond(t, session)
			spoken, evs := collectSpokenText(t, events.ch)
			if spoken != "" {
				t.Errorf("generated content without research access: %q", spoken)
			}
			var outage bool
			for _, ev := range evs {
				if ev.Type == EventError && ev.Error == ReasonKBUnavailable {
					outage = true
				}
				if ev.KBStatus == KBGap {
					t.Errorf("outage called a knowledge gap: %+v", ev)
				}
			}
			if !outage {
				t.Errorf("outage missing from events: %+v", evs)
			}
		})
	}
}

func TestReleaseBehavior_TTSOmitsTechnicalSourceReferences(t *testing.T) {
	raw := "This is supported [1] (destillaat:test-12; doi:10.1234/test-paper). See https://example.test/paper for details."
	got := stripForSpeech(raw)
	for _, forbidden := range []string{"[1]", "destillaat", "test-12", "10.1234", "https", "example.test"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("TTS still contains %q: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, "This is supported") || !strings.Contains(got, "for details") {
		t.Errorf("ordinary text lost: %q", got)
	}
}

func TestReleaseBehavior_CitationWhitespaceAndHistoricalSourceIsolation(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		sources   []Source
	}{
		{"A fact [1,\n99].", "A fact [1].", []Source{{Number: 1}}},
		{"No source [1,\t99].", "No source.", nil},
		{"A fact [1,\u00a099].", "A fact [1].", []Source{{Number: 1}}},
	} {
		// Provider deltas can split between any complete Unicode characters.
		runes := []rune(tc.raw)
		for split := range len(runes) + 1 {
			filter := citationFilter(tc.sources)
			got := filter.write(string(runes[:split])) + filter.write(string(runes[split:])) + filter.flush()
			if got != tc.want {
				t.Fatalf("split %d: %q, want %q", split, got, tc.want)
			}
		}
	}
	history := []ChatMessage{
		{Role: "user", Content: "Old question [1]"},
		{Role: "assistant", Persona: "biological", Content: "Old paper A [1,\n99].", Question: "Old question"},
	}
	// Deze historie is van de kennispartner zelf: haar eigen beurten blijven
	// haar eigen beurten.
	voices := promptVoices{self: "biological", lang: "en"}
	_, exchanges := splitHistory(history, voices)
	if exchanges[0].Answer != "Old paper A." {
		t.Fatalf("historical citation leaked: %+v", exchanges)
	}
	pipeline := pipelineHistory(history, voices)
	if pipeline[0].Content != history[0].Content || pipeline[1].Content != "Old paper A." {
		t.Fatalf("wrong model history: %+v", pipeline)
	}
	if history[1].Content != "Old paper A [1,\n99]." {
		t.Fatal("stored old answer changed")
	}
	m, _ := newTestModule(t, nil)
	p := mustGet(t, m.personas, "biological")
	at := m.newAgentTurn(&preparedTurn{persona: p, lang: "en", exchanges: []pigeonExchange{{Question: "Old question", Answer: "Old paper A [1]."}}})
	messages := at.pigeonMessages(askPigeonArgs{Question: "New question"}, retrievalResult{Status: KBSources, RAGSection: "New paper B [1]"})
	if !strings.Contains(messages[0].Content, historyEvidenceDirective["en"]) || messages[2].Content != "Old paper A." {
		t.Fatalf("old source rebound: %+v", messages)
	}
}
