//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// De kennisbank-e2e. Hij draait op een eigen database, zodat de
// geïmporteerde graaf de scenario's van boot_test.go niet kan beïnvloeden —
// en andersom. Eén import bij het opstarten, daarna alle vragen erover: de
// chat, de kennisgraaf en de vergadering.

// importStatusBody is de wire-vorm van GET /v1/knowledge/status.
type importStatusBody struct {
	State  string         `json:"state"`
	KBID   string         `json:"kbId"`
	Counts map[string]int `json:"counts"`
	// Het bundelformaat van de import: het schema altijd, de talen
	// en de brontaal alleen als de bundel ze draagt.
	BundleSchema   string   `json:"bundleSchema"`
	Languages      []string `json:"languages"`
	SourceLanguage string   `json:"sourceLanguage"`
	ImportedAt     string   `json:"importedAt"`
	Skipped        bool     `json:"skipped"`
	Reason         string   `json:"reason"`
}

// sourceBody is de bron-DTO zoals de chat en de vergadering hem sturen. De
// brontaalvelden (`text`, `perspective`, `theme`) blijven staan zoals ze altijd
// stonden; `localized` komt er in duiven/3 naast, met `sourceLanguage` erbij.
type sourceBody struct {
	Number      int    `json:"number"`
	Type        string `json:"type"`
	ID          string `json:"id"`
	Text        string `json:"text"`
	Perspective string `json:"perspective"`
	Theme       string `json:"theme"`
	Ref         struct {
		Quote string `json:"quote"`
		Paper string `json:"paper"`
	} `json:"ref"`
	SourceLanguage string           `json:"sourceLanguage"`
	Localized      *sourceLocalized `json:"localized"`
}

type sourceLocalized struct {
	Text        map[string]string `json:"text"`
	Perspective map[string]string `json:"perspective"`
	Theme       map[string]string `json:"theme"`
}

// graphNodeBody is één knoop in een graaf-antwoord. Het label en de vertaalde
// metadatavelden komen óf als kale string (onvertaalde tekst: een citaat, een
// papertitel) óf als taalobject {nl, en}; wireText leest beide vormen.
type graphNodeBody struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Label          any            `json:"label"`
	Metadata       map[string]any `json:"metadata"`
	SourceLanguage string         `json:"sourceLanguage"`
}

// wireText leest één wire-veld in de gevraagde taal, met terugval op de andere
// taal — precies zoals de client het doet.
func wireText(value any, lang string) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if text, ok := v[lang].(string); ok && text != "" {
			return text
		}
		for _, other := range []string{"nl", "en"} {
			if text, ok := v[other].(string); ok && text != "" {
				return text
			}
		}
	}
	return ""
}

type graphRelBody struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Type     string `json:"relationship_type"`
}

type graphOverviewBody struct {
	Nodes         []graphNodeBody `json:"nodes"`
	Relationships []graphRelBody  `json:"relationships"`
	Totals        map[string]int  `json:"totals"`
	KB            struct {
		State string `json:"state"`
	} `json:"kb"`
}

type graphChildrenBody struct {
	Nodes         []graphNodeBody `json:"nodes"`
	Relationships []graphRelBody  `json:"relationships"`
	Total         int             `json:"total"`
	Truncated     bool            `json:"truncated"`
}

type graphNodeResponseBody struct {
	Node  graphNodeBody   `json:"node"`
	Trail []graphNodeBody `json:"trail"`
}

// getJSON haalt een route op, eist 200 en ontleedt de body. De rauwe body komt
// mee terug: verschillende controles kijken naar de bytes zelf (geen
// `storage_key` op de wire).
func (s *duifServer) getJSON(t *testing.T, path string, out any) []byte {
	t.Helper()
	status, body := s.get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200 (body %s)", path, status, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			t.Fatalf("GET %s: body %s ontleden: %v", path, body, err)
		}
	}
	return body
}

// importStatus leest de importstand één keer.
func (s *duifServer) importStatus(t *testing.T) importStatusBody {
	t.Helper()
	var out importStatusBody
	s.getJSON(t, "/api/duif/v1/knowledge/status", &out)
	return out
}

