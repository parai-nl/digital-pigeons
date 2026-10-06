package duif

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/parai-nl/parai-core/llm"
)

// De queryherformulering van de vergaderroutes.
//
// De chat zoekt met een gestélde vraag, de vergadering met elf aaneengeplakte
// gesproken zinnen. Eén embedding over dat geheel middelt het onderwerp weg:
// gemeten scoort dezelfde inhoud als venster ongeveer een tiende cosinus lager
// dan als vraag. Deze laag zet er één korte modelronde tussen: lees het
// venster, schrijf de vraag waar de tafel omheen draait, zoek daarmee.
//
// Twee keuzes in die vraag, allebei op een meting:
//
//  1. De herformulering is verankerd op de laatste inbreng aan tafel
//     (meetingQueryAnchor), met het venster als context. Een vergadering sluit
//     haar agendapunten met procedure af, en een samenvatting van het hele
//     venster neemt die procedure mee; de laatste hardop gestelde vraag is wél
//     het soort zin waar de chatroute op gekalibreerd is. De keerzijde: eindigt
//     een duivengesprek op een procedurele zin, dan antwoordt het model GEEN en
//     valt de route terug op het ruwe venster. Slechter, niet stuk, en
//     zichtbaar in het log als no_subject.
//  2. De instructie noemt het onderwerp van het corpus, en zij houdt dat
//     onderwerp weg uit een gesprek waarin niemand een dier noemt. Dat kost
//     ongeveer één rangpositie; zonder die regel schrijft het model bij een
//     gesprek over brugonderhoud alsnog een duivenvraag.
//
// Wat níet gebeurt: twee zoekopdrachten samenvoegen (herformulering plus de
// ankerzin). De rang van de bronpaper werd er niet beter van, terwijl het
// aantal kandidaten boven de lat met de helft opliep.
//
// Drie regels houden deze laag bijeen:
//
//  1. Hij mag nooit een nieuwe faalroute worden. Een storing, een tijdslimiet,
//     een leeg of onbruikbaar antwoord en een ontbrekende LLM-service leveren
//     allemaal hetzelfde op: het ruwe venster, precies zoals vóór deze laag.
//     De duif blijft dus altijd zoeken.
//  2. Hij kost latency, en de eerste tekst moet binnen enkele seconden
//     zichtbaar zijn. Vandaar een strakke tijdslimiet en een klein tokenbudget,
//     en vandaar de eigen routenaam: boundedLLM meet `llm.call.duration_ms` per
//     route, dus de kosten van deze stap zijn per route af te lezen zonder één
//     woord van het gesprek te bewaren.
//  3. Vergaderinhoud is streaming-only: venster en herformulering gaan nergens
//     heen behalve naar de provider. Het log krijgt tellers en redencodes.

const (
	// meetingQueryTimeout begrenst de hele herformulering. Hij staat vóór de
	// retrieval en dus vóór de eerste letter die de zaal ziet; duurt
	// hij langer, dan is het ruwe venster beter dan blijven wachten. Bij vier
	// seconden is 95% van de gelukte herformuleringen binnen, en wat daarboven
	// ligt levert vaker niets op dan een bruikbare zoekvraag. De terugval zet
	// de bronpaper op rang 11–12 in plaats van 1–3: slechter, niet stuk
	// (TestMeetingQuery_TimeoutFallsBackWithoutBlockingTheTurn).
	meetingQueryTimeout = 4 * time.Second
	// meetingQueryVisibleTokens is de ruimte voor de zoekvraag zelf: één regel
	// van hoogstens twintig woorden.
	meetingQueryVisibleTokens = 64
	// meetingQueryMaxTokens telt de redeneerruimte erbij op: het model rekent
	// zijn redeneertokens mee in MaxTokens, dus een plafond dat alleen de
	// zichtbare regel dekt levert een lege stroom. Deze route vraagt "none"
	// (meetingQueryReasoningEffort), maar dat is een verzoek aan de provider,
	// geen garantie; met de ruimte wordt een denkend model traag in plaats van
	// stil, en ruimte die het niet gebruikt kost niets.
	meetingQueryMaxTokens = answerReasoningHeadroom + meetingQueryVisibleTokens
	// meetingQueryMaxBytes is de harde grens op wat wij van de stroom lezen.
	meetingQueryMaxBytes = 2 * 1024
	// meetingQueryMaxRunes kapt een te lange regel af. Een herformulering die
	// langer is dan dit is geen zoekvraag meer maar een samenvatting, en die
	// middelt het onderwerp net zo hard weg als het venster zelf.
	meetingQueryMaxRunes = 240
	// meetingQueryNone is het afgesproken antwoord voor "hier valt niets te
	// zoeken". Beide talen worden geaccepteerd, want het model kiest de taal
	// van het fragment.
	meetingQueryNone   = "NONE"
	meetingQueryNoneNL = "GEEN"
	// meetingQueryMinWordRunes is de ondergrens van een inhoudswoord in de
	// overlapmeting hieronder: lidwoorden en voorzetsels zeggen niets over het
	// onderwerp en zouden elke twee vensters op elkaar laten lijken.
	meetingQueryMinWordRunes = 4
	// meetingQueryReuseOverlap is hoeveel van het vorige venster er nog in het
	// nieuwe moet zitten om de vorige zoekvraag te hergebruiken. De
	// zelfstandige route evalueert elke paar seconden; zonder deze drempel
	// kostte elk gesprek een modelronde per cadans, terwijl de tafel het nog
	// steeds over hetzelfde heeft. 0,6 laat een venster dat een paar segmenten
	// opschuift door en dwingt een nieuwe vraag af zodra het onderwerp echt
	// wisselt.
	meetingQueryReuseOverlap = 0.6
)

