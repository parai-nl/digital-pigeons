package duif

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Van scherm naar stem. Wat de duif schrijft is opgemaakt voor een
// lezer: citatiemarkeringen, markdown, links. Een voorlezer maakt daar
// "blokhaakje één" van. Dit bestand haalt die laag eraf en knipt de uiting in
// zinnen, want de provider spreekt vloeiender als hij zin voor zin voedt.
//
// Streaming-only blijft ook hier gelden: dit bestand rekent met tekst maar
// bewaart niets en logt niets — het krijgt een uiting binnen en geeft er een
// terug.

var (
	// reLink: [tekst](url) → tekst. Vóór de citaties, anders blijft de url over.
	reLink = regexp.MustCompile(`\[([^\]\n]*)\]\([^)\s]*\)`)
	// reCitation: [1], [2, 3] en (door herhaling) [2][3].
	reCitation = regexp.MustCompile(`\[\d+(?:\s*,\s*\d+)*\]`)
	// reURL: kale adressen; een voorgelezen url is onverstaanbaar.
	reURL = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	// Bronkaarten dragen vindplaatsen; technische graaf-ID's en DOI's zijn
	// geen uitgesproken onderdeel van het antwoord.
	reSourceID = regexp.MustCompile(`(?i)\b(?:destillaat|quote|interview|thema|perspective):[a-z0-9_-]+`)
	reDOI      = regexp.MustCompile(`(?i)\b(?:doi:\s*)?10\.\d{4,9}/[-._;()/:a-z0-9]+`)
	// reFence herkent het begin of einde van een codeblok.
	reFence = regexp.MustCompile("^(?:```|~~~)")
	// reQuote, reHeading en reBullet zijn de regelmarkeringen van markdown.
	reQuote   = regexp.MustCompile(`^(?:>\s*)+`)
	reHeading = regexp.MustCompile(`^#{1,6}\s*`)
	reBullet  = regexp.MustCompile(`^(?:[-*+•]\s+|\d+[.)]\s+)`)
	// reTableRule is de scheidingsregel van een tabel (|---|:--:|).
	reTableRule = regexp.MustCompile(`^\|?[\s:|-]*-[\s:|-]*\|[\s:|-]*$`)
	// reSpaceBeforePunct ruimt de spatie op die achterblijft waar een
	// citatiemarkering weggehaald is ("erbij ." → "erbij.").
	reSpaceBeforePunct = regexp.MustCompile(`\s+([.,;:!?…])`)
)

// stripForSpeech maakt van een geschreven uiting iets dat voorgelezen kan
// worden: markup en verwijzingen eruit, interpunctie erin. Levert "" op als er
// niets uitspreekbaars overblijft.
func stripForSpeech(text string) string {
	if text == "" {
		return ""
	}

	var b strings.Builder
	var inFence bool
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if reFence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence || reTableRule.MatchString(line) {
			continue
		}
		line = reQuote.ReplaceAllString(line, "")
		line = reHeading.ReplaceAllString(line, "")
		line = reBullet.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "|", " ") // tabelcellen worden woorden
		b.WriteString(line)
		b.WriteString("\n")
	}

	out := b.String()
	out = reLink.ReplaceAllString(out, "$1")
	out = reCitation.ReplaceAllString(out, "")
	out = reURL.ReplaceAllString(out, "")
	out = reSourceID.ReplaceAllString(out, "")
	out = reDOI.ReplaceAllString(out, "")
	out = dropDecoration(out)
	return normalizeSpace(out)
}

// dropDecoration haalt de tekens weg die alleen opmaak zijn (nadruk, backticks)
// plus emoji en hun samenvoegtekens. Aanhalingstekens, gedachtestreepjes en
// interpunctie blijven staan: die stuurt de intonatie.
func dropDecoration(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '*', '_', '~', '`':
			return -1
		}
		if isDecoration(r) {
			return -1
		}
		return r
	}, s)
}

