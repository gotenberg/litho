package remediation

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestRepairContentDataRespellsNestedMalformedNumbers pins that a run that
// spells no number where an array or a dictionary holds it is respelled in
// place as the number it begins with, 0 when it holds no digit, inside the
// part's limits. Validation reads "-" there as the largest 64-bit integer and
// "1.5.5" as the largest double, as veraPDF 1.30 does, and fails the
// implementation limits on them at PDF/A-1b and PDF/A-2b.
//
// # Corpus evidence
//
//   - pdfjs bug1953099.pdf: writes a lone "-" in a TJ array.
func TestRepairContentDataRespellsNestedMalformedNumbers(t *testing.T) {
	t.Parallel()

	const path = " 0 0 m 10 10 l S"
	for _, tc := range []struct {
		name, data, want string
		part             int
	}{
		{"a lone minus in a dash array", "[- 2] 0 d" + path, "[0 2] 0 d" + path, 2},
		{"a lone minus in a TJ array", "BT [(a) - (b)] TJ ET", "BT [(a) 0 (b)] TJ ET", 2},
		{"two periods in a property list", "/Span << /MCID 1.5.5 >> BDC EMC", "/Span << /MCID 1.5 >> BDC EMC", 2},
		{"two signs in a nested array", "[[--5] 2] 0 d" + path, "[[0] 2] 0 d" + path, 2},
		{"a trailing minus in a dash array", "[1- 2] 0 d" + path, "[1 2] 0 d" + path, 2},
		{"a period in an inline image", "q BI /W 1 /H 1 /BPC 8 /CS /G /D [. 1] ID \x00 EI Q", "q BI /W 1 /H 1 /BPC 8 /CS /G /D [0 1] ID \x00 EI Q", 2},
		{"a prefix past the part-1 real limit", "[99999.5.5 2] 0 d" + path, "[32767 2] 0 d" + path, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, tally := repairContentData(context.Background(), []byte(tc.data), tc.part)
			if tally.repaired != 1 {
				t.Errorf("repaired %d malformed numbers, want 1", tally.repaired)
			}
			if string(out) != tc.want {
				t.Errorf("the repair wrote %q, want %q", out, tc.want)
			}
			assertNoSaturatedNumber(t, out)
		})
	}
}

// TestRepairContentDataKeepsANestedMalformedNumberOnRewrite pins that a
// malformed number in an array survives a stream written back from its parsed
// operations. The reading of a repair drops a keyword from an array, so
// removing an undefined operator from the stream wrote the dash array of
// "[- 2] 0 d" back as [2], another dash pattern.
func TestRepairContentDataKeepsANestedMalformedNumberOnRewrite(t *testing.T) {
	t.Parallel()

	out, tally := repairContentData(context.Background(), []byte("[- 2] 0 d xx 0 0 m 10 10 l S"), 2)
	if tally.removed != 1 || tally.repaired != 1 {
		t.Errorf("removed %d operators and repaired %d numbers, want 1 and 1", tally.removed, tally.repaired)
	}
	if got := string(out); !strings.Contains(got, "[0 2] 0 d") || strings.Contains(got, "xx") {
		t.Errorf("the rewrite %q loses a dash element or keeps the undefined operator", got)
	}
	assertNoSaturatedNumber(t, out)
}

// TestRepairContentDataLeavesNestedNumbersInTheLimits pins that a stream whose
// arrays and dictionaries hold numbers inside the limits is not rewritten,
// "3-5" among them, which validation reads as veraPDF does, as 3 then -5.
//
// # Corpus evidence
//
//   - pdfjs bug1130815.pdf: writes "3-5" in the arrays of its damaged page
//     stream.
func TestRepairContentDataLeavesNestedNumbersInTheLimits(t *testing.T) {
	t.Parallel()

	for _, data := range []string{
		"[3 -2.5] 0 d /Span << /MCID 0 /X [.5 -1] >> BDC EMC 0 0 m 10 10 l S",
		"[3-5 2] 0 d 0 0 m 10 10 l S",
	} {
		out, tally := repairContentData(context.Background(), []byte(data), 2)
		if tally != (unknownOpsTally{}) || out != nil {
			t.Errorf("the repair of %q wrote %q, %+v", data, out, tally)
		}
	}
}

// assertNoSaturatedNumber fails t when validation reads, anywhere in data, the
// largest integer or real veraPDF gives a number it cannot read.
func assertNoSaturatedNumber(t *testing.T, data []byte) {
	t.Helper()

	ops, err := content.ParseStreamData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseStreamData: %v", err)
	}
	var walk func(ast.PdfObject)
	walk = func(obj ast.PdfObject) {
		switch v := obj.(type) {
		case ast.PdfInt:
			if v == math.MaxInt64 || v == -math.MaxInt64 {
				t.Errorf("validation still reads the integer %d in %q", v, data)
			}
		case ast.PdfReal:
			if math.Abs(float64(v)) == math.MaxFloat64 {
				t.Errorf("validation still reads the real %g in %q", v, data)
			}
		case ast.PdfArray:
			for _, e := range v {
				walk(e)
			}
		case ast.PdfDict:
			for _, e := range v {
				walk(e)
			}
		}
	}
	for _, op := range ops {
		for _, operand := range op.Operands {
			walk(operand)
		}
	}
}
