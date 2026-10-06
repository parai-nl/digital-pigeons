package duif

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"
)

// De echo-zeef heeft twee poorten. De tijdpoort vraagt of de duif op dit
// moment werkelijk hoorbaar is; de tekstpoort of wat er binnenkomt op haar
// woorden lijkt. Pas samen zetten ze een segment buiten de vergadering.
//
// Vroeger was er alleen de tijdpoort, en die stond de hele spreekbeurt open
// — twintig, dertig seconden. Alles wat een mens in die tijd zei verdween
// definitief uit het venster, inclusief de vraag die aan de duif gericht was.
// De browser doet zelf al echoCancellation (frontend/src/lib/audio/browser.ts);
// wat hier overblijft hoeft alleen nog de echo te vangen die daar doorheen komt.
func (s *meetingSession) echoLocked(now time.Time, text string) bool {
	return s.echoPossibleLocked(now) && s.echoTextLocked(text)
}

// echoPossibleLocked is de tijdpoort: gaat er op dit moment geluid van de duif
// de kamer in? Een gereserveerd generatieslot is dat niet — vandaar
// playback.expected, dat pas op het eerste frame aan gaat — en na het laatste
// frame loopt de marge door (speakUntil) omdat de STT haar laatste zin
// pas een paar tellen later aflevert.
//
// Hier stond eerst playback.started (de client meldt wanneer de eerste samples
// de luidspreker in gaan), om de paar honderd milliseconden tussen frame en
// geluid uit te sparen. Dat is teruggedraaid: die melding is
// een POST van de client, en mislukt hij (netwerk, playbackSyncFailed) dan
// staat de zeef een hele spreekbeurt lang uit terwijl het geluid gewoon speelt.
// Het onderscheid dat het werk doet zit in de tekstpoort; de tijdpoort hoeft
// alleen ruw te zijn en mag niet van een bevestiging afhangen.
func (s *meetingSession) echoPossibleLocked(now time.Time) bool {
	return (s.speaking && s.playback.expected) || now.Before(s.speakUntil)
}

// echoTextLocked is de tekstpoort: komt dit segment terug uit wat de duif net
// zei? Een echte echo levert haar zin vrijwel woordelijk op; een mens die haar
// onderbreekt zegt iets anders en hoort gewoon mee te tellen.
//
// Drie uitzonderingen op de weging:
//
//   - Is er geen duiftekst (de signalerende duif heeft er geen, en vóór haar
//     eerste woord is er nog niets gezegd) of geen segmenttekst, dan blijft de
//     tijdpoort het enige oordeel — de veilige kant.
//   - Haalt het segment haar aan ("je zei net dat…"), dan is het per definitie
//     een mens: zij citeert zichzelf niet.
//   - Is het segment te kort voor een zinnig aandeel, dan telt geen aandeel
//     maar volledige dekking: élk woord moet uit haar tekst komen. Zo'n kort
//     segment blijft dus bewust in het venster zolang het niet helemaal van
//     haar is: "Klopt dat wel?" telt mee, "bij het
//     dorpsbeeld" uit haar eigen zin niet. Een deel volstaat hier niet: tegen
//     een hele beurt zei zij ergens wel "dat is" of "kost geld", en dan zouden
//     "dat is waar" en "dat kost geld" op twee derde dekking alsnog sneuvelen.
//     Alleen zonder duiftekst houdt de tijdpoort het laatste woord.
func (s *meetingSession) echoTextLocked(text string) bool {
	segment := echoTokens(text)
	spoken := echoTokens(s.spokenTail)
	if len(spoken) == 0 || len(segment) == 0 {
		return true
	}
	if echoQuotesThePigeon(segment) {
		return false
	}
	overlap := echoOverlap(segment, spoken)
	if len(segment) < echoMinSegmentTokens {
		return overlap >= 1
	}
	return overlap >= echoSimilarityThreshold
}

