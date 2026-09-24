package compliance

import (
	"fmt"
	"testing"
)

// TestSimpleFontEncoding_UnknownNameKeepsTheBuiltInEncoding pins what an
// /Encoding name outside Table 112 does to a simple font's glyph names.
//
// ISO 32000-1, Table 114 leaves the font program's built-in encoding in force
// wherever the font dictionary selects no base encoding, and a name no
// predefined encoding carries selects nothing. Reading such a name as an empty
// encoding left every code unnamed, so no glyph could be credited with a
// Unicode value and ISO 14289-1, 7.21.7 t1 fired on each one.
//
// The two encodings that already worked are held alongside it so the fallback
// stays confined to the unknown name.
//
// # Corpus evidence
//
//   - pdfjs bug859204.pdf: writes /Encoding /NULL over an embedded Type 1
//     whose built-in names are all in the Adobe Glyph List. Measured against
//     veraPDF, it passes all eleven codes the page shows, where litho reported
//     7.21.7 t1. The fixture declares the same /Flags 6, a symbolic serif font.
func TestSimpleFontEncoding_UnknownNameKeepsTheBuiltInEncoding(t *testing.T) {
	t.Parallel()

	program := type1NamedGlyphProgram(type1EncodingVector, type1EncodingGlyphs...)

	for _, tc := range []struct {
		name     string
		encoding string
	}{
		{"an /Encoding name no predefined encoding carries", " /Encoding /NULL"},
		{"a predefined /Encoding name", " /Encoding /WinAnsiEncoding"},
		{"no /Encoding at all", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const body = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
			// /Flags 6 declares a symbolic serif font.
			data := cyclePDF(
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
					"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Test /FirstChar 65 /LastChar 65"+
					" /Widths [722]%s /FontDescriptor 6 0 R >>", tc.encoding),
				"<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags 6 /FontBBox [0 0 1000 1000]"+
					" /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile 7 0 R >>",
				fmt.Sprintf("<< /Length %d /Length1 %d /Length2 %d /Length3 %d >>\nstream\n%s\nendstream",
					len(program.data), program.clear, program.eexec,
					len(program.data)-program.clear-program.eexec, program.data),
			)

			if firesUnicodeRule(t, data) {
				t.Error("7.21.7 t1 fired on code 65, which the program's built-in encoding names A")
			}
		})
	}
}
