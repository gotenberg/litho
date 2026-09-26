package compliance

import (
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// symbolicTrueTypeFont returns objects 5 to 7 of [glyphPagesObjs]: a symbolic
// TrueType font with no /Encoding and no /ToUnicode over an embedded program,
// so no code it shows has a Unicode value.
func symbolicTrueTypeFont(t *testing.T) []string {
	t.Helper()
	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	return []string{
		"<< /Type /Font /Subtype /TrueType /BaseFont /ABCDEF+Arial /FirstChar 1 /LastChar 4 /Widths [0 0 0 0] " +
			"/FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Arial /Flags 4 /Ascent 700 /Descent -200 /CapHeight 700 " +
			"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 7 0 R >>",
		exponentStream("", string(ttf)),
	}
}

// TestGlyphToUnicodeStatedPerGlyph pins that the ToUnicode presence rule is
// stated once for every shown glyph without a Unicode value, and once for a
// glyph two pages show; see [fontJudgements.glyphs]. Measured with veraPDF
// 1.30: a symbolic TrueType font with no /Encoding and no /ToUnicode fails ISO
// 19005-2, 6.2.11.7.2 t1 at PDF/A-2u once per shown code, and a Type0 font
// with no /ToUnicode fails ISO 14289-1, 7.21.7 t1 at PDF/UA-1 once per shown
// CID.
func TestGlyphToUnicodeStatedPerGlyph(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		font   []string
		bodies []string
		pdfa   bool
		want   int
	}{
		{"simple font, one page", symbolicTrueTypeFont(t), []string{glyphText("<01020304> Tj")}, true, 4},
		{"simple font, two pages", symbolicTrueTypeFont(t), []string{glyphText("<0102> Tj"), glyphText("<0201> Tj")}, true, 2},
		{"composite font, one page", subsetType0Font(t, ""), []string{glyphText("<00010002> Tj")}, false, 2},
		{"composite font, two pages", subsetType0Font(t, ""), []string{glyphText("<0001> Tj"), glyphText("<0001> Tj")}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, glyphPagesObjs(tc.font, tc.bodies...)...)
			var (
				viols []error
				err   error
				rule  Rule
			)
			if tc.pdfa {
				viols, err = ValidatePDFA(context.Background(), doc, models.PDFA2u)
				rule = Rule_19005_2_6_2_11_7_2_t01
			} else {
				viols, err = ValidatePDFUA(context.Background(), doc, models.PDFUA1)
				rule = Rule_14289_1_7_21_7_t01
			}
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if got := countRuleErrs(t, viols, rule); got != tc.want {
				t.Errorf("%s test %d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tc.want)
			}
		})
	}
}
