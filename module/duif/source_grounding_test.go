package duif

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

// De afgekeurde passage haalt de prompt niet — alleen
// de bron eromheen blijft staan, als samenvattingsbron áchter de bron die haar
// eigen woorden wél kan laten zien, en met de afkeuring in de prompt en op de
// kaart.
func TestSourceGrounding_ExplicitFalseNeverReachesCitationsOrPrompt(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "bad", samenvatting: "Pigeons always choose a measured height.", quote: "An unchecked extraction.", score: .95, paper: "synthetic-unchecked"})
	seedDestillaat(t, kg, kbSeed{id: "good", samenvatting: "A possible association in one study.", quote: "In this study, pigeons might use the available structures.", score: .9, paper: "synthetic-checked"})
	kg.nodes["quote:bad"]["grounded"] = false
	kg.nodes["quote:good"]["grounded"] = true
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "What does this study suggest?")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("sources=%+v", res.Sources)
	}
	if res.Sources[0].ID != "destillaat:good" || res.Sources[0].Number != 1 || res.Sources[0].Grounding != groundingVerified {
		t.Fatalf("de gecontroleerde bron staat niet vooraan: %+v", res.Sources)
	}
	if res.Sources[1].ID != "destillaat:bad" || res.Sources[1].Grounding != groundingRejected || len(res.Sources[1].Ref.Passages) != 0 {
		t.Fatalf("de afgekeurde bron behield haar passages of haar oordeel niet: %+v", res.Sources[1])
	}
	if strings.Contains(res.RAGSection, "unchecked extraction") {
		t.Fatal("unchecked passage leaked into model evidence")
	}
	if !strings.Contains(res.RAGSection, "could not be traced back") {
		t.Fatal("de prompt waarschuwt niet dat deze samenvatting ongecontroleerd is")
	}
	if !strings.Contains(res.RAGSection, "might use") {
		t.Fatal("qualified original passage lost")
	}
}

func TestSourceGrounding_LegacyMissingFlagRemainsExplicitlyUnverified(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "legacy", samenvatting: "A legacy extraction.", quote: "An older supplied passage.", score: .95, paper: "synthetic-legacy"})
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "What is supported?")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 1 || res.Sources[0].Grounding != groundingUnverified || !strings.Contains(res.RAGSection, "grounding is unverified") {
		t.Fatalf("legacy silently rejected or certified: %+v", res)
	}
}

type brokenGroundingReader struct{ *fakeKnowledge }

func (b brokenGroundingReader) GetNode(context.Context, data.NodeID) (map[string]any, error) {
	return nil, errors.New("private failure must not enter output")
}
func TestSourceGrounding_MetadataOutageIsNotAKnowledgeGap(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "a", samenvatting: "A claim.", quote: "A passage.", score: .9, paper: "synthetic"})
	_, err := newTestRetrieval(t, brokenGroundingReader{kg}, nil).run(t.Context(), "A question")
	if !errors.Is(err, errKBUnavailable) {
		t.Fatalf("metadata outage=%v", err)
	}
}

func TestSourceGrounding_RAGRetainsScopeAndModalEvidenceContract(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "scope", samenvatting: "All pigeons avoid water.", quote: "In the two areas studied, they will probably nest nearer built space.", score: .9, paper: "synthetic-scope"})
	kg.nodes["quote:scope"]["grounded"] = true
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "Can that be generalized?")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.RAGSection, "All pigeons avoid water.") {
		t.Fatal("editorial overstatement competes with the original passage in model evidence")
	}
	if len(res.Sources) != 1 || res.Sources[0].Text != "All pigeons avoid water." {
		t.Fatal("the original source card was rewritten instead of only projecting model evidence")
	}
	// This asserts the actual pipeline carries both the evidence and the rule.
	// It does not certify a real model's subsequent wording.
	for _, required := range []string{"probably nest", "narrower", "study-local prediction", "not a universal"} {
		if !strings.Contains(res.RAGSection, required) {
			t.Fatalf("missing actual evidence/rule %s", required)
		}
	}
}

