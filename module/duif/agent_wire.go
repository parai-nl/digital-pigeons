package duif

import (
	"context"
)

// De extra SSE-frames van de agent-motor. Ze zijn additief: `text`, `error`
// en `done` blijven wat ze waren, en een client die een onbekend type negeert
// ziet nog steeds een werkende chat.
//
// Wat er bewust níét in zit: de argumenten van een tool-aanroep en een
// samenvatting van het resultaat. Een chip toont "de duif is gevraagd", niet
// wat er precies gevraagd is — de thema-beschrijvingen en de vraag van de
// facilitator aan de duif blijven server-side.
type toolFrame struct {
	Type string `json:"type"` // "tool"
	ID   string `json:"id"`
	Name string `json:"name"`
}

// toolResultFrame sluit de chip. Error draagt een stabiele reason-code
// (llm_budget_exceeded, clarify_already_asked, …), nooit een fouttekst.
type toolResultFrame struct {
	Type  string `json:"type"` // "tool_result"
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

// phaseFrame meldt welke stap van de beurt nu loopt. Een chatbeurt
// is vier tot vijf modelcalls achter elkaar, en zonder dit frame ziet de lezer
// daar twee toestanden van: "je vraag wordt onderzocht" en "de duif denkt na".
// Dan lijkt een halve minuut wachten één trage gedachte, terwijl er
// achtereenvolgens een vraag geformuleerd, bronnen gelezen en een antwoord
// geschreven wordt.
//
// Het frame draagt uitsluitend de stand: één waarde uit de vaste reeks in
// llm_metrics.go (facilitator, pigeon_grounding, pigeon_related_search,
// pigeon, mediation). Geen zoekopdracht, geen vraag, geen argument — precies
// zoals toolFrame de naam van een tool draagt en niet wat eraan gevraagd is.
// De client vertaalt de waarde zelf naar een zin in zijn eigen taal; een fase
// die hij niet kent laat hij staan waar hij stond.
type phaseFrame struct {
	Type  string `json:"type"` // "phase"
	Phase string `json:"phase"`
}

// framingFrame is de kaderzin van de facilitator vlak vóór een duif-blok. Op
// de draad is hij niet te onderscheiden van een gewoon `text`-frame — de
// client ziet één stem die op twee momenten iets zegt — maar het eigen Go-type
// houdt hem uit de tekstverzameling van de event-lus: de tool schreef de rij
// zelf al weg, vóór de rij van de duif (zie agentRun.emitFrame en
// agentTurn.persistFacilitatorText).
type framingFrame struct {
	textFrame
}

// artifactFrame opent én sluit een blok van een andere stem dan de
// facilitator. Openen: Done is false. Sluiten: Done is true, met eventueel
// een reason-code als het blok halverwege omviel.
type artifactFrame struct {
	Type    string `json:"type"` // "artifact"
	ID      string `json:"id"`
	Kind    string `json:"kind"` // KindPigeonAnswer
	Persona string `json:"persona"`
	// KBStatus is de kennisbankstand van dít blok (sources, gap of
	// persona_only): een kennisbank-duif kan per vraag anders uitkomen, en de
	// done van de beurt zegt alleen iets over de laatste.
	KBStatus KBStatus `json:"kbStatus,omitempty"`
	Done     bool     `json:"done,omitempty"`
	Error    string   `json:"error,omitempty"`
	// StopReason staat alleen op het sluitframe, en alleen als het blok anders
	// dan normaal eindigde: "truncated" als de provider de stream op de
	// tokengrens sloot, midden in een zin, en "filtered" als zijn
	// inhoudsfilter hem sloot. Het hoort bij dít blok en niet bij de
	// done van de beurt, omdat de facilitator daarna gewoon doorpraat: de
	// melding hoort onder het antwoord dat afbrak, niet onder zijn
	// bemiddeling.
	StopReason string `json:"stopReason,omitempty"`
	// Dropped meldt dat er gevonden kandidaten wegvielen vóór de bronnenlijst
	// van dít blok. Hij staat op het openframe, ook bij een gat: juist
	// dán is "er was wél iets, maar het viel af" wat de lezer mist.
	Dropped *SourceDrops `json:"dropped,omitempty"`
	// Related zegt waar deze bronnen vandaan komen: niet uit de gewone
	// zoekopdracht op de vraag, maar uit een herformulering of uit de brede
	// zoekactie ná een kennisgat. Dat is een uitspraak over herkomst
	// en niet over waarde — die bronnen dragen het antwoord soms alsnog.
	// Zonder dit veld is "3 bronnen" op het scherm niet te onderscheiden van
	// dekking, en precies dat misverstand moet dit veld uitsluiten.
	// Anders dan Dropped wordt hij wél opgeslagen (ChatMessage.Related): de
	// duif benoemt de grens alleen als de bronnen haar vraag echt niet dragen,
	// dus haar eigen tekst is geen betrouwbare vervanging na herladen.
	Related bool `json:"related,omitempty"`
}

// artifactDeltaFrame is één stukje tekst binnen een geopend artifact.
type artifactDeltaFrame struct {
	Type  string `json:"type"` // "artifact_delta"
	ID    string `json:"id"`
	Delta string `json:"delta"`
}

// emitterKey is de contextsleutel van de frame-brug naar de tools.
type emitterKey struct{}

// toolEmitter is de brug waarlangs een tool-handler frames naar de
// SSE-stream stuurt.
//
// Het kanaal is ongebufferd, en dat is de hele truc. De handler draait op de
// producer-goroutine van RunStream, terwijl de event-lus op de
// consumer-goroutine schrijft; twee schrijvers op één HTTP-writer zouden een
// mutex nodig hebben, en een mutex beschermt bytes, geen volgorde. Nu is er
// één schrijver — de lus — en levert het ongebufferde kanaal het rendez-vous
// dat de handler pas laat doorlopen als zijn frame werkelijk verstuurd is.
//
// Een nil-emitter (geen ctx-waarde) is een no-op: de tools blijven dan
// aanroepbaar zonder stream, bijvoorbeeld in een test die alleen het
// resultaat toetst.
type toolEmitter struct {
	ch chan<- any
}

// withEmitter hangt de frame-brug aan de context die RunStream aan de
// handlers doorgeeft.
func withEmitter(ctx context.Context, ch chan<- any) context.Context {
	return context.WithValue(ctx, emitterKey{}, &toolEmitter{ch: ch})
}

// emitterFrom haalt de brug uit de context; hij geeft nooit nil terug.
func emitterFrom(ctx context.Context) *toolEmitter {
	if e, ok := ctx.Value(emitterKey{}).(*toolEmitter); ok {
		return e
	}
	return &toolEmitter{}
}

// emit levert één frame af aan de event-lus. Hij geeft false wanneer de
// context afliep — de client is dan weg of de lus is gestopt, en de handler
// hoort niet te blijven wachten op een lezer die er niet meer is.
func (e *toolEmitter) emit(ctx context.Context, frame any) bool {
	if e.ch == nil {
		return true
	}
	select {
	case e.ch <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}
