package duif

// De lege knopdruk. Iemand drukt op "Laat de duif reageren" terwijl er sinds
// haar vorige bijdrage aan tafel niets nieuws gezegd is. Sinds de duif haar
// eigen bijdragen terugziet, herhaalt ze zich niet meer; de tweede knopdruk werd
// daardoor vanzelf een aanvulling op haar eigen punt, en de derde een volle
// modelronde plus stem voor "ik heb niets nieuws toe te voegen".
//
// Wat de duif dan doet:
//
//  1. Eén korte verdieping op haar eigen vorige punt, maar alleen als dat punt
//     op bronnen rustte: één of twee zinnen, expliciet als vervolg op wat zij
//     zelf zei, met dezelfde bronnen als toen. Die bronnen worden niet opnieuw
//     gezocht: het menselijke venster is hetzelfde, en zo is het ook letterlijk
//     háár bronnenset, zonder extra herformuleringsronde.
//  2. Anders — haar punt had geen bronnen, of ze heeft het al één keer
//     verdiept — een vaste, eerlijke zin zonder modelronde. Een verdieping zonder
//     bron is opvulling, en de tweede verdieping op hetzelfde stille venster
//     ook; de uitkomst "niets nieuws" stond toch al vast.
//
// Wat er "nieuw" is, beslist de server en niet het model: een menselijk segment
// in het venster met een hoger volgnummer dan alles wat zij had gehoord toen zij
// haar vorige bijdrage schreef.

// ownPoint is de laatste reeks eigen bijdragen op hetzelfde menselijke venster:
// wat zij toen gehoord had, hoe vaak zij daarop al sprak, en waar de eerste
// bijdrage van die reeks op rustte. Het leeft in het geheugen van de sessie en
// gaat bij een duifwissel niet mee: de nieuwe duif heeft zelf nog niets gezegd.
// Komt een duif terug aan deze tafel, dan wordt het afgeleid uit haar eigen
// overgenomen regels (inheritedPoint), zonder grond.
type ownPoint struct {
	// heard is het hoogste volgnummer van een menselijk segment in het venster
	// waarop zij sprak.
	heard uint64
	// count is het aantal eigen bijdragen op dat venster; nul is "nog niets".
	count int
	// ground is de grond van de eerste bijdrage, alleen gezet als die bijdrage
	// werkelijk een bron citeerde.
	ground   ownGrounding
	grounded bool
}

// ownGrounding is waar een bijdrage op rustte: de stand van de kennisbank, de
// bronnen en de bronnensectie van die beurt, in de nummering van toen.
type ownGrounding struct {
	Status     KBStatus
	Sources    []Source
	RAGSection string
}

// quietTurn is wat een knopdruk wordt als de leegtetoets al gepasseerd is.
type quietTurn int

const (
	// quietNo: er is iets nieuws gezegd, of zij sprak nog niet — een gewone beurt.
	quietNo quietTurn = iota
	// quietDeepen: niets nieuws, en haar vorige punt rustte op bronnen.
	quietDeepen
	// quietNothingNew: niets nieuws en niets om met bronnen te verdiepen.
	quietNothingNew
)

// meetingNothingNewReply is de vaste zin van quietNothingNew. Hij zegt alleen
// wat de server weet: er kwam niets nieuws binnen sinds haar vorige bijdrage.
var meetingNothingNewReply = map[string]string{
	"en": "Coo. I have not heard anything new since my last contribution, and I have nothing to add to my own point right now.",
	"nl": "Roekoe. Sinds mijn vorige bijdrage heb ik niets nieuws gehoord, en aan mijn eigen punt heb ik nu niets toe te voegen.",
}

