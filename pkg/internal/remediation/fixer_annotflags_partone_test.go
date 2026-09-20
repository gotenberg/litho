package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchAnnotFlags_LeavesThePartOneFontRulesAlone pins the specification
// scoping of clause 6.3.2.
//
// [matchAnnotFlags] tested `clausePrefix(v, "6.3.2")` on the clause alone, and
// a clause number is not a rule id. ISO 19005-2, 19005-3 and 19005-4 all give
// 6.3.2 to the annotation /F flag, but ISO 19005-1 gives that number to the
// font dictionary keys: /Type (t1), /Subtype (t2), /BaseFont (t3), /FirstChar
// (t4), /LastChar (t5), /Widths (t6) and the FontFile subtype (t7).
//
// [fixAnnotFlags] answers a 6.3.2 violation by writing an /F key into every
// non-Popup annotation on every page. That repairs no font key, and it reports
// "added /F flag to %d annotations" while doing it.
//
// t2, t4, t5, t6 and t7 are the cells that matter: [matchFontKeys] covers only
// TestNumber 1 and 3 for this clause, so for the other five annot-flags was the
// SOLE claimant and no fixer in the registry repaired them at all.
//
// All seven share an identical opening sentence ("All fonts used in a
// conforming file shall conform to the font specifications defined in PDF
// Reference 5.5"), so the description arm cannot separate them either.
func TestMatchAnnotFlags_LeavesThePartOneFontRulesAlone(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_3_2_t01,
		compliance.Rule_19005_1_6_3_2_t02,
		compliance.Rule_19005_1_6_3_2_t03,
		compliance.Rule_19005_1_6_3_2_t04,
		compliance.Rule_19005_1_6_3_2_t05,
		compliance.Rule_19005_1_6_3_2_t06,
		compliance.Rule_19005_1_6_3_2_t07,
	} {
		if matchAnnotFlags(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d (%s): that clause is a font rule there, not an annotation flag",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchAnnotFlags_StillClaimsTheAnnotationFlagRules is the positive control
// for the narrowing.
//
// t2 of each part is the sharp one. Its description says "If present, the F
// key's Print flag bit shall be set to 1" and never uses the word "annotation",
// so the description arm does NOT claim it: it reaches [fixAnnotFlags] through
// the clause arm alone. A narrowing written as an early `return false` on 6.3.2,
// or one that dropped the clause arm in favour of the description, would keep
// t1 and silently strand t2 in all three parts.
func TestMatchAnnotFlags_StillClaimsTheAnnotationFlagRules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_3_2_t01,
		compliance.Rule_19005_2_6_3_2_t02,
		compliance.Rule_19005_3_6_3_2_t01,
		compliance.Rule_19005_3_6_3_2_t02,
		compliance.Rule_19005_4_6_3_2_t01,
		compliance.Rule_19005_4_6_3_2_t02,
	} {
		if !matchAnnotFlags(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchAnnotFlags_StillClaimsThePartOneAnnotationRuleThroughItsDescription
// pins the description arm, which is what makes the clause scoping above safe
// rather than lossy.
//
// ISO 19005-1 does carry the annotation /F rule. It numbers it 6.5.3 t2, not
// 6.3.2: "An annotation dictionary shall contain the F key. The F key's Print
// flag bit shall be set to 1". So the two arms divide the work by
// specification, arm one for parts 2 to 4 and arm two for part 1, and scoping
// the clause arm costs part 1 nothing.
//
// Without this control, a later simplification that collapsed
// [matchAnnotFlags] to a plain clause test would drop the only part 1
// annotation /F cell and still pass every other test in this file.
func TestMatchAnnotFlags_StillClaimsThePartOneAnnotationRuleThroughItsDescription(t *testing.T) {
	t.Parallel()

	rule := compliance.Rule_19005_1_6_5_3_t02
	if !matchAnnotFlags(compliance.Violation{Rule: rule}) {
		t.Errorf("must still claim %s/%s t%d (%s): that is part 1's own annotation /F rule",
			rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
	}
}
