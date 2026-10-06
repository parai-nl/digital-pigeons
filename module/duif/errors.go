package duif

import (
	"errors"
	"fmt"
	"strings"
)

// ErrBudgetExceeded is de sentinel voor een budgetoverschrijding bij de proxy: geen
// stille failure.
var ErrBudgetExceeded = errors.New("duif: llm-budget bereikt")

// errKBUnavailable is de sentinel voor "de kennisbank of de embedder was er
// niet". Bewust géén KBStatus: een storing is geen kennisgat. De duif doet dan
// geen LLM-beurt — antwoorden zonder te kunnen zoeken zou precies de fabricatie
// zijn die de bronnenregels uitsluiten.
var errKBUnavailable = errors.New("duif: kennisbank onbereikbaar")

// Stabiele reason-codes van de wire (core-conventie: vaste string-literal +
// gelokaliseerde message; nooit de upstream-fouttekst op de wire). Alles wat de
// client kan zien staat hier bij elkaar, inclusief de graafroutes: één catalogus
// is één contract. Codes die alleen in het log terechtkomen staan bewust níét
// hier, maar ongeëxporteerd bij de code die ze schrijft.
const (
	ReasonBudgetExceeded = "llm_budget_exceeded"
	ReasonLLMUnavailable = "llm_unavailable"
	// ReasonDetectorFailed: de detectielaag viel om (een paniek in de
	// classifier- of interventieketen). De sessie degradeert zichtbaar naar
	// alleen-knop.
	ReasonDetectorFailed = "detector_failed"
	// ReasonInternal: een spreekbeurt viel onderweg om. De UI krijgt het
	// spreekslot terug in plaats van eeuwig op "bezet" te blijven staan.
	ReasonInternal = "internal_error"
	// ReasonKBUnavailable: de kennisbank of de embedder was onbereikbaar. De
	// beurt gaat niet door — zichtbaar als storing, nooit vermomd als gap.
	ReasonKBUnavailable = "kb_unavailable"
	// ReasonNodeNotFound is het enige antwoord op "die node bestaat niet voor
	// jou": onbekende tabel, een node van een andere kennisbank en een node
	// die er echt niet is, zijn van buiten niet te onderscheiden.
	ReasonNodeNotFound = "node_not_found"
	// ReasonInvalidOffset: de ?offset-parameter van de graaf is geen
	// niet-negatief geheel getal.
	ReasonInvalidOffset = "invalid_offset"
	// ReasonClarifyAlreadyAsked: de facilitator vroeg een tweede wedervraag
	// aan in dezelfde beurt. Hij komt uitsluitend als `error` op een
	// tool_result-frame terug (een chip die niet doorging), nooit als
	// error-frame en nooit als foutbody: het is een cap, geen storing.
	ReasonClarifyAlreadyAsked = "clarify_already_asked"
	// ReasonPigeonLimitReached: de facilitator vroeg de duif vaker dan
	// maxPigeonCallsPerTurn in één beurt. Net als clarify_already_asked een
	// cap en geen storing: hij komt uitsluitend als `error` op een
	// tool_result-frame terug, nooit als error-frame en nooit als foutbody.
	ReasonPigeonLimitReached = "pigeon_limit_reached"
	// ReasonGraphUnavailable: de kennisgraaf is niet (volledig) gewired of
	// antwoordde met een fout — een 503, geen 500, want het is een
	// beschikbaarheidsprobleem en geen bug in het verzoek.
	ReasonGraphUnavailable = "graph_unavailable"
	// ReasonInvalidRequest: het verzoek zelf klopt niet. Welk veld het is,
	// staat in de tweetalige melding en niet in de code: de client toont de
	// zin, hij hoeft per veld niets anders te doen.
	ReasonInvalidRequest = "invalid_request"
	// ReasonTurnTimeout: de beurt liep tegen chatTurnBudget aan, de harde
	// tijdgrens die accessMiddleware om elk verzoek legt. Een eigen code en
	// geen llm_unavailable: de provider was bereikbaar, hij was te traag, en
	// dat vraagt van de lezer iets anders dan "probeer het zo weer".
	ReasonTurnTimeout = "turn_timeout"
)

