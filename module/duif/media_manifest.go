package duif

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// This internal delivery manifest is embedded alongside configuration. Only a
// validated URL/crop projection is included in the public frontend; masters,
// permissions and review records never become part of the scene labels.
type soundMediaRecord struct {
	ID         string `json:"id"`
	NeutralID  string `json:"neutralId"`
	File       string `json:"file"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	SHA256     string `json:"sha256"`
	DurationMs int    `json:"durationMs"`
	Bytes      int64  `json:"bytes"`
	Origin     struct {
		Reference string `json:"reference"`
		Maker     string `json:"maker"`
	} `json:"origin"`
	Rights struct {
		PublicDistribution bool `json:"publicDistribution"`
	} `json:"rights"`
	Approval *struct {
		ReviewedBy string `json:"reviewedBy"`
		ReviewedAt string `json:"reviewedAt"`
	} `json:"approval"`
}

func applySoundManifest(fsys fs.FS, set *PersonaSet) error {
	data, err := fs.ReadFile(fsys, "media-manifest.v1.json")
	if err != nil {
		return fmt.Errorf("duif media: manifest ontbreekt: %w", err)
	}
	var manifest struct {
		Version int                `json:"version"`
		Sounds  []soundMediaRecord `json:"sounds"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return fmt.Errorf("duif media: ongeldig manifest")
	}
	seen := make(map[string]bool)
	neutral := make(map[string]bool)
	for _, record := range manifest.Sounds {
		sound, ok := set.sounds[record.ID]
		if !ok || seen[record.ID] || !idPattern.MatchString(record.NeutralID) || neutral[record.NeutralID] {
			return fmt.Errorf("duif media: onbekend of dubbel geluid-ID")
		}
		seen[record.ID] = true
		neutral[record.NeutralID] = true
		if !fs.ValidPath(record.File) || !strings.HasPrefix(record.File, "sounds/") || strings.Contains(strings.TrimPrefix(record.File, "sounds/"), "/") || !strings.HasSuffix(record.File, ".mp3") || record.File != sound.Bestand {
			return fmt.Errorf("duif media: bestand komt niet overeen voor %q", record.ID)
		}
		if record.DurationMs < 200 || record.DurationMs > 15000 {
			return fmt.Errorf("duif media: ongeldige duur voor %q", record.ID)
		}
		if record.Origin.Maker == "" || record.Origin.Reference == "" {
			return fmt.Errorf("duif media: ontbrekende herkomst voor %q", record.ID)
		}
		switch record.Status {
		case "temporary":
			if record.Kind != "synthetic" {
				return fmt.Errorf("duif media: tijdelijke status alleen voor expliciet synthetisch materiaal")
			}
		case "approved":
			if record.Kind != "field-recording" || !record.Rights.PublicDistribution || record.Approval == nil || record.Approval.ReviewedBy == "" || record.Approval.ReviewedAt == "" {
				return fmt.Errorf("duif media: opname mist rechten of goedkeuring voor %q", record.ID)
			}
		default:
			return fmt.Errorf("duif media: ongeldige exportstatus voor %q", record.ID)
		}
		stat, err := fs.Stat(fsys, record.File)
		if err != nil || stat.IsDir() || stat.Size() != record.Bytes || stat.Size() <= 0 || stat.Size() > 1024*1024 {
			return fmt.Errorf("duif media: bestandsgrootte wijkt af voor %q", record.ID)
		}
		audio, err := fs.ReadFile(fsys, record.File)
		if err != nil {
			return fmt.Errorf("duif media: bestand niet leesbaar voor %q", record.ID)
		}
		hash := sha256.Sum256(audio)
		if len(record.SHA256) != 64 || hex.EncodeToString(hash[:]) != record.SHA256 {
			return fmt.Errorf("duif media: hash wijkt af voor %q", record.ID)
		}
		sound.DurationMs = record.DurationMs
		set.sounds[record.ID] = sound
		for _, persona := range set.Personas {
			for index := range persona.Geluiden {
				if persona.Geluiden[index].ID == record.ID {
					persona.Geluiden[index].DurationMs = record.DurationMs
				}
			}
		}
	}
	if len(seen) != len(set.sounds) {
		return fmt.Errorf("duif media: manifest dekt niet de volledige geluidenset")
	}
	return nil
}
