package duif

import (
	"strconv"
	"strings"
	"time"
)

// De kandidaat: één klaarliggende bijdrage van de duif aan tafel.
//
// Dit vervangt de oude tweetrapsraket (classificeer eerst, schrijf daarna).
// Eén model schrijft doorlopend mee: het leest het venster, de bronnen, de
// persona en wat de duif zelf al gezegd heeft, en begint zijn antwoord met een
// kopregel die zegt of er iets te zeggen valt. Zegt hij `PASS`, dan breken we
// de stroom meteen af en is er geen kandidaat. Zegt hij `SPEAK`, dan is de
// tekst die volgt de bijdrage — al geschreven, klaar om te klinken zodra de
// klok het toestaat.
//
// Twee gevolgen die het oude ontwerp niet had. De duif hoeft bij een stilte
// niets meer te bedenken: de latentie tussen "nu mag het" en het eerste woord
// is de synthese, niet een hele generatie. En "de aanleiding is verouderd" is
// geen afkapping meer maar een vervanging — de volgende evaluatie schrijft een
// nieuwe kandidaat over het nieuwere venster.

// meetingCandidate is wat de duif klaar heeft liggen. Hij leeft in het
// geheugen van de sessie, wordt vervangen door zijn opvolger en sterft met de
// sessie: vergaderinhoud blijft vluchtig en wordt nergens bewaard.
type meetingCandidate struct {
	ID     string
	Text   string
	Status KBStatus
	// StopReason zegt waarom de stroom die deze tekst schreef eindigde, en
	// staat er alleen als dat niet vanzelf spreekt: "truncated" als de
	// provider haar op het tokenplafond sloot, "filtered" als zijn
	// inhoudsfilter dat deed. De bijdrage zelf blijft ongewijzigd — de
	// duif spreekt uit wat zij schreef, ook halverwege afgekapt — maar de
	// beurt meldt het erbij, net als de knop-route. Bij de signalerende duif
	// heeft hij geen betekenis: zij heeft geen tekst en dus geen melding.
	StopReason string
	// Sound markeert de kandidaat van de signalerende duif: er is geen tekst,
	// alleen de wil om een geluid te geven.
	Sound     bool
	Score     float64
	TopicID   string
	Label     LocalName
	Addressed bool
	// At en Seq horen bij het laatste menselijke segment waarop deze kandidaat
	// rust: de klok gebruikt ze om te weten hoe oud hij is.
	At      time.Time
	Seq     uint64
	Sources []Source
	// RAGSection is de bronnensectie waarop de bijdrage geschreven is. Zij gaat
	// nooit de lijn op; een knopdruk zonder nieuwe inbreng verdiept het punt op
	// precies deze bronnen.
	RAGSection string
	// Dropped zijn de gevonden kandidaten die alsnog geen bron werden; zij
	// reizen mee zodat de zaal ze bij de bijdrage ziet en niet
	// bij een evaluatie die zij nooit gezien heeft.
	Dropped *SourceDrops
	// Window is wat de duif hoorde toen zij dit schreef. Het reist mee naar de
	// spreekbeurt zodat het antwoordpad en de bronnen over hetzelfde venster
	// gaan.
	Window []TranscriptSegment
}

// CandidateRef is de kandidaat op de wire. Er staat geen letter transcript en
// geen letter bijdrage in: de vergadering ziet dát de duif iets heeft en
// waarover, niet wat. De tekst blijft op de server tot zij klinkt.
type CandidateRef struct {
	ID    string    `json:"id"`
	Topic *TopicRef `json:"topic,omitempty"`
	Score float64   `json:"score"`
	// Reason is een vaste code, nooit vrije tekst: waarom deze duif iets wil
	// zeggen. "addressed" — iemand sprak haar aan; "topic" — het gaat over een
	// van haar thema's; "signal" — de signalerende duif wil een geluid geven,
	// zonder betekenislabel.
	Reason    string `json:"reason"`
	Addressed bool   `json:"addressed,omitempty"`
	Sources   int    `json:"sources"`
}

// Redenen op de wire.
const (
	candidateReasonAddressed = "addressed"
	candidateReasonTopic     = "topic"
	candidateReasonSignal    = "signal"
)

// ref maakt het wire-beeld van deze kandidaat. De signalerende duif krijgt
// geen thema en geen score mee: aan haar geluid hangt geen menselijke
// betekenis (kerninvariant).
func (c *meetingCandidate) ref() *CandidateRef {
	if c == nil {
		return nil
	}
	out := &CandidateRef{ID: c.ID, Score: c.Score, Addressed: c.Addressed, Sources: len(c.Sources)}
	switch {
	case c.Sound:
		return &CandidateRef{ID: c.ID, Reason: candidateReasonSignal}
	case c.Addressed:
		out.Reason = candidateReasonAddressed
	default:
		out.Reason = candidateReasonTopic
	}
	if c.TopicID != "" {
		out.Topic = &TopicRef{ID: c.TopicID, Label: c.Label}
	}
	return out
}

