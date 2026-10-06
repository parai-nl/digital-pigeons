package duif

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

// mustLoadEvalSet laadt de evaluatieset van schijf (duif/eval/kb-eval.yaml).
// Bewust niet uit de embedded config: een testset hoort niet in de binary.
func mustLoadEvalSet(t *testing.T) []kbEvalCase {
	t.Helper()
	cases, err := kbEvalSet()
	if err != nil {
		t.Fatalf("loadKBEval: %v", err)
	}
	return cases
}

// De set is het meetinstrument van de bronnendrempel. Zakt hij onder de
// minimale aantallen, dan meet een drempelsweep niets meer — vandaar een toets op de
// vorm van het bestand zelf, niet alleen op de lader.
func TestKBEval_EmbeddedSetMeetsPlanMinimums(t *testing.T) {
	cases := mustLoadEvalSet(t)
	if len(cases) < 20 {
		t.Fatalf("aantal vragen = %d, want ten minste 20", len(cases))
	}
	perClass := map[string]int{}
	inKBPerLang := map[string]int{}
	for _, c := range cases {
		perClass[c.Class]++
		if c.Class == kbClassInKB {
			inKBPerLang[c.Lang]++
		}
	}
	if perClass[kbClassInKB] < 10 {
		t.Errorf("in_kb = %d, want ten minste 10", perClass[kbClassInKB])
	}
	for _, lang := range promptLangs {
		if inKBPerLang[lang] < 5 {
			t.Errorf("in_kb in %q = %d, want ten minste 5", lang, inKBPerLang[lang])
		}
	}
	if perClass[kbClassRelated] < 5 {
		t.Errorf("related_unanswered = %d, want ten minste 5", perClass[kbClassRelated])
	}
	if perClass[kbClassOffTopic] < 5 {
		t.Errorf("off_topic = %d, want ten minste 5", perClass[kbClassOffTopic])
	}
	if perClass[kbClassInjection] < 1 {
		t.Errorf("injection = %d, want ten minste 1", perClass[kbClassInjection])
	}
}

// evalFS bouwt een eenregelig evaluatiebestand rond één vraagblok.
func evalFS(body string) fstest.MapFS {
	return fstest.MapFS{kbEvalFile: &fstest.MapFile{Data: []byte("questions:\n" + body)}}
}

const goodCase = `  - id: a
    lang: nl
    class: in_kb
    question: Waarom eten stadsduiven zo veel brood?
    expect: sources
`

func TestKBEval_LoadRejectsBrokenSets(t *testing.T) {
	tests := []struct {
		naam string
		body string
		want string
	}{
		{"lege set", "", "geen vragen"},
		{"onbekende klasse", strings.Replace(goodCase, "class: in_kb", "class: onzin", 1), "klasse"},
		{"onbekende taal", strings.Replace(goodCase, "lang: nl", "lang: de", 1), "taal"},
		{"lege vraag", strings.Replace(goodCase, "question: Waarom eten stadsduiven zo veel brood?", `question: "  "`, 1), "vraag"},
		{"onbekende verwachting", strings.Replace(goodCase, "expect: sources", "expect: misschien", 1), "verwachting"},
		{"in_kb dat een gat verwacht", strings.Replace(goodCase, "expect: sources", "expect: gap", 1), "in_kb"},
		{"off_topic dat bronnen verwacht", strings.Replace(goodCase, "class: in_kb", "class: off_topic", 1), "gap"},
		{"duplicaat-id", goodCase + goodCase, "duplicaat"},
	}
	for _, tt := range tests {
		t.Run(tt.naam, func(t *testing.T) {
			_, err := loadKBEval(evalFS(tt.body), kbEvalFile)
			if err == nil {
				t.Fatalf("loadKBEval accepteerde %q", tt.naam)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("fout = %q, want iets over %q", err, tt.want)
			}
		})
	}
}

