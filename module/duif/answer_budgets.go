package duif

// De tokenplafonds van één beurt staan in answer_stream.go, en ze zijn
// opzettelijk niet exporteerbaar: buiten deze package kiest niemand ze. De
// wire-contracttest in package e2e moet ze wél kunnen lezen. Hij leest de echte
// call van de stub-LLM terug en controleert of het plafond klopt; deed hij dat
// tegen een overgeschreven getal, dan bewees hij alleen dat iemand twee
// plaatsen tegelijk had bijgewerkt — en bleef hij staan op het oude getal toen
// een plafond de redeneerruimte erbij kreeg. Vandaar deze namen: één
// regel per plafond, zonder eigen rekenwerk, zodat de test niet kán afwijken
// van wat de server verstuurt.
const (
	// AnswerChatMaxTokens is het plafond van één chatantwoord.
	AnswerChatMaxTokens = answerChatMaxTokens
	// AnswerMeetingMaxTokens is het plafond van één gesproken beurt.
	AnswerMeetingMaxTokens = answerMeetingMaxTokens
)
