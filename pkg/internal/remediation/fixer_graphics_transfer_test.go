package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// The ExtGState transfer-function requirement, in the four spellings the parts
// give it. Part 1 says 6.2.8; parts 2, 3 and 4 say 6.2.5.
var transferFunctionRules = []compliance.Rule{
	{Specification: "ISO_19005_1", Clause: "6.2.8", TestNumber: 1, Object: "PDExtGState",
		Description: "An ExtGState dictionary shall not contain the TR key"},
	{Specification: "ISO_19005_1", Clause: "6.2.8", TestNumber: 2, Object: "PDExtGState",
		Description: "An ExtGState dictionary shall not contain the TR2 key with a value other than Default"},
	{Specification: "ISO_19005_2", Clause: "6.2.5", TestNumber: 1, Object: "PDExtGState",
		Description: "An ExtGState dictionary shall not contain the TR key"},
	{Specification: "ISO_19005_3", Clause: "6.2.5", TestNumber: 2, Object: "PDExtGState",
		Description: "An ExtGState dictionary shall not contain the TR2 key with a value other than Default"},
	{Specification: "ISO_19005_4", Clause: "6.2.5", TestNumber: 1, Object: "PDExtGState",
		Description: "A graphics state parameter dictionary shall not contain the TR key"},
}

// TestMatchGraphics_ClaimsTransferFunctionsInEverySpelling pins the part-1
// spelling.
//
// [clausePrefix] matched only "6.2.5", which parts 2, 3 and 4 use, so PDF/A-1
// targets kept /TR and /TR2 while every other part had them repaired. The
// repair itself was never part-specific: fixGraphics deletes /TR and sets /TR2
// to /Default on any ExtGState it walks.
//
// ISO 19005-1 states only t1 and t2 under clause 6.2.8, which is why the
// part-1 arm may match the clause whole. The same shape would be wrong for
// parts 2 to 4, whose 6.2.5 also covers halftone dictionaries (t4 HalftoneType,
// t5 HalftoneName, t6 TransferFunction); those stay on the prefix arm.
func TestMatchGraphics_ClaimsTransferFunctionsInEverySpelling(t *testing.T) {
	t.Parallel()

	for _, rule := range transferFunctionRules {
		if !matchGraphics(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim the transfer-function rule as %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchGraphics_IgnoresTheBitsPerComponentTests is the negative control.
// Those tests share a clause with rules this fixer does claim, carry no corpus
// cells, and have no repair here: claiming them would assert coverage that does
// not exist.
func TestMatchGraphics_IgnoresTheBitsPerComponentTests(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{Specification: "ISO_19005_1", Clause: "6.2.4", TestNumber: 4, Object: "PDXImage",
			Description: "If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4 or 8"},
		{Specification: "ISO_19005_2", Clause: "6.2.8", TestNumber: 4, Object: "PDXImage",
			Description: "If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16"},
		{Specification: "ISO_19005_2", Clause: "6.2.8", TestNumber: 5, Object: "PDMaskImage",
			Description: "If an image mask dictionary contains the BitsPerComponent key, its value shall be 1"},
		{Specification: "ISO_14289_1", Clause: "7.2", TestNumber: 2, Object: "PDOutline",
			Description: "Natural language in the Outline entries shall be determined"},
	} {
		if matchGraphics(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}
