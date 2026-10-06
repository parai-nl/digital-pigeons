package duif

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

func TestKBV2MalformedCoreFieldsFailBeforeMapping(t *testing.T) {
	for _, tc := range []struct {
		label, field string
		bad          any
		typed        any
	}{
		{"Destillaat", "primary_themes", "invalid scalar", &data.Destillaat{}},
		{"Destillaat", "domain", true, &data.Destillaat{}},
		{"Quote", "speaker", []string{"invalid array"}, &data.Quote{}},
	} {
		t.Run(tc.label+"/"+tc.field, func(t *testing.T) {
			doc := kbV2Fixture(t)
			doc.Nodes[tc.label][0][tc.field] = tc.bad
			control, err := json.Marshal(doc.Nodes[tc.label][0])
			if err != nil || json.Unmarshal(control, tc.typed) == nil {
				t.Fatal("positive control did not reproduce the pinned core read failure")
			}
			if got, err := parseKBV2JSON(kbV2Encode(t, doc)); got != nil || err == nil || !strings.HasPrefix(err.Error(), "duif: kb_v2_") {
				t.Fatal("invalid mapped type survived validation")
			}
		})
	}
}

func TestKBV2EveryMappedPropertyHasStrictPresentValueTypes(t *testing.T) {
	labels := []string{"Bron", "Domein", "Thema", "Subthema", "Perspectief", "Destillaat", "Quote"}
	// In duiven/2 is een vertaalbaar veld nog gewoon een kale string of een
	// lijst strings; de tweetalige vorm hoort daar juist geweigerd te worden.
	valid := map[string]any{"string": "Synthetic optional text", "strings": []string{"Synthetic element"}, "integer": 0, "boolean": true, "confidence": "medium",
		"localized": "Synthetic optional text", "localized-list": []string{"Synthetic element"}}
	bad := map[string]any{"string": []string{"SYNTHETIC_PRIVATE_WRONG_TYPE"}, "strings": "SYNTHETIC_PRIVATE_SCALAR", "integer": 0.5, "boolean": "SYNTHETIC_PRIVATE_TRUE", "confidence": "SYNTHETIC_PRIVATE_97%",
		"localized": map[string]any{"nl": "SYNTHETIC_PRIVATE_WRONG_TYPE", "en": "SYNTHETIC_PRIVATE_WRONG_TYPE"}, "localized-list": "SYNTHETIC_PRIVATE_SCALAR"}
	complete := kbV2Fixture(t)
	for _, label := range labels {
		for _, field := range kbNodeProps[kbLabelTables[label]] {
			kind, known := kbV2MappedKinds[field]
			if !known {
				t.Fatalf("mapped field lacks an explicit type rule: %s/%s", label, field)
			}
			if _, exists := complete.Nodes[label][0][field]; !exists {
				complete.Nodes[label][0][field] = valid[kind]
			}
			invalid := []any{bad[kind], nil}
			if kind == "strings" || kind == "localized-list" {
				invalid = append(invalid, []any{"Synthetic valid element", 3}, []any{nil}, []any{[]string{"nested"}})
			}
			for caseIndex, value := range invalid {
				t.Run(label+"/"+field+"/"+string(rune('a'+caseIndex)), func(t *testing.T) {
					doc := kbV2Fixture(t)
					doc.Nodes[label][0][field] = value
					if got, err := parseKBV2JSON(kbV2Encode(t, doc)); got != nil || err == nil || !strings.HasPrefix(err.Error(), "duif: kb_v2_") || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE") {
						t.Fatal("incorrect mapped type survived or leaked into its error")
					}
				})
			}
		}
	}
	mapped, err := parseKBV2JSON(kbV2Encode(t, complete))
	if err != nil {
		t.Fatal("positive fully populated mapped-property fixture failed:", err)
	}
	assertKBV2CoreReadable(t, mapped)
}

func assertKBV2CoreReadable(t *testing.T, mapped *kbBundle) {
	t.Helper()
	check := func(node kbNode) {
		t.Helper()
		var typed any
		switch node.Table {
		case tableDestillaat:
			typed = &data.Destillaat{}
		case tableQuote:
			typed = &data.Quote{}
		case tableInterview:
			typed = &data.Interview{}
		case tableThema, tableSubthema, tableDomein:
			typed = &data.Thema{}
		case tablePerspective:
			typed = &data.Perspective{}
		default:
			t.Fatal("v2 mapped an unexpected table")
		}
		raw, err := json.Marshal(node.Props)
		if err != nil || json.Unmarshal(raw, typed) != nil {
			t.Fatal("validated mapped properties fail a pinned core typed read")
		}
	}
	for _, node := range mapped.Structure {
		check(node)
	}
	for _, dest := range mapped.Destillaten {
		check(dest.Node)
	}
}
