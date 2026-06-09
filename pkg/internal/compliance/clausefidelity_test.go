package compliance

import "testing"

// These selectors must emit the part-correct clause id so litho's rule-level
// output matches veraPDF (the verdict was already correct under the part-1 id).
func TestPartDispatchedRuleSelectors(t *testing.T) {
	cases := []struct {
		name string
		got  Rule
		want Rule
	}{
		{"forbiddenAction A-2", forbiddenActionTypeRule(2), Rule_19005_2_6_5_1_t01},
		{"forbiddenAction A-3", forbiddenActionTypeRule(3), Rule_19005_3_6_5_1_t01},
		{"forbiddenAction A-1", forbiddenActionTypeRule(1), Rule_19005_1_6_6_1_t01},
		{"namedAction A-2", namedActionRule(2), Rule_19005_2_6_5_1_t02},
		{"namedAction A-3", namedActionRule(3), Rule_19005_3_6_5_1_t02},
		{"deviceN A-2", deviceNComponentRule(2), Rule_19005_2_6_1_13_t09},
		{"deviceN A-3", deviceNComponentRule(3), Rule_19005_3_6_1_13_t09},
		{"deviceN A-1", deviceNComponentRule(1), Rule_19005_1_6_1_12_t09},
		{"roleRemap A-2", roleMapStandardRemapRule("PDF/A-2a"), Rule_19005_2_6_7_3_4_t03},
		{"roleRemap A-3", roleMapStandardRemapRule("PDF/A-3a"), Rule_19005_3_6_7_3_4_t03},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, c.got, c.want)
		}
	}
}
