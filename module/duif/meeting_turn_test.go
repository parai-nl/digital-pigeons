package duif

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio/audiotesting"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// respond drukt op de knop "de duif antwoordt nu".
func (rig *meetingRig) respond(t *testing.T, sessionID string) (int, string) {
	t.Helper()
	return rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+sessionID+"/respond", nil)
}

func (rig *meetingRig) say(t *testing.T, sessionID, text string) {
	t.Helper()
	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+sessionID+"/transcript",
		map[string]any{"text": text, "speaker": "spreker-1"})
	if status != http.StatusAccepted {
		t.Fatalf("transcriptsegment: status = %d, body = %s", status, body)
	}
}

// collectSpokenText plakt de tekstdelta's van één beurt aan elkaar.
func collectSpokenText(t *testing.T, ch <-chan MeetingEvent) (string, []MeetingEvent) {
	t.Helper()
	var b strings.Builder
	var evs []MeetingEvent
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("kanaal ging dicht vóór response_done")
			}
			evs = append(evs, ev)
			if ev.Type == EventText {
				b.WriteString(ev.Delta)
			}
			if ev.Type == EventResponseDone {
				return b.String(), evs
			}
		case <-deadline:
			t.Fatalf("geen response_done binnen de deadline (kreeg %v)", eventTypes(evs))
			return "", evs
		}
	}
}

// De knop is het contract met de vergadering: venstersnapshot in,
// persona-antwoord + TTS-audio uit, over hetzelfde kanaal.
func TestMeetingRespondNow_StreamsPersonaTextAndAudio(t *testing.T) {
	gllm := newGatedLLM("Roekoe. ", "Ik vind daar wat van.")
	voice := newFakeVoice("mp3-frame-1", "mp3-frame-2")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
	rig.say(t, id, "Kan dat eigenlijk wel?")
	nextEvent(t, es.ch, EventTranscript)

	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: status = %d, body = %s", status, body)
	}

	persona := nextEvent(t, es.ch, EventPersona)
	if persona.PigeonID != "individualist" {
		t.Errorf("persona-event = %+v", persona)
	}
	text, evs := collectSpokenText(t, es.ch)
	if text != "Roekoe. Ik vind daar wat van." {
		t.Errorf("gesproken tekst = %q", text)
	}
	if !hasEvent(evs, EventTTSDone) {
		t.Errorf("tts_done ontbreekt: %v", eventTypes(evs))
	}

	var audio []string
	for _, ev := range evs {
		if ev.Type == EventTTSAudio {
			raw, err := base64.StdEncoding.DecodeString(ev.Audio)
			if err != nil {
				t.Fatalf("audioframe is geen base64: %v", err)
			}
			audio = append(audio, string(raw))
		}
	}
	if strings.Join(audio, "|") != "mp3-frame-1|mp3-frame-2" {
		t.Errorf("audioframes = %v", audio)
	}

	// De TTS-hook krijgt de volledige uiting (zin voor zin), de stem-id van de
	// persona en de sessietaal (de stemseam), en het tekstkanaal sluit één
	// keer.
	calls := voice.speakCalls()
	if len(calls) != 1 || calls[0].Lang != "nl" {
		t.Fatalf("TTS-aanroepen = %+v", calls)
	}
	if got := voice.spokenText(); got != text {
		t.Errorf("uitgesproken = %q, want %q", got, text)
	}
	if voice.endCount() != 1 {
		t.Errorf("End-aanroepen = %d, want 1", voice.endCount())
	}

	// De LLM ziet het venstersnapshot als invoer en het vergaderframe in de
	// systemprompt — in de sessietaal.
	call := gllm.lastCall(t)
	if len(call.Messages) != 2 || call.Messages[0].Role != "system" || call.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v", call.Messages)
	}
	system := call.Messages[0].Content
	if !strings.Contains(system, meetingDirective["nl"]) || !strings.Contains(system, langDirective["nl"]) {
		t.Errorf("systemprompt mist het vergader- of taaldirectief:\n%s", system)
	}
	if !strings.Contains(system, "eis:") {
		t.Errorf("systemprompt mist de persona-prompt:\n%s", system)
	}
	window := call.Messages[1].Content
	for _, want := range []string{"Stationsplein", "Kan dat eigenlijk wel?"} {
		if !strings.Contains(window, want) {
			t.Errorf("venster mist %q:\n%s", want, window)
		}
	}
	if call.Model != "test-model" {
		t.Errorf("model = %q", call.Model)
	}
}

// Leeg venster: een korte gescripte reactie, geen LLM-gok over
// niet-gehoorde inhoud.
func TestMeetingRespondNow_EmptyWindow_ScriptedWithoutLLM(t *testing.T) {
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond status = %d", status)
	}

	text, evs := collectSpokenText(t, es.ch)
	if text != meetingNoInputReply["nl"] {
		t.Errorf("tekst = %q, want de gescripte reactie", text)
	}
	if gllm.callCount() != 0 {
		t.Errorf("een leeg venster mag geen LLM-call kosten (got %d)", gllm.callCount())
	}
	if !hasEvent(evs, EventResponseDone) {
		t.Errorf("response_done ontbreekt: %v", eventTypes(evs))
	}
}

