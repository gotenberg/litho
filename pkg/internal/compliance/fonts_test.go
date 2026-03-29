package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckSimpleFontWidths_SkipsControlChars(t *testing.T) {
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

	t.Run("space_charcode_mismatch_skipped", func(t *testing.T) {
		t.Parallel()
		// FirstChar=32, Widths=[226] → char 32 has PDF width 226 vs binary 488.
		// Should be skipped because charCode ≤ 0x20.
		fontDict := ast.PdfDict{
			"FirstChar": ast.PdfInt(32),
			"Widths":    ast.PdfArray{ast.PdfReal(226.0)},
		}
		results := checkSimpleFontWidths(ctx, nil, fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, nil)
		if len(results) != 0 {
			t.Errorf("expected no violations for space char, got %d: %v", len(results), results)
		}
	})

	t.Run("notdef_charcode_mismatch_skipped", func(t *testing.T) {
		t.Parallel()
		// FirstChar=0, Widths=[250] → char 0 has PDF width 250 vs binary 500.
		// Should be skipped because charCode ≤ 0x20.
		fontDict := ast.PdfDict{
			"FirstChar": ast.PdfInt(0),
			"Widths":    ast.PdfArray{ast.PdfReal(250.0)},
		}
		results := checkSimpleFontWidths(ctx, nil, fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, nil)
		if len(results) != 0 {
			t.Errorf("expected no violations for .notdef char, got %d: %v", len(results), results)
		}
	})

	t.Run("visible_glyph_mismatch_reported", func(t *testing.T) {
		t.Parallel()
		// FirstChar=65, Widths=[700] → char 65 ('A') has PDF width 700 vs binary 600.
		// Difference is 100 > widthTolerance (1.0), should report violation.
		fontDict := ast.PdfDict{
			"FirstChar": ast.PdfInt(65),
			"Widths":    ast.PdfArray{ast.PdfReal(700.0)},
		}
		results := checkSimpleFontWidths(ctx, nil, fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, nil)
		if len(results) == 0 {
			t.Error("expected violation for visible glyph width mismatch, got none")
		}
	})

	t.Run("visible_glyph_within_tolerance", func(t *testing.T) {
		t.Parallel()
		// FirstChar=65, Widths=[600.5] → char 65 ('A') has PDF width 600.5 vs binary 600.
		// Difference is 0.5 ≤ widthTolerance (1.0), should pass.
		fontDict := ast.PdfDict{
			"FirstChar": ast.PdfInt(65),
			"Widths":    ast.PdfArray{ast.PdfReal(600.5)},
		}
		results := checkSimpleFontWidths(ctx, nil, fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, nil, nil)
		if len(results) != 0 {
			t.Errorf("expected no violations for within-tolerance width, got %d: %v", len(results), results)
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
		results := checkCIDFontWidths(ctx, nil, cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
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
		results := checkCIDFontWidths(ctx, nil, cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
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
		results := checkCIDFontWidths(ctx, nil, cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule)
		if len(results) == 0 {
			t.Error("expected violation for CID 10 width mismatch, got none")
		}
	})
}
