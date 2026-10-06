package duif

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/audio"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De lege knopdruk. Eén uitspraak aan tafel, dan drie keer de knop
// zonder dat er iets nieuws gezegd wordt. De eerste beurt antwoordt met een
// bron; de tweede verdiept dat eigen punt op dezelfde bronnen, zonder opnieuw te
// zoeken; de derde is de vaste zin, zonder modelronde.
func TestMeetingRespondNow_PressesWithoutNewInput_DeepenOnceThenSayNothingNew(t *testing.T) {
	for _, lang := range []string{"nl", "en"} {
		t.Run(lang, func(t *testing.T) {
			gllm := newGatedLLM("Weren verplaatst het probleem [1].")
			knowledge := &fakeKnowledge{}
			seedDestillaat(t, knowledge, kbSeed{
				id: "1", samenvatting: "Duiven in de stad zijn een menselijk probleem.",
				score: 0.9, quote: "Wij hebben ze zelf hierheen gehaald.", paper: "jerolmack2008",
				perspective: "De kritische wetenschapper", themaTitel: "Overlast",
			})
			rig := newMeetingRig(t, func(m *Module) {
				m.llm = gllm
				m.embedder = &llmtesting.MockEmbedder{}
				m.knowledge = knowledge
			}, nil)

			id := rig.openSession(t, "biological", lang)
			es := rig.events(t, id)
			rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
			nextEvent(t, es.ch, EventTranscript)

			press := func() []MeetingEvent {
				t.Helper()
				if status, body := rig.respond(t, id); status != http.StatusAccepted {
					t.Fatalf("respond: status = %d, body = %s", status, body)
				}
				nextEvent(t, es.ch, EventPersona)
				_, evs := collectSpokenText(t, es.ch)
				return evs
			}
			sourcesOf := func(evs []MeetingEvent) []Source {
				for _, ev := range evs {
					if ev.Type == EventResponseDone {
						return ev.Sources
					}
				}
				return nil
			}

			// Eerste knopdruk: de gewone beurt, met zoeken en een bron.
			eerste := press()
			if sys := gllm.lastCall(t).Messages[0].Content; strings.Contains(sys, meetingDeepenDirective[lang]) {
				t.Fatalf("de eerste beurt draagt al de verdieping:\n%s", sys)
			}
			if len(sourcesOf(eerste)) != 1 {
				t.Fatalf("de eerste beurt hoorde één bron te dragen: %v", eventTypes(eerste))
			}
			queries, kb := len(gllm.queryCalls()), knowledge.callCount()

			// Tweede knopdruk, niets nieuws gezegd: één verdieping op haar
			// eigen punt, met haar bronnen van toen.
			tweede := press()
			if n := gllm.callCount(); n != 2 {
				t.Fatalf("%d beurtcalls na de tweede knopdruk, want 2", n)
			}
			call := gllm.lastCall(t)
			if sys := call.Messages[0].Content; !strings.Contains(sys, meetingDeepenDirective[lang]) {
				t.Errorf("de tweede beurt mist de verdieping:\n%s", sys)
			}
			if sys := call.Messages[0].Content; !strings.Contains(sys, citationInstruction[lang]) {
				t.Errorf("de verdieping hoort op bronnen te rusten:\n%s", sys)
			}
			// Haar vorige punt staat in het venster, als het hare.
			s, _ := rig.engine.session(id, testUser.ID)
			if user := call.Messages[1].Content; !strings.Contains(user, ownContributionMark(lang, s.markToken)) {
				t.Errorf("haar eigen punt staat niet gemerkt in het venster:\n%s", user)
			}
			if got := len(gllm.queryCalls()); got != queries {
				t.Errorf("de verdieping herformuleerde opnieuw (%d → %d)", queries, got)
			}
			if got := knowledge.callCount(); got != kb {
				t.Errorf("de verdieping zocht opnieuw in de kennisbank (%d → %d)", kb, got)
			}
			if !reflect.DeepEqual(sourcesOf(tweede), sourcesOf(eerste)) {
				t.Errorf("de verdieping draagt andere bronnen:\neerste = %+v\ntweede = %+v", sourcesOf(eerste), sourcesOf(tweede))
			}
			// De regel "vergaderbeurt klaar" komt pas ná response_done.
			waitFor(t, func() bool { return strings.Contains(rig.logs(), "verdieping=true") },
				"het log noemt de verdieping niet")

			// Derde knopdruk: de vaste zin, zonder modelronde.
			derde := press()
			if text := textOf(derde); text != meetingNothingNewReply[lang] {
				t.Errorf("tekst = %q, want de vaste zin", text)
			}
			if n := gllm.callCount(); n != 2 {
				t.Errorf("de derde knopdruk kostte een modelronde (%d beurtcalls)", n)
			}
			if got := len(gllm.queryCalls()); got != queries {
				t.Errorf("de derde knopdruk herformuleerde (%d → %d)", queries, got)
			}
			if !strings.Contains(rig.logs(), "oorzaak=niets_nieuw") {
				t.Errorf("het log noemt de oorzaak niet:\n%s", rig.logs())
			}
			// En dat blijft zo: de vaste zin is geen punt om op voort te bouwen.
			if text := textOf(press()); text != meetingNothingNewReply[lang] {
				t.Errorf("vierde knopdruk: tekst = %q, want weer de vaste zin", text)
			}

			// Zegt iemand wél iets, dan is het weer een gewone beurt.
			rig.say(t, id, "Maar waar gaan ze dan heen?")
			nextEvent(t, es.ch, EventTranscript)
			press()
			if n := gllm.callCount(); n != 3 {
				t.Fatalf("%d beurtcalls na nieuwe inbreng, want 3", n)
			}
			if sys := gllm.lastCall(t).Messages[0].Content; strings.Contains(sys, meetingDeepenDirective[lang]) {
				t.Errorf("na nieuwe inbreng hoort er geen verdieping te staan:\n%s", sys)
			}
			if got := len(gllm.queryCalls()); got != queries+1 {
				t.Errorf("na nieuwe inbreng hoort er weer gezocht te worden (%d → %d)", queries, got)
			}
		})
	}
}

