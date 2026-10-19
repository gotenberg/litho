package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestQNestingInEveryWalkedStream pins that the q/Q nesting limit, ISO 19005-1,
// 6.1.12 t8 and ISO 19005-2, 6.1.13 t8, judges every content stream a page
// draws, each on its own nesting, and not the page's content alone. Measured
// with veraPDF 1.30.2 at PDF/A-1b and PDF/A-2b on one-page fixtures: 33 levels
// in a painted form XObject, a selected tiling pattern, a Type 3 glyph or an
// annotation's normal appearance fail the test five times, once for each q
// past the 28th; 30 unclosed q in a pattern cell fail it twice; a 29-level
// form painted twice fails it once; and a page 20 levels deep painting a form
// 15 levels deep fails nothing, since each stream starts its own count.
// PDF/A-4 states no such limit.
func TestQNestingInEveryWalkedStream(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		formDic = "/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << >>"
		tiling  = "/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>"
	)
	deep := func(levels int) string {
		return strings.Repeat("q ", levels) + "0 g 0 0 1 1 re f " + strings.Repeat("Q ", levels)
	}
	page := func(resources, extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources " + resources + extra + " >>"
	}
	cases := []struct {
		name string
		objs []string
		want int
	}{
		{"a form XObject 33 levels deep", []string{
			page("<< /XObject << /Fm0 5 0 R >> >>", ""), exponentStream("", "/Fm0 Do"),
			exponentStream(formDic, deep(33)),
		}, 5},
		{"a tiling pattern 33 levels deep", []string{
			page("<< /Pattern << /P0 5 0 R >> >>", ""), exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"),
			exponentStream(tiling, deep(33)),
		}, 5},
		{"a Type3 glyph 33 levels deep", []string{
			page("<< /Font << /T0 5 0 R >> >>", ""), exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"),
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0] " +
				"/CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> " +
				"/FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
			exponentStream("", "1000 0 d0 "+deep(33)),
		}, 5},
		{"an annotation appearance 33 levels deep", []string{
			page("<< >>", " /Annots [5 0 R]"), exponentStream("", "0 0 m 10 10 l S"),
			"<< /Type /Annot /Subtype /Square /Rect [0 0 100 100] /F 4 /AP << /N 6 0 R >> >>",
			exponentStream(formDic, deep(33)),
		}, 5},
		{"a pattern cell with 30 unclosed q", []string{
			page("<< /Pattern << /P0 5 0 R >> >>", ""), exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"),
			exponentStream(tiling, strings.Repeat("q ", 30)+"0 g 0 0 1 1 re f"),
		}, 2},
		{"a form 29 levels deep painted twice", []string{
			page("<< /XObject << /Fm0 5 0 R >> >>", ""), exponentStream("", "q /Fm0 Do Q q /Fm0 Do Q"),
			exponentStream(formDic, deep(29)),
		}, 1},
		{"a page 20 levels deep painting a form 15 levels deep", []string{
			page("<< /XObject << /Fm0 5 0 R >> >>", ""), exponentStream("", strings.Repeat("q ", 20)+"/Fm0 Do "+strings.Repeat("Q ", 20)),
			exponentStream(formDic, deep(15)),
		}, 0},
	}

	for _, tc := range cases {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
			scale   int
		}{
			{models.PDFA1b, Rule_19005_1_6_1_12_t08, 1},
			{models.PDFA2b, Rule_19005_2_6_1_13_t08, 1},
			{models.PDFA4, Rule_19005_2_6_1_13_t08, 0},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				objs := append([]string{catalog, pages}, tc.objs...)
				doc, _ := objsDocPDF(t, objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				nesting := 0
				for _, e := range viols {
					if v, ok := e.(Violation); ok && v.Rule.Object == "Op_q_gsave" {
						nesting++
					}
				}
				if want := tc.want * p.scale; nesting != want {
					t.Errorf("q/Q nesting fired %d times, want %d", nesting, want)
				}
				if p.scale == 1 {
					if got := countRuleErrs(t, viols, p.rule); got != nesting {
						t.Errorf("%s test %d fired %d of the %d nesting findings", p.rule.Clause, p.rule.TestNumber, got, nesting)
					}
				}
			})
		}
	}
}
