package duif

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
	"gopkg.in/yaml.v3"
)

// De fixture-replay van het zelfstandig ingrijpen: vier gespreksverlopen van
// circa drie minuten, in EN en NL, afgespeeld op een gesimuleerde tijdlijn —
// per duif meten we hoe vaak en wanneer zij spreekt.
//
// Wat hier gemeten wordt is de klok, niet het model. De scores in
// eval/meeting-conversations.v1.yaml zijn ontworpen verwachtingen: het orakel
// hieronder geeft ze onveranderd terug als kopregel, zodat de drempels, de
// oplopende cooldown, de themacooldown en de stilte-eis meetbaar worden zonder
// dat een providerantwoord het resultaat kleurt. Wat het echte model doet, meet
// de live drempeltest.

type fixtureSegment struct {
	AtMS          int64   `yaml:"atMs"`
	Speaker       string  `yaml:"speaker"`
	Text          string  `yaml:"text"`
	Biological    float64 `yaml:"biological"`
	Individualist float64 `yaml:"individualist"`
	Nonverbal     float64 `yaml:"nonverbal"`
	Addressed     bool    `yaml:"addressed"`
	Topic         string  `yaml:"topic"`
}

func (f fixtureSegment) scoreFor(pigeon string) float64 {
	switch pigeon {
	case "biological":
		return f.Biological
	case "individualist":
		return f.Individualist
	default:
		return f.Nonverbal
	}
}

type fixtureConversation struct {
	ID       string           `yaml:"id"`
	Lang     string           `yaml:"lang"`
	Note     string           `yaml:"note"`
	Segments []fixtureSegment `yaml:"segments"`
}

type conversationFixtures struct {
	Version       string                `yaml:"version"`
	Status        string                `yaml:"status"`
	Conversations []fixtureConversation `yaml:"conversations"`
}

func loadConversationFixtures(t *testing.T) conversationFixtures {
	t.Helper()
	raw, err := os.ReadFile("eval/meeting-conversations.v1.yaml")
	if err != nil {
		t.Fatalf("gespreksfixtures lezen: %v", err)
	}
	var doc conversationFixtures
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("gespreksfixtures parsen: %v", err)
	}
	if doc.Version == "" || len(doc.Conversations) < 8 {
		t.Fatalf("fixtureset is onvolledig: versie %q, %d gesprekken", doc.Version, len(doc.Conversations))
	}
	return doc
}

// fixtureOracle is de gescripte kandidaatgenerator: hij zoekt op welk segment
// als laatste nieuw in de prompt staat en geeft de ontworpen score van dat
// segment terug. Geen trefwoorden, geen model — een tijdgestuurd orakel.
type fixtureOracle struct {
	conv   fixtureConversation
	pigeon string
	lang   string

	mu    sync.Mutex
	calls int
}

func (o *fixtureOracle) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{Content: "{}", FinishReason: "stop"}, nil
}

func (o *fixtureOracle) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if isMeetingQueryRequest(req) {
		// De queryherformulering voor de kennisopzoeking is geen evaluatie: zij
		// telt niet mee in callCount en krijgt haar eigen vaste regel.
		return fixedStream(testMeetingQueryLine), nil
	}
	body := "PASS\n"
	if isCandidateRequest(req) {
		o.mu.Lock()
		o.calls++
		o.mu.Unlock()
		// Dezelfde kop als de sessie stuurt: de signalerende duif krijgt haar
		// eigen vensterkoppen.
		head := candidateFreshHeader
		if o.pigeon == "nonverbal" {
			head = candidateSignalFreshHeader
		}
		fresh := req.Messages[1].Content
		if _, after, ok := strings.Cut(fresh, head[o.lang]); ok {
			fresh = after
		}
		for _, seg := range o.conv.Segments {
			if !strings.Contains(fresh, seg.Text) {
				continue
			}
			score := seg.scoreFor(o.pigeon)
			if score <= 0 {
				body = "PASS\n"
				continue
			}
			topic := seg.Topic
			if topic == "" {
				topic = defaultTopicFor(o.pigeon)
			}
			body = fmt.Sprintf("SPEAK score=%.2f topic=%s addressed=%t\nDit is wat de duif zou zeggen [1].",
				score, topic, seg.Addressed)
		}
	}
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Content: body, FinishReason: "stop"}
	close(out)
	return out, nil
}

func (o *fixtureOracle) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

func defaultTopicFor(pigeon string) string {
	switch pigeon {
	case "biological":
		return "urban-ecology"
	case "individualist":
		return "pigeon-rights"
	default:
		return "nesting"
	}
}

