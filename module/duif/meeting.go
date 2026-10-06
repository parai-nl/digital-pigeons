package duif

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/parai-nl/parai-core/audio"
	"github.com/parai-nl/parai-core/llm"
)

// Vergadermodus: in-memory sessiebeheer met één gebeurtenissenkanaal per
// sessie, de knop "de duif antwoordt nu", de stopknop, de inactiviteits-cutoff
// en de luister-indicator.
//
// Vluchtigheid is hier de harde bouwregel: vergaderaudio en -transcript zijn
// streaming-only. Dit bestand kent daarom geen repository, geen bestands-API
// en geen migratie — het schuivende venster leeft uitsluitend in het geheugen
// van de sessie en sterft met haar. De toets staat in meeting_privacy_test.go.
//
// De audio-invoer zelf staat in meeting_stt.go: de zelfherstellende brug naar
// audio.RealtimeTranscriber. Het venster kent zijn bron niet — de brug en
// de transcript-route voeden exact dezelfde sessie-API.

// ListenState is de waarheidsgetrouwe luisterstatus van een sessie:
// liever vals-negatief dan vals-positief.
type ListenState string

const (
	// ListenActive: de keten luistert echt mee.
	ListenActive ListenState = "listening"
	// ListenReconnecting: de STT-brug is weg en herstelt zich.
	ListenReconnecting ListenState = "reconnecting"
	// ListenOff: er wordt niet meegeluisterd.
	ListenOff ListenState = "off"
)

// VoiceState zegt of deze sessie hoorbaar wordt. Hij wordt eenmalig
// bij het openen bepaald en verandert daarna niet meer: de UI hoeft niet te
// raden waarom er geen geluid komt.
type VoiceState string

const (
	// VoiceReady: er is een TTS-provider én een bruikbare stem-id.
	VoiceReady VoiceState = "ready"
	// VoiceOff: deze duif blijft tekst-only (geen provider, geen stem-id, of
	// een non-verbale duif).
	VoiceOff VoiceState = "off"
)

// voicePlaceholder is een geldige configuratie zonder stem: de duif blijft
// tekst-only tenzij DUIF_DEV_VOICE_ID hem alsnog een dev-stem geeft. Geen
// enkele duif draagt deze waarde nog; hij blijft bestaan omdat een configuratie
// zonder stem een geldig geval is.
const voicePlaceholder = "placeholder-voice"

// resolveVoiceID is de enige plek die "heeft deze duif een stem" beantwoordt.
// De DTO (voiceReady), de sessiestatus (VoiceState) en de spreekbeurt lezen
// alledrie hier. devVoice is de dev-only noodstem: hij vervangt uitsluitend de
// placeholder, nooit een leeggelaten voice_id — een duif zonder stem-id hoort
// stil te blijven.
func resolveVoiceID(p *Persona, devVoice string) (string, bool) {
	if !p.Chat {
		return "", false // de non-verbale duif kent alleen veldopnames
	}
	switch {
	case p.VoiceID != "" && p.VoiceID != voicePlaceholder:
		return p.VoiceID, true
	case p.VoiceID == voicePlaceholder && devVoice != "":
		return devVoice, true
	default:
		return "", false
	}
}

// Sluitredenen van een vergadersessie.
const (
	CloseReasonInactivity  = "inactivity"
	CloseReasonUser        = "ended_by_user"
	CloseReasonMaxDuration = "max_duration"
	CloseReasonShutdown    = "server_shutdown"
	// CloseReasonReplaced: de gebruiker heeft zelf gezegd dat deze vergadering
	// mocht wijken — de uitweg bij "er is al een vergadering open" en de
	// duifwissel tijdens een lopende vergadering lopen allebei hierlangs.
	// Alleen voor sessies van hetzelfde account.
	CloseReasonReplaced = "replaced_by_user"
	// CloseReasonClientGone: er zit al langer dan de coulanceperiode geen
	// enkele client meer op het sessiekanaal. Een weggeklikt tabblad hoort geen
	// plaats aan tafel bezet te houden.
	CloseReasonClientGone = "client_gone"
)

// Berichttypes op het sessiekanaal.
const (
	EventState      = "state"
	EventTranscript = "transcript"
	EventPersona    = "persona"
	EventText       = "text"
	// EventSources levert de opgehaalde bronnen zodra de retrieval klaar is en
	// dus vóór de eerste tekstdelta, net als in de chat. Daardoor kan de client
	// een "[1]" meteen als klikbare verwijzing tekenen in plaats van als ruwe
	// tekst te laten staan tot de beurt helemaal voorbij is.
	EventSources  = "sources"
	EventTTSAudio = "tts_audio"
	// EventTextDone zegt dat het geschreven antwoord compleet is. Het volgt
	// direct op de laatste tekstdelta, terwijl de stem nog kan doorspreken;
	// response_done sluit pas ná de audio. Zonder dit onderscheid bleef de
	// typcursor in de vergadering staan zolang de duif nog aan het woord was.
	EventTextDone      = "text_done"
	EventTTSDone       = "tts_done"
	EventResponseDone  = "response_done"
	EventBusy          = "busy"
	EventStopped       = "stopped"
	EventError         = "error"
	EventPresenceCheck = "presence_check"
	EventSessionClosed = "session_closed"
	EventListening     = "listening_heartbeat"
	// De kandidaatgenerator, zelfstandig ingrijpen en het arousal-model plus
	// de geluidenset van de non-verbale duif.
	//
	// EventCandidate meldt dát de duif iets klaar heeft liggen en waarover —
	// nooit wat. De bijdrage zelf blijft op de server tot zij klinkt; wat de
	// vergadering krijgt is een score, een thema en een vaste reden.
	EventCandidate      = "candidate"
	EventArousal        = "arousal"
	EventIntervention   = "intervention"
	EventDetectorStatus = "detector_status"
	EventSound          = "sound"
	EventSettings       = "settings"
)

// TopicRef is het thema zoals de vergadering het ziet: een stabiel id plus het
// tweetalige label uit de persona-config. De classifier-beschrijving blijft
// achter op de server.
type TopicRef struct {
	ID    string    `json:"id"`
	Label LocalName `json:"label"`
}

// DetectorStatus meldt of de topic-detector meedraait, en waarom niet
// (degraderen naar alleen-knop is zichtbaar, nooit stil).
type DetectorStatus struct {
	Enabled      bool   `json:"enabled"`
	Reason       string `json:"reason,omitempty"`
	State        string `json:"state,omitempty"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMS int64  `json:"retryAfterMs,omitempty"`
}

// MeetingEvent is één bericht van server naar client. Alle velden zijn
// optioneel op `type` na; de discriminator stuurt de UI.
type MeetingEvent struct {
	Type             string      `json:"type"`
	TurnID           string      `json:"turnId,omitempty"`
	EventID          string      `json:"eventId,omitempty"`
	DurationMS       int         `json:"durationMs,omitempty"`
	AutonomyPaused   *bool       `json:"autonomyPaused,omitempty"`
	PlaybackTracking bool        `json:"playbackTracking,omitempty"`
	Playback         string      `json:"playback,omitempty"`
	SessionID        string      `json:"sessionId,omitempty"`
	PigeonID         string      `json:"pigeonId,omitempty"`
	Lang             string      `json:"lang,omitempty"`
	Listening        ListenState `json:"listening,omitempty"`
	Speaking         bool        `json:"speaking"`
	Text             string      `json:"text,omitempty"`
	Speaker          string      `json:"speaker,omitempty"`
	Final            bool        `json:"final,omitempty"`
	PigeonWindow     bool        `json:"pigeonWindow,omitempty"`
	Delta            string      `json:"delta,omitempty"`
	Audio            string      `json:"audio,omitempty"` // base64 TTS-frame, alleen onderweg
	KBStatus         KBStatus    `json:"kbStatus,omitempty"`
	Sources          []Source    `json:"sources,omitempty"`
	// Dropped hoort bij het sources-frame van een beurt: gevonden kandidaten
	// die alsnog geen bron werden.
	Dropped *SourceDrops      `json:"dropped,omitempty"`
	Error   string            `json:"error,omitempty"`
	Reason  string            `json:"reason,omitempty"`
	Message map[string]string `json:"message,omitempty"`

	// De klaarliggende kandidaat, de verantwoording van een interventie, de
	// meterstand en de degradatiemelding van de generator.
	//
	// Candidate staat uitsluitend op EventCandidate. Nil op dat event betekent:
	// de duif heeft nu niets meer klaarliggen.
	Candidate *CandidateRef    `json:"candidate,omitempty"`
	Topic     *TopicRef        `json:"topic,omitempty"`
	Score     float64          `json:"score,omitempty"`
	Addressed bool             `json:"addressed,omitempty"`
	Arousal   *float64         `json:"arousal,omitempty"`
	Detector  *DetectorStatus  `json:"detector,omitempty"`
	Sound     *SoundRef        `json:"sound,omitempty"`
	Settings  *SessionSettings `json:"settings,omitempty"`

	// StopReason staat op EventTextDone en op EventResponseDone (dezelfde
	// waarde: de tekst is bij text_done al af), en alleen als de beurt anders
	// dan normaal eindigde: "truncated" als de provider de stream op de
	// tokengrens sloot en de duif midden in een zin stopte, "filtered" als zijn
	// inhoudsfilter hem sloot. Het uitgesproken antwoord blijft
	// wat het was; de vergadering bewaart niets, dus dit leeft precies zo lang
	// als de sessie.
	StopReason string `json:"stopReason,omitempty"`

	// Additieve velden voor de stem. Voice staat uitsluitend op het
	// state-event (één emitter); Seq nummert de audioframes van één beurt
	// (1-gebaseerd) en Frames meldt bij tts_done hoeveel het er waren, zodat
	// de client een gat in de stroom herkent in plaats van een kapotte MP3 te
	// decoderen.
	Voice VoiceState `json:"voice,omitempty"`
	Seq   int        `json:"seq,omitempty"`
	// Format en SampleRate staan op elk tts_audio-frame: "mp3" speelt de
	// client per beurt, "pcm16" blok voor blok zodra het binnenkomt.
	Format     string `json:"format,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
	Frames     int    `json:"frames,omitempty"`

	// GraceMS is de resterende tijd van "Ben je er nog?" in milliseconden, op
	// presence_check, op de hartslag en op het state-event zolang de vraag
	// openstaat. Een duur en geen tijdstip: de browser telt af op
	// zijn eigen klok. Ontbreekt hij op de hartslag, dan staat er geen vraag
	// (meer) open.
	GraceMS int64 `json:"graceMs,omitempty"`
}

