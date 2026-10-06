package duif

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// Het schuivende venster is dubbel begrensd: op aantal
// segmenten én op leeftijd. Wat eruit valt is definitief weg (er bestaat geen
// vollediger kopie).
func TestRollingTranscript_PrunesByCountAndAge(t *testing.T) {
	rt := NewRollingTranscript(3, 2*time.Minute)
	for i := range 5 {
		rt.Append(TranscriptSegment{Text: fmt.Sprintf("segment %d", i), At: meetingEpoch.Add(time.Duration(i) * time.Second)})
	}
	if rt.Len() != 3 {
		t.Fatalf("aantal segmenten = %d, want 3 (max-aantal)", rt.Len())
	}
	segs := rt.Snapshot(meetingEpoch.Add(5*time.Second), time.Hour, 10)
	if len(segs) != 3 || segs[0].Text != "segment 2" || segs[2].Text != "segment 4" {
		t.Fatalf("venster = %+v, want de laatste drie segmenten", segs)
	}

	rt.Append(TranscriptSegment{Text: "veel later", At: meetingEpoch.Add(10 * time.Minute)})
	if rt.Len() != 1 {
		t.Fatalf("aantal segmenten = %d, want 1: alles ouder dan de maxleeftijd vervalt", rt.Len())
	}
}

// Segmenten uit het duif-spreekvenster zijn voer voor de
// UI, maar nooit voor een volgend antwoord: anders detecteert de duif haar
// eigen thema's.
func TestRollingTranscript_SnapshotExcludesPigeonWindowAndLimits(t *testing.T) {
	rt := NewRollingTranscript(40, 10*time.Minute)
	rt.Append(TranscriptSegment{Text: "oud", At: meetingEpoch})
	rt.Append(TranscriptSegment{Text: "mens praat", At: meetingEpoch.Add(4 * time.Minute)})
	rt.Append(TranscriptSegment{Text: "duif hoort zichzelf", At: meetingEpoch.Add(4*time.Minute + 30*time.Second), PigeonWindow: true})
	rt.Append(TranscriptSegment{Text: "mens praat weer", At: meetingEpoch.Add(4*time.Minute + 50*time.Second)})

	now := meetingEpoch.Add(5 * time.Minute)
	segs := rt.Snapshot(now, 90*time.Second, 10)
	if len(segs) != 2 {
		t.Fatalf("venster = %+v, want 2 segmenten (90 s, zonder duif-venster)", segs)
	}
	for _, s := range segs {
		if s.PigeonWindow {
			t.Errorf("snapshot bevat een duif-venstersegment: %+v", s)
		}
		if s.Text == "oud" {
			t.Errorf("segment buiten het tijdvenster hoort er niet in: %+v", s)
		}
	}

	if got := rt.Snapshot(now, 90*time.Second, 1); len(got) != 1 || got[0].Text != "mens praat weer" {
		t.Errorf("K-begrenzing = %+v, want alleen het laatste segment", got)
	}
	if got := rt.Snapshot(now, time.Nanosecond, 10); got != nil {
		t.Errorf("leeg venster moet nil geven, niet %+v", got)
	}
}

// De luister-indicator is een sessieveld dat het kanaal meestuurt: bij
// het abonneren (statusframe) en op elke hartslag.
func TestMeetingSession_ListenStateTravelsOnTheChannel(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie openen: %v", err)
	}
	ch, unsub := s.subscribe()
	defer unsub()

	state := nextEvent(t, ch, EventState)
	if state.Listening != ListenActive || state.PigeonID != "individualist" || state.Lang != "nl" {
		t.Fatalf("statusframe = %+v, want luisterend + persona + taal", state)
	}

	evs := tickAndCollect(t, clock, ch, 10*time.Second)
	hb := evs[len(evs)-1]
	if hb.Type != EventListening || hb.Listening != ListenActive {
		t.Fatalf("hartslag = %+v, want listening_heartbeat met listening", hb)
	}

	// Valt de STT-brug weg, dan zegt de indicator dat meteen — liever
	// vals-negatief dan vals-positief.
	s.setListenState(ListenReconnecting)
	ev := nextEvent(t, ch, EventListening)
	if ev.Listening != ListenReconnecting {
		t.Fatalf("indicator = %q, want reconnecting", ev.Listening)
	}
	// Dezelfde status nogmaals zetten is geen nieuw event (geen gerammel).
	s.setListenState(ListenReconnecting)
	evs = tickAndCollect(t, clock, ch, 10*time.Second)
	if len(evs) != 1 {
		t.Fatalf("onveranderde status mag geen extra event geven: %v", eventTypes(evs))
	}
}

// eventOf geeft het laatste event van een type uit een tick, of een leeg event.
func eventOf(evs []MeetingEvent, typ string) MeetingEvent {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == typ {
			return evs[i]
		}
	}
	return MeetingEvent{}
}

