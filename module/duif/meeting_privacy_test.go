package duif

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// sentinel is een tekst die nergens anders kan voorkomen: als hij ooit in een
// database, een bestand of een logregel opduikt, is dat aantoonbaar lek uit de
// vergaderketen.
const sentinel = "hagelslagcode-7731-vergaderzin"

// snapshotTree telt bestanden (pad → grootte) onder dir.
func snapshotTree(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // onleesbare takken zijn voor deze meting oninteressant
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr
		}
		out[path] = info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("bestandsboom lezen: %v", err)
	}
	return out
}

// Vergaderaudio en -transcript zijn streaming-only. Deze test
// draait een volledige nep-vergadering door alle lagen (kanaal, knop, TTS,
// stopknop, aanwezigheid, sluiting) en toetst daarna de drie sporen waarlangs
// inhoud zou kunnen weglekken: database, schijf en logs.
func TestMeeting_StreamingOnly_LeavesNoTraceInDatabaseDiskOrLogs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	work, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	gllm := newGatedLLM("De duif zegt: ", sentinel)
	voice := newFakeVoice("mp3-" + sentinel)
	rig := newMeetingRig(t, func(m *Module) { m.llm = gllm }, func(e *meetingEngine) { e.voice = voice })

	beforeTmp := snapshotTree(t, tmp)
	beforeWork := snapshotTree(t, work)

	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, sentinel+" — dit zegt de vergadering")
	nextEvent(t, es.ch, EventTranscript)
	rig.respond(t, id)
	text, _ := collectSpokenText(t, es.ch)
	if !strings.Contains(text, sentinel) {
		t.Fatalf("de duif sprak de sentinel niet uit: %q", text)
	}
	rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/stop", nil)
	nextEvent(t, es.ch, EventStopped)
	rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/presence", nil)
	rig.do(t, http.MethodDelete, "/v1/meeting/sessions/"+id, nil)
	nextEvent(t, es.ch, EventSessionClosed)

	// 1. Database: geen sessierij, geen bericht — de vergadermodus heeft geen
	// repository (zie TestMeeting_CannotPersistContent_ByConstruction).
	if len(rig.repo.sessions) != 0 || len(rig.repo.messages) != 0 {
		t.Errorf("vergaderinhoud belandde in de database: %+v / %+v", rig.repo.sessions, rig.repo.messages)
	}

	// 2. Schijf: geen enkel nieuw of gegroeid bestand, niet in de tempdir en
	// niet in de werkmap.
	for label, before := range map[string]map[string]int64{"tempdir": beforeTmp, "werkmap": beforeWork} {
		dir := tmp
		if label == "werkmap" {
			dir = work
		}
		for path, size := range snapshotTree(t, dir) {
			if old, ok := before[path]; !ok || old != size {
				t.Errorf("de vergadermodus schreef naar schijf (%s): %s", label, path)
			}
		}
	}

	// 3. Logs: uitsluitend lengtes, aantallen en tijden — nooit inhoud.
	logs := rig.logs()
	if strings.Contains(logs, sentinel) {
		t.Errorf("transcript- of antwoordtekst staat in de logs:\n%s", logs)
	}
	if logs == "" {
		t.Error("de vergadermodus hoort wél iets te loggen (tellers en tijden)")
	}
}

// De tegenhanger: streamen naar de deelnemer is juist het hele punt. Zonder
// deze assert zou een module die niets doet ook "privacyvriendelijk" heten.
func TestMeeting_TranscriptDoesReachTheClientStream(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")
	es := rig.events(t, id)
	rig.say(t, id, sentinel)
	if ev := nextEvent(t, es.ch, EventTranscript); ev.Text != sentinel {
		t.Fatalf("het kanaal hoort het transcript live te tonen: %+v", ev)
	}
}

