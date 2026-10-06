package duif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMeetingSessions_Create(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{"pigeonId": "biological", "lang": "nl"})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var out struct {
		SessionID string      `json:"sessionId"`
		PigeonID  string      `json:"pigeonId"`
		Lang      string      `json:"lang"`
		Listening ListenState `json:"listening"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body = %s (%v)", body, err)
	}
	if out.SessionID == "" || out.PigeonID != "biological" || out.Lang != "nl" || out.Listening != ListenActive {
		t.Fatalf("sessieantwoord = %+v", out)
	}
	if rig.engine.count() != 1 {
		t.Errorf("sessiebeheer = %d sessies, want 1", rig.engine.count())
	}
	// De vergadersessie is puur in-memory: er komt geen enkele rij bij.
	if len(rig.repo.sessions) != 0 || len(rig.repo.messages) != 0 {
		t.Errorf("vergadersessie hoort niet in de database te landen: %+v", rig.repo.sessions)
	}
}

// Een client die (opnieuw) verbindt mag nooit hoeven raden wat de knoppen voor
// zelfstandig meedoen doen: het open-antwoord én de sessiestatus dragen
// dezelfde levende stand.
func TestMeetingOpen_AndStateEvent_CarryTheLiveSettings(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{
		"pigeonId": "individualist", "lang": "nl",
		"autonomous": true, "eagerness": "eager", "cooldownSeconds": 90,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var out struct {
		SessionID  string          `json:"sessionId"`
		Settings   SessionSettings `json:"settings"`
		Autonomous bool            `json:"autonomous"`
		Eagerness  string          `json:"eagerness"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body = %s (%v)", body, err)
	}
	want := SessionSettings{TurnID: "0", Autonomous: true, Eagerness: "eager", CooldownSeconds: 90}
	if out.Settings != want {
		t.Errorf("open-antwoord settings = %+v, want %+v", out.Settings, want)
	}
	// De losse velden blijven staan voor bestaande lezers, en zeggen hetzelfde.
	if out.Autonomous != want.Autonomous || out.Eagerness != want.Eagerness {
		t.Errorf("losse velden = %v/%q, want %v/%q", out.Autonomous, out.Eagerness, want.Autonomous, want.Eagerness)
	}

	es := rig.events(t, out.SessionID)
	first := es.recorded()[0]
	if first.Settings == nil || *first.Settings != want {
		t.Fatalf("state-event settings = %+v, want %+v", first.Settings, want)
	}

	// En na een live wijziging leest een verse abonnee de nieuwe stand terug.
	if st, resp := rig.settings(t, out.SessionID, map[string]any{"eagerness": "reserved"}); st != http.StatusAccepted {
		t.Fatalf("instellingen bijstellen: status = %d, body = %s", st, resp)
	}
	herverbonden := rig.events(t, out.SessionID)
	again := herverbonden.recorded()[0]
	if again.Settings == nil || again.Settings.Eagerness != "reserved" {
		t.Errorf("state-event na wijziging = %+v, want gretigheid reserved", again.Settings)
	}
}

func TestMeetingSessions_CreateValidates(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{"onbekende duif", map[string]any{"pigeonId": "flamingo"}},
		{"lege duif", map[string]any{}},
		{"ongeldige taal", map[string]any{"pigeonId": "individualist", "lang": "de"}},
		{"onbekend veld", map[string]any{"pigeonId": "individualist", "stiekem": true}},
	}
	rig := newMeetingRig(t, nil, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", tc.body)
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", status, body)
			}
		})
	}
	if rig.engine.count() != 0 {
		t.Errorf("een geweigerde aanvraag mag geen sessie achterlaten (%d)", rig.engine.count())
	}
}

// Een vergadersessie zonder body is geldig: de defaults (Engels) gelden.
func TestMeetingSessions_CreateWithoutBody_UsesDefaults(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{"pigeonId": "individualist"})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if !strings.Contains(body, `"lang":"en"`) {
		t.Errorf("body = %s, want de default-taal en", body)
	}
}