// echoQuoteMarkers zijn de openingen waarmee iemand aan tafel de duif aanhaalt.
// Wie haar citeert neemt haar woorden over — de overlap loopt dan op tot 0,8 —
// maar stelt er zijn eigen vraag bij, en die vraag hoort juist in het venster
// te blijven. De tokens staan genormaliseerd zoals echoTokens ze maakt,
// dus "you're" is {"you", "re"}.
//
// De keerzijde is bewust: begint de duif zélf een zin met "je zei", dan glipt de
// echo daarvan door de zeef en staat er één regel van haar in het venster. Dat
// is een mildere fout dan een vraag die definitief verdwijnt.
//
// De omgekeerde woordvolgorde hoort er net zo goed bij: wie navraag doet,
// begint even vaak met de vraagvorm ("Zei jij net dat…", "Did you say…") als
// met de mededeling.
var echoQuoteMarkers = [][]string{
	{"je", "zei"}, {"jij", "zei"}, {"je", "zegt"}, {"jij", "zegt"},
	{"je", "noemde"}, {"jij", "noemde"}, {"u", "zei"}, {"u", "zegt"},
	{"zei", "je"}, {"zei", "jij"}, {"zei", "u"},
	{"zeg", "je"}, {"zeg", "jij"}, {"zegt", "u"}, {"noemde", "je"},
	{"volgens", "jou"}, {"volgens", "jouw"}, {"volgens", "jullie"},
	{"you", "said"}, {"you", "just", "said"}, {"you", "say"},
	{"you", "mentioned"}, {"you", "re", "saying"}, {"you", "are", "saying"},
	{"you", "were", "saying"}, {"did", "you", "say"}, {"are", "you", "saying"},
	{"were", "you", "saying"}, {"according", "to", "you"},
}

// echoQuoteLeadTokens is hoe ver in de zin zo'n opening nog als opening telt:
// ruimte voor "maar je zei net…" en "wacht, volgens jou…", niet voor een
// aanhaling halverwege.
const echoQuoteLeadTokens = 4

func echoQuotesThePigeon(segment []string) bool {
	for start := 0; start < len(segment) && start < echoQuoteLeadTokens; start++ {
		for _, marker := range echoQuoteMarkers {
			if start+len(marker) <= len(segment) && slices.Equal(segment[start:start+len(marker)], marker) {
				return true
			}
		}
	}
	return false
}

const (
	// echoSimilarityThreshold is de lat van de tekstpoort: welk deel van het
	// segment aaneengesloten terug moet komen in wat de duif zei voordat het
	// als haar eigen stem geldt. Dit is de knop om aan te draaien als er in de
	// praktijk echo doorglipt (verlagen) of als er vragen blijven sneuvelen
	// (verhogen).
	//
	// Gemeten op 20 september 2026 tegen een eigen server met Soniox-STT. De
	// spraak ging als PCM16 rechtstreeks naar POST .../audio: geen microfoon,
	// geen kamer, en dus ook niet de echoCancellation van de browser die er in
	// het echt vóór zit. Wat gemeten is, is de zeef zelf, niet de akoestiek.
	//   - haar eigen zin terug de invoer in: 0,93, inclusief één verhoord woord
	//     ("levendeel" voor "leeuwendeel") en een bronmarkering die als los
	//     getal terugkwam;
	//   - een vraag van een mens tijdens diezelfde beurt ("Wat betekent dat
	//     voor de bewoners van de Rijksweg?"): 0,22;
	//   - vijf menselijke zinnen tegen een hele beurt als staart: 0,00–0,31.
	// De gevoeligheid voor een slechter verstaan echo, gerekend op diezelfde
	// zin van 27 woorden met gelijkmatig verdeelde fouten: elk vijfde woord
	// fout geeft 0,81, elk derde 0,67 en elk tweede 0,00 — dan overleeft geen
	// enkel stukje van twee woorden. Op een kortere zin wegen dezelfde
	// percentages zwaarder; een tweede meting gaf daar 0,69 bij ~20% en
	// 0,46 bij ~30%. Rond de lat hangt het dus van de zinslengte af, en een
	// zwaar verhoorde echo glipt erdoor. Dat is de prijs voor een zeef die een
	// vraag aan de duif laat staan.
	echoSimilarityThreshold = 0.6
	// echoMinRunTokens telt alleen aaneengesloten stukjes mee. Losse woorden
	// zeggen niets: "de", "en" en "dat" staan in elke zin. Pas twee woorden op
	// rij zijn een stukje duif.
	echoMinRunTokens = 2
	// echoMinSegmentTokens is de grens waaronder een aandeel niets meer zegt:
	// in "ja precies" weegt elk woord een half, en één toevallig stukje van
	// twee woorden haalt al twee derde. Daaronder geldt daarom volledige
	// dekking: alleen een segment dat hélemaal uit haar tekst komt, is haar
	// stem.
	echoMinSegmentTokens = 4
	// echoSpokenTailRunes begrenst de bewaarde duiftekst. Hij moet een hele
	// beurt kunnen dekken: de tekst is binnen seconden compleet terwijl de
	// luidspreker nog bij de eerste zin staat, dus een staart die korter is dan
	// de beurt vergelijkt de echo van zin één met tekst die er niet meer is
	// (overlap 0,00). Een
	// vergaderbeurt is begrensd op answerMeetingVisibleTokens (400 ≈ 1400–2400
	// tekens); 4000 ligt daar ruim boven en is verder alleen een noodrem tegen
	// een stroom die niet ophoudt. Het blijft een venster, geen archief — wat
	// de duif zei staat al in s.contributions.
	echoSpokenTailRunes = 4000
)

