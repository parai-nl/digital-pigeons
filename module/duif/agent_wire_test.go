package duif

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// Het wire-contract van de nieuwe frames, veldnaam voor veldnaam. De frontend
// leest hierop, dus een hernoeming hoort een testwijziging te zijn en geen
// stille breuk.
func TestAgentFrames_WireShape(t *testing.T) {
	tests := []struct {
		naam  string
		frame any
		want  string
	}{
		{"tool", toolFrame{Type: "tool", ID: "tool-1", Name: toolAskPigeon},
			`{"type":"tool","id":"tool-1","name":"ask_pigeon"}`},
		{"tool_result", toolResultFrame{Type: "tool_result", ID: "tool-1", Name: toolAskPigeon},
			`{"type":"tool_result","id":"tool-1","name":"ask_pigeon"}`},
		{"tool_result met fout", toolResultFrame{Type: "tool_result", ID: "tool-2", Name: toolClarify, Error: ReasonClarifyAlreadyAsked},
			`{"type":"tool_result","id":"tool-2","name":"clarify","error":"clarify_already_asked"}`},
		{"phase", phaseFrame{Type: "phase", Phase: phaseGrounding},
			`{"type":"phase","phase":"pigeon_grounding"}`},
		{"artifact open", artifactFrame{Type: "artifact", ID: "pigeon-1", Kind: KindPigeonAnswer, Persona: "individualist"},
			`{"type":"artifact","id":"pigeon-1","kind":"pigeon_answer","persona":"individualist"}`},
		{"artifact dicht", artifactFrame{Type: "artifact", ID: "pigeon-1", Kind: KindPigeonAnswer, Persona: "individualist", Done: true},
			`{"type":"artifact","id":"pigeon-1","kind":"pigeon_answer","persona":"individualist","done":true}`},
		{"artifact_delta", artifactDeltaFrame{Type: "artifact_delta", ID: "pigeon-1", Delta: "roekoe"},
			`{"type":"artifact_delta","id":"pigeon-1","delta":"roekoe"}`},
		{"done met stopReason", doneFrame{Type: "done", SessionID: "s1", KBStatus: KBPersonaOnly, StopReason: "natural"},
			`{"type":"done","sessionId":"s1","kbStatus":"persona_only","stopReason":"natural"}`},
		{"done zonder stopReason blijft het oude frame", doneFrame{Type: "done", SessionID: "s1", KBStatus: KBPersonaOnly},
			`{"type":"done","sessionId":"s1","kbStatus":"persona_only"}`},
	}
	for _, tc := range tests {
		raw, err := json.Marshal(tc.frame)
		if err != nil {
			t.Fatalf("%s: %v", tc.naam, err)
		}
		if string(raw) != tc.want {
			t.Errorf("%s:\n kreeg %s\n want  %s", tc.naam, raw, tc.want)
		}
	}
}

// Privacy — het standframe draagt de stand en niets anders. De
// gouden JSON hierboven toetst de vorm die er nú is; deze reflectie toetst de
// vorm die er morgen bij komt. Een veld `query`, `args` of `question` erbij
// zetten is dan een zichtbaar besluit in plaats van een regel code.
func TestPhaseFrame_CarriesOnlyTheStand(t *testing.T) {
	typ := reflect.TypeOf(phaseFrame{})
	var velden []string
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.String {
			t.Errorf("veld %s is %s; een standframe draagt alleen korte, vaste waarden", f.Name, f.Type)
		}
		velden = append(velden, f.Name)
	}
	if want := []string{"Type", "Phase"}; !slices.Equal(velden, want) {
		t.Errorf("phaseFrame-velden = %v, want %v", velden, want)
	}
}

// agentBestanden is de inventaris van de facilitator-motor: precies deze
// bestanden draaien de tool-lus. Een exacte verzameling en geen prefixregel,
// want een nieuw bestand op dit pad (`facilitator.go`) zou anders stilzwijgend
// ongescand blijven — de scan hoort dan te breken in plaats van eraan te
// ontsnappen.
var agentBestanden = []string{"agent_tools.go", "agent_turn.go", "agent_wire.go"}