// replayResult is wat één afgespeeld gesprek per duif oplevert.
type replayResult struct {
	spokenAtMS []int64
	// addressed loopt gelijk op met spokenAtMS: per bijdrage of iemand de duif
	// bij die bijdrage rechtstreeks aansprak.
	addressed []bool
	// bands zijn de banden van de geluiden die de signalerende duif liet horen,
	// in volgorde. Een band is de technische variatie in het materiaal; hij zegt
	// niets over betekenis of emotie.
	bands []SoundBand
	calls int
}

// replayConversation speelt één gesprek af op de nepklok en meet wanneer de
// duif sprak.
func replayConversation(t *testing.T, conv fixtureConversation, pigeon string) replayResult {
	t.Helper()
	oracle := &fixtureOracle{conv: conv, pigeon: pigeon, lang: conv.Lang}
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = oracle
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = seededKnowledge(t)
	}, func(e *meetingEngine) {
		e.voice = newFakeVoice("frame")
	})
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": pigeon, "lang": conv.Lang, "autonomous": true, "playbackTracking": false,
	})
	// Meelezen op het kanaal houdt de sessie aan een echte abonnee, net als een
	// browser die de vergadering volgt.
	es := rig.events(t, id)
	d := rig.candidates(t, id)
	s := rig.session(t, id)

	last := conv.Segments[len(conv.Segments)-1].AtMS + 20_000
	next := 0
	// idle wacht tot alles wat de vorige stap in gang zette werkelijk klaar is:
	// de evaluatie én de beurt die zij misschien liet klinken. Die beurt loopt
	// in een eigen goroutine; liet de replay de klok doorlopen terwijl zij nog
	// sprak, dan hing het van de planner af op welke tik zij eindigde, en
	// daarmee wanneer de rusttijd begon en of een volgende kandidaat de duif
	// nog bezet vond. De evaluatie claimt het spreekslot vóór ze
	// running loslaat (speak → beginTurn), dus samen dekken de twee de hele
	// keten. En de eventstroom moet bijblijven: de replay speelt drie minuten
	// in een paar seconden af, en een volle abonneebuffer kost de meelezer
	// onder last zijn abonnement — met de interventies die hierna geteld
	// worden. Een kwart van de buffer achterstand is ruim genoeg voor wat één
	// stap uitlokt, en houdt het wachten zeldzaam.
	backlog := s.cfg.EventBuffer / 4
	idle := func() {
		waitFor(t, func() bool {
			d.mu.Lock()
			running := d.running
			d.mu.Unlock()
			return !running && !s.isSpeaking() && s.subscribersKeepUp(backlog)
		}, "een evaluatie, beurt of de eventstroom bleef hangen")
	}
	for now := int64(0); now <= last; now += 250 {
		for next < len(conv.Segments) && conv.Segments[next].AtMS <= now {
			rig.post(t, id, "/transcript", map[string]any{
				"text": conv.Segments[next].Text, "speaker": conv.Segments[next].Speaker,
			})
			next++
			idle()
		}
		// advanceProcessed, niet advance: die laatste wacht alleen tot de
		// sessielus de tik ophaalt. Stond de lus dan nog vóór maybeEvaluate,
		// dan zag idle() geen lopende evaluatie en schoof de klok al door
		// terwijl zij nog moest beginnen; dat gaf wisselende uitkomsten.
		rig.clock.advanceProcessed(t, 250*time.Millisecond)
		idle()
	}

	// Het tijdstip per bijdrage komt uit de diagnostiek: die telt in ms sinds
	// het openen van de sessie, dus precies op de tijdlijn van het gesprek.
	var spoken []int64
	for _, mark := range rig.session(t, id).diagnostics.snapshot().Recent {
		if mark.Stage == "governor_decision" && mark.Reason == string(verdictAdmit) {
			spoken = append(spoken, mark.AtMS)
		}
	}
	// De interventie-events komen in dezelfde volgorde als de doorlaatbesluiten
	// (speak() markeert en publiceert in één adem), dus ze lopen paarsgewijs op.
	var addressed []bool
	var bands []SoundBand
	s.mu.Lock()
	subscribed := len(s.subs)
	s.mu.Unlock()
	if subscribed != 1 {
		t.Fatalf("de meelezer is onderweg losgekoppeld (%d abonnees): de getelde events zijn onvolledig", subscribed)
	}
	for _, ev := range es.recordedThrough(t, s) {
		if ev.Type == EventIntervention {
			addressed = append(addressed, ev.Addressed)
		}
		if ev.Type == EventSound && ev.Sound != nil {
			bands = append(bands, ev.Sound.Band)
		}
	}
	return replayResult{spokenAtMS: spoken, addressed: addressed, bands: bands, calls: oracle.callCount()}
}

