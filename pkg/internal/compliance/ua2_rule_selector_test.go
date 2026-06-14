package compliance

import "testing"

// TestUA2RuleSelectors pins the PDF/UA-1 vs PDF/UA-2 rule-id selection for the
// structure and form selectors. A prior off-by-one ("PDF/UA-2"[6] is '-', not
// '2') made every selector fall through to the UA-1 rule id under PDF/UA-2, so
// veraPDF saw 8.2.x while litho emitted 7.1.x on the same construct.
func TestUA2RuleSelectors(t *testing.T) {
	type want struct {
		spec   string
		clause string
		test   int
	}
	cases := []struct {
		name   string
		got    Rule
		expect want
	}{
		{"markInfo UA-1", markInfoRule("PDF/UA-1", 0), want{"ISO_14289_1", "6.2", 1}},
		{"markInfo UA-2", markInfoRule("PDF/UA-2", 0), want{"ISO_14289_2", "6.2", 1}},
		{"structTreeRoot UA-1", structTreeRootRule("PDF/UA-1", 0), want{"ISO_14289_1", "7.1", 11}},
		{"structTreeRoot UA-2", structTreeRootRule("PDF/UA-2", 0), want{"ISO_14289_2", "8.2.1", 1}},
		{"roleMapNonStandard UA-1", roleMapNonStandardRule("PDF/UA-1"), want{"ISO_14289_1", "7.1", 5}},
		{"roleMapNonStandard UA-2", roleMapNonStandardRule("PDF/UA-2"), want{"ISO_14289_2", "8.2.4", 1}},
		{"roleMapCircular UA-1", roleMapCircularRule("PDF/UA-1"), want{"ISO_14289_1", "7.1", 6}},
		{"roleMapCircular UA-2", roleMapCircularRule("PDF/UA-2"), want{"ISO_14289_2", "8.2.4", 2}},
		{"roleMapStandardRemap UA-1", roleMapStandardRemapRule("PDF/UA-1"), want{"ISO_14289_1", "7.1", 7}},
		{"roleMapStandardRemap UA-2", roleMapStandardRemapRule("PDF/UA-2"), want{"ISO_14289_2", "8.2.4", 3}},
		{"xfa UA-1", xfaRule("PDF/UA-1", 0, true), want{"ISO_14289_1", "7.15", 1}},
		{"xfa UA-2", xfaRule("PDF/UA-2", 0, true), want{"ISO_14289_2", "8.10.1", 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Specification != tc.expect.spec || tc.got.Clause != tc.expect.clause || tc.got.TestNumber != tc.expect.test {
				t.Errorf("got %s %s-t%d, want %s %s-t%d",
					tc.got.Specification, tc.got.Clause, tc.got.TestNumber,
					tc.expect.spec, tc.expect.clause, tc.expect.test)
			}
		})
	}
}
