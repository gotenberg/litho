package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchLZW_ClaimsEverySpellingOfTheBan pins the gap this closes.
//
// The LZWDecode ban is one requirement carrying four clause numbers: ISO
// 19005-1 spells it 6.1.10, ISO 19005-2 and 19005-3 spell the stream-filter
// half 6.1.7.2, and ISO 19005-4 spells it 6.1.6.2. The matcher tested only
// 6.1.10, so a PDF/A-2, -3 or -4 target kept an LZWDecode stream that
// [fixLZW] already knew how to re-encode as FlateDecode.
func TestMatchLZW_ClaimsEverySpellingOfTheBan(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{
			Specification: "ISO_19005_1", Clause: "6.1.10", TestNumber: 1, Object: "CosFilter",
			Description: "The LZWDecode filter shall not be permitted",
		},
		{
			Specification: "ISO_19005_2", Clause: "6.1.7.2", TestNumber: 1, Object: "CosFilter",
			Description: "All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode",
		},
		{
			Specification: "ISO_19005_3", Clause: "6.1.7.2", TestNumber: 1, Object: "CosFilter",
			Description: "All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode",
		},
		{
			Specification: "ISO_19005_4", Clause: "6.1.6.2", TestNumber: 1, Object: "CosFilter",
			Description: "All standard stream filters listed in ISO 32000-2:2020, 7.4, Table 6 may be used, with the exception of LZWDecode",
		},
	} {
		if !matchLZW(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchLZW_DoesNotSwallowNeighbouringClauses is the negative control for
// the literals the matcher compares.
//
// Every clause here shares a prefix with one the matcher claims, and none of
// them is about LZWDecode. Writing this as clausePrefix(v, "6.1.7") would
// swallow 6.1.7.1, the stream-length and stream-keyword-EOL rule the full
// rewrite handles; "6.1.6" would swallow both the hexadecimal-string rules and
// 6.1.6.1. Re-encoding a stream repairs none of those, so a claim here would
// report them handled while they persist.
//
// ISO 19005-4 6.1.9 is the sharpest case: parts 2 and 3 spell an
// object/generation spacing rule 6.1.9, while part 4 gives the same number to
// the Inline Image /F key. A clause number is not a rule id.
func TestMatchLZW_DoesNotSwallowNeighbouringClauses(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{
			Specification: "ISO_19005_2", Clause: "6.1.7.1", TestNumber: 1, Object: "CosStream",
			Description: "The value of the Length key specified in the stream dictionary shall match the number of bytes",
		},
		{
			Specification: "ISO_19005_2", Clause: "6.1.7.1", TestNumber: 3, Object: "CosStream",
			Description: "A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys",
		},
		{
			Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 1, Object: "CosStream",
			Description: "The value of the Length key specified in the stream dictionary shall match the number of bytes",
		},
		{
			Specification: "ISO_19005_4", Clause: "6.1.7", TestNumber: 1, Object: "CosUnicodeName",
			Description: "Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names",
		},
		{
			Specification: "ISO_19005_1", Clause: "6.1.6", TestNumber: 1, Object: "CosString",
			Description: "Hexadecimal strings shall contain an even number of non-white-space characters",
		},
		{
			Specification: "ISO_19005_4", Clause: "6.1.6.1", TestNumber: 2, Object: "CosStream",
			Description: "A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys",
		},
		{
			Specification: "ISO_19005_4", Clause: "6.1.5", TestNumber: 1, Object: "CosString",
			Description: "Hexadecimal strings shall contain an even number of non-white-space characters",
		},
		{
			Specification: "ISO_19005_2", Clause: "6.1.9", TestNumber: 1, Object: "CosIndirect",
			Description: "The object number and generation number shall be separated by a single white-space character",
		},
		{
			Specification: "ISO_19005_4", Clause: "6.1.9", TestNumber: 1, Object: "CosIIFilter",
			Description: "The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt",
		},
	} {
		if matchLZW(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d (%s)",
				rule.Specification, rule.Clause, rule.TestNumber, rule.Object)
		}
	}
}
