package duif

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio"
)

// answerFinalize speelt de provider die op een finalize-aanvraag de lopende
// uiting afsluit: precies wat Soniox doet, in 31 ms gemeten. De teller zegt hoe
// vaak erom gevraagd is.
func answerFinalize(t *testing.T, stt *fakeSTT, text string) *atomic.Int64 {
	t.Helper()
	var asked atomic.Int64
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case <-stt.commit:
				asked.Add(1)
				select {
				case stt.events <- audio.TranscriptionEvent{Type: sttEventCommitted, Text: text, IsFinal: true}:
				case <-done:
					return
				}
			}
		}
	}()
	return &asked
}

// De knop laat de lopende uiting eerst afronden. Dit is de meting van 18
// september 2026: op +0,0 s na het laatste woord staat de zin nog als partial
// in de lucht en was het venster leeg; nu gaat er een finalize uit, landt het
// segment en antwoordt de duif op díé zin.
func TestMeetingRespondNow_FinalizesTheOpenUtteranceBeforeReadingTheWindow(t *testing.T) {
	tr := &fakeTranscriber{}
	gllm := newGatedLLM("Roekoe. ", "Daar weet ik iets van.")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)
	asked := answerFinalize(t, stt, "Wat vinden jullie van de duiven op het Stationsplein?")

	// De spreker is nog niet klaar: wat hij zegt staat als partial op het
	// scherm en nergens in het venster.
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "wat vinden jullie van de duiven", IsFinal: false})
	if ev := nextEvent(t, es.ch, EventTranscript); ev.Final {
		t.Fatalf("de uiting hoorde nog open te staan: %+v", ev)
	}
	s, _ := rig.engine.session(id, testUser.ID)
	if n := s.segmentCount(); n != 0 {
		t.Fatalf("het venster hoorde leeg te zijn, kreeg %d segmenten", n)
	}

	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: status = %d, body = %s", status, body)
	}
	nextEvent(t, es.ch, EventPersona)

	text, _ := collectSpokenText(t, es.ch)
	if text != "Roekoe. Daar weet ik iets van." {
		t.Fatalf("de duif hoorde op de afgeronde zin te antwoorden, kreeg %q", text)
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("de brug vroeg %d keer om een afronding, want precies 1", n)
	}
	if n := rig.bridge(t, id).finalizeRequests(); n != 1 {
		t.Errorf("finalizeRequests = %d, want 1", n)
	}
	call := gllm.lastCall(t)
	if window := call.Messages[1].Content; !strings.Contains(window, "Stationsplein") {
		t.Errorf("de zin die net afgerond werd hoort in het venster te staan:\n%s", window)
	}
}

// Zonder lopende uiting valt het wachten door: geen finalize, geen vertraging.
// De wachttijd staat hier expres op een halve minuut — wie tóch zou wachten
// haalt de deadline van collectSpokenText niet.
func TestMeetingRespondNow_WithoutAnOpenUtterance_DoesNotWait(t *testing.T) {
	tr := &fakeTranscriber{}
	gllm := newGatedLLM("Roekoe. ", "Dat is al gezegd.")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
		e.cfg.FinalizeWait = 30 * time.Second
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	stt.emit(t, audio.TranscriptionEvent{Type: sttEventCommitted, Text: "De duiven zitten op het Stationsplein.", IsFinal: true})
	if ev := nextEvent(t, es.ch, EventTranscript); !ev.Final {
		t.Fatalf("het segment hoorde definitief te zijn: %+v", ev)
	}

	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	if text, _ := collectSpokenText(t, es.ch); text != "Roekoe. Dat is al gezegd." {
		t.Fatalf("gesproken tekst = %q", text)
	}
	if n := rig.bridge(t, id).finalizeRequests(); n != 0 {
		t.Errorf("zonder open uiting hoort er niets afgerond te worden, kreeg %d aanvragen", n)
	}
	if n := stt.commitCount(); n != 0 {
		t.Errorf("de provider kreeg %d commits binnen; de 10 s-regel noch de knop hoorde er een te sturen", n)
	}
}