// TestMeetingReplay_PerPigeonSpeakingBehaviour meet per duif, per gesprek, hoe
// vaak en wanneer er gesproken wordt.
func TestMeetingReplay_PerPigeonSpeakingBehaviour(t *testing.T) {
	if testing.Short() {
		t.Skip("de replay speelt acht gesprekken van drie minuten af")
	}
	fixtures := loadConversationFixtures(t)
	// Ondergrens en bovengrens per gesprek en duif. De ondergrens is wat de
	// bedoeling van die duif vraagt, de bovengrens is de rem: een duif die de
	// vergadering overneemt is net zo kapot als een duif die zwijgt.
	want := map[string]map[string][2]int{
		"fireworks": {"biological": {1, 3}, "individualist": {0, 2}, "nonverbal": {1, 5}},
		"vermin":    {"biological": {1, 3}, "individualist": {2, 5}, "nonverbal": {1, 6}},
		// Het ziektegesprek. De kennispartner mag alleen spreken met
		// bronnen (`zonder_bron: false`), dus elke bijdrage van haar hier is
		// tegelijk het bewijs dat de kandidaatroute bronnen vond — op de echte
		// bundel meet TestLive_MeetingCandidateThreshold hetzelfde.
		"disease":    {"biological": {1, 3}, "individualist": {1, 4}, "nonverbal": {2, 6}},
		"tangential": {"biological": {0, 2}, "individualist": {0, 2}, "nonverbal": {1, 5}},
		"control":    {"biological": {0, 0}, "individualist": {0, 0}, "nonverbal": {0, 0}},
		// Het grenzende gesprek zonder besluit. De signalerende duif hoort
		// hier meer dan één keer te klinken — de oplopende cooldown is de enige
		// rem — en de twee pratende duiven houden hun hogere lat.
		"ambient": {"biological": {0, 1}, "individualist": {0, 1}, "nonverbal": {2, 6}},
		"budget":  {"biological": {0, 0}, "individualist": {0, 0}, "nonverbal": {0, 0}},
	}
	for _, conv := range fixtures.Conversations {
		for _, pigeon := range []string{"biological", "individualist", "nonverbal"} {
			t.Run(conv.ID+"/"+conv.Lang+"/"+pigeon, func(t *testing.T) {
				got := replayConversation(t, conv, pigeon)
				bounds := want[conv.ID][pigeon]
				t.Logf("%s/%s/%s: %d bijdrage(n) op %v ms, %d evaluaties",
					conv.ID, conv.Lang, pigeon, len(got.spokenAtMS), got.spokenAtMS, got.calls)
				if len(got.spokenAtMS) < bounds[0] || len(got.spokenAtMS) > bounds[1] {
					t.Errorf("%s/%s/%s sprak %d keer (%v ms), wil %d–%d",
						conv.ID, conv.Lang, pigeon, len(got.spokenAtMS), got.spokenAtMS, bounds[0], bounds[1])
				}
				// Hoevaak is de helft; wannéér is de andere helft. Een bijdrage
				// die vóór haar aanleiding valt, is geen bijdrage maar ruis.
				if first, ok := firstReasonMS(conv, pigeon); ok {
					for _, at := range got.spokenAtMS {
						if at < first {
							t.Errorf("%s/%s/%s sprak op %d ms, vóór de eerste aanleiding op %d ms",
								conv.ID, conv.Lang, pigeon, at, first)
						}
					}
				}
				// En de directe vraag krijgt antwoord: in `vermin` spreekt Dana
				// de duif aan op 105 s, en dat gaat langs de rusttijd maar wacht
				// op het zinseinde. De bijdrage hoort dus ná die zin te vallen,
				// niet ervoor, en zij hoort als `addressed` binnen te komen.
				if at, ok := addressedSegmentMS(conv); ok && pigeon != "nonverbal" {
					if !spokeOnTheAddressedLine(got, at) {
						t.Errorf("%s/%s/%s antwoordde niet op de directe vraag van %d ms: %v ms, addressed %v",
							conv.ID, conv.Lang, pigeon, at, got.spokenAtMS, got.addressed)
					}
				}
			})
		}
	}
}

// firstReasonMS is het moment van het eerste segment waar deze duif volgens de
// fixture iets aan hoort te hebben. Vóór dat moment is elke bijdrage te vroeg.
func firstReasonMS(conv fixtureConversation, pigeon string) (int64, bool) {
	for _, seg := range conv.Segments {
		if seg.scoreFor(pigeon) > 0 {
			return seg.AtMS, true
		}
	}
	return 0, false
}

