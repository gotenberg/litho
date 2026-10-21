package typography

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestComputeLineGap_ZeroWidthGlyph pins that the gap after a glyph the font
// gives no advance is measured from the glyph's own position, while a
// negative width, which no glyph is drawn with, still takes half an em.
func TestComputeLineGap_ZeroWidthGlyph(t *testing.T) {
	t.Parallel()

	curr := models.TextElement{X: 13, FontSize: 10}
	if got := computeLineGap(models.TextElement{X: 10, Width: 0, FontSize: 10}, curr); got != 3 {
		t.Errorf("zero width: gap = %v, want 3", got)
	}
	if got := computeLineGap(models.TextElement{X: 10, Width: 2, FontSize: 10}, curr); got != 1 {
		t.Errorf("width 2: gap = %v, want 1", got)
	}
	if got := computeLineGap(models.TextElement{X: 10, Width: -1, FontSize: 10}, curr); got != -2 {
		t.Errorf("negative width: gap = %v, want -2 (half an em)", got)
	}
}

// TestGlyphWidth_NoMetricsIsNeverZero pins the premise [computeLineGap]
// rests on: a font that gives a code no width at all, of any subtype, still
// yields a non-zero advance, so a text element of width 0 always means a
// width the font states. A code the standard 14 metrics give no width in a
// font that embeds no program takes the /MissingWidth, a width ISO 32000-1,
// 9.6.2.1, Table 111 states, 0 by default, so the Type 1 face here embeds
// one.
func TestGlyphWidth_NoMetricsIsNeverZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Font /Subtype /CIDFontType2 /BaseFont /NoMetrics " +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>\nendobj\n" +
		"4 0 obj\n<< /Type /FontDescriptor /FontName /NoMetrics /Flags 32 /FontFile 5 0 R >>\nendobj\n" +
		"5 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	dicts := map[string]ast.PdfDict{
		"Type1 unknown face": {
			"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("NoMetrics"),
			"FontDescriptor": ast.PdfIndirectRef{ObjectNum: 4},
		},
		"TrueType no name": {"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType")},
		"Type3 no widths": {
			"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type3"),
			"FontMatrix": ast.PdfArray{ast.PdfReal(0.001), ast.PdfInt(0), ast.PdfInt(0), ast.PdfReal(0.001), ast.PdfInt(0), ast.PdfInt(0)},
		},
		"Type0 no /W or /DW": {
			"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type0"), "BaseFont": ast.PdfName("NoMetrics"),
			"Encoding": ast.PdfName("Identity-H"), "DescendantFonts": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 3}},
		},
	}
	for name, dict := range dicts {
		font, err := ResolveFont(ctx, doc, dict)
		if err != nil {
			t.Fatalf("%s: ResolveFont: %v", name, err)
		}
		for _, code := range []int{0, 'a', 200} {
			if got := font.GlyphWidth(code); got <= 0 {
				t.Errorf("%s: GlyphWidth(%d) = %v, want a non-zero fallback", name, code, got)
			}
		}
	}
	if got := fallbackFont().GlyphWidth(0); got <= 0 {
		t.Errorf("fallback font: GlyphWidth(0) = %v, want a non-zero fallback", got)
	}
}
