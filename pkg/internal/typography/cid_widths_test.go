package typography

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveFont_CIDWidthsThroughIndirectArray pins that a /W array whose
// individual form holds its widths in an indirect array, the shape of pdfjs
// issue16300.pdf, gives those widths to the CIDs it covers rather than /DW,
// and that a /W2 array is read the same way.
func TestResolveFont_CIDWidthsThroughIndirectArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"3 0 obj\n[600 700]\nendobj\n" +
		"4 0 obj\n[-900 250 880]\nendobj\n" +
		"5 0 obj\n<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Nunito-Bold " +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> " +
		"/DW 1000 /W [1 3 0 R] /W2 [1 4 0 R] >>\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	font, err := ResolveFont(ctx, doc, ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName("Nunito-Bold"),
		"Encoding":        ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}},
	})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	for cid, want := range map[int]float64{1: 600, 2: 700, 3: 1000} {
		if got := font.GlyphWidth(cid); got != want {
			t.Errorf("GlyphWidth(%d) = %v, want %v", cid, got, want)
		}
	}
	if w1, vx, vy := font.VerticalMetrics(1); w1 != -900 || vx != 250 || vy != 880 {
		t.Errorf("VerticalMetrics(1) = %v %v %v, want -900 250 880", w1, vx, vy)
	}
}

// TestGlyphWidth_StatedDWWinsOverTheFontProgram pins that a CID /W omits
// takes the /DW the CIDFont states (ISO 32000-1, 9.7.4.3) rather than the
// advance the font program gives the glyph whose index equals the CID, which
// a /CIDToGIDMap stream makes another glyph (pdfjs issue19494.pdf). Without a
// stated /DW the program's advance still stands in for the implied 1000.
func TestGlyphWidth_StatedDWWinsOverTheFontProgram(t *testing.T) {
	t.Parallel()

	build := func(stated bool) *Font {
		return &Font{
			Subtype:      "Type0",
			CIDWidths:    map[int]float64{1: 278},
			MissingWidth: map[bool]float64{true: 556, false: 1000}[stated],
			DWStated:     stated,
			HmtxWidths:   map[uint16]uint16{5: 600},
			UnitsPerEm:   2048,
		}
	}
	if got := build(true).GlyphWidth(5); got != 556 {
		t.Errorf("stated /DW 556: GlyphWidth(5) = %v, want 556", got)
	}
	if got, want := build(false).GlyphWidth(5), 600*1000.0/2048; got != want {
		t.Errorf("implied /DW: GlyphWidth(5) = %v, want the program's %v", got, want)
	}
	if got := build(true).GlyphWidth(1); got != 278 {
		t.Errorf("GlyphWidth(1) = %v, want the /W entry 278", got)
	}
}

// TestGlyphWidth_ZeroCIDWidthIsAWidth pins that a /W entry of 0 gives its CID
// an advance of 0 rather than /DW or the font program's advance. ISO 32000-1,
// 9.7.4.3 makes /DW the width of the CIDs /W omits, and an entry of 0 names
// the CID. Combining marks in pdfjs complex_ttf_font.pdf carry /W 0 under /DW
// 1000; reading 1000 pushed each line past the page clip and lost its tail.
func TestGlyphWidth_ZeroCIDWidthIsAWidth(t *testing.T) {
	t.Parallel()

	for _, stated := range []bool{true, false} {
		font := &Font{
			Subtype:      "Type0",
			CIDWidths:    map[int]float64{7: 0, 8: 250},
			MissingWidth: 1000,
			DWStated:     stated,
			HmtxWidths:   map[uint16]uint16{7: 1024},
			UnitsPerEm:   2048,
		}
		if got := font.GlyphWidth(7); got != 0 {
			t.Errorf("DWStated=%v: GlyphWidth(7) = %v, want the /W entry 0", stated, got)
		}
		if got := font.GlyphWidth(8); got != 250 {
			t.Errorf("DWStated=%v: GlyphWidth(8) = %v, want 250", stated, got)
		}
	}
}
