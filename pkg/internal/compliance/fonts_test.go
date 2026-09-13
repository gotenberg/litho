package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestCheckFormXObjectFonts_NonEmbeddedFontInForm pins that a font used only
// inside a Do-invoked Form XObject is reached by the font checks: a
// non-embedded font selected in the form's content must fire the embedding rule
// (ISO 19005-2 §6.2.11.4.1), matching veraPDF's descent into the form content.
func TestCheckFormXObjectFonts_NonEmbeddedFontInForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 2 is the Form XObject (its content selects and shows /F1); object 3
	// is a non-embedded Helvetica (no /FontDescriptor).
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 3 0 R >> >> /Length 15 >>\nstream\n/F1 12 Tf (x)Tj\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)

	// The page content invokes the form via Do.
	ops := []content.Operation{{Operator: "Do", Operands: []ast.PdfObject{ast.PdfName("Fm0")}}}
	res := ast.PdfDict{"XObject": ast.PdfDict{"Fm0": ast.PdfIndirectRef{ObjectNum: 2}}}

	results := checkFormXObjectFonts(ctx, doc, ops, res, nil, 1, "PDF/A-2b", map[int]bool{}, map[int]bool{}, 0)
	if !hasRule(results, fontEmbeddingRule("PDF/A-2b")) {
		t.Errorf("non-embedded font used inside a Form XObject should fire the embedding rule, got %v", results)
	}
}

// TestCheckAnnotationAppearanceFonts_NonEmbeddedFont pins that a font used only
// inside an annotation appearance stream is checked with the full font suite: a
// non-embedded font shown in a widget appearance must fire the embedding rule
// (ISO 19005-2 §6.2.11.4.1), matching veraPDF's descent into the appearance.
func TestCheckAnnotationAppearanceFonts_NonEmbeddedFont(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 3 is the page (one Widget annot); object 5 is the /N appearance
	// stream selecting /F1; object 6 is a non-embedded Helvetica.
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 100 20] /AP << /N 5 0 R >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Resources << /Font << /F1 6 0 R >> >> /Length 15 >>\nstream\n/F1 12 Tf (x)Tj\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}

	results := checkAnnotationAppearanceFonts(ctx, doc, page, 1, "PDF/A-2b", map[int]bool{})
	if !hasRule(results, fontEmbeddingRule("PDF/A-2b")) {
		t.Errorf("non-embedded font used inside an annotation appearance should fire the embedding rule, got %v", results)
	}
}