// Clock is de tijdseam van de vergadermodus: de inactiviteitstimers draaien
// erop, zodat tests elf minuten stilte kunnen simuleren zonder elf minuten te wachten.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker is de tikker-helft van de klok-seam.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) NewTicker(d time.Duration) Ticker { return systemTicker{t: time.NewTicker(d)} }

type systemTicker struct{ t *time.Ticker }

func (s systemTicker) C() <-chan time.Time { return s.t.C }

func (s systemTicker) Stop() { s.t.Stop() }

// TranscriptSegment is één door de STT gecommit stukje vergadering. Het leeft
// in het geheugen van de sessie en nergens anders.
type TranscriptSegment struct {
	// Seq is de plaats in de rij. De detector gebruikt hem in plaats van een
	// tijdstempel om "nieuw sinds de vorige evaluatie" af te bakenen: twee
	// segmenten binnen dezelfde klokresolutie blijven zo onderscheidbaar.
	Seq     uint64
	Text    string
	At      time.Time
	Speaker string
	// PigeonWindow markeert een segment dat binnenkwam terwijl de duif zelf
	// aan het woord was: de duif mag zichzelf niet terugvoeren.
	PigeonWindow bool
	// PigeonID is de duif die deze regel zei. Leeg is het normale geval: een
	// mens aan tafel. Gevuld komt alleen voor na een duifwissel — dan
	// draagt de regel de duif die hier vóór deze zat, en houdt zij dat
	// auteurschap vast: de nieuwe duif krijgt haar nooit als eigen tekst.
	PigeonID string
}

// RollingTranscript is het schuivende venster van één sessie: dubbel
// begrensd op aantal segmenten én leeftijd. Wat eruit valt is definitief weg;
// er bestaat geen vollediger kopie. Niet zelf goroutine-veilig — de sessie
// bewaakt hem met haar eigen lock.
type RollingTranscript struct {
	maxSegments int
	maxAge      time.Duration
	segments    []TranscriptSegment
}

// NewRollingTranscript maakt een leeg venster.
func NewRollingTranscript(maxSegments int, maxAge time.Duration) *RollingTranscript {
	return &RollingTranscript{maxSegments: maxSegments, maxAge: maxAge}
}

// Append voegt een segment toe en laat verlopen segmenten vervallen.
func (r *RollingTranscript) Append(seg TranscriptSegment) {
	r.segments = append(r.segments, seg)
	r.prune(seg.At)
}

func (r *RollingTranscript) prune(now time.Time) {
	if r.maxSegments > 0 && len(r.segments) > r.maxSegments {
		r.segments = r.segments[len(r.segments)-r.maxSegments:]
	}
	if r.maxAge <= 0 {
		return
	}
	cutoff := now.Add(-r.maxAge)
	keep := r.segments[:0:0]
	for _, s := range r.segments {
		if s.At.After(cutoff) {
			keep = append(keep, s)
		}
	}
	r.segments = keep
}

// Len is het aantal segmenten in het venster.
func (r *RollingTranscript) Len() int { return len(r.segments) }

// Snapshot geeft de laatste maxK segmenten binnen window, zonder de segmenten
// uit het duif-spreekvenster. Dit is wat de knop-route te zien krijgt.
func (r *RollingTranscript) Snapshot(now time.Time, window time.Duration, maxK int) []TranscriptSegment {
	cutoff := now.Add(-window)
	var out []TranscriptSegment
	for _, s := range r.segments {
		if s.PigeonWindow || !s.At.After(cutoff) {
			continue
		}
		out = append(out, s)
	}
	if maxK > 0 && len(out) > maxK {
		out = out[len(out)-maxK:]
	}
	return out
}

// EchoCount telt binnen hetzelfde tijdvenster precies wat Snapshot weglaat: de
// segmenten die binnenkwamen terwijl de duif zelf hoorbaar was. Een leeg
// venster mét echo's is een ander verhaal dan een leeg venster zonder — de
// eerste betekent "ik hoorde alleen mezelf", de tweede "ik hoorde niets".
func (r *RollingTranscript) EchoCount(now time.Time, window time.Duration) int {
	cutoff := now.Add(-window)
	n := 0
	for _, s := range r.segments {
		if s.PigeonWindow && s.At.After(cutoff) {
			n++
		}
	}
	return n
}

// transcriptWindow is het detectievenster van één evaluatie: de nieuwe
// segmenten sinds de vorige evaluatie plus een korte staart
// context, samen begrensd op een woordplafond.
type transcriptWindow struct {
	Segments []TranscriptSegment // oudste eerst; de laatste Fresh zijn nieuw
	Fresh    int
	Words    int
}

// Since bouwt dat venster: alles met een hoger volgnummer dan afterSeq geldt
// als nieuw. Duif-venstersegmenten blijven er volledig buiten — de duif mag
// haar eigen thema's niet detecteren. Bij overschrijding van maxWords
// sneuvelt eerst de context, daarna de oudste nieuwe segmenten.
func (r *RollingTranscript) Since(afterSeq uint64, contextSegments, maxWords int) transcriptWindow {
	var older, fresh []TranscriptSegment
	for _, s := range r.segments {
		switch {
		case s.PigeonWindow:
		case s.Seq > afterSeq:
			fresh = append(fresh, s)
		default:
			older = append(older, s)
		}
	}
	if contextSegments <= 0 {
		older = nil
	} else if len(older) > contextSegments {
		older = older[len(older)-contextSegments:]
	}

	win := transcriptWindow{
		Segments: append(append(make([]TranscriptSegment, 0, len(older)+len(fresh)), older...), fresh...),
		Fresh:    len(fresh),
	}
	win.Words = countSegmentWords(win.Segments)
	for maxWords > 0 && win.Words > maxWords && len(win.Segments) > 1 {
		if len(win.Segments) <= win.Fresh {
			win.Fresh--
		}
		win.Words -= segmentWords(win.Segments[0])
		win.Segments = win.Segments[1:]
	}
	return win
}

func countWords(s string) int { return len(strings.Fields(s)) }

// capRunes kapt een tekst af op een aantal tekens. Ruw en zonder
// beletselteken: dit is een plafond op invoer, geen leesbaar label.
func capRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// segmentWords telt wat er van één segment in het venster terechtkomt. Het
// sprekerlabel telt mee: het staat in de prompt vóór de dubbele punt, dus een
// venster dat alleen de tekst telt, houdt zijn eigen budget niet.
func segmentWords(s TranscriptSegment) int { return countWords(s.Speaker) + countWords(s.Text) }

func countSegmentWords(segs []TranscriptSegment) int {
	var n int
	for _, s := range segs {
		n += segmentWords(s)
	}
	return n
}

// detectConfig zijn de knoppen van de kandidaatgenerator. Wat er
// vergeleken met de oude detector niet meer in staat is minstens zo sprekend
// als wat er wél in staat: geen marge, geen thema-cooldown, geen sessieplafond
// en geen verstheidsvenster. Die zaten per duif in haar spreeklat, of ze zijn
// helemaal verdwenen.
type detectConfig struct {
	Enabled         bool          // draait de generator überhaupt mee
	MinWords        int           // cadans: evalueer pas na zoveel nieuwe gecommitte woorden
	Interval        time.Duration // cadans: hoogstens één evaluatie per zoveel tijd
	ContextAge      time.Duration // hoe ver de context terugkijkt
	ContextSegments int           // staart context bij een evaluatie
	MaxWindowWords  int           // woordplafond van het venster
	Timeout         time.Duration // noodrem op één evaluatie
	Quiet           time.Duration // vereiste stilte vóór een ongevraagde bijdrage
	ArousalHalfLife time.Duration // halveringstijd van het arousal-model
}

// meetingSettings zijn de per-sessie regelknoppen van zelfstandig meedoen
// bij het aan tafel zetten mee te geven, geen inhoud — sessie-metadata.
type meetingSettings struct {
	PlaybackTracking bool
	Autonomous       bool
	Eagerness        string
	Cooldown         time.Duration
}

// De terugvalwaarden van de inactiviteitsbewaking: dezelfde als personas.yaml,
// voor een gedragsconfig die ze niet (of als nul) noemt. De coulance voor een
// weggevallen kanaal is een eigen getal: zij deelde vroeger toevallig de
// aftelling van "Ben je er nog?" en hoort niet mee te schuiven als die
// verandert.
const (
	defaultInactivityPrompt = 10 * time.Minute
	defaultPresenceGrace    = 2 * time.Minute
	defaultMaxDuration      = 2 * time.Hour
	defaultClientGoneGrace  = 90 * time.Second
)

