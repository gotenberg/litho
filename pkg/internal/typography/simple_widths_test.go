package typography

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveFont_ZeroSimpleWidthIsAWidth pins that a /Widths entry of 0
// gives its code an advance of 0. ISO 32000-1, 9.6.2.1, Table 111 gives
// /Widths the width of every code from /FirstChar to /LastChar, and
// /MissingWidth only the codes outside it; PDF.js keeps a stated 0 the same
// way. An entry that is not a number states nothing and takes /MissingWidth,
// and an entry held in an indirect object is read through it.
func TestResolveFont_ZeroSimpleWidthIsAWidth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"3 0 obj\n640\nendobj\n" +
		"4 0 obj\n<< /Type /FontDescriptor /FontName /StoneSans /Flags 32 /MissingWidth 250 >>\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	font, err := ResolveFont(ctx, doc, ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("TrueType"),
		"BaseFont":       ast.PdfName("StoneSans"),
		"FirstChar":      ast.PdfInt(32),
		"LastChar":       ast.PdfInt(35),
		"Widths":         ast.PdfArray{ast.PdfInt(0), ast.PdfInt(500), ast.PdfName("bogus"), ast.PdfIndirectRef{ObjectNum: 3}},
		"FontDescriptor": ast.PdfIndirectRef{ObjectNum: 4},
	})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	for code, want := range map[int]float64{32: 0, 33: 500, 34: 250, 35: 640, 36: 250} {
		if got := font.GlyphWidth(code); got != want {
			t.Errorf("GlyphWidth(%d) = %v, want %v", code, got, want)
		}
	}
}

// TestResolveFont_UnstatedWidthsKeepTheirFallback pins that a width litho
// fills in itself is not a stated one: a font with no /Widths takes the
// Standard 14 metrics, and a code those metrics leave at 0 still falls
// through to a non-zero width, so no text element reads as zero-width
// unless the font says so.
func TestResolveFont_UnstatedWidthsKeepTheirFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"trailer\n<< /Size 3 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	font, err := ResolveFont(ctx, doc, ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("UnknownFace"),
	})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got := font.GlyphWidth('a'); got != 556 {
		t.Errorf("GlyphWidth('a') = %v, want Helvetica's 556", got)
	}
	// Code 0 has no glyph in WinAnsiEncoding, so the injected metrics hold 0.
	if got := font.GlyphWidth(0); got <= 0 {
		t.Errorf("GlyphWidth(0) = %v, want the non-zero fallback", got)
	}
}
