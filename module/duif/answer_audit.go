package duif

// Het bewaarde brondekkingslabel uit de tijd van de live bronaudit.
//
// Vroeger liep er na elk brongedragen antwoord een tweede
// modelcall die elke geleverde zin tegen de oorspronkelijke passages legde, en
// waarvan de uitkomst als label bij het antwoord kwam. Die call is uit de
// live beurt gehaald en het label uit de UI: de bronnen in de kennisbank zijn
// bij de import geverifieerd, terwijl het label per beurt een modelcall kostte
// en vaak "Broncontrole niet uitgevoerd" toonde bij een antwoord waar niets mis
// mee was.
//
// Wat hier staat is alleen nog de vorm van het opgeslagen label. De kolom
// duif.chat_messages.audit blijft bestaan (migratie 0006) en rijen van vóór
// deze wijziging dragen hun label nog; de repo leest ze terug zonder ze te
// kunnen schrijven, en de client toont ze niet meer. Nieuwe rijen krijgen NULL,
// wat al sinds migratie 0006 leest als "geen audit van toepassing" en nooit als
// "deze controle is mislukt". De machinerie die zo'n label maakte draait in
// geen enkele live beurt meer.

// De drie statussen die een bewaard label kan dragen.
const (
	// AuditVerified: elke geleverde zin kwam door de controle.
	AuditVerified = "verified"
	// AuditFlagged: minstens één zin niet; Findings zegt welke.
	AuditFlagged = "flagged"
	// AuditUnchecked: er kwam geen oordeel. Reason zegt waarom.
	AuditUnchecked = "unchecked"
)

// De redenen bij AuditUnchecked. Ze beschrijven de controle, nooit het antwoord.
const (
	AuditReasonTimeout     = "timeout"
	AuditReasonFormat      = "format"
	AuditReasonUnavailable = "unavailable"
	AuditReasonCanceled    = "canceled"
)

// AnswerAudit is het label zoals het op een oude rij bewaard staat.
type AnswerAudit struct {
	Status   string         `json:"status"`             // "verified" | "flagged" | "unchecked"
	Reason   string         `json:"reason,omitempty"`   // bij unchecked: "timeout" | "format" | "unavailable" | "canceled"
	Findings []AuditFinding `json:"findings,omitempty"` // alleen bij flagged
}

// AuditFinding noemt één zin die niet door de controle kwam. Text is de
// geleverde zin zelf en Reason een vaste redencode van de controle; de vrije
// prozabevinding van de auditor is nooit bewaard.
type AuditFinding struct {
	Sentence int    `json:"sentence"` // 1-gebaseerde zinsindex in de uitgeleverde tekst
	Text     string `json:"text"`     // exacte zin
	Reason   string `json:"reason"`   // vaste redencode van de controle
}
