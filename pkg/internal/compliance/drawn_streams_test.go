package compliance

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// drawnBad is a content stream every content-stream rule the drawn-stream walk
// serves fails: an odd hex string (6.1.6 t1), a hex string with a non-hex
// character (6.1.6 t2), a number written with an exponent and an operator ISO
// 32000-1 does not define (the undefined-operator rule, twice) and, at PDF/A-1,
// a real past the limit (6.1.12 t2).
const drawnBad = "BT <414> Tj <4G> Tj ET 1.5e1 w zz 40000.5 0 m 10 10 l S"

// drawnPlain is a content stream no rule fails.
const drawnPlain = "0 0 m 10 10 l S"

// drawnFixture builds the objects of the drawn-stream fixtures. Object 1 is
// the catalog and object 2 the page tree.
type drawnFixture struct{}

func (drawnFixture) pages(kids ...int) string {
	refs := make([]string, len(kids))
	for i, k := range kids {
		refs[i] = strconv.Itoa(k) + " 0 R"
	}
	return fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(refs, " "), len(kids))
}

func (drawnFixture) page(resources string, contents int, extra string) string {
	return fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources %s%s >>", contents, resources, extra)
}

func (drawnFixture) form(resources, data string) string {
	return exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources "+resources, data)
}

func (drawnFixture) pattern(resources, data string) string {
	return exponentStream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] "+
		"/XStep 10 /YStep 10 /Resources "+resources, data)
}

func (drawnFixture) type3(resources string, glyph int) string {
	return "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << /a " + strconv.Itoa(glyph) + " 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> " +
		"/FirstChar 97 /LastChar 97 /Widths [1000] /Resources " + resources + " >>"
}

func (drawnFixture) annot(ap int) string {
	return "<< /Type /Annot /Subtype /Square /Rect [0 0 100 100] /F 4 /AP << /N " + strconv.Itoa(ap) + " 0 R >> >>"
}

// xobjects writes a resource dictionary naming form XObjects, name followed by
// object number.
func (drawnFixture) xobjects(entries ...string) string {
	var b strings.Builder
	b.WriteString("<< /XObject <<")
	for i := 0; i+1 < len(entries); i += 2 {
		b.WriteString(" /" + entries[i] + " " + entries[i+1] + " 0 R")
	}
	b.WriteString(" >> >>")
	return b.String()
}

