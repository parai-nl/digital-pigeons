package duif

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"
)

// Het tijdcontract van de kandidaatgenerator: wanneer er geëvalueerd wordt,
// wanneer een klaarliggende bijdrage mag klinken, en wat er níét meer genoeg is
// om haar af te breken.

// Een gecommitte zin met genoeg woorden levert binnen de cadans één evaluatie.
func TestMeetingTiming_CommitsEvaluateWithinTheCadence(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want int
	}{
		{"korte zin boven de woordondergrens", "Weg met die duiven.", 1},
		{"volle zin", "We stellen voor om de duiven van dit plein weg te vangen.", 1},
		{"te kort voor een evaluatie", "Ja.", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCandidateLLM(pass())
			rig := eagerRig(t, svc, nil)
			id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true})
			d := rig.candidates(t, id)
			rig.post(t, id, "/transcript", map[string]any{"text": tc.text})
			rig.clock.advance(t, time.Second)
			if tc.want > 0 {
				waitForEvaluations(t, d, tc.want)
			}
			if got := d.counters().Started; got != tc.want {
				t.Errorf("gestarte evaluaties = %d, wil %d", got, tc.want)
			}
		})
	}
}

// Partials bewaren geen tekst en breken geen lopende bijdrage meer af — één
// kuchje mocht nooit een begonnen beurt doden. Ze houden haar ook niet meer
// tegen: dat iemand nog aan het woord is, is geen reden om
// te zwijgen. De duif breekt in.
func TestMeetingTiming_PartialsNeverHoldHerBackAndNeverAbortAContribution(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Een bijdrage [1]."))
	rig := eagerRig(t, svc, func(c *detectConfig) { c.Quiet = 1500 * time.Millisecond })
	// Vijf seconden rust, alleen zodat de kandidaat blijft liggen tot de
	// partial loopt; daarna is er niets meer wat haar tegenhoudt.
	id, s := openHolding(t, rig, 5*time.Second, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat hier over duivenrechten."})
	waitForCandidate(t, s)

	// Er loopt een onafgemaakte uiting en er is geen stilte geweest: precies
	// de stand die haar vroeger het zwijgen oplegde.
	s.publishPartial("eh, wacht even", "")
	rig.clock.advance(t, 6*time.Second)
	nextEvent(t, es.ch, EventPersona)

	// En een partial midden in haar beurt breekt haar niet af.
	s.publishPartial("kuch", "")
	rig.clock.advance(t, 250*time.Millisecond)
	for _, ev := range es.recorded() {
		if ev.Type == EventStopped {
			t.Fatalf("een partial brak de bijdrage af: %+v", ev)
		}
	}
	if strings.Contains(rig.logs(), "wacht even") || strings.Contains(rig.logs(), "kuch") {
		t.Error("een partial belandde in het log (streaming-only)")
	}
}

// Wat een nog onhoorbare bijdrage wél vervangt is één écht gecommit menselijk
// segment: het gesprek is dan verder, en de duif antwoordt op iets van net.
func TestMeetingTiming_NewCommittedSpeechSupersedesAnUnheardContribution(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := eagerRig(t, svc, nil)
	rig.engine.voice = nil // tekst-only: er komt geen audioframe, dus de beurt is nog onhoorbaar
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager", "playbackTracking": true,
	})
	s := rig.session(t, id)
	es := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat hier over duivenrechten."})
	nextEvent(t, es.ch, EventTranscript)

	s.mu.Lock()
	s.speaking, s.turnAutomatic, s.turnTriggerSeq = true, true, s.lastHumanSeq
	s.mu.Unlock()
	rig.post(t, id, "/transcript", map[string]any{"text": "Nee, laat maar, volgend agendapunt."})
	if ev := nextEvent(t, es.ch, EventStopped); ev.Reason != "context_changed" {
		t.Fatalf("reden = %q, wil context_changed", ev.Reason)
	}
	if s.isSpeaking() {
		t.Error("het spreekslot bleef bezet na een vervangen aanleiding")
	}
}