// Rustte haar punt niet op een bron, dan valt er niets met bronnen te
// verdiepen: de tweede knopdruk is meteen de vaste zin, zonder modelronde.
func TestMeetingRespondNow_UngroundedPointWithoutNewInput_SaysNothingNew(t *testing.T) {
	gllm := newGatedLLM("Roekoe. Weren verplaatst het probleem alleen.")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
	nextEvent(t, es.ch, EventTranscript)

	for range 2 {
		if status, _ := rig.respond(t, id); status != http.StatusAccepted {
			t.Fatal("respond")
		}
		nextEvent(t, es.ch, EventPersona)
		collectSpokenText(t, es.ch)
	}
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	text, _ := collectSpokenText(t, es.ch)
	if n := gllm.callCount(); n != 1 {
		t.Fatalf("%d beurtcalls, want 1: zonder bron en zonder nieuwe inbreng is er geen modelronde", n)
	}
	if text != meetingNothingNewReply["nl"] {
		t.Errorf("tekst = %q, want de vaste zin", text)
	}
}

// Iemand is nog aan het woord als de knop valt, en die zin landt niet op tijd.
// "Niets nieuws gehoord" is dan onwaar en een verdieping gaat aan die spreker
// voorbij: de duif zegt wat ze bij een leeg venster ook zegt.
func TestMeetingRespondNow_NothingNewButSomeoneStillSpeaking_SaysSoInstead(t *testing.T) {
	tr := &fakeTranscriber{}
	gllm := newGatedLLM("Roekoe. Daar weet ik iets van.")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) {
		e.transcriber = tr
		e.sttSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
		e.cfg.FinalizeWait = 100 * time.Millisecond // de provider antwoordt hier nooit
	})
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	stt := tr.waitSession(t, 1)

	rig.say(t, id, "We willen de duiven van het Stationsplein weren.")
	nextEvent(t, es.ch, EventTranscript)
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	collectSpokenText(t, es.ch)

	stt.emit(t, audio.TranscriptionEvent{Type: "partial", Text: "maar ik was nog", IsFinal: false})
	nextEvent(t, es.ch, EventTranscript)
	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	text, _ := collectSpokenText(t, es.ch)
	if text != meetingUnfinishedSpeechReply["nl"] {
		t.Errorf("tekst = %q, want de zin over de onafgemaakte uiting", text)
	}
	if n := gllm.callCount(); n != 1 {
		t.Errorf("%d beurtcalls, want 1", n)
	}
}

