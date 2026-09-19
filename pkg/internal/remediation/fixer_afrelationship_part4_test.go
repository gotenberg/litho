package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchAFRelationship_ClaimsThePartFourSpelling pins the gap this closes.
//
// Exactly three rules require /AFRelationship on a file specification, and the
// matcher named only two: ISO 19005-3 §6.8 t3 and ISO 14289-2 §8.9.2.4.10 t1.
// ISO 19005-4 numbers the same requirement §6.9 t4, so a PDF/A-4 target kept a
// file specification that [fixAFRelationship] already knew how to repair by
// adding /AFRelationship /Unspecified.
func TestMatchAFRelationship_ClaimsThePartFourSpelling(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_9_t04,
		compliance.Rule_19005_3_6_8_t03,
		compliance.Rule_14289_2_8_9_2_4_10_t01,
	} {
		if !matchAFRelationship(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchAFRelationship_StillClaimsWhatItAlreadyDid is the regression pin.
//
// Widening a matcher has already cost 12 corpus cells once this session, when a
// new arm returned a negative and short-circuited the clauses below it. Every
// unit test passed while those cells broke, because each asserted only the NEW
// claim. This asserts the OLD ones survive.
func TestMatchAFRelationship_StillClaimsWhatItAlreadyDid(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_3_6_8_t01,
		compliance.Rule_19005_3_6_8_t02,
		compliance.Rule_19005_3_6_8_t03,
		compliance.Rule_19005_3_6_8_t04,
		compliance.Rule_19005_2_6_8_t02,
		compliance.Rule_14289_2_8_9_2_4_10_t01,
	} {
		if !matchAFRelationship(compliance.Violation{Rule: rule}) {
			t.Errorf("must STILL claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchAFRelationship_DoesNotSwallowTheOtherClause69Rules guards the
// literals: a clause number is not a rule id.
//
// ISO 19005-4 §6.9 carries five unrelated embedded-file tests and only t4 is
// about /AFRelationship. ISO 19005-1 §6.9 is an AcroForm rule and ISO 19005-2
// and -3 §6.9 are the optional-content configuration rules. Written as
// clausePrefix(v, "6.9") this claim would take all of them.
func TestMatchAFRelationship_DoesNotSwallowTheOtherClause69Rules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_9_t01, // MIME /Subtype
		compliance.Rule_19005_4_6_9_t02, // F and UF keys
		compliance.Rule_19005_4_6_9_t03, // embedded files must themselves conform
		compliance.Rule_19005_4_6_9_t05, // EmbeddedFiles key in /Names
		compliance.Rule_19005_1_6_9_t01, // PDAcroForm NeedAppearances
		compliance.Rule_19005_2_6_9_t01, // optional-content /Name
		compliance.Rule_19005_2_6_9_t04, // optional-content /AS ban
		compliance.Rule_19005_3_6_9_t04,
	} {
		if matchAFRelationship(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d: it is not the AFRelationship rule",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}