func TestKBEval_LoadKeepsFileOrder(t *testing.T) {
	body := goodCase + `  - id: b
    lang: en
    class: off_topic
    question: What makes the best pizza base?
    expect: gap
`
	cases, err := loadKBEval(evalFS(body), kbEvalFile)
	if err != nil {
		t.Fatalf("loadKBEval: %v", err)
	}
	if len(cases) != 2 || cases[0].ID != "a" || cases[1].ID != "b" {
		t.Fatalf("cases = %+v, want a vóór b", cases)
	}
}

func TestKBEval_ThresholdsSweepThePlanRange(t *testing.T) {
	got := kbEvalThresholds()
	if len(got) != 21 {
		t.Fatalf("aantal drempels = %d, want 21 (0,45..0,85 in stappen van 0,02)", len(got))
	}
	if kbEvalThresholdKey(got[0]) != "0.45" || kbEvalThresholdKey(got[len(got)-1]) != "0.85" {
		t.Errorf("uiteinden = %s..%s, want 0.45..0.85", kbEvalThresholdKey(got[0]), kbEvalThresholdKey(got[len(got)-1]))
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("drempels lopen niet op: %v", got)
		}
	}
}

func TestKBEval_ResultVerdictsFlipAtTheThreshold(t *testing.T) {
	c := kbEvalCase{ID: "a", Lang: "nl", Class: kbClassInKB, Expect: KBSources}
	res := kbEvalResultFor(c, []float32{0.5, 0.4, 0.3})

	if res.TopScore != 0.5 {
		t.Errorf("TopScore = %v, want de hoogste score", res.TopScore)
	}
	if len(res.Verdicts) != len(kbEvalThresholds()) {
		t.Fatalf("aantal oordelen = %d, want er één per drempel", len(res.Verdicts))
	}
	// Onder de drempel van 0,49 haalt de topscore het net; boven 0,51 niet meer.
	if got, want := res.Verdicts["0.49"], "sources:ok"; got != want {
		t.Errorf("oordeel bij 0.49 = %q, want %q", got, want)
	}
	if got, want := res.Verdicts["0.51"], "gap:mismatch"; got != want {
		t.Errorf("oordeel bij 0.51 = %q, want %q", got, want)
	}
}

func TestKBEval_ResultWithoutScoresIsAGap(t *testing.T) {
	res := kbEvalResultFor(kbEvalCase{ID: "z", Lang: "en", Class: kbClassOffTopic, Expect: KBGap}, nil)
	if res.TopScore != 0 {
		t.Errorf("TopScore = %v, want 0 zonder kandidaten", res.TopScore)
	}
	if got := res.Verdicts["0.45"]; got != "gap:ok" {
		t.Errorf("oordeel = %q, want gap:ok", got)
	}
}

// scored is een kort hulpje: één resultaat met een gegeven topscore.
func scored(id, lang string, class string, expect KBStatus, top float32) kbEvalResult {
	return kbEvalResultFor(kbEvalCase{ID: id, Lang: lang, Class: class, Expect: expect}, []float32{top})
}

func TestKBEval_SummarizeScoresGapPrecisionAndRecall(t *testing.T) {
	results := []kbEvalResult{
		scored("1", "nl", kbClassInKB, KBSources, 0.70), // boven: sources, correct
		scored("2", "nl", kbClassInKB, KBSources, 0.40), // onder: gap, fout-positief gat
		scored("3", "nl", kbClassOffTopic, KBGap, 0.20), // onder: gap, correct
		scored("4", "nl", kbClassRelated, KBGap, 0.30),  // onder: gap, correct
		scored("5", "nl", kbClassRelated, KBGap, 0.80),  // boven: sources, gemist gat
	}
	prec, rec, acc := summarize(results, 0.55)
	// Drie voorspelde gaten, waarvan twee terecht; drie echte gaten, waarvan
	// twee gevonden; drie van de vijf oordelen kloppen.
	if !nearly(prec, 2.0/3.0) || !nearly(rec, 2.0/3.0) || !nearly(acc, 3.0/5.0) {
		t.Errorf("precisie=%.3f recall=%.3f accuratesse=%.3f, want 0.667/0.667/0.600", prec, rec, acc)
	}
}

