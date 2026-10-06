package duif

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// --- de non-verbale duif -----------------------------------------------------

// intervene drukt op de handmatige "de duif grijpt in"-knop.
func (rig *meetingRig) intervene(t *testing.T, sessionID string) (int, string) {
	t.Helper()
	return rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+sessionID+"/intervene", nil)
}

// nonverbalRig zet de non-verbale duif aan tafel met een cadans die op één zin
// afgaat en een voorspelbare geluidskeuze.
func nonverbalRig(t *testing.T, svc *candidateLLM, tune func(e *meetingEngine)) *meetingRig {
	t.Helper()
	return newMeetingRig(t, func(m *Module) {
		if svc != nil {
			m.llm = svc
		}
	}, func(e *meetingEngine) {
		e.cfg.Detect.MinWords = 3
		e.cfg.Detect.Interval = 0
		e.cfg.Detect.Quiet = 0
		e.pickIndex = func(int) int { return 0 }
		if tune != nil {
			tune(e)
		}
	})
}

func TestSoundBand_FollowsArousal(t *testing.T) {
	tests := []struct {
		arousal float64
		want    SoundBand
	}{
		{-1, BandCalm}, {0, BandCalm}, {0.2, BandCalm},
		{0.34, BandInsistent}, {0.5, BandInsistent},
		{0.67, BandAlarm}, {1, BandAlarm}, {2, BandAlarm},
	}
	for _, tc := range tests {
		if got := bandForArousal(tc.arousal); got != tc.want {
			t.Errorf("bandForArousal(%v) = %q, want %q", tc.arousal, got, tc.want)
		}
	}
}

