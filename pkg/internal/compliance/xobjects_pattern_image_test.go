package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPatternImageInterpolate pins ISO 19005-2, 6.2.8 t3 and ISO 19005-1, 6.2.4
// t3 on an image a tiling pattern draws: the pattern's content paints wherever
// a scn selects it (ISO
// 32000-1, 8.7.3.1), so its image is used and its /Interpolate true is judged,
// as on pdfjs bug1771477.pdf. A pattern the page declares but never selects
// paints nothing and is not judged. Every case was measured against veraPDF.
func TestPatternImageInterpolate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const img = "\x80\x80\x80\x80"
	const cell = "/Im0 Do"
	for _, tt := range []struct {
		name    string
		content string
		want    int
	}{
		{"a pattern the page selects", "/Pattern cs /P0 scn 0 0 100 100 re f", 1},
		{"a pattern the page never selects", "0 0 100 100 re S", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Pattern << /P0 5 0 R >> >> >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.content), tt.content),
				fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
					" /Resources << /XObject << /Im0 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(cell), cell),
				fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace [/CalGray << /WhitePoint [0.95 1 1.09] >>]"+
					" /BitsPerComponent 8 /Interpolate true /Length %d >>\nstream\n%s\nendstream", len(img), img),
			)
			for _, p := range []struct {
				profile models.PDFAProfile
				rule    Rule
			}{{models.PDFA1b, Rule_19005_1_6_2_4_t03}, {models.PDFA2b, Rule_19005_2_6_2_8_t03}} {
				viols, err := ValidatePDFA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tt.want {
					t.Errorf("%s %s t%d fired %d times, want %d", p.profile, p.rule.Clause, p.rule.TestNumber, got, tt.want)
				}
			}
		})
	}
}
