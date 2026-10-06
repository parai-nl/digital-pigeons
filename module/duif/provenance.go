package duif

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"

	"github.com/parai-nl/parai-core/data"
)

// AnswerProvenance is a snapshot at the start of a chat turn. Empty metadata
// on historical rows stays empty: we never attribute today's research or
// prompt contract to an older answer. Hashes identify content without exposing
// prompts or copying private framework implementation into this module.
type AnswerProvenance struct {
	ContractVersion string `json:"contractVersion"`
	PersonaVersion  string `json:"personaVersion"`
	PromptVersion   string `json:"promptVersion"`
	Model           string `json:"model"`
	KBState         string `json:"kbState"`
	KBID            string `json:"kbId,omitempty"`
	KBVersion       string `json:"kbVersion,omitempty"`
}

type provenanceContextKey struct{}

// Only this project's prompt assembly is fingerprinted; no private framework
// source or user content is copied. This also versions inline tool instructions
// and ordering changes that a list of persona prompt files would miss.
//
//go:embed prompt.go agent_tools.go agent_turn.go turn.go retrieval.go gate.go source_grounding.go source_bibliography.go answer_stream.go
var promptAssemblyFS embed.FS

var promptAssemblyVersion = func() string {
	parts := map[string]string{}
	entries, _ := promptAssemblyFS.ReadDir(".")
	for _, entry := range entries {
		raw, _ := promptAssemblyFS.ReadFile(entry.Name())
		parts[entry.Name()] = string(raw)
	}
	return contentVersion(parts)
}()

func (m *Module) promptContractVersion(p *Persona) string {
	// Assemble every chat behavior branch using fixed placeholders. These are
	// contract samples, not an assertion that all branches ran in this answer.
	// Runtime language/citation/gap directives therefore contribute as well.
	var prompts []string
	rag, err := fs.ReadFile(configFS, ragTemplatePath)
	if err == nil {
		prompts = append(prompts, string(rag))
	}
	for _, lang := range []string{"en", "nl"} {
		for _, status := range []KBStatus{KBPersonaOnly, KBGap, KBSources} {
			for _, related := range []bool{false, true} {
				prompts = append(prompts, BuildSystemPrompt(PromptInput{Persona: p, Lang: lang, Status: status, RAGSection: "<versioned-research-placeholder>", Related: related}))
			}
		}
		if m.personas.Facilitator != nil && p.EngineOr() == EngineAgent {
			prompts = append(prompts, BuildFacilitatorPrompt(m.personas.Facilitator, lang), facilitatorPersonaDirective(p.ID, pickLang(p.Naam, lang), lang))
		}
	}
	return contentVersion(struct {
		Assembly string
		Prompts  []string
	}{promptAssemblyVersion, prompts})
}

func contentVersion(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (m *Module) chatProvenance(ctx context.Context, pigeonID string) *AnswerProvenance {
	p, ok := m.personas.Get(pigeonID)
	if !ok || !p.Chat {
		return nil
	}
	v := &AnswerProvenance{
		ContractVersion: "duiven-2026-09-07-v1", PersonaVersion: contentVersion(p),
		PromptVersion: m.promptContractVersion(p), Model: m.model, KBState: "not_used",
	}
	if !p.Kennisbank {
		return v
	}
	v.KBState = "unverified"
	if m.knowledge == nil {
		v.KBState = "unavailable"
		return v
	}
	rows, err := m.knowledge.ListNodes(ctx, tableImportMarker, data.ListOpts{Limit: 1})
	if err == nil && len(rows) > 0 {
		marker := markerFromProps(rows[0])
		v.KBID, v.KBVersion = marker.KBID, marker.ContentHash
		if marker.ContentHash != "" {
			v.KBState = "versioned"
		}
	}
	return v
}

func stampAnswer(ctx context.Context, m ChatMessage) ChatMessage {
	if m.Role == "assistant" && m.Provenance == nil {
		if v, ok := ctx.Value(provenanceContextKey{}).(*AnswerProvenance); ok && v != nil {
			copy := *v
			m.Provenance = &copy
		}
	}
	return m
}