// reasonRetrievalFailed is een interne logcode: de retrieval-DAG zelf viel om
// (niet de zoekopdracht erin). De client ziet ReasonKBUnavailable; het log
// houdt het verschil vast, zodat een bouwfout niet als providerstoring leest.
const reasonRetrievalFailed = "retrieval_failed"

// apiError is de RFC-7807-achtige foutbody.
type apiError struct {
	Error   string            `json:"error"`
	Message map[string]string `json:"message"`
}

// errorMessages zijn de gelokaliseerde meldingen per reason-code.
var errorMessages = map[string]map[string]string{
	ReasonBudgetExceeded: {
		"en": "The configured model budget has been reached. Answers are paused; contact the facilitator or project team.",
		"nl": "Het ingestelde modelbudget is bereikt. Antwoorden staan stil; neem contact op met de begeleider of het projectteam.",
	},
	ReasonLLMUnavailable: {
		"en": "The pigeons cannot reach their language model right now. Please try again in a moment.",
		"nl": "De duiven kunnen hun taalmodel nu niet bereiken. Probeer het zo weer.",
	},
	ReasonInternal: {
		"en": "The pigeon lost its train of thought. The button still works — please try again.",
		"nl": "De duif verloor de draad. De knop werkt nog — probeer het opnieuw.",
	},
	ReasonKBUnavailable: {
		"en": "The pigeon cannot reach the knowledge base right now and is therefore not answering: without those sources the answer would be guesswork. Please try again in a moment.",
		"nl": "De duif kan de kennisbank nu niet bereiken en antwoordt daarom niet: zonder die bronnen zou het antwoord gokwerk zijn. Probeer het zo weer.",
	},
	ReasonNodeNotFound: {
		"en": "That node is not in this knowledge graph.",
		"nl": "Die knoop staat niet in deze kennisgraaf.",
	},
	ReasonInvalidOffset: {
		"en": "The offset must be a whole number of zero or more.",
		"nl": "De offset moet een geheel getal van nul of hoger zijn.",
	},
	ReasonGraphUnavailable: {
		"en": "The knowledge graph cannot be read right now. Please try again in a moment.",
		"nl": "De kennisgraaf is nu niet te lezen. Probeer het zo weer.",
	},
	ReasonInvalidRequest: {
		"en": "That request could not be processed. Reload the page and try again.",
		"nl": "Dat verzoek kon niet verwerkt worden. Herlaad de pagina en probeer het opnieuw.",
	},
	ReasonTurnTimeout: {
		"en": "This answer took too long and was stopped. The button still works — try again, ideally with a shorter or more specific question.",
		"nl": "Dit antwoord duurde te lang en is gestopt. De knop werkt nog — probeer het opnieuw, het liefst met een kortere of concretere vraag.",
	},
}

// badRequestReason is de reden waarom een verzoek niet klopt. Hij kiest de
// tweetalige zin hieronder; de wire-code blijft ReasonInvalidRequest. Het eigen
// type is de hele bedoeling: een losse Engelse ontwikkelaarszin past er niet
// meer in, en die zin ging vroeger ongewijzigd naar het Nederlandse
// message-slot dat de gebruiker leest.
type badRequestReason string

