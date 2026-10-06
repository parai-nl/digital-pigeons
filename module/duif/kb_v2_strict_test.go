package duif

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

func TestKBV2Strict_RejectsUnicodeFoldedStructKeyDuplicate(t *testing.T) {
	raw := string(kbV2Encode(t, kbV2Fixture(t)))
	// Go's JSON field matching folds the long s to ASCII s. Lowercasing alone
	// does not, so both keys currently reach the same trusted schema field.
	raw = strings.Replace(raw, `"bundle_schema":"duiven/2"`, `"bundle_schema":"synthetic-invalid-schema","bundle_ſchema":"duiven/2"`, 1)
	if !strings.Contains(raw, "bundle_ſchema") {
		t.Fatal("fixture replacement failed")
	}
	var decoded kbV2Document
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil || decoded.Schema != kbV2Schema {
		t.Fatal("fixture does not exercise Go's folded schema alias")
	}
	mapped, err := parseKBV2JSON([]byte(raw))
	if mapped != nil || err == nil || err.Error() != "duif: "+reasonV2JSON {
		t.Fatal("Unicode-equivalent duplicate schema keys were accepted instead of rejected before mapping")
	}
}

func TestKBV2Strict_RejectsMappedPropertiesUnreadableByCore(t *testing.T) {
	for _, tc := range []struct {
		label, key string
		bad        any
	}{
		{"Destillaat", "primary_themes", "synthetic-not-an-array"},
		{"Destillaat", "domain", true},
		{"Quote", "speaker", []string{"synthetic-not-a-string"}},
	} {
		t.Run(tc.label+"/"+tc.key, func(t *testing.T) {
			doc := kbV2Fixture(t)
			doc.Nodes[tc.label][0][tc.key] = tc.bad
			mapped, err := parseKBV2JSON(kbV2Encode(t, doc))
			if mapped == nil && err != nil && err.Error() == "duif: "+reasonV2Node {
				return
			}
			if mapped == nil || err != nil {
				t.Fatal("invalid mapped property must reject before mapping with its static node reason")
			}
			var props map[string]any
			if tc.label == "Destillaat" {
				props = mapped.Destillaten[0].Node.Props
			} else {
				props = findNode(t, mapped.Structure, derived("Duiven", tableQuote, "quote-1")).Props
			}
			raw, err := json.Marshal(props)
			if err != nil {
				t.Fatal("fixture did not encode")
			}
			var typed any
			if tc.label == "Destillaat" {
				typed = &data.Destillaat{}
			} else {
				typed = &data.Quote{}
			}
			if err := json.Unmarshal(raw, typed); err == nil {
				t.Fatal("fixture did not exercise a core typed-read error")
			}
			t.Fatal("v2 accepted mapped property with wrong type; the resulting node cannot be read by the pinned core")
		})
	}
}
