package soniox

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
)

// tok is een korte constructor: de tests gaan over de semantiek van is_final,
// niet over tijdstempels en confidences.
func tok(text string, final bool) sttToken { return sttToken{Text: text, IsFinal: final} }

func texts(evs []transcriptEvent) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		kind := "partial"
		if ev.Final {
			kind = "final"
		}
		out = append(out, kind+":"+ev.Text)
	}
	return out
}

func assertEvents(t *testing.T, got []transcriptEvent, want ...string) {
	t.Helper()
	have := texts(got)
	if len(have) != len(want) {
		t.Fatalf("%d events, verwacht %d: %v", len(have), len(want), have)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Errorf("event %d = %q, verwacht %q", i, have[i], want[i])
		}
	}
}

// Het hart van het protocolverschil met ElevenLabs: Soniox stuurt losse
// tokens en de client plakt ze. Niet-finale tokens zijn een gok die in het
// volgende bericht wordt vervangen; finale tokens komen één keer langs.
func TestAggregator_NonFinalTokensAreReplacedNotAppended(t *testing.T) {
	a := newAggregator()
	assertEvents(t, a.consume([]sttToken{tok("Goede", false)}), "partial:Goede")
	assertEvents(t, a.consume([]sttToken{tok("Goedemiddag", false), tok(" allemaal", false)}),
		"partial:Goedemiddag allemaal")
}

// Finale tokens stapelen wél: ze vormen samen het segment dat straks het
// vergadervenster in gaat.
func TestAggregator_FinalTokensAccumulateIntoOneSegment(t *testing.T) {
	a := newAggregator()
	assertEvents(t, a.consume([]sttToken{tok("Goedemiddag", true)}), "partial:Goedemiddag")
	assertEvents(t, a.consume([]sttToken{tok(" allemaal", true), tok(".", true)}),
		"partial:Goedemiddag allemaal.")
	assertEvents(t, a.consume([]sttToken{tok(markerEnd, true)}), "final:Goedemiddag allemaal.")
}

// De markertoken van endpoint-detectie is de knip. Wat er in hetzelfde bericht
// achteraan komt hoort al bij de volgende uiting.
func TestAggregator_EndMarkerClosesSegmentAndNextTokensStartAFreshOne(t *testing.T) {
	a := newAggregator()
	a.consume([]sttToken{tok("Eerste zin", true)})
	assertEvents(t,
		a.consume([]sttToken{tok(markerEnd, true), tok(" Tweede", false)}),
		"final:Eerste zin", "partial:Tweede")
}

// Een expliciete finalize levert <fin> in plaats van <end>; beide knippen.
func TestAggregator_FinMarkerAlsoClosesTheSegment(t *testing.T) {
	a := newAggregator()
	a.consume([]sttToken{tok("Klaar", true)})
	assertEvents(t, a.consume([]sttToken{tok(markerFin, true)}), "final:Klaar")
}

// Een marker op een leeg segment is geen event: twee markers achter elkaar,
// of een <end> na stilte, mag het venster niet met een lege zin voeren.
func TestAggregator_MarkerOnEmptySegmentEmitsNothing(t *testing.T) {
	a := newAggregator()
	assertEvents(t, a.consume([]sttToken{tok(markerEnd, true)}))
	assertEvents(t, a.consume([]sttToken{tok(markerEnd, true), tok(markerFin, true)}))
}

// Een leeg bericht (Soniox stuurt die als er niets te melden valt) is stil.
func TestAggregator_EmptyMessageEmitsNothing(t *testing.T) {
	assertEvents(t, newAggregator().consume(nil))
}

// De vangrail onder de endpoint-detectie: wie aan één stuk doorpraat zou
// zonder <end> nooit een gecommit segment opleveren, en dus nooit in het
// vergadervenster belanden. Bij de grens knipt de aggregator zelf.
func TestAggregator_LongMonologueIsCutSoTheWindowKeepsGettingFed(t *testing.T) {
	a := newAggregator()
	a.maxSegmentChars = 20
	got := a.consume([]sttToken{tok("een twee drie vier vijf zes", true)})
	if len(got) != 1 || !got[0].Final {
		t.Fatalf("verwacht één gecommit segment, kreeg %v", texts(got))
	}
	// En daarna begint hij vers: het volgende segment draagt de staart niet.
	assertEvents(t, a.consume([]sttToken{tok(" zeven", false)}), "partial:zeven")
}

// maxSegmentChars van nul is de default, niet "knip bij elke token".
func TestAggregator_ZeroLimitFallsBackToTheDefault(t *testing.T) {
	a := &aggregator{}
	if a.limit() != defaultMaxSegmentChars {
		t.Errorf("limit() = %d, verwacht de default %d", a.limit(), defaultMaxSegmentChars)
	}
}