// Na tien minuten zonder bediening "Ben je er nog?", met twee minuten
// aftelling; zonder antwoord sluit de server de sessie. Beide timers lopen op
// de injecteerbare klok, en de vraag draagt de resterende tijd als duur, niet
// als tijdstip: de browser telt af op zijn eigen klok.
func TestMeeting_Inactivity_PresenceCheckThenClose(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie openen: %v", err)
	}
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	if evs := tickAndCollect(t, clock, ch, 9*time.Minute); hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("na 9 minuten mag er nog geen presence_check zijn: %v", eventTypes(evs))
	}
	evs := tickAndCollect(t, clock, ch, 2*time.Minute)
	check := eventOf(evs, EventPresenceCheck)
	if check.Type == "" {
		t.Fatalf("na 11 minuten zonder bediening hoort er een presence_check te zijn: %v", eventTypes(evs))
	}
	if check.Message["nl"] == "" || check.Message["en"] == "" {
		t.Errorf("presence_check moet tweetalig zijn: %+v", check.Message)
	}
	if check.GraceMS != (2 * time.Minute).Milliseconds() {
		t.Errorf("graceMs = %d, want de volle aftelling van 120000", check.GraceMS)
	}
	if beat := eventOf(evs, EventListening); beat.GraceMS != check.GraceMS {
		t.Errorf("de hartslag van dezelfde tik draagt graceMs %d, want %d", beat.GraceMS, check.GraceMS)
	}

	evs = tickAndCollect(t, clock, ch, 90*time.Second)
	if hasEvent(evs, EventSessionClosed) {
		t.Fatalf("binnen de aftelling van twee minuten mag de sessie nog niet dicht: %v", eventTypes(evs))
	}
	if beat := eventOf(evs, EventListening); beat.GraceMS != (30 * time.Second).Milliseconds() {
		t.Errorf("hartslag na 90 s draagt graceMs %d, want 30000", beat.GraceMS)
	}
	evs = tickAndCollect(t, clock, ch, 31*time.Second)
	closed := evs[len(evs)-1]
	if closed.Type != EventSessionClosed || closed.Reason != CloseReasonInactivity {
		t.Fatalf("laatste event = %+v, want session_closed wegens inactiviteit", closed)
	}
	if _, ok := <-ch; ok {
		t.Error("het kanaal moet dicht zijn na session_closed")
	}
	// Dezelfde volgorde als hierboven: close() sluit eerst de kanalen en meldt
	// de sessie pas daarna af bij de engine.
	waitFor(t, func() bool {
		_, ok := e.session(s.id, "user-1")
		return !ok
	}, "een gesloten sessie hoort uit het sessiebeheer verdwenen te zijn")
	waitFor(t, func() bool { return clock.stopCount() > 0 }, "de sessielus moet zijn ticker stoppen")
}

// Een bevestiging ("ja, ga door") houdt de sessie open, en de hartslag
// zegt daarna tegen elk tabblad dat er geen vraag meer openstaat.
func TestMeeting_PresenceAck_KeepsSessionOpen(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	if evs := tickAndCollect(t, clock, ch, 11*time.Minute); !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("presence_check ontbreekt: %v", eventTypes(evs))
	}
	if !s.noteUserActivity() {
		t.Fatal("een klik moet de openstaande check afmelden")
	}
	evs := tickAndCollect(t, clock, ch, 2*time.Minute)
	if hasEvent(evs, EventSessionClosed) {
		t.Fatalf("na een bevestiging mag de sessie niet sluiten: %v", eventTypes(evs))
	}
	if beat := eventOf(evs, EventListening); beat.GraceMS != 0 {
		t.Errorf("na de bevestiging draagt de hartslag nog graceMs %d", beat.GraceMS)
	}
	if _, ok := e.session(s.id, "user-1"); !ok {
		t.Error("sessie hoort nog open te staan")
	}
	// De klok begon opnieuw bij de klik: de volgende vraag komt pas tien
	// minuten later.
	if evs := tickAndCollect(t, clock, ch, 7*time.Minute); hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("de klok liep niet opnieuw vanaf de klik: %v", eventTypes(evs))
	}
	if evs := tickAndCollect(t, clock, ch, 2*time.Minute); !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("tien minuten na de klik hoort de vraag terug te komen: %v", eventTypes(evs))
	}
}