// echoTokens maakt van een zin een rij vergelijkbare woorden. De STT levert
// andere leestekens en hoofdletters dan de tekstgenerator; alleen de woorden
// zelf zijn betrouwbaar.
func echoTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// echoOverlap geeft welk deel van segment terugkomt als aaneengesloten stukje
// van minstens echoMinRunTokens woorden in spoken.
//
// Waarom niet gewoon woorden tellen (Jaccard): de twee teksten zijn heel
// ongelijk van lengte. Eén zin tegen honderd woorden duif geeft altijd een
// lage Jaccard-score, ook bij een perfecte echo. En andersom telt losse
// woordoverlap een gewone Nederlandse zin al half mee. Aaneengesloten stukjes
// meten wat echo werkelijk is: dezelfde woorden in dezelfde volgorde. Eén
// afwijking (de duif schrijft "3 bronnen", haar stem zegt "drie bronnen")
// breekt het stukje, maar laat de rest van de zin gewoon meetellen.
func echoOverlap(segment, spoken []string) float64 {
	covered := make([]bool, len(segment))
	// prev/cur zijn twee rijen van de klassieke tabel voor de langste
	// gemeenschappelijke deelreeks: cur[j+1] is de lengte van het stukje dat
	// eindigt op segment[i] én spoken[j].
	prev := make([]int, len(spoken)+1)
	cur := make([]int, len(spoken)+1)
	for i := range segment {
		longest := 0
		for j := range spoken {
			if segment[i] == spoken[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = 0
			}
			if cur[j+1] > longest {
				longest = cur[j+1]
			}
		}
		if longest >= echoMinRunTokens {
			for k := i - longest + 1; k <= i; k++ {
				covered[k] = true
			}
		}
		prev, cur = cur, prev
	}
	var n int
	for _, c := range covered {
		if c {
			n++
		}
	}
	return float64(n) / float64(len(segment))
}

// Een nog onhoorbare zelfstandige bijdrage vervalt alleen als er werkelijk iets
// nieuws gezegd is: één gecommit menselijk segment ná de aanleiding. Niet bij
// een partial, niet op een klok. Dat verschil is het verschil tussen een duif
// die haar zin afmaakt en een duif die bij elk kuchje afbreekt.
func (s *meetingSession) unheardAutomaticExpiredLocked() bool {
	if !s.speaking || !s.turnAutomatic || s.playback.expected {
		return false
	}
	return s.lastHumanSeq > s.turnTriggerSeq
}

func (s *meetingSession) expireUnheardAutomatic() {
	s.mu.Lock()
	var cancel context.CancelFunc
	if !s.closed && s.unheardAutomaticExpiredLocked() {
		cancel = s.cancelUnheardAutomaticLocked("context_changed")
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// This is not a user Stop: autonomy stays in its current state. The new turn
// barrier rejects every old text/audio callback before releasing the lock.
// Return cancellation so providers are notified after unlocking. Admission
// counters/cooldowns remain consumed to bound repeated costly starts.
func (s *meetingSession) cancelUnheardAutomaticLocked(reason string) context.CancelFunc {
	if !s.speaking || !s.turnAutomatic || s.playback.expected {
		return nil
	}
	cancel := s.cancelTurn
	s.cancelTurn = nil
	s.speaking = false
	s.turnAutomatic = false
	s.turnGen++
	s.playback = playbackTracker{}
	s.diagnostics.mark(s.clock.Now(), "generation_superseded", reason, 0)
	s.dispatchLocked(MeetingEvent{Type: EventStopped, Listening: s.listen, AutonomyPaused: boolPointer(s.autonomyPaused), Reason: reason, TurnID: s.turnIDLocked()})
	settings := s.settingsLocked()
	s.dispatchLocked(MeetingEvent{Type: EventSettings, Settings: &settings})
	return cancel
}