// De beslissing zelf, los van de route die de bijdrage leverde (knop of
// zelfstandige bijdrage): wat zij gehoord had en of haar punt een bron citeerde.
func TestMeetingSession_QuietSinceOwnPoint_FollowsWhatSheHeardAndCited(t *testing.T) {
	e, _ := newTestEngine(t, nil, nil)
	s, err := e.open("user-1", "biological", "nl")
	if err != nil {
		t.Fatalf("sessie: %v", err)
	}
	s.appendSegment("We willen de duiven van het Stationsplein weren.", "spreker-1")
	window := s.window()
	if q, _ := s.quietSinceOwnPoint(window); q != quietNo {
		t.Fatalf("zonder eigen bijdrage = %v, want een gewone beurt", q)
	}
	ground := ownGrounding{Status: KBSources, Sources: []Source{{Number: 1, ID: "destillaat:1"}}, RAGSection: "[1] bron"}
	s.noteContribution("Weren verplaatst het probleem [1].", lastHumanSeq(window), ground)
	q, basis := s.quietSinceOwnPoint(s.window())
	if q != quietDeepen || !reflect.DeepEqual(basis, ground) {
		t.Fatalf("na een punt met bron = %v (%+v), want een verdieping op die bron", q, basis)
	}
	// Zonder citaat in de tekst rust het punt niet op de bronnen die er lagen.
	s.appendSegment("En nu?", "spreker-1")
	s.noteContribution("Weren verplaatst het probleem.", lastHumanSeq(s.window()), ground)
	if q, _ := s.quietSinceOwnPoint(s.window()); q != quietNothingNew {
		t.Errorf("na een punt zonder citaat = %v, want de vaste zin", q)
	}
}

// Een zelfstandige bijdrage is net zo goed haar punt. Rustte die op een bron,
// dan verdiept een knopdruk zonder nieuwe inbreng precies díé bijdrage, op de
// bronnen en de bronnensectie van de kandidaat, zonder nieuwe zoekronde.
func TestMeetingIntervention_CandidatePointWithSources_ButtonDeepensOnItsSources(t *testing.T) {
	svc := newCandidateLLM(speak(0.99, "pigeon-rights", "Noise changes how they use rooftops [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{
		"pigeonId": "biological", "autonomous": true, "eagerness": "eager",
	})
	es := rig.events(t, id)
	rig.say(t, id, "wat doet vuurwerk met de daken hier")
	held := waitForCandidate(t, s)
	if held.RAGSection == "" || len(held.Sources) == 0 {
		t.Fatalf("de kandidaat hoorde op bronnen te rusten: %+v", held)
	}

	if status, _ := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Fatal("laat horen werd geweigerd")
	}
	nextEvent(t, es.ch, EventPersona)
	_, kandidaat := collectSpokenText(t, es.ch)
	queries := len(svc.queryCalls())

	if status, _ := rig.respond(t, id); status != http.StatusAccepted {
		t.Fatal("respond")
	}
	nextEvent(t, es.ch, EventPersona)
	_, knop := collectSpokenText(t, es.ch)

	calls := svc.generationCalls()
	last := calls[len(calls)-1]
	if isCandidateRequest(last) {
		t.Fatalf("de knop leverde geen beurtcall")
	}
	sys := last.Messages[0].Content
	if !strings.Contains(sys, meetingDeepenDirective["en"]) {
		t.Errorf("de knop na een zelfstandige bijdrage met bron hoort een verdieping te zijn:\n%s", sys)
	}
	if !strings.Contains(sys, held.RAGSection) {
		t.Errorf("de verdieping rust niet op de bronnensectie van de kandidaat:\n%s", sys)
	}
	if got := len(svc.queryCalls()); got != queries {
		t.Errorf("de verdieping herformuleerde opnieuw (%d → %d)", queries, got)
	}
	doneOf := func(evs []MeetingEvent) MeetingEvent {
		for _, ev := range evs {
			if ev.Type == EventResponseDone {
				return ev
			}
		}
		return MeetingEvent{}
	}
	if got, want := doneOf(knop).Sources, doneOf(kandidaat).Sources; len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Errorf("de verdieping draagt andere bronnen dan de kandidaat:\nkandidaat = %+v\nknop      = %+v", want, got)
	}
}