// In de praktijk bleef een vergadering zonder iemand aan het scherm uren
// open: zaalrumoer, de echo van de duif en haar eigen geluiden telden als
// leven. Niets daarvan zet de klok nog terug, en niets daarvan
// beantwoordt een openstaande vraag. Alleen bediening telt.
func TestMeeting_SpeechAndThePigeonAreNoSignOfLife(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	tickAndCollect(t, clock, ch, 4*time.Minute)
	s.appendSegment("er wordt gepraat in de zaal", "spreker-1")
	nextEvent(t, ch, EventTranscript)
	s.appendTypedSegment("een getypte regel", "spreker-2")
	nextEvent(t, ch, EventTranscript)
	s.noteUtterance() // de signalerende duif koert vanzelf
	s.stopSpeakingReason("playback_timeout")
	nextEvent(t, ch, EventStopped)
	// Een zelfstandige beurt die gewoon afloopt (releaseTurnLocked, ook het
	// pad van een afspeeleinde) is evenmin een teken van leven.
	on := true
	s.applySettings(&on, "", nil)
	s.resumeAutonomy(s.settingsSnapshot().TurnID)
	gen, _, cancel, ok := s.beginTurn(true)
	if !ok {
		t.Fatal("de zelfstandige beurt hoort te beginnen")
	}
	s.endTurn(gen)
	cancel()
	tickAndCollect(t, clock, ch, 4*time.Minute)
	s.appendSegment("en nog meer", "spreker-1")
	nextEvent(t, ch, EventTranscript)
	if evs := tickAndCollect(t, clock, ch, 3*time.Minute); !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("spraak en duifgeluid mogen de vraag niet uitstellen: %v", eventTypes(evs))
	}

	// Gepraat terwijl de vraag openstaat is geen antwoord.
	s.appendSegment("we zijn er nog hoor", "spreker-2")
	nextEvent(t, ch, EventTranscript)
	evs := tickAndCollect(t, clock, ch, time.Minute)
	if eventOf(evs, EventListening).GraceMS == 0 {
		t.Fatalf("een gehoorde zin sloot de vraag: %v", eventTypes(evs))
	}
	evs = tickAndCollect(t, clock, ch, 61*time.Second)
	if closed := evs[len(evs)-1]; closed.Type != EventSessionClosed || closed.Reason != CloseReasonInactivity {
		t.Fatalf("laatste event = %+v, want session_closed wegens inactiviteit", closed)
	}
}

// Zolang de vraag openstaat begint de duif niets nieuws op eigen
// initiatief. Een beurt waar de gebruiker zelf om vraagt mag wel: die vraag is
// meteen ook het antwoord (de route roept noteUserActivity aan).
func TestMeeting_PresenceCheck_StopsNewAutonomousTurns(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	on := true
	s.applySettings(&on, "", nil)
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	if evs := tickAndCollect(t, clock, ch, 11*time.Minute); !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("presence_check ontbreekt: %v", eventTypes(evs))
	}
	if !s.governorState().PresencePending {
		t.Fatal("de governor hoort te zien dat de vraag openstaat")
	}
	if _, _, _, ok := s.beginTurn(true); ok {
		t.Fatal("een zelfstandige beurt mag niet beginnen terwijl de vraag openstaat")
	}
	gen, _, cancel, ok := s.beginTurn(false)
	if !ok {
		t.Fatal("een beurt op verzoek hoort gewoon te beginnen")
	}
	cancel()
	s.endTurn(gen)
	s.noteUserActivity()
	if _, _, cancel, ok := s.beginTurn(true); !ok {
		t.Fatal("na het antwoord mag de duif weer zelfstandig meedoen")
	} else {
		cancel()
	}
}

// Een luisterovergang (de STT-socket valt even weg en komt
// terug) is ook een hartslag. Zolang de vraag openstaat draagt hij de
// resterende tijd: zonder graceMs las de client hem als "beantwoord", sloot de
// dialoog en liet de server de vergadering alsnog stoppen.
func TestMeeting_ListenTransitionDuringPresenceCheckKeepsGrace(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	if evs := tickAndCollect(t, clock, ch, 11*time.Minute); !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("presence_check ontbreekt: %v", eventTypes(evs))
	}
	clock.skip(20 * time.Second)
	s.setListenState(ListenReconnecting)
	if beat := nextEvent(t, ch, EventListening); beat.GraceMS != (100 * time.Second).Milliseconds() {
		t.Errorf("luisterovergang tijdens de vraag draagt graceMs %d, want 100000", beat.GraceMS)
	}
	s.noteUserActivity()
	s.setListenState(ListenActive)
	if beat := nextEvent(t, ch, EventListening); beat.GraceMS != 0 {
		t.Errorf("luisterovergang zonder open vraag draagt graceMs %d", beat.GraceMS)
	}
}

// Een kandidaat die bij het verschijnen van de vraag klaarlag
// (cooldown, wachten op stilte) is na twee minuten wachten verouderd. Hij
// verdwijnt zodra de vraag opengaat, en het scherm hoort dat.
func TestMeeting_PresenceCheck_DropsTheHeldCandidate(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	s.setCandidate(&meetingCandidate{ID: "c-1", Text: "een oude bijdrage", Score: 0.9, At: clock.Now()})
	if ev := nextEvent(t, ch, EventCandidate); ev.Candidate == nil {
		t.Fatal("de kandidaat hoort eerst op het scherm te staan")
	}
	evs := tickAndCollect(t, clock, ch, 11*time.Minute)
	if !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("presence_check ontbreekt: %v", eventTypes(evs))
	}
	if s.heldCandidate() != nil {
		t.Error("de vastgehouden kandidaat hoort weg te zijn zodra de vraag opengaat")
	}
	if ev := eventOf(evs, EventCandidate); ev.Type == "" || ev.Candidate != nil {
		t.Errorf("het scherm hoort een lege kandidaat te krijgen: %v", eventTypes(evs))
	}
}

