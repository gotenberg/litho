package compliance

import "testing"

// TestICCBasedNRule_OnlyPart1 covers which part states the requirement that an
// ICCBased colour space's /N match the component count its profile header
// implies. Only ISO 19005-1, 6.2.3.2 test 2 does. Parts 2 to 4 spell 6.2.4.2
// test 1 as "the profile shall conform to ICC.1:1998-09 ... or ISO 15076-1",
// which iccBasedProfileRule already answers for, so a component-count mismatch
// reported there accused the profile of a defect it does not have.
func TestICCBasedNRule_OnlyPart1(t *testing.T) {
	t.Parallel()

	if got := iccBasedNRule(1); got != Rule_19005_1_6_2_3_2_t02 {
		t.Errorf("part 1: got %v, want 6.2.3.2-t02", got)
	}

	for part := 2; part <= 4; part++ {
		if got := iccBasedNRule(part); got.Specification != "" {
			t.Errorf("part %d: got %v, want no rule", part, got)
		}
		// The profile-conformance rule those parts do state must stay wired,
		// so suppressing the component-count report does not silence it.
		if got := iccBasedProfileRule(part); got.Clause != "6.2.4.2" || got.TestNumber != 1 {
			t.Errorf("part %d profile rule: got %s-t%d, want 6.2.4.2-t1",
				part, got.Clause, got.TestNumber)
		}
	}
}