// Eén spreekslot per duif: een tweede knopdruk tijdens het spreken wordt
// genegeerd met een busy-event, niet met een tweede pipeline.
func TestMeetingRespondNow_SecondPressIsBusy(t *testing.T) {
	gllm := newGatedLLM("eerste ", "beurt")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Somebody asks the pigeon something.")
	nextEvent(t, es.ch, EventTranscript)

	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)

	status, body := rig.respond(t, id)
	if status != http.StatusConflict {
		t.Fatalf("tweede knopdruk: status = %d, body = %s (want 409)", status, body)
	}
	if ev := nextEvent(t, es.ch, EventBusy); ev.Message["en"] == "" {
		t.Errorf("busy-event moet een uitlegtekst dragen: %+v", ev)
	}

	gllm.let(t)
	gllm.let(t)
	if _, evs := collectSpokenText(t, es.ch); !hasEvent(evs, EventResponseDone) {
		t.Fatalf("beurt liep niet af: %v", eventTypes(evs))
	}
	if gllm.callCount() != 1 {
		t.Errorf("LLM-calls = %d, want 1: de tweede knopdruk mag geen pipeline starten", gllm.callCount())
	}

	// Na afloop is het slot weer vrij.
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Errorf("na de beurt moet de knop weer werken (status %d)", status)
	}
}

// heldLog houdt de eerste logregel met dit bericht vast tot de test hem
// loslaat. Zo staat de beurtgoroutine op een vaste plek stil, en is "net na
// response_done" een toestand in plaats van een timingkwestie.
type heldLog struct {
	slog.Handler
	msg     string
	once    sync.Once
	reached chan struct{}
	gate    chan struct{}
	open    sync.Once
}

func newHeldLog(msg string) *heldLog {
	logger, _ := captureLogs()
	return &heldLog{Handler: logger.Handler(), msg: msg, reached: make(chan struct{}), gate: make(chan struct{})}
}

func (h *heldLog) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.once.Do(func() {
			close(h.reached)
			<-h.gate
		})
	}
	return h.Handler.Handle(ctx, r)
}

func (h *heldLog) release() { h.open.Do(func() { close(h.gate) }) }

// response_done is voor de client het teken dat de duif klaar is en de knop
// weer werkt. Dan moet de server dat ook vinden: wie direct daarna drukt, mag
// geen busy krijgen van een beurt die al af is. De beurtgoroutine staat hier
// stil op haar slotlogregel, ná response_done; het spreekslot hoort dan al vrij
// te zijn.
func TestMeetingRespondNow_ResponseDoneMeansTheSlotIsFree(t *testing.T) {
	held := newHeldLog("vergaderbeurt klaar")
	t.Cleanup(held.release)
	rig := newMeetingRig(t, func(m *Module) { m.llm = newGatedLLM("klaar.") },
		func(e *meetingEngine) { e.logger = slog.New(held) })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Iemand vraagt de duif iets.")
	nextEvent(t, es.ch, EventTranscript)
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("eerste knopdruk: status = %d, body = %s", status, body)
	}
	if _, evs := collectSpokenText(t, es.ch); !hasEvent(evs, EventResponseDone) {
		t.Fatalf("beurt liep niet af: %v", eventTypes(evs))
	}
	select {
	case <-held.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("de beurt kwam niet bij haar slotlogregel")
	}

	status, body := rig.respond(t, id)
	held.release()
	if status != http.StatusAccepted {
		t.Errorf("knopdruk na response_done: status = %d, body = %s (want 202)", status, body)
	}
}

// Stoppen breekt de lopende beurt per direct af: LLM-stream geannuleerd,
// en ná `stopped` komt er geen enkel audioframe meer.
func TestMeetingStop_CancelsTurnAndSilencesAudio(t *testing.T) {
	gllm := newGatedLLM("ik ", "praat ", "door")
	gllm.gate()
	voice := newFakeVoice("frame-1", "frame-2")
	voice.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Iemand stelt de duif een vraag.")
	nextEvent(t, es.ch, EventTranscript)

	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)
	gllm.let(t)
	nextEvent(t, es.ch, EventText)

	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/stop", nil)
	if status != http.StatusAccepted {
		t.Fatalf("stop: status = %d, body = %s", status, body)
	}
	if !strings.Contains(body, `"wasSpeaking":true`) {
		t.Errorf("stopantwoord = %s, want wasSpeaking=true", body)
	}
	stopped := nextEvent(t, es.ch, EventStopped)
	if stopped.Type != EventStopped {
		t.Fatalf("stopped-event ontbreekt: %+v", stopped)
	}

	select {
	case <-gllm.canceled:
	case <-time.After(2 * time.Second):
		t.Error("de LLM-stream zag de annulering niet")
	}

	// Er mag nu niets meer van de afgebroken beurt binnenkomen.
	time.Sleep(50 * time.Millisecond)
	var naStop []string
	var gezien bool
	for _, ev := range es.recorded() {
		if ev.Type == EventStopped {
			gezien = true
			continue
		}
		if gezien {
			naStop = append(naStop, ev.Type)
		}
	}
	for _, typ := range naStop {
		if typ == EventTTSAudio || typ == EventText || typ == EventResponseDone || typ == EventTTSDone {
			t.Errorf("na stopped kwam er nog %q binnen (volledige staart: %v)", typ, naStop)
		}
	}

	// Luisteren loopt door: stoppen is geen sessie-einde.
	if rig.engine.count() != 1 {
		t.Error("de sessie hoort open te blijven na de stopknop")
	}
	rig.say(t, id, "de vergadering praat door")
	nextEvent(t, es.ch, EventTranscript)
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Error("na stoppen moet de knop meteen weer werken")
	}
}