// meetingConfig zijn de gedragsknoppen van een sessie.
type meetingConfig struct {
	Heartbeat        time.Duration // hartslag van de luister-indicator
	InactivityPrompt time.Duration // tijd zonder bediening tot "Ben je er nog?"
	PresenceGrace    time.Duration // aftelling in die vraag
	MaxDuration      time.Duration // harde plafond-timeout op de sessie
	ClientGoneGrace  time.Duration // coulance voor een weggevallen sessiekanaal
	WindowDuration   time.Duration // tijdvenster van het snapshot van de knop-route
	WindowSegments   int           // maximaal aantal segmenten in dat snapshot
	MaxSegments      int           // begrenzing van de RollingTranscript
	MaxAge           time.Duration // idem, op leeftijd
	SpeakMargin      time.Duration // marge na een spreekbeurt
	CommitAfter      time.Duration // uiting die zo lang doorgroeit wordt afgesloten (0 = nooit)
	FinalizeWait     time.Duration // zo lang wacht de knop op de afronding van een lopende uiting (0 = niet wachten)
	EventBuffer      int           // bufferruimte per abonnee
	TurnTimeout      time.Duration // noodrem op één spreekbeurt
	Detect           detectConfig  // topic-detectie en governor
}

func meetingConfigFromGedrag(g Gedrag) meetingConfig {
	cfg := meetingConfig{
		Heartbeat:        5 * time.Second,
		InactivityPrompt: time.Duration(g.InactiviteitPromptSeconden) * time.Second,
		PresenceGrace:    time.Duration(g.InactiviteitSluitSeconden) * time.Second,
		MaxDuration:      time.Duration(g.VergaderingMaxSeconden) * time.Second,
		ClientGoneGrace:  defaultClientGoneGrace,
		WindowDuration:   90 * time.Second,
		WindowSegments:   12,
		MaxSegments:      40,
		MaxAge:           10 * time.Minute,
		SpeakMargin:      time.Second,
		CommitAfter:      defaultCommitAfter,
		FinalizeWait:     defaultFinalizeWait,
		EventBuffer:      64,
		TurnTimeout:      2 * time.Minute,
		Detect: detectConfig{
			Enabled:         true, // de echte evaluatie komt pas met zelfstandig meedoen
			MinWords:        g.DetectieVensterWoorden,
			Interval:        time.Duration(g.DetectieIntervalSeconden) * time.Second,
			ContextAge:      90 * time.Second,
			ContextSegments: 40,
			MaxWindowWords:  300,
			Timeout:         30 * time.Second,
			Quiet:           time.Duration(g.StilteMilliseconden) * time.Millisecond,
			ArousalHalfLife: time.Duration(g.ArousalHalveringSeconden) * time.Second,
		},
	}
	if cfg.InactivityPrompt <= 0 {
		cfg.InactivityPrompt = defaultInactivityPrompt
	}
	if cfg.PresenceGrace <= 0 {
		cfg.PresenceGrace = defaultPresenceGrace
	}
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = defaultMaxDuration
	}
	// Dezelfde defaults als de config, zodat een half ingevulde gedragsconfig
	// geen generator zonder cadans oplevert.
	if cfg.Detect.MinWords <= 0 {
		cfg.Detect.MinWords = 3
	}
	if cfg.Detect.Interval <= 0 {
		cfg.Detect.Interval = 6 * time.Second
	}
	if cfg.Detect.Quiet <= 0 {
		cfg.Detect.Quiet = 1500 * time.Millisecond
	}
	if cfg.Detect.ArousalHalfLife <= 0 {
		cfg.Detect.ArousalHalfLife = 2 * time.Minute
	}
	return cfg
}

// normEagerness normaliseert een gretigheidsstand; onbekend valt terug op
// "normal".
func normEagerness(v string) string {
	if _, ok := eagernessShift[v]; ok {
		return v
	}
	return EagernessNormal
}

// errUnknownMeetingPigeon dekt zowel "bestaat niet" als "zit niet aan tafel".
var errUnknownMeetingPigeon = errors.New("duif: onbekende of niet-vergaderende duif")
var errMeetingCapacity = errors.New("duif: meeting capacity reached")

// De twee gezichten van "vol". Ze wikkelen errMeetingCapacity, zodat elke
// bestaande errors.Is-controle blijft werken, maar de handler ze uit elkaar
// kan houden: alleen bij een eigen open sessie heeft de gebruiker een uitweg.
// Bij bezette plaatsen van anderen is er niets te bevrijden.
var (
	errMeetingCapacitySelf = fmt.Errorf("%w: eigen sessie open", errMeetingCapacity)
	errMeetingCapacityFull = fmt.Errorf("%w: alle plaatsen bezet", errMeetingCapacity)
)

// openRequest zijn de twee uitzonderingen op "gewoon een nieuwe vergadering":
// de uitweg uit de capaciteitsmelding en de duifwissel. Allebei
// raken ze uitsluitend sessies van hetzelfde account.
type openRequest struct {
	// ReplaceOwn sluit de open vergadering(en) van dít account en neemt hun
	// plaats in. Nooit die van iemand anders.
	ReplaceOwn bool
	// Handover is de eigen sessie waarvan de vergadercontext meeverhuist. Een
	// handover vervangt die sessie dus altijd: twee tegelijk zou de
	// capaciteitsregel breken.
	Handover string
}

// meetingEngine is het sessiebeheer van de vergadermodus. Merk op wat er
// níét in staat: geen repository, geen database, geen bestandssysteem — de
// engine kán vergaderinhoud niet opslaan.
type meetingEngine struct {
	personas     *PersonaSet
	llm          llm.Service
	answerStream answerStreamFunc
	// retrieval is dezelfde gedeelde DAG die de chat gebruikt; hij draagt
	// uitsluitend leesseams (KnowledgeReader). Nil = geen kennisbank gewired,
	// en de kennisbank-duif spreekt dan in gap-modus.
	retrieval   *retrieval
	voice       Voice
	clock       Clock
	logger      *slog.Logger
	model       string
	detectModel string
	cfg         meetingConfig
	// devVoice is de dev-only stem-id (DUIF_DEV_VOICE_ID), door de module
	// meegegeven. De engine leest zelf geen omgeving.
	devVoice string
	// queryTimeout begrenst de queryherformulering voor de kennisopzoeking. Nul
	// betekent meetingQueryTimeout; een test zet hem korter om de terugval op
	// het ruwe venster te kunnen zien zonder er seconden op te wachten.
	queryTimeout time.Duration

	// transcriber is de streaming-STT-seam. Nil betekent: er is geen
	// microfoonstroom, en de sessie leunt op de transcript-route.
	transcriber audio.RealtimeTranscriber
	// sttSleep en sttJitter zijn de wachtseams van de zelfherstellende brug;
	// tests lezen de backoff af in plaats van hem uit te zitten.
	sttSleep  func(context.Context, time.Duration) bool
	sttJitter func() float64
	// intervene is de seam waar zelfstandig ingrijpen op landt: de klok roept
	// hem aan zodra een kandidaat mag klinken. Nil betekent dat de doorgelaten
	// kandidaat alleen op het sessiekanaal wordt aangekondigd.
	intervene func(*meetingSession, *meetingCandidate)
	// pickIndex is de trekking binnen een geluidsband; nil = willekeurig.
	pickIndex func(n int) int
	// openTimeout is de eigen deadline van het openen van een stemsessie; 0
	// betekent speakerOpenTimeout. Alleen tests zetten hem korter.
	openTimeout time.Duration

	mu              sync.Mutex
	sessions        map[string]*meetingSession
	maxSessions     int
	maxUserSessions int
}

func (e *meetingEngine) log() *slog.Logger { return logOr(e.logger) }

// open zet een duif aan tafel met de standaardinstellingen: meeluisteren en
// zelfstandig meedoen uit; classificatie kost dan geen modelcalls.
func (e *meetingEngine) open(userID, pigeonID, lang string) (*meetingSession, error) {
	return e.openWith(userID, pigeonID, lang, meetingSettings{})
}

// openWith is dezelfde route mét de sessie-instellingen uit het verzoek.
func (e *meetingEngine) openWith(userID, pigeonID, lang string, settings meetingSettings) (*meetingSession, error) {
	return e.openSession(userID, pigeonID, lang, settings, openRequest{})
}