// Komt een duif terug aan een tafel waar zij al sprak (A → B → A) en is er
// sindsdien niets nieuws gezegd, dan is de knop de vaste zin, zonder modelronde.
// De regel van B is geen nieuwe inbreng van de tafel.
func TestMeetingRespondNow_ReturningPigeonWithoutNewInput_SaysNothingNew(t *testing.T) {
	gllm := newGatedLLM("Roekoe. Weren verplaatst het probleem alleen.")
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, nil)
	press := func(id string) string {
		t.Helper()
		es := rig.events(t, id)
		if status, body := rig.respond(t, id); status != http.StatusAccepted {
			t.Fatalf("respond: status = %d, body = %s", status, body)
		}
		nextEvent(t, es.ch, EventPersona)
		text, _ := collectSpokenText(t, es.ch)
		return text
	}

	a1 := rig.openSession(t, "individualist", "nl")
	rig.say(t, a1, "We willen de duiven van het Stationsplein weren.")
	press(a1)
	b := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "lang": "nl", "handoverFrom": a1})
	rig.session(t, b).noteContribution("Roekoe-van-B. De tellingen zeggen iets anders.", 0, ownGrounding{})
	a2 := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "lang": "nl", "handoverFrom": b})
	calls, queries := gllm.callCount(), len(gllm.queryCalls())

	if text := press(a2); text != meetingNothingNewReply["nl"] {
		t.Errorf("tekst = %q, want de vaste zin", text)
	}
	if gllm.callCount() != calls || len(gllm.queryCalls()) != queries {
		t.Errorf("de terugkerende duif kostte een modelronde (%d → %d beurten, %d → %d herformuleringen)",
			calls, gllm.callCount(), queries, len(gllm.queryCalls()))
	}
}

// De afleiding zelf: alleen haar eigen regels ná het laatste menselijke
// segment tellen, en een duif die voor het eerst aan tafel komt begint leeg.
func TestInheritedPoint_CountsOnlyHerOwnLinesAfterTheLastHumanSegment(t *testing.T) {
	segs := []TranscriptSegment{
		{Seq: 1, Text: "mens"},
		{Seq: 2, Text: "A", PigeonID: "individualist"},
		{Seq: 3, Text: "B", PigeonID: "biological"},
		{Seq: 4, Text: "echo", PigeonWindow: true},
	}
	if got := inheritedPoint(segs, "individualist"); got.count != 1 || got.heard != 1 || got.grounded {
		t.Errorf("terugkerende A = %+v, want één eigen regel na mens 1, zonder grond", got)
	}
	if got := inheritedPoint(segs, "signal"); got.count != 0 {
		t.Errorf("een nieuwe duif = %+v, want leeg", got)
	}
	// Spreekt er daarna een mens, dan is haar oude regel geen vorige bijdrage
	// op dit venster meer.
	segs = append(segs, TranscriptSegment{Seq: 5, Text: "nog een mens"})
	if got := inheritedPoint(segs, "individualist"); got.count != 0 {
		t.Errorf("na nieuwe menselijke inbreng = %+v, want leeg", got)
	}
}
