package duif

import (
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De vergaderkant van de lege kennisbank: een lege kennisbank is een storing,
// geen kennisgat. De chat krijgt zijn 503 uit runRetrieval; de vergadering
// heeft twee eigen routes, en beide horen hier een zichtbare storing te melden
// in plaats van een stille gatbeurt.
//
// De importer staat op zijn beginstand (`empty`): geen marker en geen
// standpunten, precies de installatie waar nooit een bundel in ging.

// emptyImportRig is een vergaderrig met een bereikbare maar nooit geïmporteerde
// kennisbank.
func emptyImportRig(t *testing.T, tune func(m *Module)) *meetingRig {
	t.Helper()
	return newMeetingRig(t, func(m *Module) {
		knowledge := &fakeKnowledge{}
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = knowledge
		m.kbImport = testImporter(knowledge, nil, false)
		if tune != nil {
			tune(m)
		}
	}, func(e *meetingEngine) {
		e.cfg.Detect.MinWords = 3
		e.cfg.Detect.Interval = 0
	})
}

// De knop-route (meeting_turn.go): de duif zwijgt en de zaal ziet waarom.
func TestMeetingRespondNow_EmptyKnowledgeBase_EmitsFailureNotGap(t *testing.T) {
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := emptyImportRig(t, func(m *Module) { m.llm = gllm })

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "Kunnen stadsduiven ziekten overdragen op mensen?")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)

	_, evs := collectSpokenText(t, es.ch)
	var gezien bool
	for _, ev := range evs {
		switch ev.Type {
		case EventError:
			gezien = true
			if ev.Error != ReasonKBUnavailable {
				t.Errorf("reason = %q, want %q", ev.Error, ReasonKBUnavailable)
			}
		case EventSources:
			if ev.KBStatus == KBGap {
				t.Error("een nooit geïmporteerde kennisbank kwam als kennisgat op het kanaal")
			}
		case EventText:
			t.Errorf("er mag geen tekst gestreamd zijn: %+v", ev)
		}
	}
	if !gezien {
		t.Fatalf("foutframe ontbreekt: %v", eventTypes(evs))
	}
	if !hasEvent(evs, EventResponseDone) {
		t.Error("na een fout hoort het spreekslot terug te komen (response_done)")
	}
}

// De zelfstandige route (meeting_detect.go): geen stille PASS, maar een
// gemelde storing op het detectorkanaal — en geen enkele modelcall.
func TestCandidate_EmptyKnowledgeBase_ReportsFailureWithoutAModelCall(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "dit mag nooit klinken"))
	rig := emptyImportRig(t, func(m *Module) { m.llm = svc })

	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Kunnen die duiven op het plein ziekten overdragen?"})
	rig.clock.advance(t, time.Second)

	waitFor(t, func() bool {
		for _, ev := range stream.collected() {
			if ev.Type == EventDetectorStatus && ev.Reason == ReasonKBUnavailable {
				return true
			}
		}
		return false
	}, "de zelfstandige route meldde de lege kennisbank niet")
	if n := len(svc.candidateCalls()); n != 0 {
		t.Fatalf("zonder kennisbank hoort er geen modelcall uit te gaan: %d call(s)", n)
	}
}
