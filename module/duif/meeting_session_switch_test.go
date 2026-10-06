package duif

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// De uitweg uit "er is al een vergadering open" en de duifwissel tijdens een
// lopende vergadering delen één mechanisme: een open-verzoek dat de
// plaats van je eigen sessie inneemt. Deze toetsen bewaken de twee grenzen die
// eromheen staan — het is altijd je eigen sessie, en het auteurschap van wat er
// al gezegd is verschuift niet — plus de derde reden dat de melding überhaupt
// verscheen: een weggeklikt tabblad hield zijn plaats vast.

// De gebruiker mag zichzelf losmaken: zijn eigen open vergadering sluit
// en de nieuwe begint meteen.
func TestMeetingOpen_ReplaceOwn_ClosesOwnSessionAndSeatsTheNewOne(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxSessions = 4
	e.maxUserSessions = 1

	first, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("eerste sessie: %v", err)
	}
	ch, unsub := first.subscribe()
	defer unsub()
	nextEvent(t, ch, EventState)

	if _, err := e.open("user-1", "individualist", "nl"); !errors.Is(err, errMeetingCapacitySelf) {
		t.Fatalf("tweede sessie zonder vervanging = %v, want errMeetingCapacitySelf", err)
	}

	second, err := e.openSession("user-1", "individualist", "nl", meetingSettings{}, openRequest{ReplaceOwn: true})
	if err != nil {
		t.Fatalf("vervangend openen: %v", err)
	}
	if second.id == first.id {
		t.Fatal("de vervanger hoort een nieuwe sessie te zijn")
	}
	closed := nextEvent(t, ch, EventSessionClosed)
	if closed.Reason != CloseReasonReplaced {
		t.Errorf("sluitreden = %q, want %q", closed.Reason, CloseReasonReplaced)
	}
	if !first.isClosed() {
		t.Error("de oude sessie hoort dicht te zijn")
	}
	if _, ok := e.session(second.id, "user-1"); !ok {
		t.Error("de nieuwe sessie hoort te leven")
	}
	if e.count() != 1 {
		t.Errorf("aantal sessies = %d, want 1", e.count())
	}
}

// De harde grens: vervangen raakt uitsluitend het eigen account. Zit de tafel
// vol met andermans vergaderingen, dan blijft de melding zonder uitweg en
// verliest niemand zijn plaats.
func TestMeetingOpen_ReplaceOwn_NeverFreesAnotherAccountsSeat(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxSessions = 1
	e.maxUserSessions = 1

	other, err := e.open("user-2", "individualist", "en")
	if err != nil {
		t.Fatalf("sessie van de ander: %v", err)
	}

	_, err = e.openSession("user-1", "individualist", "en", meetingSettings{}, openRequest{ReplaceOwn: true})
	if !errors.Is(err, errMeetingCapacityFull) {
		t.Fatalf("openen = %v, want errMeetingCapacityFull", err)
	}
	if errors.Is(err, errMeetingCapacitySelf) {
		t.Error("zonder eigen sessie hoort de melding geen uitweg te beloven")
	}
	if other.isClosed() {
		t.Fatal("de sessie van een ander account is gesloten — dat mag nooit")
	}
	if e.count() != 1 {
		t.Errorf("aantal sessies = %d, want 1", e.count())
	}
}