// De TTS-sessie wordt óók afgebroken, ook als de LLM al klaar was.
func TestMeetingStop_CancelsPendingTTS(t *testing.T) {
	gllm := newGatedLLM("kort antwoord")
	voice := newFakeVoice("frame-1", "frame-2", "frame-3")
	voice.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Iemand stelt de duif een vraag.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	nextEvent(t, es.ch, EventText)
	voice.let(t)
	nextEvent(t, es.ch, EventTTSAudio)

	rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/stop", nil)
	nextEvent(t, es.ch, EventStopped)
	select {
	case <-voice.canceled:
	case <-time.After(2 * time.Second):
		t.Error("de TTS-sessie zag de annulering niet")
	}
}

// Stoppen terwijl de duif zwijgt is een no-op met bevestiging.
func TestMeetingStop_WithoutSpeaking_IsNoOp(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)

	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/stop", nil)
	if status != http.StatusAccepted {
		t.Fatalf("stop: status = %d, body = %s", status, body)
	}
	if !strings.Contains(body, `"wasSpeaking":false`) {
		t.Errorf("stopantwoord = %s, want wasSpeaking=false", body)
	}
	if ev := nextEvent(t, es.ch, EventStopped); ev.Type != EventStopped {
		t.Error("ook een no-op-stop wordt bevestigd")
	}
	if rig.engine.count() != 1 {
		t.Error("stoppen mag de sessie nooit sluiten")
	}
}

// Capfout mídden in de vergadering: nette foutmelding op het kanaal, en
// het spreekslot komt weer vrij — de sessie blijft luisteren.
func TestMeetingRespondNow_BudgetError_FreesTheSlot(t *testing.T) {
	rig := newMeetingRig(t, func(m *Module) { m.llm = failingLLM{err: ErrBudgetExceeded} }, nil)
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Somebody asks something expensive.")
	nextEvent(t, es.ch, EventTranscript)

	rig.respond(t, id)
	ev := nextEvent(t, es.ch, EventError)
	if ev.Error != ReasonBudgetExceeded || ev.Message["nl"] == "" {
		t.Fatalf("foutevent = %+v, want de tweetalige budgetmelding", ev)
	}
	if strings.Contains(strings.ToLower(ev.Message["en"]), "budget has been exceeded") {
		t.Error("de upstream-fouttekst hoort niet op de wire")
	}
	nextEvent(t, es.ch, EventResponseDone)
	if rig.engine.count() != 1 {
		t.Error("een LLM-fout mag de sessie niet slopen")
	}
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Error("na een fout hoort het spreekslot weer vrij te zijn")
	}
}

// Een fout mídden in de stream wordt een foutframe, geen kale stilte.
func TestMeetingRespondNow_MidStreamError_EmitsErrorFrame(t *testing.T) {
	gllm := newGatedLLM()
	gllm.chunks = []llm.StreamChunk{{Content: "ik begin nog"}, {Err: errStreamBroken}}
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Somebody asks something.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	if !hasEvent(evs, EventError) {
		t.Fatalf("foutframe ontbreekt: %v", eventTypes(evs))
	}
	for _, ev := range evs {
		if ev.Type == EventError && ev.Error != ReasonLLMUnavailable {
			t.Errorf("reason = %q, want llm_unavailable", ev.Error)
		}
	}
}

// De kennispartner spreekt in de vergadering op dezelfde grond als in de
// chat: bronnen boven de drempel, anders het kennisgat — nooit fabricatie.
func TestMeetingRespondNow_KnowledgePigeonUsesSources(t *testing.T) {
	gllm := newGatedLLM("Volgens het onderzoek [1] ligt dat anders.")
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{
		id: "1", samenvatting: "Duiven in de stad zijn een menselijk probleem.",
		score: 0.9, quote: "Wij hebben ze zelf hierheen gehaald.", paper: "jerolmack2008",
		perspective: "De kritische wetenschapper", themaTitel: "Overlast",
	})
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = knowledge
	}, nil)

	id := rig.openSession(t, "biological", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Are city pigeons a problem?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	var done MeetingEvent
	for _, ev := range evs {
		if ev.Type == EventResponseDone {
			done = ev
		}
	}
	if done.KBStatus != KBSources || len(done.Sources) != 1 {
		t.Fatalf("response_done = %+v, want kbStatus sources met één bron", done)
	}
	// response_done draagt hetzelfde DTO als de chat: id, tekst, citaat, paper.
	got := done.Sources[0]
	if got.Number != 1 || got.ID != "destillaat:1" || got.Text == "" {
		t.Errorf("bron = %+v", got)
	}
	if got.Ref.Quote == "" || got.Ref.Paper != "jerolmack2008" {
		t.Errorf("terugverwijzing ontbreekt: %+v", got.Ref)
	}
	if knowledge.callCount() != 2 {
		t.Errorf("kb-aanroepen = %d, want 2 (standpunten en citaten)", knowledge.callCount())
	}
	if sys := gllm.lastCall(t).Messages[0].Content; !strings.Contains(sys, citationInstruction["en"]) {
		t.Errorf("systemprompt mist de citatie-instructie:\n%s", sys)
	}
}

