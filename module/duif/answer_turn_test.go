package duif

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// groundedChatModule is a facilitator chat whose knowledge base answers, so a
// turn reaches kbStatus=sources — the state that used to buy a second model
// call for the source-check label.
func groundedChatModule(t *testing.T, answer string, tune func(m *Module)) (*Module, *fakeRepo, *llmtesting.MockService) {
	t.Helper()
	m, repo, mock, _ := kbAgentModule(t,
		[]kbSeed{{id: "label", samenvatting: "Synthetic summary about ledges.", score: .9, quote: answerSourcesFixture[0].Ref.Passages[0].Text, paper: "synthetic-study"}},
		[]llmtesting.StreamStep{askBiological(), textStep(answer)},
		func(m *Module) {
			m.answerStream = nil
			if tune != nil {
				tune(m)
			}
		})
	return m, repo, mock
}

// De beurt eindigt bij `done`. Een brongedragen antwoord — precies de stand
// die vroeger een tweede modelcall en een `audit`-frame opleverde
// — sluit nu af op het done-frame, en er gaat niets meer over de lijn daarna.
// De rij die eruit komt draagt geen label meer.
func TestAnswerTurn_GroundedChatEndsAtDoneWithoutAnAuditFrame(t *testing.T) {
	const answer = "In the northern enclosure, the authors proposed a possible pattern [1]."
	m, repo, mock := groundedChatModule(t, answer, nil)

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "biological", "message": "What do the sources establish?", "lang": "en"})
	types := frameTypes(frames)
	if types[len(types)-1] != "done" {
		t.Fatalf("de beurt hoort op done te eindigen, kreeg %v", types)
	}
	for _, frame := range frames {
		if frame["type"] == "audit" {
			t.Fatalf("er kwam alsnog een audit-frame: %+v", frame)
		}
	}
	done := doneFrameOf(t, frames)
	if done["kbStatus"] != string(KBSources) {
		t.Fatalf("dit moet de brongedragen stand zijn: %+v", done)
	}
	// Twee betaalde calls: de facilitator en de duif zelf. Geen derde.
	if mock.CallCount() != 2 {
		t.Fatalf("modelcalls = %d, want 2 (facilitator, duif)", mock.CallCount())
	}
	stored, err := repo.ListMessages(t.Context(), done["sessionId"].(string), 0)
	if err != nil {
		t.Fatal(err)
	}
	var answers int
	for _, row := range stored {
		if row.Kind == KindPigeonAnswer {
			answers++
			if row.Audit != nil {
				t.Fatalf("er werd nog een label weggeschreven: %+v", row.Audit)
			}
		}
	}
	if answers != 1 {
		t.Fatalf("het duif-antwoord werd %d keer bewaard", answers)
	}
}

// Dezelfde afspraak aan de vergadertafel: de beurt is klaar bij response_done
// en er volgt geen audit-event meer. Terwijl de tekst nog liep kreeg de stem
// haar zinnen al, en het tokenplafond van een gesproken beurt blijft het zijne.
func TestAnswerTurn_MeetingEndsAtResponseDoneWithoutAnAuditEvent(t *testing.T) {
	const first = "In the northern enclosure, the authors proposed a possible pattern [1]."
	const second = "That limit belongs to the study."
	rig, mock := groundedMeetingRig(t, []llmtesting.StreamStep{
		{Chunks: []llm.StreamChunk{{Content: first + " "}, {Content: second, FinishReason: "stop"}}},
	})
	id := rig.openSession(t, "biological", "en")
	events := rig.events(t, id)
	rig.say(t, id, "What do the sources establish about those ledges?")
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: %d %s", status, body)
	}

	var responseDone *MeetingEvent
	deadline := time.After(5 * time.Second)
	for responseDone == nil {
		select {
		case ev := <-events.ch:
			if ev.Type == "audit" {
				t.Fatalf("de vergadering stuurde alsnog een audit-event: %+v", ev)
			}
			if ev.Type == EventResponseDone {
				copied := ev
				responseDone = &copied
			}
		case <-deadline:
			t.Fatal("geen response_done op het vergaderkanaal")
		}
	}
	// Nog even luisteren: een label liep vroeger seconden achter de beurt aan,
	// dus een kanaal dat meteen stil is bewijst pas iets ná die wachttijd.
	select {
	case ev := <-events.ch:
		if ev.Type == "audit" {
			t.Fatalf("de vergadering stuurde alsnog een audit-event: %+v", ev)
		}
	case <-time.After(300 * time.Millisecond):
	}

	voice, ok := rig.engine.voice.(*fakeVoice)
	if !ok {
		t.Fatal("meeting rig lost its voice fixture")
	}
	spoken := voice.spokenChunks()
	if len(spoken) < 2 || !strings.Contains(spoken[0], "proposed a possible pattern") {
		t.Fatalf("TTS did not receive sentences during the stream: %q", spoken)
	}
	if strings.ContainsAny(strings.Join(spoken, " "), "[]") {
		t.Fatalf("TTS received citation markers: %q", spoken)
	}
	for _, call := range mock.Calls {
		if call.MaxTokens != answerMeetingMaxTokens {
			t.Fatalf("meeting answer bound = %d, want %d", call.MaxTokens, answerMeetingMaxTokens)
		}
	}
}