// Beide soorten "vol" zijn een 429, maar met een eigen code en een eigen
// tweetalige tekst: de UI mag alleen bij de eerste een knop aanbieden.
func TestMeetingOpenHandler_CapacityTellsOwnSessionApartFromFullTable(t *testing.T) {
	rig := newMeetingRig(t, nil, func(e *meetingEngine) {
		e.maxSessions = 2
		e.maxUserSessions = 1
	})
	first := rig.openSession(t, "individualist", "nl")

	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{"pigeonId": "individualist", "lang": "nl"})
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var fail struct {
		Error   string            `json:"error"`
		Message map[string]string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &fail); err != nil {
		t.Fatalf("foutbody = %s (%v)", body, err)
	}
	if fail.Error != "meeting_capacity_self" {
		t.Errorf("foutcode = %q, want meeting_capacity_self", fail.Error)
	}
	if fail.Message["en"] == "" || fail.Message["nl"] == "" {
		t.Errorf("de melding moet tweetalig zijn: %+v", fail.Message)
	}

	// Dezelfde route mét bevestiging: de eigen sessie wijkt en de nieuwe start.
	status, body = rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{
		"pigeonId": "individualist", "lang": "nl", "replaceExisting": true,
	})
	if status != http.StatusCreated {
		t.Fatalf("vervangend openen: status = %d, body = %s", status, body)
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.SessionID == "" || out.SessionID == first {
		t.Fatalf("nieuwe sessie-body = %s (%v)", body, err)
	}
	if _, ok := rig.engine.session(first, testUser.ID); ok {
		t.Error("de oude sessie hoort gesloten te zijn")
	}
	if logs := rig.logs(); !strings.Contains(logs, "reden="+CloseReasonReplaced) {
		t.Errorf("het log meldt de vervanging niet: %s", logs)
	}
}

// De wissel neemt de vergadering mee: wat de mensen zeiden én wat de
// vorige duif bijdroeg, met haar naam eraan vast.
func TestMeetingHandover_CarriesContextWithStableAuthorship(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxUserSessions = 1

	first, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("eerste sessie: %v", err)
	}
	first.appendSegment("We willen de duiven van het Stationsplein weren.", "spreker-1")
	first.noteContribution("Roekoe. Daar heb ik wel wat op te zeggen.", 0, ownGrounding{})
	first.appendSegment("Wat vind jij daarvan?", "spreker-2")

	second, err := e.openSession("user-1", "biological", "nl", meetingSettings{}, openRequest{Handover: first.id})
	if err != nil {
		t.Fatalf("wissel: %v", err)
	}
	if !first.isClosed() {
		t.Error("de vorige sessie hoort met de wissel te sluiten")
	}
	if e.count() != 1 {
		t.Errorf("aantal sessies = %d, want 1 (de wissel mag nooit op de capaciteitsregel stuiten)", e.count())
	}

	window := second.window()
	if len(window) != 3 {
		t.Fatalf("venster = %d segmenten, want 3: %+v", len(window), window)
	}
	if window[0].Text != "We willen de duiven van het Stationsplein weren." || window[0].PigeonID != "" {
		t.Errorf("eerste regel = %+v, want de menselijke openingszin", window[0])
	}
	if window[1].PigeonID != "individualist" {
		t.Errorf("de bijdrage van de vorige duif = %+v, want auteur individualist", window[1])
	}
	if window[2].Text != "Wat vind jij daarvan?" || window[2].PigeonID != "" {
		t.Errorf("laatste regel = %+v, want de menselijke vraag", window[2])
	}
	for i := 1; i < len(window); i++ {
		if window[i].Seq <= window[i-1].Seq {
			t.Fatalf("de volgorde van de vergadering is niet bewaard: %+v", window)
		}
	}

	// Wat er hierna binnenkomt staat achter de erfenis, niet ertussen.
	second.appendSegment("Dat is wel een verschil.", "spreker-1")
	fresh := second.window()
	if last := fresh[len(fresh)-1]; last.Text != "Dat is wel een verschil." || last.Seq <= window[2].Seq {
		t.Errorf("nieuw segment = %+v, want ná de overgenomen context", last)
	}
}