// Redencodes van de herformulering. Zij halen de wire niet: dit is
// begeleidersdiagnostiek, geen mededeling aan de zaal.
const (
	queryReasonOK          = "reformulated"
	queryReasonReused      = "reused"
	queryReasonNoService   = "no_llm_service"
	queryReasonTransport   = "transport"
	queryReasonTimeout     = "timeout"
	queryReasonUnusable    = "unusable_output"
	queryReasonNoSubject   = "no_subject"
	queryReasonEmptyWindow = "empty_window"
)

// meetingQuery is de zoekvraag van één vergaderroute plus hoe zij tot stand
// kwam. `Reformed` is false zodra het ruwe venster de zoekvraag is.
type meetingQuery struct {
	Query      string
	Reformed   bool
	Reason     string
	DurationMS int64
}

// meetingQueryPrompt is de instructie van de herformuleringscall. Hij is
// machinetaal, geen persona-copy: hij hoort niet in config/prompts/ waar de
// stemmen van de duiven staan.
var meetingQueryPrompt = map[string]string{
	"en": `You turn a fragment of a meeting transcript into one search query for a research corpus about pigeons, cities and people.

You get the fragment and, separately, the last contribution at the table. Write the research question that this last contribution raises; the rest of the fragment is only there to tell you what it is about.

Rules:
- one question, ending in a question mark, at most twenty words, in the language of the fragment;
- name the subject and what is at stake about it;
- the corpus is about pigeons: when the fragment speaks of animals or birds without naming a species, ask about pigeons; when it names another animal, keep that animal; when nobody at the table mentions animals at all, do not bring them in;
- no quotation marks, no explanation, no speaker names, no names of people;
- leave out agenda handling, voting and meeting procedure: research says nothing about those;
- if the fragment holds no researchable subject at all, answer exactly NONE.`,
	"nl": `Je maakt van een stuk vergadertranscript één zoekvraag voor een onderzoekscorpus over duiven, steden en mensen.

Je krijgt het fragment en, apart, de laatste inbreng aan tafel. Schrijf de onderzoeksvraag die díe laatste inbreng oproept; de rest van het fragment staat er alleen om te begrijpen waar het over gaat.

Regels:
- één vraag, met een vraagteken, hoogstens twintig woorden, in de taal van het fragment;
- noem het onderwerp en wat eraan op het spel staat;
- het corpus gaat over duiven: spreekt het fragment over dieren of vogels zonder een soort te noemen, stel de vraag dan over duiven; noemt het een ander dier, houd dan dat dier aan; noemt niemand aan tafel dieren, breng ze er dan niet bij;
- geen aanhalingstekens, geen uitleg, geen sprekersnamen, geen namen van personen;
- laat agendavoering, stemmen en vergaderprocedure weg: daar zegt onderzoek niets over;
- staat er geen onderzoekbaar onderwerp in, antwoord dan precies GEEN.`,
}