// openSession is openWith plus de twee eigen-account-uitzonderingen uit
// openRequest: de plaats van je eigen open vergadering innemen en de
// vergadercontext van je eigen vorige duif meenemen.
func (e *meetingEngine) openSession(userID, pigeonID, lang string, settings meetingSettings, req openRequest) (*meetingSession, error) {
	persona, ok := e.personas.Get(pigeonID)
	if !ok || !persona.Vergadering {
		return nil, errUnknownMeetingPigeon
	}
	now := e.clock.Now()
	cfg := e.cfg
	// De cooldown en de beleefdheid staan in de spreeklat van de duif
	// zelf (personas.yaml); de sessie-instelling is nog uitsluitend een
	// expliciete override door de vergadering.
	settings.Eagerness = normEagerness(settings.Eagerness)

	// De stemstatus ligt vast zodra de duif aan tafel zit: één berekening,
	// daarna alleen nog gelezen. Zonder provider is elke duif tekst-only.
	voice := VoiceOff
	if e.voice != nil {
		if _, ok := resolveVoiceID(persona, e.devVoice); ok {
			voice = VoiceReady
		}
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	s := &meetingSession{
		id:           newID(),
		markToken:    newID(),
		userID:       userID,
		pigeonID:     persona.ID,
		persona:      persona,
		lang:         normLang(lang),
		voice:        voice,
		cfg:          cfg,
		settings:     settings,
		clock:        e.clock,
		engine:       e,
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		transcript:   NewRollingTranscript(e.cfg.MaxSegments, e.cfg.MaxAge),
		arousal:      newArousalMeter(now, cfg.Detect.ArousalHalfLife),
		diagnostics:  newMeetingDiagnostics(now),
		listen:       ListenActive,
		startedAt:    now,
		lastActivity: now,
		// Er zit nog niemand op het kanaal: de klok van "de client is weg"
		// loopt vanaf het openen en stopt zodra de eerste SSE-verbinding staat.
		clientGoneSince: now,
		subs:            map[int]chan MeetingEvent{},
		done:            make(chan struct{}),
	}
	if cfg.Detect.Enabled && len(persona.Topics) > 0 {
		s.candidates = newCandidateEngine(e, s)
	}
	if e.transcriber != nil {
		// Liever vals-negatief: zolang de brug niet staat, luistert er
		// niets mee, en dat is wat de indicator zegt.
		s.listen = ListenReconnecting
		s.stt = newSTTBridge(e, s)
	}
	e.mu.Lock()
	var owned []*meetingSession
	for _, current := range e.sessions {
		if current.userID == userID {
			owned = append(owned, current)
		}
	}
	// Een handover die naar een sessie wijst die er niet (meer) is, of naar
	// die van iemand anders, is gewoon een nieuwe vergadering zonder context:
	// eigenaarschap lekt niet.
	var source *meetingSession
	if req.Handover != "" {
		if current, ok := e.sessions[req.Handover]; ok && current.userID == userID {
			source = current
		}
	}
	// Vervangen is alleen vervangen wat van jou is. De plaatsen die anderen
	// bezetten tellen gewoon door; die maakt niemand met een knop vrij.
	// Een wissel is altijd een vervanging van het eigen account, ook als de
	// sessie waarnaar hij wijst intussen vanzelf sloot of nooit bestond: hij
	// verliest dan de context, maar hij hoort nooit op de 429 te stuiten.
	replace := req.ReplaceOwn || req.Handover != ""
	seats, mine := len(e.sessions), len(owned)
	if replace {
		seats, mine = seats-len(owned), 0
	}
	if (e.maxSessions > 0 && seats >= e.maxSessions) || (e.maxUserSessions > 0 && mine >= e.maxUserSessions) {
		e.mu.Unlock()
		cancelCtx()
		// De uitweg beloven we alleen wanneer er iets te vervangen valt. Wie
		// zijn eigen plaats al opgaf en alsnog strandt, zit achter andermans
		// bezette tafel: dan is er geen knop die helpt.
		if len(owned) > 0 && !replace {
			return nil, errMeetingCapacitySelf
		}
		return nil, errMeetingCapacityFull
	}
	inherited := false
	if source != nil {
		inherited = s.inheritFrom(source)
	}
	if replace {
		for _, old := range owned {
			delete(e.sessions, old.id)
		}
	}
	e.sessions[s.id] = s
	e.mu.Unlock()
	if replace {
		// Buiten de lock: close() pakt zijn eigen sessielock en meldt zich
		// daarna bij de engine, die hier al vrij is.
		for _, old := range owned {
			old.close(CloseReasonReplaced)
		}
	}

	e.log().Info("vergadersessie geopend", "component", "duif", "sessie", s.id, "duif", s.pigeonID, "taal", s.lang,
		"vervangen", len(owned), "context_overgenomen", inherited)
	go e.run(s)
	if s.stt != nil {
		go s.stt.run(ctx)
	}
	return s, nil
}

// session zoekt een sessie op id binnen het eigenaarschap van userID; het
// verschil tussen "bestaat niet" en "is niet van jou" lekt niet.
func (e *meetingEngine) session(id, userID string) (*meetingSession, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[id]
	if !ok || s.userID != userID {
		return nil, false
	}
	return s, true
}

func (e *meetingEngine) remove(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sessions, id)
}

func (e *meetingEngine) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.sessions)
}

// shutdown sluit elke openstaande sessie en laat geen goroutine achter.
func (e *meetingEngine) shutdown() {
	e.mu.Lock()
	open := make([]*meetingSession, 0, len(e.sessions))
	for _, s := range e.sessions {
		open = append(open, s)
	}
	e.sessions = map[string]*meetingSession{}
	e.mu.Unlock()
	for _, s := range open {
		s.close(CloseReasonShutdown)
	}
}

// run is de bewakingslus van één sessie: hartslag plus de
// inactiviteitstimers, allebei op de injecteerbare klok.
func (e *meetingEngine) run(s *meetingSession) {
	interval := e.cfg.Heartbeat
	if s.candidates != nil && interval > 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	ticker := e.clock.NewTicker(interval)
	lastHeartbeat := s.startedAt
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C():
			s.expireUnheardAutomatic()
			// Eerst de generator (de klok over een klaarliggende kandidaat),
			// dan de sessie zelf: zo sluit de hartslag elke tick af.
			if s.candidates != nil {
				s.candidates.onTick(now)
			}
			if now.Sub(lastHeartbeat) >= e.cfg.Heartbeat {
				lastHeartbeat = now
				if !s.tick(now) {
					return
				}
			}
		}
	}
}

// meetingSession is één duif aan één tafel. Alle inhoud (venster, lopende
// beurt, abonnees) leeft hier en verdwijnt met close.
type meetingSession struct {
	diagnostics    *meetingDiagnostics
	autonomyPaused bool
	playback       playbackTracker
	turnStartedAt  time.Time
	turnTextSeen   bool
	// spokenTail is de staart van wat de duif in de lopende beurt gezegd heeft:
	// de maatstaf waartegen de echo-zeef een binnenkomend segment houdt
	// (meeting_freshness.go). Hij leeft net zo lang als de beurt plus haar
	// marge en verlaat de server nooit.
	spokenTail string
	// stopReasonSeen en stopReasonGen houden bij waaróm de laatst afgebroken
	// beurt afgebroken werd, en welke beurt dat was. De spreekbeurt leest ze
	// als hij merkt dat zijn context geannuleerd is: "de gebruiker drukte
	// stop" is iets heel anders dan "de beurt viel zomaar weg", en zonder dit
	// onderscheid is elke stille beurt in het log hetzelfde.
	stopReasonSeen string
	stopReasonGen  uint64
	id             string
	// markToken is het willekeurige token in het merkteken van de duifwissel
	// (previousPigeonMark). Het staat los van id: id staat in de URL en is dus
	// bekend bij wie de sessie bedient, terwijl dit token de prompt nooit
	// verlaat. Alleen het merkteken mét dit token telt als machinemerkteken,
	// zodat een sprekerlabel er geen kan nabootsen.
	markToken string
	userID    string
	pigeonID  string
	persona   *Persona
	lang      string
	// voice is onveranderlijk na openWith: de UI leest hem op het state-event
	// en het open-antwoord.
	voice    VoiceState
	cfg      meetingConfig
	settings meetingSettings
	clock    Clock
	engine   *meetingEngine
	// candidates is de kandidaatgenerator van deze sessie; nil zonder
	// thema's of met de generator uitgezet in de config.
	candidates *candidateEngine
	// stt is de continu meeluisterende brug; nil zonder transcriber.
	stt *sttBridge
	// arousal is de meter van de non-verbale duif: sessiestand, geen
	// detectorstand, zodat de handmatige knop en de UI-meter een uitgevallen
	// detector overleven.
	arousal *arousalMeter

	// ctx leeft zo lang als de sessie: achtergrondwerk dat niet aan één
	// spreekbeurt hangt (de kandidaatevaluaties) hangt eraan en stopt
	// vanzelf zodra de sessie sluit.
	ctx       context.Context
	cancelCtx context.CancelFunc

	mu         sync.Mutex
	transcript *RollingTranscript
	// contributions is wat de duif zélf zei. Het gaat bij een duifwissel mee als
	// "wat de vorige duif zei", en het gaat mee in de prompt van deze duif
	// zelf: in het venster waarop zij antwoordt en in het blok boven haar
	// kandidaatvenster. Het verlaat de server nooit en het verdwijnt met de
	// sessie.
	//
	// Het staat los van het transcript en dus ook los van de 90 seconden
	// daarvan: wat zij zei blijft de hele vergadering bij haar, begrensd op
	// maxContributions.
	contributions []TranscriptSegment
	// lastPoint is de laatste reeks eigen bijdragen op hetzelfde menselijke
	// venster: of een knopdruk iets nieuws te beantwoorden heeft.
	lastPoint ownPoint
	listen    ListenState
	startedAt time.Time
	// lastActivity is de laatste bediening door een mens (noteUserActivity);
	// de inactiviteitsbewaking telt vanaf hier.
	lastActivity time.Time
	// clientGoneSince is niet-nul zodra er geen enkele abonnee meer op het
	// sessiekanaal zit. Een tabblad dat weg is (gesloten, gecrasht, in slaap)
	// houdt zo geen plaats aan tafel bezet: na de coulanceperiode sluit de
	// sessie zichzelf.
	clientGoneSince    time.Time
	lastHumanSeq       uint64
	lastSpeechActivity time.Time
	partialHash        [32]byte
	uncommittedSpeech  bool
	lastSegment        time.Time // laatste gecommitte menselijke uiting (beleefdheidsregel)
	lastSoundID        string    // previous admitted asset; only technical variation, guarded by mu
	lastUtterance      time.Time // laatste duif-uiting, van welke bron dan ook
	// candidate is de klaarliggende bijdrage van de duif. Hij wordt vervangen
	// door zijn opvolger, weggegeven zodra hij klinkt, en sterft met de sessie
	// — nergens anders bewaard, want vergaderinhoud blijft vluchtig.
	candidate  *meetingCandidate
	presenceAt time.Time // niet-nul = "Ben je er nog?" staat open
	segSeq     uint64    // volgnummergever van de transcriptsegmenten
	// segmentWait gaat dicht zodra er een vers menselijk segment landt. Het is
	// het seintje waarop de knop wacht terwijl de lopende uiting afgerond wordt
	// (meeting_settle.go); nil zolang er niemand wacht.
	segmentWait    chan struct{}
	speaking       bool
	turnAutomatic  bool
	turnTriggerSeq uint64
	turnTriggerAt  time.Time
	speakUntil     time.Time
	turnGen        uint64
	cancelTurn     context.CancelFunc
	closed         bool
	dropped        int
	nextSub        int
	subs           map[int]chan MeetingEvent
	done           chan struct{}
}

