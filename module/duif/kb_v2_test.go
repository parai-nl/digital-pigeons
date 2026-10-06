package duif

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

func kbV2Fixture(t *testing.T) kbV2Document {
	t.Helper()
	quote := "The synthetic study reported a limited association."
	nodes := map[string][]map[string]any{
		"Bron":        {{"id": "source-1", "ext_id": "ext-source", "key": "synthetic-key", "titel": "Synthetic source", "citatie": "Synthetic citation", "doi": "10.1234/synthetic", "categorie": "Synthetic category", "citatie_confidence": "low", "citatie_ontbreekt": "issue", "citatie_bron": "/PRIVATE_DO_NOT_OPEN", "type": "report", "source_kind": "document", "storage_key": "/PRIVATE_DO_NOT_OPEN", "bron_bestand": "https://PRIVATE_DO_NOT_FETCH.invalid"}},
		"Domein":      {{"id": "domain-1", "ext_id": "ext-domain", "titel": "Synthetic domain"}},
		"Thema":       {{"id": "theme-1", "ext_id": "ext-theme", "titel": "Synthetic theme"}},
		"Subthema":    {{"id": "sub-1", "ext_id": "ext-sub", "titel": "Synthetic subtheme", "destillaat_count": 1}},
		"Perspectief": {{"id": "perspective-1", "ext_id": "ext-perspective", "naam": "Synthetic perspective", "destillaat_count": 1}},
		"Destillaat":  {{"id": "dest-1", "ext_id": "ext-dest", "samenvatting": "Synthetic editorial summary."}},
		"Quote":       {{"id": "quote-1", "ext_id": "ext-quote", "text": quote, "quote_index": 0, "grounded": true, "start_byte": 10, "end_byte": 10 + len(quote), "destillaat_id": "dest-1", "bron_id": "source-1", "bron_key": "synthetic-key"}},
	}
	labelOf := map[string]string{}
	for label, rows := range nodes {
		for _, row := range rows {
			labelOf[row["id"].(string)] = label
		}
	}
	rels := map[string][]mosaicRel{"RELATES_TO": {}}
	for _, edge := range [][3]string{{"CONTAINS", "source-1", "dest-1"}, {"HAS_QUOTE", "dest-1", "quote-1"}, {"BELONGS_TO", "dest-1", "theme-1"}, {"BELONGS_TO_SUBTHEMA", "dest-1", "sub-1"}, {"BELONGS_TO_PERSPECTIEF", "dest-1", "perspective-1"}, {"CONTAINS_THEMA", "domain-1", "theme-1"}, {"CONTAINS_SUBTHEMA", "theme-1", "sub-1"}, {"CONTAINS_PERSPECTIEF", "theme-1", "perspective-1"}} {
		r := mosaicRel{Type: edge[0], Properties: map[string]any{}, StartNode: mosaicRelNode{Labels: []string{labelOf[edge[1]]}}, EndNode: mosaicRelNode{Labels: []string{labelOf[edge[2]]}}}
		r.StartNode.Properties.ID, r.EndNode.Properties.ID = edge[1], edge[2]
		rels[edge[0]] = append(rels[edge[0]], r)
	}
	doc := kbV2Document{mosaicBundle: mosaicBundle{Schema: kbV2Schema, Nodes: nodes, Relationships: rels}}
	kbV2RefreshStatistics(&doc)
	return doc
}
func kbV2RefreshStatistics(doc *kbV2Document) {
	doc.Statistics = &kbV2Statistics{Nodes: map[string]int{}, Relationships: map[string]int{}}
	for label, rows := range doc.Nodes {
		doc.Statistics.Nodes[label] = len(rows)
	}
	for typ, rows := range doc.Relationships {
		doc.Statistics.Relationships[typ] = len(rows)
	}
}
func kbV2Encode(t *testing.T, doc kbV2Document) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestKBV2RawPreservesHistoricalIdentityMetadataAndGrounding(t *testing.T) {
	doc := kbV2Fixture(t)
	raw := kbV2Encode(t, doc)
	name := filepath.Join(t.TempDir(), "new-version-and-location.json")
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
	mapped, err := loadKBBundle(name)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if mapped.KBID != "Duiven" || mapped.Schema != "duiven/2" || mapped.ContentHash != hex.EncodeToString(digest[:]) || len(mapped.Skipped) != 0 || mapped.Counts[tableInterview] != 1 || mapped.Counts[tablePersona] != 0 || mapped.Counts[data.RelContains] != 1 {
		t.Fatal("v2 identity/schema/count metadata mismatch")
	}
	source := findNode(t, mapped.Structure, derived("Duiven", tableInterview, "source-1"))
	if source.Props["title"] != "Synthetic source" || source.Props["citatie_ontbreekt"] != "issue" || source.Props["citatie_confidence"] != "low" || source.Props["doi"] != "10.1234/synthetic" || source.Props["key"] != "synthetic-key" {
		t.Fatal("bibliography lost or renamed")
	}
	if _, exists := source.Props["bron_bestand"]; exists {
		t.Fatal("source filename was promoted into stored public bibliography")
	}
	quote := findNode(t, mapped.Structure, derived("Duiven", tableQuote, "quote-1"))
	if quote.Props["grounded"] != true || quote.Props["bron_id"] != "source-1" || quote.Props["bron_key"] != "synthetic-key" || quote.Props["destillaat_id"] != "dest-1" {
		t.Fatal("validated quote ancestry lost")
	}
	for _, node := range mapped.Structure {
		if node.Props["bundle_schema"] != "duiven/2" {
			t.Fatal("v2 structure lacks trusted schema marker")
		}
	}
	if mapped.Destillaten[0].Node.Props["bundle_schema"] != "duiven/2" {
		t.Fatal("v2 candidate lacks trusted schema marker")
	}
	after, err := os.ReadFile(name)
	if err != nil || string(after) != string(raw) {
		t.Fatal("source JSON mutated")
	}
	// Equivalent legacy export uses the old label/title but exactly the same
	// namespace/export IDs. Compare derived IDs and edges, not filenames.
	doc.Schema = kbBundleSchema
	doc.Nodes["Interview"] = doc.Nodes["Bron"]
	delete(doc.Nodes, "Bron")
	doc.Nodes["Interview"][0]["title"] = doc.Nodes["Interview"][0]["titel"]
	legacy := mapMosaic("Duiven", "legacy", &doc.mosaicBundle)
	if findNode(t, legacy.Structure, source.ID).ID != source.ID || legacy.Destillaten[0].Node.ID != mapped.Destillaten[0].Node.ID {
		t.Fatal("v2 renamed historical IDs")
	}
	if _, exists := legacy.Destillaten[0].Node.Props["bundle_schema"]; exists {
		t.Fatal("legacy candidate was falsely marked v2")
	}
	if len(kbTables()) != 9 {
		t.Fatal("alias mapping duplicated an owned KB table")
	}
	seen := map[string]bool{}
	for _, table := range kbTables() {
		if seen[table] {
			t.Fatal("duplicate KB table")
		}
		seen[table] = true
	}
}