// waitImport polt de statusroute tot `klaar` waar is. Een mislukte import
// stopt meteen: doorpollen tot de deadline zou de reden-code verstoppen achter
// een timeout.
//
// Het predicaat is er om een echte race te ondervangen. Bij een tweede boot
// leest de module eerst de bestaande marker (state `ready`, skipped false) en
// pas daarna draait de overslaande import. Wachten op alleen `ready` zou dus
// de stand van vóór de import kunnen vangen.
func (s *duifServer) waitImport(t *testing.T, timeout time.Duration, klaar func(importStatusBody) bool) importStatusBody {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last importStatusBody
	for time.Now().Before(deadline) {
		last = s.importStatus(t)
		if klaar(last) {
			return last
		}
		if last.State == "failed" {
			t.Fatalf("import mislukt: reason = %q; serverlog:\n%s", last.Reason, s.logs.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("de import bereikte de verwachte stand niet binnen %s (laatste %+v); serverlog:\n%s",
		timeout, last, s.logs.String())
	return last
}

// waitImportReady wacht op een afgeronde import.
func (s *duifServer) waitImportReady(t *testing.T, timeout time.Duration) importStatusBody {
	t.Helper()
	return s.waitImport(t, timeout, func(st importStatusBody) bool { return st.State == "ready" })
}

// graphNodeByType zoekt de eerste knoop van een type in een lijst.
func graphNodeByType(t *testing.T, nodes []graphNodeBody, typ string) graphNodeBody {
	t.Helper()
	for _, n := range nodes {
		if n.Type == typ {
			return n
		}
	}
	t.Fatalf("geen knoop van type %q in %+v", typ, nodes)
	return graphNodeBody{}
}

// TestKnowledgeBootE2E boot de server mét een kennisbankbundel en toetst de
// hele keten: import → status → chat met bronnen → kennisgat → storing →
// kennisgraaf → vergadering → tweede boot.
func TestKnowledgeBootE2E(t *testing.T) {
	bundle := writeKBFixture(t)
	dbURL := env(t, "DUIF_E2E_KB_DB_URL")
	bootEnv := []string{
		"DUIF_IMPORT_KB=" + bundle,
		// Klein genoeg om de parallelle schrijfweg echt te lopen, klein genoeg
		// om de stub-embedder niet te overvragen.
		"DUIF_IMPORT_WORKERS=3",
	}

	srv := bootServerWith(t, serverConfig{dbURL: dbURL, env: bootEnv})

	// Node-id's die latere subtests nodig hebben, gevonden via de graaf.
	var subthemaID, destillaatFeedingID, quoteMarketID string
	var chatSourceID string
	// De chat met bronnen is ook het brongesprek van de vergelijking.
	var comparisonSourceSession string

	t.Run("import bij boot vult de graaf", func(t *testing.T) {
		st := srv.waitImportReady(t, 30*time.Second)
		if st.KBID != kbFixtureID {
			t.Errorf("kbId = %q, want %q", st.KBID, kbFixtureID)
		}
		if st.Skipped {
			t.Error("de eerste import op een lege database hoort niet overgeslagen te zijn")
		}
		if st.ImportedAt == "" {
			t.Error("een geslaagde import hoort een importedAt te dragen")
		}
		if st.Reason != "" {
			t.Errorf("reason = %q, want leeg bij een geslaagde import", st.Reason)
		}
		for table, want := range kbFixtureCounts {
			if st.Counts[table] != want {
				t.Errorf("counts[%s] = %d, want %d (volledige telling %+v)", table, st.Counts[table], want, st.Counts)
			}
		}
		// De relatietellingen staan in dezelfde map; één ervan volstaat als
		// bewijs dat de import ze meeschrijft.
		if st.Counts["has_quote"] != 3 {
			t.Errorf("counts[has_quote] = %d, want 3", st.Counts["has_quote"])
		}
		// Het bundelformaat hoort op de wire. Deze bundel is
		// mosaic/1 en draagt geen talen, dus die twee velden blijven leeg —
		// dat is iets anders dan een vertaling die zou ontbreken.
		if st.BundleSchema != "mosaic/1" {
			t.Errorf("bundleSchema = %q, want mosaic/1", st.BundleSchema)
		}
		if len(st.Languages) != 0 || st.SourceLanguage != "" {
			t.Errorf("een eentalige bundel hoort geen talen te melden: %v / %q", st.Languages, st.SourceLanguage)
		}
	})

	t.Run("chat vindt het standpunt en citeert de bron", func(t *testing.T) {
		beforeRequests, beforeEmbeds := len(srv.llm.requests()), srv.llm.embedCallCount()
		status, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "biological",
			"message":  kbSummaryNesting,
			"lang":     "nl",
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, raw)
		}
		// Beide sprekende duiven gebruiken de agent. Bronnen horen bij het
		// artifact en moeten vóór de eerste duiftekst beschikbaar zijn. Het
		// aantal SSE-chunks is vrij; de volgorde en het auteurschap zijn dat niet.
		var phases []string
		for _, typ := range frameTypes(frames) {
			if len(phases) == 0 || phases[len(phases)-1] != typ {
				phases = append(phases, typ)
			}
		}
		// De done sluit de rij: er volgt geen bronlabel meer.
		wantPhases := []string{"tool", "artifact", "sources", "artifact_delta", "artifact", "tool_result", "done"}
		if !reflect.DeepEqual(phases, wantPhases) {
			t.Fatalf("SSE-fasen = %v, want %v", phases, wantPhases)
		}
		open := frames[1]
		artifactID, _ := open["id"].(string)
		if artifactID == "" || open["kind"] != "pigeon_answer" || open["persona"] != "biological" || open["kbStatus"] != "sources" || open["done"] == true {
			t.Fatalf("artifact-open mist duif-auteurschap of bronstatus: %+v", open)
		}
		for _, frame := range frames {
			switch frame["type"] {
			case "sources", "artifact", "artifact_delta":
				if frame["id"] != artifactID {
					t.Errorf("bron of duiftekst hoort bij artifact %q: %+v", artifactID, frame)
				}
			}
		}
		closed := lastFrame(t, frames, "artifact")
		if closed["done"] != true || closed["error"] != nil || closed["persona"] != "biological" {
			t.Errorf("artifact niet succesvol afgesloten door dezelfde duif: %+v", closed)
		}
		result := lastFrame(t, frames, "tool_result")
		if frames[0]["name"] != "ask_pigeon" || result["name"] != "ask_pigeon" || result["id"] != frames[0]["id"] || result["error"] != nil {
			t.Errorf("ask_pigeon-tool niet succesvol afgesloten: %+v → %+v", frames[0], result)
		}
		var sourcesFrame struct {
			Sources []sourceBody `json:"sources"`
		}
		reMarshal(t, frames[2], &sourcesFrame)
		if len(sourcesFrame.Sources) != 1 {
			t.Fatalf("sources-frame = %+v, want alleen de passende fixturebron", frames[2])
		}
		bron := sourcesFrame.Sources[0]
		if bron.Number != 1 {
			t.Errorf("nummer = %d, want 1", bron.Number)
		}
		if bron.Type != "destillaat" {
			t.Errorf("type = %q, want destillaat", bron.Type)
		}
		chatSourceID = bron.ID
		if !strings.HasPrefix(bron.ID, "destillaat:") {
			t.Errorf("id = %q, want een destillaat-node-id", bron.ID)
		}
		if bron.Text != kbSummaryNesting {
			t.Errorf("tekst = %q, want de samenvatting van het standpunt", bron.Text)
		}
		if bron.Ref.Quote != kbQuoteNesting {
			t.Errorf("ref.quote = %q, want %q", bron.Ref.Quote, kbQuoteNesting)
		}
		if bron.Ref.Paper != kbPaperTitle {
			t.Errorf("ref.paper = %q, want %q", bron.Ref.Paper, kbPaperTitle)
		}
		if bron.Theme != kbThemaTitle {
			t.Errorf("theme = %q, want %q (gedenormaliseerd bij de import)", bron.Theme, kbThemaTitle)
		}

		done := lastFrame(t, frames, "done")
		if done["kbStatus"] != "sources" {
			t.Fatalf("kbStatus = %v, want sources", done["kbStatus"])
		}
		if done["stopReason"] != "natural" || done["sources"] != nil {
			t.Errorf("done hoort succesvol af te sluiten zonder artifactbronnen te verplaatsen naar de facilitator: %+v", done)
		}
		if answer := artifactText(frames); !strings.Contains(answer, strings.TrimSuffix(fixtureNestingAnswer, ".")+" [1]") {
			t.Errorf("de duif citeert de opgehaalde fixturezin niet: %q", answer)
		}
		if facilitator := streamedText(frames); facilitator != "" {
			t.Errorf("facilitatortekst ontbreekt of heeft ten onrechte een citaat: %q", facilitator)
		}

		// Een herladen chat moet dezelfde bron-ID's, nummers en metadata bij
		// dezelfde auteur houden; een turnbrede lijst zou verkeerd citeren.
		sessionID, _ := done["sessionId"].(string)
		if sessionID == "" {
			t.Fatal("done mist sessionId")
		}
		comparisonSourceSession = sessionID
		var history struct {
			Messages []struct {
				Role       string            `json:"role"`
				Kind       string            `json:"kind"`
				Persona    string            `json:"persona"`
				Content    string            `json:"content"`
				KBStatus   string            `json:"kbStatus"`
				Sources    []sourceBody      `json:"sources"`
				Provenance map[string]string `json:"provenance"`
			} `json:"messages"`
		}
		srv.getJSON(t, "/api/duif/v1/chat/sessions/"+sessionID+"/messages", &history)
		if len(history.Messages) != 2 {
			t.Fatalf("opgeslagen berichten = %+v, want vraag en duif; biologische facilitator blijft intern", history.Messages)
		}
		human, pigeon := history.Messages[0], history.Messages[1]
		if human.Role != "user" || human.Content != kbSummaryNesting {
			t.Errorf("oorspronkelijke mensvraag gewijzigd: %+v", human)
		}
		if pigeon.Kind != "pigeon_answer" || pigeon.Persona != "biological" || pigeon.Content != artifactText(frames) || pigeon.KBStatus != "sources" || !reflect.DeepEqual(pigeon.Sources, sourcesFrame.Sources) {
			t.Errorf("opgeslagen duif verliest auteurschap, antwoord of bronnen: %+v", pigeon)
		}
		var metadata struct {
			Provenance map[string]string `json:"provenance"`
		}
		reMarshal(t, done, &metadata)
		assertProvenance(t, metadata.Provenance, true)
		if !reflect.DeepEqual(pigeon.Provenance, metadata.Provenance) || human.Provenance != nil {
			t.Fatal("stored source answer lost corpus/prompt provenance or stamped the human")
		}
		if srv.llm.embedCallCount() <= beforeEmbeds {
			t.Error("bronvraag bereikte de embedder niet")
		}
		reqs := srv.llm.requests()[beforeRequests:]
		assertAnswerRequests(t, reqs, "biological", "nl", false, true, []string{kbSummaryNesting}, artifactText(frames))
	})

	// Vergelijken op de wire: het brongesprek heeft een duifantwoord met
	// bronnen, en die reizen mee naar het gesprek van de andere duif zonder
	// van auteur te wisselen.
	t.Run("vergelijken kopieert het duifantwoord met auteur en bronnen", func(t *testing.T) {
		if comparisonSourceSession == "" {
			t.Skip("geen brongesprek: de chat-subtest liep niet")
		}
		type storedMessage struct {
			ID       string       `json:"id"`
			Role     string       `json:"role"`
			Kind     string       `json:"kind"`
			Persona  string       `json:"persona"`
			Content  string       `json:"content"`
			KBStatus string       `json:"kbStatus"`
			Sources  []sourceBody `json:"sources"`
			// Audit is de kolom van de vroegere bronaudit. Nieuwe antwoorden dragen
			// hem niet meer; de e2e leest hem nog om te bewijzen dat hij ook
			// niet stiekem terugkomt.
			Audit *struct {
				Status string `json:"status"`
			} `json:"audit"`
		}
		var source struct {
			Messages []storedMessage `json:"messages"`
		}
		srv.getJSON(t, "/api/duif/v1/chat/sessions/"+comparisonSourceSession+"/messages", &source)
		if len(source.Messages) != 2 || source.Messages[1].Audit != nil {
			t.Fatalf("brongesprek zonder duifantwoord, of met een label dat er niet meer hoort: %+v", source.Messages)
		}
		question, answer := source.Messages[0], source.Messages[1]

		requestID := fmt.Sprintf("e2e-comparison-%d", time.Now().UnixNano())
		status, body := srv.postJSON(t, http.MethodPost, "/api/duif/v1/chat/comparisons", map[string]string{
			"requestId": requestID, "sourceSessionId": comparisonSourceSession,
			"questionId": question.ID, "pigeonId": "individualist",
		})
		if status != http.StatusCreated {
			t.Fatalf("vergelijking aanmaken: status = %d (body %s)", status, body)
		}
		var created struct {
			Session struct {
				ID         string `json:"id"`
				PigeonID   string `json:"pigeonId"`
				Comparison struct {
					SessionID         string   `json:"sessionId"`
					PigeonID          string   `json:"pigeonId"`
					OriginalQuestion  string   `json:"originalQuestion"`
					ContextMessageIDs []string `json:"contextMessageIds"`
				} `json:"comparison"`
			} `json:"session"`
		}
		if err := json.Unmarshal([]byte(body), &created); err != nil {
			t.Fatalf("vergelijkbody %s: %v", body, err)
		}
		target := created.Session
		// De herkomst noemt het brongesprek, de eerste duif en precies wat er
		// meereisde: de vraag en de antwoordbeurt erop.
		if target.PigeonID != "individualist" || target.Comparison.SessionID != comparisonSourceSession ||
			target.Comparison.PigeonID != "biological" || target.Comparison.OriginalQuestion != question.Content {
			t.Fatalf("herkomst onvolledig: %+v", target)
		}
		if !reflect.DeepEqual(target.Comparison.ContextMessageIDs, []string{question.ID, answer.ID}) {
			t.Fatalf("kopie noemt niet de beantwoorde beurt: %+v", target.Comparison.ContextMessageIDs)
		}

		var copied struct {
			Messages []storedMessage `json:"messages"`
		}
		srv.getJSON(t, "/api/duif/v1/chat/sessions/"+target.ID+"/messages", &copied)
		if len(copied.Messages) != 2 {
			t.Fatalf("gekopieerd gesprek = %+v, want de vraag en het antwoord", copied.Messages)
		}
		got := copied.Messages[1]
		if got.ID == answer.ID {
			t.Fatalf("de kopie hergebruikte de bron-id: %+v", got)
		}
		if got.Persona != "biological" || got.Kind != answer.Kind || got.Content != answer.Content ||
			got.KBStatus != answer.KBStatus || !reflect.DeepEqual(got.Sources, answer.Sources) {
			t.Fatalf("gekopieerd antwoord verliest auteur, tekst of bronnen: %+v want %+v", got, answer)
		}
		if got.Audit != nil {
			t.Fatalf("de kopie kreeg een dekkingslabel dat niet meer bestaat: %+v", got.Audit)
		}
		if copied.Messages[0].Role != "user" || copied.Messages[0].Content != question.Content || copied.Messages[0].Persona != "" {
			t.Fatalf("de mensvraag reisde niet schoon mee: %+v", copied.Messages[0])
		}
		// Het brongesprek blijft ongewijzigd naast zijn kopie.
		var after struct {
			Messages []storedMessage `json:"messages"`
		}
		srv.getJSON(t, "/api/duif/v1/chat/sessions/"+comparisonSourceSession+"/messages", &after)
		if !reflect.DeepEqual(after.Messages, source.Messages) {
			t.Fatal("het brongesprek veranderde door het kopiëren")
		}
	})

	t.Run("systemische vraag is een kennisgat zonder citaten", func(t *testing.T) {
		beforeRequests := len(srv.llm.requests())
		status, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "biological",
			"message":  "Wat is de zin van het bestaan van de gemeente? " + citationTrigger,
			"lang":     "nl",
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, raw)
		}
		types := frameTypes(frames)
		for _, tp := range types {
			if tp == "sources" {
				t.Fatalf("een kennisgat mag geen sources-frame hebben: %v", types)
			}
		}
		done := lastFrame(t, frames, "done")
		if done["kbStatus"] != "gap" {
			t.Errorf("kbStatus = %v, want gap", done["kbStatus"])
		}
		if done["sources"] != nil {
			t.Errorf("done draagt bronnen in gap-modus: %+v", done["sources"])
		}

		// De menselijke citatiemarker mag geen bron creëren. De bronfixture
		// levert een bronloze grenszin en de echte structuurcontrole blijft actief.
		if streamedText(frames) != "" {
			t.Fatal("bio facilitator must stay internal")
		}
		for author, tekst := range map[string]string{"duif": artifactText(frames)} {
			if !strings.Contains(tekst, fixtureGapNL) {
				t.Errorf("%s-tekst = %q, want het stub-antwoord", author, tekst)
			}
			if strings.ContainsAny(tekst, "[]") {
				t.Errorf("%s-tekst draagt nog een citatiemarkering: %q", author, tekst)
			}
		}
		if answer := lastFrame(t, frames, "artifact"); answer["kbStatus"] != "gap" || answer["done"] != true {
			t.Errorf("duifartifact moet als kennisgat afsluiten: %+v", answer)
		}

		if done["stopReason"] != "natural" {
			t.Fatal("knowledge gap ended as an operational failure")
		}
		assertAnswerRequests(t, srv.llm.requests()[beforeRequests:], "biological", "nl", false, false, []string{"Wat is de zin van het bestaan van de gemeente? " + citationTrigger}, artifactText(frames))

	})

	t.Run("embedder weg eindigt met kb_unavailable, geen kennisgat", func(t *testing.T) {
		srv.llm.failEmbeddings(true)
		defer srv.llm.failEmbeddings(false)
		beforeRequests := len(srv.llm.requests())

		status, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "biological",
			"message":  kbSummaryNesting,
			"lang":     "nl",
		})
		// ask_pigeon start nadat de agent de SSE-stream al heeft geopend.
		// HTTP 200 is daarom geen bewijs van succes: error + done zijn leidend.
		if status != http.StatusOK {
			t.Fatalf("status = %d, want geopende SSE-stream (body %s)", status, raw)
		}
		types := frameTypes(frames)
		if len(types) < 4 || types[0] != "tool" || types[1] != "tool_result" || types[len(types)-2] != "error" || types[len(types)-1] != "done" {
			t.Fatalf("storing mist toolfout en terminale afsluiting: %v", types)
		}
		for _, frame := range frames[2 : len(frames)-2] {
			if frame["type"] != "text" {
				t.Errorf("KB-storing mag geen duifantwoord of bronnen produceren: %+v", frame)
			}
		}
		result, failure, done := frames[1], frames[len(frames)-2], frames[len(frames)-1]
		if result["error"] != "kb_unavailable" || result["name"] != "ask_pigeon" || result["id"] != frames[0]["id"] || failure["error"] != "kb_unavailable" {
			t.Errorf("tool- en turnfout moeten dezelfde kb_unavailable dragen: %+v / %+v", result, failure)
		}
		message, _ := failure["message"].(map[string]any)
		for _, lang := range []string{"nl", "en"} {
			if text, _ := message[lang].(string); text == "" {
				t.Errorf("KB-storing mist melding in %s: %+v", lang, message)
			}
		}
		if done["kbStatus"] != "unavailable" || done["stopReason"] != "error" || done["sources"] != nil {
			t.Errorf("KB-storing mag niet eindigen als gap/persona_only of natural: %+v", done)
		}
		if strings.ContainsAny(streamedText(frames), "[]") {
			t.Errorf("facilitator draagt bronloze citatie na storing: %q", streamedText(frames))
		}
		reqs := srv.llm.requests()[beforeRequests:]
		if len(reqs) != 1 {
			t.Errorf("LLM-aanroepen = %d, want alleen router; geen betaalde afronding na KB-fout", len(reqs))
		}
		for _, req := range reqs {
			if len(req.Tools) == 0 {
				t.Error("de duif mag niet worden aangeroepen wanneer de KB onbereikbaar is")
			}
		}
	})

	t.Run("kennisgraaf: overzicht, kinderen, knoop en spoor", func(t *testing.T) {
		var overview graphOverviewBody
		raw := srv.getJSON(t, "/api/duif/v1/graph/overview", &overview)
		assertNoStorageKey(t, "overview", raw)

		if overview.KB.State != "ready" {
			t.Fatalf("kb.state = %q, want ready", overview.KB.State)
		}
		// Perspectieven en persona-knopen staan in de kennisbank (de statusroute
		// telt ze hierboven), maar de graaf toont ze niet — ook hun totalen
		// niet.
		for table, want := range kbFixtureCounts {
			if table == "perspective" || table == "persona" {
				if _, ok := overview.Totals[table]; ok {
					t.Errorf("totals draagt %s: %+v", table, overview.Totals)
				}
				continue
			}
			if overview.Totals[table] != want {
				t.Errorf("totals[%s] = %d, want %d", table, overview.Totals[table], want)
			}
		}

		// Het structuurniveau: domein, thema en subthema, plus de twee
		// relaties daartussen. Standpunten en citaten komen per klik.
		if len(overview.Nodes) != 3 {
			t.Errorf("aantal knopen in het overzicht = %d, want 3: %+v", len(overview.Nodes), overview.Nodes)
		}
		for _, n := range overview.Nodes {
			switch n.Type {
			case "destillaat", "quote", "perspective", "persona":
				t.Errorf("het overzicht hoort geen %s te tonen: %+v", n.Type, n)
			}
		}
		if subthema := graphNodeByType(t, overview.Nodes, "subthema"); wireText(subthema.Label, "nl") != kbSubthemaTitle {
			t.Errorf("label van het subthema = %v, want %q", subthema.Label, kbSubthemaTitle)
		}
		if len(overview.Relationships) != 2 {
			t.Errorf("aantal relaties in het overzicht = %d, want 2: %+v",
				len(overview.Relationships), overview.Relationships)
		}

		// Het spoor van het standpunt dat de chat citeerde levert de id's van
		// de tak erboven — en het is meteen de controle dat "open in de
		// kennisgraaf" vanuit een bronkaart werkt.
		if chatSourceID == "" {
			t.Fatal("voorwaarde ontbreekt: de chat leverde geen bron-ID om in de graaf te openen")
		}
		var standpunt graphNodeResponseBody
		raw = srv.getJSON(t, "/api/duif/v1/graph/nodes/"+chatSourceID, &standpunt)
		assertNoStorageKey(t, "node(destillaat)", raw)
		if standpunt.Node.ID != chatSourceID {
			t.Errorf("graafknoop %q verschilt van geciteerde bron-ID %q", standpunt.Node.ID, chatSourceID)
		}
		if wireText(standpunt.Node.Label, "nl") != kbSummaryNesting {
			t.Errorf("label van het standpunt = %v, want de samenvatting", standpunt.Node.Label)
		}
		if len(standpunt.Trail) != 3 {
			t.Fatalf("spoor van het standpunt = %d stappen, want 3: %+v", len(standpunt.Trail), standpunt.Trail)
		}
		for i, want := range []string{"domein", "thema", "subthema"} {
			if standpunt.Trail[i].Type != want {
				t.Fatalf("spoor[%d] = %s, want %s (volledig %+v)", i, standpunt.Trail[i].Type, want, standpunt.Trail)
			}
		}
		if wireText(standpunt.Trail[2].Label, "nl") != kbSubthemaTitle {
			t.Errorf("label van het subthema = %v, want %q", standpunt.Trail[2].Label, kbSubthemaTitle)
		}
		subthemaID = standpunt.Trail[2].ID

		// Eén niveau dieper: de drie standpunten onder het subthema.
		var kinderen graphChildrenBody
		raw = srv.getJSON(t, "/api/duif/v1/graph/nodes/"+subthemaID+"/children", &kinderen)
		assertNoStorageKey(t, "children(subthema)", raw)
		if kinderen.Total != 3 || len(kinderen.Nodes) != 3 {
			t.Fatalf("kinderen van het subthema = %d (total %d), want 3: %+v",
				len(kinderen.Nodes), kinderen.Total, kinderen.Nodes)
		}
		if kinderen.Truncated {
			t.Error("drie kinderen passen ruim binnen de cap")
		}
		for _, n := range kinderen.Nodes {
			if n.Type != "destillaat" {
				t.Errorf("kind van een subthema = %s, want destillaat: %+v", n.Type, n)
			}
			if wireText(n.Metadata["text"], "nl") == kbSummaryFeeding {
				destillaatFeedingID = n.ID
			}
		}
		if destillaatFeedingID == "" {
			t.Fatalf("het standpunt met de twee citaten staat niet onder het subthema: %+v", kinderen.Nodes)
		}

		// Nog een niveau dieper: de citaten van dat standpunt plus het
		// bronpaper dat naar hem wijst.
		raw = srv.getJSON(t, "/api/duif/v1/graph/nodes/"+destillaatFeedingID+"/children", &kinderen)
		assertNoStorageKey(t, "children(destillaat)", raw)
		perType := map[string]int{}
		for _, n := range kinderen.Nodes {
			perType[n.Type]++
			if n.Type == "quote" && wireText(n.Label, "nl") == kbQuoteMarket {
				quoteMarketID = n.ID
			}
			if n.Type == "interview" && wireText(n.Label, "nl") != kbPaperTitle {
				t.Errorf("label van het bronpaper = %v, want %q", n.Label, kbPaperTitle)
			}
		}
		if perType["quote"] != 2 || perType["interview"] != 1 {
			t.Fatalf("kinderen van het standpunt = %+v, want 2 citaten en 1 bronpaper: %+v", perType, kinderen.Nodes)
		}
		if quoteMarketID == "" {
			t.Fatalf("het gegronde citaat staat niet onder het standpunt: %+v", kinderen.Nodes)
		}

		// Eén knoop met zijn spoor terug naar de wortel:
		// citaat → standpunt → subthema → thema → domein.
		var node graphNodeResponseBody
		raw = srv.getJSON(t, "/api/duif/v1/graph/nodes/"+quoteMarketID, &node)
		assertNoStorageKey(t, "node(quote)", raw)
		if node.Node.ID != quoteMarketID {
			t.Errorf("node.id = %q, want %q", node.Node.ID, quoteMarketID)
		}
		if len(node.Trail) != 4 {
			t.Fatalf("spoor = %d stappen, want 4: %+v", len(node.Trail), node.Trail)
		}
		wantTrail := []string{"domein", "thema", "subthema", "destillaat"}
		for i, want := range wantTrail {
			if node.Trail[i].Type != want {
				t.Errorf("spoor[%d] = %s, want %s (volledig %+v)", i, node.Trail[i].Type, want, node.Trail)
			}
		}
		if wireText(node.Trail[0].Label, "nl") != kbDomeinTitle {
			t.Errorf("wortel van het spoor = %v, want %q", node.Trail[0].Label, kbDomeinTitle)
		}
		if strings.Contains(string(raw), `"perspective"`) {
			t.Errorf("de graaf toont geen perspectief meer: %s", raw)
		}

		// De drie foutpaden van de graaf.
		for _, tc := range []struct {
			naam   string
			path   string
			status int
			reason string
		}{
			{"onbekende node", "/api/duif/v1/graph/nodes/destillaat:00000000-0000-0000-0000-000000000000", http.StatusNotFound, "node_not_found"},
			{"vormfoute id", "/api/duif/v1/graph/nodes/destillaat:geen-uuid", http.StatusNotFound, "node_not_found"},
			{"negatieve offset", "/api/duif/v1/graph/nodes/" + subthemaID + "/children?offset=-1", http.StatusBadRequest, "invalid_offset"},
		} {
			status, body := srv.get(t, tc.path)
			if status != tc.status {
				t.Errorf("%s: status = %d, want %d (body %s)", tc.naam, status, tc.status, body)
			}
			var out struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				t.Errorf("%s: body %s ontleden: %v", tc.naam, body, err)
				continue
			}
			if out.Error != tc.reason {
				t.Errorf("%s: error = %q, want %q", tc.naam, out.Error, tc.reason)
			}
		}
	})

	t.Run("vergadering citeert dezelfde bron", func(t *testing.T) {
		status, body := srv.postJSON(t, http.MethodPost, "/api/duif/v1/meeting/sessions",
			map[string]any{"pigeonId": "biological", "lang": "nl"})
		if status != http.StatusCreated {
			t.Fatalf("vergadersessie openen: status = %d, body = %s", status, body)
		}
		var geopend struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal([]byte(body), &geopend); err != nil || geopend.SessionID == "" {
			t.Fatalf("sessiebody = %s (%v)", body, err)
		}
		basis := "/api/duif/v1/meeting/sessions/" + geopend.SessionID
		srv.cleanupMeeting(t, geopend.SessionID)

		events, stopStream := srv.openMeetingStream(t, geopend.SessionID)
		defer stopStream()
		nextMeetingEvent(t, events, "state")

		if status, body := srv.postJSON(t, http.MethodPost, basis+"/transcript",
			map[string]any{"text": kbSummaryNesting, "speaker": "voorzitter"}); status != http.StatusAccepted {
			t.Fatalf("transcriptsegment: status = %d, body = %s", status, body)
		}
		nextMeetingEvent(t, events, "transcript")

		beforeRequests := len(srv.llm.requests())
		if status, body := srv.postJSON(t, http.MethodPost, basis+"/respond", nil); status != http.StatusAccepted {
			t.Fatalf("de knop: status = %d, body = %s", status, body)
		}
		var answer strings.Builder
		var klaar sseFrame
		for {
			ev := nextMeetingEvent(t, events, "")
			if ev["type"] == "error" {
				t.Fatal("source meeting returned a terminal error")
			}
			if ev["type"] == "text" {
				delta, _ := ev["delta"].(string)
				answer.WriteString(delta)
			}
			if ev["type"] == "response_done" {
				klaar = ev
				break
			}
		}
		// Twee calls per vergaderbeurt: de herformulering vóór de retrieval
		// en de spreekbeurt zelf.
		assertAnswerRequests(t, srv.waitForRequests(t, beforeRequests+2)[beforeRequests:], "biological", "nl", true, true, []string{kbSummaryNesting}, answer.String())
		if klaar["kbStatus"] != "sources" {
			t.Fatalf("kbStatus = %v, want sources: %+v", klaar["kbStatus"], klaar)
		}
		var done struct {
			Sources []sourceBody `json:"sources"`
		}
		reMarshal(t, klaar, &done)
		if len(done.Sources) == 0 {
			t.Fatalf("response_done zonder bronnen: %+v", klaar)
		}
		bron := done.Sources[0]
		if bron.Text != kbSummaryNesting || bron.Ref.Quote != kbQuoteNesting || bron.Ref.Paper != kbPaperTitle {
			t.Errorf("vergaderbron = %+v, want dezelfde DTO als in de chat", bron)
		}

		if status, body := srv.postJSON(t, http.MethodDelete, basis, nil); status != http.StatusNoContent {
			t.Fatalf("sessie sluiten: status = %d, body = %s", status, body)
		}
	})

	t.Run("tweede boot slaat de import over", func(t *testing.T) {
		srv.stop()
		voor := srv.llm.embedCallCount()

		// Dezelfde bundel, dezelfde database, dezelfde stub-LLM: de marker
		// (content-hash + mapper-versie + embedder) hoort elke schrijfactie en
		// dus elke embed-aanroep te voorkomen.
		tweede := bootServerWith(t, serverConfig{dbURL: dbURL, env: bootEnv, llm: srv.llm})
		st := tweede.waitImport(t, 30*time.Second, func(st importStatusBody) bool {
			return st.State == "ready" && st.Skipped
		})
		if st.KBID != kbFixtureID {
			t.Errorf("kbId = %q, want %q", st.KBID, kbFixtureID)
		}
		for table, want := range kbFixtureCounts {
			if st.Counts[table] != want {
				t.Errorf("counts[%s] = %d, want %d (uit de marker)", table, st.Counts[table], want)
			}
		}
		if na := tweede.llm.embedCallCount(); na != voor {
			t.Errorf("embed-aanroepen %d → %d: een overgeslagen import embedt niets", voor, na)
		}
		tweede.stop()
	})
}

// reMarshal leest een geparsed frame opnieuw in een getypeerde vorm. De
// SSE-helpers leveren map[string]any; deze omweg houdt de assertions leesbaar
// zonder de frames twee keer te hoeven parsen.
func reMarshal(t *testing.T, in any, out any) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("frame herserialiseren: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("frame %s ontleden: %v", raw, err)
	}
}

// assertNoStorageKey bewaakt de allowlist van de graaf op de bytes zelf: het
// bronpaper draagt een storage_key in de database, en die mag nergens in een
// antwoord opduiken — ook niet als sleutel van een lege waarde.
func assertNoStorageKey(t *testing.T, route string, body []byte) {
	t.Helper()
	for _, verboden := range []string{"storage_key", "storageKey", "embedding", "content_hash", "import_hash"} {
		if strings.Contains(string(body), verboden) {
			t.Errorf("%s lekt %q op de wire: %s", route, verboden, body)
		}
	}
}
