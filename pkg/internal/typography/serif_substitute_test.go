package typography

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestSubstituteIsSerif pins the metrics a simple font with no /Widths takes
// to PDF.js's choice (evaluator.js, getBaseFontMetrics through isSerifFont):
// the family before the first hyphen in PDF.js's serif table, or "serif" in
// the name, which makes "SansSerif" serif. A name is read as written when
// the font has a font descriptor and normalised when it has none.
func TestSubstituteIsSerif(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		described bool
		want      bool
	}{
		{"Garamond-Bold", true, true},
		{"Georgia", true, true},
		{"SansSerif", true, true},
		{"SansSerif-Bold", true, true},
		{"MySerifFace", true, true},
		{"Monaco", false, false},
		{"Verdana-Bold", true, false},
		{"Bodoni-Italic", true, true},
		// A subset prefix is part of the family PDF.js looks up.
		{"ABCDEF+Garamond", true, false},
		// Not the first segment: PDF.js looks no further than the hyphen.
		{"Foo-Times", true, false},
		// Without a descriptor PDF.js normalises "Garamond_Bold" first.
		{"Garamond_Bold", false, true},
		{"Garamond_Bold", true, false},
	} {
		if got := substituteIsSerif(tc.name, tc.described); got != tc.want {
			t.Errorf("substituteIsSerif(%q, %v) = %v, want %v", tc.name, tc.described, got, tc.want)
		}
	}
}

// TestResolveFont_SerifFlagDoesNotPickTimes pins that the Serif flag of
// /Flags (ISO 32000-1, 9.8.2, Table 123) does not decide the metrics a font
// with no /Widths takes: PDF.js reads the name alone, so a sans name with
// the flag set takes Helvetica's widths, and SansSerif with the flag clear
// takes Times-Roman's.
func TestResolveFont_SerifFlagDoesNotPickTimes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"3 0 obj\n<< /Type /FontDescriptor /FontName /Frutiger /Flags 34 >>\nendobj\n" +
		"4 0 obj\n<< /Type /FontDescriptor /FontName /SansSerif /Flags 32 >>\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	for _, tc := range []struct {
		name string
		fd   int
		want float64
	}{
		{"Frutiger", 3, 556},  // Helvetica 'a'
		{"SansSerif", 4, 444}, // Times-Roman 'a'
	} {
		font, err := ResolveFont(ctx, doc, ast.PdfDict{
			"Type":           ast.PdfName("Font"),
			"Subtype":        ast.PdfName("Type1"),
			"BaseFont":       ast.PdfName(tc.name),
			"Encoding":       ast.PdfName("WinAnsiEncoding"),
			"FontDescriptor": ast.PdfIndirectRef{ObjectNum: tc.fd},
		})
		if err != nil {
			t.Fatalf("%s: ResolveFont: %v", tc.name, err)
		}
		if got := font.GlyphWidth('a'); got != tc.want {
			t.Errorf("%s: GlyphWidth('a') = %v, want %v", tc.name, got, tc.want)
		}
	}
}