func TestKBV2RejectsCorruptGraphBeforeMappingWithStaticErrors(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		mutate       func(*kbV2Document)
	}{
		{"schema", reasonV2Schema, func(d *kbV2Document) { d.Schema = "PRIVATE_BAD_SCHEMA" }},
		{"persona", reasonV2Node, func(d *kbV2Document) { d.Nodes["Persona"] = []map[string]any{} }},
		{"unknown label", reasonV2Node, func(d *kbV2Document) { d.Nodes["PRIVATE_UNKNOWN"] = []map[string]any{} }},
		{"missing id", reasonV2ID, func(d *kbV2Document) { delete(d.Nodes["Bron"][0], "id") }},
		{"duplicate id", reasonV2ID, func(d *kbV2Document) { d.Nodes["Quote"][0]["id"] = "dest-1" }},
		{"duplicate external id", reasonV2ID, func(d *kbV2Document) { d.Nodes["Quote"][0]["ext_id"] = "ext-dest" }},
		{"duplicate source key", reasonV2Key, func(d *kbV2Document) {
			copy := kbCloneProps(d.Nodes["Bron"][0])
			copy["id"], copy["ext_id"] = "source-2", "ext-source-2"
			d.Nodes["Bron"] = append(d.Nodes["Bron"], copy)
		}},
		{"bad missing-parts type", reasonV2Node, func(d *kbV2Document) { d.Nodes["Bron"][0]["citatie_ontbreekt"] = true }},
		{"malformed grounding", reasonV2Grounding, func(d *kbV2Document) { d.Nodes["Quote"][0]["grounded"] = "true" }},
		{"missing grounding", reasonV2Grounding, func(d *kbV2Document) { delete(d.Nodes["Quote"][0], "grounded") }},
		{"true without offsets", reasonV2Grounding, func(d *kbV2Document) {
			delete(d.Nodes["Quote"][0], "start_byte")
			delete(d.Nodes["Quote"][0], "end_byte")
		}},
		{"partial offsets", reasonV2Grounding, func(d *kbV2Document) { delete(d.Nodes["Quote"][0], "end_byte") }},
		{"invalid offset span", reasonV2Grounding, func(d *kbV2Document) { d.Nodes["Quote"][0]["end_byte"] = 11 }},
		{"fractional quote index", reasonV2Node, func(d *kbV2Document) { d.Nodes["Quote"][0]["quote_index"] = 0.5 }},
		{"missing quote parent", reasonV2Ancestry, func(d *kbV2Document) { d.Relationships["HAS_QUOTE"] = nil }},
		{"wrong destillaat", reasonV2Ancestry, func(d *kbV2Document) { d.Nodes["Quote"][0]["destillaat_id"] = "PRIVATE_OTHER" }},
		{"wrong bron id", reasonV2Ancestry, func(d *kbV2Document) { d.Nodes["Quote"][0]["bron_id"] = "PRIVATE_OTHER" }},
		{"wrong bron key", reasonV2Ancestry, func(d *kbV2Document) { d.Nodes["Quote"][0]["bron_key"] = "PRIVATE_OTHER" }},
		{"contains absent", reasonV2Ancestry, func(d *kbV2Document) { d.Relationships["CONTAINS"] = nil }},
		{"duplicate edge", reasonV2Relation, func(d *kbV2Document) {
			d.Relationships["CONTAINS"] = append(d.Relationships["CONTAINS"], d.Relationships["CONTAINS"][0])
		}},
		{"endpoint absent", reasonV2Relation, func(d *kbV2Document) { d.Relationships["CONTAINS"][0].StartNode.Properties.ID = "PRIVATE_OTHER" }},
		{"wrong endpoint label", reasonV2Relation, func(d *kbV2Document) { d.Relationships["CONTAINS"][0].StartNode.Labels = []string{"Interview"} }},
		{"wrong relation type", reasonV2Relation, func(d *kbV2Document) { d.Relationships["CONTAINS"][0].Type = "PRIVATE_UNKNOWN" }},
		{"wrong taxonomy ancestry", reasonV2Ancestry, func(d *kbV2Document) { d.Relationships["CONTAINS_SUBTHEMA"] = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := kbV2Fixture(t)
			tc.mutate(&doc)
			kbV2RefreshStatistics(&doc)
			mapped, err := parseKBV2JSON(kbV2Encode(t, doc))
			if mapped != nil || err == nil || err.Error() != "duif: "+tc.reason || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("invalid graph did not produce its fixed reason: %v", err)
			}
		})
	}
}