// De kaart in de vergadering hangt aan de volgorde van de beurt, niet
// aan één slotbericht. De bronnen moeten er zijn vóór de eerste tekstdelta,
// anders blijft een "[1]" ruwe tekst tot de duif is uitgesproken; text_done
// moet vóór response_done komen, anders blijft de typcursor staan zolang de
// stem klinkt. Deze test bewaakt die vier posities en de gelijkheid van de
// velden die de kaart eruit leest.
func TestMeetingRespondNow_EventOrder_SourcesThenTextThenTextDoneThenResponseDone(t *testing.T) {
	gllm := newGatedLLM("Volgens het onderzoek [1] ", "ligt dat anders.")
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{
		id: "1", samenvatting: "Duiven in de stad zijn een menselijk probleem.",
		score: 0.9, quote: "Wij hebben ze zelf hierheen gehaald.", paper: "jerolmack2008",
		perspective: "De kritische wetenschapper", themaTitel: "Overlast",
	})
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = knowledge
	}, nil)

	id := rig.openSession(t, "biological", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Are city pigeons a problem?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	sources := indexOfEvent(evs, EventSources)
	text := indexOfEvent(evs, EventText)
	textDone := indexOfEvent(evs, EventTextDone)
	done := indexOfEvent(evs, EventResponseDone)
	if sources < 0 || text < 0 || textDone < 0 || done < 0 {
		t.Fatalf("beurt mist een event: %v", eventTypes(evs))
	}
	if !(sources < text && text < textDone && textDone < done) {
		t.Errorf("volgorde = %v, want sources(%d) < text(%d) < text_done(%d) < response_done(%d)",
			eventTypes(evs), sources, text, textDone, done)
	}
	// Wat de kaart bij text_done te zien krijgt, moet bij response_done nog
	// steeds kloppen: anders springt de voetregel na het uitspreken alsnog.
	if got, want := evs[textDone].KBStatus, evs[done].KBStatus; got != want {
		t.Errorf("kbStatus: text_done = %q, response_done = %q", got, want)
	}
	if evs[textDone].KBStatus != KBSources {
		t.Errorf("kbStatus = %q, want sources", evs[textDone].KBStatus)
	}
	if !reflect.DeepEqual(evs[textDone].Sources, evs[done].Sources) {
		t.Errorf("bronnen lopen uiteen:\ntext_done     = %+v\nresponse_done = %+v",
			evs[textDone].Sources, evs[done].Sources)
	}
	// De bronnen die vooruit gestuurd zijn, zijn dezelfde als die aan het slot.
	if !reflect.DeepEqual(evs[sources].Sources, evs[done].Sources) {
		t.Errorf("het sources-event wijkt af van response_done:\nsources       = %+v\nresponse_done = %+v",
			evs[sources].Sources, evs[done].Sources)
	}
	if len(evs[sources].Sources) != 1 {
		t.Errorf("bronnen = %d, want 1", len(evs[sources].Sources))
	}
}

// Hetzelfde contract op het lege-venster-pad: daar komt de gescripte reactie
// zonder kennisbank, dus zonder sources-event. text_done moet er wél zijn, met
// dezelfde velden als response_done — de kaart kent maar één afronding.
func TestMeetingRespondNow_EmptyWindow_TextDoneMatchesResponseDone(t *testing.T) {
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	text := indexOfEvent(evs, EventText)
	textDone := indexOfEvent(evs, EventTextDone)
	done := indexOfEvent(evs, EventResponseDone)
	if text < 0 || textDone < 0 || done < 0 {
		t.Fatalf("beurt mist een event: %v", eventTypes(evs))
	}
	if !(text < textDone && textDone < done) {
		t.Errorf("volgorde = %v, want text(%d) < text_done(%d) < response_done(%d)",
			eventTypes(evs), text, textDone, done)
	}
	if hasEvent(evs, EventSources) {
		t.Errorf("een leeg venster raadpleegt geen kennisbank: %v", eventTypes(evs))
	}
	if got, want := evs[textDone].KBStatus, evs[done].KBStatus; got != want || got != KBPersonaOnly {
		t.Errorf("kbStatus: text_done = %q, response_done = %q, want beide persona_only", got, want)
	}
	// Geen bronnen op allebei: het veld valt uit de JSON weg (omitempty), dus
	// de clientguard `ev.sources !== undefined` slaat hier over en laat de
	// (lege) bronlijst van de kaart met rust.
	if len(evs[textDone].Sources) != 0 || len(evs[done].Sources) != 0 {
		t.Errorf("bronnen op een gescripte reactie: text_done = %+v, response_done = %+v",
			evs[textDone].Sources, evs[done].Sources)
	}
	if body, err := json.Marshal(evs[textDone]); err != nil {
		t.Fatalf("text_done serialiseren: %v", err)
	} else if bytes.Contains(body, []byte(`"sources"`)) {
		t.Errorf("text_done draagt een sources-veld: %s", body)
	}
}

func TestMeetingRespondNow_KnowledgePigeonWithoutSources_IsGap(t *testing.T) {
	gllm := newGatedLLM("Daar weet ik niets over.")
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = &fakeKnowledge{}
	}, nil)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Wat vindt de wetenschap van duivenpieken?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	for _, ev := range evs {
		if ev.Type == EventResponseDone && ev.KBStatus != KBGap {
			t.Errorf("kbStatus = %q, want gap", ev.KBStatus)
		}
	}
	if sys := gllm.lastCall(t).Messages[0].Content; !strings.Contains(sys, gapDirective["nl"]) {
		t.Errorf("systemprompt mist het gap-directief:\n%s", sys)
	}
}