// subscribe opent een abonnement op het sessiekanaal. Het eerste bericht is
// altijd de sessiestatus, zodat een verse UI meteen klopt.
func (s *meetingSession) subscribe() (<-chan MeetingEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan MeetingEvent, s.cfg.EventBuffer)
	if s.closed {
		close(ch)
		return ch, func() {}
	}
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	s.clientGoneSince = time.Time{}
	// De sessiestatus draagt ook de knoppen voor zelfstandig meedoen mee: een
	// client die opnieuw verbindt (of net gekickt is) leest de lévende stand
	// terug in plaats van zijn eigen laatste aanname.
	settings := s.settingsLocked()
	var detector *DetectorStatus
	if s.candidates != nil {
		status := s.candidates.status()
		detector = &status
	}
	state := MeetingEvent{
		Type: EventState, SessionID: s.id, PigeonID: s.pigeonID, Lang: s.lang, Detector: detector,
		Listening: s.listen, Speaking: s.speaking, Voice: s.voice, Settings: &settings,
		AutonomyPaused: boolPointer(s.autonomyPaused), PlaybackTracking: s.settings.PlaybackTracking, TurnID: s.turnIDLocked(), Playback: s.playbackPhaseLocked(),
	}
	// Een client die opnieuw verbindt terwijl "Ben je er nog?" openstaat, krijgt
	// de vraag en de resterende tijd meteen terug.
	if grace := s.presenceGraceLocked(s.clock.Now()); grace > 0 {
		state.GraceMS, state.Message = grace, presenceCheckMessage
	}
	ch <- state
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, id)
			if len(s.subs) == 0 && !s.closed {
				// De laatste luisteraar is weg. Komt er binnen de
				// coulanceperiode geen nieuwe (de browser verbindt een
				// weggevallen SSE-kanaal vanzelf opnieuw), dan sluit de tick
				// de sessie.
				s.clientGoneSince = s.clock.Now()
			}
			stop := len(s.subs) == 0 && s.settings.PlaybackTracking && s.speaking
			gen := s.turnGen
			s.mu.Unlock()
			if stop {
				s.stopSpeakingGeneration("playback_disconnected", gen)
			}
		})
	}
}

// eventClass zegt hoe een event zich gedraagt als de buffer van een abonnee
// vol staat.
type eventClass int

const (
	// classStream: vervalt meteen. Een audioframe, een partial, een hartslag
	// of een meterstand die te laat komt is per stuk waardeloos.
	classStream eventClass = iota
	// classControl: bepaalt wat de UI denkt te weten. Stil verdwijnen laat een
	// scherm in "de duif praat nog" hangen, dus dan gaat de abonnee eruit.
	classControl
)

// classOf pint de klasse van een eventtype vast. Hij faalt dicht: een type dat
// hier niet staat is besturing, zodat een nieuw event nooit ongemerkt in de
// wegwerpklasse belandt (de tabeltoets in meeting_test.go bewaakt dat).
func classOf(ev MeetingEvent) eventClass {
	switch ev.Type {
	case EventTTSAudio, EventListening, EventArousal:
		return classStream
	case EventTranscript:
		// Een partial is weergave; een gecommit segment is de vergadering.
		if ev.Final {
			return classControl
		}
		return classStream
	case EventState, EventPersona, EventText, EventSources, EventTextDone,
		EventTTSDone, EventResponseDone,
		EventBusy, EventStopped, EventError, EventPresenceCheck, EventSessionClosed,
		EventIntervention, EventDetectorStatus, EventSound, EventSettings, EventPlaybackState,
		EventCandidate:
		return classControl
	default:
		return classControl
	}
}

// dispatchLocked stuurt een event naar elke abonnee, en blokkeert nooit — ook
// niet een microseconde. Dat is de harde eis: dispatch draait onder s.mu, dus
// wachten op een trage lezer zou de hele sessie (en elke andere aanroeper van
// die lock) meenemen.
//
// Op een volle buffer beslist de klasse. Stroomevents vervallen geteld.
// Besturingsevents kosten de abonnee zijn abonnement: het kanaal gaat dicht en
// de inschrijving verdwijnt. De SSE-lus eindigt daarop, de EventSource van de
// browser verbindt opnieuw en subscribe() geeft hem een verse state — beter
// een korte herverbinding dan een scherm dat stilletjes achterloopt.
//
// Omdat elke verzending onder s.mu gebeurt en het kanaal in hetzelfde
// kritieke stuk uit s.subs verdwijnt, kan er na close(ch) niets meer op.
func (s *meetingSession) dispatchLocked(ev MeetingEvent) {
	s.dispatchClassLocked(ev, classOf(ev))
}

// dispatchClassLocked is dispatchLocked met een expliciete klasse, voor het
// ene geval waar het type niet genoeg zegt: de hartslag. Periodiek is hij
// stroom, maar als hij een échte overgang van de luisterstand meldt is hij
// besturing (zie setListenState).
func (s *meetingSession) dispatchClassLocked(ev MeetingEvent, class eventClass) {
	if ev.SessionID == "" {
		ev.SessionID = s.id
	}
	for id, ch := range s.subs {
		select {
		case ch <- ev:
			continue
		default:
		}
		if class == classStream {
			s.dropped++
			continue
		}
		close(ch)
		delete(s.subs, id)
		// Alleen het type, nooit de inhoud: vergaderinhoud komt nooit in het log.
		s.engine.log().Debug("vergaderabonnee losgekoppeld", "component", "duif",
			"sessie", s.id, "reason", "subscriber_kicked", "type", ev.Type)
	}
}

// publish stuurt een sessie-breed event (niet gebonden aan een spreekbeurt).
func (s *meetingSession) publish(ev MeetingEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.dispatchLocked(ev)
}

func (s *meetingSession) droppedEvents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

func (s *meetingSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// setListenState verandert de luister-indicator en meldt dat meteen.
// Dit is de seam waarop de STT-brug straks "verbinding herstellen…" zet.
func (s *meetingSession) setListenState(state ListenState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.listen == state {
		return
	}
	s.listen = state
	// Een overgang is geen hartslag maar nieuws: hij mag niet stil wegvallen.
	// Hij draagt wel dezelfde resterende tijd als de hartslag: de client leest
	// een listening_heartbeat zonder graceMs als "er staat geen vraag open".
	s.dispatchClassLocked(MeetingEvent{Type: EventListening, Listening: state, Speaking: s.speaking, AutonomyPaused: boolPointer(s.autonomyPaused), GraceMS: s.presenceGraceLocked(s.clock.Now())}, classControl)
}

// listenState is de huidige waarheidsgetrouwe luisterstand.
func (s *meetingSession) listenState() ListenState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listen
}

// Partials contribute only a timestamp and fixed-size fingerprint to the pause
// gate. Their text remains a transient SSE event, never generator input.
//
// Een partial breekt géén lopende bijdrage meer af. Dat deed hij wel,
// en het was de meest zichtbare kapotte kant van de modus: elk kuchje, elke
// stoelbeweging die de STT een partial ontlokte, doodde de beurt tussen "de
// duif begint" en het eerste audioframe. Alleen een écht gecommit menselijk
// segment vervangt nu de aanleiding (zie appendSegment).
func (s *meetingSession) publishPartial(text, speaker string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	now := s.clock.Now()
	if strings.TrimSpace(text) != "" && !s.echoPossibleLocked(now) {
		hash := sha256.Sum256([]byte(text))
		if !s.uncommittedSpeech || hash != s.partialHash {
			s.lastSpeechActivity = now
			s.partialHash = hash
		}
		s.uncommittedSpeech = true
	}
	// Hetzelfde plafond als op een gecommit segment: het label komt van de
	// STT-dienst en de deelnemersweergave zet het op het scherm.
	s.dispatchLocked(MeetingEvent{Type: EventTranscript, Text: text, Speaker: capRunes(speaker, maxSpeakerRunes), Final: false})
	s.mu.Unlock()
}

// appendSegment voegt een gecommit segment van de microfoon toe. Alleen daar
// bestaat de echo: de luidspreker staat in dezelfde kamer, dus wat er binnenkomt
// terwijl de duif hoorbaar is kan haar eigen stem zijn. Zo'n segment wordt
// gemarkeerd en telt niet als vergaderinhoud.
func (s *meetingSession) appendSegment(text, speaker string) bool {
	return s.appendSegmentFrom(text, speaker, true)
}

// appendTypedSegment is de getypte bijdrage (POST .../transcript). Daar is geen
// luidspreker en dus geen echo: wie tijdens de spraak van de duif iets intikt
// is een mens die haar aanspreekt, en die regel hoort gewoon meegewogen te
// worden zodra de duif is uitgesproken. Vóór deze splitsing verdween elk woord
// dat tijdens haar bijdrage viel — in een test zo achttien seconden gesprek,
// inclusief een directe vraag aan de duif.
func (s *meetingSession) appendTypedSegment(text, speaker string) bool {
	return s.appendSegmentFrom(text, speaker, false)
}

