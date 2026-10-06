package duif

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De queryherformulering, op beide vergaderroutes.
//
// Wat hier vastligt is niet "het model schrijft een goede vraag" — dat is een
// live meting. Hier ligt het contract eromheen vast: de herformulering bereikt de zoekopdracht, zij is nooit een nieuwe
// faalroute, en de zelfstandige route betaalt haar niet elke cadans opnieuw.

// recordingEmbedder onthoudt met welke tekst er werkelijk gezocht is. Dat is de
// enige plek waar zichtbaar wordt of de herformulering de retrieval bereikte:
// de gate en de kennisbank zien alleen nog een vector.
type recordingEmbedder struct {
	inner llm.Embedder
	mu    sync.Mutex
	texts []string
}

func newRecordingEmbedder() *recordingEmbedder {
	return &recordingEmbedder{inner: &llmtesting.MockEmbedder{}}
}

func (e *recordingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	e.texts = append(e.texts, text)
	e.mu.Unlock()
	return e.inner.Embed(ctx, text)
}

func (e *recordingEmbedder) queries() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.texts...)
}

// queryReplyLLM stuurt de herformuleringscall een eigen antwoord of een eigen
// storing; de beurt zelf gaat naar de onderliggende dienst.
type queryReplyLLM struct {
	llm.Service
	reply string
	err   error
	stall time.Duration
}

func (q queryReplyLLM) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if !isMeetingQueryRequest(req) {
		return q.Service.CompleteStream(ctx, req)
	}
	if q.err != nil {
		return nil, q.err
	}
	if q.stall > 0 {
		out := make(chan llm.StreamChunk)
		go func() {
			defer close(out)
			select {
			case <-ctx.Done():
			case <-time.After(q.stall):
			}
		}()
		return out, nil
	}
	return fixedStream(q.reply), nil
}

const spokenAboutRoofs = "Een centrale show op het plein betekent heel harde knallen en veel licht pal tegen het stationsdak aan."

// segsOf maakt van één gesproken regel het venster van één segment, zodat de
// contracttoetsen hieronder hun venster als tekst kunnen blijven schrijven.
func segsOf(text string) []TranscriptSegment { return []TranscriptSegment{{Text: text}} }

// queryRig is een vergaderrig met een kennisbank die echt antwoordt en een
// embedder die onthoudt waarmee er gezocht is.
func queryRig(t *testing.T, service llm.Service) (*meetingRig, *recordingEmbedder) {
	t.Helper()
	embedder := newRecordingEmbedder()
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = service
		m.embedder = embedder
		m.knowledge = seededKnowledge(t)
	}, func(e *meetingEngine) {
		// De echte tijdslimiet is vier seconden; die uitzitten zou elke test
		// van de terugval even lang laten duren als de storing die zij meet.
		e.queryTimeout = 50 * time.Millisecond
	})
	return rig, embedder
}

// De knop-route zoekt met de herformuleerde vraag, niet met het ruwe venster.
func TestMeetingQuery_ButtonRouteSearchesWithTheReformulatedQuestion(t *testing.T) {
	gllm := newGatedLLM("Een korte bijdrage.")
	rig, embedder := queryRig(t, gllm)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, spokenAboutRoofs)
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	collectSpokenText(t, es.ch)

	if n := len(gllm.queryCalls()); n != 1 {
		t.Fatalf("herformuleringscalls = %d, want precies 1", n)
	}
	got := embedder.queries()
	if len(got) != 1 || got[0] != testMeetingQueryLine {
		t.Fatalf("er is gezocht met %q, want de herformuleerde vraag %q", got, testMeetingQueryLine)
	}
	// De instructie krijgt het venster als gebruikersbericht mee; anders zou de
	// herformulering over niets gaan.
	if user := gllm.queryCalls()[0].Messages[1].Content; !strings.Contains(user, "stationsdak") {
		t.Errorf("het venster bereikte de herformulering niet: %q", user)
	}
}