// De harde plafond-timeout (standaard twee uur) blijft als backstop bestaan,
// ook in een overleg waar steeds iemand aan het scherm zit.
func TestMeeting_MaxDuration_ClosesEvenWhenBusy(t *testing.T) {
	e, clock := newTestEngine(t, nil, func(e *meetingEngine) {
		e.cfg.MaxDuration = 30 * time.Minute
	})
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	for range 3 {
		s.noteUserActivity()
		if evs := tickAndCollect(t, clock, ch, 8*time.Minute); hasEvent(evs, EventSessionClosed) || hasEvent(evs, EventPresenceCheck) {
			t.Fatalf("de sessie sloot of vroeg te vroeg: %v", eventTypes(evs))
		}
	}
	s.noteUserActivity()
	evs := tickAndCollect(t, clock, ch, 8*time.Minute)
	closed := evs[len(evs)-1]
	if closed.Type != EventSessionClosed || closed.Reason != CloseReasonMaxDuration {
		t.Fatalf("laatste event = %+v, want session_closed wegens de plafond-timeout", closed)
	}
}

// Sessiebeheer: sessies zijn user-scoped en een onbekende of andermans sessie
// bestaat simpelweg niet.
func TestMeetingEngine_SessionsAreUserScoped(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "en")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, ok := e.session(s.id, "user-1"); !ok {
		t.Error("eigen sessie moet vindbaar zijn")
	}
	if _, ok := e.session(s.id, "user-2"); ok {
		t.Error("de sessie van een andere gebruiker mag niet vindbaar zijn")
	}
	if _, ok := e.session("bestaat-niet", "user-1"); ok {
		t.Error("onbekende sessie mag niet vindbaar zijn")
	}
}

func TestMeetingEngine_OpenValidatesPersonaAndLang(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	if _, err := e.open("user-1", "flamingo", "en"); err == nil {
		t.Error("onbekende duif moet een fout geven")
	}
	s, err := e.open("user-1", "biological", "de")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if s.lang != "en" {
		t.Errorf("taal = %q, want de default en", s.lang)
	}
}

// Een dichte sessie is dicht: tweede sluiting doet niets, en events erna
// verdwijnen zonder te blokkeren of te paniekeren.
func TestMeetingSession_CloseIsIdempotent(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	s.close(CloseReasonUser)
	closed := nextEvent(t, ch, EventSessionClosed)
	if closed.Reason != CloseReasonUser {
		t.Fatalf("reden = %q", closed.Reason)
	}
	s.close(CloseReasonInactivity)
	s.appendSegment("valt in het niets", "")
	if _, ok := <-ch; ok {
		t.Error("na sluiten hoort het kanaal dicht te zijn en dicht te blijven")
	}
	if !s.isClosed() {
		t.Error("isClosed moet true geven")
	}
}

// De klasse van een event bepaalt wat er op een volle abonneebuffer gebeurt:
// vervallen of de abonnee eruit. Die keuze is te belangrijk om impliciet te
// laten — deze tabel pint hem per eventtype vast, en de AST-controle eronder
// dwingt af dat een nieuw Event*-constante hier een regel krijgt.
func TestEventClass_EveryEventTypeIsPinned(t *testing.T) {
	rows := []struct {
		constant string
		ev       MeetingEvent
		want     eventClass
	}{
		// Stroom: per stuk waardeloos zodra het te laat is.
		{"EventTTSAudio", MeetingEvent{Type: EventTTSAudio}, classStream},
		{"EventListening", MeetingEvent{Type: EventListening}, classStream},
		{"EventArousal", MeetingEvent{Type: EventArousal}, classStream},
		{"EventTranscript", MeetingEvent{Type: EventTranscript, Final: false}, classStream},
		// Besturing: bepaalt wat de UI denkt te weten.
		{"EventTranscript", MeetingEvent{Type: EventTranscript, Final: true}, classControl},
		{"EventState", MeetingEvent{Type: EventState}, classControl},
		{"EventPersona", MeetingEvent{Type: EventPersona}, classControl},
		{"EventText", MeetingEvent{Type: EventText}, classControl},
		{"EventSources", MeetingEvent{Type: EventSources}, classControl},
		{"EventTextDone", MeetingEvent{Type: EventTextDone}, classControl},
		{"EventTTSDone", MeetingEvent{Type: EventTTSDone}, classControl},
		{"EventResponseDone", MeetingEvent{Type: EventResponseDone}, classControl},
		{"EventBusy", MeetingEvent{Type: EventBusy}, classControl},
		{"EventStopped", MeetingEvent{Type: EventStopped}, classControl},
		{"EventError", MeetingEvent{Type: EventError}, classControl},
		{"EventPresenceCheck", MeetingEvent{Type: EventPresenceCheck}, classControl},
		{"EventSessionClosed", MeetingEvent{Type: EventSessionClosed}, classControl},
		{"EventIntervention", MeetingEvent{Type: EventIntervention}, classControl},
		{"EventDetectorStatus", MeetingEvent{Type: EventDetectorStatus}, classControl},
		{"EventSound", MeetingEvent{Type: EventSound}, classControl},
		{"EventSettings", MeetingEvent{Type: EventSettings}, classControl},
		{"EventCandidate", MeetingEvent{Type: EventCandidate}, classControl},
	}
	pinned := map[string]bool{}
	for _, row := range rows {
		pinned[row.constant] = true
		if got := classOf(row.ev); got != row.want {
			t.Errorf("classOf(%s, final=%v) = %s, want %s",
				row.constant, row.ev.Final, className(got), className(row.want))
		}
	}
	for _, name := range eventConstantNames(t) {
		if !pinned[name] {
			t.Errorf("%s staat niet in de klassetabel: kies bewust stroom of besturing", name)
		}
	}
	// Faalt dicht: wat de tabel niet kent is besturing, nooit wegwerp.
	if got := classOf(MeetingEvent{Type: "nog-niet-bedacht"}); got != classControl {
		t.Errorf("een onbekend eventtype = %s, want %s", className(got), className(classControl))
	}
}

