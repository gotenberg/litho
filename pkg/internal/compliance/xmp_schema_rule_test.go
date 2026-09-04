package compliance

import "testing"

// TestXMPSchemaRuleAnswersBothTests pins that an undeclared XMP namespace is
// reported under both tests of the clause.
//
// veraPDF splits ISO 19005-2/3 6.6.2.3.1 into
// "isPredefinedInXMP2005 || isDefinedInMainPackage || isDefinedInCurrentPackage"
// (test 1) and "isValueTypeCorrect" (test 2). A property whose schema is
// declared nowhere fails the second for the same reason as the first: there is
// no declared type to hold its value to. Reporting only test 1 left litho
// answering two findings on a remediated 6-6-4-t01-fail-a where veraPDF answers
// three.
func TestXMPSchemaRuleAnswersBothTests(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		part  int
		tests []int
	}{
		{1, []int{2, 3}}, // ISO 19005-1 numbers them 6.7.9 t2 and t3.
		{2, []int{1, 2}},
		{3, []int{1, 2}},
	} {
		got := xmpSchemaRule(tc.part, "http://example.invalid/ns/")
		if len(got) != len(tc.tests) {
			t.Errorf("part %d: reported %d violation(s), want %d", tc.part, len(got), len(tc.tests))
			continue
		}
		for i, want := range tc.tests {
			v := got[i]
			if v.Rule.TestNumber != want {
				t.Errorf("part %d: violation %d is test %d, want test %d",
					tc.part, i, v.Rule.TestNumber, want)
			}
			if v.Context == "" {
				t.Errorf("part %d: violation %d carries no context", tc.part, i)
			}
		}
	}
}

// TestXMPSchemaRuleIsSilentForPart4 is the control: ISO 19005-4 drops the
// extension-schema requirement, so neither test applies.
func TestXMPSchemaRuleIsSilentForPart4(t *testing.T) {
	t.Parallel()

	if got := xmpSchemaRule(4, "http://example.invalid/ns/"); len(got) != 0 {
		t.Errorf("part 4 reported %v, want nothing", got)
	}
}
