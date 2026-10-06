package duif

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"unicode"
)

const kbV2Schema = "duiven/2"

// kbV3Schema is dezelfde graaf met beide talen erin: elk mensleesbaar veld dat
// vertaald is, staat als {nl, en}. Het contract staat in module/README.md,
// "Het bundelformaat duiven/3".
const kbV3Schema = "duiven/3"
const kbV2Namespace = "Duiven" // existing corpus namespace, never the versioned filename
const kbV2MaxDepth = 32

const (
	reasonV2Read       = "kb_v2_read_failed"
	reasonV2Size       = "kb_v2_size_limit"
	reasonV2JSON       = "kb_v2_json_invalid"
	reasonV2Schema     = "kb_v2_schema_unsupported"
	reasonV2Node       = "kb_v2_node_invalid"
	reasonV2ID         = "kb_v2_id_duplicate_or_missing"
	reasonV2Key        = "kb_v2_source_key_invalid"
	reasonV2Relation   = "kb_v2_relation_invalid"
	reasonV2Ancestry   = "kb_v2_ancestry_invalid"
	reasonV2Grounding  = "kb_v2_grounding_invalid"
	reasonV2Statistics = "kb_v2_statistics_invalid"
	reasonV2Language   = "kb_v2_language_invalid"
)

func kbV2Error(reason string) error { return errors.New("duif: " + reason) }

type kbV2Statistics struct {
	Nodes         map[string]int `json:"node_labels"`
	Relationships map[string]int `json:"relationship_types"`
}
type kbV2Document struct {
	mosaicBundle
	Statistics *kbV2Statistics `json:"statistics"`
}

// Raw JSON is explicitly selected by its .json suffix. It must be duiven/2;
// existing ZIP ingestion keeps the mosaic/1 contract and its layout guards.
// All external filenames/URLs in the JSON are inert data, never followed.
func loadKBV2JSON(name string) (*kbBundle, error) {
	before, err := os.Stat(name)
	if err != nil || !before.Mode().IsRegular() {
		return nil, kbV2Error(reasonV2Read)
	}
	if before.Size() > maxKnowledgeBaseBytes {
		return nil, kbV2Error(reasonV2Size)
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, kbV2Error(reasonV2Read)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return nil, kbV2Error(reasonV2Read)
	}
	if info.Size() > maxKnowledgeBaseBytes {
		return nil, kbV2Error(reasonV2Size)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxKnowledgeBaseBytes+1))
	if err != nil {
		return nil, kbV2Error(reasonV2Read)
	}
	if len(raw) > maxKnowledgeBaseBytes {
		return nil, kbV2Error(reasonV2Size)
	}
	return parseKBV2JSON(raw)
}

func parseKBV2JSON(raw []byte) (*kbBundle, error) {
	if len(raw) > maxKnowledgeBaseBytes {
		return nil, kbV2Error(reasonV2Size)
	}
	if !kbV2UniqueJSON(raw) {
		return nil, kbV2Error(reasonV2JSON)
	}
	var doc kbV2Document
	if json.Unmarshal(raw, &doc) != nil {
		return nil, kbV2Error(reasonV2JSON)
	}
	if doc.Schema != kbV2Schema && doc.Schema != kbV3Schema {
		return nil, kbV2Error(reasonV2Schema)
	}
	if err := validateKBLanguages(&doc.mosaicBundle); err != nil {
		return nil, err
	}
	if err := validateKBV2(&doc); err != nil {
		return nil, err
	}
	// Validation precedes all normalization/mapping. Never silently salvage an
	// invalid graph. Only the known title spelling changes; identity stays put.
	for _, source := range doc.Nodes["Bron"] {
		source["title"] = source["titel"]
	}
	hash := sha256.Sum256(raw)
	mapped := mapMosaic(kbV2Namespace, hex.EncodeToString(hash[:]), &doc.mosaicBundle)
	if len(mapped.Skipped) != 0 {
		return nil, kbV2Error(reasonV2Node)
	}
	return mapped, nil
}

// A bounded token walk rejects duplicate keys (including Go's case-folded
// struct-key aliases), excess nesting, trailing JSON and malformed syntax.
func kbV2UniqueJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > kbV2MaxDepth {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				name = kbV2FoldKey(name)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !walk(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

// encoding/json matches struct fields with Unicode simple folding, not only
// lowercasing (for example s and long-s). Canonicalize each rune once to its
// fold class minimum; object-key lookup remains linear in total key length.
func kbV2FoldKey(key string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return minimum
	}, key)
}

func kbV2String(row map[string]any, key string) (string, bool) {
	value, ok := row[key].(string)
	return value, ok && strings.TrimSpace(value) != ""
}
func kbV2ID(row map[string]any, key string) (string, bool) {
	value, ok := kbV2String(row, key)
	return value, ok && len(value) <= 512 && value == strings.TrimSpace(value)
}
func kbV2Integer(value any) (int64, bool) {
	n, ok := value.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 || n != math.Trunc(n) {
		return 0, false
	}
	return int64(n), true
}
func kbV2OptionalStrings(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, exists := row[key]; exists && value != nil {
			if _, ok := value.(string); !ok {
				return false
			}
		}
	}
	return true
}