func className(c eventClass) string {
	if c == classStream {
		return "stroom"
	}
	return "besturing"
}

// eventConstantNames leest de Event*-constanten uit meeting.go. Zo kan een
// nieuw eventtype er niet ongemerkt bij komen.
func eventConstantNames(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "meeting.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("meeting.go parsen: %v", err)
	}
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				if strings.HasPrefix(id.Name, "Event") {
					names = append(names, id.Name)
				}
			}
		}
	}
	if len(names) < 10 {
		t.Fatalf("slechts %d Event-constanten gevonden — controleer de parser", len(names))
	}
	return names
}

// subscriberCount is het aantal levende abonnementen van een sessie.
func subscriberCount(s *meetingSession) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// drainUntilClosed leest een abonneekanaal leeg en geeft de gelezen types
// terug; hij geeft false als het kanaal niet binnen de deadline dichtging.
func drainUntilClosed(t *testing.T, ch <-chan MeetingEvent) ([]string, bool) {
	t.Helper()
	var types []string
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return types, true
			}
			types = append(types, ev.Type)
		case <-deadline:
			return types, false
		}
	}
}

// Een trage of weggelopen abonnee mag de sessie nooit blokkeren — geen
// milliseconde, want dispatch draait onder de sessielock. Stroomevents
// vervallen daarom geteld en de abonnee mag blijven meekijken.
func TestMeetingSession_SlowSubscriberDropsStreamEventsInsteadOfBlocking(t *testing.T) {
	e, _ := newTestEngine(t, nil, func(e *meetingEngine) { e.cfg.EventBuffer = 4 })
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			s.publishPartial(fmt.Sprintf("partial %d", i), "")
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("een trage abonnee blokkeerde de sessie")
	}
	if s.droppedEvents() == 0 {
		t.Error("gedropte events horen geteld te worden")
	}
	if len(ch) > 4 {
		t.Errorf("bufferlengte = %d, want hooguit de geconfigureerde 4", len(ch))
	}
	if subscriberCount(s) != 1 {
		t.Error("een stroomevent mag geen abonnement kosten")
	}
}

// Ook de echte weg (gecommitte segmenten, dus besturingsevents) houdt de
// sessie nooit op: de abonnee vliegt eruit in plaats van dat er gewacht wordt.
func TestMeetingSession_ControlEventStormNeverBlocksTheSession(t *testing.T) {
	e, _ := newTestEngine(t, nil, func(e *meetingEngine) { e.cfg.EventBuffer = 4 })
	s, _ := e.open("user-1", "individualist", "en")
	_, unsub := s.subscribe()
	defer unsub()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			s.appendSegment(fmt.Sprintf("segment %d", i), "")
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("een trage abonnee blokkeerde de sessie")
	}
	if subscriberCount(s) != 0 {
		t.Error("een abonnee die besturing mist hoort losgekoppeld te zijn")
	}
	if s.isClosed() {
		t.Error("een gekickte abonnee mag de sessie niet sluiten")
	}
}

