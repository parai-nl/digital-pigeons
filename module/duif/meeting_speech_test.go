package duif

import (
	"strings"
	"testing"
)

// Wat de duif zegt is geschreven voor een scherm; wat zij uitspreekt moet
// klinken als spraak. De stripper haalt alles weg wat een voorlezer zou
// oplezen als ruis, en laat interpunctie staan.
func TestStripForSpeech_RemovesMarkupButKeepsProse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"citations in all three shapes",
			"Duiven horen erbij [1]. En dat blijft zo [2, 3][4].",
			"Duiven horen erbij. En dat blijft zo."},
		{"headings",
			"## Kop van de duif\nDe duif spreekt.",
			"Kop van de duif De duif spreekt."},
		{"emphasis and inline code",
			"Dit is **vet** en *cursief* en `code`.",
			"Dit is vet en cursief en code."},
		{"markdown link keeps its text",
			"Zie [de nota](https://example.org/nota) maar.",
			"Zie de nota maar."},
		{"bare url disappears",
			"Kijk op https://example.org/x voor meer.",
			"Kijk op voor meer."},
		{"code fence disappears entirely",
			"Voor de duidelijkheid:\n```go\nfmt.Println(\"koer\")\n```\nEinde.",
			"Voor de duidelijkheid: Einde."},
		{"list markers",
			"- eerste punt\n- tweede punt",
			"eerste punt tweede punt"},
		{"table becomes plain words",
			"| plein | duiven |\n|---|---|\n| Grote Markt | veel |",
			"plein duiven Grote Markt veel"},
		{"blockquote",
			"> de raad zei dit",
			"de raad zei dit"},
		{"emoji disappears",
			"Hoi 🐦 daar 👋🏽.",
			"Hoi daar."},
		{"dutch quotes, dashes and ellipsis survive",
			"„Wij blijven\" — zei de duif, en toen…",
			"„Wij blijven\" — zei de duif, en toen…"},
		{"arrows and joined emoji disappear too",
			"De duif ➜ het plein 👨‍👩‍👧 en verder ❤️.",
			"De duif het plein en verder."},
		{"nothing but markup leaves nothing",
			"[1] **__**",
			""},
		{"empty stays empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripForSpeech(tc.in); got != tc.want {
				t.Errorf("stripForSpeech(%q)\n = %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// De provider wil zinnen: kort genoeg om vlot te beginnen, heel genoeg om
// natuurlijk te klinken.
func TestSplitSentences_SplitsOnRealSentenceBoundaries(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"plain sentences",
			"De duiven zitten op het plein en kijken toe. Ze wachten op een besluit van de raad. Komt dat besluit er eigenlijk wel?",
			[]string{
				"De duiven zitten op het plein en kijken toe.",
				"Ze wachten op een besluit van de raad.",
				"Komt dat besluit er eigenlijk wel?",
			}},
		{"dutch abbreviation does not end a sentence",
			"Wij bespreken bijv. Duiven op het plein en de raad daarover. Daarna sluiten wij de vergadering.",
			[]string{
				"Wij bespreken bijv. Duiven op het plein en de raad daarover.",
				"Daarna sluiten wij de vergadering.",
			}},
		{"english abbreviation does not end a sentence",
			"Please ask Mr. Jansen about the pigeons on the square. He will answer you later today.",
			[]string{
				"Please ask Mr. Jansen about the pigeons on the square.",
				"He will answer you later today.",
			}},
		{"decimals stay together",
			"De drempel staat op 12. 5 procent en dat is genoeg voor nu. Daarmee is de zaak afgerond.",
			[]string{
				"De drempel staat op 12. 5 procent en dat is genoeg voor nu.",
				"Daarmee is de zaak afgerond.",
			}},
		{"ellipsis ends a sentence",
			"De duif zweeg toen heel erg lang… Daarna zei zij eindelijk iets terug.",
			[]string{
				"De duif zweeg toen heel erg lang…",
				"Daarna zei zij eindelijk iets terug.",
			}},
		{"closing quote travels with the sentence",
			"„Wij blijven op het plein zitten.\" Daarna vloog de duif toch weg.",
			[]string{
				"„Wij blijven op het plein zitten.\"",
				"Daarna vloog de duif toch weg.",
			}},
		{"lowercase after a dot is no boundary",
			"De duif keek op naar de wethouder. de zin loopt gewoon door hier.",
			[]string{"De duif keek op naar de wethouder. de zin loopt gewoon door hier."}},
		{"short fragment merges into the next sentence",
			"Ja. De duiven blijven zitten waar zij nu zitten op het plein.",
			[]string{"Ja. De duiven blijven zitten waar zij nu zitten op het plein."}},
		{"a very short utterance stays as it is",
			"Ja.",
			[]string{"Ja."}},
		{"a trailing fragment joins the sentence before it",
			"De duiven blijven zitten waar zij zitten. Ja.",
			[]string{"De duiven blijven zitten waar zij zitten. Ja."}},
		{"text without punctuation stays whole",
			"geen leesteken hier maar toch een uiting",
			[]string{"geen leesteken hier maar toch een uiting"}},
		{"empty text yields nothing", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSentences(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitSentences(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("zin %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Een zin die maar door blijft gaan wordt op een adempauze geknipt: de
// provider krijgt nooit een blok van duizend tekens in één keer.
func TestSplitSentences_BreaksVeryLongSentencesAtClauses(t *testing.T) {
	long := "De duiven van het plein blijven zitten waar zij zitten" +
		strings.Repeat(", en de raad vergadert daar rustig verder over", 12) + "."
	got := splitSentences(long)
	if len(got) < 2 {
		t.Fatalf("een zin van %d tekens hoort geknipt te worden, kreeg %d deel(en)", len([]rune(long)), len(got))
	}
	for i, zin := range got {
		if n := len([]rune(zin)); n > 400 {
			t.Errorf("deel %d is %d tekens", i, n)
		}
	}
	if joined := strings.Join(got, " "); joined != long {
		t.Errorf("de tekst raakte onderweg iets kwijt:\n%q\n%q", joined, long)
	}
}

// Een zin zonder enige adempauze blijft heel: knippen midden in een woord is
// erger dan een lang chunk.
func TestSplitSentences_LongSentenceWithoutClauseBreaks(t *testing.T) {
	long := strings.Repeat("duiven ", 90) + "einde."
	got := splitSentences(long)
	if len(got) != 1 {
		t.Fatalf("zonder adempauze hoort de zin heel te blijven, kreeg %d delen", len(got))
	}
}

// Ligt de enige adempauze voorbij het plafond, dan knippen we daar — later dan
// gewenst, maar nog altijd op een grens.
func TestSplitSentences_ClauseBreakAfterTheCeiling(t *testing.T) {
	long := strings.Repeat("duiven ", 70) + ", en daarna de raad."
	got := splitSentences(long)
	if len(got) != 2 {
		t.Fatalf("delen = %d, want 2: %q", len(got), got)
	}
	if !strings.HasSuffix(got[0], ",") {
		t.Errorf("het eerste deel hoort op de adempauze te eindigen: %q", got[0])
	}
}
