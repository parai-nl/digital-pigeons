package duif

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// kbEvalSet leest de evaluatieset van schijf, naast de package.
func kbEvalSet() ([]kbEvalCase, error) {
	return loadKBEval(os.DirFS(kbEvalDir), kbEvalFile)
}

// Het rekenwerk van de drempelkalibratie: de evaluatieset lezen,
// retrieval-scores tot een oordeel maken en dat oordeel leesbaar opschrijven.
// Bewust zonder database, embedder of netwerk — de live-test
// (kb_eval_live_test.go, buildtag `live`) levert alleen de scores aan.
//
// Testcode, en daarom een _test.go zonder buildtag: dit rekenwerk heeft geen
// enkele aanroeper in de productiebinary. De unit-test (kb_eval_test.go) en de
// live-test delen het via de package, en de binary wordt er niet groter van.
//
// De vraag die dit beantwoordt is er één met twee verliezen. Een te lage
// DUIF_MIN_SIMILARITY laat de duif over dingen praten waar geen standpunt over
// bestaat; een te hoge maakt een kennisgat van een vraag die de kennisbank wél
// dekt. Daarom draait alles hier om de scheiding tussen twee groepen — vragen
// die bronnen horen op te leveren en vragen die een gat horen te geven — en
// niet om één getal dat "goed" zou zijn.

const (
	// De evaluatieset staat naast de package op schijf, niet in config/: die
	// map gaat via go:embed de binary in, en een set van honderd vragen die
	// alleen een test leest hoort niet mee te reizen naar productie. De tests
	// lezen hem relatief aan de package-dir, want daar draait het testbinary.
	kbEvalDir  = "eval"
	kbEvalFile = "kb-eval.yaml"

	// De vier vraagklassen. Alleen in_kb hoort bronnen op te leveren; de drie
	// andere zijn elk een eigen manier waarop een gat hoort te ontstaan.
	kbClassInKB      = "in_kb"              // de kennisbank dekt dit vrijwel zeker
	kbClassRelated   = "related_unanswered" // zelfde domein, feit ontbreekt (de strenge klasse)
	kbClassOffTopic  = "off_topic"          // systemisch of buiten het onderwerp
	kbClassInjection = "injection"          // poging de duif zijn instructies te laten verlaten

	// De drempelsweep: 0,45 tot en met 0,85 in stappen van 0,02.
	kbEvalThresholdMin  = 0.45
	kbEvalThresholdStep = 0.02
	kbEvalThresholdN    = 21

	// kbEvalBucket is de bakbreedte van de histogrammen; kbEvalBuckets telt de
	// bakken over het volledige scorebereik [0,1).
	kbEvalBucket  = 0.05
	kbEvalBuckets = 20

	// De twee helften van een oordeel: wat de drempel zou beslissen, en of dat
	// klopt met wat de vraag verwacht.
	kbVerdictOK       = "ok"
	kbVerdictMismatch = "mismatch"
)

// kbEvalCase is één vraag uit de evaluatieset.
type kbEvalCase struct {
	ID       string   `yaml:"id"`
	Lang     string   `yaml:"lang"`
	Class    string   `yaml:"class"`
	Question string   `yaml:"question"`
	Expect   KBStatus `yaml:"expect"`
}

// kbEvalDoc is de vorm van kb-eval.yaml.
type kbEvalDoc struct {
	Questions []kbEvalCase `yaml:"questions"`
}

// kbEvalResult is de uitkomst van één vraag: de scores die de vectorzoekopdracht
// gaf, plus wat elke kandidaatdrempel daarmee zou hebben besloten. De vraagtekst
// zelf blijft eruit — het id wijst naar kb-eval.yaml, en zo blijft een
// uitvoerbestand klein en vergelijkbaar tussen runs.
type kbEvalResult struct {
	ID       string            `json:"id"`
	Lang     string            `json:"lang"`
	Class    string            `json:"class"`
	Expect   KBStatus          `json:"expect"`
	TopScore float32           `json:"topScore"`
	Scores   []float32         `json:"scores"`
	Verdicts map[string]string `json:"verdicts"`
}

// loadKBEval leest en valideert de evaluatieset. De validatie is streng om
// dezelfde reden als bij personas.yaml: een vraag met een verkeerde klasse of
// een ontbrekende verwachting meet stilletjes iets anders dan bedoeld, en dat
// zou pas opvallen in de uitkomst van een dure live-run.
func loadKBEval(fsys fs.FS, path string) ([]kbEvalCase, error) {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("duif: evaluatieset lezen: %w", err)
	}
	var doc kbEvalDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("duif: evaluatieset parsen: %w", err)
	}
	if len(doc.Questions) == 0 {
		return nil, fmt.Errorf("duif: evaluatieset %s bevat geen vragen", path)
	}
	seen := make(map[string]bool, len(doc.Questions))
	for _, c := range doc.Questions {
		if c.ID == "" {
			return nil, fmt.Errorf("duif: evaluatieset: vraag zonder id")
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("duif: evaluatieset: duplicaat-id %q", c.ID)
		}
		seen[c.ID] = true
		if err := validateKBEvalCase(c); err != nil {
			return nil, err
		}
	}
	return doc.Questions, nil
}