// isDecoration herkent emoji, symbolen en de onzichtbare tekens die ze aan
// elkaar plakken.
func isDecoration(r rune) bool {
	switch {
	case r == 0x200D, r == 0xFE0E, r == 0xFE0F: // ZWJ en variatieselectors
		return true
	case r >= 0x1F000: // de emoji-vlakken, inclusief huidskleurtekens
		return true
	case r >= 0x2190 && unicode.Is(unicode.So, r): // pijlen en dingbats
		return true
	default:
		return false
	}
}

// normalizeSpace maakt van alle witruimte één spatie en haalt de spatie weg
// die vóór een leesteken achterbleef.
func normalizeSpace(s string) string {
	out := strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(reSpaceBeforePunct.ReplaceAllString(out, "$1"))
}

const (
	// minSentenceRunes: een losse "Ja." is geen chunk maar een hik; onder deze
	// lengte gaat een fragment mee met de volgende zin.
	minSentenceRunes = 20
	// maxSentenceRunes: boven dit plafond knippen we op een adempauze, zodat
	// de provider niet minutenlang op één blok kauwt.
	maxSentenceRunes = 400
)

// sentenceEnd zijn de tekens die een zin kunnen afsluiten.
var sentenceEnd = map[rune]bool{'.': true, '!': true, '?': true, '…': true}

// closingRunes horen nog bij de zin die eindigt ("… zitten.").
var closingRunes = map[rune]bool{'"': true, '\'': true, '”': true, '’': true, '»': true, ')': true, ']': true}

// openingRunes zijn de tekens waarmee een volgende zin mag beginnen.
var openingRunes = map[rune]bool{'"': true, '\'': true, '„': true, '“': true, '‘': true, '«': true}

// clauseBreaks zijn de adempauzes binnen een te lange zin.
var clauseBreaks = map[rune]bool{';': true, ':': true, ',': true}

// abbreviations zijn de afkortingen waarachter een punt géén zinseinde is
// (Engels en Nederlands; de duif spreekt beide).
var abbreviations = map[string]bool{
	"mr.": true, "mrs.": true, "ms.": true, "dr.": true, "prof.": true, "st.": true,
	"vs.": true, "etc.": true, "e.g.": true, "i.e.": true, "cf.": true, "no.": true,
	"approx.": true,
	"bijv.":   true, "b.v.": true, "o.a.": true, "d.w.z.": true, "e.d.": true,
	"enz.": true, "m.b.t.": true, "n.a.v.": true, "t.a.v.": true, "i.v.m.": true,
	"dhr.": true, "mevr.": true, "drs.": true, "ir.": true, "ing.": true,
	"ca.": true, "nr.": true, "blz.": true, "jl.": true, "a.s.": true, "m.i.": true,
}

// splitSentences knipt een gestripte uiting in zinnen. Een grens is een
// zinsteken, eventueel gevolgd door een sluitend aanhalingsteken, dan
// witruimte, en dan iets dat als nieuwe zin begint — met een afkortingsgids en
// een getallenregel als vangnet.
func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	rs := []rune(text)
	var raw []string
	start := 0
	for _, next := range sentenceCuts(rs) {
		raw = append(raw, strings.TrimSpace(string(rs[start:next])))
		start = next
	}
	if tail := strings.TrimSpace(string(rs[start:])); tail != "" {
		raw = append(raw, tail)
	}

	out := breakLongSentences(mergeShortFragments(raw))
	if len(out) == 0 {
		// Onbereikbaar zolang de staart altijd meegaat; blijft staan omdat het
		// contract is: niet-lege tekst levert altijd minstens één chunk.
		return []string{text}
	}
	return out
}

