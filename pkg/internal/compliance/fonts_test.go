package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckSimpleFontWidths_UsedGlyphGating(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	// Binary widths: GID 0 (.notdef)=500, GID 32 (space)=488, GID 65 ('A')=600.
	binaryWidths := map[uint16]uint16{
		0:  500,
		32: 488,
		65: 600,
	}
	var unitsPerEm uint16 = 1000
	fontDict := ast.PdfDict{
		"FirstChar": ast.PdfInt(32),
		"Widths":    ast.PdfArray{ast.PdfReal(226.0)},
	}

	t.Run("used char with mismatch fires", func(t *testing.T) {
		t.Parallel()
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, usage)
		if len(results) == 0 {
			t.Errorf("expected violation for used space char width mismatch (226 vs 488), got none")
		}
	})

	t.Run("unused char skipped", func(t *testing.T) {
		t.Parallel()
		// veraPDF's test runs per USED glyph: a mismatching width on a
		// charCode never referenced by a text operator passes.
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{65: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, usage)
		if len(results) != 0 {
			t.Errorf("expected no violations for unused char, got %d: %v", len(results), results)
		}
	})

	t.Run("nil usage skipped", func(t *testing.T) {
		t.Parallel()
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, nil)
		if len(results) != 0 {
			t.Errorf("expected no violations without usage tracking, got %d: %v", len(results), results)
		}
	})

	t.Run("invisible-only usage skipped", func(t *testing.T) {
		t.Parallel()
		// renderingMode 3 (invisible) exempts the font per veraPDF's test.
		usage := &fontUsageInfo{visible: false, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, usage)
		if len(results) != 0 {
			t.Errorf("expected no violations for invisible-only usage, got %d: %v", len(results), results)
		}
	})

	t.Run("missing Widths passes", func(t *testing.T) {
		t.Parallel()
		// veraPDF: widthFromDictionary == null PASSES the width rule; the
		// missing key fails the font-dictionary key rules instead.
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), ast.PdfDict{"FirstChar": ast.PdfInt(32)}, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, usage)
		if len(results) != 0 {
			t.Errorf("expected no violations for missing /Widths, got %d: %v", len(results), results)
		}
	})
}

func TestCheckCIDFontWidths_SkipsNotdef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	binaryWidths := map[uint16]uint16{
		0:  500,
		1:  600,
		10: 700,
	}
	var unitsPerEm uint16 = 1000

	t.Run("cid0_array_format_skipped", func(t *testing.T) {
		t.Parallel()
		// /W [0 [250]] → CID 0 has PDF width 250 vs binary 500. Should be skipped.
		cidFontDict := ast.PdfDict{
			"W": ast.PdfArray{
				ast.PdfInt(0),
				ast.PdfArray{ast.PdfReal(250.0)},
			},
		}
		results := checkCIDFontWidths(ctx, nopDoc(), cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violations for CID 0, got %d: %v", len(results), results)
		}
	})

	t.Run("cid0_range_format_skipped", func(t *testing.T) {
		t.Parallel()
		// /W [0 0 250] → CID 0..0 has PDF width 250 vs binary 500. Should be skipped.
		cidFontDict := ast.PdfDict{
			"W": ast.PdfArray{
				ast.PdfInt(0),
				ast.PdfInt(0),
				ast.PdfReal(250.0),
			},
		}
		results := checkCIDFontWidths(ctx, nopDoc(), cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violations for CID 0 range, got %d: %v", len(results), results)
		}
	})

	t.Run("nonzero_cid_mismatch_reported", func(t *testing.T) {
		t.Parallel()
		// /W [10 [800]] → CID 10 has PDF width 800 vs binary 700. Should report.
		cidFontDict := ast.PdfDict{
			"W": ast.PdfArray{
				ast.PdfInt(10),
				ast.PdfArray{ast.PdfReal(800.0)},
			},
		}
		results := checkCIDFontWidths(ctx, nopDoc(), cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) == 0 {
			t.Error("expected violation for CID 10 width mismatch, got none")
		}
	})
}