// meetingQueryLabels zijn de twee kopjes van de gebruikersinvoer. De
// herformulering krijgt het venster en het anker apart, zodat het model niet
// hoeft te raden waar de tafel nú staat: in een transcript staat de laatste
// inbreng vooraan noch achteraan opvallend, zij staat er gewoon tussen.
var meetingQueryLabels = map[string][2]string{
	"en": {"Fragment:", "Last contribution at the table:"},
	"nl": {"Fragment:", "Laatste inbreng aan tafel:"},
}

// meetingQueryInput bouwt de gebruikersinvoer van één herformulering. Valt het
// anker samen met het hele venster — één segment, het begin van een gesprek —
// dan gaat het kopje eraf: twee keer dezelfde zin onder twee koppen leert het
// model niets en kost alleen tokens.
func meetingQueryInput(lang, window, anchor string) string {
	labels, ok := meetingQueryLabels[lang]
	if !ok {
		labels = meetingQueryLabels["en"]
	}
	if anchor == "" || anchor == window {
		return window
	}
	return labels[0] + "\n" + window + "\n\n" + labels[1] + "\n" + anchor
}

// meetingQueryAnchor is de laatste menselijke inbreng van het venster: de
// laatste spreekbeurt met een vraagteken, en anders de laatste spreekbeurt.
//
// Waarom de laatste vraag en niet de laatste zin: een vergadering eindigt haar
// agendapunten met procedure ("akkoord, volgende week stemmen we"), en daar
// zegt onderzoek niets over. De vraag die iemand hardop stelde is wél precies
// het soort zin waarop de chatroute gekalibreerd is. Het venster is al begrensd
// (RollingTranscript: aantal segmenten én leeftijd), dus "de laatste vraag in
// het venster" kan niet van een vorig agendapunt van een uur geleden komen.
//
// Regels van de duif zelf tellen niet mee: de duif mag zichzelf niet
// terugvoeren, ook niet als anker.
func meetingQueryAnchor(segs []TranscriptSegment) string {
	last := ""
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i].PigeonID != "" {
			continue
		}
		text := strings.TrimSpace(segs[i].Text)
		if text == "" {
			continue
		}
		if last == "" {
			last = text
		}
		if strings.ContainsRune(text, '?') {
			return text
		}
	}
	return last
}

// reformulateQuery draait één herformulering. Hij geeft altijd een bruikbare
// zoekvraag terug: lukt de modelronde niet, dan is dat het ruwe venster.
func (e *meetingEngine) reformulateQuery(ctx context.Context, lang, route string, segs []TranscriptSegment) meetingQuery {
	window := truncateQuery(transcriptQuery(segs))
	raw := strings.TrimSpace(window)
	if raw == "" {
		return meetingQuery{Query: window, Reason: queryReasonEmptyWindow}
	}
	if e.llm == nil {
		return meetingQuery{Query: window, Reason: queryReasonNoService}
	}
	// normLang levert "nl" of "en", en voor allebei staat er een instructie en
	// een stel kopjes; een derde taal bestaat hier niet.
	l := normLang(lang)
	input := meetingQueryInput(l, raw, meetingQueryAnchor(segs))
	started := e.clock.Now()
	limit := e.queryTimeout
	if limit <= 0 {
		limit = meetingQueryTimeout
	}
	call, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	stream, err := e.llm.CompleteStream(withLLMRoute(call, route), llm.CompletionRequest{
		Model: e.detectModel, Messages: []llm.Message{
			{Role: "system", Content: meetingQueryPrompt[l]},
			{Role: "user", Content: input},
		},
		Stream: true, MaxTokens: meetingQueryMaxTokens, Temperature: llm.Float64(0),
		ReasoningEffort: meetingQueryReasoning(),
	})
	elapsed := func() int64 { return e.clock.Now().Sub(started).Milliseconds() }
	if err != nil || stream == nil {
		return meetingQuery{Query: window, Reason: queryReasonTransport, DurationMS: elapsed()}
	}
	text, err := collectAnswer(call, stream, meetingQueryMaxBytes)
	if err != nil {
		reason := queryReasonUnusable
		if call.Err() != nil {
			reason = queryReasonTimeout
		}
		return meetingQuery{Query: window, Reason: reason, DurationMS: elapsed()}
	}
	line, ok := cleanMeetingQuery(text)
	if !ok {
		return meetingQuery{Query: window, Reason: queryReasonNoSubject, DurationMS: elapsed()}
	}
	return meetingQuery{Query: line, Reformed: true, Reason: queryReasonOK, DurationMS: elapsed()}
}