// appendSegmentFrom voegt een gecommit transcriptsegment toe aan het venster en
// markeert het als de duif op dat moment zelf aan het woord was én de route
// een echo kan dragen.
//
// Een segment is geen teken van leven voor de inactiviteitsbewaking: de
// microfoon hoort ook zaalrumoer en de echo van de duif, en in de praktijk
// hield dat een vergadering zonder iemand aan het scherm uren open.
// Alleen bediening telt (noteUserActivity), en een openstaande vraag "Ben je
// er nog?" blijft dus ook staan als er gepraat wordt.
func (s *meetingSession) appendSegmentFrom(text, speaker string, echoRoute bool) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	now := s.clock.Now()
	s.segSeq++
	seg := TranscriptSegment{
		Seq:  s.segSeq,
		Text: text,
		At:   now,
		// Het plafond zit hier en niet alleen in de route: de STT-brug voedt
		// dezelfde sessie-API en levert het sprekerlabel van de dienst aan,
		// buiten elke handlervalidatie om.
		Speaker:      capRunes(speaker, maxSpeakerRunes),
		PigeonWindow: echoRoute && s.echoLocked(now, text),
	}
	s.transcript.Append(seg)
	s.diagnostics.mark(now, "segment_commit", "", 0)
	// De luisterstand komt níét van hier. Met een STT-brug is die brug de enige
	// die weet of er werkelijk iemand meeluistert; een segment dat via
	// POST .../transcript binnenkomt terwijl de brug in backoff staat zou de
	// indicator anders op "listening" zetten terwijl er niets luistert. De regel
	// is: liever vals-negatief dan vals-positief. Zonder brug staat de sessie al op
	// ListenActive vanaf het openen.
	//
	// De lopende uiting is hoe dan ook afgesloten: ook een segment dat als echo
	// binnenkomt is een final van de provider, en dus geen partial meer. Stond
	// dit in de tak hieronder, dan bleef uncommittedSpeech na een echo-final
	// openstaan en vroeg elke volgende knopdruk opnieuw om een afronding die er
	// al was — de duif zat dan de volle wachttijd uit en meldde "de zin was nog
	// niet bij me" terwijl de oorzaak haar eigen stem was.
	s.uncommittedSpeech = false
	s.partialHash = [32]byte{}
	var cancel context.CancelFunc
	if !seg.PigeonWindow {
		s.lastSegment = now
		s.lastSpeechActivity = now
		s.lastHumanSeq = seg.Seq
		s.signalSegmentLocked()
		cancel = s.cancelUnheardAutomaticLocked("context_changed")
	}
	s.dispatchLocked(MeetingEvent{
		Type: EventTranscript, Text: seg.Text, Speaker: seg.Speaker,
		Final: true, PigeonWindow: seg.PigeonWindow,
	})
	det := s.candidates
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	// De generator is een zijtak op het venster, geen schakel in de keten: hij
	// draait buiten de sessielock en kan de knop nooit ophouden.
	if det != nil {
		det.onSegment(seg)
	}
	return true
}

// noteSpokenTextLocked schuift een stukje geleverde duiftekst in de staart
// waartegen de echo-zeef meet. Alleen de laatste echoSpokenTailRunes blijven
// staan: verder terug dan dat kan geen echo meer vandaan komen.
func (s *meetingSession) noteSpokenTextLocked(delta string) {
	if delta == "" {
		return
	}
	s.spokenTail += delta
	if r := []rune(s.spokenTail); len(r) > echoSpokenTailRunes {
		s.spokenTail = string(r[len(r)-echoSpokenTailRunes:])
	}
}

// maxContributions begrenst het geheugen van eigen bijdragen. Het is geen
// archief maar een overdrachtsvenster: net als het scherm houdt het de laatste
// twaalf vast en laat het de rest los.
const maxContributions = 12

// noteContribution legt vast wat de duif zojuist zei: voor de duif die na haar
// aan tafel komt (die krijgt deze regels mét deze auteur mee) en voor haarzelf,
// want de volgende knopdruk leest ze terug om erop voort te bouwen in plaats van
// dezelfde punten opnieuw te maken.
//
// `heard` is het laatste menselijke volgnummer in het venster waarop zij sprak
// en `ground` waar de bijdrage op rustte: daarmee weet een volgende knopdruk of
// er sindsdien iets nieuws gezegd is, en of er iets met bronnen te verdiepen
// valt.
func (s *meetingSession) noteContribution(text string, heard uint64, ground ownGrounding) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.segSeq++
	s.contributions = append(s.contributions, TranscriptSegment{
		Seq: s.segSeq, Text: text, At: s.clock.Now(), PigeonID: s.pigeonID,
		Speaker: pickLang(s.persona.Naam, s.lang),
	})
	if len(s.contributions) > maxContributions {
		s.contributions = s.contributions[len(s.contributions)-maxContributions:]
	}
	s.notePointLocked(text, heard, ground)
}

// inheritFrom verhuist de vergadercontext van de vorige duif naar deze sessie:
// het menselijke transcript zoals het was, plus de bijdragen van die
// duif — met haar naam eraan vast. Er wordt niets opgeslagen en niets over de
// lijn gestuurd; dit is één verplaatsing binnen het geheugen van de server, en
// de oude sessie sluit er direct achteraan.
//
// De nieuwe sessie is hier nog van niemand: er draait nog geen goroutine op,
// dus zij heeft haar eigen lock niet nodig. De bron wel — die leeft.
//
// Sluit de bron net terwijl de wissel binnenkomt (een sessie is even dicht
// vóór zij uit de engine verdwijnt), dan gaat de overdracht niet door: het
// venster van een gesloten vergadering is losgelaten en half meenemen is
// erger dan niets meenemen. De wissel zelf loopt gewoon door, het log meldt
// `context_overgenomen=false`, en het scherm zegt er niets over.
func (s *meetingSession) inheritFrom(src *meetingSession) bool {
	src.mu.Lock()
	if src.closed {
		src.mu.Unlock()
		return false
	}
	segs := make([]TranscriptSegment, 0, len(src.transcript.segments)+len(src.contributions))
	segs = append(segs, src.transcript.segments...)
	segs = append(segs, src.contributions...)
	seq := src.segSeq
	src.mu.Unlock()
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].Seq < segs[j].Seq })

	for _, seg := range segs {
		s.transcript.Append(seg)
		if seg.PigeonID == "" && !seg.PigeonWindow {
			s.lastHumanSeq = seg.Seq
			s.lastSegment = seg.At
			s.lastSpeechActivity = seg.At
		}
	}
	// De volgnummers lopen door: wat hierna binnenkomt staat ná de erfenis.
	s.segSeq = seq
	// Komt deze duif terug aan een tafel waar zij al sprak, dan telt wat zij
	// toen zei als haar vorige bijdrage: zonder nieuwe menselijke inbreng
	// sindsdien is een knopdruk geen gewone beurt. De grond van die regels
	// ging niet mee, dus er valt niets met bronnen te verdiepen.
	s.lastPoint = inheritedPoint(segs, s.pigeonID)
	if s.candidates != nil {
		// Alles wat meekwam is context, geen aanleiding: de nieuwe duif valt
		// niet meteen in op een zin die de vorige al gehoord heeft.
		s.candidates.mu.Lock()
		s.candidates.lastSeq = seq
		s.candidates.lastSeen = seq
		s.candidates.mu.Unlock()
	}
	return true
}

// governorState is de sessiestand waartegen de governor een hit weegt.
func (s *meetingSession) governorState() governorState {
	s.mu.Lock()
	defer s.mu.Unlock()
	lastSpeech := s.lastSegment
	if s.lastSpeechActivity.After(lastSpeech) {
		lastSpeech = s.lastSpeechActivity
	}
	return governorState{
		Autonomous:           s.settings.Autonomous,
		Paused:               s.autonomyPaused,
		Eagerness:            s.settings.Eagerness,
		Cooldown:             s.settings.Cooldown,
		Speaking:             s.speaking,
		PresencePending:      !s.presenceAt.IsZero(),
		LastUtterance:        s.lastUtterance,
		LastSpeechActivity:   lastSpeech,
		UncommittedSpeech:    s.uncommittedSpeech,
		ListeningUnavailable: s.listen != ListenActive,
	}
}

// detectionWindow is het snapshot waarop de kandidaatgenerator schrijft.
func (s *meetingSession) detectionWindow(afterSeq uint64) transcriptWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript.DetectionSnapshot(s.clock.Now(), afterSeq, s.cfg.Detect)
}

// segmentCount is het aantal segmenten in het venster (een telling, nooit
// inhoud — geschikt voor de wire en voor logs).
func (s *meetingSession) segmentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript.Len()
}

// window is het snapshot waarop de duif op de knop antwoordt.
func (s *meetingSession) window() []TranscriptSegment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript.Snapshot(s.clock.Now(), s.cfg.WindowDuration, s.cfg.WindowSegments)
}

// setCandidate legt de nieuwe kandidaat neer en meldt hem. Nil betekent: de
// duif heeft nu niets — dat is óók nieuws, want het kandidaatblok in de
// vergadering moet dan verdwijnen.
func (s *meetingSession) setCandidate(c *meetingCandidate) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if s.candidate == nil && c == nil {
		s.mu.Unlock()
		return
	}
	s.candidate = c
	s.dispatchLocked(MeetingEvent{Type: EventCandidate, Candidate: c.ref()})
	s.mu.Unlock()
}