// sentenceCuts vindt de plekken waar een nieuwe zin begint: de index van het
// eerste teken ná een zinsgrens. Dit is de enige zinsgrensregel; de splitser
// en de streaming-feeder (meeting_speaker.go) lezen allebei hieruit, zodat
// "is deze zin af?" op beide plekken hetzelfde antwoord krijgt.
func sentenceCuts(rs []rune) []int {
	var cuts []int
	start := 0
	for i := 0; i < len(rs); i++ {
		if !sentenceEnd[rs[i]] {
			continue
		}
		end := i + 1
		for end < len(rs) && closingRunes[rs[end]] {
			end++
		}
		// Een citatiemarkering achter het zinsteken ("… beheersen? [3] Sommige")
		// hoort nog bij de zin: de stripper haalt hem weg, maar de grens ligt
		// erachter. Zonder deze stap zag de feeder pas een zinseinde bij de
		// eerste zin zónder citatie — in een goed onderbouwd antwoord kon dat
		// de laatste zijn, en dan begon de stem pas als de tekst af was.
		end = skipCitations(rs, end)
		next := end
		for next < len(rs) && unicode.IsSpace(rs[next]) {
			next++
		}
		switch {
		case next == end || next >= len(rs):
			continue // geen witruimte erachter, of we zijn aan het eind
		case !startsSentence(rs[next]):
			continue // "… wethouder. de zin loopt door"
		case rs[i] == '.' && isAbbreviation(rs[start:i+1]):
			continue // "bijv. Duiven"
		case rs[i] == '.' && i > 0 && unicode.IsDigit(rs[i-1]) && unicode.IsDigit(rs[next]):
			continue // "12. 5 procent"
		}
		cuts = append(cuts, next)
		start = next
		i = next - 1
	}
	return cuts
}

// skipCitations slaat vanaf `from` witruimte plus citatiemarkeringen ([1],
// [2, 3], [4][5]) over en geeft de positie ná de laatste markering terug — of
// `from` zelf als er geen markering staat, zodat witruimte zonder citatie de
// grensregel niet verschuift.
func skipCitations(rs []rune, from int) int {
	end := from
	for {
		j := end
		for j < len(rs) && unicode.IsSpace(rs[j]) {
			j++
		}
		if j >= len(rs) || rs[j] != '[' {
			return end
		}
		k := j + 1
		digits := 0
		for k < len(rs) && (unicode.IsDigit(rs[k]) || rs[k] == ',' || unicode.IsSpace(rs[k])) {
			if unicode.IsDigit(rs[k]) {
				digits++
			}
			k++
		}
		if digits == 0 || k >= len(rs) || rs[k] != ']' {
			return end
		}
		end = k + 1
	}
}

// startsSentence zegt of dit teken het begin van een nieuwe zin kan zijn.
func startsSentence(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsDigit(r) || openingRunes[r]
}

// isAbbreviation kijkt of het laatste woord vóór de punt een afkorting is.
func isAbbreviation(sentence []rune) bool {
	i := len(sentence) - 1
	for i > 0 && !unicode.IsSpace(sentence[i-1]) {
		i--
	}
	word := strings.ToLower(strings.TrimLeft(string(sentence[i:]), `("'„“‘[`))
	return abbreviations[word]
}

// mergeShortFragments plakt losse flarden aan de zin erna, of — als er geen
// zin meer komt — aan die ervoor.
func mergeShortFragments(raw []string) []string {
	var out []string
	var pending string
	for _, s := range raw {
		if pending == "" {
			pending = s
		} else {
			pending += " " + s
		}
		if utf8.RuneCountInString(pending) >= minSentenceRunes {
			out = append(out, pending)
			pending = ""
		}
	}
	if pending == "" {
		return out
	}
	if len(out) == 0 {
		return append(out, pending)
	}
	out[len(out)-1] += " " + pending
	return out
}

// breakLongSentences knipt te lange zinnen op een puntkomma, dubbele punt of
// komma — zo dicht mogelijk vóór het plafond.
func breakLongSentences(sentences []string) []string {
	var out []string
	for _, s := range sentences {
		rs := []rune(s)
		for len(rs) > maxSentenceRunes {
			cut := -1
			for i := maxSentenceRunes; i > 0; i-- {
				if clauseBreaks[rs[i]] {
					cut = i
					break
				}
			}
			if cut < 0 {
				for i := maxSentenceRunes; i < len(rs); i++ {
					if clauseBreaks[rs[i]] {
						cut = i
						break
					}
				}
			}
			if cut < 0 {
				break // geen adempauze te vinden: dan maar in één stuk
			}
			out = append(out, strings.TrimSpace(string(rs[:cut+1])))
			rs = []rune(strings.TrimSpace(string(rs[cut+1:])))
		}
		if len(rs) > 0 {
			out = append(out, string(rs))
		}
	}
	return out
}