func TestCheckSimpleFontWidths_UsedGlyphGating(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	// Binary widths: GID 0 (.notdef)=500, GID 32 (space)=488, GID 65 ('A')=600.
	binaryWidths := map[uint16]float64{
		0:  500,
		32: 488,
		65: 600,
	}
	var unitsPerEm uint16 = 1000
	// /Widths spans codes 32 to 65. Index 0 declares the space at a
	// deliberately wrong 226, and index 33 declares 'A' at its true 600 so the
	// subtests that show 'A' exercise usage gating alone: a shown code the
	// array did not cover would take /MissingWidth (ISO 32000-1, Table 111)
	// and mismatch on that instead.
	widths := make(ast.PdfArray, 34)
	for i := range widths {
		widths[i] = ast.PdfReal(0)
	}
	widths[0] = ast.PdfReal(226.0)
	widths[33] = ast.PdfReal(600)
	fontDict := ast.PdfDict{
		"FirstChar": ast.PdfInt(32),
		"Widths":    widths,
	}
	// Explicit code→GID mapping. The check no longer assumes identity for a
	// nil map (an unresolved cmap must not misread a proportional font), so the
	// usage-gating cases below supply the mapping they exercise.
	identMap := map[uint16]uint16{32: 32, 65: 65}

	t.Run("used char with mismatch fires", func(t *testing.T) {
		t.Parallel()
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, identMap, usage)
		if len(results) == 0 {
			t.Errorf("expected violation for used space char width mismatch (226 vs 488), got none")
		}
	})

	t.Run("unused char skipped", func(t *testing.T) {
		t.Parallel()
		// ISO 19005-1 §6.3.6 scopes the consistency requirement to glyphs
		// "used for rendering". A charCode never referenced by a text
		// operator is not used for rendering, so a width mismatch on it
		// passes.
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{65: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, identMap, usage)
		if len(results) != 0 {
			t.Errorf("expected no violations for unused char, got %d: %v", len(results), results)
		}
	})

	t.Run("nil usage skipped", func(t *testing.T) {
		t.Parallel()
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, identMap, nil)
		if len(results) != 0 {
			t.Errorf("expected no violations without usage tracking, got %d: %v", len(results), results)
		}
	})

	t.Run("invisible-only usage skipped", func(t *testing.T) {
		t.Parallel()
		// Text rendering mode 3 paints no glyphs, so nothing is "used for
		// rendering" under ISO 19005-1 §6.3.6 and the font is exempt.
		usage := &fontUsageInfo{visible: false, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, unitsPerEm, "TestFont", 1, rule, identMap, usage)
		if len(results) != 0 {
			t.Errorf("expected no violations for invisible-only usage, got %d: %v", len(results), results)
		}
	})

	// A font with no /Widths still declares a width for every code it shows:
	// ISO 32000-1, 9.6.2.1 and Table 111 give an uncovered code the
	// descriptor's /MissingWidth, default 0. The rule is not vacuous there, it
	// compares the program advance against that default, which is what veraPDF
	// does (pdfjs issue11150_reduced.pdf, ISO 19005-2 §6.2.11.5 t1). The
	// missing-key rules report the absent array itself and say nothing about
	// the widths it would have held.
	t.Run("missing Widths compares against MissingWidth", func(t *testing.T) {
		t.Parallel()
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), ast.PdfDict{"FirstChar": ast.PdfInt(32)}, binaryWidths, unitsPerEm, "TestFont", 1, rule, identMap, usage)
		if len(results) == 0 {
			t.Error("expected a violation: no /Widths and no /MissingWidth declares 0 against a 488-unit glyph")
		}
	})

	t.Run("missing Widths matching MissingWidth passes", func(t *testing.T) {
		t.Parallel()
		fd := ast.PdfDict{
			"FirstChar":      ast.PdfInt(32),
			"FontDescriptor": ast.PdfDict{"MissingWidth": ast.PdfReal(488)},
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
		results := checkSimpleFontWidths(ctx, nopDoc(), fd, binaryWidths, unitsPerEm, "TestFont", 1, rule, identMap, usage)
		if len(results) != 0 {
			t.Errorf("/MissingWidth 488 matches the glyph advance, got %d: %v", len(results), results)
		}
	})
}

// TestCheckSimpleFontWidths_UnresolvedMappingSkipped pins that a nil code→GID
// map (the font's cmap used a format litho does not parse, e.g. a format-2 CJK
// subtable, and no glyph names were available) skips the width check instead of
// assuming identity. Identity misreads a proportional font: code 32 would read
// GID 32's advance (488) rather than the cmap's space glyph, a false positive
// veraPDF does not raise (pdfjs issue14117 @PDF/A-3a).
func TestCheckSimpleFontWidths_UnresolvedMappingSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01
	binaryWidths := map[uint16]float64{0: 500, 32: 488}
	fontDict := ast.PdfDict{"FirstChar": ast.PdfInt(32), "Widths": ast.PdfArray{ast.PdfReal(226.0)}}
	usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
	results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths, 1000, "TestFont", 1, rule, nil, usage)
	if len(results) != 0 {
		t.Errorf("nil code→GID map must skip (identity is a wrong guess), got %d: %v", len(results), results)
	}
}

func TestCheckCIDFontWidths_SkipsNotdef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	binaryWidths := map[uint16]float64{
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
		results := checkCIDFontWidths(ctx, nopDoc(), cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule, nil)
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
		results := checkCIDFontWidths(ctx, nopDoc(), cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule, nil)
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
		results := checkCIDFontWidths(ctx, nopDoc(), cidFontDict, binaryWidths, unitsPerEm, "TestCID", 1, rule, nil)
		if len(results) == 0 {
			t.Error("expected violation for CID 10 width mismatch, got none")
		}
	})
}
