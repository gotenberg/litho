package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchXMP_ClaimsThePartOneXPacketAttributes closes the part 1 spelling of
// a requirement litho already repairs.
//
// ISO 19005-1 §6.7.5 t1 and t2 forbid the bytes and encoding attributes on the
// <?xpacket?> header. Parts 2 to 4 spell those same two requirements §6.6.2.1
// t2 and t3, word for word, and [matchXMP] reaches them through its "6.6.2"
// prefix. Its part 1 entries are 6.7.2, 6.7.3, 6.7.8, 6.7.9 and 6.7.11, which
// simply skip over 6.7.5.
//
// [stripForbiddenXPacketAttrs] already strips exactly those two attributes,
// and names the parts 2 to 4 clause while doing it, so the repair was there
// and only the claim was missing.
func TestMatchXMP_ClaimsThePartOneXPacketAttributes(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_7_5_t01,
		compliance.Rule_19005_1_6_7_5_t02,
	} {
		if !matchXMP(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}

// TestMatchXMP_StillClaimsThePartsTwoToFourSpelling is the positive control.
// The parts 2 to 4 spelling already worked and must keep working: widening to
// part 1 must add a clause, never move one.
func TestMatchXMP_StillClaimsThePartsTwoToFourSpelling(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_6_2_1_t02,
		compliance.Rule_19005_2_6_6_2_1_t03,
	} {
		if !matchXMP(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchXMP_LeavesTheLanguageClauseAlone is the negative control that stops
// the new entry becoming a prefix sweep.
//
// ISO 19005-2 and 19005-3 spell the /Lang rule §6.7.4, one number below the
// clause added here. catalog-lang and marked-content-lang claim it and repair
// it; rewriting an XMP packet does nothing for a malformed language tag, so a
// claim here would assert coverage this fixer does not have.
func TestMatchXMP_LeavesTheLanguageClauseAlone(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_7_4_t01,
		compliance.Rule_19005_3_6_7_4_t01,
	} {
		if matchXMP(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d (%s): that clause is the /Lang rule",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}