type kbV2IndexedNode struct {
	label string
	raw   map[string]any
}

// validateKBLanguages toetst de talenkop. duiven/2 kent geen taal: staat hij er
// tóch, dan is dit waarschijnlijk een half vertaalde bundel met het oude etiket,
// en die hoort niet stil half te landen. duiven/3 eist precies nl en en, met het
// Nederlands als brontaal.
func validateKBLanguages(mb *mosaicBundle) error {
	if mb.Schema != kbV3Schema {
		if mb.Languages != nil || mb.SourceLanguage != "" {
			return kbV2Error(reasonV2Language)
		}
		return nil
	}
	if mb.SourceLanguage != kbLangSource || len(mb.Languages) != 2 {
		return kbV2Error(reasonV2Language)
	}
	seen := map[string]bool{}
	for _, lang := range mb.Languages {
		if lang != kbLangSource && lang != kbLangEnglish || seen[lang] {
			return kbV2Error(reasonV2Language)
		}
		seen[lang] = true
	}
	return nil
}

// kbV2TextKey is per label het verplichte tekstveld, en of dat veld vertaald
// wordt. De Bron draagt óók een `titel`, maar die is de titel van een paper en
// blijft onvertaald — vandaar de vlag per label in plaats van per veldnaam.
type kbV2TextKey struct {
	key       string
	localized bool
}

var kbV2TextKeys = map[string]kbV2TextKey{
	"Bron":        {"titel", false},
	"Domein":      {"titel", true},
	"Thema":       {"titel", true},
	"Subthema":    {"titel", true},
	"Perspectief": {"naam", true},
	"Destillaat":  {"samenvatting", true},
	"Quote":       {"text", false},
}

// kbV2RequiredText leest het verplichte tekstveld van een knoop in de brontaal.
// Voor een vertaald veld is dat de `nl`-kant van het object; de Engelse kant is
// dan al door de typeregel op niet-leeg getoetst.
func kbV2RequiredText(schema string, rule kbV2TextKey, row map[string]any) (string, bool) {
	if !rule.localized {
		return kbV2String(row, rule.key)
	}
	source, _, ok := kbLocalizedText(schema, row[rule.key])
	return source, ok && strings.TrimSpace(source) != ""
}

