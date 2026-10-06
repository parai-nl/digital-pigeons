package duif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

type bibliographyReader struct {
	*fakeKnowledge
	mu    sync.Mutex
	reads []data.NodeID
	fail  bool
}

func (r *bibliographyReader) GetNode(ctx context.Context, id data.NodeID) (map[string]any, error) {
	if strings.HasPrefix(string(id), "interview:") {
		r.mu.Lock()
		r.reads = append(r.reads, id)
		r.mu.Unlock()
		if r.fail {
			return nil, errors.New("SYNTHETIC_PRIVATE_METADATA_ERROR")
		}
	}
	return r.fakeKnowledge.GetNode(ctx, id)
}

func syntheticBibliography(t *testing.T) *fakeKnowledge {
	f := &fakeKnowledge{}
	seedDestillaat(t, f, kbSeed{id: "1", samenvatting: "Synthetic summary.", score: .9, quote: "Synthetic original observation.", paper: "Synthetic title"})
	f.nodes["quote:1"]["grounded"] = true
	for key, value := range map[string]any{"citatie": "Synthetic Author (1999). A bounded synthetic study.", "doi": "10.1234/synthetic", "citatie_confidence": "medium", "citatie_ontbreekt": "Volume and pages", "storage_key": "SYNTHETIC_PRIVATE_STORAGE", "content_hash": "SYNTHETIC_PRIVATE_HASH", "bron_bestand": "SYNTHETIC_PRIVATE_FILE", "citatie_bron": "SYNTHETIC_PRIVATE_ORIGIN", "key": "SYNTHETIC_PRIVATE_KEY", "categorie": "SYNTHETIC_PRIVATE_CATEGORY"} {
		f.nodes["interview:1"][key] = value
	}
	return f
}

func TestSourceBibliography_RetrievalAndReviewKeepOnlyPublicMetadata(t *testing.T) {
	f := syntheticBibliography(t)
	reader := &bibliographyReader{fakeKnowledge: f}
	got, err := newTestRetrieval(t, reader, nil).run(t.Context(), "Synthetic question")
	if err != nil || got.Status != KBSources || len(got.Sources) != 1 {
		t.Fatalf("retrieval failed: %v", err)
	}
	s := got.Sources[0]
	if s.Ref.ID != "interview:1" || s.Ref.Paper != "Synthetic title" || s.Ref.Citation != "Synthetic Author (1999). A bounded synthetic study." || s.Ref.DOI != "10.1234/synthetic" || s.Ref.CitationConfidence != "medium" || s.Ref.CitationMissing != "Volume and pages" || s.Ref.File != "" {
		t.Fatalf("wrong bibliography: %+v", s.Ref)
	}
	if s.Grounding != groundingVerified || len(s.Ref.Passages) != 1 {
		t.Fatal("citation confidence altered original passage grounding")
	}
	if len(reader.reads) != 1 {
		t.Fatalf("bibliography reads=%d", len(reader.reads))
	}
	wire, _ := json.Marshal(got.Sources)
	for _, output := range []string{string(wire), got.RAGSection} {
		if strings.Contains(output, "SYNTHETIC_PRIVATE_") {
			t.Fatal("private metadata escaped allowlist")
		}
	}
}

func TestSourceBibliography_LegacyAndMetadataOutageKeepGrounding(t *testing.T) {
	for _, kind := range []string{"legacy", "outage", "unavailable_reader"} {
		t.Run(kind, func(t *testing.T) {
			f := syntheticBibliography(t)
			var reader KnowledgeReader = f
			if kind == "legacy" {
				for _, key := range []string{"citatie", "doi", "citatie_confidence", "citatie_ontbreekt"} {
					delete(f.nodes["interview:1"], key)
				}
			}
			if kind == "outage" {
				reader = &bibliographyReader{fakeKnowledge: f, fail: true}
			}
			if kind == "unavailable_reader" {
				reader = tableFailSearcher{KnowledgeReader: f, table: "never"}
			}
			var logs bytes.Buffer
			r := newTestRetrieval(t, reader, nil)
			r.logger = slog.New(slog.NewTextHandler(&logs, nil))
			got, err := r.run(t.Context(), "Synthetic question")
			if err != nil || got.Status != KBSources || len(got.Sources) != 1 {
				t.Fatalf("optional bibliography failure became a corpus gap/outage: %v", err)
			}
			ref := got.Sources[0].Ref
			if ref.Paper != "Synthetic title" || ref.Citation != "" || ref.DOI != "" || ref.CitationMissing != "" || ref.CitationConfidence != "" || len(ref.Passages) != 1 {
				t.Fatal("missing metadata was invented or passages lost")
			}
			if kind != "unavailable_reader" && got.Sources[0].Grounding != groundingVerified {
				t.Fatal("metadata failure changed grounding")
			}
			if strings.Contains(logs.String(), "SYNTHETIC_PRIVATE_") {
				t.Fatal("metadata error logged private data")
			}
		})
	}
}