// Het venster kijkt terug op leeftijd en respecteert het woordplafond.
func TestMeetingTiming_ContextByAgeAndHardWordCap(t *testing.T) {
	cfg := detectConfig{ContextAge: 45 * time.Second, ContextSegments: 40, MaxWindowWords: 10}
	base := meetingEpoch
	r := NewRollingTranscript(40, time.Hour)
	r.Append(TranscriptSegment{Seq: 1, Text: "dit is oude context van lang geleden", At: base})
	r.Append(TranscriptSegment{Seq: 2, Text: "dit is recente context", At: base.Add(50 * time.Second)})
	r.Append(TranscriptSegment{Seq: 3, Text: "dit is nieuw", At: base.Add(59 * time.Second)})

	win := r.DetectionSnapshot(base.Add(60*time.Second), 2, cfg)
	if win.Fresh != 1 {
		t.Fatalf("nieuw = %d, wil 1", win.Fresh)
	}
	for _, s := range win.Segments {
		if s.Seq == 1 {
			t.Error("context ouder dan de leeftijdsgrens hoort niet in het venster")
		}
	}
	if win.Words > cfg.MaxWindowWords {
		t.Errorf("woorden = %d, wil hoogstens %d", win.Words, cfg.MaxWindowWords)
	}
}

// Streaming-only: geen transcript, geen kandidaat en geen kopregel in het log
// of in de diagnostiek.
func TestMeetingTiming_StreamingOnly_LeavesNoTraceInLogsOrDiagnostics(t *testing.T) {
	secret := "de geheime zin over ratten met vleugels"
	svc := newCandidateLLM(speak(0.95, "pest-framing", "Een geheime bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, 2*time.Second, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "eagerness": "eager",
	})
	rig.post(t, id, "/transcript", map[string]any{"text": secret})
	waitForCandidate(t, s)
	rig.clock.advance(t, 3*time.Second)

	if got := aboveDebug(rig.logs()); strings.Contains(got, secret) || strings.Contains(got, "geheime bijdrage") {
		t.Error("transcript of bijdrage staat in het log")
	}
	status, body := rig.do(t, "GET", "/v1/meeting/sessions/"+id+"/diagnostics", nil)
	if status != 200 {
		t.Fatalf("diagnostiek: status = %d", status)
	}
	for _, forbidden := range []string{secret, "geheime bijdrage", "ratten"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("de diagnostiek draagt inhoud: %s", forbidden)
		}
	}
	var diag struct {
		Parameters map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(body), &diag); err != nil {
		t.Fatalf("diagnostiek parsen: %v", err)
	}
	if _, ok := diag.Parameters["speakBar"]; !ok {
		t.Error("de diagnostiek meldt de spreeklat van deze duif niet")
	}
}

// De spreeklat verschilt per duif, en dat verschil staat in de configuratie —
// niet in een gedeelde regel in code.
func TestSpeakBar_DiffersPerPigeon(t *testing.T) {
	set := testPersonas(t)
	want := map[string]struct {
		threshold      float64
		cooldown       time.Duration
		topicCooldown  time.Duration
		polite         bool
		withoutSources bool
	}{
		// Beleefdheid staat bij alle drie uit: niemand wacht nog op een
		// stilte, het verschil zit in de lat, de rust en de bronplicht.
		"biological":    {0.55, 45 * time.Second, 180 * time.Second, false, false},
		"individualist": {0.45, 45 * time.Second, 120 * time.Second, false, true},
		"nonverbal":     {0.30, 20 * time.Second, 0, false, true},
	}
	for id, w := range want {
		p := mustGet(t, set, id)
		bar := p.Spreeklat
		if bar == nil {
			t.Fatalf("%s heeft geen spreeklat", id)
		}
		if bar.Drempel != w.threshold {
			t.Errorf("%s: drempel = %.2f, wil %.2f", id, bar.Drempel, w.threshold)
		}
		if bar.Cooldown(1) != w.cooldown {
			t.Errorf("%s: cooldown = %s, wil %s", id, bar.Cooldown(1), w.cooldown)
		}
		if bar.TopicCooldown() != w.topicCooldown {
			t.Errorf("%s: themacooldown = %s, wil %s", id, bar.TopicCooldown(), w.topicCooldown)
		}
		if bar.Polite() != w.polite {
			t.Errorf("%s: beleefdheid = %v, wil %v", id, bar.Polite(), w.polite)
		}
		if bar.WithoutSources() != w.withoutSources {
			t.Errorf("%s: zonder bron = %v, wil %v", id, bar.WithoutSources(), w.withoutSources)
		}
		for _, lang := range []string{"en", "nl"} {
			text := bar.Instruction(lang)
			for _, eis := range []string{"eis:M1", "eis:M2", "eis:M3", "eis:M4"} {
				if !strings.Contains(text, eis) {
					t.Errorf("%s (%s): spreeklat mist sentinel %s", id, lang, eis)
				}
			}
		}
	}
	// De signalerende duif heeft de laagste lat van de drie.
	low := mustGet(t, set, "nonverbal").Spreeklat.Drempel
	for _, id := range []string{"biological", "individualist"} {
		if mustGet(t, set, id).Spreeklat.Drempel <= low {
			t.Errorf("%s heeft geen hogere lat dan de signalerende duif", id)
		}
	}
}

