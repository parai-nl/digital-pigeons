package duif

import (
	"testing"

	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// Wat de gate liet vallen bereikt de vergadertafel. De zaal krijgt
// geen reden-codes te zien — dat doet de client — maar het sources-frame draagt
// ze, zodat de client er één neutrale zin en een devMode-detail van kan maken.
func TestMeetingSources_CarriesWhatFellAway(t *testing.T) {
	gllm := newGatedLLM("Een korte bijdrage.")
	knowledge := &fakeKnowledge{}
	// Eén bruikbaar standpunt en één standpunt zonder leesbare tekst: die
	// laatste is precies het pad dat vroeger volledig stil was. Een lege tekst
	// is één van de vier uitvalredenen.
	seedDestillaat(t, knowledge, kbSeed{
		id: "goed", samenvatting: "Noise and light change how city pigeons use rooftops.",
		score: 0.9, quote: answerSourcesFixture[0].Ref.Passages[0].Text, paper: "synthetic-study",
	})
	seedDestillaat(t, knowledge, kbSeed{
		id: "leeg", samenvatting: "   ", score: 0.89, quote: "Een passage zonder standpunt.", paper: "synthetic-study",
	})
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = knowledge
	}, nil)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Wat doen die knallen met de duiven op het stationsdak?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	_, evs := collectSpokenText(t, es.ch)

	var gezien bool
	for _, ev := range evs {
		if ev.Type != EventSources {
			continue
		}
		gezien = true
		if ev.Dropped == nil || ev.Dropped.Count == 0 {
			t.Fatalf("het sources-frame meldt geen uitval: %+v", ev.Dropped)
		}
		if ev.Dropped.Reasons[dropEmptyText] == 0 {
			t.Errorf("reden ontbreekt: %+v", ev.Dropped.Reasons)
		}
		if len(ev.Sources) == 0 {
			t.Error("de bruikbare bron hoort er gewoon te staan")
		}
	}
	if !gezien {
		t.Fatalf("geen sources-frame: %v", eventTypes(evs))
	}
}

// En een beurt waarin niets wegviel draagt geen melding: anders zou elke beurt
// er een krijgen en zegt hij niets meer.
func TestMeetingSources_NoDropsNoMessage(t *testing.T) {
	gllm := newGatedLLM("Een korte bijdrage.")
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = gllm
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = seededKnowledge(t)
	}, nil)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Wat doen die knallen met de duiven op het stationsdak?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	_, evs := collectSpokenText(t, es.ch)

	for _, ev := range evs {
		if ev.Type == EventSources && ev.Dropped != nil {
			t.Fatalf("zonder uitval hoort er geen melding te staan: %+v", ev.Dropped)
		}
	}
}
