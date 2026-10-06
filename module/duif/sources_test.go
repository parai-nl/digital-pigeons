package duif

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// kbTestModule bouwt een module met een gevulde kennisgraaf en een gescripte
// LLM-stream.
func kbTestModule(t *testing.T, seeds []kbSeed, script []llmtesting.StreamStep) *Module {
	t.Helper()
	mock := llmtesting.NewMockService()
	mock.SetStreamScript(script)
	f := &fakeKnowledge{}
	for _, s := range seeds {
		seedDestillaat(t, f, s)
	}
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = f
	})
	// Dit is de regressiesuite van het pipeline-contract; de kennispartner
	// staat in productie op de facilitator (agent_kb_test.go toetst dat pad).
	usePipeline(t, m, "biological")
	return m
}

// Gap-gedrag op de wire.
func TestTurn_Gap_NoSourcesFrame_KbStatusGap(t *testing.T) {
	m := kbTestModule(t,
		[]kbSeed{{id: "1", samenvatting: "te vaag", score: 0.2}},
		[]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "Daar is geen onderzoek over.", FinishReason: "stop"}}}})

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Wat vinden duiven van belastingbeleid?"}, false)
	frames := parseSSE(t, rec.Body.String())
	types := frameTypes(frames)
	if strings.Join(types, ",") != "text,done" {
		t.Fatalf("frames = %v, want [text done] — géén sources-frame in gap", types)
	}
	done := frames[len(frames)-1]
	if done["kbStatus"] != string(KBGap) {
		t.Errorf("done.kbStatus = %v, want gap", done["kbStatus"])
	}
	if _, has := done["sources"]; has {
		t.Error("gap-done mag geen sources dragen")
	}
}

// In gap-modus komt er geen citatiemarkering op het scherm, ook niet als
// het model er tóch een schrijft. Het directief is de bedoeling, dit de
// garantie.
func TestTurn_Gap_StripsCitationMarkersFromStreamAndStorage(t *testing.T) {
	m := kbTestModule(t,
		[]kbSeed{{id: "1", samenvatting: "te vaag", score: 0.2}},
		[]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{
			{Content: "Daar is geen onderzoek over [1"},
			{Content: "]. Wel weet ik [2, 3] iets."},
			{Content: " Zie het kader [zie kader].", FinishReason: "stop"},
		}}})

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Iets systemisch?"}, false)
	frames := parseSSE(t, rec.Body.String())
	var text strings.Builder
	for _, f := range frames {
		if f["type"] == "text" {
			text.WriteString(f["delta"].(string))
		}
	}
	got := text.String()
	for _, marker := range []string{"[1]", "[2, 3]"} {
		if strings.Contains(got, marker) {
			t.Errorf("citatiemarkering %s haalde de stream: %q", marker, got)
		}
	}
	if !strings.Contains(got, "geen onderzoek over.") {
		t.Errorf("de zin hoort netjes te sluiten zonder losse spatie: %q", got)
	}
	if !strings.Contains(got, "[zie kader]") {
		t.Errorf("blokhaken zonder cijfer zijn gewone tekst: %q", got)
	}
}

// Het sources-contract op de wire.
func TestTurn_Sources_FrameBeforeFirstText(t *testing.T) {
	m := kbTestModule(t,
		[]kbSeed{
			{id: "9", samenvatting: "Duiven herkennen gezichten.", score: 0.9, quote: "Ze kenden mij.", paper: "lewald1987", perspective: "De kritische wetenschapper", themaTitel: "Cognitie"},
			{id: "8", samenvatting: "Duiven navigeren op magneetvelden.", score: 0.7, quote: "Ik zag ze zo terugvliegen.", paper: "walcott1978"},
		},
		[]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{
			{Content: "Wij herkennen jullie echt [1]."},
			{Content: " En we verdwalen zelden [2].", FinishReason: "stop"},
		}}})

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Herkennen duiven mensen?"}, false)
	frames := parseSSE(t, rec.Body.String())
	types := frameTypes(frames)
	// De done sluit de rij: de bronnen staan vóór de eerste tekst, en er
	// volgt geen label meer achter de beurt aan.
	if strings.Join(types, ",") != "sources,text,text,done" {
		t.Fatalf("frames = %v, want [sources text text done] (sources vóór de eerste text)", types)
	}

	raw, _ := json.Marshal(frames[0]["sources"])
	var sources []Source
	if err := json.Unmarshal(raw, &sources); err != nil {
		t.Fatalf("sources-frame parsen: %v", err)
	}
	if len(sources) != 2 || sources[0].Number != 1 || sources[1].Number != 2 {
		t.Fatalf("nummering moet stabiel en 1-based zijn: %+v", sources)
	}
	if sources[0].Ref.Quote != "Ze kenden mij." || sources[0].Ref.Paper != "lewald1987" {
		t.Errorf("ref moet naar citaat en bronpaper terugwijzen: %+v", sources[0].Ref)
	}
	if sources[0].Perspective != "De kritische wetenschapper" || sources[0].Theme != "Cognitie" {
		t.Errorf("perspectief/thema ontbreken: %+v", sources[0])
	}
	if sources[0].ID == "" {
		t.Error("de bron moet een node-id dragen (open in de kennisgraaf)")
	}

	// In sources-modus blijft de tekst ongemoeid: de markeringen wijzen ergens naar.
	if !strings.Contains(rec.Body.String(), "[1]") {
		t.Error("bij bronnen horen citatiemarkeringen juist te blijven staan")
	}

	// done herhaalt de bronnenlijst.
	done := doneFrameOf(t, frames)
	if done["kbStatus"] != string(KBSources) {
		t.Errorf("done.kbStatus = %v, want sources", done["kbStatus"])
	}
	rawDone, _ := json.Marshal(done["sources"])
	var doneSources []Source
	if err := json.Unmarshal(rawDone, &doneSources); err != nil || len(doneSources) != 2 {
		t.Errorf("done moet de bronnenlijst herhalen: %v (%v)", done["sources"], err)
	}
	if doneSources[0].Number != sources[0].Number || doneSources[0].ID != sources[0].ID {
		t.Errorf("done en sources moeten dezelfde bronnen dragen: %+v vs %+v", doneSources[0], sources[0])
	}
}

