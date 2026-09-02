package document

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestPDFDocEncodingEuroSign pins the character at 0xA0.
//
// ISO 32000-1, Table D.2 places EURO SIGN there in PDFDocEncoding, where
// Latin-1 has NO-BREAK SPACE. PDFDocEncoding has no no-break space at all: the
// typographic characters already occupy 0x80-0x9E, so the euro went to 0xA0.
// Passing the byte through as U+00A0 turned a euro into a space: pdfjs
// bug858128.pdf carries one in its /Title, and pdf.js, poppler and pikepdf all
// read it as the euro sign.
func TestPDFDocEncodingEuroSign(t *testing.T) {
	t.Parallel()

	got := DecodePdfString(ast.PdfString([]byte{0xA0}))
	if got != "€" {
		t.Errorf("0xA0 decoded to %q (%U), want the euro sign", got, []rune(got))
	}
}

// TestPDFDocEncodingUpperRangeUnchanged is the control: every other byte above
// 0xA0 still maps to its own code point, as Latin-1 does.
func TestPDFDocEncodingUpperRangeUnchanged(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		b    byte
		want string
	}{
		{0xA1, "¡"}, // INVERTED EXCLAMATION MARK
		{0xE9, "é"}, // LATIN SMALL LETTER E WITH ACUTE
		{0xFF, "ÿ"}, // LATIN SMALL LETTER Y WITH DIAERESIS
	} {
		if got := DecodePdfString(ast.PdfString([]byte{tc.b})); got != tc.want {
			t.Errorf("%#x decoded to %q, want %q", tc.b, got, tc.want)
		}
	}
}

// TestPDFDocEncodingLowRangeUnchanged is the control for the typographic block,
// which the euro must not disturb.
func TestPDFDocEncodingLowRangeUnchanged(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		b    byte
		want string
	}{
		{0x80, "•"}, // BULLET
		{0x92, "™"}, // TRADE MARK SIGN
		{0x9E, "ž"}, // LATIN SMALL LETTER Z WITH CARON
	} {
		if got := DecodePdfString(ast.PdfString([]byte{tc.b})); got != tc.want {
			t.Errorf("%#x decoded to %q, want %q", tc.b, got, tc.want)
		}
	}
}

// TestPDFDocEncodingEuroInAString covers the shape the corpus actually carries:
// the byte sits inside a longer title rather than standing alone.
func TestPDFDocEncodingEuroInAString(t *testing.T) {
	t.Parallel()

	// The bytes around the euro in pdfjs bug858128.pdf's /Title.
	raw := []byte{'T', 'h', 'e', ' ', 0xE2, 0xA0, 0x1F, 'u', 'n', 'k'}
	got := DecodePdfString(ast.PdfString(raw))
	want := "The â€˜unk"
	if got != want {
		t.Errorf("decoded %q, want %q", got, want)
	}
}
