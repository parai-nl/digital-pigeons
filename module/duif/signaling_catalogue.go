package duif

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"
)

const reasonSignalResearchUnavailable = "signal_research_unavailable"

type signalRelation struct {
	TopicID   string `yaml:"topic_id"`
	SourceID  string `yaml:"source_id"`
	SourceKey string `yaml:"source_key"`
	FindingID string `yaml:"finding_id"`
	QuoteID   string `yaml:"quote_id"`
	Grounded  bool   `yaml:"grounded"`
	Finding   string `yaml:"finding"`
	Rationale string `yaml:"rationale"`
	Boundary  string `yaml:"boundary"`
}
type signalCatalogue struct {
	Version            string           `yaml:"version"`
	ReviewBasis        string           `yaml:"review_basis"`
	ExternalValidation string           `yaml:"external_validation"`
	Relations          []signalRelation `yaml:"relations"`
}

// A missing catalogue disables automatic signaling while retaining the manual
// demonstration. A supplied but malformed catalogue is a configuration error.
// Corpus relations were inspected at build time; this is not a live retrieval
// claim. IDs, own paraphrases and boundaries remain internal configuration.
func loadSignalCatalogue(fsys fs.FS, p *Persona) error {
	p.signalResearchReady = false
	if p.Chat || !p.Vergadering || p.SignalCatalogue == "" {
		return nil
	}
	raw, err := fs.ReadFile(fsys, p.SignalCatalogue)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var cat signalCatalogue
	if err := yaml.Unmarshal(raw, &cat); err != nil {
		return fmt.Errorf("duif: invalid signal catalogue: %w", err)
	}
	if cat.Version == "" || cat.ReviewBasis == "" || cat.ExternalValidation == "" {
		return fmt.Errorf("duif: signal catalogue requires version and review boundary")
	}
	covered := map[string]bool{}
	for _, r := range cat.Relations {
		_, known := p.Topic(r.TopicID)
		if !known || !r.Grounded || !strings.HasPrefix(r.SourceID, "interview:") || !strings.HasPrefix(r.FindingID, "destillaat:") || !strings.HasPrefix(r.QuoteID, "quote:") || r.SourceKey == "" || r.Finding == "" || r.Rationale == "" || r.Boundary == "" {
			return fmt.Errorf("duif: incomplete or ungrounded signal relation for %s", r.TopicID)
		}
		covered[r.TopicID] = true
		for i := range p.Topics {
			if p.Topics[i].ID == r.TopicID {
				p.Topics[i].researchBoundary += r.Boundary + " "
			}
		}
	}
	for _, t := range p.Topics {
		if !covered[t.ID] {
			return fmt.Errorf("duif: signal theme %s lacks research relation", t.ID)
		}
	}
	p.signalResearchReady = len(p.Topics) > 0
	return nil
}
