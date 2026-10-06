package duif

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGraphGrounding_OnlyExplicitQuoteFlagBecomesPublicEnum(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
		want string
	}{
		{"false", false, groundingRejected}, {"true", true, groundingVerified}, {"legacy", nil, ""}, {"string_is_not_bool", "false", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := map[string]any{"text": "Synthetic original passage.", "storage_key": "PRIVATE_CANARY", "grounding": "PRIVATE_CANARY"}
			if tc.raw != nil {
				props["grounded"] = tc.raw
			}
			got := graphNodeOf("quote:synthetic", props)
			if tc.want == "" {
				if _, ok := got.Metadata["grounding"]; ok {
					t.Fatal("unknown flag gained a status")
				}
			} else if got.Metadata["grounding"] != tc.want {
				t.Fatal("wrong normalized quote status")
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "PRIVATE_CANARY") || strings.Contains(string(raw), `"grounded":`) {
				t.Fatal("raw/private property leaked")
			}
			for _, value := range got.Metadata {
				if _, ok := value.(string); !ok {
					t.Fatal("graph metadata must remain text only")
				}
			}
			other := graphNodeOf("interview:synthetic", props)
			if _, ok := other.Metadata["grounding"]; ok {
				t.Fatal("quote grounding was assigned to a source record")
			}
		})
	}
}