// Diarisatie: het eerste sprekerlabel in een segment is het label van het
// segment — de brug geeft het door aan het venster.
func TestAggregator_SegmentCarriesTheFirstSpeakerLabel(t *testing.T) {
	a := newAggregator()
	a.consume([]sttToken{{Text: "Hallo", IsFinal: true, Speaker: "1"}, {Text: " daar", IsFinal: true, Speaker: "1"}})
	evs := a.consume([]sttToken{tok(markerEnd, true)})
	if len(evs) != 1 || evs[0].Speaker != "1" {
		t.Fatalf("sprekerlabel raakte kwijt: %+v", evs)
	}
	// En het label reist niet mee naar het volgende segment.
	evs = a.consume([]sttToken{{Text: "Ik ook", IsFinal: true, Speaker: "2"}})
	if len(evs) != 1 || evs[0].Speaker != "2" {
		t.Fatalf("het vorige label plakte aan het nieuwe segment: %+v", evs)
	}
}

// Een niet-finale token mag het sprekerlabel zetten zolang het segment er nog
// geen heeft: de weergave toont dan meteen wie er praat.
func TestAggregator_NonFinalTokenMaySetTheSpeakerForTheDisplay(t *testing.T) {
	a := newAggregator()
	evs := a.consume([]sttToken{{Text: "Even", IsFinal: false, Speaker: "3"}})
	if len(evs) != 1 || evs[0].Speaker != "3" {
		t.Fatalf("partial droeg geen sprekerlabel: %+v", evs)
	}
}

// close() op een leeg segment is geen event — dat pad loopt bij het sluiten
// van een sessie waar niets meer in stond.
func TestAggregator_CloseOnEmptySegmentIsSilent(t *testing.T) {
	if _, ok := newAggregator().close(); ok {
		t.Error("een leeg segment hoort geen event op te leveren")
	}
}

// De gewirede logger wint van slog.Default(); zonder logger valt het pakket
// daarop terug. Beide takken, want een pakket dat stilletjes naar de
// default-logger schrijft ontsnapt aan de logconfiguratie van de server.
func TestLoggers_PreferTheWiredOneAndFallBackToTheDefault(t *testing.T) {
	eigen := slog.New(slog.NewTextHandler(io.Discard, nil))
	if (&transcriber{cfg: Config{Logger: eigen}}).log() != eigen {
		t.Error("de STT-kant negeert de gewirede logger")
	}
	if (&synthesizer{cfg: SynthConfig{Logger: eigen}}).log() != eigen {
		t.Error("de TTS-kant negeert de gewirede logger")
	}
	if (&transcriber{}).log() == nil || (&synthesizer{}).log() == nil {
		t.Error("zonder gewirede logger hoort de default te komen")
	}
}

// Een geannuleerde context is een nette afsluiting, geen storing: de brug
// erboven zou anders elke keer met een waarschuwing herstarten.
func TestIsClosed_TreatsCancellationAsANormalEnding(t *testing.T) {
	if !isClosed(context.Canceled) {
		t.Error("annulering hoort als nette afsluiting te tellen")
	}
	if isClosed(io.ErrUnexpectedEOF) {
		t.Error("een echte leesfout is geen nette afsluiting")
	}
}

// logMethods zijn de slog-niveaus; textArg herkent een argument dat inhoud
// kan dragen.
var logMethods = map[string]bool{"Debug": true, "Info": true, "Warn": true, "Error": true}

func textArg(arg ast.Expr) (string, bool) {
	switch v := arg.(type) {
	case *ast.Ident:
		if strings.EqualFold(v.Name, "text") || strings.EqualFold(v.Name, "transcript") {
			return v.Name, true
		}
	case *ast.SelectorExpr:
		if strings.EqualFold(v.Sel.Name, "text") || strings.EqualFold(v.Sel.Name, "transcript") {
			return v.Sel.Name, true
		}
	}
	return "", false
}

// Vergaderinhoud blijft vluchtig, statisch getoetst: dit pakket streamt en
// bewaart niets. Geen enkel bestand mag schijf, opslag of een subproces kunnen
// aanraken. Dit is de tegenhanger
// van TestMeeting_SourceImportsNoStorageAPI, die alleen duif/meeting*.go scant
// en dus over dit pakket niets zegt.
func TestSoniox_SourceImportsNoStorageAPI(t *testing.T) {
	verboden := []string{"os", "io/fs", "path/filepath", "database/sql", "embed", "os/exec", "github.com/jackc/pgx"}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var gezien int
	for _, e := range entries {
		naam := e.Name()
		if !strings.HasSuffix(naam, ".go") || strings.HasSuffix(naam, "_test.go") {
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
				t.Fatal(err)
			}
			for _, v := range verboden {
				if pad == v || strings.HasPrefix(pad, v+"/") {
					t.Errorf("%s importeert %q — de sonioxbrug raakt schijf noch opslag", naam, pad)
				}
			}
		}
		// En geen enkele logregel draagt inhoud: het pakket logt tellingen en
		// redenen. Deze wandeling vangt de makkelijke fout — `ev.Text` of
		// `text` als logargument — voordat een vergaderzin in een logbestand
		// belandt.
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !logMethods[sel.Sel.Name] {
				return true
			}
			for _, arg := range call.Args {
				if naam, ok := textArg(arg); ok {
					t.Errorf("%s logt %s — een logregel draagt nooit vergaderinhoud", naam, naam)
				}
			}
			return true
		})
	}
	if gezien < 3 {
		t.Fatalf("slechts %d bronbestanden gescand — controleer de bestandsnamen", gezien)
	}
}