// De prompt zegt met zoveel woorden dat die regels van een ándere duif zijn —
// in beide talen. Zonder die zin zou de nieuwe duif ze als eigen herinnering
// kunnen oplezen.
func TestMeetingHandover_PromptMarksThePreviousPigeonInBothLanguages(t *testing.T) {
	segs := []TranscriptSegment{
		{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"},
		{Seq: 2, Text: "Roekoe. Ik woon hier ook.", Speaker: "Rafael", PigeonID: "individualist"},
	}
	const token = "tok-handover"
	for _, lang := range []string{"en", "nl"} {
		rendered := renderTranscriptWindow(lang, token, "kennispartner", segs, nil)
		if !strings.Contains(rendered, previousPigeonMark(lang, token)) {
			t.Errorf("[%s] de markering ontbreekt: %s", lang, rendered)
		}
		if !strings.Contains(rendered, previousPigeonNote(lang, token)) {
			t.Errorf("[%s] de uitleg over het auteurschap ontbreekt: %s", lang, rendered)
		}
		if !strings.Contains(rendered, "Rafael") {
			t.Errorf("[%s] de naam van de vorige duif ontbreekt: %s", lang, rendered)
		}
	}
	// Zonder erfenis blijft de prompt precies wat hij was.
	plain := renderTranscriptWindow("nl", token, "kennispartner", segs[:1], nil)
	if strings.Contains(plain, previousPigeonNote("nl", token)) {
		t.Errorf("een gewoon venster hoort de uitleg niet te dragen: %s", plain)
	}
}

// De uitweg belooft alleen wat zij kan waarmaken. Wie zijn eigen plaats al
// opgeeft en dan alsnog strandt, zit achter een tafel die anderen vol hebben:
// dan is er geen knop die helpt, en de eigen vergadering blijft staan.
func TestMeetingOpen_ReplaceOwn_DoesNotPromiseTheSelfWayOutAgain(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxSessions = 2
	e.maxUserSessions = 1

	mine, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("eigen sessie: %v", err)
	}
	other, err := e.open("user-2", "individualist", "en")
	if err != nil {
		t.Fatalf("sessie van de ander: %v", err)
	}
	// De tafel krimpt onder de lopende vergaderingen: de plaats die de ander
	// bezet is nu de enige die er is.
	e.mu.Lock()
	e.maxSessions = 1
	e.mu.Unlock()

	_, err = e.openSession("user-1", "individualist", "nl", meetingSettings{}, openRequest{ReplaceOwn: true})
	if !errors.Is(err, errMeetingCapacityFull) {
		t.Fatalf("vervangend openen = %v, want errMeetingCapacityFull", err)
	}
	if errors.Is(err, errMeetingCapacitySelf) {
		t.Error("na een vervangingsverzoek hoort de melding de uitweg niet opnieuw te beloven")
	}
	if mine.isClosed() {
		t.Error("een mislukte vervanging hoort de eigen vergadering te laten staan")
	}
	if other.isClosed() {
		t.Fatal("de sessie van een ander account is gesloten — dat mag nooit")
	}
}

// Een wissel is een vervanging van het eigen account, ook wanneer de sessie
// waarnaar hij wijst er niet meer is. Zonder dat zou de duifwissel na een
// vanzelf gesloten sessie op de 429 van een volle tafel stuiten en de gebruiker
// met een lege vergadering achterlaten.
func TestMeetingHandover_ToAVanishedSessionNeverHitsTheCapacityLimit(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxSessions = 4
	e.maxUserSessions = 1

	first, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("eerste sessie: %v", err)
	}

	second, err := e.openSession("user-1", "biological", "nl", meetingSettings{}, openRequest{Handover: "sessie-die-er-niet-meer-is"})
	if err != nil {
		t.Fatalf("wissel na een verdwenen sessie = %v, want geslaagd", err)
	}
	if !first.isClosed() {
		t.Error("de wissel hoort de eigen plaats in te nemen")
	}
	if e.count() != 1 {
		t.Errorf("aantal sessies = %d, want 1", e.count())
	}
	if got := second.window(); len(got) != 0 {
		t.Errorf("venster = %+v, want leeg: er was geen context om over te nemen", got)
	}
}

