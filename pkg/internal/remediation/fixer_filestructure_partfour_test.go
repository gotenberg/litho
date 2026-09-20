package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchFileStructure_LeavesThePartFourInlineImageAlone is the negative
// control for the clause the part-1 guard did not cover.
//
// [matchFileStructure] already scopes 6.1.7 to ISO 19005-1, because part 4
// spells an unrelated CosUnicodeName requirement that way. 6.1.9 carries the
// same trap one clause over: parts 2 and 3 spell the object and generation
// spacing rule 6.1.9 (CosIndirect), while ISO 19005-4 gives that number to the
// Inline Image /F key (CosIIFilter). compliance.content.go hands the whole
// family out through one selector, so part 4's inline-image rule and parts 1
// to 3's 6.1.10 spellings are the same requirement under four numbers.
//
// [fixFileStructure] repairs nothing itself. It returns [FullRewriteNote] so
// the orchestrator re-serialises the file, and re-serialisation carries a
// content stream's body across as bytes, which [fixHexString] documents from
// measurement. A BI...ID...EI sequence is therefore beyond it, no other fixer
// claims this rule, and claiming it asserts a repair that does not happen
// while removing the violation from the residual.
func TestMatchFileStructure_LeavesThePartFourInlineImageAlone(t *testing.T) {
	t.Parallel()

	if matchFileStructure(compliance.Violation{Rule: compliance.Rule_19005_4_6_1_9_t01}) {
		t.Error("must not claim ISO 19005-4 6.1.9 t1: that clause is the Inline Image /F key there")
	}
}

// TestMatchFileStructure_StillClaimsThePartTwoAndThreeSpacingRules is the
// positive control that stops the fix above from narrowing too far.
//
// Parts 2 and 3 genuinely spell the object and generation spacing rule 6.1.9,
// and a full rewrite is exactly what repairs it. Scoping the arm by test
// number alone, or dropping 6.1.9 outright, would silently stop repairing
// these.
func TestMatchFileStructure_StillClaimsThePartTwoAndThreeSpacingRules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_1_9_t01,
		compliance.Rule_19005_3_6_1_9_t01,
	} {
		if !matchFileStructure(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchFileStructure_LeavesThePartFour3DStreamSubtypeAlone is the second
// sole-claimant cell a full rewrite cannot reach.
//
// ISO 19005-4 6.1.6.1 t3 requires a 3D stream /Subtype of U3D or PRC. The
// rewrite copies dictionary values verbatim, so a wrong /Subtype survives it,
// and litho ships no fixer that repairs a 3D stream at all. The neighbouring
// tests of the same clause are genuine rewrite targets, which is why this has
// to be scoped by test number rather than by clause.
func TestMatchFileStructure_LeavesThePartFour3DStreamSubtypeAlone(t *testing.T) {
	t.Parallel()

	if matchFileStructure(compliance.Violation{Rule: compliance.Rule_19005_4_6_1_6_1_t03}) {
		t.Error("must not claim ISO 19005-4 6.1.6.1 t3: a full rewrite cannot change a 3D stream /Subtype")
	}
}

// TestMatchFileStructure_StillClaimsThePartFourStreamRules is the positive
// control for the clause the test above narrows.
func TestMatchFileStructure_StillClaimsThePartFourStreamRules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_1_6_1_t01,
		compliance.Rule_19005_4_6_1_6_1_t02,
	} {
		if !matchFileStructure(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}