// Een besturingsevent op een volle buffer kost de abonnee zijn abonnement:
// het kanaal gaat dicht en de inschrijving verdwijnt. De SSE-lus eindigt
// daarop, de EventSource verbindt opnieuw en krijgt een verse state — beter
// dan een scherm dat stil achterloopt.
func TestMeetingSession_ControlEventOnFullBufferKicksSubscriber(t *testing.T) {
	logger, logged := captureLogs()
	e, _ := newTestEngine(t, func(m *Module) { m.logger = logger }, func(e *meetingEngine) {
		e.cfg.EventBuffer = 1
	})
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe() // het state-event vult de buffer meteen
	defer unsub()

	start := time.Now()
	s.publish(MeetingEvent{Type: EventStopped})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("het besturingsevent wachtte %s op een volle buffer", elapsed)
	}

	types, closed := drainUntilClosed(t, ch)
	if !closed {
		t.Fatalf("het kanaal van de gekickte abonnee hoort dicht te zijn (kreeg %v)", types)
	}
	if len(types) != 1 || types[0] != EventState {
		t.Errorf("gelezen events = %v, want alleen de state die al in de buffer stond", types)
	}
	if subscriberCount(s) != 0 {
		t.Error("de gekickte abonnee staat nog in de sessie")
	}
	if s.isClosed() {
		t.Error("een gekickte abonnee mag de sessie niet sluiten")
	}
	// Alleen het type, nooit inhoud: vergaderinhoud komt nooit in het log.
	if !strings.Contains(logged(), "subscriber_kicked") || !strings.Contains(logged(), "type="+EventStopped) {
		t.Errorf("de kick hoort met reden en type gelogd te worden:\n%s", logged())
	}

	// Uitschrijven van een al verwijderd abonnement is een no-op, en een
	// volgende abonnee krijgt gewoon zijn verse state.
	unsub()
	ch2, unsub2 := s.subscribe()
	defer unsub2()
	if st := nextEvent(t, ch2, EventState); st.SessionID != s.id {
		t.Errorf("herverbinding kreeg de verkeerde sessiestatus: %+v", st)
	}
}

// Na een kick mag close() niet alsnog hetzelfde kanaal sluiten: het is uit
// s.subs verdwenen. (Draai deze test ook met -race.)
func TestMeetingSession_CloseAfterKickDoesNotPanic(t *testing.T) {
	e, _ := newTestEngine(t, nil, func(e *meetingEngine) { e.cfg.EventBuffer = 1 })
	s, _ := e.open("user-1", "individualist", "en")
	kicked, unsub := s.subscribe() // buffer meteen vol, niemand leest
	defer unsub()
	s.publish(MeetingEvent{Type: EventStopped})
	if subscriberCount(s) != 0 {
		t.Fatal("de abonnee hoorde gekickt te zijn")
	}

	// Een tweede, wél lezende abonnee hoort de sluiting nog gewoon.
	live, unsubLive := s.subscribe()
	defer unsubLive()
	nextEvent(t, live, EventState)

	s.close(CloseReasonUser)
	s.close(CloseReasonUser) // idempotent, ook na een kick

	if _, closed := drainUntilClosed(t, kicked); !closed {
		t.Error("het gekickte kanaal hoort dicht te blijven")
	}
	if ev := nextEvent(t, live, EventSessionClosed); ev.Reason != CloseReasonUser {
		t.Errorf("reden = %q, want %q", ev.Reason, CloseReasonUser)
	}
}

// Stroomevents blijven vervallen op een volle buffer: een audioframe dat te
// laat komt is waardeloos, en de sessie mag er geen milliseconde op wachten.
func TestMeetingSession_StreamEventsDropImmediately(t *testing.T) {
	e, _ := newTestEngine(t, nil, func(e *meetingEngine) { e.cfg.EventBuffer = 1 })
	s, _ := e.open("user-1", "individualist", "en")
	_, unsub := s.subscribe() // buffer vol met het state-event, niemand leest
	defer unsub()

	before := s.droppedEvents()
	start := time.Now()
	s.publish(MeetingEvent{Type: EventTTSAudio, Audio: "frame"})
	s.publish(MeetingEvent{Type: EventListening, Listening: ListenActive})
	s.publish(MeetingEvent{Type: EventTranscript, Text: "partial"})
	elapsed := time.Since(start)

	if got := s.droppedEvents() - before; got != 3 {
		t.Errorf("gedropte stroomevents = %d, want 3", got)
	}
	if subscriberCount(s) != 1 {
		t.Error("stroomevents mogen geen abonnement kosten")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("stroomevents wachtten %s op een volle buffer", elapsed)
	}
}

// De hartslag is de uitzondering die de klasse niet aan het type kan aflezen:
// periodiek is hij stroom (vervalt), maar een échte overgang van de
// luisterstand is nieuws en kost de trage abonnee zijn abonnement.
func TestMeetingSession_ListenStateTransitionIsControl(t *testing.T) {
	e, clock := newTestEngine(t, nil, func(e *meetingEngine) { e.cfg.EventBuffer = 1 })
	s, _ := e.open("user-1", "individualist", "en")
	_, unsub := s.subscribe() // buffer vol met het state-event
	defer unsub()

	// De periodieke hartslag uit de bewakingslus vervalt geteld.
	before := s.droppedEvents()
	s.tick(clock.Now())
	if s.droppedEvents() == before {
		t.Error("een periodieke hartslag hoort te vervallen, niet te kicken")
	}
	if subscriberCount(s) != 1 {
		t.Fatal("een periodieke hartslag mag geen abonnement kosten")
	}

	// Een werkelijke overgang wél.
	s.setListenState(ListenReconnecting)
	if subscriberCount(s) != 0 {
		t.Error("een overgang van de luisterstand hoort besturing te zijn")
	}
}