func TestKBV2RejectsStatisticsDuplicateJSONDepthAndRawLegacy(t *testing.T) {
	doc := kbV2Fixture(t)
	doc.Statistics.Nodes["Quote"]++
	if _, err := parseKBV2JSON(kbV2Encode(t, doc)); err == nil || err.Error() != "duif: "+reasonV2Statistics {
		t.Fatal("incorrect declared count accepted")
	}
	doc = kbV2Fixture(t)
	doc.Nodes["Subthema"][0]["destillaat_count"] = 2
	if _, err := parseKBV2JSON(kbV2Encode(t, doc)); err == nil || err.Error() != "duif: "+reasonV2Statistics {
		t.Fatal("incorrect subtheme count accepted")
	}
	doc = kbV2Fixture(t)
	doc.Nodes["Perspectief"][0]["destillaat_count"] = 0
	if _, err := parseKBV2JSON(kbV2Encode(t, doc)); err == nil || err.Error() != "duif: "+reasonV2Statistics {
		t.Fatal("incorrect perspective count accepted")
	}
	raw := string(kbV2Encode(t, kbV2Fixture(t)))
	for _, bad := range []string{raw + " {}", strings.Replace(raw, `"bundle_schema":`, `"BUNDLE_SCHEMA":"PRIVATE_OVERRIDE","bundle_schema":`, 1), strings.Repeat("[", kbV2MaxDepth+2) + "0" + strings.Repeat("]", kbV2MaxDepth+2), `{"nodes":"PRIVATE_BAD_SHAPE"}`} {
		if mapped, err := parseKBV2JSON([]byte(bad)); mapped != nil || err == nil || err.Error() != "duif: "+reasonV2JSON {
			t.Fatal("invalid/duplicate/deep JSON accepted")
		}
	}
	doc = kbV2Fixture(t)
	doc.Schema = kbBundleSchema
	if _, err := parseKBV2JSON(kbV2Encode(t, doc)); err == nil || err.Error() != "duif: "+reasonV2Schema {
		t.Fatal("raw mosaic/1 accepted as v2")
	}
}