// De kennispartner is kennispartner, geen adviseur in het duivenvraagstuk. Een
// proefvergadering kreeg van haar één bijdrage, en die adviseerde
// weringsmaatregelen: wat de vergadering met duiven kan doen, in plaats van wat
// het besluit met een duif doet. Haar spreeklat moet dat verschil benoemen, in
// beide talen.
func TestSpeakBar_Biological_AsksWhatTheDecisionDoesToAPigeon_NotWhatToDoAboutPigeons(t *testing.T) {
	set := testPersonas(t)
	bar := mustGet(t, set, "biological").Spreeklat
	for _, tc := range []struct {
		lang string
		// De grens die zij niet over mag: geen aanbevolen maatregelen.
		refusal []string
		// En waar haar bijdrage wél over gaat: de duif die het ondergaat.
		framing []string
	}{
		{lang: "en", refusal: []string{"not an advisor", "do not recommend", "do not tell the table what works against pigeons"}, framing: []string{"does to a pigeon", "Say what that measure does to her", "leave the decision to the table"}},
		{lang: "nl", refusal: []string{"geen adviseur", "je beveelt niets aan", "vertelt de tafel niet wat wel of niet werkt tegen duiven"}, framing: []string{"met een duif doet", "Zeg wat hij met haar doet", "laat het besluit aan de tafel"}},
	} {
		// Zonder de regelafbreking: hoe de tekst afgebroken staat mag deze
		// toets niet uitmaken, wat er staat wel.
		text := strings.Join(strings.Fields(bar.Instruction(tc.lang)), " ")
		for _, want := range append(append([]string(nil), tc.refusal...), tc.framing...) {
			if !strings.Contains(text, want) {
				t.Errorf("[%s] de spreeklat mist %q:\n%s", tc.lang, want, text)
			}
		}
	}
}

// De oplopende cooldown is de rem die in de plaats kwam van het sessieplafond.
func TestSpeakBar_CooldownRisesWithEachContribution(t *testing.T) {
	bar := &SpeakBar{CooldownSeconden: 45, CooldownGroeiSeconden: 15, CooldownMaxSeconden: 120}
	for _, tc := range []struct {
		spoken int
		want   time.Duration
	}{{1, 45 * time.Second}, {2, 60 * time.Second}, {4, 90 * time.Second}, {10, 120 * time.Second}} {
		if got := bar.Cooldown(tc.spoken); got != tc.want {
			t.Errorf("cooldown na %d bijdragen = %s, wil %s", tc.spoken, got, tc.want)
		}
	}
	fixed := &SpeakBar{CooldownSeconden: 20}
	if got := fixed.Cooldown(9); got != 20*time.Second {
		t.Errorf("zonder groei = %s, wil 20s", got)
	}
}