// --- de kopregel -------------------------------------------------------------

// candidateHeader is de geparseerde eerste regel van een kandidaatantwoord.
type candidateHeader struct {
	Speak     bool
	Score     float64
	TopicID   string
	Addressed bool
}

// headerScanLimit is hoeveel tekens we op de eerste regel wachten. Komt er
// binnen die ruimte geen regelovergang, dan begon het model aan een bijdrage
// zonder kopregel — en dat behandelen we als PASS.
const headerScanLimit = 320

// Diagnosecodes van de parser. Vaste codes, nooit modeltekst: een kopregel kan
// een thema-id dragen dat uit een privétranscript is verzonnen, en
// vergaderinhoud blijft vluchtig.
const (
	headerOK           = ""
	headerNoHeader     = "no_header"
	headerMissingField = "missing_field"
	headerBadScore     = "bad_score"
	headerUnknownTopic = "unknown_topic"
	// reasonCandidateTimeout hoort niet bij de parser maar bij de stroom: de
	// evaluatie liep in haar eigen tijdslimiet en heeft dit stuk gesprek dus
	// níét gelezen.
	reasonCandidateTimeout = "stream_timeout"
)

// parseCandidateHeader leest de kopregel. Hij is met opzet vergevingsgezind op
// vorm en streng op inhoud: extra spaties, hoofdletters, een decimale komma,
// een puntje erachter of een opsommingsstreepje ervoor mogen de bijdrage niet
// kosten, maar een ontbrekende score of thema wél — dan is er geen kandidaat.
//
// Wat er misgaat is nooit een reden om de hele sessie uit te zetten: elke
// afwijking is één PASS met een diagnosecode, en de volgende evaluatie
// probeert het gewoon opnieuw.
func parseCandidateHeader(line string) (candidateHeader, string) {
	s := strings.TrimSpace(line)
	s = strings.TrimLeft(s, "#*->` \t\"'")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return candidateHeader{}, headerNoHeader
	}
	switch strings.ToUpper(strings.Trim(fields[0], ".,:;!*_`\"'")) {
	case "PASS":
		return candidateHeader{}, headerOK
	case "SPEAK":
	default:
		return candidateHeader{}, headerNoHeader
	}

	out := candidateHeader{Speak: true}
	haveScore, haveTopic := false, false
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.Trim(key, " ,;\"'`*_"))
		value = strings.Trim(value, " ,;\"'`*_")
		switch key {
		case "score":
			trimmed := strings.TrimRight(value, ".")
			n, err := strconv.ParseFloat(strings.Replace(trimmed, ",", ".", 1), 64)
			if err != nil {
				return candidateHeader{}, headerBadScore
			}
			if n < 0 || n > 1 {
				return candidateHeader{}, headerBadScore
			}
			out.Score, haveScore = n, true
		case "topic":
			value = strings.Trim(value, ".")
			if value == "" {
				continue
			}
			out.TopicID, haveTopic = value, true
		case "addressed":
			switch strings.ToLower(strings.Trim(value, ".")) {
			case "true", "yes", "ja", "1":
				out.Addressed = true
			}
		}
	}
	if !haveScore || !haveTopic {
		return candidateHeader{}, headerMissingField
	}
	return out, headerOK
}

// headerSplitter knipt een binnenkomende stroom in de kopregel en de rest. Hij
// bestaat omdat de kopregel streamend gelezen moet worden: bij `PASS` willen we
// de generatie afbreken vóór het model een hele bijdrage schrijft die niemand
// gaat horen.
type headerSplitter struct {
	buf  strings.Builder
	done bool
}

// feed voegt een stuk stroom toe. Zodra de kopregel compleet is geeft hij hem
// terug, met de rest van dit stuk als begin van de bijdrage.
func (h *headerSplitter) feed(chunk string) (line, rest string, ready bool) {
	if h.done {
		return "", chunk, false
	}
	h.buf.WriteString(chunk)
	s := h.buf.String()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		h.done = true
		return s[:i], s[i+1:], true
	}
	if len(s) >= headerScanLimit {
		h.done = true
		return s, "", true
	}
	return "", "", false
}

// finish sluit een stroom af die eindigde zonder regelovergang: een kale
// `PASS` zonder afsluitende nieuwe regel is nog steeds een kopregel.
func (h *headerSplitter) finish() (line string, ready bool) {
	if h.done {
		return "", false
	}
	h.done = true
	return h.buf.String(), true
}