func TestSourceGrounding_MixedQuotesOnlyExposeVerifiedPassages(t *testing.T) {
	kg := &fakeKnowledge{}
	did := seedDestillaat(t, kg, kbSeed{id: "mixed", samenvatting: "One limited finding.", quote: "A checked possible association.", score: .95, paper: "synthetic"})
	kg.nodes["quote:mixed"]["grounded"] = true
	badID := data.NodeID("quote:rejected")
	if _, err := kg.CreateNodeWithID(t.Context(), badID, map[string]any{"text": "Secret rejected passage.", "grounded": false}, ""); err != nil {
		t.Fatal(err)
	}
	if err := kg.Relate(t.Context(), did, badID, data.RelHasQuote, nil); err != nil {
		t.Fatal(err)
	}
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "What can this finding support?")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 1 || res.Sources[0].Grounding != groundingVerified {
		t.Fatalf("sources=%+v", res.Sources)
	}
	if strings.Contains(res.RAGSection, "Secret rejected") {
		t.Fatal("mixed source leaked rejected quote")
	}
}

// Een afgekeurde of misvormde vlag maakt van een passage nooit bewijs. Het
// antwoord daarop is geen kennisgat meer maar een samenvattingsbron:
// de lezer ziet de bron, de duif krijgt de passages niet.
func TestSourceGrounding_AllFalseBecomesASummarySourceAndMalformedFlagIsNotVerified(t *testing.T) {
	for _, flag := range []any{false, "true", 1} {
		kg := &fakeKnowledge{}
		seedDestillaat(t, kg, kbSeed{id: "bad", samenvatting: "Unsupported extraction.", quote: "An unverified passage.", score: .95, paper: "synthetic"})
		kg.nodes["quote:bad"]["grounded"] = flag
		res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "What is actually checked?")
		if err != nil || res.Status != KBSources || len(res.Sources) != 1 {
			t.Fatalf("flag %v: %+v / %v", flag, res, err)
		}
		if res.Sources[0].Grounding != groundingRejected || len(res.Sources[0].Ref.Passages) != 0 {
			t.Fatalf("flag %v: passage werd alsnog bewijs: %+v", flag, res.Sources[0])
		}
		if strings.Contains(res.RAGSection, "An unverified passage") {
			t.Fatalf("flag %v: afgekeurde passage lekte in de prompt", flag)
		}
	}
}

func TestSourceGrounding_EveryModelPassageIsVisibleInTheSourceEvidence(t *testing.T) {
	kg := &fakeKnowledge{}
	id := seedDestillaat(t, kg, kbSeed{id: "two", samenvatting: "Editorial interpretation.", quote: "First limited observation.", score: .95, paper: "synthetic-two"})
	kg.nodes["quote:two"]["grounded"] = true
	second := data.NodeID("quote:two-more")
	_, err := kg.CreateNodeWithID(t.Context(), second, map[string]any{"text": "Second observation might differ.", "grounded": true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := kg.Relate(t.Context(), id, second, data.RelHasQuote, nil); err != nil {
		t.Fatal(err)
	}
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "Compare the evidence")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result=%+v / %v", res, err)
	}
	passages := res.Sources[0].Ref.Passages
	if len(passages) != 2 {
		t.Fatalf("missing source evidence: %+v", passages)
	}
	for _, p := range passages {
		if p.ID == "" || !strings.Contains(res.RAGSection, p.Text) {
			t.Fatalf("source/model evidence mismatch: %+v", p)
		}
	}
	if strings.Contains(res.RAGSection, "Editorial interpretation.") {
		t.Fatal("editorial text used as original evidence")
	}
}

func TestSourceGrounding_EmptyVerifiedPassageCannotCertifyAnExtraction(t *testing.T) {
	kg := &fakeKnowledge{}
	seedDestillaat(t, kg, kbSeed{id: "empty", samenvatting: "A claim without readable evidence.", quote: " ", score: .95, paper: "synthetic"})
	kg.nodes["quote:empty"]["grounded"] = true
	res, err := newTestRetrieval(t, kg, nil).run(t.Context(), "What supports this?")
	if err != nil || res.Status != KBSources || len(res.Sources) != 1 {
		t.Fatalf("de bron viel weg in plaats van door te gaan als samenvatting: %+v / %v", res, err)
	}
	if res.Sources[0].Grounding != groundingRejected || len(res.Sources[0].Ref.Passages) != 0 {
		t.Fatalf("empty passage certified: %+v", res.Sources[0])
	}
}
