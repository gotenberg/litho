package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// cidRangeWidths is the binary width table these tests compare against. Only
// CID 1 carries a width, so a violation can only arise from a CID that reaches
// gid 1 -- either legitimately, or by the uint16 narrowing these tests pin.
var cidRangeWidths = map[uint16]float64{1: 600}

// TestCheckCIDFontWidths_OutOfRangeCIDReportsNothing pins that a /W entry
// naming a CID outside the 16-bit space is ignored rather than aliased.
//
// Measured before the bound, with usedCIDs nil: /W [65537 65537 250] and
// /W [65537 [250]] each produced one violation reading "CID 65537 width
// mismatch (PDF=250.0, font=600.0)". uint16(65537) is 1, so the check compared
// the declared width against CID 1's metrics and attributed the result to a CID
// no CIDFont can address. The range test was fused into the usedCIDs clause, so
// a nil usage map skipped it along with the usage filter.
func TestCheckCIDFontWidths_OutOfRangeCIDReportsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	for _, tc := range []struct {
		name string
		w    ast.PdfArray
	}{
		{"range form", ast.PdfArray{ast.PdfInt(65537), ast.PdfInt(65537), ast.PdfReal(250.0)}},
		{"individual form", ast.PdfArray{ast.PdfInt(65537), ast.PdfArray{ast.PdfReal(250.0)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dict := ast.PdfDict{"W": tc.w}
			got := checkCIDFontWidths(ctx, nopDoc(), dict, cidRangeWidths, 1000, "TestCID", 1, rule, nil)
			if len(got) != 0 {
				t.Errorf("CID 65537 produced %d violation(s), want none: %v", len(got), got)
			}
		})
	}
}

// TestCheckCIDFontWidths_LegalCIDStillReported is the control. A bound that
// dropped every entry would satisfy the test above while silently disabling the
// width check, so this pins that a CID inside the space still reports, and that
// the violation names that CID rather than another.
func TestCheckCIDFontWidths_LegalCIDStillReported(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	for _, tc := range []struct {
		name string
		w    ast.PdfArray
	}{
		{"range form", ast.PdfArray{ast.PdfInt(1), ast.PdfInt(1), ast.PdfReal(250.0)}},
		{"individual form", ast.PdfArray{ast.PdfInt(1), ast.PdfArray{ast.PdfReal(250.0)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dict := ast.PdfDict{"W": tc.w}
			got := checkCIDFontWidths(ctx, nopDoc(), dict, cidRangeWidths, 1000, "TestCID", 1, rule, nil)
			if len(got) != 1 {
				t.Fatalf("CID 1 produced %d violation(s), want 1: the bound rejects a legal CID", len(got))
			}
			if msg := got[0].Error(); !strings.Contains(msg, "CID 1 ") {
				t.Errorf("violation names the wrong CID: %s", msg)
			}
		})
	}
}

// TestCheckCIDFontWidths_LastLegalCIDStillWalked pins the edge of the bound:
// 65535 is addressable, so a range ending there must still be inspected.
func TestCheckCIDFontWidths_LastLegalCIDStillWalked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	// A range covering the whole space reaches CID 1, whose declared 250
	// disagrees with the binary 600.
	dict := ast.PdfDict{"W": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(maxCID), ast.PdfReal(250.0)}}
	got := checkCIDFontWidths(ctx, nopDoc(), dict, cidRangeWidths, 1000, "TestCID", 1, rule, nil)
	if len(got) != 1 {
		t.Fatalf("a 0..%d range produced %d violation(s), want 1: the bound cuts the space short",
			maxCID, len(got))
	}
}