// Blijft de uiting ook ná de afronding weg, dan is "ik heb nog niets gehoord"
// onwaar: er wérd gepraat. De duif zegt dat, en het log noemt de oorzaak.
func TestMeetingRespondNow_UnfinishedUtterance_HasItsOwnSentence(t *testing.T) {
	tr := &fakeTranscriber{}
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
		e.cfg.FinalizeWait = 100 * time.Millisecond // de provider antwoordt hier nooit
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "ik was nog aan het", IsFinal: false})
	nextEvent(t, es.ch, EventTranscript)

	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	text, _ := collectSpokenText(t, es.ch)
	if text != meetingUnfinishedSpeechReply["nl"] {
		t.Errorf("tekst = %q, want de zin over de onafgemaakte uiting", text)
	}
	if gllm.callCount() != 0 {
		t.Errorf("een leeg venster mag geen LLM-call kosten (got %d)", gllm.callCount())
	}
	if n := rig.bridge(t, id).finalizeRequests(); n != 1 {
		t.Errorf("er hoorde wél om een afronding gevraagd te zijn, kreeg %d", n)
	}
	if logs := rig.logs(); !strings.Contains(logs, "oorzaak="+emptyWindowUnfinished) {
		t.Errorf("het log noemt de oorzaak niet:\n%s", logs)
	}
}

// Kwam er alleen geluid binnen terwijl de duif zelf aan het woord was, dan is
// dat haar eigen stem. "Nog niets gehoord" verbergt dat; de derde zin
// zegt het.
func TestMeetingRespondNow_EchoOnlyWindow_SaysItHeardItself(t *testing.T) {
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	s, _ := rig.engine.session(id, testUser.ID)

	// Een vorige beurt die hoorbaar was, met een microfoon die haar terughoorde.
	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("de beurt ging niet open")
	}
	defer cancel()
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	s.appendSegment("Roekoe, ik vind daar wat van.", "")
	s.endTurn(gen)
	if n := s.echoedInWindow(); n != 1 {
		t.Fatalf("er hoorde één echo in het venster te staan, kreeg %d", n)
	}
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	text, _ := collectSpokenText(t, es.ch)
	if text != meetingEchoOnlyReply["nl"] {
		t.Errorf("tekst = %q, want de zin over de eigen stem", text)
	}
	if logs := rig.logs(); !strings.Contains(logs, "oorzaak="+emptyWindowEcho) {
		t.Errorf("het log noemt de oorzaak niet:\n%s", logs)
	}
}

// Een final sluit de lopende uiting af, ook als hij als echo binnenkomt: de
// provider heeft hem dan wél afgerond. Stond uncommittedSpeech daarna nog open,
// dan vroeg elke volgende knopdruk opnieuw om een afronding, zat de volle
// wachttijd uit en meldde "de zin was nog niet bij me" terwijl de oorzaak de
// eigen stem van de duif was — precies de tweede druk vlak na een beurt.
//
// De wachttijd staat hier op een halve minuut: wie tóch zou wachten haalt de
// deadline van collectSpokenText niet.
func TestMeetingRespondNow_EchoFinalClosesTheOpenUtterance(t *testing.T) {
	tr := &fakeTranscriber{}
	gllm := newGatedLLM("dit mag nooit klinken")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
		e.cfg.FinalizeWait = 30 * time.Second
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)
	s, _ := rig.engine.session(id, testUser.ID)

	// Er wordt gepraat: de uiting staat open.
	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "ik wilde nog iets vragen", IsFinal: false})
	nextEvent(t, es.ch, EventTranscript)
	if _, pending := s.speechPending(); !pending {
		t.Fatal("de partial hoorde de uiting open te zetten")
	}

	// De duif is aan het woord en hoorbaar; de final landt in haar venster.
	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("de beurt ging niet open")
	}
	defer cancel()
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	stt.emit(t, audio.TranscriptionEvent{Type: sttEventCommitted, Text: "ik wilde nog iets vragen", IsFinal: true})
	if ev := nextEvent(t, es.ch, EventTranscript); !ev.Final || !ev.PigeonWindow {
		t.Fatalf("dit segment hoorde als duif-venster binnen te komen: %+v", ev)
	}
	s.endTurn(gen)

	if _, pending := s.speechPending(); pending {
		t.Fatal("een final sluit de uiting af, ook als hij als echo gemarkeerd wordt")
	}

	// De knop daarna wacht dus nergens op en noemt de echte oorzaak.
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	if text, _ := collectSpokenText(t, es.ch); text != meetingEchoOnlyReply["nl"] {
		t.Errorf("tekst = %q, want de zin over de eigen stem", text)
	}
	if n := rig.bridge(t, id).finalizeRequests(); n != 0 {
		t.Errorf("er viel niets meer af te ronden, maar de brug kreeg %d aanvragen", n)
	}
	if logs := rig.logs(); !strings.Contains(logs, "oorzaak="+emptyWindowEcho) {
		t.Errorf("het log noemt de oorzaak niet:\n%s", logs)
	}
}