// Valt de kennisbank weg, dan spreekt de duif niet: een foutmelding op het
// kanaal en het spreekslot terug — nooit een beurt uit het niets.
func TestMeetingRespondNow_KBUnavailable_EmitsErrorNotGap(t *testing.T) {
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = &fakeKnowledge{err: errors.New("test: kb plat")}
	}, nil)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Wat weet het onderzoek hierover?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	var gezien bool
	for _, ev := range evs {
		if ev.Type == EventError {
			gezien = true
			if ev.Error != ReasonKBUnavailable {
				t.Errorf("reason = %q, want %q", ev.Error, ReasonKBUnavailable)
			}
			if ev.Message["en"] == "" || ev.Message["nl"] == "" {
				t.Errorf("melding moet tweetalig zijn: %+v", ev.Message)
			}
		}
		if ev.Type == EventText {
			t.Errorf("er mag geen tekst gestreamd zijn: %+v", ev)
		}
	}
	if !gezien {
		t.Fatalf("foutframe ontbreekt: %v", eventTypes(evs))
	}
	if !hasEvent(evs, EventResponseDone) {
		t.Error("na een fout hoort het spreekslot terug te komen (response_done)")
	}
	// Reason-codes op Info/Warn/Error; de ruwe providerfout hooguit op Debug.
	if boven := logsAboveDebug(rig.logs()); strings.Contains(boven, "kb plat") {
		t.Errorf("de providerfout hoort niet in een gewone logregel:\n%s", boven)
	}
}

// logsAboveDebug geeft de logregels die géén Debug zijn.
func logsAboveDebug(logs string) string {
	var kept []string
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "level=DEBUG") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// Het vergadervenster wordt vóór het embedden afgekapt: een embedding van een
// half uur vergadering middelt elk onderwerp weg.
func TestTruncateQuery_KeepsTailOnSentenceBoundary(t *testing.T) {
	kort := "Dit past gewoon."
	if got := truncateQuery(kort); got != kort {
		t.Errorf("korte tekst hoort ongewijzigd te blijven: %q", got)
	}

	lang := strings.Repeat("Oude zin over iets anders. ", 200) + "De laatste vraag gaat over duiven."
	got := truncateQuery(lang)
	if n := len([]rune(got)); n > maxQueryRunes {
		t.Errorf("venster = %d tekens, want hooguit %d", n, maxQueryRunes)
	}
	if !strings.HasSuffix(got, "De laatste vraag gaat over duiven.") {
		t.Errorf("de staart van het gesprek hoort te blijven: %q", got)
	}
	if !strings.HasPrefix(got, "Oude zin") {
		t.Errorf("de knip hoort op een zinsgrens te liggen, kreeg %q", got[:20])
	}
}

// Zonder zinsgrens in de staart valt de knip terug op tekenniveau.
func TestTruncateQuery_WithoutSentenceBoundary(t *testing.T) {
	lang := strings.Repeat("woord ", 600)
	got := truncateQuery(lang)
	if n := len([]rune(got)); n == 0 || n > maxQueryRunes {
		t.Errorf("venster = %d tekens, want 1..%d", n, maxQueryRunes)
	}
}

// De non-verbale duif praat niet: haar interventie is de geluidenset, niet
// deze knop. Nu een nette 400 in plaats van een stille LLM-beurt.
func TestMeetingRespondNow_NonverbalPigeon_IsRefused(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "nonverbal", "en")
	status, body := rig.respond(t, id)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s (want 400)", status, body)
	}
	if !strings.Contains(body, "invalid_request") {
		t.Errorf("body = %s", body)
	}
}

// Wat de duif zichzelf hoort zeggen, telt niet mee als vergaderinhoud. Het gaat
// daar ook werkelijk om: niet alles wat tijdens haar
// beurt binnenkomt, maar wat op háár woorden lijkt. De zin van een mens die
// haar onderbreekt blijft staan en komt in het volgende venster terug.
func TestMeeting_SegmentsDuringPigeonSpeech_AreMarkedAndExcluded(t *testing.T) {
	// Twee chunks: na de eerste staat de duif nog midden in haar zin, en dat is
	// precies het moment waarop de echo en de onderbreking binnenkomen.
	const pigeonLead = "De duiven op het Remigiusplein "
	// Wat de microfoon van die zin terugkrijgt: kleine letters, geen leesteken.
	// De zeef vergelijkt woorden, geen tekens.
	const pigeonEcho = "de duiven op het remigiusplein"
	gllm := newGatedLLM(pigeonLead, "horen al jaren bij het dorpsbeeld")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "mensen praten over duiven")
	nextEvent(t, es.ch, EventTranscript)

	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)
	s, _ := rig.engine.session(id, testUser.ID)
	s.mu.Lock()
	gen := s.turnGen
	s.mu.Unlock()
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	gllm.let(t)
	// Pas als de tekst geleverd is, weet de sessie waar een echo op zou lijken.
	nextEvent(t, es.ch, EventText)

	// Via de microfoon: de luidspreker staat in dezelfde kamer, en dit is
	// woordelijk wat zij zojuist zei — de duif hoort zichzelf terug.
	s.appendSegment(pigeonEcho, "")
	seg := nextEvent(t, es.ch, EventTranscript)
	if !seg.PigeonWindow {
		t.Errorf("de eigen zin van de duif moet als echo gemarkeerd zijn: %+v", seg)
	}
	// Dezelfde route, dezelfde spreekbeurt, andere woorden: een mens die haar
	// onderbreekt. Vroeger verdween deze zin definitief uit het venster.
	s.appendSegment("kun je daar iets over zeggen voor de buurt", "")
	human := nextEvent(t, es.ch, EventTranscript)
	if human.PigeonWindow {
		t.Errorf("een menselijke zin tijdens de spreekbeurt werd als echo weggezet: %+v", human)
	}
	// Via de getypte route bestaat die echo niet: wie tijdens haar bijdrage
	// iets intikt is een mens, en die regel hoort te blijven staan.
	rig.say(t, id, "mag ik jou daar iets over vragen")
	typed := nextEvent(t, es.ch, EventTranscript)
	if typed.PigeonWindow {
		t.Errorf("een getypte bijdrage werd als echo weggezet: %+v", typed)
	}
	gllm.let(t) // de duif maakt haar zin af
	collectSpokenText(t, es.ch)

	rig.respond(t, id)
	nextEvent(t, es.ch, EventPersona)
	gllm.let(t)
	gllm.let(t)
	collectSpokenText(t, es.ch)

	window := gllm.lastCall(t).Messages[1].Content
	if !strings.Contains(window, "mensen praten over duiven") {
		t.Errorf("venster mist de menselijke inbreng:\n%s", window)
	}
	if strings.Contains(window, pigeonEcho) {
		t.Errorf("de duif mag zichzelf niet terugvoeren:\n%s", window)
	}
	if !strings.Contains(window, "kun je daar iets over zeggen voor de buurt") {
		t.Errorf("de vraag die tijdens haar bijdrage gesproken werd is verdwenen:\n%s", window)
	}
	if !strings.Contains(window, "mag ik jou daar iets over vragen") {
		t.Errorf("de vraag die tijdens haar bijdrage getypt werd is verdwenen:\n%s", window)
	}
}