func validateKBV2(doc *kbV2Document) error {
	if len(doc.Nodes["Bron"]) == 0 || len(doc.Nodes["Destillaat"]) == 0 || len(doc.Nodes["Quote"]) == 0 {
		return kbV2Error(reasonV2Node)
	}
	index := map[string]kbV2IndexedNode{}
	extIDs, sourceKeys := map[string]bool{}, map[string]string{}
	for label, rows := range doc.Nodes {
		rule, known := kbV2TextKeys[label]
		if !known {
			return kbV2Error(reasonV2Node)
		}
		for _, row := range rows {
			id, valid := kbV2ID(row, "id")
			ext, validExt := kbV2ID(row, "ext_id")
			if _, duplicate := index[id]; !valid || !validExt || duplicate || extIDs[ext] {
				return kbV2Error(reasonV2ID)
			}
			if _, valid := kbV2RequiredText(doc.Schema, rule, row); !valid {
				return kbV2Error(reasonV2Node)
			}
			extIDs[ext] = true
			index[id] = kbV2IndexedNode{label, row}
			if label == "Bron" {
				key, valid := kbV2ID(row, "key")
				if !valid || sourceKeys[key] != "" {
					return kbV2Error(reasonV2Key)
				}
				sourceKeys[key] = id
				if _, valid := kbV2String(row, "citatie"); !valid {
					return kbV2Error(reasonV2Node)
				}
				if !kbV2OptionalStrings(row, "doi", "categorie", "citatie_confidence", "citatie_ontbreekt", "citatie_bron", "type", "source_kind", "storage_key", "bron_bestand", "origin", "content_hash", "notitie") {
					return kbV2Error(reasonV2Node)
				}
			}
			if label == "Quote" {
				grounded, valid := row["grounded"].(bool)
				if !valid {
					return kbV2Error(reasonV2Grounding)
				}
				if _, valid := kbV2Integer(row["quote_index"]); !valid {
					return kbV2Error(reasonV2Node)
				}
				start, hasStart := row["start_byte"]
				end, hasEnd := row["end_byte"]
				if hasStart != hasEnd || grounded && !hasStart {
					return kbV2Error(reasonV2Grounding)
				}
				if hasStart {
					a, okA := kbV2Integer(start)
					b, okB := kbV2Integer(end)
					if !okA || !okB || b <= a || grounded && b-a != int64(len(row["text"].(string))) {
						return kbV2Error(reasonV2Grounding)
					}
				}
			}
			if !kbV2MappedPropertiesValid(doc.Schema, label, row) {
				return kbV2Error(reasonV2Node)
			}
		}
	}
	shapes := map[string][2]string{"CONTAINS": {"Bron", "Destillaat"}, "HAS_QUOTE": {"Destillaat", "Quote"}, "BELONGS_TO": {"Destillaat", "Thema"}, "BELONGS_TO_SUBTHEMA": {"Destillaat", "Subthema"}, "BELONGS_TO_PERSPECTIEF": {"Destillaat", "Perspectief"}, "CONTAINS_THEMA": {"Domein", "Thema"}, "CONTAINS_SUBTHEMA": {"Thema", "Subthema"}, "CONTAINS_PERSPECTIEF": {"Thema", "Perspectief"}, "RELATES_TO": {"Destillaat", "Destillaat"}}
	parents := map[string]map[string][]string{}
	children := map[string]map[string][]string{}
	for typ := range shapes {
		parents[typ] = map[string][]string{}
		children[typ] = map[string][]string{}
	}
	for typ, edges := range doc.Relationships {
		shape, known := shapes[typ]
		if !known {
			return kbV2Error(reasonV2Relation)
		}
		seen := map[[2]string]bool{}
		for _, edge := range edges {
			fromID, toID := edge.StartNode.Properties.ID, edge.EndNode.Properties.ID
			from, hasFrom := index[fromID]
			to, hasTo := index[toID]
			pair := [2]string{fromID, toID}
			if edge.Type != typ || !hasFrom || !hasTo || from.label != shape[0] || to.label != shape[1] || seen[pair] || len(edge.StartNode.Labels) != 1 || len(edge.EndNode.Labels) != 1 || edge.StartNode.Labels[0] != from.label || edge.EndNode.Labels[0] != to.label {
				return kbV2Error(reasonV2Relation)
			}
			seen[pair] = true
			parents[typ][toID] = append(parents[typ][toID], fromID)
			children[typ][fromID] = append(children[typ][fromID], toID)
		}
	}
	positions := map[string]map[int64]bool{}
	for id, node := range index {
		switch node.label {
		case "Quote":
			did, okD := kbV2ID(node.raw, "destillaat_id")
			bid, okB := kbV2ID(node.raw, "bron_id")
			key, okK := kbV2ID(node.raw, "bron_key")
			if !okD || !okB || !okK || index[did].label != "Destillaat" || index[bid].label != "Bron" || sourceKeys[key] != bid || len(parents["HAS_QUOTE"][id]) != 1 || parents["HAS_QUOTE"][id][0] != did || len(parents["CONTAINS"][did]) != 1 || parents["CONTAINS"][did][0] != bid {
				return kbV2Error(reasonV2Ancestry)
			}
			position, _ := kbV2Integer(node.raw["quote_index"])
			if positions[did] == nil {
				positions[did] = map[int64]bool{}
			}
			if positions[did][position] {
				return kbV2Error(reasonV2Ancestry)
			}
			positions[did][position] = true
		case "Destillaat":
			if len(parents["CONTAINS"][id]) != 1 || len(children["HAS_QUOTE"][id]) == 0 {
				return kbV2Error(reasonV2Ancestry)
			}
			for _, typ := range []string{"BELONGS_TO", "BELONGS_TO_SUBTHEMA", "BELONGS_TO_PERSPECTIEF"} {
				if len(children[typ][id]) != 1 {
					return kbV2Error(reasonV2Ancestry)
				}
			}
			theme := children["BELONGS_TO"][id][0]
			for _, path := range [][2]string{{"BELONGS_TO_SUBTHEMA", "CONTAINS_SUBTHEMA"}, {"BELONGS_TO_PERSPECTIEF", "CONTAINS_PERSPECTIEF"}} {
				up := parents[path[1]][children[path[0]][id][0]]
				if len(up) != 1 || up[0] != theme {
					return kbV2Error(reasonV2Ancestry)
				}
			}
		case "Thema":
			if len(parents["CONTAINS_THEMA"][id]) != 1 {
				return kbV2Error(reasonV2Ancestry)
			}
		case "Subthema", "Perspectief":
			typ := "CONTAINS_SUBTHEMA"
			membership := "BELONGS_TO_SUBTHEMA"
			if node.label == "Perspectief" {
				typ = "CONTAINS_PERSPECTIEF"
				membership = "BELONGS_TO_PERSPECTIEF"
			}
			if len(parents[typ][id]) != 1 {
				return kbV2Error(reasonV2Ancestry)
			}
			if value, has := node.raw["destillaat_count"]; has {
				count, ok := kbV2Integer(value)
				if !ok || count != int64(len(parents[membership][id])) {
					return kbV2Error(reasonV2Statistics)
				}
			}
		}
	}
	if doc.Statistics == nil || len(doc.Statistics.Nodes) != len(doc.Nodes) || len(doc.Statistics.Relationships) != len(doc.Relationships) {
		return kbV2Error(reasonV2Statistics)
	}
	for label, rows := range doc.Nodes {
		if count, ok := doc.Statistics.Nodes[label]; !ok || count != len(rows) {
			return kbV2Error(reasonV2Statistics)
		}
	}
	for typ, rows := range doc.Relationships {
		if count, ok := doc.Statistics.Relationships[typ]; !ok || count != len(rows) {
			return kbV2Error(reasonV2Statistics)
		}
	}
	return nil
}