func TestMeetingRoutes_UnknownSessionIs404(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/meeting/sessions/onbekend/events"},
		{http.MethodPost, "/v1/meeting/sessions/onbekend/respond"},
		{http.MethodPost, "/v1/meeting/sessions/onbekend/intervene"},
		{http.MethodPost, "/v1/meeting/sessions/onbekend/settings"},
		{http.MethodPost, "/v1/meeting/sessions/onbekend/stop"},
		{http.MethodPost, "/v1/meeting/sessions/onbekend/presence"},
		{http.MethodPost, "/v1/meeting/sessions/onbekend/transcript"},
		{http.MethodDelete, "/v1/meeting/sessions/onbekend"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body any
			switch {
			case strings.HasSuffix(tc.path, "transcript"):
				body = map[string]any{"text": "hoi"}
			case strings.HasSuffix(tc.path, "settings"):
				body = map[string]any{"autonomous": true}
			}
			status, raw := rig.do(t, tc.method, tc.path, body)
			if status != http.StatusNotFound {
				t.Errorf("status = %d, want 404 (body %s)", status, raw)
			}
			if strings.Contains(raw, "user") {
				t.Errorf("de 404 mag niets over eigenaarschap lekken: %s", raw)
			}
		})
	}
}

func TestMeetingRoutes_RequireLogin(t *testing.T) {
	m, _ := newTestModule(t, nil)
	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/v1/meeting/sessions"},
		{http.MethodGet, "/v1/meeting/sessions/x/events"},
		{http.MethodPost, "/v1/meeting/sessions/x/respond"},
		{http.MethodPost, "/v1/meeting/sessions/x/stop"},
		{http.MethodPost, "/v1/meeting/sessions/x/presence"},
		{http.MethodPost, "/v1/meeting/sessions/x/transcript"},
		{http.MethodDelete, "/v1/meeting/sessions/x"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := serve(t, m, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"text":"x"}`)), true)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestMeetingTranscript_AppendsAndStreams(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)

	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/transcript",
		map[string]any{"text": "de vergadering praat", "speaker": "spreker-2"})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if !strings.Contains(body, `"segments":1`) {
		t.Errorf("body = %s, want het aantal segmenten in het venster", body)
	}

	ev := nextEvent(t, es.ch, EventTranscript)
	if ev.Text != "de vergadering praat" || !ev.Final || ev.PigeonWindow {
		t.Errorf("transcript-event = %+v", ev)
	}
}

func TestMeetingTranscript_Validates(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")
	cases := []struct {
		name string
		body any
	}{
		{"lege tekst", map[string]any{"text": ""}},
		{"te lang", map[string]any{"text": strings.Repeat("a", 4097)}},
		{"onbekend veld", map[string]any{"text": "hoi", "stiekem": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/transcript", tc.body)
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", status, body)
			}
		})
	}
}

// De bevestigingsknop van "Ben je er nog?" is een gewone route.
func TestMeetingPresence_AcknowledgesAndKeepsSessionOpen(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)

	if evs := tickAndCollect(t, rig.clock, es.ch, 11*time.Minute); !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("presence_check ontbreekt: %v", eventTypes(evs))
	}
	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/presence", nil)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if evs := tickAndCollect(t, rig.clock, es.ch, 2*time.Minute); hasEvent(evs, EventSessionClosed) {
		t.Fatalf("na de bevestiging mag de sessie niet sluiten: %v", eventTypes(evs))
	}
}

// De knoppen die een mens indrukt zetten de inactiviteitsklok terug;
// wat de browser zelf meldt (afspeelstand, transcript) niet. Een client die
// opnieuw verbindt terwijl de vraag openstaat, krijgt hem met de resterende
// tijd terug op het state-event.
func TestMeetingPresence_OnlyControlRoutesCountAsActivity(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)
	base := "/v1/meeting/sessions/" + id

	tickAndCollect(t, rig.clock, es.ch, 9*time.Minute)
	if status, body := rig.do(t, http.MethodPost, base+"/stop", nil); status != http.StatusAccepted {
		t.Fatalf("stop: status = %d, body = %s", status, body)
	}
	if evs := tickAndCollect(t, rig.clock, es.ch, 9*time.Minute); hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("de stopknop hoort de klok terug te zetten: %v", eventTypes(evs))
	}
	// Een afspeelmelding en een getypt transcript zijn geen bediening.
	rig.do(t, http.MethodPost, base+"/playback", map[string]any{"turnId": "1", "state": "ended"})
	rig.do(t, http.MethodPost, base+"/transcript", map[string]any{"text": "zaalrumoer", "speaker": "zaal"})
	evs := tickAndCollect(t, rig.clock, es.ch, 2*time.Minute)
	if !hasEvent(evs, EventPresenceCheck) {
		t.Fatalf("afspeel- en transcriptmeldingen mogen de klok niet terugzetten: %v", eventTypes(evs))
	}

	// Herverbinden: het state-event draagt de vraag en de resterende tijd.
	rig.clock.skip(30 * time.Second)
	state := rig.events(t, id).recorded()[0] // events() wachtte al op dit statusframe
	if state.GraceMS != (90*time.Second).Milliseconds() || state.Message["nl"] == "" {
		t.Errorf("state na herverbinden: graceMs = %d, message = %+v; want 90000 en de vraag", state.GraceMS, state.Message)
	}
}

// Elke bedieningsroute zet de klok terug, ook als de actie zelf
// geweigerd wordt (er zat iemand aan het scherm); wat de browser zelf stuurt
// (audio, transcript, afspeelmeldingen) niet.
func TestMeetingPresence_EveryControlRouteCountsAndNothingElse(t *testing.T) {
	cases := []struct {
		route  string
		body   any
		counts bool
	}{
		{"/respond", nil, true},
		{"/intervene", nil, true},
		{"/settings", map[string]any{}, true},
		{"/stop", nil, true},
		{"/resume", map[string]any{"turnId": "0"}, true},
		{"/candidate/speak", nil, true},
		{"/detector/retry", nil, true},
		{"/presence", nil, true},
		{"/transcript", map[string]any{"text": "zaalrumoer", "speaker": "zaal"}, false},
		{"/playback", map[string]any{"turnId": "1", "state": "ended"}, false},
		{"/audio", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			rig := newMeetingRig(t, nil, nil)
			id := rig.openSession(t, "individualist", "en")
			s := rig.session(t, id)
			opened := rig.clock.Now()
			rig.clock.skip(9 * time.Minute)
			if tc.route == "/audio" {
				req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+"/v1/meeting/sessions/"+id+"/audio", strings.NewReader("\x00\x00"))
				req.Header.Set("Content-Type", "application/octet-stream")
				if resp, err := rig.srv.Client().Do(req); err == nil {
					resp.Body.Close()
				}
			} else {
				rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+tc.route, tc.body)
			}
			s.mu.Lock()
			last := s.lastActivity
			s.mu.Unlock()
			if counted := last.After(opened); counted != tc.counts {
				t.Errorf("%s telde als bediening: %v, want %v", tc.route, counted, tc.counts)
			}
		})
	}
}

func TestMeetingSession_EndClosesStreamAndSession(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	es := rig.events(t, id)

	status, body := rig.do(t, http.MethodDelete, "/v1/meeting/sessions/"+id, nil)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	closed := nextEvent(t, es.ch, EventSessionClosed)
	if closed.Reason != CloseReasonUser {
		t.Errorf("reden = %q, want de nette gebruikersafsluiting", closed.Reason)
	}
	if _, ok := <-es.ch; ok {
		t.Error("de SSE-stroom hoort te eindigen als de sessie sluit")
	}
	if rig.engine.count() != 0 {
		t.Errorf("sessiebeheer = %d, want leeg", rig.engine.count())
	}
	if status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/respond", nil); status != http.StatusNotFound {
		t.Errorf("een gesloten sessie bestaat niet meer (status %d)", status)
	}
}

// Racevenster: sluit de sessie tussen het opzoeken en het toevoegen, dan is
// een binnenkomend segment geen paniek maar een nette 404 — en het segment
// belandt nergens.
func TestMeetingTranscript_ClosedSessionIsRefused(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "en")
	s, ok := rig.engine.session(id, testUser.ID)
	if !ok {
		t.Fatal("sessie niet gevonden")
	}
	s.close(CloseReasonUser)
	// De sessie is dicht maar nog vindbaar: precies het racevenster.
	rig.engine.mu.Lock()
	rig.engine.sessions[id] = s
	rig.engine.mu.Unlock()

	status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/transcript", map[string]any{"text": "te laat"})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", status, body)
	}
	if !strings.Contains(body, "session_closed") {
		t.Errorf("body = %s", body)
	}
	if s.segmentCount() != 0 {
		t.Errorf("een gesloten sessie mag niets meer opnemen (%d segmenten)", s.segmentCount())
	}
}

// Valt de client weg terwijl de server schrijft, dan eindigt de SSE-stroom
// zonder de sessie mee te slepen: de vergadering luistert door.
func TestMeetingEvents_WriteFailure_EndsStreamNotSession(t *testing.T) {
	m, _ := newTestModule(t, nil)
	e := m.newMeetingEngine()
	m.meetings = e
	t.Cleanup(e.shutdown)
	s, err := e.open(testUser.ID, "individualist", "en")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	w := newFailWriter(0)
	serveTo(t, m, w, httptest.NewRequest(http.MethodGet, "/v1/meeting/sessions/"+s.id+"/events", nil))
	if w.bodyString() != "" {
		t.Errorf("er kwam toch iets door: %q", w.bodyString())
	}
	if s.isClosed() {
		t.Error("een weggevallen kijker mag de vergadersessie niet sluiten")
	}
}

// unflushableWriter is een ResponseWriter zonder Flush: precies wat een SSE-
// stroom niet kan gebruiken.
type unflushableWriter struct {
	hdr    http.Header
	status int
	body   strings.Builder
}

func (w *unflushableWriter) Header() http.Header {
	if w.hdr == nil {
		w.hdr = http.Header{}
	}
	return w.hdr
}

func (w *unflushableWriter) WriteHeader(status int) { w.status = status }

func (w *unflushableWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

// Een verbinding die niet doorgespoeld kan worden is geen stroom: dan stopt de
// route meteen in plaats van stilletjes te bufferen — en de sessie loopt door.
func TestMeetingEvents_WithoutFlushSupport_EndsStreamNotSession(t *testing.T) {
	m, _ := newTestModule(t, nil)
	e := m.newMeetingEngine()
	m.meetings = e
	t.Cleanup(e.shutdown)
	s, err := e.open(testUser.ID, "individualist", "en")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	w := &unflushableWriter{}
	serveTo(t, m, w, httptest.NewRequest(http.MethodGet, "/v1/meeting/sessions/"+s.id+"/events", nil))
	if w.status != http.StatusOK {
		t.Errorf("status = %d, want 200 (de kop was al verstuurd)", w.status)
	}
	if w.body.Len() != 0 {
		t.Errorf("zonder flush hoort er geen frame te vertrekken: %q", w.body.String())
	}
	if s.isClosed() {
		t.Error("een onbruikbare verbinding mag de vergadersessie niet sluiten")
	}
}

// Een vergadering die niets over zelfstandig meedoen zegt, krijgt de stand van
// de duif die aan tafel gaat. De signalerende duif doet vanzelf mee;
// de twee pratende duiven wachten op een uitnodiging. De sessie-DTO draagt die
// stand, zodat de frontend hem niet hoeft te raden.
func TestMeetingOpen_AutonomyDefaultFollowsThePigeon(t *testing.T) {
	for pigeon, want := range map[string]bool{"biological": false, "individualist": false, "nonverbal": true} {
		t.Run(pigeon, func(t *testing.T) {
			rig := newMeetingRig(t, nil, nil)
			status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{"pigeonId": pigeon, "lang": "nl"})
			if status != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", status, body)
			}
			var out struct {
				Settings   SessionSettings `json:"settings"`
				Autonomous bool            `json:"autonomous"`
			}
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				t.Fatalf("body = %s (%v)", body, err)
			}
			if out.Settings.Autonomous != want || out.Autonomous != want {
				t.Errorf("duif %q: autonomous = %v (settings) / %v (los veld), want %v", pigeon, out.Settings.Autonomous, out.Autonomous, want)
			}
		})
	}
}

// De vergadering blijft de baas over haar eigen stand: een expliciete keuze in
// het verzoek gaat boven de standaard van de duif, in beide richtingen.
func TestMeetingOpen_ExplicitAutonomyOverridesThePigeonDefault(t *testing.T) {
	for _, tc := range []struct {
		pigeon string
		want   bool
	}{{"nonverbal", false}, {"biological", true}} {
		t.Run(tc.pigeon, func(t *testing.T) {
			rig := newMeetingRig(t, nil, nil)
			status, body := rig.do(t, http.MethodPost, "/v1/meeting/sessions", map[string]any{
				"pigeonId": tc.pigeon, "lang": "nl", "autonomous": tc.want,
			})
			if status != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", status, body)
			}
			var out struct {
				Settings SessionSettings `json:"settings"`
			}
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				t.Fatalf("body = %s (%v)", body, err)
			}
			if out.Settings.Autonomous != tc.want {
				t.Errorf("duif %q: autonomous = %v, want %v", tc.pigeon, out.Settings.Autonomous, tc.want)
			}
		})
	}
}
