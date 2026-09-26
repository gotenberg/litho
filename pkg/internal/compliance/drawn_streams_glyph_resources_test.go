package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestGlyphResourcesFollowTheProfile pins that a Type3 glyph description with
// a /Resources of its own reads it in its font's stead under a profile built
// on ISO 32000-1, and its font's under PDF/A-4; see [drawnStreamWalk]. Every
// count was measured with veraPDF 1.30.
func TestGlyphResourcesFollowTheProfile(t *testing.T) {
	t.Parallel()

	gs := func(bm string) string { return "<< /ExtGState << /GS1 << /Type /ExtGState /BM " + bm + " >> >> >>" }
	glyphPage := func(fontRes, glyphRes string) []string {
		return []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			exponentStream("", "BT /F1 12 Tf (a) Tj ET"),
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.1 0 0 0.1 0 0] /CharProcs << /a 6 0 R >> " +
				"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [10] /Resources " + fontRes + " >>",
			exponentStream("/Resources "+glyphRes, "10 0 d0 /GS1 gs 0 0 5 5 re f"),
		}
	}

	cases := []struct {
		name    string
		objs    []string
		profile models.PDFAProfile
		want    int
	}{
		{"the glyph's state at 2b", glyphPage(gs("/Normal"), gs("/Foo")), models.PDFA2b, 1},
		{"the font's state at 2b", glyphPage(gs("/Foo"), gs("/Normal")), models.PDFA2b, 0},
		{"a state only the font defines at 2b", glyphPage(gs("/Foo"), "<< /ExtGState << /GS2 << /Type /ExtGState /BM /Normal >> >> >>"), models.PDFA2b, 0},
		{"the glyph's state at 4", glyphPage(gs("/Normal"), gs("/Foo")), models.PDFA4, 0},
		{"the font's state at 4", glyphPage(gs("/Foo"), gs("/Normal")), models.PDFA4, 1},
		{"a state only the font defines at 4", glyphPage(gs("/Foo"), "<< /ExtGState << /GS2 << /Type /ExtGState /BM /Normal >> >> >>"), models.PDFA4, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rule := blendModeRule(PDFAPart(string(tc.profile)))
			if got := transparencyCounts(t, tc.objs, tc.profile, rule)[0]; got != tc.want {
				t.Errorf("%s test %d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tc.want)
			}
		})
	}
}