// heldCandidate is de kandidaat die nu klaarligt.
func (s *meetingSession) heldCandidate() *meetingCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidate
}

// takeCandidate geeft de kandidaat af en wist hem. Hij komt maar één keer
// terug: twee tikken tegelijk kunnen de duif niet twee keer aan het woord
// helpen.
func (s *meetingSession) takeCandidate(id string) *meetingCandidate {
	s.mu.Lock()
	c := s.candidate
	if c == nil || c.ID != id || s.closed {
		s.mu.Unlock()
		return nil
	}
	s.candidate = nil
	s.dispatchLocked(MeetingEvent{Type: EventCandidate})
	s.mu.Unlock()
	return c
}

// maxOwnContributions is hoeveel eigen bijdragen er in één prompt meegaan —
// naar de kandidaatgenerator én naar het venster van de knop.
// Genoeg om niet in herhaling te vallen, kort genoeg om het gesprek niet te
// verdringen: het venster zelf houdt hoogstens WindowSegments (12) regels vast,
// en vijf eigen regels daartussen blijft leesbaar. Wat er verder terug ligt is
// vijf beurten later zelden nog het punt waar de tafel op zit te wachten;
// s.contributions bewaart het wél, want de duifwissel heeft de hele reeks nodig.
const maxOwnContributions = 5

// maxOwnContributionRunes is het plafond op de omvang van datzelfde blok. Een
// bijdrage is er een van 2–4 gesproken zinnen (~250–450 tekens) en hard begrensd
// op answerMeetingVisibleTokens, dus vijf normale bijdragen passen er ruim in.
// Loopt één beurt uit — een afgekapte of ongewoon lange uiting — dan groeit de
// prompt niet mee: de oudste bijdragen vallen af tot het weer past. De jongste
// blijft altijd staan; zonder haar is de hele voorziening zinloos.
const maxOwnContributionRunes = 2400

// ownContributionSegments is wat déze duif in deze sessie al bijdroeg, oudste
// eerst, begrensd op aantal en omvang. Eén opslag voor twee doelen:
// s.contributions draagt tekst én auteur, zodat de duifwissel de regels
// met naam kan doorgeven en de duif zelf hier alleen haar eigen regels uit
// leest — een overgenomen regel van een andere duif is niet van haar.
func (s *meetingSession) ownContributionSegments() []TranscriptSegment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TranscriptSegment, 0, maxOwnContributions)
	for _, seg := range s.contributions {
		if seg.PigeonID != s.pigeonID {
			continue
		}
		out = append(out, seg)
	}
	if len(out) > maxOwnContributions {
		out = out[len(out)-maxOwnContributions:]
	}
	runes := 0
	for _, seg := range out {
		runes += len([]rune(seg.Text))
	}
	for len(out) > 1 && runes > maxOwnContributionRunes {
		runes -= len([]rune(out[0].Text))
		out = out[1:]
	}
	return out
}

// isSpeaking zegt of het spreekslot bezet is.
func (s *meetingSession) isSpeaking() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.speaking
}

// noteUtterance boekt een duif-uiting die geen spreekbeurt is (een geluid van
// de non-verbale duif). De cooldown van de governor telt hem mee, net als een
// beurt via de knop. De inactiviteitsbewaking telt hem niet: een duif die
// vanzelf koert houdt anders een lege zaal open.
func (s *meetingSession) noteUtterance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUtterance = s.clock.Now()
}

// settingsSnapshot is de huidige stand van de knoppen voor zelfstandig meedoen.
func (s *meetingSession) settingsSnapshot() SessionSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsLocked()
}

// settingsLocked is settingsSnapshot voor aanroepers die s.mu al vasthouden.
func (s *meetingSession) settingsLocked() SessionSettings {
	return SessionSettings{
		TurnID:           s.turnIDLocked(),
		Autonomous:       s.settings.Autonomous,
		AutonomyPaused:   s.autonomyPaused,
		PlaybackTracking: s.settings.PlaybackTracking,
		Eagerness:        s.settings.Eagerness,
		CooldownSeconds:  int(s.settings.Cooldown / time.Second),
	}
}

// applySettings stelt de knoppen voor zelfstandig meedoen live bij. Nil-velden
// blijven staan;
// uitzetten werkt per direct, omdat de governor de stand bij elke weging
// opnieuw uit de sessie leest. Een lopende beurt loopt gewoon af.
func (s *meetingSession) applySettings(autonomous *bool, eagerness string, cooldown *time.Duration) SessionSettings {
	s.mu.Lock()
	if autonomous != nil {
		// Changing the preference is not a versioned resume. The client uses
		// /resume after an explicit off-to-on action, so a delayed settings
		// request cannot undo a more recent Stop.
		s.settings.Autonomous = *autonomous
	}
	if eagerness != "" {
		s.settings.Eagerness = normEagerness(eagerness)
	}
	if cooldown != nil {
		s.settings.Cooldown = *cooldown
	}
	out := s.settingsLocked()
	if !s.closed {
		s.dispatchLocked(MeetingEvent{Type: EventSettings, Settings: &out, AutonomyPaused: boolPointer(s.autonomyPaused)})
	}
	s.mu.Unlock()
	s.engine.log().Info("vergaderinstellingen bijgesteld", "component", "duif", "sessie", s.id,
		"zelfstandig", out.Autonomous, "gretigheid", out.Eagerness, "cooldown_seconden", out.CooldownSeconds)
	return out
}

// beginTurn claimt het spreekslot. Is de duif al aan het woord, dan is dat
// een busy — geen tweede pipeline. Een zelfstandige beurt begint ook niet
// zolang "Ben je er nog?" openstaat: wie misschien al weg is, krijgt geen
// nieuwe bijdrage over zich heen. De governor houdt haar daar al
// tegen; dit is de laatste deur, voor de beurt die net door de weging kwam.
func (s *meetingSession) beginTurn(automatic ...bool) (uint64, context.Context, context.CancelFunc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	auto := len(automatic) > 0 && automatic[0]
	if s.closed || s.speaking || (auto && (s.autonomyPaused || !s.settings.Autonomous || !s.presenceAt.IsZero())) {
		return 0, nil, nil, false
	}
	s.speaking = true
	s.turnAutomatic = auto
	// Wat er nú gecommit is, is de aanleiding van deze beurt. Zegt er iemand
	// daarna iets nieuws terwijl de duif nog geen geluid maakte, dan is de
	// aanleiding vervangen en breekt de beurt af (meeting_freshness.go).
	s.turnTriggerSeq = s.lastHumanSeq
	s.turnGen++
	s.playback = playbackTracker{}
	s.turnTextSeen = false
	// Een nieuwe beurt begint met een schone staart: wat de vorige duifzin was
	// heeft zijn marge gehad en mag geen segment van nu meer wegzetten.
	s.spokenTail = ""
	s.turnStartedAt = s.clock.Now()
	s.diagnostics.mark(s.turnStartedAt, "generation_started", "", 0)
	s.lastUtterance = s.turnStartedAt
	// De beurt hangt niet aan het HTTP-verzoek dat hem startte: de knopdruk
	// is meteen klaar, de duif praat door over het sessiekanaal.
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.TurnTimeout)
	s.cancelTurn = cancel
	return s.turnGen, ctx, cancel, true
}

// emitTurn stuurt een event dat bij beurt `gen` hoort. Na een stopknop (die
// de generatie ophoogt) verdwijnen late chunks en audioframes hier — dát is
// de garantie "na `stopped` geen enkel audioframe meer".
func (s *meetingSession) emitTurn(gen uint64, ev MeetingEvent) bool {
	return s.emitTurnEnding(gen, ev, false)
}

// finishTurn stuurt het laatste event van beurt `gen` (response_done) en geeft
// in dezelfde lock het spreekslot terug. Wie response_done ziet en meteen weer
// op de knop drukt, krijgt zo nooit een busy van een beurt die al klaar is: met
// een losse endTurn ná het event zat daar een venster tussen, ter grootte van
// een logregel en de uitgestelde opruiming van de beurt. Loopt het
// geluid nog (afspeelbewaking), dan blijft het slot bezet tot de client het
// einde meldt, net als bij endTurn.
func (s *meetingSession) finishTurn(gen uint64, ev MeetingEvent) bool {
	return s.emitTurnEnding(gen, ev, true)
}

func (s *meetingSession) emitTurnEnding(gen uint64, ev MeetingEvent, last bool) bool {
	s.mu.Lock()
	if s.closed || gen != s.turnGen {
		s.mu.Unlock()
		return false
	}
	if s.unheardAutomaticExpiredLocked() {
		cancel := s.cancelUnheardAutomaticLocked("context_changed")
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return false
	}
	s.observeTurnLocked(gen, &ev)
	if ev.Type == EventSound && ev.Sound != nil {
		s.lastSoundID = ev.Sound.ID
	}
	s.dispatchLocked(ev)
	if last {
		s.endTurnLocked(gen)
	}
	s.mu.Unlock()
	return true
}

// endTurn geeft het spreekslot terug na een normaal afgelopen beurt. Hij is
// idempotent: de beurtroutines stellen hem uit, ook als finishTurn het slot al
// teruggaf.
func (s *meetingSession) endTurn(gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endTurnLocked(gen)
}

