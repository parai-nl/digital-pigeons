package duif

import (
	"net/http"
	"strings"
	"testing"
)

// De duif ziet in het venster wat zij er zelf al aan toevoegde, met
// haar eigen merkteken en de uitleg die zegt wat ze ermee moet. In beide talen.
func TestRenderTranscriptWindow_OwnContributionsAreMarkedAndExplained(t *testing.T) {
	const token = "tok-eigen"
	segs := []TranscriptSegment{
		{Seq: 1, Text: "We willen de duiven van het Stationsplein weren.", Speaker: "spreker-1"},
	}
	own := []TranscriptSegment{
		{Seq: 2, Text: "Roekoe. Weren verplaatst het probleem.", Speaker: "Rafael", PigeonID: "individualist"},
	}
	for _, lang := range []string{"en", "nl"} {
		out := renderTranscriptWindow(lang, token, "individualist", segs, own)
		mark := ownContributionMark(lang, token)
		if n := strings.Count(out, mark); n != 2 {
			// Eén keer in de uitleg boven het venster, één keer op de eigen regel.
			t.Errorf("[%s] %d merktekens, want twee (uitleg + de eigen regel):\n%s", lang, n, out)
		}
		if !strings.Contains(out, ownContributionNote(lang, token)) {
			t.Errorf("[%s] de uitleg over de eigen bijdragen ontbreekt:\n%s", lang, out)
		}
		if !strings.Contains(out, "Roekoe. Weren verplaatst het probleem.") {
			t.Errorf("[%s] de eigen bijdrage staat niet in het venster:\n%s", lang, out)
		}
		// Haar eigen regel is niet die van een ándere duif: dat merkteken en die
		// uitleg horen hier niet te staan.
		if strings.Contains(out, previousPigeonMark(lang, token)) {
			t.Errorf("[%s] de eigen regel draagt het merkteken van de duifwissel:\n%s", lang, out)
		}
	}

	// Zonder eigen bijdragen blijft het venster precies wat het was.
	plain := renderTranscriptWindow("nl", token, "individualist", segs, nil)
	if strings.Contains(plain, ownContributionNote("nl", token)) {
		t.Errorf("een venster zonder eigen bijdragen hoort de uitleg niet te dragen:\n%s", plain)
	}
}