// Zonder TTS-provider degradeert de beurt
// netjes naar tekst — de vergadering ziet mee, hoort niets.
func TestMeetingRespondNow_WithoutVoice_IsTextOnly(t *testing.T) {
	gllm := newGatedLLM("alleen tekst")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Say something.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	text, evs := collectSpokenText(t, es.ch)
	if text != "alleen tekst" {
		t.Errorf("tekst = %q", text)
	}
	if hasEvent(evs, EventTTSAudio) || hasEvent(evs, EventTTSDone) {
		t.Errorf("zonder stem horen er geen TTS-events te zijn: %v", eventTypes(evs))
	}
}

// Een kapotte TTS-provider mag de beurt niet slopen: tekst blijft komen.
func TestMeetingRespondNow_VoiceFailure_DegradesToText(t *testing.T) {
	gllm := newGatedLLM("tekst blijft")
	voice := newFakeVoice("nooit")
	voice.startErr = errStreamBroken
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Say something.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	text, evs := collectSpokenText(t, es.ch)
	if text != "tekst blijft" {
		t.Errorf("tekst = %q", text)
	}
	if hasEvent(evs, EventTTSAudio) {
		t.Errorf("een gefaalde TTS-start mag geen audio opleveren: %v", eventTypes(evs))
	}
}

// Een beurt die niets oplevert (lege stream) eindigt netjes zonder TTS-poging.
func TestMeetingRespondNow_EmptyLLMOutput_EndsCleanly(t *testing.T) {
	gllm := newGatedLLM()
	voice := newFakeVoice("nooit")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Say something.")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	text, evs := collectSpokenText(t, es.ch)
	if text != "" || hasEvent(evs, EventTTSAudio) {
		t.Errorf("lege beurt = %q / %v", text, eventTypes(evs))
	}
	if len(voice.speakCalls()) != 0 {
		t.Errorf("zonder tekst hoort de TTS niet aangeroepen te worden: %+v", voice.speakCalls())
	}
}

// De client moet een gat in de audiostroom kunnen zien: elk frame draagt zijn
// volgnummer en de afsluiting meldt hoeveel het er waren.
func TestMeetingRespondNow_AudioFramesAreNumbered(t *testing.T) {
	gllm := newGatedLLM("Roekoe. Ik vind daar wat van.")
	voice := newFakeVoice("mp3-1", "mp3-2", "mp3-3")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	var seqs []int
	var done MeetingEvent
	for _, ev := range evs {
		switch ev.Type {
		case EventTTSAudio:
			seqs = append(seqs, ev.Seq)
		case EventTTSDone:
			done = ev
		}
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Errorf("volgnummers = %v, want 1,2,3", seqs)
	}
	if done.Frames != 3 {
		t.Errorf("tts_done meldde %d frames, want 3", done.Frames)
	}
}

// Ook een zelfstandige interventie gaat door de stripper: de duif spreekt geen
// blokhaakjes of sterretjes uit. Hier draait de échte adapter mee, met de
// dev-stem als stem-id (de persona draagt nog de placeholder).
func TestMeetingIntervention_SpokenTextIsStrippedForSpeech(t *testing.T) {
	svc := newCandidateLLM(speak(0.98, "pest-framing", "**Roekoe** [1]. Dat woord doet iets met ons allemaal hier."))
	sess := newScriptedSession()
	rig := eagerRig(t, svc, func(c *detectConfig) { c.Quiet = 0 })
	rig.engine.devVoice = "dev-stem"
	rig.engine.voice = newSynthesizerVoice(sess.synthesizer(), testMP3Format, rig.engine.logger)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "lang": "nl", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)

	rig.say(t, id, "het zijn toch gewoon ratten met vleugels")
	nextEvent(t, es.ch, EventIntervention)
	sess.finishAudio()
	collectSpokenText(t, es.ch)

	waitFor(t, func() bool { return strings.Contains(strings.Join(sess.writtenText(), ""), "Roekoe") },
		"de interventie hoort de provider te bereiken")
	spoken := strings.Join(sess.writtenText(), "")
	for _, verboden := range []string{"[1]", "**"} {
		if strings.Contains(spoken, verboden) {
			t.Errorf("de duif spreekt %q uit: %q", verboden, spoken)
		}
	}
}