func (s *meetingSession) endTurnLocked(gen uint64) {
	if gen != s.turnGen {
		return // de beurt was al gestopt; stopSpeaking heeft opgeruimd
	}
	if s.playback.generationDone {
		return // finishTurn sloot deze beurt al af
	}
	s.playback.generationDone = true
	if s.playback.failed || s.playback.answerFailed {
		// Deze beurt eindigde niet netjes: de weergave viel om, of de
		// antwoordketen liep vast. In beide gevallen is het venster weg en
		// mogen late frames niet meer terugkomen. Het verschil zit in wat
		// erna mag: alleen een echte weergavestoring pauzeert zelfstandig
		// meedoen. Een mislukte antwoordketen is één beurt — de gebruiker
		// heeft de fout gezien, en wat de autonomie degradeert is de
		// storingsreeks van de generator zelf.
		if s.playback.failed {
			s.autonomyPaused = true
		} else {
			s.diagnostics.mark(s.clock.Now(), "turn_failed", "answer_chain", 0)
		}
		paused := s.autonomyPaused
		s.releaseTurnLocked("failed")
		if !s.playback.expected {
			s.speakUntil = time.Time{}
		}
		s.playback = playbackTracker{}
		s.turnGen++
		settings := s.settingsLocked()
		s.dispatchLocked(MeetingEvent{Type: EventSettings, Settings: &settings, AutonomyPaused: boolPointer(paused)})
		return
	}
	s.diagnostics.mark(s.clock.Now(), "generation_done", "", s.clock.Now().Sub(s.turnStartedAt))
	if s.settings.PlaybackTracking && s.playback.expected && !s.playback.ended {
		s.cancelTurn = nil
		s.dispatchLocked(MeetingEvent{Type: EventPlaybackState, TurnID: s.turnIDLocked(), Playback: s.playbackPhaseLocked(), Speaking: true})
		return
	}
	s.releaseTurnLocked("ended")
	// De rust van deze duif loopt vanaf het moment dat zij klaar is, niet
	// vanaf het moment dat zij begon.
	s.lastUtterance = s.clock.Now()
	if !s.playback.expected {
		s.speakUntil = time.Time{}
	}
}

// stopSpeaking is de stopknop: de lopende beurt wordt geannuleerd en
// bevestigd met `stopped`. Luisteren en het venster blijven onaangeroerd —
// stoppen is geen sessie-einde.
func (s *meetingSession) stopSpeaking() bool { return s.stopSpeakingReason("user_stop") }

func (s *meetingSession) stopSpeakingReason(reason string) bool {
	return s.stopSpeakingGeneration(reason, 0)
}

func (s *meetingSession) stopSpeakingGeneration(reason string, expected uint64) bool {
	s.mu.Lock()
	if s.closed || (expected != 0 && expected != s.turnGen) {
		s.mu.Unlock()
		return false
	}
	was := s.speaking
	hadAudio := s.playback.expected || s.clock.Now().Before(s.speakUntil)
	s.autonomyPaused = true
	s.playback = playbackTracker{}
	s.diagnostics.mark(s.clock.Now(), "stop", reason, 0)
	s.stopReasonSeen, s.stopReasonGen = reason, s.turnGen
	cancel := s.cancelTurn
	now := s.clock.Now()
	s.speaking = false
	s.cancelTurn = nil
	s.turnGen++
	s.speakUntil = now.Add(s.cfg.SpeakMargin)
	if !hadAudio {
		s.speakUntil = time.Time{}
	}
	if was {
		s.lastUtterance = now
	}
	if !s.closed {
		s.dispatchLocked(MeetingEvent{Type: EventStopped, Listening: s.listen, AutonomyPaused: boolPointer(true), Reason: reason, TurnID: s.turnIDLocked()})
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return was
}

// stopReasonFor geeft de reden waarom beurt `gen` afgebroken werd, of leeg
// wanneer er voor deze beurt geen reden genoteerd is.
func (s *meetingSession) stopReasonFor(gen uint64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopReasonGen != gen {
		return ""
	}
	return s.stopReasonSeen
}

// noteUserActivity is het enige teken van leven voor de inactiviteitsbewaking:
// iemand bediende het scherm. Elke route die een mens met een klik of
// toets start roept hem aan, en de browser meldt er de klikken en toetsen
// tussendoor mee via POST .../presence. Hij zet de klok terug en beantwoordt
// een openstaande "Ben je er nog?"; de hartslag zegt dat daarna tegen elk
// tabblad (graceMs valt weg). Hij geeft terug of er een vraag openstond.
//
// Wat níét hierlangs komt, telt ook niet: spraak, de duif zelf, afspeel-
// meldingen. Een vergadering waar niemand meer aan het scherm zit, stopt dus
// ook in een rumoerige zaal.
func (s *meetingSession) noteUserActivity() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	pending := !s.presenceAt.IsZero()
	s.presenceAt = time.Time{}
	s.lastActivity = s.clock.Now()
	return pending
}

// presenceGraceLocked is wat er nog over is van de aftelling, als duur: de
// client rekent zijn eigen klok erbij, dus een verschil tussen de klok van de
// server en die van de browser doet er niet toe. Nul = er staat geen vraag
// open; een openstaande vraag heeft altijd minstens een milliseconde.
func (s *meetingSession) presenceGraceLocked(now time.Time) int64 {
	if s.presenceAt.IsZero() {
		return 0
	}
	return max(1, s.presenceAt.Add(s.cfg.PresenceGrace).Sub(now).Milliseconds())
}

// tick is één slag van de bewakingslus. Volgorde: eerst sluiten wat gesloten
// moet worden, dan eventueel "Ben je er nog?", dan de hartslag — zo is de
// hartslag altijd het laatste bericht van een tick en kan een test hard
// vaststellen dat er géén presence_check kwam.
func (s *meetingSession) tick(now time.Time) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	if s.speaking && s.settings.PlaybackTracking && s.playback.expected && !now.Before(s.playback.deadline) {
		gen := s.turnGen
		s.mu.Unlock()
		s.stopSpeakingGeneration("playback_timeout", gen)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return false
		}
	}
	switch {
	case now.Sub(s.startedAt) >= s.cfg.MaxDuration:
		s.mu.Unlock()
		s.close(CloseReasonMaxDuration)
		return false
	case !s.presenceAt.IsZero() && now.Sub(s.presenceAt) >= s.cfg.PresenceGrace:
		s.mu.Unlock()
		s.close(CloseReasonInactivity)
		return false
	case !s.clientGoneSince.IsZero() && now.Sub(s.clientGoneSince) >= s.cfg.ClientGoneGrace:
		// Niemand meer op het kanaal, en ook geen herverbinding: het tabblad
		// is weg. De plaats aan tafel komt vrij in plaats van pas over tien
		// minuten inactiviteit.
		s.mu.Unlock()
		s.close(CloseReasonClientGone)
		return false
	}
	if level, moved := s.arousal.decay(now); moved && s.persona.Chat {
		// De vergadering ziet de duif tot rust komen, ook als de detector
		// niet meedraait: de meter hangt aan de sessie.
		s.dispatchLocked(MeetingEvent{Type: EventArousal, Arousal: &level})
	}
	// De vraag breekt niets af: een bijdrage die al loopt mag uitklinken. Wat
	// wel stopt is nieuw zelfstandig werk (beginTurn, de governor). Een
	// kandidaat die klaarlag gaat weg: na het antwoord is hij minuten oud en
	// hoort hij bij een gesprek dat intussen verder is.
	if s.presenceAt.IsZero() && now.Sub(s.lastActivity) >= s.cfg.InactivityPrompt {
		s.presenceAt = now
		if s.candidate != nil {
			s.candidate = nil
			s.dispatchLocked(MeetingEvent{Type: EventCandidate})
		}
		s.dispatchLocked(MeetingEvent{
			Type: EventPresenceCheck, Listening: s.listen, Lang: s.lang, Message: presenceCheckMessage,
			GraceMS: s.presenceGraceLocked(now),
		})
	}
	// De hartslag draagt de resterende tijd zolang de vraag openstaat. Zo
	// loopt de aftelling in elk tabblad gelijk met de server, en sluit een
	// tabblad zijn vraag zodra er in een ander tabblad geantwoord is.
	s.dispatchLocked(MeetingEvent{Type: EventListening, Listening: s.listen, Speaking: s.speaking, AutonomyPaused: boolPointer(s.autonomyPaused), GraceMS: s.presenceGraceLocked(now)})
	s.mu.Unlock()
	return true
}

// close sluit de sessie netjes: één slotbericht, alle abonnementen dicht, de
// lopende beurt geannuleerd en het geheugen los. Idempotent.
func (s *meetingSession) close(reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.playback = playbackTracker{}
	s.transcript.segments = nil
	s.candidate = nil
	s.contributions = nil
	s.lastPoint = ownPoint{}
	s.partialHash = [32]byte{}
	s.uncommittedSpeech = false
	s.lastSpeechActivity = time.Time{}
	s.diagnostics.clear()
	s.listen = ListenOff
	s.speaking = false
	s.turnGen++
	cancel := s.cancelTurn
	s.cancelTurn = nil
	s.dispatchLocked(MeetingEvent{Type: EventSessionClosed, Reason: reason, Listening: ListenOff})
	// Wat dispatchLocked hierboven al kickte staat niet meer in s.subs: elk
	// kanaal gaat precies één keer dicht.
	for _, ch := range s.subs {
		close(ch)
	}
	s.subs = map[int]chan MeetingEvent{}
	dropped := s.dropped
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.cancelCtx() // lopende kandidaatevaluaties vallen hiermee stil
	if s.candidates != nil {
		s.candidates.close()
	}
	close(s.done)
	s.engine.remove(s.id)
	s.engine.log().Info("vergadersessie gesloten", "component", "duif", "sessie", s.id,
		"reden", reason, "gedropte_events", dropped)
}
