package duif

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

type boundaryContextReader struct {
	*fakeKnowledge
	nilContext bool
}

func (b boundaryContextReader) GetWithContext(ctx context.Context, id data.NodeID) (*data.DestillaatContext, error) {
	if id == "destillaat:bad" {
		if b.nilContext {
			return nil, nil
		}
		return nil, errors.New("synthetic evidence outage")
	}
	return b.fakeKnowledge.GetWithContext(ctx, id)
}

func TestGroundingBoundary_ContextOutageAndNilCannotBecomeLegacyEvidence(t *testing.T) {
	for _, nilContext := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			t.Run(map[bool]string{true: "nil", false: "error"}[nilContext]+map[bool]string{true: "_partial", false: "_all"}[partial], func(t *testing.T) {
				kg := &fakeKnowledge{}
				seedDestillaat(t, kg, kbSeed{id: "bad", samenvatting: "Rejected summary sentinel.", quote: "Rejected quote.", score: .99, paper: "synthetic-bad"})
				kg.nodes["quote:bad"]["grounded"] = false
				if partial {
					seedDestillaat(t, kg, kbSeed{id: "good", samenvatting: "An editorial summary.", quote: "A qualified original passage.", score: .95, paper: "synthetic-good"})
					kg.nodes["quote:good"]["grounded"] = true
				}
				res, err := newTestRetrieval(t, boundaryContextReader{kg, nilContext}, nil).run(t.Context(), "Synthetic question")
				if !partial {
					if !errors.Is(err, errKBUnavailable) || len(res.Sources) != 0 {
						t.Fatalf("outage=%+v/%v", res, err)
					}
				} else if err != nil || res.Status != KBSources || len(res.Sources) != 1 || res.Sources[0].ID != "destillaat:good" || res.Sources[0].Number != 1 || res.Sources[0].Grounding != groundingVerified {
					t.Fatalf("partial=%+v/%v", res, err)
				}
				if strings.Contains(res.RAGSection, "Rejected") {
					t.Fatal("failed evidence candidate leaked")
				}
			})
		}
	}
}

func TestGroundingBoundary_ReadableLegacyWithoutQuotesStaysExplicitlyUnverified(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "legacy", samenvatting: "A readable old extraction.", score: .95, paper: "synthetic-legacy"})
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "Legacy question")
	if err != nil || len(res.Sources) != 1 || res.Sources[0].Grounding != groundingUnverified || !strings.Contains(res.RAGSection, "grounding is unverified") {
		t.Fatalf("legacy=%+v/%v", res, err)
	}
}