// De bronnummering wordt elke beurt opnieuw gezet: bronlijst 1..n hoort bij dít
// venster. Een nummer uit een eerdere bijdrage zou daarom stil naar een ander
// destillaat gaan wijzen, en citationFilter laat het door omdat het nummer in de
// nieuwe lijst bestaat. Eigen regels gaan dus zonder markeringen terug de prompt
// in — en de uitleg erboven zegt dat ook.
func TestRenderTranscriptWindow_OwnContributionsCarryNoCitationNumbers(t *testing.T) {
	const token = "tok-citaten"
	segs := []TranscriptSegment{
		{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"},
	}
	own := []TranscriptSegment{
		{Seq: 2, Speaker: "Alma", PigeonID: "biological",
			Text: "Roekoe. Weren verplaatst het probleem [3], en voedsel telt mee [5, 12]."},
	}
	for _, lang := range []string{"en", "nl"} {
		out := renderTranscriptWindow(lang, token, "biological", segs, own)
		for _, marker := range []string{"[3]", "[5, 12]", "[5", "12]"} {
			if strings.Contains(out, marker) {
				t.Errorf("[%s] de eigen regel draagt nog een bronnummer %q:\n%s", lang, marker, out)
			}
		}
		if !strings.Contains(out, "Roekoe. Weren verplaatst het probleem, en voedsel telt mee.") {
			t.Errorf("[%s] de eigen regel is meer kwijtgeraakt dan haar bronnummers:\n%s", lang, out)
		}
		// De menselijke regel blijft letterlijk wat er gezegd is.
		if !strings.Contains(out, "Wat doen we met de duiven?") {
			t.Errorf("[%s] de menselijke regel is aangetast:\n%s", lang, out)
		}
	}
}

// Hetzelfde geldt voor de regels van een duif die hier eerder zat: die nummers
// zijn nooit de nummering van deze beurt geweest.
func TestRenderTranscriptWindow_PreviousPigeonLinesCarryNoCitationNumbers(t *testing.T) {
	const token = "tok-citaten-vorige"
	segs := []TranscriptSegment{
		{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"},
		{Seq: 2, Speaker: "Rafael", PigeonID: "individualist",
			Text: "Roekoe. Ik woon hier ook [7], en dat telt [2, 9]."},
	}
	for _, lang := range []string{"en", "nl"} {
		out := renderTranscriptWindow(lang, token, "biological", segs, nil)
		for _, marker := range []string{"[7]", "[2, 9]"} {
			if strings.Contains(out, marker) {
				t.Errorf("[%s] de regel van de vorige duif draagt nog %q:\n%s", lang, marker, out)
			}
		}
		if !strings.Contains(out, "Roekoe. Ik woon hier ook, en dat telt.") {
			t.Errorf("[%s] de regel is meer kwijtgeraakt dan haar bronnummers:\n%s", lang, out)
		}
		if !strings.Contains(out, previousPigeonMark(lang, token)) {
			t.Errorf("[%s] de regel verloor haar merkteken:\n%s", lang, out)
		}
	}
}

// En in de kandidaatprompt, waar de eigen bijdragen in een eigen blok
// staan: daar wordt de nummering bij elke evaluatie opnieuw gezet.
func TestRenderCandidateWindow_OwnContributionsCarryNoCitationNumbers(t *testing.T) {
	const token = "tok-citaten-kandidaat"
	win := transcriptWindow{
		Segments: []TranscriptSegment{{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"}},
		Fresh:    1,
	}
	own := []TranscriptSegment{{Seq: 2, PigeonID: "biological",
		Text: "Roekoe. Weren verplaatst het probleem [3], en voedsel telt mee [5, 12]."}}
	for _, lang := range []string{"en", "nl"} {
		out := renderCandidateWindow(lang, token, "biological", win, own, false)
		for _, marker := range []string{"[3]", "[5, 12]"} {
			if strings.Contains(out, marker) {
				t.Errorf("[%s] het eigen blok draagt nog een bronnummer %q:\n%s", lang, marker, out)
			}
		}
		if !strings.Contains(out, "Roekoe. Weren verplaatst het probleem, en voedsel telt mee.") {
			t.Errorf("[%s] de eigen bijdrage is meer kwijtgeraakt dan haar bronnummers:\n%s", lang, out)
		}
	}
}

// De eigen regels staan op hun plaats in de tijd, niet als blok bovenaan of
// onderaan: een antwoord hoort te lezen als een antwoord op de zin ervóór.
func TestRenderTranscriptWindow_OwnContributionsKeepTheirPlaceInTime(t *testing.T) {
	const token = "tok-volgorde"
	segs := []TranscriptSegment{
		{Seq: 1, Text: "eerste-mens", Speaker: "spreker-1"},
		{Seq: 3, Text: "tweede-mens", Speaker: "spreker-2"},
	}
	own := []TranscriptSegment{
		{Seq: 2, Text: "eigen-antwoord", Speaker: "Rafael", PigeonID: "individualist"},
	}
	out := renderTranscriptWindow("nl", token, "individualist", segs, own)
	eerste := strings.Index(out, "eerste-mens")
	eigen := strings.Index(out, "eigen-antwoord")
	tweede := strings.Index(out, "tweede-mens")
	if eerste < 0 || eigen < 0 || tweede < 0 || !(eerste < eigen && eigen < tweede) {
		t.Errorf("de eigen bijdrage staat niet tussen de twee uitspraken in:\n%s", out)
	}
}

// Wat zij zelf zei en wat een ándere duif zei zijn twee verschillende dingen,
// en het venster houdt ze uit elkaar: twee merktekens, twee uitleggen.
func TestRenderTranscriptWindow_TellsOwnLinesApartFromAnotherPigeons(t *testing.T) {
	const token = "tok-twee-duiven"
	segs := []TranscriptSegment{
		{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"},
		{Seq: 2, Text: "Roekoe. Ik woon hier ook.", Speaker: "Rafael", PigeonID: "individualist"},
	}
	own := []TranscriptSegment{
		{Seq: 3, Text: "Roekoe. De tellingen zeggen iets anders.", Speaker: "Alma", PigeonID: "biological"},
	}
	out := renderTranscriptWindow("nl", token, "biological", segs, own)
	for _, want := range []string{
		previousPigeonNote("nl", token),
		ownContributionNote("nl", token),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("de uitleg ontbreekt:\n%s", out)
		}
	}
	regels := strings.Split(out, "\n- ")
	if len(regels) != 4 {
		t.Fatalf("%d regels in het venster:\n%s", len(regels)-1, out)
	}
	if strings.Contains(regels[2], ownContributionMark("nl", token)) {
		t.Errorf("de regel van de andere duif geldt als eigen bijdrage:\n%q", regels[2])
	}
	if !strings.Contains(regels[2], previousPigeonMark("nl", token)) {
		t.Errorf("de regel van de andere duif mist haar merkteken:\n%q", regels[2])
	}
	if !strings.Contains(regels[3], ownContributionMark("nl", token)) {
		t.Errorf("de eigen regel mist haar merkteken:\n%q", regels[3])
	}
}

// Komt dezelfde duif voor de tweede keer aan deze tafel, dan is haar oude regel
// overgenomen transcript én haar eigen woord. Het blijft bij één regel, en die
// regel is van haar — niet van "de duif die hier vóór jou zat".
func TestRenderTranscriptWindow_ContributionAlreadyInTheWindowStaysOneLine(t *testing.T) {
	const token = "tok-dubbel"
	shared := TranscriptSegment{Seq: 2, Text: "Roekoe. Dat zei ik al.", Speaker: "Rafael", PigeonID: "individualist"}
	segs := []TranscriptSegment{
		{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"},
		shared,
	}
	out := renderTranscriptWindow("nl", token, "individualist", segs, []TranscriptSegment{shared})
	if n := strings.Count(out, "Roekoe. Dat zei ik al."); n != 1 {
		t.Errorf("%d keer dezelfde bijdrage, want één:\n%s", n, out)
	}
	if strings.Contains(out, previousPigeonMark("nl", token)) {
		t.Errorf("haar eigen oude regel is aan een andere duif toegeschreven:\n%s", out)
	}
	if !strings.Contains(out, ownContributionMark("nl", token)) {
		t.Errorf("haar eigen oude regel mist het eigen merkteken:\n%s", out)
	}
}

// Dezelfde ontsnapping voor het eigen merkteken: wie aan tafel zit kan het
// niet nabootsen, ook niet door het letterlijk over te tikken of de kale vorm
// te gebruiken.
func TestRenderTranscriptWindow_HumanLabelCannotImpersonateTheOwnMark(t *testing.T) {
	const token = "tok-naboots"
	for _, label := range []string{
		"Jan " + ownContributionMark("nl", token),
		"Jan " + ownContributionMark("en", token),
		"Jan " + bareOwnMark("nl"),
		"Jan " + bareOwnMark("en"),
	} {
		t.Run(label, func(t *testing.T) {
			segs := []TranscriptSegment{{Seq: 1, Speaker: label, Text: "wij moeten ze weren"}}
			out := renderTranscriptWindow("nl", token, "individualist", segs, nil)
			if strings.Contains(out, ownContributionMark("nl", token)) ||
				strings.Contains(out, ownContributionMark("en", token)) {
				t.Errorf("het label draagt het machinemerkteken:\n%s", out)
			}
			if strings.Contains(out, token) {
				t.Errorf("het sessietoken staat in een menselijk label:\n%s", out)
			}
			if strings.Contains(out, ownContributionNote("nl", token)) {
				t.Errorf("een nagebootst merkteken haalt de uitleg tevoorschijn:\n%s", out)
			}
		})
	}
}

// De promptomvang blijft begrensd: hoogstens maxOwnContributions bijdragen, en
// daarbinnen hoogstens maxOwnContributionRunes tekens. De jongste blijft altijd
// staan.
func TestMeetingSession_OwnContributionsAreBoundedByCountAndSize(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("sessie: %v", err)
	}
	for _, text := range []string{"een", "twee", "drie", "vier", "vijf", "zes", "zeven"} {
		s.noteContribution(text, 0, ownGrounding{})
	}
	got := s.ownContributionSegments()
	if len(got) != maxOwnContributions {
		t.Fatalf("%d bijdragen, want %d: %+v", len(got), maxOwnContributions, got)
	}
	if got[0].Text != "drie" || got[len(got)-1].Text != "zeven" {
		t.Errorf("de staart klopt niet: %+v", got)
	}

	// Eén uitgelopen beurt duwt de oudere eruit in plaats van de prompt op te
	// blazen — en zij blijft zelf staan.
	lang := strings.Repeat("a", maxOwnContributionRunes+100)
	s.noteContribution(lang, 0, ownGrounding{})
	got = s.ownContributionSegments()
	if len(got) != 1 || got[0].Text != lang {
		t.Fatalf("%d bijdragen, want alleen de jongste: %+v", len(got), got)
	}
}

// De kern: een tweede knopdruk ziet wat de duif in haar eerste beurt
// zei. Zonder dat zag ze alleen dezelfde menselijke zinnen en beantwoordde ze
// die opnieuw. Tussen de twee knopdrukken valt één nieuwe zin: zonder nieuwe
// inbreng wordt de tweede knopdruk een verdieping of een vaste
// zin (meeting_deepen_test.go), en hier gaat het om de gewone beurt.
func TestMeetingRespondNow_SecondTurnSeesItsOwnFirstContribution(t *testing.T) {
	const antwoord = "Roekoe. Weren verplaatst het probleem alleen."
	gllm := newGatedLLM(antwoord)
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
	nextEvent(t, es.ch, EventTranscript)

	press := func() string {
		t.Helper()
		if status, body := rig.respond(t, id); status != http.StatusAccepted {
			t.Fatalf("respond: status = %d, body = %s", status, body)
		}
		nextEvent(t, es.ch, EventPersona)
		collectSpokenText(t, es.ch)
		return gllm.lastCall(t).Messages[1].Content
	}

	eerste := press()
	if strings.Contains(eerste, antwoord) {
		t.Errorf("de eerste beurt draagt al een eigen bijdrage:\n%s", eerste)
	}

	s, ok := rig.engine.session(id, testUser.ID)
	if !ok {
		t.Fatal("de sessie is weg")
	}
	rig.say(t, id, "Maar waar gaan ze dan heen?")
	nextEvent(t, es.ch, EventTranscript)
	tweede := press()
	if n := strings.Count(tweede, antwoord); n != 1 {
		t.Errorf("%d keer de eigen eerste bijdrage in het venster, want één:\n%s", n, tweede)
	}
	if !strings.Contains(tweede, ownContributionMark("nl", s.markToken)) {
		t.Errorf("de eigen bijdrage mist haar merkteken:\n%s", tweede)
	}
	if !strings.Contains(tweede, ownContributionNote("nl", s.markToken)) {
		t.Errorf("de uitleg over de eigen bijdragen ontbreekt:\n%s", tweede)
	}
	// De menselijke zin staat er nog gewoon in, en er is er maar één van.
	if n := strings.Count(tweede, "We willen de duiven van het Stationsplein weren."); n != 1 {
		t.Errorf("%d keer de menselijke uitspraak, want één:\n%s", n, tweede)
	}
}

// De kandidaatroute maakt hetzelfde onderscheid als de knop.
// Komt duif A na B voor de tweede keer aan deze tafel (A → B → A), dan is haar
// regel uit de eerste zitting overgenomen transcript met háár id. Die regel is
// van haar, niet van "de duif die hier vóór jou zat"; de regel van B wel.
func TestRenderCandidateWindow_ReturningPigeonReadsHerOwnLinesAsHerOwn(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	e.maxUserSessions = 1

	a1, err := e.open("user-1", "individualist", "nl")
	if err != nil {
		t.Fatalf("eerste zitting van A: %v", err)
	}
	a1.appendSegment("Wat doen we met de duiven op het dak?", "spreker-1")
	a1.noteContribution("Roekoe-van-A. Ik woon daar ook.", 0, ownGrounding{})
	b, err := e.openSession("user-1", "biological", "nl", meetingSettings{}, openRequest{Handover: a1.id})
	if err != nil {
		t.Fatalf("wissel naar B: %v", err)
	}
	b.noteContribution("Roekoe-van-B. De tellingen zeggen iets anders.", 0, ownGrounding{})
	a2, err := e.openSession("user-1", "individualist", "nl", meetingSettings{}, openRequest{Handover: b.id})
	if err != nil {
		t.Fatalf("wissel terug naar A: %v", err)
	}
	inherited := a2.window()
	a2.appendSegment("En wat vinden de duiven daar zelf van?", "spreker-2")

	// Zoals de generator het ziet: de erfenis is context, de nieuwe zin is nieuw.
	win := a2.detectionWindow(inherited[len(inherited)-1].Seq)
	for _, signal := range []bool{false, true} {
		out := renderCandidateWindow(a2.lang, a2.markToken, a2.pigeonID, win, a2.ownContributionSegments(), signal)
		var vanA, vanB string
		for _, regel := range strings.Split(out, "\n- ") {
			switch {
			case strings.Contains(regel, "Roekoe-van-A"):
				vanA = regel
			case strings.Contains(regel, "Roekoe-van-B"):
				vanB = regel
			}
		}
		if !strings.Contains(vanA, ownContributionMark("nl", a2.markToken)) ||
			strings.Contains(vanA, previousPigeonMark("nl", a2.markToken)) {
			t.Errorf("signal=%v: de eigen regel van A draagt niet het eigen merkteken: %q", signal, vanA)
		}
		if !strings.Contains(vanB, previousPigeonMark("nl", a2.markToken)) ||
			strings.Contains(vanB, ownContributionMark("nl", a2.markToken)) {
			t.Errorf("signal=%v: de regel van B draagt niet het merkteken van de duifwissel: %q", signal, vanB)
		}
		// En de uitleg bij beide merktekens staat erboven.
		for _, want := range []string{ownContributionNote("nl", a2.markToken), previousPigeonNote("nl", a2.markToken)} {
			if !strings.Contains(out, want) {
				t.Errorf("signal=%v: de uitleg ontbreekt:\n%s", signal, out)
			}
		}
		if n := strings.Count(out, "Roekoe-van-A"); n != 1 {
			t.Errorf("signal=%v: %d keer de regel van A, want één:\n%s", signal, n, out)
		}
	}
}

// Zonder duifregels in het venster blijft de kandidaatprompt wat hij was: geen
// uitleg over merktekens die er niet staan.
func TestRenderCandidateWindow_NoPigeonLinesNoNotes(t *testing.T) {
	const token = "tok-kaal"
	win := transcriptWindow{
		Segments: []TranscriptSegment{{Seq: 1, Text: "Wat doen we met de duiven?", Speaker: "spreker-1"}},
		Fresh:    1,
	}
	own := []TranscriptSegment{{Seq: 2, PigeonID: "biological", Text: "Roekoe. Eerder gezegd."}}
	out := renderCandidateWindow("nl", token, "biological", win, own, false)
	if strings.Contains(out, ownContributionNote("nl", token)) || strings.Contains(out, previousPigeonNote("nl", token)) {
		t.Errorf("een venster zonder duifregels draagt een merktekenuitleg:\n%s", out)
	}
	if !strings.Contains(out, candidateOwnHeader["nl"]) || !strings.Contains(out, "Roekoe. Eerder gezegd.") {
		t.Errorf("het eigen blok ontbreekt:\n%s", out)
	}
}