// De nummering heeft één bron: wat de prompt aan de duif toont, staat ook op
// de wire.
func TestTurn_Sources_NumbersMatchPromptAndWire(t *testing.T) {
	mock := llmtesting.NewMockService()
	mock.SetStreamScript([]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "Zo is dat [1][2].", FinishReason: "stop"}}}})
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "a", samenvatting: "Eerste.", score: 0.95, paper: "p1"})
	seedDestillaat(t, f, kbSeed{id: "b", samenvatting: "Tweede.", score: 0.85, paper: "p2"})
	m, _ := newTestModule(t, func(m *Module) {
		m.llm = mock
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = f
	})

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Iets?"}, false)
	frames := parseSSE(t, rec.Body.String())
	raw, _ := json.Marshal(frames[0]["sources"])
	var sources []Source
	if err := json.Unmarshal(raw, &sources); err != nil {
		t.Fatalf("sources-frame parsen: %v", err)
	}
	sys := mock.Calls[0].Messages[0].Content
	for _, s := range sources {
		if !strings.Contains(sys, `<source id="`+strconv.Itoa(s.Number)+`"`) {
			t.Errorf("bron %d staat niet met datzelfde nummer in de prompt:\n%s", s.Number, sys)
		}
		if !strings.Contains(sys, s.Text) {
			t.Errorf("brontekst %q ontbreekt in de prompt", s.Text)
		}
	}
}

// De node-id gaat mee (die opent het standpunt in de graaf); de similarity-score
// nooit — die zou op het scherm autoriteit suggereren die hij niet heeft.
func TestSources_DTO_CarriesIDButNeverScores(t *testing.T) {
	m := kbTestModule(t,
		[]kbSeed{{id: "geheim-42", samenvatting: "Iets.", score: 0.9, paper: "p1"}},
		[]llmtesting.StreamStep{{Chunks: []llm.StreamChunk{{Content: "Iets [1].", FinishReason: "stop"}}}})

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Iets?"}, false)
	body := rec.Body.String()
	if !strings.Contains(body, "destillaat:geheim-42") {
		t.Error("de node-id hoort mee te gaan: zonder id kan de bronkaart de graaf niet openen")
	}
	if strings.Contains(body, "\"score\"") || strings.Contains(body, "0.9") {
		t.Error("similarity-scores mogen de wire niet bereiken")
	}
}

// Oudere berichten dragen het oude bronschema (ref{file,doi}).
// Ze moeten zonder fout te lezen blijven — anders breekt een oud gesprek.
func TestSource_DecodesLegacyRowSchema(t *testing.T) {
	const legacy = `[{"number":1,"type":"standpunt","text":"Duiven eten brood.","ref":{"file":"duiven.pdf","doi":"10.1/duiven"},"perspective":"De bezorgde bewoner","theme":"voedsel"}]`
	var sources []Source
	if err := json.Unmarshal([]byte(legacy), &sources); err != nil {
		t.Fatalf("oude rij is niet meer te decoderen: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("bronnen = %+v", sources)
	}
	got := sources[0]
	if got.Ref.File != "duiven.pdf" || got.Ref.DOI != "10.1/duiven" {
		t.Errorf("oude ref-velden gingen verloren: %+v", got.Ref)
	}
	if got.ID != "" || got.Ref.Quote != "" || got.Ref.Paper != "" {
		t.Errorf("nieuwe velden horen leeg te blijven bij een oude rij: %+v", got)
	}
	// En terug op de wire draagt zo'n rij geen lege nieuwe velden.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, veld := range []string{`"id"`, `"quote"`, `"paper"`} {
		if strings.Contains(string(raw), veld) {
			t.Errorf("leeg veld %s hoort niet op de wire: %s", veld, raw)
		}
	}
}