// En de stilte zelf houdt haar oorspronkelijke zin: niets gehoord is niets
// gehoord.
func TestMeetingRespondNow_TrueSilence_KeepsTheOriginalSentence(t *testing.T) {
	for _, lang := range []string{"nl", "en"} {
		t.Run(lang, func(t *testing.T) {
			gllm := newGatedLLM("dit mag nooit klinken")
			rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)
			id := rig.openSession(t, "individualist", lang)
			es := rig.events(t, id)
			if status, _ := rig.respond(t, id); status != http.StatusAccepted {
				t.Fatal("respond")
			}
			text, _ := collectSpokenText(t, es.ch)
			if text != meetingNoInputReply[lang] {
				t.Errorf("tekst = %q", text)
			}
			if logs := rig.logs(); !strings.Contains(logs, "oorzaak="+emptyWindowSilent) {
				t.Errorf("het log noemt de oorzaak niet:\n%s", logs)
			}
		})
	}
}

// De drie zinnen bestaan in beide talen en verschillen van elkaar — anders is
// de oorzaak aan tafel alsnog onzichtbaar (EN is brontaal, NL volgt).
func TestMeetingEmptyWindowReply_ThreeCausesThreeSentences(t *testing.T) {
	for _, lang := range []string{"nl", "en"} {
		seen := map[string]string{}
		for _, reason := range []string{emptyWindowSilent, emptyWindowUnfinished, emptyWindowEcho} {
			reply := meetingEmptyWindowReply(reason, lang)
			if strings.TrimSpace(reply) == "" {
				t.Fatalf("%s/%s heeft geen zin", lang, reason)
			}
			if other, dup := seen[reply]; dup {
				t.Errorf("%s/%s deelt zijn zin met %s", lang, reason, other)
			}
			seen[reply] = reason
		}
		// Een onbekende oorzaak valt terug op de veiligste bewering.
		if got := meetingEmptyWindowReply("iets nieuws", lang); got != meetingNoInputReply[lang] {
			t.Errorf("onbekende oorzaak (%s) = %q, want de stiltezin", lang, got)
		}
	}
}

// --- de haakjes zelf ----------------------------------------------------------

// finalize is niet-blokkerend en eerlijk over wat hij kan: zonder open sessie
// of zonder commit-kanaal valt er niets af te ronden.
func TestSTTBridge_Finalize_AsksOnceAndNeverBlocks(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.close(CloseReasonUser) })

	b := &sttBridge{engine: e, session: s, frames: make(chan []byte, 1)}
	if b.finalize() {
		t.Error("zonder open provider-sessie valt er niets af te ronden")
	}
	b.setSession(&audio.RealtimeSTTSession{Audio: make(chan []byte, 1), Events: make(chan audio.TranscriptionEvent)})
	if b.finalize() {
		t.Error("een provider zonder commit-kanaal kan niet afronden")
	}

	commit := make(chan struct{}, 1)
	b.setSession(&audio.RealtimeSTTSession{Audio: make(chan []byte, 1), Commit: commit, Events: make(chan audio.TranscriptionEvent)})
	if !b.finalize() || len(commit) != 1 {
		t.Fatalf("de aanvraag hoorde het commit-kanaal op te gaan (%d in de wacht)", len(commit))
	}
	// Er staat er al een in de wacht: dat is genoeg, en het blokkeert niet.
	if !b.finalize() {
		t.Error("een aanvraag die al klaarstaat is nog steeds een afronding")
	}
	if n := b.finalizeRequests(); n != 1 {
		t.Errorf("finalizeRequests = %d, want 1 — de tweede aanvraag was dezelfde", n)
	}
}

