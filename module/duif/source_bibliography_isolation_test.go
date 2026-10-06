package duif

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

func TestBibliography_CrossCorpusCannotDonateMetadata(t *testing.T) {
	f := syntheticBibliography(t)
	f.nodes["interview:1"]["kb_id"] = "other-corpus"
	f.nodes["interview:1"]["title"] = "FOREIGN_PRIVATE_TITLE"
	f.nodes["interview:1"]["citatie"] = "FOREIGN_PRIVATE_REFERENCE"
	r := newTestRetrieval(t, f, nil)
	r.status = func() importStatus { return importStatus{State: ImportReady, KBID: "active-corpus"} }
	selected := []interaction.RAGSource{{Context: &data.DestillaatContext{Interview: &data.Interview{ID: "interview:1", Title: "Already selected source"}, Quotes: []data.Quote{{ID: "quote:1", Text: "Already selected original passage."}}}}}
	sources := r.wireSources(nil, selected)
	r.hydrateBibliography(t.Context(), selected, sources)
	wire, err := json.Marshal(sources)
	if err != nil || strings.Contains(string(wire), "FOREIGN_PRIVATE") {
		t.Fatal("other corpus donated bibliography")
	}
	if sources[0].Ref.Paper != "Already selected source" || sources[0].Ref.Citation != "" || len(sources[0].Ref.Passages) != 1 {
		t.Fatal("optional metadata changed the selected original evidence")
	}
	// Control: the exact same optional record is used when it belongs to the
	// active corpus, proving this test exercised the corpus gate rather than
	// a reader incapable of hydration.
	f.nodes["interview:1"]["kb_id"] = "active-corpus"
	r.hydrateBibliography(t.Context(), selected, sources)
	if sources[0].Ref.Citation != "FOREIGN_PRIVATE_REFERENCE" {
		t.Fatal("positive hydration control failed")
	}
}

// Een afgekeurde gronding laat de bron niet meer vallen, maar de
// blanco passage wordt nog steeds nooit bewijs: de bron gaat verder als
// samenvattingsbron, zonder passages en met het oordeel op de kaart.
func TestBibliography_V2WhitespaceIsNotOriginalEvidence(t *testing.T) {
	f := syntheticBibliography(t)
	f.nodes["destillaat:1"]["bundle_schema"] = "duiven/2"
	f.nodes["quote:1"]["text"] = " \t\n\u2003 "
	f.nodes["quote:1"]["grounded"] = true
	got, err := newTestRetrieval(t, f, nil).run(t.Context(), "Synthetic question")
	if err != nil || got.Status != KBSources || len(got.Sources) != 1 {
		t.Fatalf("de bron viel weg in plaats van door te gaan als samenvatting: %+v / %v", got, err)
	}
	src := got.Sources[0]
	if src.Grounding != groundingRejected || len(src.Ref.Passages) != 0 || src.Ref.Quote != "" {
		t.Fatalf("a true annotation promoted a blank original passage: %+v", src)
	}
	if !strings.Contains(got.RAGSection, "could not be traced back") {
		t.Fatalf("de prompt zegt niet dat deze samenvatting ongecontroleerd is: %s", got.RAGSection)
	}
}