const (
	reasonMalformedBody           badRequestReason = "malformed_body"
	reasonUnknownChatPigeon       badRequestReason = "unknown_chat_pigeon"
	reasonUnsupportedLang         badRequestReason = "unsupported_lang"
	reasonMessageLength           badRequestReason = "message_length"
	reasonPigeonSessionMismatch   badRequestReason = "pigeon_session_mismatch"
	reasonPlaybackState           badRequestReason = "playback_state"
	reasonResumeTurnID            badRequestReason = "resume_turn_id"
	reasonUnknownMeetingPigeon    badRequestReason = "unknown_meeting_pigeon"
	reasonMeetingTextLength       badRequestReason = "meeting_text_length"
	reasonMeetingSpeakerLength    badRequestReason = "meeting_speaker_length"
	reasonPigeonDoesNotSpeak      badRequestReason = "pigeon_does_not_speak"
	reasonPigeonSpeaks            badRequestReason = "pigeon_speaks"
	reasonEagerness               badRequestReason = "eagerness"
	reasonCooldown                badRequestReason = "cooldown"
	reasonComparisonUnreadable    badRequestReason = "comparison_unreadable"
	reasonComparisonTargetNoChat  badRequestReason = "comparison_target_no_chat"
	reasonComparisonSamePigeon    badRequestReason = "comparison_same_pigeon"
	reasonComparisonQuestionGone  badRequestReason = "comparison_question_gone"
	reasonComparisonContextTooBig badRequestReason = "comparison_context_too_big"
)

// allBadRequestReasons is de volledige lijst 400-redenen. Zet hier elke nieuwe
// constante hierboven bij: TestBadRequestMessages_EveryReasonIsInTheCatalogue
// toetst hem tegen badRequestMessages in beide richtingen. Zonder die lijst valt
// een reden zonder catalogusregel stil terug op de algemene zin, en dan leest de
// gebruiker "dat verzoek klopte niet" waar hij had moeten lezen wat hij kan doen.
var allBadRequestReasons = []badRequestReason{
	reasonMalformedBody,
	reasonUnknownChatPigeon,
	reasonUnsupportedLang,
	reasonMessageLength,
	reasonPigeonSessionMismatch,
	reasonPlaybackState,
	reasonResumeTurnID,
	reasonUnknownMeetingPigeon,
	reasonMeetingTextLength,
	reasonMeetingSpeakerLength,
	reasonPigeonDoesNotSpeak,
	reasonPigeonSpeaks,
	reasonEagerness,
	reasonCooldown,
	reasonComparisonUnreadable,
	reasonComparisonTargetNoChat,
	reasonComparisonSamePigeon,
	reasonComparisonQuestionGone,
	reasonComparisonContextTooBig,
}

