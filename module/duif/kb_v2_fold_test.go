package duif

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKBV2UnicodeStructAliasCannotOverrideInvalidSchema(t *testing.T) {
	raw := string(kbV2Encode(t, kbV2Fixture(t)))
	for _, alias := range []string{"bundle_ſchema", `bundle_\u017fchema`, "BUNDLE_SCHEMA"} {
		t.Run(alias, func(t *testing.T) {
			bad := strings.Replace(raw, `"bundle_schema":"duiven/2"`, `"bundle_schema":"synthetic-invalid-schema","`+alias+`":"duiven/2"`, 1)
			var control kbV2Document
			if err := json.Unmarshal([]byte(bad), &control); err != nil || control.Schema != kbV2Schema {
				t.Fatal("positive control did not exercise the Go decoder alias")
			}
			if got, err := parseKBV2JSON([]byte(bad)); got != nil || err == nil || err.Error() != "duif: "+reasonV2JSON {
				t.Fatal("duplicate folded key overrode a rejected schema")
			}
		})
	}
}

func TestKBV2UnicodeKeyFoldHasStableEquivalentClasses(t *testing.T) {
	for _, raw := range []string{`{"Key":1,"Key":2}`, `{"Σ":1,"ς":2}`, `{"S":1,"ſ":2}`, `{"outer":{"ſchema":1,"schema":2}}`} {
		if kbV2UniqueJSON([]byte(raw)) {
			t.Fatal("Unicode-equivalent object keys accepted")
		}
	}
	// Distinct keys remain legal; same keys in separate objects are not
	// duplicates. The guard is structural, not a global corpus-key index.
	for _, raw := range []string{`{"Key":1,"other":2}`, `[{"Σ":1},{"ς":2}]`, `{"é":1,"e":2}`} {
		if !kbV2UniqueJSON([]byte(raw)) {
			t.Fatal("distinct keys or separate objects conflated")
		}
	}
}
