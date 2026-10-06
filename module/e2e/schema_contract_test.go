//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This independent wire fixture declares what the compiled module must send.
// It does not import a product constructor or inspect private framework code.
type fixtureResponseFormat struct {
	Type       string `json:"type"`
	JSONSchema struct {
		Name   string          `json:"name"`
		Strict bool            `json:"strict"`
		Schema json.RawMessage `json:"schema"`
	} `json:"json_schema"`
}

// fixtureSchemaFormat bouwt een willekeurig gestructureerd antwoordformaat, om
// te bewijzen dat de stub er geen enkele fase meer voor kent.
func fixtureSchemaFormat(name string) fixtureResponseFormat {
	var result fixtureResponseFormat
	result.Type, result.JSONSchema.Strict = "json_schema", true
	result.JSONSchema.Name = name
	result.JSONSchema.Schema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["ok"],"properties":{"ok":{"type":"boolean"}}}`)
	return result
}

// Vroeger was er één fase met een schema: de bronaudit, die na het antwoord
// elke geleverde zin toetste en er een JSON-oordeel over terugstuurde. Die call
// is uit de beurt verdwenen, en daarmee is een beurt vrije prose van begin tot
// eind — het antwoord van de duif was dat altijd al, en niets anders vraagt de
// provider nog om een vorm.
//
// Deze test is de wacht die dat vasthoudt: wélke response_format een verzoek
// ook draagt, de stub kent er geen fase voor en weigert hem. Zou er ooit weer
// een gestructureerde call bijkomen, dan valt de e2e daarop om in plaats van
// hem stilzwijgend als vrije prose te laten passeren.
func TestStubRefusesEveryStructuredPhase(t *testing.T) {
	base := chatRequest{Model: "vertex/gemini-3.6-flash-eu", Stream: true,
		Messages: []chatMessage{
			{Role: "system", Content: "SYNTHETIC_INSTRUCTION"},
			{Role: "user", Content: "SYNTHETIC_HUMAN"},
		}}
	for name, format := range map[string]fixtureResponseFormat{
		"het oude auditschema": fixtureSchemaFormat("duiven_answer_audit"),
		"een nieuwe naam":      fixtureSchemaFormat("duiven_iets_anders"),
		"legacy json object": func() fixtureResponseFormat {
			f := fixtureSchemaFormat("duiven_answer_audit")
			f.Type, f.JSONSchema.Schema = "json_object", nil
			f.JSONSchema.Name = ""
			return f
		}(),
		"alleen een naam": func() fixtureResponseFormat {
			var f fixtureResponseFormat
			f.JSONSchema.Name = "duiven_answer_audit"
			return f
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			req := base
			req.ResponseFormat = format
			raw, _ := json.Marshal(req)
			response := httptest.NewRecorder()
			(&stubLLM{}).handleChat(response, httptest.NewRequest(http.MethodPost, "http://fixture.invalid/chat/completions", bytes.NewReader(raw)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("een gestructureerd verzoek werd alsnog beantwoord: status %d", response.Code)
			}
		})
	}
	// Hetzelfde verzoek zonder response_format is gewoon het antwoord van de
	// duif: de weigering hierboven zit in de vorm, niet in de rest van de call.
	raw, _ := json.Marshal(base)
	response := httptest.NewRecorder()
	(&stubLLM{}).handleChat(response, httptest.NewRequest(http.MethodPost, "http://fixture.invalid/chat/completions", bytes.NewReader(raw)))
	if response.Code != http.StatusOK {
		t.Fatalf("vrije prose werd geweigerd: status %d", response.Code)
	}
}