func TestKBV2FalseGroundingAndOptionalBibliographyRemainHonest(t *testing.T) {
	doc := kbV2Fixture(t)
	q := doc.Nodes["Quote"][0]
	q["grounded"] = false
	delete(q, "start_byte")
	delete(q, "end_byte")
	for _, field := range []string{"doi", "citatie_confidence", "citatie_ontbreekt", "type"} {
		delete(doc.Nodes["Bron"][0], field)
	}
	mapped, err := parseKBV2JSON(kbV2Encode(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	quote := findNode(t, mapped.Structure, derived("Duiven", tableQuote, "quote-1"))
	if quote.Props["grounded"] != false {
		t.Fatal("false quote promoted")
	}
	source := findNode(t, mapped.Structure, derived("Duiven", tableInterview, "source-1"))
	for _, field := range []string{"doi", "citatie_confidence", "citatie_ontbreekt", "type"} {
		if _, ok := source.Props[field]; ok {
			t.Fatal("missing bibliography invented")
		}
	}
	// Repeated parse stays deterministic and does not change source bytes.
	again, err := parseKBV2JSON(kbV2Encode(t, doc))
	if err != nil || !reflect.DeepEqual(mapped, again) {
		t.Fatal("v2 mapping is not deterministic")
	}
}

func TestKBV2RawInputBoundsAndStaticReadErrors(t *testing.T) {
	if _, err := loadKBV2JSON(t.TempDir()); err == nil || err.Error() != "duif: "+reasonV2Read {
		t.Fatal("nonregular raw input accepted")
	}
	if _, err := loadKBV2JSON(filepath.Join(t.TempDir(), "PRIVATE_MISSING.json")); err == nil || err.Error() != "duif: "+reasonV2Read {
		t.Fatal("read error exposed path or was accepted")
	}
	name := filepath.Join(t.TempDir(), "oversized.json")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxKnowledgeBaseBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := loadKBBundle(name); err == nil || err.Error() != "duif: "+reasonV2Size {
		t.Fatal("raw JSON size bound not enforced")
	}
}
