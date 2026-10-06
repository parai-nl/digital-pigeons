package duif

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Het contract van de kopregel. De parser is met opzet vergevingsgezind
// op vorm en streng op inhoud, en elke afwijking is één overgeslagen evaluatie
// — nooit een sessiebrede uitschakeling. Dat laatste is de belangrijkste regel
// van dit bestand: in het oude ontwerp zette één onbruikbaar antwoord de hele
// detector voor de rest van de vergadering uit.

func TestCandidateHeader_ParsesTheStrictFormAndTheMessyOnes(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		speak     bool
		score     float64
		topic     string
		addressed bool
		reason    string
	}{
		{name: "pass", line: "PASS", reason: headerOK},
		{name: "pass met punt", line: "pass.", reason: headerOK},
		{name: "pass met opsommingsstreepje", line: "- PASS", reason: headerOK},
		{name: "pass tussen backticks", line: "`PASS`", reason: headerOK},
		{name: "strak", line: "SPEAK score=0.72 topic=urban-ecology addressed=false",
			speak: true, score: 0.72, topic: "urban-ecology", reason: headerOK},
		{name: "extra spaties", line: "  SPEAK   score=0.72   topic=urban-ecology   addressed=true  ",
			speak: true, score: 0.72, topic: "urban-ecology", addressed: true, reason: headerOK},
		{name: "hoofdletters door elkaar", line: "Speak Score=0.5 Topic=urban-ecology Addressed=TRUE",
			speak: true, score: 0.5, topic: "urban-ecology", addressed: true, reason: headerOK},
		{name: "decimale komma", line: "SPEAK score=0,55 topic=urban-ecology addressed=false",
			speak: true, score: 0.55, topic: "urban-ecology", reason: headerOK},
		{name: "aanhalingstekens", line: `SPEAK score="0.6" topic="urban-ecology" addressed="false"`,
			speak: true, score: 0.6, topic: "urban-ecology", reason: headerOK},
		{name: "markdown-nadruk", line: "**SPEAK score=0.6 topic=urban-ecology addressed=false**",
			speak: true, score: 0.6, topic: "urban-ecology", reason: headerOK},
		{name: "addressed ontbreekt", line: "SPEAK score=0.6 topic=urban-ecology",
			speak: true, score: 0.6, topic: "urban-ecology", reason: headerOK},
		{name: "onbekende extra sleutel", line: "SPEAK score=0.6 topic=urban-ecology mood=curious",
			speak: true, score: 0.6, topic: "urban-ecology", reason: headerOK},

		{name: "score ontbreekt", line: "SPEAK topic=urban-ecology addressed=false", reason: headerMissingField},
		{name: "thema ontbreekt", line: "SPEAK score=0.6 addressed=false", reason: headerMissingField},
		{name: "score is geen getal", line: "SPEAK score=high topic=urban-ecology", reason: headerBadScore},
		{name: "score buiten bereik", line: "SPEAK score=1.4 topic=urban-ecology", reason: headerBadScore},
		{name: "geen kopregel", line: "Ik denk dat vuurwerk voor duiven vervelend is.", reason: headerNoHeader},
		{name: "lege regel", line: "   ", reason: headerNoHeader},
		{name: "json in plaats van een kopregel", line: `{"speak": true, "score": 0.7}`, reason: headerNoHeader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, reason := parseCandidateHeader(tc.line)
			if reason != tc.reason {
				t.Fatalf("reden = %q, wil %q", reason, tc.reason)
			}
			if header.Speak != tc.speak {
				t.Fatalf("speak = %v, wil %v", header.Speak, tc.speak)
			}
			if !tc.speak {
				return
			}
			if header.Score != tc.score || header.TopicID != tc.topic || header.Addressed != tc.addressed {
				t.Errorf("kopregel = %+v, wil score %.2f topic %q addressed %v",
					header, tc.score, tc.topic, tc.addressed)
			}
		})
	}
}

// De kopregel wordt streamend gelezen: hij komt vaak in stukjes binnen, en een
// `PASS` moet herkend worden zodra de regel af is.
func TestCandidateHeader_ReadsAcrossStreamChunks(t *testing.T) {
	var split headerSplitter
	if _, _, ready := split.feed("SPE"); ready {
		t.Fatal("een half woord is nog geen kopregel")
	}
	if _, _, ready := split.feed("AK score=0.8 topic=urban-ecology"); ready {
		t.Fatal("zonder regelovergang is de kopregel nog niet af")
	}
	line, rest, ready := split.feed(" addressed=false\nDe duif zegt iets.")
	if !ready {
		t.Fatal("de regelovergang sloot de kopregel niet af")
	}
	if rest != "De duif zegt iets." {
		t.Errorf("rest = %q", rest)
	}
	header, reason := parseCandidateHeader(line)
	if reason != headerOK || !header.Speak || header.Score != 0.8 {
		t.Fatalf("kopregel = %+v (%q)", header, reason)
	}
	// Wat er daarna binnenkomt is bijdrage, niet kopregel.
	if _, more, ready := split.feed(" En nog iets."); ready || more != " En nog iets." {
		t.Errorf("na de kopregel = %q, ready %v", more, ready)
	}
}

