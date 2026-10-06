//go:build live

package duif

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

// TestLive_Probe is de handsonde naast make kb-eval: de scores van losse
// vragen tegen de geïmporteerde kennisbank, en een greep uit de standpunten
// die een trefwoord bevatten. Zo is "waarom kreeg deze vraag een gat?" in
// één run te beantwoorden:
//
//	set -a; source .env; set +a
//	DUIF_PROBE="hoe nestelen duiven?;Hoe bouwen duiven hun nest?" DUIF_PROBE_KEYWORD=nest \
//	  go test -tags live -run TestLive_Probe -v ./duif
func TestLive_Probe(t *testing.T) {
	queries := strings.Split(os.Getenv("DUIF_PROBE"), ";")
	if len(queries) == 0 || queries[0] == "" {
		t.Skip("DUIF_PROBE=vraag;vraag")
	}
	ctx, cancel := context.WithTimeout(t.Context(), kbEvalTimeout)
	defer cancel()
	rig := newKBEvalRig(ctx, t)
	rig.requireImportedKB(ctx, t)

	if kw := os.Getenv("DUIF_PROBE_KEYWORD"); kw != "" {
		all, err := rig.knowledge.ListDestillaten(ctx, data.ListOpts{Unbounded: true})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, d := range all {
			if strings.Contains(strings.ToLower(d.Samenvatting), strings.ToLower(kw)) {
				n++
				if n <= 8 {
					fmt.Printf("  KW %-30s | %s\n", strings.Join(d.PrimaryThemes, ","), clipRunes(d.Samenvatting, 120))
				}
			}
		}
		fmt.Printf("standpunten met %q: %d van %d\n", kw, n, len(all))
	}
	for _, q := range queries {
		vec, err := rig.provider.Embed(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		hits, err := rig.knowledge.SemanticSearch(ctx, tableDestillaat, vec, 8)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("\nVRAAG %q\n", q)
		for _, h := range hits {
			sam, _ := h.Node["samenvatting"].(string)
			fmt.Printf("  %.3f | %s\n", h.Score, clipRunes(sam, 110))
		}
	}
}
