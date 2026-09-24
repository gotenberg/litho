package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchAggressive_LeavesThePartsTwoAndThreeOptionalContentAlone pins the
// half of clause 6.9 that has nothing to do with appearances.
//
// [matchAggressive] tested `case "6.3.3", "6.5.3", "6.9"` on the clause alone,
// and a clause number is not a rule id. ISO 19005-1 spells the form-appearance
// rules 6.9, but ISO 19005-2 and 19005-3 give that number to the optional
// content configuration dictionary: /Name, its uniqueness, the /Order array
// and the /AS ban.
//
// [fixAggressive] responds to a 6.9 violation by running
// fixAnnotationAppearances over every page, which writes an /AP into every
// annotation that lacks one. That repairs no optional-content rule, and unlike
// a no-op claim it MUTATES the document on an unrelated trigger.
//
// Test 3 is the sharpest of the four: it is the /Order completeness rule that
// [matchCatalogCleanup] deliberately refuses because fixOCGConfigs cannot
// complete /Order, and that refusal was extended to part 4 in an earlier
// commit. There aggressive claimed it as the SOLE claimant.
func TestMatchAggressive_LeavesThePartsTwoAndThreeOptionalContentAlone(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_9_t01,
		compliance.Rule_19005_2_6_9_t02,
		compliance.Rule_19005_2_6_9_t03,
		compliance.Rule_19005_2_6_9_t04,
		compliance.Rule_19005_3_6_9_t01,
		compliance.Rule_19005_3_6_9_t02,
		compliance.Rule_19005_3_6_9_t03,
		compliance.Rule_19005_3_6_9_t04,
	} {
		if matchAggressive(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d (%s): that clause is optional content there, not an appearance",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchAggressive_LeavesThePartFourEmbeddedFileRulesAlone is the same trap
// in a third specification.
//
// ISO 19005-4 gives 6.9 to embedded files: the MIME subtype, the F and UF
// keys, embedded-file conformance, /AFRelationship, and the EmbeddedFiles name
// tree. Generating annotation appearances repairs none of them, and t1 and t5
// have no other claimant at all.
func TestMatchAggressive_LeavesThePartFourEmbeddedFileRulesAlone(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_9_t01,
		compliance.Rule_19005_4_6_9_t02,
		compliance.Rule_19005_4_6_9_t03,
		compliance.Rule_19005_4_6_9_t04,
		compliance.Rule_19005_4_6_9_t05,
	} {
		if matchAggressive(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d (%s): that clause is an embedded-file rule there",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchAggressive_StillClaimsThePartOneFormAppearances is the positive
// control for the narrowing: ISO 19005-1 is the one specification whose 6.9 IS
// about appearances, and fixAnnotationAppearances genuinely repairs it.
func TestMatchAggressive_StillClaimsThePartOneFormAppearances(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_9_t01,
		compliance.Rule_19005_1_6_9_t02,
	} {
		if !matchAggressive(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchAggressive_StillClaimsItsOtherArms guards the arms that share the
// same switch and are NOT mis-scoped.
//
// 6.3.3 and 6.5.3 each carry exactly one meaning across every part that spells
// them, PDAnnot, so they need no specification scoping. Without this control a
// narrowing that also dropped them would pass the tests above.
//
// The Op_Undefined case is written as a literal rather than a named rule on
// purpose: that arm keys on the Object, not the clause, because ISO 19005-1
// numbers the undefined-operator rule 6.2.10 while parts 2 to 4 number it
// 6.2.2. A named rule would exercise the clause path instead.
func TestMatchAggressive_StillClaimsItsOtherArms(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_5_3_t01,
		compliance.Rule_19005_2_6_3_3_t01,
		compliance.Rule_19005_4_6_3_3_t01,
		compliance.Rule_19005_2_6_2_9_t01,
		compliance.Rule_19005_1_6_2_6_t01,
		{
			Specification: "ISO_19005_2", Clause: "6.2.2", TestNumber: 1,
			Object:      "Op_Undefined",
			Description: "A content stream shall not contain any operators not defined in ISO 32000-1",
		},
	} {
		if !matchAggressive(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}
