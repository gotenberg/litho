package compliance

import (
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// type0TwoPagePDF builds a two-page PDF whose pages both select the same
// embedded Type0/CIDFontType2 font object, showing page1 then page2.
func type0TwoPagePDF(t *testing.T, page1, page2 string) []byte {
	t.Helper()
	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 8 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page1), page1),
		"<< /Type /Font /Subtype /Type0 /BaseFont /Arial /Encoding /Identity-H "+
			"/DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Arial /DW 1000 "+
			"/CIDToGIDMap /Identity /FontDescriptor 7 0 R "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 "+
			"/Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
			"/FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 10 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 9 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page2), page2),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), string(ttf)),
	)
}

// TestType0Font_UsageJudgedOnEveryPage pins that a composite font is judged
// against the glyphs of every page that selects it, not only the first.
//
// A font object's dictionary is reported once, but each page shows its own
// codes. litho gated the whole of the Type0 check on "font object not yet
// seen", so a font selected on many pages was judged against the first page's
// codes alone; simple fonts already avoid this through [checkFontUsageRules].
// govdocs thread_100_100870.pdf selects one Times-Roman Identity-H font on
// fourteen pages and only page 20 shows CID 0 and a CID its CFF charset does
// not hold, both of which veraPDF reports and litho did not.
func TestType0Font_UsageJudgedOnEveryPage(t *testing.T) {
	t.Parallel()

	// The fixture font defines 38 glyphs, so CID 0x0100 is past its end. An
	// Identity /CIDToGIDMap makes the glyph index the CID (ISO 32000-2 9.7.4.2),
	// and rendering mode 0 paints, which the glyph-presence rule requires.
	const clean = "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
	const absent = "BT /F1 12 Tf 10 50 Td <0100> Tj ET"

	cases := []struct {
		name  string
		page1 string
		page2 string
		want  bool
	}{
		{"an absent glyph on the first page fires", absent, clean, true},
		{"an absent glyph on a later page fires too", clean, absent, true},
		{"no absent glyph on either page stays silent", clean, clean, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := type0TwoPagePDF(t, tc.page1, tc.page2)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.11.4.1", 2); got != tc.want {
				t.Errorf("glyph-presence violation = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestType0Font_MalformedCodesJudgedOncePerObject pins the one Type0 rule that
// is deliberately not re-run per page.
//
// A code string that does not decode is read from the content stream itself,
// and a page whose stream decompresses only partway yields bytes zlib invented:
// pdfjs issue11678.pdf inflates one into a hex string with an odd digit count,
// which under Identity-H is indistinguishable from a rendered .notdef. veraPDF
// reports nothing there, so litho keeps judging malformed codes where it always
// has, once per font object, rather than on every page that selects it.
func TestType0Font_MalformedCodesJudgedOncePerObject(t *testing.T) {
	t.Parallel()

	const clean = "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
	// An odd-length string under Identity-H leaves a trailing byte that forms
	// no CID.
	const malformed = "BT /F1 12 Tf 10 50 Td <000102> Tj ET"

	cases := []struct {
		name  string
		page1 string
		page2 string
		want  bool
	}{
		{"a malformed code on the first page fires", malformed, clean, true},
		{"a malformed code only on a later page does not", clean, malformed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := type0TwoPagePDF(t, tc.page1, tc.page2)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.11.8", 1); got != tc.want {
				t.Errorf("notdef violation = %v, want %v", got, tc.want)
			}
		})
	}
}