// validateKBEvalCase bewaakt één vraag. De laatste regel is de belangrijkste:
// klasse en verwachting mogen niet uit elkaar lopen, want de klasse is waarom
// een vraag in de set staat en de verwachting is wat er gemeten wordt.
func validateKBEvalCase(c kbEvalCase) error {
	if !slices.Contains(promptLangs, c.Lang) {
		return fmt.Errorf("duif: evaluatieset: vraag %q heeft taal %q, want %s", c.ID, c.Lang, strings.Join(promptLangs, " of "))
	}
	switch c.Class {
	case kbClassInKB, kbClassRelated, kbClassOffTopic, kbClassInjection:
	default:
		return fmt.Errorf("duif: evaluatieset: vraag %q heeft onbekende klasse %q", c.ID, c.Class)
	}
	if strings.TrimSpace(c.Question) == "" {
		return fmt.Errorf("duif: evaluatieset: vraag %q heeft geen vraagtekst", c.ID)
	}
	switch c.Expect {
	case KBSources, KBGap:
	default:
		return fmt.Errorf("duif: evaluatieset: vraag %q heeft onbekende verwachting %q", c.ID, c.Expect)
	}
	if c.Class == kbClassInKB && c.Expect != KBSources {
		return fmt.Errorf("duif: evaluatieset: vraag %q is in_kb maar verwacht %q", c.ID, c.Expect)
	}
	if c.Class != kbClassInKB && c.Expect != KBGap {
		return fmt.Errorf("duif: evaluatieset: vraag %q is %s en hoort daarom gap te verwachten, niet %q", c.ID, c.Class, c.Expect)
	}
	return nil
}

// kbEvalThresholds levert de kandidaatdrempels. Ze worden uit de index gerekend
// in plaats van opgeteld, zodat afrondingsfouten zich niet opstapelen en de
// sleutels van run tot run gelijk blijven.
func kbEvalThresholds() []float32 {
	out := make([]float32, 0, kbEvalThresholdN)
	for i := 0; i < kbEvalThresholdN; i++ {
		out = append(out, float32(kbEvalThresholdMin+kbEvalThresholdStep*float64(i)))
	}
	return out
}

// kbEvalThresholdKey is de sleutel van een drempel in Verdicts en in de tabel.
func kbEvalThresholdKey(t float32) string {
	return strconv.FormatFloat(float64(t), 'f', 2, 32)
}

// kbEvalResultFor maakt het oordeel van één vraag uit haar retrieval-scores.
// De topscore beslist: de gate laat een bron door zodra hij de drempel haalt,
// dus haalt de hoogste score hem niet, dan is de hele beurt een kennisgat.
func kbEvalResultFor(c kbEvalCase, scores []float32) kbEvalResult {
	var top float32
	for _, s := range scores {
		if s > top {
			top = s
		}
	}
	verdicts := make(map[string]string, kbEvalThresholdN)
	for _, th := range kbEvalThresholds() {
		verdicts[kbEvalThresholdKey(th)] = kbEvalVerdict(c.Expect, top, th)
	}
	return kbEvalResult{
		ID:       c.ID,
		Lang:     c.Lang,
		Class:    c.Class,
		Expect:   c.Expect,
		TopScore: top,
		Scores:   scores,
		Verdicts: verdicts,
	}
}

// kbEvalVerdict zegt wat één drempel zou beslissen en of dat klopt.
func kbEvalVerdict(expect KBStatus, top, threshold float32) string {
	got := kbEvalPredict(top, threshold)
	if got == expect {
		return string(got) + ":" + kbVerdictOK
	}
	return string(got) + ":" + kbVerdictMismatch
}

// kbEvalPredict spiegelt de filter van RAGRetrieve: een score haalt het vanaf
// de drempel, niet erboven.
func kbEvalPredict(top, threshold float32) KBStatus {
	if top >= threshold {
		return KBSources
	}
	return KBGap
}

// summarize telt één drempel door over de meegegeven resultaten. Filter de
// slice op taal om dezelfde cijfers per taal te krijgen.
//
// Het gat is hier de positieve klasse, want dat is de belofte van de duif:
// liever zwijgen dan iets verzinnen. Precisie zegt hoeveel van de gemelde gaten
// terecht waren, recall hoeveel van de echte gaten gevonden zijn. Een lege
// noemer levert 0 — er valt dan niets te meten, en NaN maakt de tabel
// onleesbaar.
func summarize(results []kbEvalResult, threshold float32) (precisionGap, recallGap, accuracy float64) {
	var predictedGap, trueGap, correctGap, correct int
	for _, r := range results {
		got := kbEvalPredict(r.TopScore, threshold)
		if got == r.Expect {
			correct++
		}
		if got == KBGap {
			predictedGap++
		}
		if r.Expect == KBGap {
			trueGap++
			if got == KBGap {
				correctGap++
			}
		}
	}
	return ratio(correctGap, predictedGap), ratio(correctGap, trueGap), ratio(correct, len(results))
}

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

