package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchFileStructure_ClaimsThePartOneStreamRules pins the part-1 spelling
// of the stream-length and stream-keyword-EOL rules.
//
// [matchFileStructure] tested HasPrefix(clause, "6.1.7.1"), which parts 2 and 3
// use. That literal is LONGER than part 1's "6.1.7", so it could never match,
// and PDF/A-1 targets kept both defects while the full rewrite repaired them
// for every other part. The repair is not part-specific: fixFileStructure only
// marks the violation handled, and the orchestrator re-serialises the file.
func TestMatchFileStructure_ClaimsThePartOneStreamRules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{
			Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 1, Object: "CosStream",
			Description: "The value of the Length key specified in the stream dictionary shall match the number of bytes",
		},
		{
			Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 2, Object: "CosStream",
			Description: "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah)",
		},
	} {
		if !matchFileStructure(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}

	// The spellings that already worked must keep working.
	for _, rule := range []compliance.Rule{
		{Specification: "ISO_19005_2", Clause: "6.1.7.1", TestNumber: 1, Object: "CosStream"},
		{Specification: "ISO_19005_3", Clause: "6.1.7.1", TestNumber: 2, Object: "CosStream"},
		{Specification: "ISO_19005_4", Clause: "6.1.6.1", TestNumber: 1, Object: "CosStream"},
	} {
		if !matchFileStructure(compliance.Violation{Rule: rule}) {
			t.Errorf("narrowing regression: must still claim %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchFileStructure_LeavesThePartOneFKeysAlone is the negative control
// that matters most.
//
// ISO 19005-1 6.1.7 t3 forbids the F, FFilter and FDecodeParms keys.
// fixFileStructure repairs nothing itself: it marks violations handled so the
// orchestrator forces a full rewrite, and re-serialisation cannot delete a
// dictionary key. Claiming t3 here would assert a repair that does not happen,
// and stream-file-keys already claims and actually repairs it.
func TestMatchFileStructure_LeavesThePartOneFKeysAlone(t *testing.T) {
	t.Parallel()

	v := compliance.Violation{Rule: compliance.Rule{
		Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 3, Object: "CosStream",
		Description: "A stream object dictionary shall not contain the F, FFilter, or FDecodeParms keys",
	}}
	if matchFileStructure(v) {
		t.Error("must not claim ISO 19005-1 6.1.7 t3: a full rewrite cannot delete a dictionary key")
	}
}

// TestMatchFileStructure_IgnoresThePartFourUnicodeNameRule is the second
// negative control. ISO 19005-4 spells an unrelated requirement 6.1.7
// (CosUnicodeName: font and colourant names), so the new arm has to check the
// specification rather than the clause alone.
func TestMatchFileStructure_IgnoresThePartFourUnicodeNameRule(t *testing.T) {
	t.Parallel()

	for _, tn := range []int{1, 2} {
		v := compliance.Violation{Rule: compliance.Rule{
			Specification: "ISO_19005_4", Clause: "6.1.7", TestNumber: tn,
			Object:      "CosUnicodeName",
			Description: "Font names, names of colourants in Separation and DeviceN colour spaces",
		}}
		if matchFileStructure(v) {
			t.Errorf("must not claim ISO 19005-4 6.1.7 t%d: that clause is CosUnicodeName there", tn)
		}
	}
}
