package duif

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// De tekstpoort van de echo-zeef: lijkt dit segment
// op wat de duif net zei? Alleen dan is het haar eigen stem die terugkomt.
func TestMeetingEcho_TextGateSeparatesEchoFromSpeech(t *testing.T) {
	const spokenNL = "De duiven op het Remigiusplein horen al jaren bij het dorpsbeeld."
	const spokenEN = "The pigeons on the square have been part of the village for years."
	// Een hele beurt staat in de staart, en hoe langer die is, hoe groter de
	// kans dat een alledaags stukje van twee woorden er toevallig in voorkomt.
	const spokenTurn = spokenNL + " Jullie kunnen ons verjagen met netten en pinnen, " +
		"maar dat is duur onderhoud en het kost geld dat jullie liever aan de winkelstraat besteden."

	for _, tc := range []struct {
		name    string
		spoken  string
		segment string
		want    bool
	}{
		{"woordelijke echo", spokenNL, "de duiven op het remigiusplein horen al jaren bij het dorpsbeeld", true},
		{"echo met een gaatje erin", spokenNL, "de duiven op het remigiusplein horen al jaren bij dorpsbeeld", true},
		{"andere zin tijdens haar beurt", spokenNL, "wat vind jij van het voerverbod voor de bewoners", false},
		{"vraag die één begrip van haar leent", spokenNL, "kunnen we iets doen aan de duiven op het plein hier", false},
		{"verbatim echo in english", spokenEN, "the pigeons on the square have been part of the village for years", true},
		{"a question during her turn", spokenEN, "could you say something about that for the neighbours", false},
		// Citeren is geen echo: de mens neemt haar woorden over en stelt er zijn
		// eigen vraag bij. Zonder de aanhaling haalt zo'n zin 0,6 tot 0,8 en zou
		// hij dus verdwijnen — juist de zin die de zeef hoort te bewaren.
		{"mens haalt haar aan", spokenNL, "je zei net dat de duiven op het remigiusplein al jaren bij het dorpsbeeld horen", false},
		{"mens haalt haar aan met aanloop", spokenNL, "maar volgens jou horen de duiven op het remigiusplein al jaren bij het dorpsbeeld", false},
		{"quoting her in english", spokenEN, "you said the pigeons on the square have been part of the village for years", false},
		// Navraag komt net zo vaak in de vraagvorm als in de mededeling.
		{"mens vraagt het na in de vraagvorm", spokenNL, "zei jij net dat de duiven op het remigiusplein bij het dorpsbeeld horen", false},
		{"asking her back in english", spokenEN, "did you say the pigeons on the square have been part of the village", false},
		// Korte segmenten blijven bewust in het venster zolang ze niet hélemaal
		// van haar zijn. Te kort voor een aandeel:
		// één toevallig stukje van twee woorden haalt al twee derde.
		{"kort vervolg op een citaat blijft", spokenNL, "klopt dat wel", false},
		{"kort en van niemand blijft", spokenNL, "ja precies", false},
		{"kort en half toevallig blijft", spokenTurn, "dat is waar", false},
		{"kort en half toevallig blijft ook in het duurdere geval", spokenTurn, "dat kost geld", false},
		// En andersom: kort én woordelijk uit haar zin is wel haar stem.
		{"kort flardje uit haar eigen zin", spokenNL, "bij het dorpsbeeld", true},
		{"kort flardje uit haar eigen zin, ander stuk", spokenNL, "op het remigiusplein", true},
		{"kort flardje uit een hele beurt", spokenTurn, "met netten en pinnen", true},
		{"leeg segment", spokenNL, "", true},
		{"zij heeft nog niets gezegd", "", "wat vind jij van het voerverbod voor de bewoners", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &meetingSession{spokenTail: tc.spoken}
			if got := s.echoTextLocked(tc.segment); got != tc.want {
				t.Errorf("echoTextLocked(%q) = %v, want %v (overlap %.2f)",
					tc.segment, got, tc.want, echoOverlap(echoTokens(tc.segment), echoTokens(tc.spoken)))
			}
		})
	}
}

// Losse woorden zijn geen echo: een gewone Nederlandse zin deelt "de", "op" en
// "het" met elke andere. Pas aaneengesloten stukjes tellen mee.
func TestMeetingEcho_OverlapCountsRunsNotLooseWords(t *testing.T) {
	spoken := echoTokens("De duiven op het plein zijn er al jaren en dat blijft zo")
	if got := echoOverlap(echoTokens("wie gaat dat betalen"), spoken); got != 0 {
		t.Errorf("overlap zonder gemeenschappelijke woorden = %.2f, want 0", got)
	}
	if got := echoOverlap(echoTokens("de gemeente zegt op het moment niets"), spoken); got >= echoSimilarityThreshold {
		t.Errorf("losse woordoverlap = %.2f, dat is boven de lat %.2f", got, echoSimilarityThreshold)
	}
}

func lastSegmentPigeonWindow(t *testing.T, s *meetingSession) bool {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.transcript.segments) == 0 {
		t.Fatal("er staat geen segment in het venster")
	}
	return s.transcript.segments[len(s.transcript.segments)-1].PigeonWindow
}