// Sluit de bron net terwijl de wissel binnenkomt, dan gaat de overdracht niet
// door: half meenemen van een losgelaten venster is erger dan niets meenemen.
// De wissel zelf loopt gewoon door en het log zegt eerlijk dat er niets
// meekwam.
func TestMeetingHandover_SkipsASourceThatJustClosed(t *testing.T) {
	rig := newMeetingRig(t, nil, func(e *meetingEngine) {
		e.maxSessions = 2
		e.maxUserSessions = 1
	})
	first := rig.openSession(t, "individualist", "nl")
	src, ok := rig.engine.session(first, testUser.ID)
	if !ok {
		t.Fatal("sessie verdwenen")
	}
	src.appendSegment("We willen de duiven van het Stationsplein weren.", "spreker-1")
	// Precies het venster tussen close() en engine.remove(): dicht, maar nog
	// aan tafel. Aan het eind ruimen we hem alsnog netjes op.
	src.mu.Lock()
	src.closed = true
	src.mu.Unlock()
	t.Cleanup(func() {
		src.mu.Lock()
		src.closed = false
		src.mu.Unlock()
		src.close(CloseReasonShutdown)
	})

	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{
		"pigeonId": "biological", "lang": "nl", "handoverFrom": first,
	})
	if status != http.StatusCreated {
		t.Fatalf("wissel: status = %d, body = %s", status, body)
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.SessionID == "" {
		t.Fatalf("sessie-body = %s (%v)", body, err)
	}
	second, ok := rig.engine.session(out.SessionID, testUser.ID)
	if !ok {
		t.Fatal("de nieuwe sessie hoort te leven")
	}
	if got := second.window(); len(got) != 0 {
		t.Errorf("venster = %+v, want leeg: een gesloten bron draagt niets over", got)
	}
	if logs := rig.logs(); !strings.Contains(logs, "context_overgenomen=false") {
		t.Errorf("het log hoort te melden dat er geen context meekwam: %s", logs)
	}
}

// Een handover naar andermans sessie is geen handover: geen context, en die
// sessie blijft ongemoeid.
func TestMeetingHandover_IgnoresASessionOfAnotherAccount(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxSessions = 4
	e.maxUserSessions = 1

	other, err := e.open("user-2", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie van de ander: %v", err)
	}
	other.appendSegment("Dit is niet van jou.", "spreker-1")

	mine, err := e.openSession("user-1", "individualist", "nl", meetingSettings{}, openRequest{Handover: other.id})
	if err != nil {
		t.Fatalf("openen: %v", err)
	}
	if other.isClosed() {
		t.Fatal("de sessie van een ander account is gesloten — dat mag nooit")
	}
	if got := mine.window(); len(got) != 0 {
		t.Errorf("venster = %+v, want leeg", got)
	}
}

// Een echte spreekbeurt legt vast wat de duif zei, zodat de volgende duif het
// als háár bijdrage kan krijgen. Het venster van deze duif zelf verandert niet.
func TestMeetingTurn_RecordsTheContributionForALaterHandover(t *testing.T) {
	gllm := newGatedLLM("Roekoe. ", "Ik vind daar wat van.")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
	nextEvent(t, es.ch, EventTranscript)
	if status, body := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatalf("respond: status = %d, body = %s", status, body)
	}
	if text, _ := collectSpokenText(t, es.ch); text != "Roekoe. Ik vind daar wat van." {
		t.Fatalf("gesproken tekst = %q", text)
	}

	s, ok := rig.engine.session(id, testUser.ID)
	if !ok {
		t.Fatal("sessie verdwenen")
	}
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.contributions) == 1
	}, "de bijdrage van de duif hoort vastgelegd te zijn")

	s.mu.Lock()
	got := s.contributions[0]
	s.mu.Unlock()
	if got.Text != "Roekoe. Ik vind daar wat van." || got.PigeonID != "individualist" {
		t.Errorf("bijdrage = %+v", got)
	}
	for _, seg := range s.window() {
		if seg.PigeonID != "" {
			t.Errorf("de duif hoort haar eigen bijdrage niet in haar eigen venster te zien: %+v", seg)
		}
	}
}