func TestKBEval_SummarizeIsZeroWithoutData(t *testing.T) {
	prec, rec, acc := summarize(nil, 0.55)
	if prec != 0 || rec != 0 || acc != 0 {
		t.Errorf("lege set gaf %.3f/%.3f/%.3f, want nullen", prec, rec, acc)
	}
}

func nearly(got, want float64) bool { return got-want < 1e-9 && want-got < 1e-9 }

func TestKBEval_HistogramSplitsInKBFromTheRest(t *testing.T) {
	results := []kbEvalResult{
		scored("1", "nl", kbClassInKB, KBSources, 0.72),
		scored("2", "en", kbClassInKB, KBSources, 0.71),
		scored("3", "nl", kbClassOffTopic, KBGap, 0.31),
	}
	out := histogramText(results)
	if !strings.Contains(out, kbClassInKB) {
		t.Errorf("histogram noemt de in_kb-groep niet:\n%s", out)
	}
	if !strings.Contains(out, "n=2") || !strings.Contains(out, "n=1") {
		t.Errorf("histogram toont de groepsgroottes niet:\n%s", out)
	}
	// De bak 0,70–0,75 draagt beide in_kb-vragen; de rest hoort er niet in.
	if !strings.Contains(out, "0.70-0.75") || !strings.Contains(out, "0.30-0.35") {
		t.Errorf("histogram mist de bakken van de topscores:\n%s", out)
	}
	if strings.ContainsRune(out, '–') {
		t.Errorf("histogram hoort ASCII te zijn:\n%s", out)
	}
}

func TestKBEval_HistogramSurvivesAnEmptySet(t *testing.T) {
	if out := histogramText(nil); !strings.Contains(out, "n=0") {
		t.Errorf("leeg histogram = %q, want twee lege groepen", out)
	}
}

func TestKBEval_SummaryTextHasARowPerThresholdAndLanguage(t *testing.T) {
	results := []kbEvalResult{
		scored("1", "nl", kbClassInKB, KBSources, 0.72),
		scored("2", "en", kbClassOffTopic, KBGap, 0.31),
	}
	out := summaryText(results)
	for _, want := range []string{"0.45", "0.65", "alle", "en", "nl"} {
		if !strings.Contains(out, want) {
			t.Errorf("samenvatting mist %q:\n%s", want, out)
		}
	}
	// Per drempel drie regels: alle, en, nl. Zo is de tabel per taal te lezen
	// zonder de JSON erbij te pakken.
	if got, want := strings.Count(out, "0.45"), 3; got != want {
		t.Errorf("regels bij drempel 0.45 = %d, want %d", got, want)
	}
}

func TestKBEval_WriteJSONLinesOneObjectPerResult(t *testing.T) {
	results := []kbEvalResult{
		scored("1", "nl", kbClassInKB, KBSources, 0.72),
		scored("2", "en", kbClassOffTopic, KBGap, 0.31),
	}
	var buf strings.Builder
	if err := writeKBEvalJSON(&buf, results); err != nil {
		t.Fatalf("writeKBEvalJSON: %v", err)
	}
	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	var n int
	for sc.Scan() {
		n++
		var got kbEvalResult
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			t.Fatalf("regel %d is geen JSON-object: %v", n, err)
		}
		if got.ID == "" || got.Verdicts["0.55"] == "" {
			t.Errorf("regel %d mist id of oordelen: %s", n, sc.Text())
		}
	}
	if n != 2 {
		t.Errorf("aantal regels = %d, want er één per resultaat", n)
	}
}

func TestKBEval_CitationNumbersFindsMarkers(t *testing.T) {
	got := citationNumbers("Volgens [1] en [12] klopt dat, maar [0] en [x] niet.")
	want := []int{1, 12}
	if len(got) != len(want) {
		t.Fatalf("citaten = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("citaten = %v, want %v", got, want)
		}
	}
	if n := citationNumbers("Hier staat geen enkel nummer."); len(n) != 0 {
		t.Errorf("citaten = %v, want er geen", n)
	}
}