// TestContentRulesInEveryDrawnStream pins the reach of the content-stream
// rules: the hex string tests, ISO 19005-1 and ISO 19005-2, 6.1.6 t1 and t2,
// the undefined-operator rule, ISO 19005-1, 6.2.10 t1 and ISO 19005-2, 6.2.2
// t1, the real limit, ISO 19005-1, 6.1.12 t2, and the inherited-resource test,
// ISO 19005-2, 6.2.2 t2.
//
// Every case was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, and
// judged counts how many times veraPDF judges [drawnBad]: each judged copy
// fails t1 and t2 once, the undefined-operator test twice and the real limit
// once. A form nested in a form, in an appearance, in a pattern or in a Type3
// glyph is judged, once for the whole document however many pages or names
// draw it, and a cycle of forms judges each once. A stream only declared, or
// drawn only by a stream nothing draws, is not judged, and a name resolves
// against the drawing stream's resources and then the page's alone.
func TestContentRulesInEveryDrawnStream(t *testing.T) {
	t.Parallel()

	var f drawnFixture
	const catalog = "<< /Type /Catalog /Pages 2 0 R >>"
	x := f.xobjects
	cases := []struct {
		name   string
		judged int
		// inherited is how many times PDF/A-2b fails 6.2.2 t2.
		inherited int
		objs      []string
	}{
		{"a form a form paints", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6"), "/Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form three levels down", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6"), "/Fm1 Do"), f.form(x("Fm2", "7"), "/Fm2 Do"), f.form("<< >>", drawnBad),
		}},
		{"a nested form only the page's resources name", 1, 1, []string{
			f.pages(3), f.page(x("Fm0", "5", "Fm1", "6"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form("<< >>", "/Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		{"a nested form only the drawing form's resources name", 0, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6", "Fm2", "7"), "/Fm1 Do"), f.form("<< >>", "/Fm2 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form a form nothing paints would paint", 0, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", drawnPlain),
			f.form(x("Fm1", "6"), "/Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		{"a bad form painting a bad form", 2, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6"), drawnBad+" /Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		{"a cycle of two forms", 2, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6"), drawnBad+" /Fm1 Do"), f.form(x("Fm0", "5"), drawnBad+" /Fm0 Do"),
		}},
		{"a form painting itself", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm0", "5"), drawnBad+" /Fm0 Do"),
		}},
		{"a form an appearance paints", 1, 0, []string{
			f.pages(3), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form(x("Fm0", "7"), "/Fm0 Do"), f.form("<< >>", drawnBad),
		}},
		{"two forms deep in an appearance", 1, 0, []string{
			f.pages(3), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form(x("Fm0", "7"), "/Fm0 Do"), f.form(x("Fm1", "8"), "/Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		// veraPDF states 6.2.2 t2 for the appearance's own /Fm0, which the page
		// alone defines.
		{"a form an appearance names through the page", 1, 1, []string{
			f.pages(3), f.page(x("Fm0", "7"), 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form("<< >>", "/Fm0 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form in an appearance naming a form through the page", 1, 1, []string{
			f.pages(3), f.page(x("Fm1", "8"), 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form(x("Fm0", "7"), "/Fm0 Do"), f.form("<< >>", "/Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form a pattern paints", 1, 0, []string{
			f.pages(3), f.page("<< /Pattern << /P0 5 0 R >> >>", 4, ""),
			exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"), f.pattern(x("Fm0", "6"), "/Fm0 Do"),
			f.form("<< >>", drawnBad),
		}},
		{"a form a stroking pattern paints", 1, 0, []string{
			f.pages(3), f.page("<< /Pattern << /P0 5 0 R >> >>", 4, ""),
			exponentStream("", "/Pattern CS /P0 SCN 0 0 m 100 100 l S"), f.pattern(x("Fm0", "6"), "/Fm0 Do"),
			f.form("<< >>", drawnBad),
		}},
		{"a form a pattern nothing selects would paint", 0, 0, []string{
			f.pages(3), f.page("<< /Pattern << /P0 5 0 R >> >>", 4, ""), exponentStream("", drawnPlain),
			f.pattern(x("Fm0", "6"), "/Fm0 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form a Type3 glyph paints", 1, 0, []string{
			f.pages(3), f.page("<< /Font << /T0 5 0 R >> >>", 4, ""),
			exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), f.type3(x("Fm0", "7"), 6),
			exponentStream("", "1000 0 d0 /Fm0 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form a glyph of a font no text shows would paint", 0, 0, []string{
			f.pages(3), f.page("<< /Font << /T0 5 0 R >> >>", 4, ""), exponentStream("", "BT /T0 12 Tf ET"),
			f.type3(x("Fm0", "7"), 6), exponentStream("", "1000 0 d0 /Fm0 Do"), f.form("<< >>", drawnBad),
		}},
		{"a glyph of a Type3 font no text shows", 0, 0, []string{
			f.pages(3), f.page("<< /Font << /T0 5 0 R >> >>", 4, ""), exponentStream("", "BT /T0 12 Tf ET"),
			f.type3("<< >>", 6), exponentStream("", "1000 0 d0 "+drawnBad),
		}},
		{"a pattern a form selects", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form("<< /Pattern << /P0 6 0 R >> >>", "/Pattern cs /P0 scn 0 0 100 100 re f"), f.pattern("<< >>", drawnBad),
		}},
		{"a pattern a form selects in the space its Do inherits", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Pattern cs /Fm0 Do"),
			f.form("<< /Pattern << /P0 6 0 R >> >>", "/P0 scn 0 0 100 100 re f"), f.pattern("<< >>", drawnBad),
		}},
		{"a Type3 glyph a form shows", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form("<< /Font << /T0 6 0 R >> >>", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), f.type3("<< >>", 7),
			exponentStream("", "1000 0 d0 "+drawnBad),
		}},
		{"a pattern an appearance selects", 1, 0, []string{
			f.pages(3), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form("<< /Pattern << /P0 7 0 R >> >>", "/Pattern cs /P0 scn 0 0 100 100 re f"), f.pattern("<< >>", drawnBad),
		}},
		{"a Type3 glyph an appearance shows", 1, 0, []string{
			f.pages(3), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form("<< /Font << /T0 7 0 R >> >>", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), f.type3("<< >>", 8),
			exponentStream("", "1000 0 d0 "+drawnBad),
		}},
		{"a form two pages paint", 1, 0, []string{
			f.pages(3, 6), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"), f.form("<< >>", drawnBad),
			f.page(x("Fm0", "5"), 4, ""),
		}},
		{"a nested form two pages paint", 1, 0, []string{
			f.pages(3, 7), f.page(x("Fm0", "5"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form(x("Fm1", "6"), "/Fm1 Do"), f.form("<< >>", drawnBad), f.page(x("Fm0", "5"), 4, ""),
		}},
		{"a form the page paints directly and through a form", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5", "Fm1", "6"), 4, ""), exponentStream("", "/Fm0 Do /Fm1 Do"),
			f.form(x("Fm1", "6"), "/Fm1 Do"), f.form("<< >>", drawnBad),
		}},
		{"a form the page paints under two names", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "5", "Fm1", "5"), 4, ""), exponentStream("", "/Fm0 Do /Fm1 Do"),
			f.form("<< >>", drawnBad),
		}},
		{"a form the page paints and a pattern paints", 1, 0, []string{
			f.pages(3), f.page("<< /XObject << /Fm0 6 0 R >> /Pattern << /P0 5 0 R >> >>", 4, ""),
			exponentStream("", "/Fm0 Do /Pattern cs /P0 scn 0 0 100 100 re f"), f.pattern(x("Fm0", "6"), "/Fm0 Do"),
			f.form("<< >>", drawnBad),
		}},
		{"a form serving as the page's appearance too", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "6"), 4, " /Annots [5 0 R]"), exponentStream("", "/Fm0 Do"), f.annot(6),
			f.form("<< >>", drawnBad),
		}},
		{"a pattern two pages select", 1, 0, []string{
			f.pages(3, 6), f.page("<< /Pattern << /P0 5 0 R >> >>", 4, ""),
			exponentStream("", "/Pattern cs /P0 scn 0 0 100 100 re f"), f.pattern("<< >>", drawnBad),
			f.page("<< /Pattern << /P0 5 0 R >> >>", 4, ""),
		}},
		{"a Type3 font two pages show", 1, 0, []string{
			f.pages(3, 7), f.page("<< /Font << /T0 5 0 R >> >>", 4, ""),
			exponentStream("", "BT /T0 12 Tf 10 10 Td (a) Tj ET"), f.type3("<< >>", 6),
			exponentStream("", "1000 0 d0 "+drawnBad), f.page("<< /Font << /T0 5 0 R >> >>", 4, ""),
		}},
		{"an appearance two pages' annotations share", 1, 0, []string{
			f.pages(3, 7), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form("<< >>", drawnBad), f.page("<< >>", 4, " /Annots [8 0 R]"), f.annot(6),
		}},
		{"an annotation two pages share", 1, 0, []string{
			f.pages(3, 7), f.page("<< >>", 4, " /Annots [5 0 R]"), exponentStream("", drawnPlain), f.annot(6),
			f.form("<< >>", drawnBad), f.page("<< >>", 4, " /Annots [5 0 R]"),
		}},
		// The page's own content is judged as such, and not again as a form.
		{"a page's content its own Do paints", 1, 0, []string{
			f.pages(3), f.page(x("Fm0", "4"), 4, ""), f.form("<< >>", drawnBad+" /Fm0 Do"),
		}},
		// The first page sets the context: its /Fm1 is the plain form, and the
		// second page's bad /Fm1 is never reached. Each page names /Fm1 only in
		// its own resources, so the form's Do is inherited, once.
		{"a form two pages paint, whose Do each page maps differently, first plain", 0, 1, []string{
			f.pages(3, 8), f.page(x("Fm0", "5", "Fm1", "6"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form("<< >>", "/Fm1 Do"), f.form("<< >>", drawnPlain), f.form("<< >>", drawnBad),
			f.page(x("Fm0", "5", "Fm1", "7"), 4, ""),
		}},
		{"a form two pages paint, whose Do each page maps differently, first bad", 1, 1, []string{
			f.pages(3, 8), f.page(x("Fm0", "5", "Fm1", "7"), 4, ""), exponentStream("", "/Fm0 Do"),
			f.form("<< >>", "/Fm1 Do"), f.form("<< >>", drawnPlain), f.form("<< >>", drawnBad),
			f.page(x("Fm0", "5", "Fm1", "6"), 4, ""),
		}},
		{"a form serving as a soft mask", 0, 0, []string{
			f.pages(3), f.page("<< /ExtGState << /GS0 5 0 R >> >>", 4, ""), exponentStream("", "/GS0 gs "+drawnPlain),
			"<< /Type /ExtGState /SMask << /Type /Mask /S /Luminosity /G 6 0 R >> >>",
			exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /CS /DeviceGray >> /Resources << >>", drawnBad),
		}},
		{"a form a page with no content declares", 0, 0, []string{
			f.pages(3), "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Fm0 4 0 R >> >> >>",
			f.form("<< >>", drawnBad),
		}},
	}

	for _, tc := range cases {
		for _, p := range []struct {
			profile                       models.PDFAProfile
			odd, invalid, operator, limit Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_6_t01, Rule_19005_1_6_1_6_t02, Rule_19005_1_6_2_10_t01, Rule_19005_1_6_1_12_t02},
			{models.PDFA2b, Rule_19005_2_6_1_6_t01, Rule_19005_2_6_1_6_t02, Rule_19005_2_6_2_2_t01, Rule{}},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, append([]string{catalog}, tc.objs...)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				want := []struct {
					rule Rule
					n    int
				}{
					{p.odd, tc.judged},
					{p.invalid, tc.judged},
					{p.operator, 2 * tc.judged},
				}
				if p.limit.Specification != "" {
					want = append(want, struct {
						rule Rule
						n    int
					}{p.limit, tc.judged})
				}
				if p.profile == models.PDFA2b {
					want = append(want, struct {
						rule Rule
						n    int
					}{Rule_19005_2_6_2_2_t02, tc.inherited})
				}
				for _, w := range want {
					if got := countRuleErrs(t, viols, w.rule); got != w.n {
						t.Errorf("%s test %d fired %d times, want %d: %v", w.rule.Clause, w.rule.TestNumber, got, w.n, viols)
					}
				}
			})
		}
	}
}

// TestDrawnStreamWalkStopsAtTheFormBudget pins the anti-DoS cap on the chain
// of forms: a form the page paints directly is at depth 1, and one past the
// document's form budget, [limits.DefaultMaxFormDepth], is not judged. veraPDF
// sets no such cap; ISO 32000-1, 8.10 puts no limit on nesting.
func TestDrawnStreamWalkStopsAtTheFormBudget(t *testing.T) {
	t.Parallel()

	var f drawnFixture
	chain := func(depth int) []string {
		// Objects 5 onward are the forms, each painting the next.
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R >>", f.pages(3), f.page(f.xobjects("Fm", "5"), 4, ""),
			exponentStream("", "/Fm Do"),
		}
		for level := 1; level < depth; level++ {
			objs = append(objs, f.form(f.xobjects("Fm", strconv.Itoa(5+level)), "/Fm Do"))
		}
		return append(objs, f.form("<< >>", drawnBad))
	}
	for _, tc := range []struct {
		depth int
		want  int
	}{
		{limits.DefaultMaxFormDepth, 1},
		{limits.DefaultMaxFormDepth + 1, 0},
	} {
		t.Run(strconv.Itoa(tc.depth), func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, chain(tc.depth)...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_1_6_t01); got != tc.want {
				t.Errorf("a bad form at depth %d fired 6.1.6 t1 %d times, want %d", tc.depth, got, tc.want)
			}
		})
	}
}
