package duif

import (
	"context"
	"strings"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

// Fetch only the already selected source records, once per interview ID and at
// most maxAnswerSources per retrieval. This optional read supplies bibliographic
// data, never extra evidence or a reason to upgrade quote grounding.
//
// Dat plafond staat op twintig (vroeger drie). De cache per
// interview-ID doet daar het meeste werk: meerdere standpunten uit dezelfde
// paper delen één read, en dat is bij twintig bronnen eerder regel dan
// uitzondering.
func (r *retrieval) hydrateBibliography(ctx context.Context, selected []interaction.RAGSource, sources []Source) {
	reader, ok := r.reader.(quoteMetadataReader)
	if !ok {
		return
	}
	cache := map[data.NodeID]map[string]any{}
	for i, src := range selected[:min(len(selected), len(sources), maxAnswerSources)] {
		if ctx.Err() != nil {
			return
		}
		if src.Context == nil || src.Context.Interview == nil || src.Context.Interview.ID == "" {
			continue
		}
		id := src.Context.Interview.ID
		props, seen := cache[id]
		if !seen {
			var err error
			props, err = reader.GetNode(ctx, id)
			if err != nil {
				r.log().Warn("source bibliography unavailable", "component", "duif", "reason", "source_bibliography_unavailable")
				props = nil
			}
			// A cross-corpus record must not donate metadata to the active source.
			if kb := r.activeKB(); kb != "" {
				if got, _ := props["kb_id"].(string); got != kb {
					props = nil
				}
			}
			cache[id] = props
		}
		if props == nil {
			continue
		}
		ref := &sources[i].Ref
		if title := bibliographyText(props["title"]); title != "" {
			ref.Paper = title
		}
		ref.Citation = bibliographyText(props["citatie"])
		ref.DOI = bibliographyText(props["doi"])
		ref.CitationMissing = bibliographyText(props["citatie_ontbreekt"])
		ref.CitationConfidence = bibliographyConfidence(props["citatie_confidence"])
	}
}

func bibliographyText(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }
func bibliographyConfidence(v any) string {
	s := bibliographyText(v)
	switch s {
	case "high", "medium", "low":
		return s
	}
	return ""
}

// hydratePerspective haalt de vertaalde perspectiefnaam op. De naam staat niet
// in de props van het standpunt zelf — het standpunt wijst er alleen naar — en
// core's getypeerde read laat de zijspoorprop vallen. Eén gerichte lezing per
// perspectief per beurt, hooguit maxAnswerSources bronnen: een perspectiefknoop
// draagt geen embedding, dus dit is een kleine rij. Perspectieven zijn bovendien
// breed — bij twintig bronnen delen er veel hetzelfde, en de cache leest elk
// perspectief één keer.
//
// Zonder deze lezing zou een Engelse lezer een Nederlandse perspectiefnaam op
// zijn bronkaart zien terwijl de rest van de kaart Engels is.
func (r *retrieval) hydratePerspective(ctx context.Context, selected []interaction.RAGSource, sources []Source) {
	reader, ok := r.reader.(quoteMetadataReader)
	if !ok {
		return
	}
	cache := map[data.NodeID]Bilingual{}
	for i, src := range selected[:min(len(selected), len(sources), maxAnswerSources)] {
		if ctx.Err() != nil {
			return
		}
		if src.Context == nil || src.Context.Perspective == nil || src.Context.Perspective.ID == "" {
			continue
		}
		id := src.Context.Perspective.ID
		naam, seen := cache[id]
		if !seen {
			props, err := reader.GetNode(ctx, id)
			if err != nil {
				r.log().Warn("perspective name unavailable", "component", "duif", "reason", "source_perspective_unavailable")
				props = nil
			}
			// Een perspectief uit een vórige kennisbank mag geen naam doneren
			// aan de actieve bron; dezelfde regel als bij de bibliografie.
			if kb := r.activeKB(); kb != "" {
				if got, _ := props["kb_id"].(string); got != kb {
					props = nil
				}
			}
			naam = bilingualProp(props, "naam")
			cache[id] = naam
		}
		if naam == nil {
			continue
		}
		if sources[i].Localized == nil {
			sources[i].Localized = &SourceLocalized{}
		}
		sources[i].Localized.Perspective = naam
	}
}
