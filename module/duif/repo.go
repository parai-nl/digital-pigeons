package duif

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ErrSessionNotFound dekt zowel "bestaat niet" als "is niet van deze
// gebruiker": sessies zijn user-scoped en het verschil lekt niet (404).
var ErrSessionNotFound = errors.New("duif: chatsessie niet gevonden")

var ErrComparisonConflict = errors.New("duif: comparison request ID reused with different selection")

// ChatSession is één chatgesprek van één gebruiker met één duif.
type ChatSession struct {
	ID                    string            `json:"id"`
	UserID                string            `json:"-"` // nooit op de wire
	PigeonID              string            `json:"pigeonId"`
	Lang                  string            `json:"lang"`
	CreatedAt             time.Time         `json:"createdAt"`
	UpdatedAt             time.Time         `json:"updatedAt"`
	Comparison            *ComparisonOrigin `json:"comparison,omitempty"`
	Title                 string            `json:"title,omitempty"`
	ComparisonRequestID   string            `json:"-"`
	ComparisonRequestHash string            `json:"-"`
}

// ChatMessage is één bericht in een sessie. Sources/KBStatus zijn de
// bronnen-metadata van een assistantbericht; Partial markeert een
// afgebroken stream (client-disconnect of stream-fout).
//
// Sources gaat als jsonb de kolom `duif.chat_messages.sources` in, in precies
// de vorm die ook de wire draagt. Het schema van Source groeit daarom alleen
// additief: oudere rijen dragen nog `ref{file, doi}` en moeten
// zonder migratie leesbaar blijven (zie TestSource_DecodesLegacyRowSchema).
// Let op het naamsverschil dat hier samenkomt: de props in de kennisgraaf zijn
// snake_case (`thema_titel`, `primary_themes`), de wire is camelCase.
type ChatMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"-"`
	Role      string    `json:"role"` // "user" | "assistant"
	Content   string    `json:"content"`
	KBStatus  string    `json:"kbStatus,omitempty"`
	Sources   []Source  `json:"sources,omitempty"`
	Partial   bool      `json:"partial,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Related zegt dat Sources uit de brede zoekactie ná een kennisgat komt of
	// uit een herformulering, en niet uit de gewone zoekopdracht op de vraag
	// Het wordt bewaard omdat het anders alleen tijdens de beurt
	// bestaat: die bronnen dragen het antwoord soms wél, de duif zegt dan niets
	// over een grens, en na herladen las dezelfde rij als volledige dekking met
	// N bronnen. Onwaar op elke rij van vóór dit veld — daar
	// kwámen de bronnen uit de gewone zoekopdracht.
	Related bool `json:"related,omitempty"`

	// Kind zegt welk soort bericht dit is: gewone tekst, of het eigen blok
	// waarin een duif antwoordde op de facilitator (KindPigeonAnswer). De
	// frontend rendert die twee verschillend.
	Kind string `json:"kind"`
	// Persona is de stem achter een assistantbericht (PersonaFacilitator of
	// een duif-id). Leeg bij userberichten en bij rijen van vóór de
	// facilitator: die renderen als de duif zelf.
	Persona string `json:"persona,omitempty"`
	// Question is de facilitator-vraag waarop een KindPigeonAnswer het
	// antwoord is. Server-side: de duif krijgt haar eigen historie ermee
	// terug, de client heeft er niets aan.
	Question string `json:"-"`
	// Seq is de volgorde binnen de sessie (monotoon, door de database gezet).
	// Geen productieregel leest hem: de kolom doet het werk (ListMessages
	// sorteert erop in SQL). Het veld staat er als bewust testvenster, zodat
	// een test kan bewijzen dát de volgorde monotoon is en dat een agent-beurt
	// haar rijen in de zichtbare volgorde wegschrijft.
	Seq        int64             `json:"-"`
	Provenance *AnswerProvenance `json:"provenance,omitempty"`
	// StopReason zegt waarom deze uiting eindigde, en staat er alleen als dat
	// niet vanzelf spreekt: StopReasonTruncated als de provider de stream op de
	// tokengrens sloot en het antwoord midden in een zin stopt,
	// StopReasonFiltered als zijn inhoudsfilter hem sloot, of
	// StopReasonRefusal als het model weigerde (notifiedStopReasons in
	// answer_stream.go is de enige lijst). Leeg is het
	// normale geval, ook op elke rij van vóór dit veld. Het verandert Content
	// nooit: de tekst is wat de duif zei, ook afgekapt.
	StopReason string `json:"stopReason,omitempty"`
	// Audit is het brondekkingslabel dat vroeger achter een brongedragen
	// antwoord aan werd geschreven. Er wordt er geen meer gemaakt (de bronnen
	// zijn bij de import al gecontroleerd) en niemand leest het meer: de chip
	// is uit de UI en het `audit`-frame van de wire. Het veld blijft bestaan
	// omdat de kolom blijft bestaan — een oude rij leest zo terug zoals hij
	// bewaard staat, in plaats van te struikelen over een veld dat de code niet
	// meer kent.
	//
	// Serverzijdig, net als SourceMessageID hieronder: `json:"-"` houdt het van
	// de REST-lijn. Zou het meegaan, dan kreeg de client bij elk herladen van
	// een oud gesprek een oordeel toegestuurd dat niets meer rendert en dat wij
	// vandaag niet meer zouden durven onderbouwen. Het veranderde Content nooit.
	Audit *AnswerAudit `json:"-"`
	// SourceMessageID names the row this one was copied from when a comparison
	// carried it into a second conversation. It always names the ORIGINAL row,
	// never an intermediate copy. Empty on everything that is not a copy.
	// Server-side only: the reader sees a copied answer as the first pigeon's
	// answer, not as a reference.
	SourceMessageID string `json:"-"`
}

