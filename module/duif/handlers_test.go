package duif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Handler-tests voor de bootstrap-routes.
func TestHealth_OK(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/health", nil), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["status"] != "ok" {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestDuiven_ListsPersonasWithoutPrompts(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/duiven", nil), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Duiven []struct {
			ID            string    `json:"id"`
			Name          LocalName `json:"name"`
			KnowledgeBase bool      `json:"knowledgeBase"`
			Chat          bool      `json:"chat"`
			Meeting       bool      `json:"meeting"`
		} `json:"duiven"`
		Gedrag Gedrag `json:"gedrag"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body parsen: %v — %s", err, rec.Body.String())
	}
	if len(out.Duiven) != 3 {
		t.Fatalf("want 3 duiven, got %d", len(out.Duiven))
	}
	byID := map[string]bool{}
	for _, d := range out.Duiven {
		byID[d.ID] = true
		if d.Name.EN == "" || d.Name.NL == "" {
			t.Errorf("%s: naam mist een taal", d.ID)
		}
	}
	for _, id := range []string{"biological", "individualist", "nonverbal"} {
		if !byID[id] {
			t.Errorf("duif %q ontbreekt", id)
		}
	}
	if out.Gedrag.InactiviteitPromptSeconden != 600 || out.Gedrag.DetectieIntervalSeconden != 6 {
		t.Errorf("gedragsconfig-metadata hoort in de lijst (got %+v)", out.Gedrag)
	}

	// Systemprompts verlaten de server nooit (expliciete assert).
	body := rec.Body.String()
	for _, marker := range []string{"eis:P1", "Large Pigeon Model", "kennisgat-modus", "prompts/"} {
		if strings.Contains(body, marker) {
			t.Errorf("respons lekt promptinhoud (marker %q)", marker)
		}
	}
}

func TestDuiven_Unauthorized(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/duiven", nil), true)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestModule_NameAndMetadata(t *testing.T) {
	m := New()
	if m.Name() != "duif" {
		t.Errorf("Name = %q, want duif", m.Name())
	}
	md := m.Metadata()
	if md.Label["en"] == "" || md.Label["nl"] == "" {
		t.Errorf("Metadata moet een EN/NL-label dragen: %+v", md)
	}
	if md.Category != "interact" {
		t.Errorf("Category = %q, want interact", md.Category)
	}
}

func TestModule_PostgresMigrations(t *testing.T) {
	m := New()
	mig := m.PostgresMigrations()
	if mig.Module != "duif" || mig.Schema != "duif" {
		t.Errorf("migratiebundel fout: %+v", mig)
	}
	if mig.FS == nil || mig.FSRoot != "migrations" {
		t.Errorf("migratie-FS ontbreekt: %+v", mig)
	}
}

// De persona-DTO leest dezelfde bron als de vergadersessie: een placeholder,
// een lege stem-id én een non-verbale duif leveren voiceReady=false, een
// echte stem-id levert true. De dev-stem telt hier bewust niet mee — de DTO
// beschrijft personas.yaml, niet de omgeving.
func TestDuiven_VoiceReadyFollowsResolveVoiceID(t *testing.T) {
	m, _ := newTestModule(t, nil)
	// De config draagt inmiddels demo-stemmen; deze test toetst de
	// resolve-regel en zet de stemmen daarom zelf.
	for _, p := range m.personas.Personas {
		if p.ID == "individualist" {
			p.VoiceID = "echte-stem-id"
		} else {
			p.VoiceID = voicePlaceholder
		}
	}
	req, err := http.NewRequest(http.MethodGet, "/v1/duiven", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, m, req, false)
	var out struct {
		Duiven []duifDTO `json:"duiven"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body = %s (%v)", rec.Body.String(), err)
	}
	if len(out.Duiven) == 0 {
		t.Fatal("geen duiven in het antwoord")
	}
	for _, d := range out.Duiven {
		want := d.ID == "individualist"
		if d.VoiceReady != want {
			t.Errorf("duif %q: voiceReady = %v, want %v", d.ID, d.VoiceReady, want)
		}
	}
}

// De client moet vanaf het eerste teken weten wélke stem er praat: op de
// agent-motor opent de facilitator, op de pipeline de duif zelf. Dat staat
// per duif vast, dus het hoort in de personalijst en niet in een frame per
// beurt.
func TestDuiven_CarriesEngine(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/duiven", nil), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Duiven []duifDTO `json:"duiven"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body parsen: %v — %s", err, rec.Body.String())
	}
	want := map[string]string{
		"biological":    EngineAgent,
		"individualist": EngineAgent,
		"nonverbal":     EnginePipeline,
	}
	if len(out.Duiven) != len(want) {
		t.Fatalf("duiven = %d, want %d", len(out.Duiven), len(want))
	}
	for _, d := range out.Duiven {
		if d.Engine != want[d.ID] {
			t.Errorf("duif %q: engine = %q, want %q", d.ID, d.Engine, want[d.ID])
		}
	}
	// Het veld staat ook werkelijk onder deze naam op de wire.
	if !strings.Contains(rec.Body.String(), `"engine":"agent"`) {
		t.Errorf("de lijst moet het veld engine dragen: %s", rec.Body.String())
	}
}

// De opzetkaart moet de schakelaar "zelfstandig meedoen" op de goede stand
// kunnen zetten vóór de vergadering begint, en die stand verschilt per duif.
// Dus draagt de personalijst hem mee: de signalerende duif staat aan, de twee
// pratende duiven uit.
func TestDuiven_CarriesAutonomousDefaultPerPigeon(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/v1/duiven", nil), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Duiven []duifDTO `json:"duiven"`
		Gedrag Gedrag    `json:"gedrag"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body parsen: %v — %s", err, rec.Body.String())
	}
	want := map[string]bool{"biological": false, "individualist": false, "nonverbal": true}
	if len(out.Duiven) != len(want) {
		t.Fatalf("duiven = %d, want %d", len(out.Duiven), len(want))
	}
	for _, d := range out.Duiven {
		if d.AutonomousDefault != want[d.ID] {
			t.Errorf("duif %q: autonomousDefault = %v, want %v", d.ID, d.AutonomousDefault, want[d.ID])
		}
	}
	// De gedeelde terugval blijft ernaast staan; hij is niet meer wat de drie
	// duiven bepaalt, en dat verschil moet zichtbaar zijn op de wire.
	if out.Gedrag.InterventieZelfstandig {
		t.Error("de gedeelde terugval hoort uit te staan")
	}
	if !strings.Contains(rec.Body.String(), `"autonomousDefault":true`) {
		t.Errorf("de lijst moet het veld autonomousDefault dragen: %s", rec.Body.String())
	}
}