// addressedSegmentMS geeft het moment waarop iemand de duif rechtstreeks
// aanspreekt, als dit gesprek dat kent.
func addressedSegmentMS(conv fixtureConversation) (int64, bool) {
	for _, seg := range conv.Segments {
		if seg.Addressed {
			return seg.AtMS, true
		}
	}
	return 0, false
}

// spokeOnTheAddressedLine zoekt een bijdrage die aan die aangesproken zin hangt:
// gemarkeerd als `addressed`, en gevallen ná de zin — binnen het venster tot de
// volgende spreker het overneemt.
func spokeOnTheAddressedLine(got replayResult, at int64) bool {
	const window = 20_000
	for i, spoken := range got.spokenAtMS {
		if spoken < at || spoken > at+window {
			continue
		}
		if i < len(got.addressed) && got.addressed[i] {
			return true
		}
	}
	return false
}

// TestMeetingReplay_SignalBandSpread meet over de bestaande fixtures hoe vaak
// elk van de drie banden bij de signalerende duif valt. Zij bestaat omdat de
// alarmband pas verschoven mag worden nadat er een ondergrens gemeten en
// vastgelegd is: de defaults horen zonder tuning te werken.
func TestMeetingReplay_SignalBandSpread(t *testing.T) {
	if testing.Short() {
		t.Skip("de replay speelt de gesprekken van drie minuten af")
	}
	fixtures := loadConversationFixtures(t)
	// De vier gemeten gesprekken. `ambient` en `budget` blijven
	// erbuiten: die zijn voor de cadans geschreven, niet voor het bandbereik.
	measured := map[string]bool{"fireworks": true, "vermin": true, "tangential": true, "control": true}
	totals := map[SoundBand]int{}
	perConversation := map[string]map[SoundBand]int{}
	runs := 0
	for _, conv := range fixtures.Conversations {
		if !measured[conv.ID] {
			continue
		}
		got := replayConversation(t, conv, "nonverbal")
		runs++
		counts := map[SoundBand]int{}
		for _, band := range got.bands {
			counts[band]++
			totals[band]++
		}
		if perConversation[conv.ID] == nil {
			perConversation[conv.ID] = map[SoundBand]int{}
		}
		for band, n := range counts {
			perConversation[conv.ID][band] += n
		}
		t.Logf("%s/%s: %d geluid(en) — rustig %d, nadrukkelijk %d, alarm %d",
			conv.ID, conv.Lang, len(got.bands), counts[BandCalm], counts[BandInsistent], counts[BandAlarm])
	}
	if runs != 8 {
		t.Fatalf("meting verwacht vier gesprekken in twee talen, kreeg %d afspeelbeurten", runs)
	}
	t.Logf("totaal over %d afspeelbeurten: rustig %d, nadrukkelijk %d, alarm %d",
		runs, totals[BandCalm], totals[BandInsistent], totals[BandAlarm])

	// De meting van 2026-09-15 gaf rustig 0, nadrukkelijk 18 en alarm 7
	// tot 8 (de trekking binnen een band is willekeurig). De ondergrenzen
	// hieronder zijn met marge van die meting afgeleid, ze zíjn de meting niet:
	// een rem op stille regressie, geen streefwaarde. Zakt het alarm hieronder, dan is dát het moment om
	// `spreeklat.drempel` of de bandgrenzen te bespreken — niet om deze getallen
	// te verlagen: de defaults horen zonder tuning te werken.
	//
	// De rustige band staat er bewust niet bij. Op de kandidaatroute krijgt de
	// meter de score van de kandidaat als impuls, en een score die de spreeklat
	// haalt ligt al boven de rustige bovengrens; rustig is de band van de
	// handmatige knop op een verse meter
	// (TestNonverbalPigeon_ManualIntervene_WorksWithTheDetectorOff).
	const (
		alarmFloor     = 4
		insistentFloor = 9
	)
	if totals[BandAlarm] < alarmFloor {
		t.Errorf("alarmband klonk %d keer over %d afspeelbeurten, ondergrens %d: bespreek de bandgrenzen of de spreeklat voordat iemand ze verschuift",
			totals[BandAlarm], runs, alarmFloor)
	}
	if totals[BandInsistent] < insistentFloor {
		t.Errorf("nadrukkelijke band klonk %d keer, ondergrens %d", totals[BandInsistent], insistentFloor)
	}
	// Het lege gesprek is de controle: geen aanleiding, dus geen enkel geluid.
	for band, n := range perConversation["control"] {
		t.Errorf("het controlegesprek liet %d keer band %q horen; daar hoort niets te klinken", n, band)
	}
}