// Uitschrijven laat de sessie ongemoeid: de UI kan de stream sluiten en later
// opnieuw meekijken.
func TestMeetingSession_UnsubscribeDoesNotCloseSession(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	nextEvent(t, ch, EventState)
	unsub()
	unsub() // idempotent
	s.appendSegment("gaat gewoon door", "")
	if s.isClosed() {
		t.Error("uitschrijven mag de sessie niet sluiten")
	}
	ch2, unsub2 := s.subscribe()
	defer unsub2()
	if st := nextEvent(t, ch2, EventState); st.SessionID != s.id {
		t.Errorf("nieuw abonnement kreeg de verkeerde sessiestatus: %+v", st)
	}
}

// shutdown ruimt alles op — de vergadermodus laat geen sessies of goroutines
// achter als de server stopt.
func TestMeetingEngine_ShutdownClosesEverySession(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s1, _ := e.open("user-1", "individualist", "en")
	s2, _ := e.open("user-2", "biological", "nl")
	ch, unsub := s1.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	e.shutdown()
	closed := nextEvent(t, ch, EventSessionClosed)
	if closed.Reason != CloseReasonShutdown {
		t.Errorf("reden = %q, want shutdown", closed.Reason)
	}
	if !s1.isClosed() || !s2.isClosed() {
		t.Error("shutdown moet elke sessie sluiten")
	}
	if e.count() != 0 {
		t.Errorf("sessiebeheer = %d sessies, want leeg", e.count())
	}
}

// De systeemklok is de productie-implementatie van de seam; hij moet echt
// tikken (en zijn ticker netjes kunnen stoppen).
func TestSystemClock_TicksAndStops(t *testing.T) {
	c := systemClock{}
	if time.Since(c.Now()) > time.Minute {
		t.Error("systemClock.Now moet de echte tijd geven")
	}
	tk := c.NewTicker(time.Millisecond)
	defer tk.Stop()
	select {
	case <-tk.C():
	case <-time.After(2 * time.Second):
		t.Fatal("de systeemticker tikte niet")
	}
}

// De gedragsconfig valt terug op de defaults als personas.yaml de
// inactiviteitswaarden niet (of als nul) meegeeft, en neemt ze anders over, in
// seconden. De coulance voor een weggevallen kanaal schuift niet mee.
func TestMeetingConfigFromGedrag_FallsBackToSpecDefaults(t *testing.T) {
	cfg := meetingConfigFromGedrag(Gedrag{})
	if cfg.InactivityPrompt != 10*time.Minute {
		t.Errorf("inactiviteitsdrempel = %s, want 10m", cfg.InactivityPrompt)
	}
	if cfg.PresenceGrace != 2*time.Minute {
		t.Errorf("aftelling = %s, want 2m", cfg.PresenceGrace)
	}
	if cfg.MaxDuration != 2*time.Hour {
		t.Errorf("plafond = %s, want 2h", cfg.MaxDuration)
	}
	if cfg.ClientGoneGrace != 90*time.Second {
		t.Errorf("coulance weggevallen kanaal = %s, want 90s", cfg.ClientGoneGrace)
	}

	uit := meetingConfigFromGedrag(Gedrag{InactiviteitPromptSeconden: 20, InactiviteitSluitSeconden: 10, VergaderingMaxSeconden: 600})
	if uit.InactivityPrompt != 20*time.Second || uit.PresenceGrace != 10*time.Second || uit.MaxDuration != 10*time.Minute {
		t.Errorf("config uit de yaml niet overgenomen: %+v", uit)
	}
	if uit.ClientGoneGrace != 90*time.Second {
		t.Errorf("coulance weggevallen kanaal schoof mee met de aftelling: %s", uit.ClientGoneGrace)
	}
}

// Een venster zonder leeftijdsgrens vervalt alleen op aantal.
func TestRollingTranscript_WithoutMaxAge_PrunesOnlyByCount(t *testing.T) {
	rt := NewRollingTranscript(2, 0)
	for i := range 3 {
		rt.Append(TranscriptSegment{Text: "x", At: meetingEpoch.Add(-time.Duration(i) * time.Hour)})
	}
	if rt.Len() != 2 {
		t.Fatalf("aantal = %d, want 2", rt.Len())
	}
}