func TestPickSound_StaysInBand_AndFallsBackWhenEmpty(t *testing.T) {
	set := []Sound{
		{ID: "kalm-1", Band: BandCalm}, {ID: "kalm-2", Band: BandCalm},
		{ID: "alarm-1", Band: BandAlarm},
	}
	tests := []struct {
		name    string
		sounds  []Sound
		arousal float64
		pick    func(int) int
		want    string
		wantOK  bool
	}{
		{"kalm, eerste", set, 0.1, func(int) int { return 0 }, "kalm-1", true},
		{"kalm, tweede — de keuze is seedbaar", set, 0.1, func(int) int { return 1 }, "kalm-2", true},
		{"alarm", set, 0.9, func(int) int { return 0 }, "alarm-1", true},
		{"lege band valt terug op een naburige", set, 0.5, func(int) int { return 0 }, "kalm-1", true},
		{"zonder geluiden geen keuze", nil, 0.5, func(int) int { return 0 }, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pickSound(tc.sounds, tc.arousal, tc.pick)
			if ok != tc.wantOK || got.ID != tc.want {
				t.Fatalf("pickSound = %q/%v, want %q/%v", got.ID, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// Een doorgelaten hit bij de non-verbale duif is een geluid, geen zin: geen
// LLM-pipeline, geen TTS — een mp3 uit de band van de actuele arousal.
func TestNonverbalPigeon_Candidate_PlaysSoundWithoutExplainingItsMeaning(t *testing.T) {
	svc := newCandidateLLM(speak(0.95, "threat-and-removal", ""))
	rig := nonverbalRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "nonverbal", "lang": "nl", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)

	rig.say(t, id, "we vangen ze gewoon weg")

	sound := nextEvent(t, es.ch, EventSound)
	if sound.Sound == nil || sound.Sound.Band != BandAlarm || sound.Sound.File == "" {
		t.Fatalf("de hoge interne selectiewaarde hoort de bestaande variatieband te gebruiken: %+v", sound.Sound)
	}
	if sound.Sound.URL == "" || !strings.Contains(sound.Sound.URL, sound.Sound.ID) {
		t.Errorf("het geluid hoort af te spelen zijn: %+v", sound.Sound)
	}

	// Geen spreekbeurt: deze duif praat niet.
	for _, ev := range es.recorded() {
		if ev.Type == EventText || ev.Type == EventPersona || ev.Type == EventTTSAudio || ev.Type == EventArousal || ev.Topic != nil {
			t.Fatalf("de non-verbale duif hoort niet te spreken, kreeg %q", ev.Type)
		}
		if ev.Candidate != nil && (ev.Candidate.Topic != nil || ev.Candidate.Score != 0) {
			t.Fatalf("de kandidaat van de non-verbale duif draagt betekenis: %+v", ev.Candidate)
		}
	}
}

// De handmatige knop is voor deze duif wat de spreekknop is voor de pratende
// duiven: hij werkt altijd, ook met een uitgeschakelde detector.
func TestNonverbalPigeon_ManualIntervene_WorksWithTheDetectorOff(t *testing.T) {
	rig := nonverbalRig(t, nil, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "lang": "nl"})
	es := rig.events(t, id)
	rig.candidates(t, id).disable(ReasonBudgetExceeded)
	nextEvent(t, es.ch, EventDetectorStatus)

	status, body := rig.intervene(t, id)
	if status != http.StatusAccepted {
		t.Fatalf("de handmatige knop: status = %d, body = %s", status, body)
	}
	var out struct {
		Sound   SoundRef `json:"sound"`
		Arousal float64  `json:"arousal"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("antwoord %s: %v", body, err)
	}
	if out.Sound.ID == "" || out.Arousal < manualArousalImpulse {
		t.Fatalf("de knop hoort een geluid te spelen en de meter een zet te geven: %+v", out)
	}

	sound := nextEvent(t, es.ch, EventSound)
	if sound.Sound == nil || sound.Sound.ID != out.Sound.ID {
		t.Fatalf("het kanaal hoort hetzelfde geluid te melden: %+v", sound.Sound)
	}
	// De knop leest de meter zoals hij stond en zet hem daarna pas. Op een verse
	// sessie staat die op nul, dus dit is de route waarlangs de rustige band
	// hoorbaar wordt: de kandidaatroute komt er niet aan toe, want een score die
	// de spreeklat haalt ligt al boven de rustige bovengrens (meting in
	// TestMeetingReplay_SignalBandSpread).
	if sound.Sound.Band != BandCalm {
		t.Errorf("de knop op een verse meter hoort uit de rustige band te kiezen, kreeg %q", sound.Sound.Band)
	}
}

// Voor een pratende duif is de knop de spreekknop; deze bestaat daar niet.
func TestManualIntervene_OnASpeakingPigeon_IsRejected(t *testing.T) {
	rig := nonverbalRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")
	status, body := rig.intervene(t, id)
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid_request") {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

// De meter is sessiestand, geen detectorstand: hij zakt op de klok, ook als de
// detector helemaal niet meedraait (leaky integrator).
func TestMeetingArousal_DecaysOnTheClock_WithoutADetector(t *testing.T) {
	rig := nonverbalRig(t, nil, func(e *meetingEngine) {
		e.cfg.Detect.Enabled = false
		e.cfg.Detect.ArousalHalfLife = 2 * time.Minute
	})
	id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "lang": "nl"})
	es := rig.events(t, id)
	s := rig.session(t, id)
	if s.candidates != nil {
		t.Fatal("deze sessie hoort zonder kandidaatgenerator te draaien")
	}

	rig.intervene(t, id)
	first := s.arousal.level()
	if first < manualArousalImpulse {
		t.Fatal("manual selection impulse missing")
	}
	evs := tickAndCollect(t, rig.clock, es.ch, 2*time.Minute)
	for _, ev := range evs {
		if ev.Type == EventArousal {
			t.Fatal("internal selection value leaked to signal stage")
		}
	}
	decayed := s.arousal.level()
	if want := first / 2; decayed < want-.05 || decayed > want+.05 {
		t.Fatalf("selection decay=%v, want %v", decayed, want)
	}

}

// De geluidenset is echte, opvraagbare audio — placeholders tot de opnamedag,
// maar wel met een werkende route.
func TestSoundRoute_ServesTheRecordings(t *testing.T) {
	m, _ := newTestModule(t, nil)
	req, err := http.NewRequest(http.MethodGet, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, m, req, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, want audio/mpeg", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("de route leverde een leeg geluid")
	}
	// Achter de login: cachen mag, maar alleen in de browser van de luisteraar.
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Errorf("Cache-Control = %q, want private, max-age=3600", cc)
	}

	missing, err := http.NewRequest(http.MethodGet, "/v1/sounds/knalgele-fanfare", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := serve(t, m, missing, false); got.Code != http.StatusNotFound {
		t.Fatalf("onbekend geluid: status = %d", got.Code)
	}
	anon, err := http.NewRequest(http.MethodGet, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := serve(t, m, anon, true); got.Code != http.StatusUnauthorized {
		t.Fatalf("anoniem: status = %d", got.Code)
	}

	// Een luisteraar die middenin wegloopt is geen incident: de route stopt met
	// streamen en laat de rest van de module ongemoeid.
	weggelopen, err := http.NewRequest(http.MethodGet, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := newFailWriter(0)
	serveTo(t, m, w, weggelopen)
	if w.bodyString() != "" {
		t.Errorf("er kwam toch geluid door: %q", w.bodyString())
	}
	if w.status != http.StatusOK {
		t.Errorf("de kop was al verstuurd voordat het schrijven brak: status = %d", w.status)
	}
}

// unseekableFS levert bestanden zonder Seek: niet elke fs.FS is een
// bestandssysteem. Dan kan ServeContent zijn werk niet doen en valt de route
// terug op gewoon doorstromen.
type unseekableFS struct{ fs.FS }

func (u unseekableFS) Open(name string) (fs.File, error) {
	f, err := u.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return unseekableFile{f}, nil
}

type unseekableFile struct{ fs.File }

func TestSoundRoute_NonSeekableSource_StreamsAnyway(t *testing.T) {
	m, _ := newTestModule(t, nil)
	m.personas.soundFS = unseekableFS{m.personas.soundFS}

	req, err := http.NewRequest(http.MethodGet, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, m, req, false)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("status = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, want audio/mpeg", ct)
	}
	if ar := rec.Header().Get("Accept-Ranges"); ar != "" {
		t.Errorf("zonder Seek valt er geen Range te beloven, kreeg %q", ar)
	}

	// En ook op dit pad is een weggelopen luisteraar geen incident.
	weggelopen, err := http.NewRequest(http.MethodGet, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := newFailWriter(0)
	serveTo(t, m, w, weggelopen)
	if w.bodyString() != "" {
		t.Errorf("er kwam toch geluid door: %q", w.bodyString())
	}
}

// De veldopnames zijn statische bestanden, geen stroom: een audiospeler mag
// een stuk opvragen (Range) en de kop los aftasten (HEAD).
func TestSoundRoute_SupportsRangeAndHead(t *testing.T) {
	m, _ := newTestModule(t, nil)

	ranged, err := http.NewRequest(http.MethodGet, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	ranged.Header.Set("Range", "bytes=0-9")
	rec := serve(t, m, ranged, false)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if rec.Body.Len() != 10 {
		t.Errorf("Range-body = %d bytes, want 10", rec.Body.Len())
	}
	if cr := rec.Header().Get("Content-Range"); !strings.HasPrefix(cr, "bytes 0-9/") {
		t.Errorf("Content-Range = %q", cr)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, want audio/mpeg", ct)
	}

	head, err := http.NewRequest(http.MethodHead, "/v1/sounds/coo-soft", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := serve(t, m, head, false)
	if got.Code != http.StatusOK {
		t.Fatalf("HEAD-status = %d, want 200", got.Code)
	}
	if got.Body.Len() != 0 {
		t.Errorf("een HEAD hoort geen body te hebben (%d bytes)", got.Body.Len())
	}
	if cl := got.Header().Get("Content-Length"); cl == "" || cl == "0" {
		t.Errorf("Content-Length = %q, want de lengte van de opname", cl)
	}
}

// De geluidenset hoort bij de non-verbale duif in de meegeleverde config, en
// elke band is bezet — anders valt de bandkeuze altijd terug.
func TestEmbeddedConfig_NonverbalHasSoundsInEveryBand(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	p, ok := set.Get("nonverbal")
	if !ok {
		t.Fatal("de non-verbale duif ontbreekt")
	}
	perBand := map[SoundBand]int{}
	for _, s := range p.Geluiden {
		perBand[s.Band]++
		f, err := set.OpenSound(s.ID)
		if err != nil {
			t.Fatalf("geluid %q is niet te openen: %v", s.ID, err)
		}
		f.Close()
	}
	for _, band := range soundBands {
		if perBand[band] == 0 {
			t.Errorf("band %q heeft geen enkel geluid", band)
		}
	}
	// De pratende duiven hebben er geen: hun stem is TTS.
	for _, id := range []string{"biological", "individualist"} {
		if p, _ := set.Get(id); len(p.Geluiden) != 0 {
			t.Errorf("%s heeft een geluidenset; die duif praat juist", id)
		}
	}
}

// Fail-fast op een kapotte geluidenset: een non-verbale vergaderduif zonder
// geluid kan nooit iets doen, en dat is een configuratiefout.
func TestLoadPersonas_ValidatesTheSoundSet(t *testing.T) {
	withNonverbal := func(sounds string) fstest.MapFS {
		return minimalConfig(func(m fstest.MapFS) {
			doc := string(m["personas.yaml"].Data)
			doc = strings.Replace(doc, "gedrag:", `  - id: nonverbal
    naam: { en: "The Non-verbal Pigeon", nl: "De non-verbale duif" }
    voice_id: ""
    prompts: { en: prompts/nonverbal.en.md, nl: prompts/nonverbal.nl.md }
    kennisbank: false
    chat: false
    vergadering: true
    spreeklat:
      prompts: { en: prompts/speakup.en.md, nl: prompts/speakup.nl.md }
      drempel: 0.30
      cooldown_seconden: 20
      beleefdheid: false
      zonder_bron: true
    topics:
      - id: threat
        label: { en: "Threat", nl: "Dreiging" }
        beschrijving: { en: "removal talk", nl: "praten over wegjagen" }
        gevoeligheid: 1.0
`+sounds+`
gedrag:`, 1)
			m["personas.yaml"] = &fstest.MapFile{Data: []byte(doc)}
			m["prompts/nonverbal.en.md"] = &fstest.MapFile{Data: []byte("nonverbal en")}
			m["prompts/nonverbal.nl.md"] = &fstest.MapFile{Data: []byte("nonverbal nl")}
			m["sounds/coo.mp3"] = &fstest.MapFile{Data: []byte("ID3-placeholder")}
		})
	}
	goodSet := `    geluiden:
      - { id: coo, bestand: sounds/coo.mp3, band: calm }
`
	tests := []struct {
		name   string
		sounds string
		want   string
	}{
		{"geldig", goodSet, ""},
		{"geen geluiden", "", "geen geluiden"},
		{"onbekende band", `    geluiden:
      - { id: coo, bestand: sounds/coo.mp3, band: hysterisch }
`, "band"},
		{"ontbrekend bestand", `    geluiden:
      - { id: coo, bestand: sounds/kwijt.mp3, band: calm }
`, "sounds/kwijt.mp3"},
		{"duplicaat-id", goodSet + `      - { id: coo, bestand: sounds/coo.mp3, band: alarm }
`, "duplicaat"},
		{"id niet URL-safe", `    geluiden:
      - { id: "Coo Zacht", bestand: sounds/coo.mp3, band: calm }
`, "URL-safe"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadPersonas(withNonverbal(tc.sounds))
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("geldige config werd geweigerd: %v", err)
			case tc.want == "":
			case err == nil:
				t.Fatalf("deze config hoorde te falen op %q", tc.want)
			case !strings.Contains(err.Error(), tc.want):
				t.Fatalf("foutmelding %q noemt %q niet", err, tc.want)
			}
		})
	}
}

// De trekking is willekeurig zolang een test hem niet vastzet, en blijft
// binnen de band ook als de trekker onzin teruggeeft.
func TestRandomIndex_StaysInRange(t *testing.T) {
	if got := randomIndex(0); got != 0 {
		t.Errorf("randomIndex(0) = %d, want 0", got)
	}
	if got := randomIndex(1); got != 0 {
		t.Errorf("randomIndex(1) = %d, want 0", got)
	}
	for range 50 {
		if got := randomIndex(3); got < 0 || got > 2 {
			t.Fatalf("randomIndex(3) = %d, buiten [0,2]", got)
		}
	}
	e, _ := newTestEngine(t, nil, nil)
	if got := e.pick(2); got < 0 || got > 1 {
		t.Fatalf("de standaardtrekking gaf %d, buiten [0,1]", got)
	}
}

func TestPickSound_SurvivesABadDrawAndAnUnknownBand(t *testing.T) {
	set := []Sound{{ID: "kalm-1", Band: BandCalm}, {ID: "kalm-2", Band: BandCalm}}
	if got, ok := pickSound(set, 0.1, func(int) int { return 99 }); !ok || got.ID != "kalm-1" {
		t.Fatalf("een trekking buiten de band hoort terug te vallen op het eerste geluid, kreeg %q/%v", got.ID, ok)
	}
	vreemd := []Sound{{ID: "onbekend", Band: SoundBand("fanfare")}}
	if got, ok := pickSound(vreemd, 0.5, func(int) int { return 0 }); ok {
		t.Fatalf("een geluid buiten elke band hoort niet gekozen te worden, kreeg %q", got.ID)
	}
}

// Een duif zonder bruikbare opnames zwijgt met een nette melding in plaats van
// een lege belofte.
func TestManualIntervene_WithoutRecordings_IsAnHonestUnavailable(t *testing.T) {
	rig := nonverbalRig(t, nil, func(e *meetingEngine) { e.cfg.Detect.Enabled = false })
	id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "lang": "nl"})
	s, _ := rig.engine.session(id, testUser.ID)
	// Een eigen persona-kopie zonder geluidenset: de config kan dit niet
	// opleveren (fail-fast), een halve deploy wel.
	stil := *s.persona
	stil.Geluiden = nil
	s.persona = &stil

	status, body := rig.intervene(t, id)
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "no_sound_available") {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

// Instellingen bijstellen op een gesloten sessie is een stille no-op, geen
// paniek en geen bericht op een dood kanaal.
func TestMeetingSettings_OnAClosedSession_IsSilent(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatal(err)
	}
	s.close(CloseReasonUser)
	on := true
	if got := s.applySettings(&on, EagernessEager, nil); !got.Autonomous || got.Eagerness != EagernessEager {
		t.Fatalf("de stand hoort ook na sluiting terug te komen: %+v", got)
	}
}
