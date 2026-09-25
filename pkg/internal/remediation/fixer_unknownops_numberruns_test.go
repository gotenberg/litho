package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestRepairContentDataKeepsANumberRunWhole pins that a repair reads a run
// that starts with a digit to white space or a delimiter, where validation
// ends the number where veraPDF ends it.
//
// veraPDF reads "55.8492#7136" as the number 55.8492, the undefined operator
// # and the number 7136. Removing the operator with the number before it left
// 7136 to the Td after the run, which moved the text after it 7136 units
// right. Read whole, the run goes with its number, and the Td keeps the one
// operand it had.
//
// # Corpus evidence
//
//   - pdfjs poppler-90-0-fuzzed.pdf: page 14 writes such a run, and the
//     extracted text of its PDF/A-1b remediation dropped from 1079 to 72
//     bytes.
func TestRepairContentDataKeepsANumberRunWhole(t *testing.T) {
	t.Parallel()

	out, tally := repairContentData(context.Background(), []byte("BT /F1 12 Tf 55.8492#7136 0 Td (a) Tj ET"), 1)
	if tally.removed != 1 {
		t.Fatalf("removed %d operators, want the one run", tally.removed)
	}
	if got := string(out); strings.Contains(got, "7136") || !strings.Contains(got, "0 Td") {
		t.Errorf("the rewrite %q keeps a number of the run or loses the Td", got)
	}
}

// TestMatchUnknownOpsClaimsSaturatedNumbers pins that the repair that
// respells a malformed number claims the implementation-limit violation
// validation states for it, marked by [compliance.SaturatedNumberNote], and no
// other limit violation.
func TestMatchUnknownOpsClaimsSaturatedNumbers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		v    compliance.Violation
		want bool
	}{
		{compliance.Violation{Rule: compliance.Rule_19005_1_6_1_12_t02, Context: "object 4 (stream content): real 1.7976931348623157e+308 exceeds +/-32767, " + compliance.SaturatedNumberNote}, true},
		{compliance.Violation{Rule: compliance.Rule_19005_2_6_1_13_t01, Context: "object 4 (stream content): integer -9223372036854775807 out of range, " + compliance.SaturatedNumberNote}, true},
		{compliance.Violation{Rule: compliance.Rule_19005_1_6_1_12_t02, Context: "object 4 (stream content): real 99999.5 exceeds +/-32767"}, false},
	} {
		if got := matchUnknownOps(tc.v); got != tc.want {
			t.Errorf("matchUnknownOps(%q) = %v, want %v", tc.v.Context, got, tc.want)
		}
	}
}

// TestFixRealLimitsLeavesMalformedNumbers pins that the real-limits repair
// leaves a run that spells no number to the repair that respells it. Validation
// reads "-" as the largest 64-bit integer veraPDF gives it, which fails the
// integer limit, and clamping that value to the limit wrote a coordinate no
// viewer draws, PDF.js reading 0 there.
//
// # Corpus evidence
//
//   - pdfjs poppler-90-0-fuzzed.pdf: the clamp wrote a page back from the
//     reading of veraPDF, and the extracted text of its PDF/A-1b remediation
//     dropped past the rollback of the content repairs.
func TestFixRealLimitsLeavesMalformedNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := malformedNumberPDF("-")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	fixes, err := fixRealLimits(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixRealLimits: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("fixRealLimits clamped %v", fixes)
	}
	if _, rewritten := alloc.Pending()[4]; rewritten {
		t.Error("the content stream was rewritten")
	}
}