// Privacy — het agent-pad raakt de schijf niet. Het schrijft per ontwerp wél
// in de chatrepo (dat is de historie die de gebruiker terugziet), dus deze
// scan verbiedt alleen bestandssysteem- en procestoegang: daar heeft een
// tool-lus niets te zoeken, en een tool die dat ooit wél doet hoort een
// besluit te zijn en geen import.
//
// De importcheck alleen is te zwak: een package-lokale helper (configFS,
// migrationsFS, frontendFS, OpenSound) brengt de schijf binnen zonder ook maar
// één verboden import. Vandaar dezelfde identifier-pass als de vergaderscan,
// met de namen die op dít pad de schijf openen.
func TestAgentSource_ImportsNoDiskOrProcessAPI(t *testing.T) {
	verbodenImports := []string{"os", "io/ioutil", "io/fs", "path/filepath", "os/exec", "embed", "net"}
	verbodenNamen := map[string]string{
		"configFS":        "de embedded persona-config",
		"migrationsFS":    "de migratiebundel",
		"frontendFS":      "de frontend-bundel",
		"OpenSound":       "de geluiden-FS",
		"LoadPersonas":    "de configlader (leest van schijf)",
		"newPostgresRepo": "de Postgres-repo (buiten de ChatRepo-seam om)",
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var gezien []string
	for _, e := range entries {
		naam := e.Name()
		if !strings.HasPrefix(naam, "agent_") || !strings.HasSuffix(naam, ".go") || strings.HasSuffix(naam, "_test.go") {
			continue
		}
		gezien = append(gezien, naam)
		file, err := parser.ParseFile(fset, naam, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s parsen: %v", naam, err)
		}
		for _, imp := range file.Imports {
			pad, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: import %s: %v", naam, imp.Path.Value, err)
			}
			for _, verboden := range verbodenImports {
				if pad == verboden || strings.HasPrefix(pad, verboden+"/") {
					t.Errorf("%s importeert %q — de facilitator-motor raakt schijf noch proces", naam, pad)
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if wat, verboden := verbodenNamen[id.Name]; verboden {
				t.Errorf("%s noemt %s (%s) — de facilitator-motor raakt schijf noch proces", naam, id.Name, wat)
			}
			return true
		})
	}
	slices.Sort(gezien)
	if !slices.Equal(gezien, agentBestanden) {
		t.Fatalf("gescande bestanden = %v, want %v — nieuw bestand op dit pad? zet hem in agentBestanden", gezien, agentBestanden)
	}
}

// Privacy — gedachtestromen komen de module niet uit: geen reasoning-tekst in
// een frame en niets ervan in de opslag. Het redeneren zelf staat bewust aan
// (chatReasoningEffort); juist daarom is deze test de vangrail: de proxy
// stuurt werkelijk gedachtetekst terug.
func TestAgentTurn_ReasoningNeverLeavesTheLoop(t *testing.T) {
	const gedachte = "GEHEIME-GEDACHTE-8891"
	m, repo, mock := newAgentModule(t, []llmtesting.StreamStep{
		{Chunks: []llm.StreamChunk{{Reasoning: gedachte}, {Content: "Zo denk ik erover.", FinishReason: "stop"}}},
	})

	rec := postTurn(t, m, map[string]any{"pigeonId": "individualist", "message": "hoi"}, false)
	if strings.Contains(rec.Body.String(), gedachte) {
		t.Errorf("reasoning-tekst staat op de wire:\n%s", rec.Body.String())
	}
	for _, msg := range repo.messagesWithRole("assistant") {
		if strings.Contains(msg.Content, gedachte) {
			t.Errorf("reasoning-tekst is opgeslagen: %+v", msg)
		}
	}
	// Wat de constante wáárd hoort te zijn staat in
	// TestReasoningEffort_TheDecisionIsLowForChatAndSpokenTurnNoneForDetectionAndQueries;
	// hier gaat het erom dat de facilitator hem meestuurt.
	if got := mock.Calls[0].ReasoningEffort; got == nil || *got != chatReasoningEffort {
		t.Errorf("de facilitator hoort reasoning_effort %q mee te sturen, kreeg %v", chatReasoningEffort, got)
	}
}
