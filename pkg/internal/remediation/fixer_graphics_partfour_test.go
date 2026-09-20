package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchGraphics_LeavesThePartFourJPEG2000Alone is the negative control for
// the reach of the 6.2.7 arm.
//
// Only ISO 19005-1 spells a clause 6.2.7, the PostScript XObject ban that
// [fixGraphics] really does repair by replacing the XObject with an empty Form.
// clausePrefix reaches past it: ISO 19005-4 numbers its image rules 6.2.7.1 and
// its JPEG2000 rules 6.2.7.3, so the prefix claimed both families.
//
// Repairing these means re-encoding JPX data, the colour channel count, the
// METH entry of the colr box, the bit depth. litho re-encodes no JPEG2000 data
// at all, so the claim asserted a repair that never happened and took the
// violation out of the residual.
//
// [TestMatchGraphics_IgnoresTheBitsPerComponentTests] is the same control for
// the sibling family, and it already carried the principle: a rule sharing a
// clause with rules this fixer claims, with no repair here, must not be
// claimed.
func TestMatchGraphics_LeavesThePartFourJPEG2000Alone(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_2_7_3_t01,
		compliance.Rule_19005_4_6_2_7_3_t02,
		compliance.Rule_19005_4_6_2_7_3_t03,
		compliance.Rule_19005_4_6_2_7_3_t04,
		compliance.Rule_19005_4_6_2_7_3_t05,
	} {
		if matchGraphics(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d: litho re-encodes no JPEG2000 data",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchGraphics_StillClaimsThePartFourImageKeys pins that the part 4 image
// rules survive on their own merits.
//
// Their descriptions name the Alternates, OPI and Interpolate keys, and
// [matchGraphics] claims them through descriptionContains, which carries no
// clause at all. Narrowing the 6.2.7 arm must therefore leave them claimed.
// Without this control the narrowing could silently drop three rules
// [fixGraphics] genuinely repairs.
func TestMatchGraphics_StillClaimsThePartFourImageKeys(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_2_7_1_t01,
		compliance.Rule_19005_4_6_2_7_1_t02,
		compliance.Rule_19005_4_6_2_7_1_t03,
	} {
		if !matchGraphics(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}
