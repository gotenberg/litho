package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestNotdefGlyph_DELIsUndefinedWithoutAProgram pins that DEL earns no
// exemption from the .notdef rule when the font embeds no program.
//
// ISO 14289-1, 7.21.8 t1 asks that no text-showing operator reference the
// .notdef glyph, and veraPDF names a glyph .notdef by reading
// predefinedEncoding[code] in Encoding.getName, which carves out nothing for
// 0x7F. None of the four predefined tables of ISO 32000-1, Annex D defines a
// glyph at DEL, so a code the declared encoding leaves undefined selects no
// glyph. The exemption only makes sense for an embedded program, which can
// supply a glyph the encoding omits (ISO 32000-1, 9.6.6.2); this branch runs
// exactly when no program is embedded.
//
// The symbolic exemption is held alongside, since a symbolic font resolves
// codes through its own built-in encoding.
//
// # Corpus evidence
//
//   - govdocs thread_000_000153.pdf: page 8 shows code 127 through an
//     unembedded NewYork declared /MacRomanEncoding, and veraPDF reports
//     7.21.8 t1 while litho stayed silent.
func TestNotdefGlyph_DELIsUndefinedWithoutAProgram(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		code     byte
		flags    int
		encoding string
		fires    bool
	}{
		{"DEL under MacRomanEncoding", 0x7F, 32, "/MacRomanEncoding", true},
		// ISO 32000-1, Annex D.2 maps every unused WinAnsiEncoding code above
		// octal 40 to bullet, so DEL selects a glyph there and stays silent.
		// Measured: veraPDF reports the MacRoman fixture and passes the
		// WinAnsi one built the same way.
		{"DEL under WinAnsiEncoding", 0x7F, 32, "/WinAnsiEncoding", false},
		{"DEL in a symbolic font", 0x7F, 4, "/MacRomanEncoding", false},
		{"a code the table defines", 'A', 32, "/MacRomanEncoding", false},
		{"DEL with no named encoding", 0x7F, 32, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%02X> Tj ET", tc.code)
			encoding := ""
			if tc.encoding != "" {
				encoding = " /Encoding " + tc.encoding
			}
			data := cyclePDF(
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
					"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /NewYork /FirstChar 0 /LastChar 255"+
					" /Widths [%s] /FontDescriptor 6 0 R%s >>", widths256(), encoding),
				fmt.Sprintf("<< /Type /FontDescriptor /FontName /NewYork /Flags %d /FontBBox [0 0 1000 1000]"+
					" /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>", tc.flags),
			)

			if got := firesRuleUA(t, data, models.PDFUA1, "7.21.8", 1); got != tc.fires {
				t.Errorf("7.21.8 t1 fired = %v, want %v for code %d", got, tc.fires, tc.code)
			}
		})
	}
}

// widths256 returns a /Widths array covering every byte code.
func widths256() string {
	out := make([]byte, 0, 256*4)
	for range 256 {
		out = append(out, "500 "...)
	}
	return string(out)
}