// Een kale PASS zonder afsluitende regelovergang is nog steeds een kopregel.
func TestCandidateHeader_UnterminatedLineStillCounts(t *testing.T) {
	var split headerSplitter
	split.feed("PASS")
	line, ready := split.finish()
	if !ready {
		t.Fatal("de afgesloten stroom leverde geen kopregel")
	}
	if _, reason := parseCandidateHeader(line); reason != headerOK {
		t.Errorf("reden = %q, wil geen", reason)
	}
}

// Een model dat zonder kopregel begint te schrijven mag niet de hele stroom
// opgeslokt krijgen: na een vaste hoeveelheid tekens is het oordeel geveld.
func TestCandidateHeader_UnboundedProseStopsAtTheScanLimit(t *testing.T) {
	var split headerSplitter
	line, _, ready := split.feed(strings.Repeat("x", headerScanLimit+10))
	if !ready {
		t.Fatal("de parser bleef wachten op een regelovergang die niet komt")
	}
	if _, reason := parseCandidateHeader(line); reason != headerNoHeader {
		t.Errorf("reden = %q, wil %q", reason, headerNoHeader)
	}
}

// De belangrijkste regel: rommelige uitvoer kost één evaluatie, geen sessie.
func TestCandidate_MalformedHeader_IsOnePassNotASessionWideShutdown(t *testing.T) {
	messy := []candidateStep{
		raw("Ik vind hier eigenlijk wel iets van, namelijk dit.\n"),
		raw("SPEAK topic=urban-ecology\nDe score ontbreekt.\n"),
		raw("SPEAK score=nogal topic=urban-ecology\nGeen getal.\n"),
		raw(""),
	}
	svc := newCandidateLLM(messy...)
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	d := rig.candidates(t, id)
	s := rig.session(t, id)
	stream := rig.events(t, id)

	for i := range messy {
		rig.post(t, id, "/transcript", map[string]any{"text": legibleLine(i)})
		waitForEvaluations(t, d, i+1)
		rig.clock.advance(t, time.Second)
	}
	if !d.status().Enabled {
		t.Fatalf("rommelige uitvoer zette de generator uit: %+v", d.status())
	}
	if c := s.heldCandidate(); c != nil {
		t.Errorf("een onleesbare kopregel leverde toch een kandidaat: %+v", c)
	}
	if hasEvent(stream.collected(), EventIntervention) {
		t.Error("een onleesbare kopregel liet de duif spreken")
	}
	messyCounts := d.counters()
	if messyCounts.Malformed != len(messy) {
		t.Errorf("misvormd = %d, want %d (%+v)", messyCounts.Malformed, len(messy), messyCounts)
	}

	// Een schone PASS is geen misvorming. Zolang de teller elke nette PASS
	// meetelde stond de diagnostiek vol met "malformed" bij een duif die
	// simpelweg niets te zeggen had, en was dit getal onbruikbaar om een echt
	// kapotte kopregel aan te herkennen.
	svc.mu.Lock()
	svc.steps = []candidateStep{raw("PASS\n"), raw("PASS\n")}
	svc.served = 0
	svc.mu.Unlock()
	for i := range 2 {
		rig.post(t, id, "/transcript", map[string]any{"text": legibleLine(len(messy) + i)})
		waitForEvaluations(t, d, len(messy)+i+1)
		rig.clock.advance(t, time.Second)
	}
	clean := d.counters()
	if clean.Malformed != messyCounts.Malformed {
		t.Errorf("schone passes telden mee als misvormd: %d → %d", messyCounts.Malformed, clean.Malformed)
	}
	if clean.Passed <= messyCounts.Passed {
		t.Errorf("de schone passes werden niet als pass geteld: %+v", clean)
	}

	// En na een schone kopregel doet zij weer gewoon mee.
	svc.mu.Lock()
	svc.steps = []candidateStep{speak(0.9, "urban-ecology", "Nu wel iets zinnigs [1].")}
	svc.served = 0
	svc.mu.Unlock()
	rig.post(t, id, "/transcript", map[string]any{"text": "En nu gaat het weer over het stationsdak."})
	waitFor(t, func() bool { return d.counters().Held > 0 }, "een schone SPEAK leverde geen bijdrage op")
	if spoke := d.counters(); spoke.Malformed != messyCounts.Malformed {
		t.Errorf("een schone SPEAK telde als misvormd: %d → %d", messyCounts.Malformed, spoke.Malformed)
	}
}

// De kopregel is een vormafspraak, geen stijlkeuze: de kandidaatcall gaat op
// temperatuur 0 de deur uit, net als de audit. Geen nieuwe knop — de waarde
// staat vast, en het model blijft het ene vaste model van de module.
func TestCandidate_CallIsDeterministicAndOnTheFixedModel(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Iets zinnigs [1]."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	d := rig.candidates(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForEvaluations(t, d, 1)

	calls := svc.candidateCalls()
	if len(calls) == 0 {
		t.Fatal("er ging geen kandidaatcall uit")
	}
	req := calls[0]
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Errorf("temperatuur = %v, want 0", req.Temperature)
	}
	if req.Model != rig.engine.detectModel {
		t.Errorf("model = %q, want %q", req.Model, rig.engine.detectModel)
	}
	if !req.Stream {
		t.Error("de kandidaatcall hoort te streamen: de kopregel wordt lezend geparseerd")
	}
}

