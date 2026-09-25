package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestType3GlyphUndefinedOperatorsFollowShownFonts pins which Type3 glyph
// procedures answer to the undefined-operator rule, ISO 19005-1, 6.2.10 t1 and
// ISO 19005-2, 6.2.2 t1: every glyph of a font a text-showing operator uses,
// once for the document, and none of a font no text shows. The font's glyphs
// /b and /c write the undefined operator zz; /b is encoded, /c is not. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixtures, which fail
// the test twice when the page shows the font, whatever it shows and however it
// renders it, and pass it when a Tf selects the font and no text shows it or
// when nothing selects it.
func TestType3GlyphUndefinedOperatorsFollowShownFonts(t *testing.T) {
	t.Parallel()

	const (
		catalog  = "<< /Type /Catalog /Pages 2 0 R >>"
		onePage  = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		twoPages = "<< /Type /Pages /Kids [3 0 R 9 0 R] /Count 2 >>"
		page     = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /T3 5 0 R >> >> /Contents 4 0 R >>"
		page2    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /T3 5 0 R >> >> /Contents 10 0 R >>"
		font     = "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
			"/CharProcs << /a 6 0 R /b 7 0 R /c 8 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
			"/FirstChar 97 /LastChar 98 /Widths [1000 1000] /Resources << >> >>"
	)
	glyph := exponentStream("", "1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f")
	glyphZZ := exponentStream("", "1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f zz")
	for _, tc := range []struct {
		name    string
		content string
		second  bool
		want    int
	}{
		{"a string shows the glyph", "BT /T3 12 Tf 10 10 Td (b) Tj ET", false, 2},
		{"a string shows another glyph", "BT /T3 12 Tf 10 10 Td (a) Tj ET", false, 2},
		{"a string shows a code the encoding does not map", "BT /T3 12 Tf 10 10 Td (z) Tj ET", false, 2},
		{"an empty string", "BT /T3 12 Tf 10 10 Td () Tj ET", false, 2},
		{"rendering mode 3", "BT 3 Tr /T3 12 Tf 10 10 Td (b) Tj ET", false, 2},
		{"the glyph shown twice", "BT /T3 12 Tf 10 10 Td (bb) Tj (b) Tj ET", false, 2},
		{"the glyph shown on two pages", "BT /T3 12 Tf 10 10 Td (b) Tj ET", true, 2},
		{"a Tf and no text", "BT /T3 12 Tf 10 10 Td ET", false, 0},
		{"nothing selects the font", "0 0 m 10 10 l S", false, 0},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_10_t01},
			{models.PDFA2b, Rule_19005_2_6_2_2_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				stream := exponentStream("", tc.content)
				objs := []string{catalog, onePage, page, stream, font, glyph, glyphZZ, glyphZZ}
				if tc.second {
					objs = []string{catalog, twoPages, page, stream, font, glyph, glyphZZ, glyphZZ, page2, stream}
				}
				doc, _ := objsDocPDF(t, objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("undefined-operator test fired %d times, want %d: %v", got, tc.want, viols)
				}
			})
		}
	}
}