// The pigeon sees her persona prompt and her own earlier exchange. Before
// this, the answer call ignored req.Messages entirely and she had no memory.
func TestAnswerTurn_PigeonCallCarriesPersonaPromptAndHerOwnMemory(t *testing.T) {
	const earlierQuestion = "EARLIER_HUMAN_QUESTION: which ledges were studied?"
	const earlierAnswer = "EARLIER_PIGEON_ANSWER: the northern enclosure was studied [1]."
	const followUp = "FOLLOW_UP_HUMAN_QUESTION: and what did that tell you?"

	m, repo, mock := groundedChatModule(t, "A synthetic follow-up answer [1].", nil)
	if err := repo.CreateSession(t.Context(), ChatSession{ID: "memory", UserID: testUser.ID, PigeonID: "biological", Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []ChatMessage{
		{ID: "q1", Role: "user", Content: earlierQuestion, Kind: KindText},
		{ID: "a1", Role: "assistant", Persona: "biological", Kind: KindPigeonAnswer, Question: earlierQuestion, Content: earlierAnswer},
	} {
		row.SessionID = "memory"
		if err := repo.AppendMessage(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}

	agentTurnFrames(t, m, map[string]any{"sessionId": "memory", "pigeonId": "biological", "message": followUp, "lang": "en"})

	var pigeon *llm.CompletionRequest
	for i, call := range mock.Calls {
		if len(call.Tools) == 0 && len(call.Messages) > 0 {
			pigeon = &mock.Calls[i]
			break
		}
	}
	if pigeon == nil {
		t.Fatal("no pigeon call was made")
	}
	if pigeon.Messages[0].Role != "system" || !strings.Contains(pigeon.Messages[0].Content, "eis:P1") {
		t.Fatalf("the pigeon lost her persona system prompt: %+v", pigeon.Messages[0])
	}
	if pigeon.MaxTokens != answerChatMaxTokens {
		t.Fatalf("chat answer bound = %d, want %d", pigeon.MaxTokens, answerChatMaxTokens)
	}
	// Elke chatcall denkt op chatReasoningEffort; dat zijn er nog maar twee,
	// en geen van beide is een auditcall met een eigen regel.
	for _, call := range mock.Calls {
		if call.ReasoningEffort == nil || *call.ReasoningEffort != chatReasoningEffort {
			t.Fatalf("een chatcall mist reasoning effort %q: tools=%d", chatReasoningEffort, len(call.Tools))
		}
	}
	var history strings.Builder
	for _, message := range pigeon.Messages[1:] {
		history.WriteString(message.Role + ":" + message.Content + "\n")
	}
	// Her own earlier answer, the question it belonged to, and the new one.
	// Historical citation numbers are local to their old answer and are gone.
	for _, want := range []string{earlierQuestion, "EARLIER_PIGEON_ANSWER: the northern enclosure was studied.", followUp} {
		if !strings.Contains(history.String(), want) {
			t.Fatalf("the pigeon has no memory of %q:\n%s", want, history.String())
		}
	}
	if strings.Contains(history.String(), earlierAnswer) {
		t.Fatal("an old citation number was lent to a new source list")
	}
}

// Een kennisgat betaalde al geen auditcall; nu betaalt geen enkel antwoord er
// nog een. De stand blijft wel zichtbaar op het done-frame.
func TestAnswerTurn_GapAnswerCostsNoExtraCall(t *testing.T) {
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		textStep("I cannot establish that from the available sources."),
	})
	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "What do you know?", "lang": "en"})
	for _, frame := range frames {
		if frame["type"] == "audit" {
			t.Fatalf("een kennisgat kreeg een label: %+v", frame)
		}
	}
	if mock.CallCount() != 2 {
		t.Fatalf("modelcalls = %d, want 2 (facilitator, duif)", mock.CallCount())
	}
	stored, err := repo.ListMessages(t.Context(), doneFrameOf(t, frames)["sessionId"].(string), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range stored {
		if row.Audit != nil {
			t.Fatalf("a gap answer stored a label: %+v", row)
		}
	}
}

// Een lezer die wegloopt kost het antwoord zijn rij niet: de opslag draait op
// een van het verzoek losgekoppelde context.
func TestAnswerTurn_DisconnectedClientStillGetsItsAnswerStored(t *testing.T) {
	const answer = "In the northern enclosure, the authors proposed a possible pattern [1]."
	m, repo, _ := groundedChatModule(t, answer, nil)

	ctx, cancel := context.WithCancel(t.Context())
	body, _ := json.Marshal(map[string]any{"pigeonId": "biological", "message": "What do the sources establish?", "lang": "en"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/turn", strings.NewReader(string(body))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	// Six frames of this turn (tool, artifact, sources, delta, close,
	// tool_result) reach the reader; the tab closes before its done.
	writer := &disconnectingWriter{after: 6, cancel: cancel}
	serveTo(t, m, writer, req)

	messages := repo.messagesWithKind(KindPigeonAnswer)
	if len(messages) != 1 || messages[0].Content != answer {
		t.Fatalf("het antwoord van een weggelopen lezer werd niet bewaard: %+v", messages)
	}
}

// disconnectingWriter drops the reader after a few frames, like a closed tab.
type disconnectingWriter struct {
	mu     sync.Mutex
	header http.Header
	writes int
	after  int
	cancel context.CancelFunc
}

func (w *disconnectingWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}
func (w *disconnectingWriter) WriteHeader(int) {}
func (w *disconnectingWriter) Flush()          {}
func (w *disconnectingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	gone := w.writes > w.after
	w.mu.Unlock()
	if gone {
		w.cancel()
		return 0, errors.New("synthetic client disconnect")
	}
	return len(p), nil
}