// De gretigheidsknop verschuift elke lat met dezelfde stap.
func TestSpeakBar_EagernessShiftsEveryBarEqually(t *testing.T) {
	bar := &SpeakBar{Drempel: 0.55}
	normal := bar.Threshold(EagernessNormal, 0.5)
	if normal != 0.55 {
		t.Fatalf("neutrale lat = %.2f, wil 0.55", normal)
	}
	if got := bar.Threshold(EagernessReserved, 0.5); got <= normal {
		t.Errorf("terughoudend = %.2f, wil hoger dan %.2f", got, normal)
	}
	if got := bar.Threshold(EagernessEager, 0.5); got >= normal {
		t.Errorf("gretig = %.2f, wil lager dan %.2f", got, normal)
	}
	// Een gevoelig thema haalt de lat omlaag, een ongevoelig omhoog.
	if bar.Threshold(EagernessNormal, 1.0) >= bar.Threshold(EagernessNormal, 0.1) {
		t.Error("de gevoeligheid van een thema verschuift de lat de verkeerde kant op")
	}
}

// --- de signaleringscatalogus (ongewijzigd van opzet) ------------------------

func TestSignalingCatalogue_MissingBasisDisablesAutoButManualWorks(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := set.Get("nonverbal")
	p.signalResearchReady = false
	if err := loadSignalCatalogue(fstest.MapFS{}, p); err != nil {
		t.Fatal(err)
	}
	if p.signalResearchReady {
		t.Fatal("missing evidence marked available")
	}
	rig := newMeetingRig(t, func(m *Module) { m.personas = set }, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "autonomous": true})
	d := rig.candidates(t, id)
	if st := d.status(); st.Enabled || st.Retryable || st.Reason != reasonSignalResearchUnavailable {
		t.Fatalf("status=%+v", st)
	}
	if d.retryNow() != reasonSignalResearchUnavailable {
		t.Fatal("retry bypassed missing research")
	}
	if status, body := rig.intervene(t, id); status != 202 {
		t.Fatalf("manual signal=%d %s", status, body)
	}
}

func TestSignalingCatalogue_AllActiveThemesHaveGroundedBoundedRelations(t *testing.T) {
	sub, _ := fs.Sub(configFS, "config")
	raw, _ := fs.ReadFile(sub, "signaling-catalogue.v1.yaml")
	set, _ := LoadEmbeddedPersonas()
	p, _ := set.Get("nonverbal")
	if !p.signalResearchReady || len(p.Topics) != 4 {
		t.Fatal("catalogue not active")
	}
	// Changing a grounded flag or leaving a theme uncovered must fail closed.
	for _, bad := range []string{
		strings.Replace(string(raw), "grounded: true", "grounded: false", 1),
		strings.Replace(string(raw), "topic_id: food", "topic_id: invented", 1),
	} {
		if err := loadSignalCatalogue(fstest.MapFS{p.SignalCatalogue: &fstest.MapFile{Data: []byte(bad)}}, p); err == nil {
			t.Fatal("bad catalogue accepted")
		}
	}
}

// The semantic set is a review instrument, not a fake provider evaluation.
func TestSignalingCatalogue_EvaluationSetIsVersionedAndReviewable(t *testing.T) {
	raw, err := os.ReadFile("eval/signaling-eval.v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Version, Catalogue, Status string
		Cases                      []struct{ ID, Lang, Topic, Fresh, Context, Expected string }
	}
	if err := yaml.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	if set.Version == "" || set.Catalogue != "signaling-2026-09-07.1" || set.Status != "designed-expectations-not-live-model-results" || len(set.Cases) < 24 {
		t.Fatal("evaluation provenance missing")
	}
	personas, _ := LoadEmbeddedPersonas()
	p, _ := personas.Get("nonverbal")
	seen := map[string]bool{}
	for _, c := range set.Cases {
		if seen[c.ID] || c.ID == "" || c.Fresh == "" || (c.Lang != "en" && c.Lang != "nl") {
			t.Fatalf("unusable case=%s", c.ID)
		}
		seen[c.ID] = true
		if _, ok := p.Topic(c.Topic); !ok {
			t.Fatalf("case uses inactive topic=%s", c.Topic)
		}
		switch c.Expected {
		case "absent", "incidental", "partial", "substantive":
		default:
			t.Fatalf("unknown rubric=%s", c.Expected)
		}
	}
}