// Een SPEAK zonder bijdrage is geen bijdrage.
func TestCandidate_SpeakWithoutBody_IsAPass(t *testing.T) {
	svc := newCandidateLLM(raw("SPEAK score=0.9 topic=urban-ecology addressed=false\n   \n"))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	d := rig.candidates(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForEvaluations(t, d, 1)
	rig.clock.advance(t, 2*time.Second)
	if c := rig.session(t, id).heldCandidate(); c != nil {
		t.Fatalf("een lege bijdrage werd toch een kandidaat: %+v", c)
	}
}

// Een verzonnen thema kost de bijdrage niet: zij hangt dan aan geen thema en de
// vergadering ziet dat de duif iets heeft over wat er net gezegd is.
func TestCandidate_UnknownTopic_KeepsTheContributionWithoutATheme(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "een-verzonnen-thema", "Toch iets zinnigs [1]."))
	rig := eagerRig(t, svc, nil)
	// Haar rust loopt nog, zodat de kandidaat blijft liggen om te bekijken.
	id, s := openHolding(t, rig, time.Hour, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})

	cand := waitForCandidate(t, s)
	if cand.TopicID != "" {
		t.Errorf("thema = %q, wil leeg", cand.TopicID)
	}
	ev := stream.wait(t, EventCandidate)
	if ev.Candidate == nil || ev.Candidate.Topic != nil {
		t.Errorf("kandidaat-event = %+v, wil geen thema", ev.Candidate)
	}
}

// Bij `PASS` breekt de generatie meteen af: dat is de goedkope uitkomst en ook
// de meest voorkomende, dus zij mag geen hele bijdrage kosten.
func TestCandidate_Pass_StopsTheStreamAtOnce(t *testing.T) {
	svc := newCandidateLLM(candidateStep{chunks: []string{"PASS\n", "dit hoort nooit gelezen te worden"}})
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	d := rig.candidates(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForEvaluations(t, d, 1)
	if c := rig.session(t, id).heldCandidate(); c != nil {
		t.Fatalf("na PASS bleef er een kandidaat liggen: %+v", c)
	}
}

// De signalerende duif krijgt geen betekenislabel mee: geen thema, geen score,
// alleen "wil een geluid geven".
func TestCandidate_SignalingPigeon_CarriesNoMeaning(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "nesting", ""))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "nonverbal", "autonomous": true, "eagerness": "eager"})
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "De richels onder de brug gaan tijdelijk dicht."})

	ev := stream.wait(t, EventCandidate)
	if ev.Candidate == nil || ev.Candidate.Reason != candidateReasonSignal {
		t.Fatalf("kandidaat-event = %+v", ev.Candidate)
	}
	if ev.Candidate.Topic != nil || ev.Candidate.Score != 0 {
		t.Errorf("de signalerende duif kreeg een betekenislabel: %+v", ev.Candidate)
	}
	got := stream.wait(t, EventIntervention)
	if got.Topic != nil || got.Score != 0 {
		t.Errorf("de interventie van de signalerende duif draagt betekenis: %+v", got)
	}
}

// Stop pauzeert zelfstandig meedoen, hervatten zet het terug, en de knop blijft
// er de hele tijd doorheen werken.
func TestCandidate_StopPausesAndResumeRestores(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Een bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForCandidate(t, s)

	if status, _ := rig.post(t, id, "/stop", nil); status != http.StatusAccepted && status != http.StatusOK {
		t.Fatalf("stop: status = %d", status)
	}
	calls := len(svc.candidateCalls())
	rig.post(t, id, "/transcript", map[string]any{"text": "En nog iets over dat dak erbij."})
	rig.clock.advance(t, 3*time.Second)
	if n := len(svc.candidateCalls()); n != calls {
		t.Errorf("na Stop gingen er alsnog %d call(s) uit", n-calls)
	}
	if status, body := rig.post(t, id, "/respond", nil); status != http.StatusAccepted {
		t.Errorf("de knop werkt niet na Stop: status = %d, body = %s", status, body)
	}
	resume := map[string]any{"turnId": s.settingsSnapshot().TurnID}
	if status, body := rig.post(t, id, "/resume", resume); status != http.StatusAccepted {
		t.Fatalf("hervatten: status = %d, body = %s", status, body)
	}
	rig.post(t, id, "/transcript", map[string]any{"text": "Nu weer verder over het dak hierboven."})
	waitFor(t, func() bool { return len(svc.candidateCalls()) > calls }, "na hervatten evalueerde de generator niet meer")
}

func legibleLine(i int) string {
	return strings.Repeat("woord ", 4) + string(rune('a'+i))
}