// Wie zich abonneert op een al gesloten sessie krijgt een dicht kanaal, geen
// stille hang.
func TestMeetingSession_SubscribeAfterClose_ReturnsClosedChannel(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, _ := e.open("user-1", "individualist", "en")
	s.close(CloseReasonUser)

	ch, unsub := s.subscribe()
	defer unsub()
	if _, ok := <-ch; ok {
		t.Error("het kanaal van een gesloten sessie hoort meteen dicht te zijn")
	}
	s.publish(MeetingEvent{Type: EventBusy})
	if !s.tick(s.clock.Now()) {
		return // verwacht: een gesloten sessie tikt niet meer
	}
	t.Error("tick op een gesloten sessie moet false geven")
}

// De stopgrendel, los getest: alles wat na een stopknop nog uit de LLM of de TTS
// komt, hoort bij een verlopen beurtgeneratie en bereikt het kanaal niet.
func TestMeetingTurn_StaleGeneration_ReachesNobody(t *testing.T) {
	voice := newFakeVoice("frame-1", "frame-2")
	e, _ := newTestEngine(t, func(m *Module) { m.llm = newGatedLLM("laat antwoord") },
		func(e *meetingEngine) { e.voice = voice })
	s, _ := e.open("user-1", "individualist", "en")
	ch, unsub := s.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("spreekslot niet gekregen")
	}
	defer cancel()
	s.stopSpeaking() // hoogt de generatie op: `gen` is nu verlopen
	nextEvent(t, ch, EventStopped)

	// Bewust een eigen context: hier wordt de generatiewacht getoetst, niet de
	// annulering — ook een provider die de cancel mist, komt er niet langs.
	ctx := context.Background()
	sp := e.newSpeaker(ctx, s, gen)
	if _, _, ok := e.streamTurn(ctx, s, gen, nil, sp, nil, nil); ok {
		t.Error("streamTurn hoort een verlopen beurt af te breken")
	}
	sp.feed("Wordt niet meer uitgesproken. En dit ook niet.")
	sp.finish()

	select {
	case ev := <-ch:
		t.Errorf("er kwam nog %q binnen na de stopknop", ev.Type)
	default:
	}
}

// Zonder LLM-service is een spreekbeurt een nette foutmelding, geen stilte.
func TestMeetingRespondNow_WithoutLLMService_ReportsUnavailable(t *testing.T) {
	e, _ := newTestEngine(t, func(m *Module) { m.llm = nil }, nil)
	s, _ := e.open("user-1", "individualist", "en")
	s.appendSegment("er wordt iets gevraagd", "")
	ch, unsub := s.subscribe()
	defer unsub()

	if err := e.respondNow(s); err != nil {
		t.Fatalf("respondNow: %v", err)
	}
	ev := nextEvent(t, ch, EventError)
	if ev.Error != ReasonLLMUnavailable {
		t.Errorf("reason = %q, want llm_unavailable", ev.Error)
	}
	nextEvent(t, ch, EventResponseDone)
}

// Lege chunks (keep-alives van de proxy) worden overgeslagen, niet als lege
// tekstdelta doorgestuurd.
func TestMeetingRespondNow_EmptyChunksAreSkipped(t *testing.T) {
	gllm := newGatedLLM()
	gllm.chunks = []llm.StreamChunk{{Content: ""}, {Content: "wel tekst", FinishReason: "stop"}}
	e, _ := newTestEngine(t, func(m *Module) { m.llm = gllm }, nil)
	s, _ := e.open("user-1", "individualist", "en")
	s.appendSegment("er wordt iets gevraagd", "")
	ch, unsub := s.subscribe()
	defer unsub()

	if err := e.respondNow(s); err != nil {
		t.Fatalf("respondNow: %v", err)
	}
	text, evs := collectSpokenText(t, ch)
	if text != "wel tekst" {
		t.Errorf("tekst = %q", text)
	}
	var tekstFrames int
	for _, ev := range evs {
		if ev.Type == EventText {
			tekstFrames++
		}
	}
	if tekstFrames != 1 {
		t.Errorf("aantal tekstframes = %d, want 1 (lege chunk overgeslagen)", tekstFrames)
	}
}

// Init wiredt de vergadermodus met de echte klok — zonder repository.
func TestInit_WiresMeetingEngineWithoutRepository(t *testing.T) {
	m := New()
	if err := m.Init(&parai.Core{DB: idlePool(t), LLM: llmtesting.NewMockService()}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.meetings == nil {
		t.Fatal("Init moet het vergadersessiebeheer wiren")
	}
	if _, ok := m.meetings.clock.(systemClock); !ok {
		t.Errorf("klok = %T, want de systeemklok", m.meetings.clock)
	}
	if m.meetings.cfg.InactivityPrompt != 10*time.Minute {
		t.Errorf("gedragsconfig niet overgenomen: %+v", m.meetings.cfg)
	}
	if m.meetings.voice != nil {
		t.Error("er is nog geen TTS-provider: de seam hoort leeg te zijn")
	}
}
