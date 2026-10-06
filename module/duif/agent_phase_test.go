package duif

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De stand van de beurt op de wire. Een chatbeurt is vier tot vijf
// modelcalls achter elkaar; deze frames zijn het enige waaruit de lezer kan
// opmaken bij welke hij zit. Dit bestand pint waar ze staan — vóór het werk
// dat ze aankondigen — en wat ze mogen dragen.
//
// De overige volgorde-tests van dit pakket lezen via parseSSE en zien deze
// frames niet; hier is parseSSEAll de bron, zodat de plaats tussen tool,
// artifact en done op één plek vastligt.

// knownPhases is de gesloten set die de server mag uitzenden. Hij staat hier
// als literal en niet als verzameling van de constanten: gaat er ooit een
// waarde stilletjes de deur uit die de client niet kent, dan hoort dat hier te
// breken en niet pas op het scherm.
var knownPhases = []string{"facilitator", "pigeon_grounding", "pigeon_related_search", "pigeon", "mediation"}

// phaseSequence geeft de fases in de volgorde waarin ze de deur uit gingen.
func phaseSequence(t *testing.T, frames []sseFrame) []string {
	t.Helper()
	var out []string
	for _, f := range frames {
		if f["type"] != "phase" {
			continue
		}
		// Privacy: een standframe draagt de stand en verder niets. Geen vraag,
		// geen zoekopdracht, geen argument.
		if len(f) != 2 {
			t.Fatalf("phase-frame draagt meer dan zijn stand: %+v", f)
		}
		phase, _ := f["phase"].(string)
		if !slices.Contains(knownPhases, phase) {
			t.Fatalf("onbekende fase %q op de wire; de client kent alleen %v", phase, knownPhases)
		}
		out = append(out, phase)
	}
	return out
}

// De client vertaalt elke fase naar een zin in zijn eigen taal. Kent hij er
// één niet, dan blijft de stapregel staan waar hij stond: geen machinetaal op
// het scherm, maar ook geen stap die de lezer mist. Lopen de twee lijsten uit
// de pas, dan is dat hier te zien en niet pas in de app.
func TestPhases_GoAndClientNameTheSameSet(t *testing.T) {
	const pad = "frontend/src/lib/chat/chatStore.svelte.ts"
	bron, err := os.ReadFile(pad)
	if err != nil {
		t.Fatalf("clientbron lezen: %v", err)
	}
	lijst := regexp.MustCompile(`(?s)export const PHASE_NAMES = \[(.*?)\]`).FindSubmatch(bron)
	if lijst == nil {
		t.Fatalf("PHASE_NAMES niet gevonden in %s", pad)
	}
	var client []string
	for _, item := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(string(lijst[1]), -1) {
		client = append(client, item[1])
	}
	server := slices.Clone(knownPhases)
	slices.Sort(client)
	slices.Sort(server)
	if !slices.Equal(client, server) {
		t.Errorf("client %v, server %v: de twee lijsten horen dezelfde set te zijn", client, server)
	}
}

// De gewone beurt van de Stakeholder: vraag formuleren, bronnen lezen,
// antwoord schrijven, afronden. Elk frame staat vóór de stap die het
// aankondigt — een stand die pas ná het wachten komt, vertelt niets.
func TestAgentPhases_NameEveryStepBeforeItRuns(t *testing.T) {
	m, _, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		textStep("Die brede stoep is van mij."),
		textStep("Ze heeft er duidelijke ideeën over."),
	})

	rec := postTurn(t, m, map[string]any{
		"pigeonId": "individualist", "message": "Wat vindt de duif van de Coolsingel?", "lang": "nl",
	}, false)
	frames := parseSSEAll(t, rec.Body.String())

	// De testkennisbank is leeg, dus deze beurt loopt ook langs de brede
	// zoekactie na een kennisgat — die levert hier niets op, maar hij kostte de
	// lezer wel tijd en heeft dus zijn eigen stap.
	want := []string{
		"phase", "tool", "phase", "phase", "artifact", "phase", "artifact_delta",
		"artifact", "tool_result", "phase", "text", "done",
	}
	if got := frameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v,\nwant %v", got, want)
	}
	wantPhases := []string{routeFacilitator, phaseGrounding, phaseRelated, routePigeon, routeMediation}
	if got := phaseSequence(t, frames); !slices.Equal(got, wantPhases) {
		t.Fatalf("fases = %v, want %v", got, wantPhases)
	}
	// De beurt eindigt bij done: daarna is er geen stap meer om te tonen.
	if frames[len(frames)-1]["type"] != "done" {
		t.Errorf("laatste frame = %v, want done", frames[len(frames)-1]["type"])
	}
}