// meetingDeepenDirective komt achter het vergaderframe als de knop een
// verdieping wordt. Het frame zelf zegt "reageer op wat er gezegd is"; dit
// zegt dat er niets nieuws gezegd is en wat ze dan wél doet.
var meetingDeepenDirective = map[string]string{
	"en": "Nobody at the table has said anything new since your last contribution, and someone pressed the button anyway. Do not answer the transcript again and do not act as if someone just said something. Go one step deeper on your own most recent point instead: in one or two short spoken sentences, make one part of it more concrete with something from the sources below that you have not said yet, and cite it. Make it explicit that you are coming back to your own point, for example by starting with \"To add to my own point:\". Never repeat what you already said. If the sources hold nothing beyond what you already said, say only that you have nothing new to add.",
	"nl": "Aan tafel is sinds je vorige bijdrage niets nieuws gezegd, en toch drukte iemand op de knop. Beantwoord het transcript niet opnieuw en doe niet alsof iemand zojuist iets zei. Ga in plaats daarvan één stap dieper op je eigen laatste punt: maak in één of twee korte gesproken zinnen één deel ervan concreter met iets uit de bronnen hieronder dat je nog niet gezegd hebt, en citeer het. Maak duidelijk dat je terugkomt op je eigen punt, bijvoorbeeld door te beginnen met \"Om op mijn eigen punt door te gaan:\". Herhaal nooit wat je al zei. Staat er in de bronnen niets meer dan wat je al zei, zeg dan alleen dat je niets nieuws toe te voegen hebt.",
}

// lastHumanSeq is het hoogste volgnummer van een menselijk segment: een regel
// van een duif — van haarzelf of van een duif die hier eerder zat — is geen
// nieuwe inbreng van de tafel, en wat de microfoon opving terwijl de duif zelf
// klonk ook niet (het venster laat dat al weg; de overdracht niet).
func lastHumanSeq(segs []TranscriptSegment) uint64 {
	var last uint64
	for _, s := range segs {
		if s.PigeonID == "" && !s.PigeonWindow && s.Seq > last {
			last = s.Seq
		}
	}
	return last
}

// inheritedPoint leidt de reeks eigen bijdragen af uit een overgenomen
// transcript: de regels van deze duif ná het laatste menselijke segment. Een
// duif die voor het eerst aan deze tafel komt heeft er geen, en begint leeg.
func inheritedPoint(segs []TranscriptSegment, pigeonID string) ownPoint {
	heard := lastHumanSeq(segs)
	n := 0
	for _, s := range segs {
		if s.PigeonID == pigeonID && s.Seq > heard {
			n++
		}
	}
	if n == 0 {
		return ownPoint{}
	}
	return ownPoint{heard: heard, count: n}
}

// notePointLocked boekt een eigen bijdrage in de reeks. Sprak zij opnieuw op
// hetzelfde venster, dan telt de reeks door en blijft de grond van de eerste
// bijdrage staan; anders begint er een nieuwe reeks. De aanroeper houdt s.mu.
func (s *meetingSession) notePointLocked(text string, heard uint64, ground ownGrounding) {
	if s.lastPoint.count > 0 && heard <= s.lastPoint.heard {
		s.lastPoint.count++
		return
	}
	s.lastPoint = ownPoint{heard: heard, count: 1}
	// Een bijdrage die geen enkele bron citeerde rust niet op die bronnen, ook
	// als ze er wel lagen: dan valt er niets met bronnen te verdiepen.
	if ground.Status == KBSources && ground.RAGSection != "" && len(ground.Sources) > 0 && reCitation.MatchString(text) {
		s.lastPoint.ground = ownGrounding{
			Status:     ground.Status,
			Sources:    append([]Source(nil), ground.Sources...),
			RAGSection: ground.RAGSection,
		}
		s.lastPoint.grounded = true
	}
}

// quietSinceOwnPoint beslist wat een knopdruk op dit venster wordt.
func (s *meetingSession) quietSinceOwnPoint(window []TranscriptSegment) (quietTurn, ownGrounding) {
	heard := lastHumanSeq(window)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.lastPoint
	switch {
	case p.count == 0 || heard > p.heard:
		return quietNo, ownGrounding{}
	case p.count == 1 && p.grounded:
		return quietDeepen, p.ground
	default:
		return quietNothingNew, ownGrounding{}
	}
}
