package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// TestFormatViolationScope pins which part states which raw-byte format
// requirement, measured against veraPDF 1.30.2.
//
// The three that moved:
//
//   - Subsection-header spacing is ISO 19005-1, 6.1.4 t1
//     (subsectionHeaderSpaceSeparated) and nothing else. Parts 2 and up kept
//     only xrefEOLMarkersComplyPDFA, so a spacing defect has no rule there.
//   - The stream-keyword EOL requirement is ISO 19005-1, 6.1.7 t2 and
//     ISO 19005-2/3, 6.1.7.1 t2. ISO 19005-4, 6.1.6.1 dropped it, so folding it
//     into t1 would state a defect part 4 does not.
//   - A declared /Length that disagrees with the bytes is swept by
//     [checkStreamLengths], so resolution's copy of the same finding is dropped
//     here. A stream with no /Length key at all is NOT swept, and still comes
//     through.
func TestFormatViolationScope(t *testing.T) {
	t.Parallel()

	var (
		xrefSpacing = parser.NewFormatViolation(document.ErrXRefSubsectionSpacing,
			"compliance: xref subsection header has 2 spaces between start and count (must be 1)")
		xrefEOL      = parser.NewFormatViolation(document.ErrXRefEOLMarkers, "compliance: xref keyword not followed by EOL")
		streamCR     = parser.NewFormatViolation(parser.ErrStreamKeywordEOL, "stream keyword followed by CR only (must be CR+LF or LF)")
		streamLength = parser.NewFormatViolation(parser.ErrStreamLengthMismatch,
			"stream /Length 57 does not match actual byte count 56")
		streamNoLenth = parser.NewFormatViolation(parser.ErrStreamMissingLength, "stream missing /Length key")
		objHeader     = parser.NewFormatViolation(document.ErrObjectHeaderFormat,
			"compliance: obj header 4: 2 white-space characters between object number and generation (must be 1)")
		hexChar    = fmt.Errorf("compliance: %w", fmt.Errorf("%w %q", parser.ErrHexStringInvalidChar, '!'))
		hexOdd     = fmt.Errorf("compliance: %w", parser.ErrHexStringOddDigits)
		misleading = fmt.Errorf("compliance: xref stream: %w", fmt.Errorf("%w %q", parser.ErrHexStringInvalidChar, 'g'))
		exponent   = parser.NewFormatViolation(parser.ErrNumberExponent,
			"compliance: number uses scientific notation, which ISO 32000-1 §7.3.3 does not permit")
		exponentAlone = parser.NewFormatViolation(parser.ErrNumberExponentOperand,
			"compliance: number uses scientific notation, which ISO 32000-1 §7.3.3 does not permit")
	)

	for _, tc := range []struct {
		name    string
		msg     error
		profile string
		want    Rule
	}{
		{"xref spacing at part 1", xrefSpacing, "PDF/A-1b", Rule_19005_1_6_1_4_t01},
		{"xref spacing at part 2", xrefSpacing, "PDF/A-2b", Rule{}},
		{"xref spacing at part 4", xrefSpacing, "PDF/A-4", Rule{}},
		{"xref EOL at part 1", xrefEOL, "PDF/A-1b", Rule_19005_1_6_1_4_t02},
		{"xref EOL at part 2", xrefEOL, "PDF/A-2b", Rule_19005_2_6_1_4_t02},
		{"xref EOL at part 4", xrefEOL, "PDF/A-4", Rule_19005_4_6_1_4_t01},
		{"stream keyword EOL at part 1", streamCR, "PDF/A-1b", Rule_19005_1_6_1_7_t02},
		{"stream keyword EOL at part 2", streamCR, "PDF/A-2b", Rule_19005_2_6_1_7_1_t02},
		{"stream keyword EOL at part 4", streamCR, "PDF/A-4", Rule{}},
		{"length mismatch is swept elsewhere", streamLength, "PDF/A-2b", Rule{}},
		{"a missing /Length is not swept", streamNoLenth, "PDF/A-2b", Rule_19005_2_6_1_7_1_t01},
		{"an object header is swept elsewhere", objHeader, "PDF/A-2b", Rule{}},
		{"a hex string with an invalid character", hexChar, "PDF/A-2b", Rule_19005_2_6_1_6_t02},
		{"a hex string with an odd digit count", hexOdd, "PDF/A-2b", Rule_19005_2_6_1_6_t01},
		// The class decides, not the text: this message names an xref and a
		// stream, which the substring match read as an xref defect.
		{"the class decides, not the message", misleading, "PDF/A-2b", Rule_19005_2_6_1_6_t02},
		// No ISO 19005 rule states number syntax in an object, and veraPDF
		// stops at a parse error there; a content stream is judged elsewhere.
		{"a number with an exponent at part 1", exponent, "PDF/A-1b", Rule{}},
		{"a number with an exponent at part 2", exponent, "PDF/A-2b", Rule{}},
		{"an unnested number with an exponent", exponentAlone, "PDF/A-2b", Rule{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vctx := &validationCtx{formatViolations: []error{tc.msg}}
			got := checkFormatViolations(vctx, tc.profile)
			if tc.want == (Rule{}) {
				if len(got) != 0 {
					t.Fatalf("want silence, got %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want exactly one violation, got %d: %v", len(got), got)
			}
			if got[0].Rule != tc.want {
				t.Errorf("got rule %s test %d, want %s test %d",
					got[0].Rule.Clause, got[0].Rule.TestNumber, tc.want.Clause, tc.want.TestNumber)
			}
		})
	}
}