// Een duif zonder bruikbare stem-id blijft tekst-only, en dat is een
// logregel met een reden — nooit met de uiting erin.
func TestMeetingRespondNow_WithoutVoiceID_LogsReasonOnly(t *testing.T) {
	gllm := newGatedLLM("Roekoe. ", sentinel)
	mock := &audiotesting.MockSynthesizer{}
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		// De config draagt demo-stemmen; deze test wil juist een duif zonder.
		for _, p := range m.personas.Personas {
			if p.ID == "individualist" {
				p.VoiceID = voicePlaceholder
			}
		}
	}, func(e *meetingEngine) {
		e.voice = newSynthesizerVoice(mock, testMP3Format, e.logger)
		e.devVoice = "" // placeholder blijft placeholder: deze duif heeft geen stem
	})

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	_, evs := collectSpokenText(t, es.ch)

	if hasEvent(evs, EventTTSAudio) {
		t.Errorf("zonder stem hoort er geen audio te zijn: %v", eventTypes(evs))
	}
	if mock.CallCount() != 0 {
		t.Errorf("de provider werd %d keer aangeroepen zonder stem-id", mock.CallCount())
	}
	logs := rig.logs()
	if !strings.Contains(logs, "no_voice") {
		t.Errorf("de reden hoort in het log te staan:\n%s", logs)
	}
	if strings.Contains(logs, sentinel) {
		t.Errorf("de uiting staat in de logs:\n%s", logs)
	}
}

// Een antwoord dat na het strippen leeg is (alleen bronverwijzing en opmaak)
// levert geen TTS-sessie op: de provider wordt niet aangeroepen en de reden
// staat als code in het log.
func TestMeetingRespondNow_NothingToSay_LogsReasonAndSkipsTheProvider(t *testing.T) {
	gllm := newGatedLLM("[1] **__**")
	mock := &audiotesting.MockSynthesizer{}
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.voice = newSynthesizerVoice(mock, testMP3Format, e.logger) // de échte adapter, met de dev-stem
	})

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	_, evs := collectSpokenText(t, es.ch)

	if hasEvent(evs, EventTTSAudio) {
		t.Errorf("er viel niets uit te spreken: %v", eventTypes(evs))
	}
	if mock.CallCount() != 0 {
		t.Errorf("de provider werd %d keer aangeroepen voor een lege uiting", mock.CallCount())
	}
	if logs := rig.logs(); !strings.Contains(logs, "nothing_to_say") {
		t.Errorf("de reden hoort in het log te staan:\n%s", logs)
	}
}

// Streaming-only door de échte adapter: waar de TTS ook stukloopt, geen letter
// van de uiting belandt in een logregel.
func TestMeetingTurn_RealVoiceAdapter_LeaksNoUtteranceIntoLogs(t *testing.T) {
	tests := []struct {
		name  string
		build func(e *meetingEngine) func()
		want  string
	}{
		{"synthesize fails", func(e *meetingEngine) func() {
			e.voice = newSynthesizerVoice(&audiotesting.MockSynthesizer{Err: errStreamBroken}, testMP3Format, e.logger)
			return nil
		}, "tts_start_failed"},
		{"provider ends while writing", func(e *meetingEngine) func() {
			sess := newStuckScriptedSession()
			e.voice = newSynthesizerVoice(sess.synthesizer(), testMP3Format, e.logger)
			return func() { _ = sess.close() }
		}, ""},
		{"provider ends while reading", func(e *meetingEngine) func() {
			sess := newScriptedSession()
			e.voice = newSynthesizerVoice(sess.synthesizer(), testMP3Format, e.logger)
			return func() {
				sess.audioCh <- []byte("mp3-fragment")
				_ = sess.close()
			}
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gllm := newGatedLLM("De duif zegt: ", sentinel)
			var after func()
			rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
				e.devVoice = "dev-stem"
				after = tc.build(e)
			})
			id := rig.openSession(t, "individualist", "nl")
			es := rig.events(t, id)
			rig.say(t, id, sentinel)
			nextEvent(t, es.ch, EventTranscript)
			rig.respond(t, id)
			if after != nil {
				after()
			}
			_, evs := collectSpokenText(t, es.ch)
			// Faalde de stem al bij het openen, dan klonk er nooit iets: tts_done
			// draagt dan tts_unavailable, niet tts_incomplete.
			if tc.want == "tts_start_failed" {
				var done *MeetingEvent
				for i := range evs {
					if evs[i].Type == EventTTSDone {
						done = &evs[i]
					}
				}
				if done == nil || done.Error != ttsUnavailable || done.Message["nl"] == "" || done.Message["en"] == "" {
					t.Errorf("tts_done na een mislukte spraakstart = %+v, want %q met tweetalige melding", done, ttsUnavailable)
				}
			}

			logs := rig.logs()
			if strings.Contains(logs, sentinel) {
				t.Errorf("de uiting staat in de logs:\n%s", logs)
			}
			if tc.want != "" && !strings.Contains(logs, tc.want) {
				t.Errorf("het log mist de reden %q:\n%s", tc.want, logs)
			}
		})
	}
}