// De tijdpoort loopt van het eerste audioframe tot de marge na de beurt, en
// hangt uitdrukkelijk níét aan de afspeelbevestiging van de client: die is een
// POST die kan mislukken, en dan zou de zeef een hele spreekbeurt lang uitstaan
// terwijl het geluid gewoon speelt. Het onderscheid komt van de tekstpoort.
func TestMeetingEcho_TimeGateRunsFromTheFirstFrameWithoutAnAck(t *testing.T) {
	const spoken = "The pigeons on the square have been part of the village for years."
	const echo = "the pigeons on the square have been part of the village for years"
	const human = "could you say something about that for the neighbours"

	_, s, clock := trackedSession(t)
	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("begin failed")
	}
	defer cancel()
	id := strconv.FormatUint(gen, 10)
	s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: spoken})

	// Nog geen frame: de duif schrijft, er klinkt niets.
	s.appendSegment(echo, "")
	if lastSegmentPigeonWindow(t, s) {
		t.Error("een segment vóór het eerste audioframe werd als echo weggezet")
	}
	// Eerste frame verstuurd. De client bevestigt hier niets — de bevestiging
	// raakte onderweg kwijt — en toch hoort haar eigen zin gemarkeerd te worden.
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})
	s.appendSegment(echo, "")
	if !lastSegmentPigeonWindow(t, s) {
		t.Error("haar eigen zin kwam tijdens het afspelen ongemarkeerd binnen")
	}
	s.appendSegment(human, "")
	if lastSegmentPigeonWindow(t, s) {
		t.Error("een menselijke zin tijdens het afspelen werd als echo weggezet")
	}
	// Komt de bevestiging alsnog, dan verandert er niets aan dat oordeel.
	if !s.ackPlayback(id, "started") {
		t.Fatal("playback started geweigerd")
	}
	s.appendSegment(echo, "")
	if !lastSegmentPigeonWindow(t, s) {
		t.Error("haar eigen zin werd na de afspeelbevestiging niet meer gemarkeerd")
	}

	// Na het laatste frame loopt de marge door: de STT levert de
	// laatste woorden later af dan ze klonken.
	s.emitTurn(gen, MeetingEvent{Type: EventTTSDone, Frames: 1})
	if !s.ackPlayback(id, "ended") {
		t.Fatal("playback ended geweigerd")
	}
	s.endTurn(gen)
	if s.isSpeaking() {
		t.Fatal("het spreekslot bleef na het einde van de weergave bezet")
	}
	s.appendSegment(echo, "")
	if !lastSegmentPigeonWindow(t, s) {
		t.Error("een echo binnen de marge na de beurt telde als vergaderinhoud")
	}
	clock.skip(s.cfg.SpeakMargin + time.Millisecond)
	s.appendSegment(echo, "")
	if lastSegmentPigeonWindow(t, s) {
		t.Error("na de marge hoort niets meer als echo weggezet te worden")
	}
}

// De staart moet een hele beurt dekken. De tekst van de duif is binnen seconden
// compleet terwijl de luidspreker nog bij haar eerste zin staat, dus juist die
// eerste zin komt als laatste terug door de microfoon. Met een staart die
// korter was dan de beurt vergeleek de zeef die echo met tekst die er niet meer
// stond: overlap 0,00, en de echo glipte erdoor.
func TestMeetingEcho_TailCoversAWholeTurn(t *testing.T) {
	const opening = "De duiven op het Remigiusplein horen al jaren bij het dorpsbeeld en niemand keek daar ooit van op."
	rest := strings.Repeat(
		"Jullie schuiven ons telkens door naar een andere straat alsof het probleem daarmee verdwijnt, "+
			"maar wij zoeken gewoon de kruimels op waar jullie ze laten liggen. ", 8)

	_, s, _ := trackedSession(t)
	gen, _, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("begin failed")
	}
	defer cancel()
	// Zoals een echte beurt: de tekst stroomt in stukjes binnen, ver voor het
	// geluid uit. Een vergaderbeurt is begrensd op answerMeetingVisibleTokens.
	s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: opening})
	for chunk := range strings.SplitSeq(rest, " ") {
		s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: chunk + " "})
	}
	if n := len([]rune(opening + rest)); n < 1200 {
		t.Fatalf("de proefbeurt is maar %d tekens; dat toetst de staart niet", n)
	}
	s.emitTurn(gen, MeetingEvent{Type: EventTTSAudio, Audio: "synthetic", Seq: 1})

	s.appendSegment("de duiven op het remigiusplein horen al jaren bij het dorpsbeeld en niemand keek daar ooit van op", "")
	if !lastSegmentPigeonWindow(t, s) {
		t.Error("de echo van haar eerste zin viel buiten de bewaarde staart")
	}
	s.appendSegment("kunnen we hier als raad eigenlijk iets mee opschieten", "")
	if lastSegmentPigeonWindow(t, s) {
		t.Error("een menselijke zin werd tegen de volle beurt alsnog als echo weggezet")
	}
}
