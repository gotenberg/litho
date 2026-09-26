package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestLimitsFollowTheColourStateDrawnStreamsInherit pins that a form and a
// Type 3 glyph start with their colour operators ignored when the stream
// drawing them ignores its own there, and that every tiling cell of a stream
// selecting an uncoloured pattern starts so (see [drawnScan]), for the
// implementation limits that judge the operands of colour operators alone
// while the stream processes them ([ColourState]). Every case was measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixtures, which fail
// the integer limit, ISO 19005-1, 6.1.12 t1 and ISO 19005-2, 6.1.13 t1, as
// many times as the case states.
func TestLimitsFollowTheColourStateDrawnStreamsInherit(t *testing.T) {
	t.Parallel()

	const (
		big        = "99999999999 g"
		fill       = " 0 0 10 10 re f"
		uncoloured = "/CS0 cs 0.5 /P2 scn" + fill
		type3Use   = "BT /T3 12 Tf (a) Tj ET"
	)
	page := func(res string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources " + res + " >>"
	}
	tiling := func(paintType, res, cell string) string {
		return exponentStream("/Type /Pattern /PatternType 1 /PaintType "+paintType+" /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources "+res, cell)
	}
	form := func(content string) string {
		return exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", content)
	}
	type3 := func(glyph, res string) string {
		return "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.01 0 0 0.01 0 0] /CharProcs << /a " + glyph +
			" 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [10] /Resources " + res + " >>"
	}
	patternCS := "/ColorSpace << /CS0 [/Pattern /DeviceGray] >> "
	for _, tc := range []struct {
		name    string
		objs    []string
		content string
		integer int
	}{
		{
			name:    "g in a coloured cell alone",
			objs:    []string{page("<< /Pattern << /P1 5 0 R >> >>"), tiling("1", "<< >>", big+fill)},
			content: "/Pattern cs /P1 scn" + fill, integer: 1,
		},
		{
			name:    "g in a coloured cell of a page selecting an uncoloured pattern",
			objs:    []string{page("<< " + patternCS + "/Pattern << /P1 5 0 R /P2 6 0 R >> >>"), tiling("1", "<< >>", big+fill), tiling("2", "<< >>", "0 0 5 5 re f")},
			content: "/Pattern cs /P1 scn" + fill + " " + uncoloured,
		},
		{
			name:    "g in a form an uncoloured cell paints",
			objs:    []string{page("<< " + patternCS + "/Pattern << /P2 5 0 R >> >>"), tiling("2", "<< /XObject << /Fm0 6 0 R >> >>", "/Fm0 Do"), form(big)},
			content: uncoloured,
		},
		{
			name: "g in a form a glyph paints past its d1",
			objs: []string{
				page("<< /Font << /T3 5 0 R >> >>"), type3("6", "<< /XObject << /Fm0 7 0 R >> >>"),
				exponentStream("", "10 0 0 0 10 10 d1 /Fm0 Do"), form(big),
			},
			content: type3Use,
		},
		{
			name: "g in a glyph an uncoloured cell shows",
			objs: []string{
				page("<< " + patternCS + "/Pattern << /P2 5 0 R >> >>"), tiling("2", "<< /Font << /T3 6 0 R >> >>", type3Use),
				type3("7", "<< >>"), exponentStream("", "10 0 d0 "+big),
			},
			content: uncoloured,
		},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			integer Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_12_t01},
			{models.PDFA2b, Rule_19005_2_6_1_13_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				objs := append([]string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", tc.objs[0], exponentStream("", tc.content)}, tc.objs[1:]...)
				doc, _ := objsDocPDF(t, objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.integer); got != tc.integer {
					t.Errorf("the integer limit fired %d times, want %d: %v", got, tc.integer, viols)
				}
			})
		}
	}
}