// Berichtsoorten (kolom `kind`). Eén naamruimte voor database, code en wire.
const (
	// KindText is gewone prose: het userbericht, de facilitator-tekst, en
	// elk antwoord van een duif op de pipeline.
	KindText = "text"
	// KindPigeonAnswer is het antwoord van een duif binnen een
	// facilitator-beurt: op de wire een artifact, in de opslag een eigen rij.
	KindPigeonAnswer = "pigeon_answer"
)

// PersonaFacilitator is de persona-waarde van de facilitator-stem. Bewust
// geen persona-id uit personas.yaml: de facilitator is geen duif en staat
// niet in /v1/duiven.
const PersonaFacilitator = "facilitator"

// normalizeKind vult de default van de kolom in Go, zodat de fake-repo en
// Postgres dezelfde berichten teruggeven.
//
// Hij staat op de SQL-grens (postgres.go) en wordt door de fake-repo nagedaan;
// eigenlijk hoort hij op de domeingrens — in AppendMessage van beide repo's,
// of als ChatMessage.normalize(). Bewust niet verplaatst: dat raakt beide
// implementaties en de Postgres-repotest, en de schrijvers zetten Kind al
// overal expliciet.
func normalizeKind(kind string) string {
	if kind == "" {
		return KindText
	}
	return kind
}

// ChatRepo is de persistentie-seam van de chat. Productie: Postgres
// (postgres.go, migraties in migrations/); tests: in-memory fake.
type ChatRepo interface {
	CreateSession(ctx context.Context, s ChatSession) error
	// CreateComparison atomically stores a new target session and the copied
	// conversation that came with it: this user's own messages, the question,
	// and the first pigeon's answers, each keeping its own author, sources and
	// label. The caller validates source ownership and decides what travels.
	CreateComparison(ctx context.Context, s ChatSession, selected []ChatMessage) (*ChatSession, error)
	// GetComparison resolves one user-scoped idempotent request, including a
	// target whose original session was deleted. A different payload conflicts.
	GetComparison(ctx context.Context, userID, requestID, requestHash string) (*ChatSession, error)
	// GetSession geeft ErrSessionNotFound wanneer de sessie niet bestaat óf
	// niet van userID is.
	GetSession(ctx context.Context, id, userID string) (*ChatSession, error)
	ListSessions(ctx context.Context, userID string) ([]ChatSession, error)
	AppendMessage(ctx context.Context, m ChatMessage) error
	// ListMessages geeft berichten in schrijfvolgorde (seq); limit > 0
	// beperkt tot de laatste N, 0 = alles.
	ListMessages(ctx context.Context, sessionID string, limit int) ([]ChatMessage, error)
}

// newID munt een URL-safe random id (128 bits hex).
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand faalt alleen bij een kapot OS
	}
	return hex.EncodeToString(b[:])
}

// sessionTitle is a short user-authored label, never a generated summary.
func sessionTitle(text string) string {
	return clipRunes(strings.Join(strings.Fields(text), " "), 96)
}