// De gespreksfixtures zijn een herzienbaar instrument met eigen herkomst, geen
// gemeten modeluitvoer.
func TestMeetingConversations_FixtureSetIsVersionedAndReviewable(t *testing.T) {
	doc := loadConversationFixtures(t)
	if !strings.Contains(doc.Status, "synthetic-project-written-fixtures") {
		t.Errorf("herkomst ontbreekt: %q", doc.Status)
	}
	set := testPersonas(t)
	seen := map[string]bool{}
	for _, conv := range doc.Conversations {
		key := conv.ID + "/" + conv.Lang
		if seen[key] {
			t.Fatalf("dubbel gesprek %s", key)
		}
		seen[key] = true
		if conv.Lang != "en" && conv.Lang != "nl" {
			t.Fatalf("%s: onbekende taal", key)
		}
		if len(conv.Segments) < 8 {
			t.Errorf("%s: %d segmenten, wil er minstens acht", key, len(conv.Segments))
		}
		if last := conv.Segments[len(conv.Segments)-1].AtMS; last < 150_000 {
			t.Errorf("%s: duurt %d ms, wil ongeveer drie minuten", key, last)
		}
		for _, seg := range conv.Segments {
			if strings.TrimSpace(seg.Text) == "" {
				t.Errorf("%s: leeg segment", key)
			}
			if seg.Topic == "" {
				continue
			}
			known := false
			for _, pigeon := range []string{"biological", "individualist", "nonverbal"} {
				if _, ok := mustGet(t, set, pigeon).Topic(seg.Topic); ok {
					known = true
				}
			}
			if !known {
				t.Errorf("%s: onbekend thema %q", key, seg.Topic)
			}
		}
	}
	// Beide talen bestaan voor elk gesprek.
	for _, id := range []string{"fireworks", "vermin", "tangential", "control", "ambient", "budget"} {
		if !seen[id+"/en"] || !seen[id+"/nl"] {
			t.Errorf("gesprek %s bestaat niet in beide talen", id)
		}
	}
}

// De kandidaatprompt gaat elke cadans de deur uit, dus zijn omvang ís de
// kostenpost van deze modus. Deze test meet hem en houdt hem binnen een
// plafond; de gemeten bytes staan in de testuitvoer.
func TestCandidatePrompt_StaysWithinACostBudget(t *testing.T) {
	set := testPersonas(t)
	// Een vol venster: het woordplafond van de generator, in segmenten van
	// twaalf woorden.
	var segments []TranscriptSegment
	for i := range 25 {
		segments = append(segments, TranscriptSegment{
			Seq: uint64(i + 1), At: meetingEpoch,
			Text: "dit is een gewone vergaderzin van precies twaalf woorden lang zonder meer",
		})
	}
	win := transcriptWindow{Segments: segments, Fresh: 2, Words: 300}
	own := []TranscriptSegment{
		{Seq: 26, Text: "Een eerdere bijdrage van de duif in deze vergadering, ongeveer zo lang als ze werkelijk zijn."},
		{Seq: 27, Text: "En nog een eerdere bijdrage van de duif, met dezelfde orde van grootte aan tekst erin."},
	}
	const maxPromptBytes = 24 * 1024
	for _, id := range []string{"biological", "individualist", "nonverbal"} {
		p := mustGet(t, set, id)
		for _, lang := range []string{"en", "nl"} {
			system := buildCandidatePrompt(candidatePromptInput{Persona: p, Lang: lang, Status: KBPersonaOnly})
			user := renderCandidateWindow(lang, "tok-timing", id, win, own, p.SignalsOnly())
			total := len(system) + len(user)
			t.Logf("kandidaatprompt %s/%s: systeem %d bytes, venster %d bytes, samen %d bytes",
				id, lang, len(system), len(user), total)
			if total > maxPromptBytes {
				t.Errorf("%s/%s: %d bytes, wil hoogstens %d", id, lang, total, maxPromptBytes)
			}
		}
	}
}
