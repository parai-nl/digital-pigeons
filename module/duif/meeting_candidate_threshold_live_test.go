//go:build live

package duif

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// De kalibratie van meetingMinSimilarity, en daarmee ook van
// relatedMinSimilarity: beide latten liggen op 0,65 en leunen
// op dezelfde grens — waar houdt aangrenzend materiaal op en begint willekeur?
// De twee constanten bestaan om verschillende redenen (retrieval.go), maar
// schuift die grens, dan is dit de meting die dat als eerste laat zien.
//
// De kolom >=0,73 zegt wat de chatlat de tafel zou kosten. Om dat getal hebben
// chat en vergadertafel geen gedeelde drempel meer.
//
// Hij draait niet in de gewone suite: buildtag `live`, en zonder database of
// proxy slaat hij zichzelf over. Wat hij meet is smal en goedkoop: per venster
// van de vier gespreksfixtures één embed-aanroep en één vectorzoekopdracht,
// precies het getal waar de drempel op werkt. Er komt geen taalmodel aan te pas
// en er wordt niets geschreven.
//
// De vraag die hij beantwoordt: bij welke lat ziet de kennispartner aan de
// vergadertafel het aangrenzende materiaal — en bij welke lat begint het
// negatieve gesprek (`control`, geen duif in zicht) er ook bronnen bij te
// halen? De eerste waarde moet zo hoog mogelijk zijn zonder dat de tweede
// meeloopt.
//
//	go test -tags "dev live" -run TestLive_MeetingCandidateThreshold -v ./duif/
func TestLive_MeetingCandidateThreshold(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	rig := newKBEvalRig(ctx, t)
	rig.requireImportedKB(ctx, t)
	fixtures := loadConversationFixtures(t)

	// Dezelfde vensterbreedte als de sessie: de laatste twaalf segmenten binnen
	// het tijdvenster. De fixtures zijn korter dan twaalf, dus dit is in de
	// praktijk "alles tot hier".
	const window = 12
	thresholds := []float32{0.55, 0.60, 0.65, 0.73}

	type tally struct {
		windows int
		passing map[float32]int
		best    []float32
	}
	byConv := map[string]*tally{}
	order := []string{}

	for _, conv := range fixtures.Conversations {
		// Per taal apart: het corpus is Engelstalig, dus een Nederlands venster
		// vraagt de embedder om een kruistalige match. Of dat de scores drukt is
		// precies wat deze meting moet uitwijzen, niet iets om aan te nemen.
		key := conv.ID + "/" + conv.Lang
		agg, ok := byConv[key]
		if !ok {
			agg = &tally{passing: map[float32]int{}}
			byConv[key] = agg
			order = append(order, key)
		}
		for i := range conv.Segments {
			from := max(0, i+1-window)
			segs := make([]TranscriptSegment, 0, i+1-from)
			for _, seg := range conv.Segments[from : i+1] {
				segs = append(segs, TranscriptSegment{Text: seg.Text, Speaker: seg.Speaker})
			}
			query := truncateQuery(transcriptQuery(segs))
			vec, err := rig.provider.Embed(ctx, query)
			if err != nil {
				t.Fatalf("%s venster %d embedden: %v", conv.ID, i, err)
			}
			hits, err := rig.knowledge.SemanticSearch(ctx, tableDestillaat, vec, retrieveTopK)
			if err != nil {
				t.Fatalf("%s venster %d zoeken: %v", conv.ID, i, err)
			}
			agg.windows++
			var top float32
			for _, h := range hits {
				if h.Score > top {
					top = h.Score
				}
				for _, lat := range thresholds {
					if h.Score >= lat {
						agg.passing[lat]++
					}
				}
			}
			agg.best = append(agg.best, top)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-15s %8s %11s %8s", "gesprek", "vensters", "top-mediaan", "top-max")
	for _, lat := range thresholds {
		fmt.Fprintf(&b, "  >=%.2f", lat)
	}
	for _, id := range order {
		agg := byConv[id]
		sorted := append([]float32(nil), agg.best...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		median, top := float32(0), float32(0)
		if n := len(sorted); n > 0 {
			median, top = sorted[n/2], sorted[n-1]
		}
		fmt.Fprintf(&b, "\n%-15s %8d %11.3f %8.3f", id, agg.windows, median, top)
		for _, lat := range thresholds {
			fmt.Fprintf(&b, "  %6d", agg.passing[lat])
		}
	}
	t.Logf("bronnen per venster over de vier fixtures (top-%d, embedder %s)\n%s",
		retrieveTopK, rig.embeddingModel, b.String())

	// De meting is pas een motivatie als zij de twee kanten laat zien: het
	// aangrenzende materiaal komt door, de negatieve controle niet.
	for _, lang := range []string{"en", "nl"} {
		// `disease` staat erbij omdat de kennispartner daar bronnen hoort te
		// vinden — het corpus dekt het aantoonbaar (kb-eval-vraag kb-nl-ziekten
		// scoort 0,86).
		for _, gesprek := range []string{"fireworks", "disease"} {
			if agg, ok := byConv[gesprek+"/"+lang]; ok && agg.passing[meetingMinSimilarity] == 0 {
				t.Errorf("bij %.2f haalt %s/%s geen enkele bron; dan zwijgt de kennispartner daar nog steeds",
					meetingMinSimilarity, gesprek, lang)
			}
		}
		// Bekende stand op de duiven/3-bundel:
		// `control/en` haalt er hier één bron bij op 240 vensterkandidaten, en
		// deze toets is dus rood. Bewust niet verzacht — of één op 240 nog "leeg
		// genoeg" is, is een kalibratiebeslissing over de lat van 0,65, niet iets
		// om met een marge stilletjes in de assertie weg te werken.
		if agg, ok := byConv["control/"+lang]; ok && agg.passing[meetingMinSimilarity] > 0 {
			t.Errorf("bij %.2f haalt de negatieve controle (%s) %d bron(nen); de lat staat te laag",
				meetingMinSimilarity, lang, agg.passing[meetingMinSimilarity])
		}
	}
}