// waitForSegment wacht op wat de duif kan gebruiken: een menselijk segment.
func TestMeetingSession_WaitForSegment(t *testing.T) {
	t.Run("een vers segment maakt een eind aan het wachten", func(t *testing.T) {
		_, s, _ := trackedSession(t)
		after, _ := s.speechPending()
		go func() {
			time.Sleep(20 * time.Millisecond)
			s.appendSegment("Daar wilde ik iets over vragen.", "")
		}()
		if !s.waitForSegment(context.Background(), after, 2*time.Second) {
			t.Fatal("het segment landde wel degelijk")
		}
	})

	t.Run("blijft het weg, dan is de tijd op", func(t *testing.T) {
		_, s, _ := trackedSession(t)
		after, _ := s.speechPending()
		started := time.Now()
		if s.waitForSegment(context.Background(), after, 60*time.Millisecond) {
			t.Fatal("er landde niets")
		}
		if waited := time.Since(started); waited < 50*time.Millisecond {
			t.Errorf("de wacht duurde %s; hij hoort de hele tijd uit te zitten", waited)
		}
	})

	t.Run("op de eigen stem wordt niet gewacht", func(t *testing.T) {
		// Een echo is pas een echo als er geluid uitgaat én de
		// tekst op de hare lijkt: dus eerst haar tekst, dan het frame, en pas
		// dan haar eigen zin. De afspeelbevestiging hoort bij het echte verloop
		// maar is voor het oordeel niet nodig (zie echoPossibleLocked).
		const spoken = "Roekoe, de duiven op het plein horen al jaren bij het dorp."
		_, s, _ := trackedSession(t)
		gen, _, cancel, ok := s.beginTurn()
		if !ok {
			t.Fatal("de beurt ging niet open")
		}
		defer cancel()
		s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: spoken})
		s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
		if !s.ackPlayback(strconv.FormatUint(gen, 10), "started") {
			t.Fatal("playback started geweigerd")
		}
		after, _ := s.speechPending()
		go func() {
			time.Sleep(20 * time.Millisecond)
			s.appendSegment("de duiven op het plein horen al jaren bij het dorp", "")
		}()
		if s.waitForSegment(context.Background(), after, 150*time.Millisecond) {
			t.Fatal("een duif-venstersegment is geen vergaderinhoud om op te wachten")
		}
	})

	t.Run("de stopknop breekt het wachten af", func(t *testing.T) {
		_, s, _ := trackedSession(t)
		after, _ := s.speechPending()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		started := time.Now()
		if s.waitForSegment(ctx, after, 10*time.Second) {
			t.Fatal("er landde niets")
		}
		if waited := time.Since(started); waited > 2*time.Second {
			t.Errorf("de wacht duurde %s; de beurt was al afgebroken", waited)
		}
	})

	t.Run("een sessie die sluit laat niemand hangen", func(t *testing.T) {
		_, s, _ := trackedSession(t)
		after, _ := s.speechPending()
		go func() {
			time.Sleep(20 * time.Millisecond)
			s.close(CloseReasonUser)
		}()
		started := time.Now()
		if s.waitForSegment(context.Background(), after, 10*time.Second) {
			t.Fatal("een gesloten sessie levert geen segment meer")
		}
		if waited := time.Since(started); waited > 2*time.Second {
			t.Errorf("de wacht duurde %s; de sessie was al dicht", waited)
		}
	})

	t.Run("zonder wachttijd wordt er niet gewacht", func(t *testing.T) {
		_, s, _ := trackedSession(t)
		if s.waitForSegment(context.Background(), 0, 0) {
			t.Fatal("FinalizeWait 0 zet het wachten uit")
		}
	})
}

// settleSpeech laat alles wat niet kan afronden meteen door: dat houdt de
// getypte route (POST .../transcript) en elke test zonder STT op volle snelheid.
func TestSettleSpeech_WithoutABridge_FallsThrough(t *testing.T) {
	e, s, _ := trackedSession(t)
	s.mu.Lock()
	s.uncommittedSpeech = true
	s.mu.Unlock()

	started := time.Now()
	settle := e.settleSpeech(context.Background(), s)
	if waited := time.Since(started); waited > time.Second {
		t.Fatalf("zonder brug hoort er niet gewacht te worden, kreeg %s", waited)
	}
	if !settle.pending || settle.requested || settle.landed {
		t.Fatalf("settle = %+v: de uiting stond open, maar er was niemand om hem af te ronden", settle)
	}
	if got := s.emptyWindowReason(settle); got != emptyWindowUnfinished {
		t.Errorf("oorzaak = %q, want %q", got, emptyWindowUnfinished)
	}
}
