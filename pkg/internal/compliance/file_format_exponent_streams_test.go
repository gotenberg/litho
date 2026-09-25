package compliance

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// exponentStream writes an indirect stream object body holding data.
func exponentStream(dict, data string) string {
	return "<< " + dict + " /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream"
}

// TestExponentOperandsInEveryWalkedStream pins that a number written with an
// exponent where an operand stands answers to the undefined-operator rule,
// ISO 19005-1, 6.2.10 t1 and ISO 19005-2, 6.2.2 t1, in every content stream
// that rule is stated for, not only in the page's own content. Measured with
// veraPDF 1.30 on one-page fixtures at PDF/A-1b and PDF/A-2b: "1.5e1 w" in a
// painted form XObject, a selected tiling pattern, a Type3 glyph or an
// annotation's normal appearance fails the test once, a form painted twice and
// an appearance two annotations share fail it once, and "15 w" passes it.
func TestExponentOperandsInEveryWalkedStream(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		formDic = "/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << >>"
		annot   = "<< /Type /Annot /Subtype /Square /Rect [0 0 100 100] /F 4 /AP << /N 6 0 R >> >>"
	)
	page := func(resources, extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources " + resources + extra + " >>"
	}
	cases := []struct {
		name string
		objs func(width string) []string
	}{
		{"a form XObject", func(width string) []string {
			return []string{page("<< /XObject << /Fm0 5 0 R >> >>", ""), exponentStream("", "/Fm0 Do"),
				exponentStream(formDic, width+" 0 0 m 10 10 l S")}
		}},
		{"a form XObject painted twice", func(width string) []string {
			return []string{page("<< /XObject << /Fm0 5 0 R >> >>", ""), exponentStream("", "/Fm0 Do /Fm0 Do"),
				exponentStream(formDic, width+" 0 0 m 10 10 l S")}
		}},
		{"a tiling pattern", func(width string) []string {
			return []string{page("<< /Pattern << /P0 5 0 R >> >>", ""),
				exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"),
				exponentStream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] "+
					"/XStep 10 /YStep 10 /Resources << >>", width+" 0 0 m 10 10 l S")}
		}},
		{"a Type3 glyph", func(width string) []string {
			return []string{page("<< /Font << /T0 5 0 R >> >>", ""),
				exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"),
				"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0] " +
					"/CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> " +
					"/FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
				exponentStream("", "1000 0 d0 "+width+" 0 0 m 10 10 l S")}
		}},
		{"an annotation appearance", func(width string) []string {
			return []string{page("<< >>", " /Annots [5 0 R]"), exponentStream("", "0 0 m 10 10 l S"), annot,
				exponentStream(formDic, width+" 0 0 m 10 10 l S")}
		}},
		{"an appearance two annotations share", func(width string) []string {
			return []string{page("<< >>", " /Annots [5 0 R 7 0 R]"), exponentStream("", "0 0 m 10 10 l S"), annot,
				exponentStream(formDic, width+" 0 0 m 10 10 l S"), annot}
		}},
	}

	for _, tc := range cases {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_10_t01},
			{models.PDFA2b, Rule_19005_2_6_2_2_t01},
		} {
			for _, w := range []struct {
				width string
				want  int
			}{{"1.5e1 w", 1}, {"15 w", 0}} {
				t.Run(tc.name+" "+strings.Fields(w.width)[0]+" "+string(p.profile), func(t *testing.T) {
					t.Parallel()
					ctx := context.Background()
					objs := append([]string{catalog, pages}, tc.objs(w.width)...)
					doc, _ := objsDocPDF(t, objs...)
					viols, err := ValidatePDFA(ctx, doc, p.profile)
					if err != nil {
						t.Fatalf("ValidatePDFA: %v", err)
					}
					if got := countRuleErrs(t, viols, p.rule); got != w.want {
						t.Errorf("%s test %d fired %d times, want %d: %v", p.rule.Clause, p.rule.TestNumber, got, w.want, viols)
					}
				})
			}
		}
	}
}