// filterKBEvalLang geeft de resultaten van één taal.
func filterKBEvalLang(results []kbEvalResult, lang string) []kbEvalResult {
	out := make([]kbEvalResult, 0, len(results))
	for _, r := range results {
		if r.Lang == lang {
			out = append(out, r)
		}
	}
	return out
}

// histogramText tekent twee histogrammen van de topscores: de in_kb-vragen en
// alle vragen die een gat horen te geven. Dit is het beeld waar de drempel uit
// volgt — niet één cijfer, maar de vraag of er tussen de twee verdelingen
// überhaupt ruimte zit. Overlappen ze, dan is geen enkele drempel goed en is
// het probleem de kennisbank of de embedding, niet de knop.
//
// Beide histogrammen delen hetzelfde bakbereik, anders staan de staven van de
// twee groepen naast elkaar zonder vergelijkbaar te zijn.
func histogramText(results []kbEvalResult) string {
	var inKB, rest []kbEvalResult
	for _, r := range results {
		if r.Class == kbClassInKB {
			inKB = append(inKB, r)
			continue
		}
		rest = append(rest, r)
	}
	lo, hi := kbEvalBucketRange(results)

	var b strings.Builder
	writeHistogram(&b, kbClassInKB+" (verwacht bronnen)", inKB, lo, hi)
	b.WriteString("\n")
	writeHistogram(&b, "overig (verwacht een kennisgat)", rest, lo, hi)
	return b.String()
}

// kbEvalBucketRange zoekt de laagste en hoogste bak met data, zodat lege bakken
// aan de randen de tabel niet opvullen.
func kbEvalBucketRange(results []kbEvalResult) (lo, hi int) {
	lo, hi = kbEvalBuckets-1, 0
	var seen bool
	for _, r := range results {
		i := kbEvalBucketIndex(r.TopScore)
		seen = true
		if i < lo {
			lo = i
		}
		if i > hi {
			hi = i
		}
	}
	if !seen {
		return 0, 0
	}
	return lo, hi
}

func kbEvalBucketIndex(score float32) int {
	i := int(float64(score) / kbEvalBucket)
	if i < 0 {
		return 0
	}
	if i >= kbEvalBuckets {
		return kbEvalBuckets - 1
	}
	return i
}

func writeHistogram(b *strings.Builder, titel string, results []kbEvalResult, lo, hi int) {
	fmt.Fprintf(b, "%s  n=%d\n", titel, len(results))
	counts := make([]int, kbEvalBuckets)
	for _, r := range results {
		counts[kbEvalBucketIndex(r.TopScore)]++
	}
	for i := lo; i <= hi; i++ {
		from := float64(i) * kbEvalBucket
		fmt.Fprintf(b, "  %.2f-%.2f | %-20s %d\n", from, from+kbEvalBucket, bar(counts[i]), counts[i])
	}
}

// bar tekent een staaf van maximaal twintig tekens; het getal ernaast blijft
// de waarheid.
func bar(n int) string {
	if n > 20 {
		n = 20
	}
	if n < 0 {
		n = 0
	}
	return strings.Repeat("#", n)
}

// summaryText is de tabel waar de default uit gekozen wordt: per drempel de
// accuratesse en de precisie/recall op de gat-vragen, eerst over alles en dan
// per taal. Een drempel die alleen in het Engels werkt is geen default.
func summaryText(results []kbEvalResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s %-5s %4s %9s %9s %9s\n", "drempel", "taal", "n", "acc", "prec-gap", "rec-gap")
	for _, th := range kbEvalThresholds() {
		writeSummaryRow(&b, th, "alle", results)
		for _, lang := range promptLangs {
			writeSummaryRow(&b, th, lang, filterKBEvalLang(results, lang))
		}
	}
	return b.String()
}

func writeSummaryRow(b *strings.Builder, th float32, label string, results []kbEvalResult) {
	prec, rec, acc := summarize(results, th)
	fmt.Fprintf(b, "%-8s %-5s %4d %9.2f %9.2f %9.2f\n", kbEvalThresholdKey(th), label, len(results), acc, prec, rec)
}

// writeKBEvalJSON schrijft één JSON-object per regel (JSON Lines): de ruwe
// meting, zodat een latere analyse niet van deze tabellen afhangt.
func writeKBEvalJSON(w io.Writer, results []kbEvalResult) error {
	enc := json.NewEncoder(w)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("duif: evaluatieregel schrijven: %w", err)
		}
	}
	return nil
}

// citationMarker vindt de [n]-verwijzingen in een antwoord.
var citationMarker = regexp.MustCompile(`\[([1-9][0-9]*)\]`)

// citationNumbers geeft de genoemde bronnummers in volgorde van voorkomen. Het
// is de controle achter de belofte "geen bron, geen antwoord": elk nummer dat
// de duif noemt hoort bij een bron die de gate werkelijk heeft doorgelaten.
func citationNumbers(s string) []int {
	matches := citationMarker.FindAllStringSubmatch(s, -1)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}
