package duif

import (
	"net/http"
)

// knowledgeStatus leest de stand van de import. De reden-code vertelt óók
// waarom er niets te importeren viel: zonder kennisgraaf of zonder embedder is
// de kennisbank leeg omdat deze installatie hem niet kan vullen, en dat is iets
// anders dan een bank die nog geladen moet worden. De graaf geeft in datzelfde
// geval een 503; wie beide leest, ziet nu waarom.
func (m *Module) knowledgeStatus() importStatus {
	var st importStatus
	if m.kbImport != nil {
		st = m.kbImport.snapshot()
	}
	if st.State == "" {
		st.State = ImportEmpty
	}
	// Alleen een lege bank zónder eigen reden krijgt er een: bij ready, failed
	// of importing vertelt de stand zelf al wat er aan de hand is.
	if st.State == ImportEmpty && st.Reason == "" {
		switch {
		case m.knowledge == nil:
			st.Reason = ReasonImportNoKnowledge
		case m.embedder == nil:
			st.Reason = ReasonImportNoEmbedder
		}
	}
	return st
}

// handleKnowledgeStatus — GET /v1/knowledge/status. Strikt lezen: FORCE en
// het bundelpad bestaan alleen als omgevingsvariabele, nooit via HTTP.
func (m *Module) handleKnowledgeStatus(w http.ResponseWriter, r *http.Request) {
	if requireUser(w, r) == nil {
		return
	}
	// De stand verandert tijdens een import; een gecachet antwoord zou de UI
	// op "importing" laten staan.
	noStore(w)
	writeJSON(w, http.StatusOK, m.knowledgeStatus())
}