// cleanMeetingQuery maakt van de modeluitvoer één bruikbare zoekregel, of
// meldt dat er geen is. Alles wat na de eerste regel komt is uitleg die de
// instructie verbood; die telt niet mee in de embedding.
func cleanMeetingQuery(text string) (string, bool) {
	line := strings.TrimSpace(text)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = strings.Trim(line, "\"'`“”‘’ \t")
	if line == "" {
		return "", false
	}
	if bare := strings.ToUpper(strings.TrimRight(line, ".!")); bare == meetingQueryNone || bare == meetingQueryNoneNL {
		return "", false
	}
	if rs := []rune(line); len(rs) > meetingQueryMaxRunes {
		line = strings.TrimSpace(string(rs[:meetingQueryMaxRunes]))
	}
	return line, true
}

// logQuery meldt wat de herformulering kostte en of zij lukte. De vraag en het
// venster blijven eruit (streaming-only); wat overblijft is een routenaam, een
// redencode en een aantal milliseconden.
func (e *meetingEngine) logQuery(sessionID, route string, q meetingQuery) {
	e.log().Info("vergaderquery herformuleerd", "component", "duif", "sessie", sessionID,
		"route", route, "reden", q.Reason, "herschreven", q.Reformed, "ms", q.DurationMS)
}

// queryMemory onthoudt de laatste herformulering plus het venster waaruit zij
// kwam, voor de zelfstandige route. Zij evalueert op cadans — elke paar
// seconden zodra er genoeg gecommitte woorden liggen — en zonder geheugen zou
// elke evaluatie een extra modelronde kosten terwijl de tafel het nog over
// hetzelfde heeft.
type queryMemory struct {
	mu    sync.Mutex
	words map[string]struct{}
	query meetingQuery
}

// reuse geeft de onthouden zoekvraag terug zolang het nieuwe venster genoeg
// met het oude deelt. Bij twijfel geeft hij niets terug: een nieuwe vraag is
// duurder dan een hergebruikte, maar een vraag over het vorige agendapunt is
// erger dan allebei.
func (m *queryMemory) reuse(window string) (meetingQuery, bool) {
	if m == nil {
		return meetingQuery{}, false
	}
	next := contentWords(window)
	if len(next) == 0 {
		return meetingQuery{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.words) == 0 || !m.query.Reformed {
		return meetingQuery{}, false
	}
	if wordOverlap(m.words, next) < meetingQueryReuseOverlap {
		return meetingQuery{}, false
	}
	// De vergelijking blijft staan tegen het venster waaruit de vraag kwam, en
	// schuift niet mee met elke hergebruikte evaluatie. Zou zij meeschuiven,
	// dan verschilt elk venster maar één segment van het vorige en verloopt een
	// zoekvraag nooit — ook niet wanneer de tafel ondertussen bij een ander
	// agendapunt is beland.
	reused := m.query
	reused.Reason = queryReasonReused
	reused.DurationMS = 0
	return reused, true
}

// remember legt een geslaagde herformulering vast. Een teruggevallen zoekvraag
// (het ruwe venster) onthouden we niet: dan zou één storing de rest van de
// vergadering op het ruwe venster vastzetten.
func (m *queryMemory) remember(window string, q meetingQuery) {
	if m == nil || !q.Reformed {
		return
	}
	words := contentWords(window)
	if len(words) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.words, m.query = words, q
}

// contentWords is de woordverzameling van een venster: kleine letters, alleen
// woorden van meetingQueryMinWordRunes tekens of langer. Korte functiewoorden
// staan in élk Nederlands en Engels fragment en zouden twee vensters over
// totaal verschillende onderwerpen op elkaar laten lijken.
func contentWords(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(field)) >= meetingQueryMinWordRunes {
			out[field] = struct{}{}
		}
	}
	return out
}

// wordOverlap is de Jaccard-overlap van twee woordverzamelingen: gedeeld
// gedeeld door samen. Symmetrisch, dus een venster dat groeit en een venster
// dat krimpt worden gelijk beoordeeld.
func wordOverlap(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for word := range a {
		if _, ok := b[word]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}