// badRequestMessages is de tweetalige catalogus bij die redenen. Elke zin zegt
// wat de lezer kan doen; geen veldnaam, geen limiet die alleen in de code
// bestaat en nergens Engels in de Nederlandse kolom.
var badRequestMessages = map[badRequestReason]map[string]string{
	reasonMalformedBody: {
		"en": "That request could not be read. Reload the page and try again.",
		"nl": "Dat verzoek was niet te lezen. Herlaad de pagina en probeer het opnieuw.",
	},
	reasonUnknownChatPigeon: {
		"en": "That pigeon does not hold conversations. Choose a pigeon that chats.",
		"nl": "Die duif voert geen gesprekken. Kies een duif die chat.",
	},
	reasonUnsupportedLang: {
		"en": "This conversation runs in English or Dutch only.",
		"nl": "Dit gesprek gaat alleen in het Engels of het Nederlands.",
	},
	reasonMessageLength: {
		"en": "Your message is empty or too long. Keep it under four thousand characters.",
		"nl": "Je bericht is leeg of te lang. Houd het onder de vierduizend tekens.",
	},
	reasonPigeonSessionMismatch: {
		"en": "This conversation belongs to another pigeon. Open the conversation of the pigeon you want to ask.",
		"nl": "Dit gesprek hoort bij een andere duif. Open het gesprek van de duif die je wilt vragen.",
	},
	reasonPlaybackState: {
		"en": "The sound status of this contribution could not be processed. The meeting carries on.",
		"nl": "De geluidsstatus van deze bijdrage kon niet verwerkt worden. De vergadering loopt door.",
	},
	reasonResumeTurnID: {
		"en": "Resuming needs the meeting as you see it now. Reload the meeting and try again.",
		"nl": "Hervatten vraagt de vergadering zoals je die nu voor je hebt. Herlaad de vergadering en probeer het opnieuw.",
	},
	reasonUnknownMeetingPigeon: {
		"en": "That pigeon does not take a seat at the table. Choose a pigeon that joins meetings.",
		"nl": "Die duif neemt geen plaats aan tafel. Kies een duif die meevergadert.",
	},
	reasonMeetingTextLength: {
		"en": "That line is empty or too long. Keep it under four thousand characters.",
		"nl": "Die regel is leeg of te lang. Houd het onder de vierduizend tekens.",
	},
	// Het getal komt uit het plafond zelf: staat het er twee keer, dan lopen
	// de twee vroeg of laat uiteen en leest de gebruiker een grens die niet
	// bestaat.
	reasonMeetingSpeakerLength: {
		"en": fmt.Sprintf("That speaker name is too long. Use at most %d characters.", maxSpeakerRunes),
		"nl": fmt.Sprintf("Die sprekernaam is te lang. Gebruik hoogstens %d tekens.", maxSpeakerRunes),
	},
	reasonPigeonDoesNotSpeak: {
		"en": "This pigeon does not speak in meetings; she answers with movement and sound.",
		"nl": "Deze duif spreekt niet in een vergadering; zij antwoordt met beweging en geluid.",
	},
	reasonPigeonSpeaks: {
		"en": "This pigeon speaks. Use the button that invites her to respond.",
		"nl": "Deze duif spreekt. Gebruik de knop waarmee je haar om een reactie vraagt.",
	},
	reasonEagerness: {
		"en": "Choose how readily the pigeon joins in: reserved, normal or eager.",
		"nl": "Kies hoe graag de duif meedoet: terughoudend, normaal of gretig.",
	},
	reasonCooldown: {
		"en": "The pause between contributions runs from zero to one hour.",
		"nl": "De pauze tussen bijdragen loopt van nul tot een uur.",
	},
	reasonComparisonUnreadable: {
		"en": "That comparison could not be read. Ask the other pigeon again.",
		"nl": "Die vergelijking was niet te lezen. Vraag het de andere duif opnieuw.",
	},
	reasonComparisonTargetNoChat: {
		"en": "Choose a pigeon that holds conversations.",
		"nl": "Kies een duif die gesprekken voert.",
	},
	reasonComparisonSamePigeon: {
		"en": "Choose the other pigeon for this comparison.",
		"nl": "Kies de andere duif voor deze vergelijking.",
	},
	reasonComparisonQuestionGone: {
		"en": "The question of this comparison is no longer available.",
		"nl": "De vraag van deze vergelijking is niet meer beschikbaar.",
	},
	reasonComparisonContextTooBig: {
		"en": "This conversation is too large to take along to the other pigeon.",
		"nl": "Dit gesprek is te groot om mee te nemen naar de andere duif.",
	},
}

// classifyLLMError mapt een LLM-fout op een stabiele reason-code. De
// taalmodelproxy meldt een budgetoverschrijding als fout waarvan de tekst
// "budget" bevat; die vertalen we naar ReasonBudgetExceeded. Alle andere
// fouten blijven generiek. De upstream-tekst wordt gelogd, nooit doorgegeven
// aan de client.
func classifyLLMError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrBudgetExceeded) || strings.Contains(strings.ToLower(err.Error()), "budget") {
		return ReasonBudgetExceeded
	}
	return ReasonLLMUnavailable
}

// errorBody bouwt de foutbody voor een reason-code; onbekende codes vallen
// terug op llm_unavailable zodat er nooit een lege message op de wire staat.
func errorBody(reason string) apiError {
	msg, ok := errorMessages[reason]
	if !ok {
		msg = errorMessages[ReasonLLMUnavailable]
	}
	return apiError{Error: reason, Message: msg}
}