// Valt de herformulering om, dan zoekt de vergadering met het ruwe venster —
// precies zoals vóór de herformulering. Zij mag nooit een nieuwe faalroute
// worden.
func TestMeetingQuery_FallsBackToTheRawWindow(t *testing.T) {
	gevallen := map[string]queryReplyLLM{
		"storing":           {err: errors.New("test: herformulering onbereikbaar")},
		"geen onderwerp":    {reply: "NONE"},
		"geen onderwerp nl": {reply: "GEEN."},
		"leeg antwoord":     {reply: "   "},
	}
	for naam, gebroken := range gevallen {
		t.Run(naam, func(t *testing.T) {
			gllm := newGatedLLM("Een korte bijdrage.")
			gebroken.Service = gllm
			rig, embedder := queryRig(t, gebroken)

			id := rig.openSession(t, "biological", "nl")
			es := rig.events(t, id)
			rig.say(t, id, spokenAboutRoofs)
			nextEvent(t, es.ch, EventTranscript)
			rig.respond(t, id)
			_, evs := collectSpokenText(t, es.ch)

			got := embedder.queries()
			if len(got) != 1 || !strings.Contains(got[0], "stationsdak") {
				t.Fatalf("er is gezocht met %q, want het ruwe venster", got)
			}
			for _, ev := range evs {
				if ev.Type == EventError {
					t.Errorf("een mislukte herformulering mag geen foutmelding opleveren: %+v", ev)
				}
			}
			if !hasEvent(evs, EventResponseDone) {
				t.Error("de beurt is niet afgerond")
			}
		})
	}
}

// Een herformulering die blijft hangen kost de zaal hoogstens de tijdslimiet en
// levert daarna alsnog het ruwe venster op.
func TestMeetingQuery_TimeoutFallsBackWithoutBlockingTheTurn(t *testing.T) {
	gllm := newGatedLLM("Een korte bijdrage.")
	rig, embedder := queryRig(t, queryReplyLLM{Service: gllm, stall: 2 * time.Second})

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	rig.say(t, id, spokenAboutRoofs)
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	_, evs := collectSpokenText(t, es.ch)

	if got := embedder.queries(); len(got) != 1 || !strings.Contains(got[0], "stationsdak") {
		t.Fatalf("er is gezocht met %q, want het ruwe venster", got)
	}
	if !hasEvent(evs, EventResponseDone) {
		t.Error("de beurt is niet afgerond")
	}
}

// De zelfstandige route betaalt de herformulering niet elke cadans opnieuw
// zolang het gesprek over hetzelfde gaat, en stelt wél een nieuwe vraag zodra
// de tafel bij een ander onderwerp zit.
//
// Het venster groeit aan het begin van een sessie hard: van één naar zes
// segmenten is een wezenlijk ander gesprek, en daar hóórt een nieuwe vraag bij.
// De meting begint daarom pas als het venster al gevuld is.
func TestCandidateQuery_ReusedWhileTheWindowHoldsAndRenewedWhenItTurns(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "iets over daken"))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)

	evaluaties := 0
	zeg := func(t *testing.T, zinnen ...string) {
		t.Helper()
		for _, zin := range zinnen {
			rig.post(t, id, "/transcript", map[string]any{"text": zin})
			evaluaties++
			waitForEvaluations(t, d, evaluaties)
			rig.clock.advance(t, time.Second)
		}
	}

	zeg(t,
		"Een centrale vuurwerkshow op het plein geeft harde knallen tegen het stationsdak.",
		"De bewonersvereniging wil die vuurwerkshow juist op het plein houden vanwege de knallen.",
		"Het stationsdak vangt volgens de bewonersvereniging het meeste licht van die vuurwerkshow.",
		"Ruben vindt de knallen tegen het stationsdak erger dan het licht van de vuurwerkshow.",
		"De knallen van die vuurwerkshow houden volgens de bewonersvereniging het plein wakker.",
		"Het licht tegen het stationsdak hoort volgens Ruben bij zo'n centrale vuurwerkshow.",
	)
	gevuld := len(svc.queryCalls())
	if gevuld == 0 {
		t.Fatal("de zelfstandige route herformuleerde helemaal niet")
	}

	// Hetzelfde onderwerp, twee segmenten verder: het venster is opgeschoven,
	// niet omgeslagen.
	zeg(t,
		"De bewonersvereniging noemt de knallen van die vuurwerkshow opnieuw een probleem.",
		"Ruben herhaalt dat het licht tegen het stationsdak bij die vuurwerkshow hoort.",
	)
	if n := len(svc.queryCalls()); n != gevuld {
		t.Fatalf("herformuleringscalls = %d, want %d: hetzelfde onderwerp hoort geen nieuwe vraag te kosten", n, gevuld)
	}

	// En nu een tafel die volledig van onderwerp verandert.
	zeg(t,
		"Volgend agendapunt: de subsidieaanvraag voor het buurthuis aan de overkant.",
		"De penningmeester vraagt of de begroting voor dat buurthuis inmiddels rond is.",
		"Zonder sluitende begroting kan de subsidieaanvraag voor het buurthuis niet weg.",
		"De penningmeester stuurt de begroting van het buurthuis morgen naar de subsidieverstrekker.",
		"De secretaris legt de subsidieaanvraag van het buurthuis daarna aan de wethouder voor.",
		"De wethouder beslist volgende maand over de subsidieaanvraag van dat buurthuis.",
		"De penningmeester noteert de deadline van de subsidieaanvraag in de agenda.",
		"De secretaris archiveert de begroting van het buurthuis bij de subsidieaanvraag.",
	)
	if n := len(svc.queryCalls()); n <= gevuld {
		t.Fatalf("herformuleringscalls = %d, want meer dan %d: een ander onderwerp hoort een nieuwe vraag te krijgen", n, gevuld)
	}
}

func TestCleanMeetingQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Wat doet vuurwerk met duiven in de stad?", "Wat doet vuurwerk met duiven in de stad?", true},
		{"  \"Wat doet vuurwerk met duiven?\"  ", "Wat doet vuurwerk met duiven?", true},
		{"Wat doet vuurwerk met duiven?\nUitleg: het gesprek ging over knallen.", "Wat doet vuurwerk met duiven?", true},
		{"NONE", "", false},
		{"geen.", "", false},
		{"", "", false},
		{strings.Repeat("a", meetingQueryMaxRunes+50), strings.Repeat("a", meetingQueryMaxRunes), true},
	}
	for _, c := range cases {
		got, ok := cleanMeetingQuery(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("cleanMeetingQuery(%.40q) = %.40q,%v — want %.40q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Een teruggevallen zoekvraag wordt niet onthouden: één storing mag de rest van
// de vergadering niet op het ruwe venster vastzetten.
func TestQueryMemory_RemembersOnlyRealReformulations(t *testing.T) {
	m := &queryMemory{}
	m.remember(spokenAboutRoofs, meetingQuery{Query: spokenAboutRoofs, Reason: queryReasonTransport})
	if _, ok := m.reuse(spokenAboutRoofs); ok {
		t.Fatal("een teruggevallen zoekvraag werd onthouden")
	}
	m.remember(spokenAboutRoofs, meetingQuery{Query: "Wat doet vuurwerk met duiven?", Reformed: true, Reason: queryReasonOK})
	got, ok := m.reuse(spokenAboutRoofs + " De bewonersvereniging wil die show op het plein.")
	if !ok || got.Query != "Wat doet vuurwerk met duiven?" || got.Reason != queryReasonReused {
		t.Fatalf("hergebruik = %+v, %v", got, ok)
	}
	if _, ok := m.reuse("Volgend agendapunt: de subsidieaanvraag voor het buurthuis aan de overkant."); ok {
		t.Fatal("een volstrekt ander venster hergebruikte de oude zoekvraag")
	}
}

// Elke terugval draagt haar eigen redencode, en die code staat in het
// log. Zonder dat onderscheid telt een latencytabel timeouts, transportfouten,
// lege antwoorden en "hier valt niets te zoeken" als één getal op, en leest dat
// getal als het aantal calls dat de tijdslimiet raakte — precies de conclusie
// die de keuze voor die limiet moet dragen.
func TestMeetingQuery_EveryFallbackCarriesItsOwnReason(t *testing.T) {
	gevallen := []struct {
		naam    string
		service llm.Service
		venster string
		reden   string
	}{
		{"geen dienst", nil, spokenAboutRoofs, queryReasonNoService},
		{"leeg venster", queryReplyLLM{reply: testMeetingQueryLine}, "   ", queryReasonEmptyWindow},
		{"storing", queryReplyLLM{err: errors.New("test: herformulering onbereikbaar")}, spokenAboutRoofs, queryReasonTransport},
		{"tijdslimiet", queryReplyLLM{stall: time.Second}, spokenAboutRoofs, queryReasonTimeout},
		{"onbruikbaar antwoord", queryReplyLLM{reply: "   "}, spokenAboutRoofs, queryReasonUnusable},
		{"geen onderwerp", queryReplyLLM{reply: "NONE"}, spokenAboutRoofs, queryReasonNoSubject},
	}
	for _, tc := range gevallen {
		t.Run(tc.naam, func(t *testing.T) {
			logger, logged := captureLogs()
			engine := &meetingEngine{
				llm: tc.service, detectModel: "test-model", clock: systemClock{},
				logger: logger, queryTimeout: 50 * time.Millisecond,
			}
			q := engine.reformulateQuery(context.Background(), "nl", routeMeetingQuery, segsOf(tc.venster))
			if q.Reformed {
				t.Fatalf("%s hoort een terugval te zijn, kreeg %+v", tc.naam, q)
			}
			if q.Reason != tc.reden {
				t.Errorf("reden = %q, wil %q", q.Reason, tc.reden)
			}
			if q.Query != tc.venster {
				t.Errorf("een terugval zoekt met het ruwe venster, kreeg %q", q.Query)
			}
			engine.logQuery("sessie-1", routeMeetingQuery, q)
			if !strings.Contains(logged(), "reden="+tc.reden) {
				t.Errorf("de redencode staat niet in het log:\n%s", logged())
			}
		})
	}

	// En het geslaagde geval draagt zijn eigen code, zodat "terugval" nooit
	// een geslaagde herformulering kan opslokken.
	logger, _ := captureLogs()
	engine := &meetingEngine{
		llm: queryReplyLLM{reply: testMeetingQueryLine}, detectModel: "test-model",
		clock: systemClock{}, logger: logger, queryTimeout: time.Second,
	}
	if q := engine.reformulateQuery(context.Background(), "nl", routeMeetingQuery, segsOf(spokenAboutRoofs)); !q.Reformed || q.Reason != queryReasonOK {
		t.Fatalf("geslaagde herformulering = %+v", q)
	}
}

// De herformulering is verankerd op de laatste inbreng aan tafel. Wat
// hier vastligt is welke zin dat is — niet of de zoekvraag daarmee beter wordt,
// want dat is een live meting.
func TestMeetingQueryAnchor_PicksTheLastHumanQuestion(t *testing.T) {
	gevallen := []struct {
		naam string
		segs []TranscriptSegment
		wil  string
	}{
		{
			// Het kerngeval: de tafel stelt halverwege de echte
			// vraag en sluit daarna af met procedure. De procedure is geen
			// onderzoeksonderwerp, de vraag wel.
			naam: "een vraag halverwege wint van de procedurele slotzin",
			segs: []TranscriptSegment{
				{Text: "Punt vier: het vuurwerkbeleid voor oud en nieuw."},
				{Text: "Is dat echt een probleem? Ik heb nog nooit een vogel uit de lucht zien vallen."},
				{Text: "Akkoord. Ilse schrijft één alinea, volgende week stemmen we."},
			},
			wil: "Is dat echt een probleem? Ik heb nog nooit een vogel uit de lucht zien vallen.",
		},
		{
			naam: "zonder vraagteken is het de laatste spreekbeurt",
			segs: []TranscriptSegment{
				{Text: "Het staal onder het dek moet deze zomer behandeld worden."},
				{Text: "De netten gaan er daarna weer af."},
			},
			wil: "De netten gaan er daarna weer af.",
		},
		{
			// De duif mag zichzelf niet terugvoeren, ook niet als anker.
			naam: "regels van de duif zelf tellen niet mee",
			segs: []TranscriptSegment{
				{Text: "Wat mij zorgen baart zijn de dieren op die daken."},
				{Text: "Wat weten we daar eigenlijk van?", PigeonID: "biological"},
			},
			wil: "Wat mij zorgen baart zijn de dieren op die daken.",
		},
		{
			naam: "lege regels worden overgeslagen",
			segs: []TranscriptSegment{{Text: "De bewonersvereniging wil één centrale show."}, {Text: "   "}},
			wil:  "De bewonersvereniging wil één centrale show.",
		},
		{naam: "een leeg venster heeft geen anker", segs: nil, wil: ""},
	}
	for _, tc := range gevallen {
		t.Run(tc.naam, func(t *testing.T) {
			if got := meetingQueryAnchor(tc.segs); got != tc.wil {
				t.Errorf("anker = %q, wil %q", got, tc.wil)
			}
		})
	}
}

// Het anker gaat als eigen blok naar het model. Staat het er niet apart, dan
// moet het model in een lap transcript raden waar de tafel nú staat — precies
// wat de verankering repareerde.
func TestMeetingQueryInput_CarriesTheAnchorSeparately(t *testing.T) {
	venster := "Wat mij zorgen baart zijn de dieren. Is dat echt een probleem?"
	anker := "Is dat echt een probleem?"
	got := meetingQueryInput("nl", venster, anker)
	if !strings.Contains(got, "Laatste inbreng aan tafel:") || !strings.HasSuffix(got, anker) {
		t.Errorf("de invoer draagt het anker niet als eigen blok:\n%s", got)
	}
	if !strings.Contains(got, venster) {
		t.Errorf("de invoer draagt het venster niet:\n%s", got)
	}
	// Eén segment: anker en venster zijn dezelfde zin. Twee koppen boven twee
	// keer dezelfde tekst leert het model niets en kost alleen tokens.
	if got := meetingQueryInput("nl", anker, anker); got != anker {
		t.Errorf("een venster van één zin kreeg toch koppen: %q", got)
	}
	// Een onbekende taal valt terug op de Engelse koppen in plaats van op geen.
	if got := meetingQueryInput("de", venster, anker); !strings.Contains(got, "Last contribution at the table:") {
		t.Errorf("onbekende taal kreeg geen koppen:\n%s", got)
	}
}

// En de wiring erachter: de knop-route stuurt het venster én het anker naar het
// model, in die volgorde. Het anker is de laatste vraag die aan tafel viel, niet
// de laatste zin die er toevallig achteraan kwam.
func TestMeetingQuery_ButtonRouteSendsWindowAndAnchorToTheModel(t *testing.T) {
	gllm := newGatedLLM("Een korte bijdrage.")
	rig, _ := queryRig(t, gllm)

	id := rig.openSession(t, "biological", "nl")
	es := rig.events(t, id)
	for _, zin := range []string{
		spokenAboutRoofs,
		"Is dat echt een probleem? Ik heb nog nooit een vogel uit de lucht zien vallen.",
		"Akkoord, volgende week stemmen we.",
	} {
		rig.say(t, id, zin)
		nextEvent(t, es.ch, EventTranscript)
	}
	rig.respond(t, id)
	collectSpokenText(t, es.ch)

	calls := gllm.queryCalls()
	if len(calls) != 1 {
		t.Fatalf("herformuleringscalls = %d, want precies 1", len(calls))
	}
	user := calls[0].Messages[1].Content
	if !strings.Contains(user, "stationsdak") || !strings.Contains(user, "volgende week stemmen we") {
		t.Errorf("het volledige venster bereikte de herformulering niet:\n%s", user)
	}
	if !strings.HasSuffix(user, "Is dat echt een probleem? Ik heb nog nooit een vogel uit de lucht zien vallen.") {
		t.Errorf("het anker is niet de laatste vraag aan tafel:\n%s", user)
	}
}