// Een tabblad dat weg is houdt geen plaats aan tafel bezet. Zolang er
// een kanaal openstaat blijft de sessie; valt het weg en komt er geen nieuw,
// dan sluit zij binnen de bestaande coulanceperiode van 90 s. Die is een eigen
// waarde: zij schuift niet mee met de aftelling van "Ben je er nog?", die hier
// expres anders staat.
func TestMeetingSession_ClosesAfterTheClientChannelStaysGone(t *testing.T) {
	e, clock := newTestEngine(t, nil, func(e *meetingEngine) { e.cfg.PresenceGrace = 5 * time.Minute })
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie openen: %v", err)
	}
	ch, unsub := s.subscribe()
	nextEvent(t, ch, EventState)

	// Met een client op de lijn verandert er niets: dit is geen tweede
	// inactiviteitstimer.
	if evs := tickAndCollect(t, clock, ch, 2*time.Minute); hasEvent(evs, EventSessionClosed) {
		t.Fatalf("een verbonden sessie mag niet sluiten: %v", eventTypes(evs))
	}

	unsub()
	if !waitForBool(func() bool { return s.clientGoneAt().IsZero() == false }) {
		t.Fatal("na de laatste afmelding hoort de coulanceklok te lopen")
	}
	clock.advance(t, 60*time.Second)
	if s.isClosed() {
		t.Fatal("binnen de coulanceperiode hoort de sessie te blijven staan")
	}
	clock.advance(t, 31*time.Second)
	waitFor(t, s.isClosed, "een sessie zonder client hoort na de coulanceperiode te sluiten")
	// close() zet de sessie eerst dicht en meldt haar pas daarna af bij de
	// engine; daartussen zit geen gedeelde lock. De plaats komt dus een
	// haarbreedte later vrij dan isClosed() waar is; de toets wacht daarop in
	// plaats van een race te toetsen.
	waitFor(t, func() bool {
		_, ok := e.session(s.id, "user-1")
		return !ok
	}, "de plaats aan tafel is niet vrijgekomen")
}

// Een client die binnen de coulanceperiode terugkomt (de browser verbindt een
// weggevallen SSE-kanaal zelf opnieuw) houdt de vergadering gewoon open.
func TestMeetingSession_ReconnectWithinGraceKeepsTheSessionOpen(t *testing.T) {
	e, clock := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie openen: %v", err)
	}
	ch, unsub := s.subscribe()
	nextEvent(t, ch, EventState)
	unsub()
	clock.advance(t, 60*time.Second)

	again, unsub2 := s.subscribe()
	defer unsub2()
	nextEvent(t, again, EventState)
	clock.advance(t, 60*time.Second)
	if s.isClosed() {
		t.Fatal("een herverbonden sessie hoort te blijven leven")
	}
}

// Het overdrachtsvenster is geen archief: sluiten laat het los, net als het
// transcript (streaming-only).
func TestMeetingSession_CloseReleasesTheRecordedContributions(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie openen: %v", err)
	}
	s.appendSegment("Dit zei een mens.", "spreker-1")
	s.noteContribution("Roekoe. Dit zei de duif [1].", 1, ownGrounding{
		Status: KBSources, Sources: []Source{{Number: 1, ID: "destillaat:1"}}, RAGSection: "[1] bron",
	})
	s.close(CloseReasonUser)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.contributions) != 0 || len(s.transcript.segments) != 0 {
		t.Errorf("na sluiten bleef vergaderinhoud staan: %+v / %+v", s.contributions, s.transcript.segments)
	}
	// Ook de grond van haar laatste punt: de bronnensectie is vergaderinhoud.
	if !reflect.DeepEqual(s.lastPoint, ownPoint{}) {
		t.Errorf("na sluiten bleef haar laatste punt staan: %+v", s.lastPoint)
	}
}

// clientGoneAt is de leesbare kant van de coulanceklok, alleen voor de toets.
func (s *meetingSession) clientGoneAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientGoneSince
}

func waitForBool(cond func() bool) bool {
	for i := 0; i < 200; i++ {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