// De knop "laat de duif antwoorden" zoekt op de lat van de vergadertafel,
// niet op die van de chat. Een transcriptvenster haalt de chatlat (0,73) vaak
// niet: gesproken zinnen staan verspreid rond het onderwerp. Deze bron scoort
// 0,70 — onder de chatlat, boven de vergaderlat (0,65). Vroeger leverde de
// knop hier nul bronnen terwijl de kandidaatroute op hetzelfde gesprek wél
// bronnen vond.
//
// De tafel heeft kort op de chatlat gestaan; de meting bij
// meetingMinSimilarity in retrieval.go zegt waarom zij terugkwam.
// Deze test pint beide latten vast: 0,70 bereikt de tafel wél en de chat niet.
func TestMeetingRespondNow_UsesTheWiderMeetingThreshold(t *testing.T) {
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{
		id: "1", samenvatting: "Duiven in de stad zijn een menselijk probleem.",
		score: 0.70, quote: "Wij hebben ze zelf hierheen gehaald.", paper: "jerolmack2008",
		perspective: "De kritische wetenschapper", themaTitel: "Overlast",
	})
	build := func(t *testing.T) *meetingRig {
		t.Helper()
		return newMeetingRig(t, func(m *Module) {
			m.llm = newGatedLLM("Volgens het onderzoek [1] ligt dat anders.")
			m.embedder = &llmtesting.MockEmbedder{}
			m.knowledge = knowledge
			// De chatlat, zoals in productie gewired.
			m.minSimilarity = defaultMinSimilarity
		}, nil)
	}

	// Controle op de aanname: op de chatlat valt deze bron er wél buiten.
	// Zonder dit is de test hieronder ook groen met een te lage seed.
	narrow := build(t)
	res, err := narrow.engine.retrieval.run(t.Context(), "Are city pigeons a problem?")
	if err != nil {
		t.Fatalf("smalle retrieval: %v", err)
	}
	if len(res.Sources) != 0 {
		t.Fatalf("op de chatlat %.2f kwamen er %d bronnen door; de seed ligt te hoog",
			defaultMinSimilarity, len(res.Sources))
	}

	rig := build(t)
	id := rig.openSession(t, "biological", "en")
	es := rig.events(t, id)
	rig.say(t, id, "Are city pigeons a problem?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	var done MeetingEvent
	for _, ev := range evs {
		if ev.Type == EventResponseDone {
			done = ev
		}
	}
	if done.KBStatus != KBSources || len(done.Sources) != 1 {
		t.Fatalf("response_done = %+v, want kbStatus sources met één bron op de vergaderlat %.2f",
			done, meetingMinSimilarity)
	}
}

// Een beurt die de beurtklok haalt eindigt zichtbaar. Voorheen kwam er
// geen text_done, geen response_done en geen fout: de kaart bleef op "denkt"
// staan tot iemand zelf op stop drukte. Nu volgt een fout met de reden
// turn_timeout en een eigen tweetalige zin, daarna response_done; wat er al
// stond blijft staan, en de knop werkt meteen weer.
func TestMeetingRespondNow_TurnClockEndsTheTurnVisibly(t *testing.T) {
	gllm := newGatedLLM("Roekoe. Eerste zin staat er al. ", "Deze komt nooit.")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.cfg.TurnTimeout = 300 * time.Millisecond
	})

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: status = %d, body = %s", status, body)
	}
	gllm.let(t) // de eerste zin gaat eruit, de tweede houdt het model vast

	text, evs := collectSpokenText(t, es.ch)
	if strings.TrimSpace(text) != "Roekoe. Eerste zin staat er al." {
		t.Errorf("geleverde tekst = %q", text)
	}
	if hasEvent(evs, EventTextDone) {
		t.Errorf("een afgekapte beurt hoort niet als afgerond te gelden: %v", eventTypes(evs))
	}
	var failure *MeetingEvent
	for i := range evs {
		if evs[i].Type == EventError {
			failure = &evs[i]
		}
	}
	if failure == nil {
		t.Fatalf("geen foutgebeurtenis vóór response_done: %v", eventTypes(evs))
	}
	if failure.Error != ReasonTurnTimeout {
		t.Errorf("reden = %q, want %q", failure.Error, ReasonTurnTimeout)
	}
	for _, lang := range []string{"en", "nl"} {
		if failure.Message[lang] == "" || failure.Message[lang] != meetingTurnTimeoutMessage[lang] {
			t.Errorf("[%s] melding = %q, want de vergaderzin", lang, failure.Message[lang])
		}
	}
	if last := evs[len(evs)-1]; last.StopReason != ReasonTurnTimeout {
		t.Errorf("response_done draagt stopreden %q, want %q", last.StopReason, ReasonTurnTimeout)
	}
	if logs := rig.logs(); !strings.Contains(logs, "reden="+ReasonTurnTimeout) {
		t.Errorf("het log noemt de beurtklok niet: %s", logs)
	}

	// Het spreekslot is vrij: een nieuwe knopdruk wordt aangenomen.
	s, _ := rig.engine.session(id, testUser.ID)
	waitFor(t, func() bool { return !s.isSpeaking() }, "het spreekslot bleef bezet")
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("tweede respond: status = %d, body = %s", status, body)
	}
}

// De stopknop is geen beurtklok: wie zelf stopt, weet waarom het stil is en
// krijgt geen foutmelding over een te trage duif.
func TestMeetingRespondNow_StopButtonIsNoTurnTimeout(t *testing.T) {
	gllm := newGatedLLM("Roekoe. Eerste zin staat er al. ", "Deze komt nooit.")
	gllm.gate()
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "zeg eens wat")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	gllm.let(t)
	nextEvent(t, es.ch, EventText)
	rig.post(t, id, "/stop", nil)
	nextEvent(t, es.ch, EventStopped)
	select {
	case <-gllm.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("de stream werd niet afgebroken")
	}
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-es.ch:
			if ev.Type == EventError {
				t.Fatalf("de stopknop gaf een foutgebeurtenis: %+v", ev)
			}
		case <-deadline:
			return
		}
	}
}