// Een kennisgat met grenzend onderzoek: de brede zoekactie kost de
// lezer seconden, en die seconden krijgen hun eigen stap. Zij staat vóór het
// duif-blok, want ze bepaalt nog welke bronnen erin komen.
func TestAgentPhases_TheWideSearchAfterAGapIsItsOwnStep(t *testing.T) {
	// Met de productielat (0,73) staat deze bron boven de brede drempel (0,65)
	// en onder de gewone: de mensvraag levert een gat op, de brede zoekactie
	// vindt grenzend onderzoek. De testmodule zet beide latten anders gelijk,
	// en dan is het verschil dat deze stap bestáát niet te meten.
	m, _, _, _ := kbAgentModule(t,
		[]kbSeed{{
			id: "9", samenvatting: "Geluid verandert hoe stadsduiven daken gebruiken.",
			score: 0.68, quote: "Ze verlaten de nok bij harde knallen.", paper: "walcott1978",
			perspective: "De kritische wetenschapper", themaTitel: "Stadsecologie",
		}},
		[]llmtesting.StreamStep{
			askBiological(),
			textStep("Daarover weet ik niets."),
		}, func(m *Module) { m.minSimilarity = defaultMinSimilarity })

	rec := postTurn(t, m, map[string]any{"pigeonId": "biological", "message": "Dromen duiven?", "lang": "nl"}, false)
	frames := parseSSEAll(t, rec.Body.String())

	wantPhases := []string{routeFacilitator, phaseGrounding, phaseRelated, routePigeon}
	got := phaseSequence(t, frames)
	if !slices.Equal(got, wantPhases) {
		t.Fatalf("fases = %v, want %v", got, wantPhases)
	}
	// De verwante stap staat vóór het openen van het blok: de lezer wacht er
	// immers op, en pas daarna op haar eerste woord. De kennispartner
	// bemiddelt niet, dus er volgt ook geen stap die dat belooft.
	types := frameTypes(frames)
	open := slices.Index(types, "artifact")
	wide := -1
	for i, f := range frames {
		if f["type"] == "phase" && f["phase"] == phaseRelated {
			wide = i
		}
	}
	if open < 0 || wide < 0 || wide > open {
		t.Errorf("de brede zoekactie hoort vóór het duif-blok gemeld te zijn: %v", types)
	}
}

// Een wedervraag heeft geen duif en geen bronnen: dan blijft het bij de ene
// stap die er werkelijk is. Geen verzonnen "bronnen lezen" om de regel te
// vullen.
func TestAgentPhases_ClarifyStaysWithTheFacilitator(t *testing.T) {
	m, _, _ := newAgentModule(t, []llmtesting.StreamStep{
		toolCallStep("call-1", toolClarify, `{"ask":"Bedoel je de Coolsingel in Rotterdam?"}`),
	})

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "Wat vind je ervan?", "lang": "nl"}, false)
	frames := parseSSEAll(t, rec.Body.String())

	if got := phaseSequence(t, frames); !slices.Equal(got, []string{routeFacilitator}) {
		t.Fatalf("fases = %v, want alleen %q", got, routeFacilitator)
	}
}

// Zonder kennisbank leest de duif geen bronnen, en dan zegt het scherm dat ook
// niet. De pipeline-motor kent helemaal geen stappen: daar is de beurt zelf het
// antwoord.
func TestAgentPhases_PipelineTurnHasNone(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) {
		mock := llmtesting.NewMockService()
		mock.SetStreamScript([]llmtesting.StreamStep{textStep("Roekoe.")})
		m.llm = mock
	})

	rec := postTurn(t, m, map[string]any{"pigeonId": "nonverbal", "message": "Hoi", "lang": "nl"}, false)
	if got := phaseSequence(t, parseSSEAll(t, rec.Body.String())); len(got) != 0 {
		t.Fatalf("de pipeline-motor hoort geen fases te melden, kreeg %v", got)
	}
}