// Niet-kunnen-persisteren is een compile-eigenschap: de vergader-engine krijgt
// via interface-segregatie alleen leesseams mee. Geen enkel veld draagt een
// schrijfmethode voor inhoud.
func TestMeeting_CannotPersistContent_ByConstruction(t *testing.T) {
	verboden := map[string]bool{
		"AppendMessage": true, "ListMessages": true, "ListSessions": true,
		"GetSession": true, "Exec": true, "Query": true, "Upsert": true,
		"Insert": true, "Save": true, "Store": true, "Write": true,
		"WriteFile": true, "Relate": true, "Unrelate": true,
	}
	// Prefixen in plaats van een naamlijst: de kennisgraaf heeft een hele
	// familie schrijfmethoden (CreateNodeWithID, UpdateNode, UpdatePersonaName,
	// …) en een lijst die per methode moet worden bijgehouden vergeet er ooit
	// één. Wat begint met Create/Update/Delete schrijft.
	verbodenPrefix := []string{"Create", "Update", "Delete"}
	isVerboden := func(naam string) bool {
		if verboden[naam] {
			return true
		}
		for _, p := range verbodenPrefix {
			if strings.HasPrefix(naam, p) {
				return true
			}
		}
		return false
	}
	var walk func(t *testing.T, typ reflect.Type, pad string, diepte int)
	walk = func(t *testing.T, typ reflect.Type, pad string, diepte int) {
		if diepte > 4 {
			return
		}
		// Een seam achter een pointer is nog steeds een seam: volg hem, anders
		// verdwijnt de hele retrieval-DAG uit deze toets.
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		for i := range typ.NumField() {
			f := typ.Field(i)
			naam := pad + "." + f.Name
			switch f.Type.Kind() {
			case reflect.Interface:
				for j := range f.Type.NumMethod() {
					if isVerboden(f.Type.Method(j).Name) {
						t.Errorf("%s (%s) draagt de schrijfmethode %s — de vergadermodus mag niet kunnen persisteren",
							naam, f.Type, f.Type.Method(j).Name)
					}
				}
			case reflect.Struct, reflect.Pointer:
				walk(t, f.Type, naam, diepte+1)
			}
		}
	}
	walk(t, reflect.TypeOf(meetingEngine{}), "meetingEngine", 0)
	walk(t, reflect.TypeOf(meetingSession{}), "meetingSession", 0)
}

// De toets hierboven is alleen iets waard als hij de leesseam werkelijk
// bereikt: een pointerveld dat stilletjes wordt overgeslagen zou elke
// schrijfmethode erachter verbergen.
func TestMeeting_PrivacyWalk_ReachesTheKnowledgeSeam(t *testing.T) {
	var gezien bool
	var walk func(typ reflect.Type, diepte int)
	walk = func(typ reflect.Type, diepte int) {
		if diepte > 4 {
			return
		}
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		for i := range typ.NumField() {
			f := typ.Field(i)
			if f.Type == reflect.TypeOf((*KnowledgeReader)(nil)).Elem() {
				gezien = true
			}
			if f.Type.Kind() == reflect.Struct || f.Type.Kind() == reflect.Pointer {
				walk(f.Type, diepte+1)
			}
		}
	}
	walk(reflect.TypeOf(meetingEngine{}), 0)
	if !gezien {
		t.Fatal("de privacywandeling bereikt de KnowledgeReader-seam niet meer; de toets bewijst dan niets")
	}
}

// Dezelfde eis statisch: de vergaderbestanden importeren geen opslag-API en
// noemen geen enkele persistentiemethode. Dit is de check die in CI de
// discipline vervangt.
func TestMeeting_SourceImportsNoStorageAPI(t *testing.T) {
	verbodenImports := []string{
		"os", "io/fs", "path/filepath", "database/sql",
		"github.com/jackc/pgx", "embed", "os/exec",
	}
	verbodenNamen := map[string]string{
		"ChatRepo":        "de chat-repository",
		"AppendMessage":   "een berichtschrijver",
		"newPostgresRepo": "de Postgres-repo",
		"migrationsFS":    "de migratiebundel",
		"knowledgeWriter": "de schrijfseam van de kennisgraaf",
		"kbImporter":      "de kennisbank-import",
		// De facilitator-motor schrijft per ontwerp wél in de chatrepo. Een
		// vergaderbestand dat er één van aanroept persisteert dus indirect,
		// zonder ook maar één van de namen hierboven te noemen.
		"runAgentTurn":        "de facilitator-motor (die schrijft in de chatrepo)",
		"newAgentTurn":        "de beurttoestand van de facilitator-motor",
		"persistPigeonAnswer": "de schrijver van duif-antwoorden",
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var gezien int
	for _, e := range entries {
		naam := e.Name()
		if !strings.HasPrefix(naam, "meeting") || !strings.HasSuffix(naam, ".go") || strings.HasSuffix(naam, "_test.go") {
			continue
		}
		gezien++
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
					t.Errorf("%s importeert %q — de vergadermodus raakt schijf noch opslag", naam, pad)
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if wat, verboden := verbodenNamen[id.Name]; verboden {
				t.Errorf("%s noemt %s (%s) — vergaderinhoud wordt nooit opgeslagen", naam, id.Name, wat)
			}
			return true
		})
	}
	if gezien < 3 {
		t.Fatalf("slechts %d vergaderbestanden gescand — controleer de bestandsnamen", gezien)
	}
}