// De bibliografie leest alleen de gekozen bronnen — nooit meer dan het plafond,
// ook niet als de zoekopdracht er meer aandroeg. Dat plafond ligt op
// maxAnswerSources (vroeger op drie), dus de test rekent met de constante en zet
// er een paar kandidaten boven.
func TestSourceBibliography_ReadsOnlySelectedAndCachesSharedPaper(t *testing.T) {
	f := &fakeKnowledge{}
	for i := 1; i <= maxAnswerSources+3; i++ {
		id := fmt.Sprint(i)
		seedDestillaat(t, f, kbSeed{id: id, samenvatting: "Synthetic " + id, score: 1 - float64(i)/1000, quote: "Synthetic original " + id, paper: "Synthetic paper " + id})
	}
	reader := &bibliographyReader{fakeKnowledge: f}
	r := newTestRetrieval(t, reader, nil)
	got, err := r.run(t.Context(), "Synthetic question")
	if err != nil || len(got.Sources) != maxAnswerSources || len(reader.reads) != maxAnswerSources {
		t.Fatalf("reads were not bounded to the ceiling: sources=%d reads=%d want=%d err=%v", len(got.Sources), len(reader.reads), maxAnswerSources, err)
	}
	for i, id := range reader.reads {
		if id != data.NodeID(fmt.Sprintf("interview:%d", i+1)) {
			t.Fatal("bibliography read an unselected record")
		}
	}
	reader.reads = nil
	selected := []interaction.RAGSource{{Context: &data.DestillaatContext{Interview: &data.Interview{ID: "interview:1", Title: "Synthetic title"}}}, {Context: &data.DestillaatContext{Interview: &data.Interview{ID: "interview:1", Title: "Synthetic title"}}}}
	sources := r.wireSources(nil, selected)
	r.hydrateBibliography(t.Context(), selected, sources)
	if len(reader.reads) != 1 {
		t.Fatal("same paper read twice")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.hydrateBibliography(ctx, selected, sources)
	if len(reader.reads) != 1 {
		t.Fatal("canceled metadata read did work")
	}
}

func TestGraphMetadata_BibliographyExplicitTextAllowlist(t *testing.T) {
	props := syntheticBibliography(t).nodes["interview:1"]
	got := graphNodeOf("interview:1", props)
	if got.Label != "Synthetic title" || got.Metadata["citation"] != props["citatie"] || got.Metadata["doi"] != props["doi"] || got.Metadata["citationConfidence"] != "medium" || got.Metadata["citationMissing"] != "Volume and pages" {
		t.Fatal("missing graph bibliography")
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "SYNTHETIC_PRIVATE_") {
		t.Fatal("private bibliography properties in graph")
	}
	props["citatie"] = map[string]any{"storage_key": "SYNTHETIC_PRIVATE_NESTED"}
	props["citatie_confidence"] = "97%"
	props["citatie_ontbreekt"] = true
	got = graphNodeOf("interview:1", props)
	for _, key := range []string{"citation", "citationConfidence", "citationMissing"} {
		if _, ok := got.Metadata[key]; ok {
			t.Fatalf("invalid bibliography type/value retained: %s", key)
		}
	}
}

// Dit contract is omgedraaid: een v2-bron zonder oorspronkelijke passages
// wordt niet meer weggelaten maar gaat mee als samenvattingsbron. Wat blijft is
// dat zo'n bron zich nooit als gecontroleerd bewijs voordoet — op de kaart staat
// `unverified`, en in de prompt staat het er met zoveel woorden bij.
func TestSourceBibliography_V2SummaryOnlyTravelsAsAnUncheckedSource(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		t.Run(fmt.Sprint(withContext), func(t *testing.T) {
			r := newTestRetrieval(t, &fakeKnowledge{}, nil)
			candidate := interaction.RAGSource{ID: "destillaat:v2", Type: tableDestillaat, Text: "SYNTHETIC_UNSUPPORTED_SUMMARY", Score: .9}
			if withContext {
				candidate.Context = &data.DestillaatContext{Interview: &data.Interview{ID: "interview:1", Title: "Synthetic title"}}
			}
			for _, props := range []map[string]any{{"bundle_schema": "duiven/2"}, {}} {
				st := runGate(t, r, []interaction.RAGSource{candidate}, map[data.NodeID]map[string]any{candidate.ID: props})
				if st.status != KBSources || len(st.sources) != 1 || st.sources[0].Grounding != groundingUnverified {
					t.Fatalf("props %v: samenvattingsbron ontbreekt of doet zich als gecontroleerd voor: %+v", props, st.sources)
				}
				if originalExcerpts(st.sources[0]) != 0 {
					t.Fatalf("props %v: een bron zonder passages kreeg er toch: %+v", props, st.sources[0])
				}
				if !strings.Contains(st.RAGSources[0].Text, "grounding is unverified") {
					t.Fatalf("props %v: de prompt noemt de bron niet ongecontroleerd: %q", props, st.RAGSources[0].Text)
				}
			}
		})
	}
	for _, mixed := range []bool{false, true} {
		t.Run("grounded_false_mixed_"+fmt.Sprint(mixed), func(t *testing.T) {
			f := syntheticBibliography(t)
			f.nodes["destillaat:1"]["bundle_schema"] = "duiven/2"
			f.nodes["quote:1"]["grounded"] = false
			if mixed {
				if _, err := f.CreateNodeWithID(t.Context(), "quote:valid", map[string]any{"text": "A separate checked original passage.", "grounded": true}, ""); err != nil {
					t.Fatal(err)
				}
				if err := f.Relate(t.Context(), "destillaat:1", "quote:valid", data.RelHasQuote, nil); err != nil {
					t.Fatal(err)
				}
			}
			got, err := newTestRetrieval(t, f, nil).run(t.Context(), "Synthetic question")
			if err != nil {
				t.Fatal(err)
			}
			if !mixed {
				// Alleen afgekeurde passages: de bron blijft, de passages niet.
				if got.Status != KBSources || len(got.Sources) != 1 || got.Sources[0].Grounding != groundingRejected {
					t.Fatalf("v2 false-only bron viel weg in plaats van door te gaan zonder passages: %+v", got.Sources)
				}
				if len(got.Sources[0].Ref.Passages) != 0 || strings.Contains(got.RAGSection, "SYNTHETIC_PRIVATE_") {
					t.Fatal("v2 false-only evidence survived")
				}
				return
			}
			if got.Status != KBSources || len(got.Sources) != 1 || got.Sources[0].Grounding != groundingVerified || len(got.Sources[0].Ref.Passages) != 1 || got.Sources[0].Ref.Passages[0].ID != "quote:valid" {
				t.Fatal("mixed source must contain only checked passage")
			}
			if strings.Contains(got.RAGSection, "Synthetic original observation.") {
				t.Fatal("rejected original entered generator prompt")
			}
		})
	}
}

func TestSourceBibliography_CitationAloneCannotGroundResearch(t *testing.T) {
	source := Source{Number: 1, Ref: SourceRef{Paper: "Synthetic title", Citation: "Synthetic Author (1999). A claim-shaped title."}}
	if originalExcerpts(source) != 0 {
		t.Fatal("bibliography became a research passage")
	}
	// De verwijzing zelf blijft wel staan: zij is context op de bronkaart,
	// nooit iets waar een claim op kan leunen.
	if source.Ref.Citation == "" || source.Ref.Quote != "" {
		t.Fatalf("bibliografische context veranderde: %+v", source.Ref)
	}
}
