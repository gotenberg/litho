package typography

import (
	"bytes"
	"context"
	"maps"
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
// Standard 14 metrics, and a code those metrics leave at 0 in a font that
// embeds a program still falls through to the program's fallback width.
func TestResolveFont_UnstatedWidthsKeepTheirFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"3 0 obj\n<< /Type /FontDescriptor /FontName /UnknownFace /Flags 32 /FontBBox [0 0 900 900] /FontFile 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	font, err := ResolveFont(ctx, doc, ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("Type1"),
		"BaseFont":       ast.PdfName("UnknownFace"),
		"FontDescriptor": ast.PdfIndirectRef{ObjectNum: 3},
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

// TestResolveFont_UnembeddedUnnamedCodeTakesMissingWidth pins that a code
// the standard 14 metrics give no width, in a simple font that states no
// /Widths and embeds no program, advances by the /MissingWidth, 0 by default
// (ISO 32000-1, 9.6.2.1, Table 111), as poppler and PDF.js read it. The
// duplicate codes of WinAnsiEncoding read the widths of the space and the
// hyphen (Annex D.2, Table D.2, notes 1 and 2).
func TestResolveFont_UnembeddedUnnamedCodeTakesMissingWidth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"3 0 obj\n<< /Type /FontDescriptor /FontName /Helvetica /Flags 32 /FontBBox [-166 -225 1000 931] /MissingWidth 300 >>\nendobj\n" +
		"4 0 obj\n<< /Type /FontDescriptor /FontName /Arial /Flags 32 /FontBBox [0 -200 1000 900] >>\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	for _, tc := range []struct {
		name  string
		dict  ast.PdfDict
		codes map[int]float64
	}{
		{
			name:  "standard encoding",
			dict:  ast.PdfDict{"BaseFont": ast.PdfName("Helvetica")},
			codes: map[int]float64{0x41: 667, 0x01: 0, 0x80: 0},
		},
		{
			name: "differences name the metrics lack",
			dict: ast.PdfDict{
				"BaseFont": ast.PdfName("Helvetica"),
				"Encoding": ast.PdfDict{"Differences": ast.PdfArray{ast.PdfInt(1), ast.PdfName("foo")}},
			},
			codes: map[int]float64{0x01: 0},
		},
		{
			name:  "stated missing width",
			dict:  ast.PdfDict{"BaseFont": ast.PdfName("Helvetica"), "FontDescriptor": ast.PdfIndirectRef{ObjectNum: 3}},
			codes: map[int]float64{0x01: 300},
		},
		{
			name:  "substitute metrics",
			dict:  ast.PdfDict{"BaseFont": ast.PdfName("Arial"), "FontDescriptor": ast.PdfIndirectRef{ObjectNum: 4}},
			codes: map[int]float64{0x41: 667, 0x01: 0},
		},
		{
			name:  "duplicate codes",
			dict:  ast.PdfDict{"BaseFont": ast.PdfName("Helvetica"), "Encoding": ast.PdfName("WinAnsiEncoding")},
			codes: map[int]float64{0xA0: 278, 0xAD: 333},
		},
		{
			name:  "courier",
			dict:  ast.PdfDict{"BaseFont": ast.PdfName("Courier")},
			codes: map[int]float64{0x01: 600},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dict := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")}
			maps.Copy(dict, tc.dict)
			font, err := ResolveFont(ctx, doc, dict)
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			for code, want := range tc.codes {
				if got := font.GlyphWidth(code); got != want {
					t.Errorf("GlyphWidth(%#x) = %v, want %v", code, got, want)
				}
			}
		})
	}
}
