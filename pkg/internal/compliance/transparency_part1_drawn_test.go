package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestBlendModeValuePart1 pins which /BM values fail ISO 19005-1, 6.4 t4: every
// value but the names Normal and Compatible, the array form included; see
// [blendModeForbidden1]. Every count was measured with veraPDF 1.30 at
// PDF/A-1b.
func TestBlendModeValuePart1(t *testing.T) {
	t.Parallel()

	pageState := func(bm string, extra ...string) []string {
		return append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /ExtGState << /GS1 << /Type /ExtGState /BM " +
				bm + " >> >> >> /Contents 4 0 R >>",
			exponentStream("", "/GS1 gs 0 0 m 10 10 l S"),
		}, extra...)
	}

	cases := []struct {
		name string
		objs []string
		want int
	}{
		{"Normal", pageState("/Normal"), 0},
		{"Multiply", pageState("/Multiply"), 1},
		{"an array whose first name is Normal", pageState("[/Normal /Foo]"), 1},
		{"an array whose first element is no name", pageState("[1 /Foo]"), 1},
		{"an array of Normal alone", pageState("[/Normal]"), 1},
		{"an array of Compatible alone", pageState("[/Compatible]"), 1},
		{"an empty array", pageState("[]"), 1},
		{"an array naming Normal through a reference", pageState("[5 0 R]", "/Normal"), 1},
		{"the empty name", pageState("/"), 1},
		{"an integer", pageState("1"), 1},
		{"a direct null", pageState("null"), 0},
		{"a reference to Normal", pageState("5 0 R", "/Normal"), 0},
		{"a reference to an unknown name", pageState("5 0 R", "/Foo"), 1},
		{"a reference to null", pageState("5 0 R", "null"), 1},
		{"a reference to an undefined object", pageState("9 0 R"), 1},
		{"a reference to an array of Normal", pageState("5 0 R", "[/Normal]"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := transparencyCounts(t, tc.objs, models.PDFA1b, Rule_19005_1_6_4_t04)[0]; got != tc.want {
				t.Errorf("6.4 test 4 fired %d times, want %d", got, tc.want)
			}
		})
	}
}

// TestTransparencyPart1ReachesWhatVeraPDFDraws pins that ISO 19005-1, 6.4
// judges the graphics states and XObjects of the streams a page draws as
// veraPDF reaches them: a name through the drawing stream's own resources and
// then the page's, and a stream only through the operator that draws it; see
// [checkTransparencyPart1] and [drawnStreamWalk]. The counts are listed for
// t1 to t6, and every one was measured with veraPDF 1.30 at PDF/A-1b.
func TestTransparencyPart1ReachesWhatVeraPDFDraws(t *testing.T) {
	t.Parallel()

	const (
		form     = "/Type /XObject /Subtype /Form /BBox [0 0 10 10]"
		multiply = "<< /Type /ExtGState /BM /Multiply >>"
		tiling   = "/PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"
		image    = "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"
		square   = "/Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4"
		softMask = "<< /Type /ExtGState /SMask << /Type /Mask /S /Alpha /G 6 0 R >> >>"
	)
	onePage := func(res, content, pageExtra string, extra ...string) []string {
		return append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 4 0 R" + pageExtra + " >>",
			exponentStream("", content),
		}, extra...)
	}
	// type3 is a Type3 font, object 5, whose glyph /a is object 6 and whose
	// dictionary ends in extra.
	type3 := func(extra string) string {
		return "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.1 0 0 0.1 0 0] /CharProcs << /a 6 0 R >> " +
			"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [10]" + extra + " >>"
	}
	pageGS1 := "<< /XObject << /Fm1 5 0 R >> /ExtGState << /GS1 " + multiply + " >> >>"

	cases := []struct {
		name string
		objs []string
		want [6]int
	}{
		{"a form without resources setting a page state", onePage(pageGS1, "/Fm1 Do", "",
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{"a form with empty resources setting a page state", onePage(pageGS1, "/Fm1 Do", "",
			exponentStream(form+" /Resources << >>", "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{"a form whose states lack the page state it sets", onePage(pageGS1, "/Fm1 Do", "",
			exponentStream(form+" /Resources << /ExtGState << /GS2 << /Type /ExtGState /LW 1 >> >> >>", "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{"a form without resources setting a direct page state twice", onePage(pageGS1, "/Fm1 Do", "",
			exponentStream(form, "/GS1 gs /GS1 gs 0 0 m 5 5 l S")), [6]int{3: 2}},
		{"a page and its form setting a direct page state", onePage(pageGS1, "/GS1 gs /Fm1 Do", "",
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 2}},
		{"a page and its form setting an indirect page state", onePage("<< /XObject << /Fm1 5 0 R >> /ExtGState << /GS1 6 0 R >> >>", "/GS1 gs /Fm1 Do", "",
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S"), multiply), [6]int{3: 1}},
		{"a form painted twice setting a page state", onePage(pageGS1, "/Fm1 Do /Fm1 Do", "",
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{
			"a form without resources setting a transparent page state",
			onePage("<< /XObject << /Fm1 5 0 R >> /ExtGState << /GS1 << /Type /ExtGState /CA 0.5 /ca 0.5 /SMask /Foo >> >> >>", "/Fm1 Do", "",
				exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")),
			[6]int{0: 1, 4: 1, 5: 1},
		},
		{"a form without resources painting a form the page defines", onePage("<< /XObject << /Fm1 5 0 R /Fm2 6 0 R >> >>", "/Fm1 Do", "",
			exponentStream(form, "/Fm2 Do"),
			exponentStream(form+" /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{"a form without resources painting a soft masked image the page defines", onePage("<< /XObject << /Fm1 5 0 R /Im1 6 0 R >> >>", "/Fm1 Do", "",
			exponentStream(form, "q 5 0 0 5 0 0 cm /Im1 Do Q"),
			exponentStream(image+" /SMask 7 0 R", "\x80"), exponentStream(image, "\x80")), [6]int{1: 1}},
		{"a form without resources painting a group the page defines", onePage("<< /XObject << /Fm1 5 0 R /Fm2 6 0 R >> >>", "/Fm1 Do", "",
			exponentStream(form, "/Fm2 Do"),
			exponentStream(form+" /Group << /S /Transparency >> /Resources << >>", "0 0 m 5 5 l S")), [6]int{2: 1}},
		{"a nested form setting a state only its parent form defines", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do", "",
			exponentStream(form+" /Resources << /XObject << /Fm2 6 0 R >> /ExtGState << /GS1 "+multiply+" >> >>", "/Fm2 Do"),
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")), [6]int{}},
		{"a form two pages paint, judged with the first page's state", []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /XObject << /Fm1 7 0 R >> /ExtGState << /GS1 " + multiply + " >> >> /Contents 5 0 R >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /XObject << /Fm1 7 0 R >> /ExtGState << /GS1 << /Type /ExtGState /BM /Screen >> >> >> /Contents 6 0 R >>",
			exponentStream("", "/Fm1 Do"), exponentStream("", "/Fm1 Do"),
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S"),
		}, [6]int{3: 1}},
		{"a page content stream that is also the form the page paints", []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /ExtGState << /E1 " + softMask +
				" >> /XObject << /X1 4 0 R >> >> /Contents 4 0 R >>",
			exponentStream(form+" /Group << /S /Transparency >> /Resources << /ExtGState << /E1 "+softMask+" >> /XObject << /X1 5 0 R >> >>", "/E1 gs /X1 Do"),
			exponentStream(form+" /Group << /S /Transparency >> /Resources << >>", "0 0 m 5 5 l S"),
			exponentStream(form+" /Group << /S /Transparency >> /Resources << >>", "0 0 m 5 5 l S"),
		}, [6]int{0: 1, 2: 1}},
		{"a form no Do paints", onePage("<< /XObject << /Fm1 5 0 R >> >>", "0 0 m 10 10 l S", "",
			exponentStream(form+" /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 m 5 5 l S")), [6]int{}},
		{"a tiling pattern without resources setting a page state", onePage("<< /Pattern << /P1 5 0 R >> /ExtGState << /GS1 "+multiply+" >> >>",
			"/Pattern cs /P1 scn 0 0 10 10 re f", "", exponentStream(tiling, "/GS1 gs 0 0 5 5 re f")), [6]int{3: 1}},
		{"a tiling pattern no scn selects", onePage("<< /Pattern << /P1 5 0 R >> >>", "0 0 10 10 re f", "",
			exponentStream(tiling+" /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 5 5 re f")), [6]int{}},
		{"a tiling pattern a form without resources selects", onePage("<< /XObject << /Fm1 5 0 R >> /Pattern << /P1 6 0 R >> >>", "/Fm1 Do", "",
			exponentStream(form, "/Pattern cs /P1 scn 0 0 10 10 re f"),
			exponentStream(tiling+" /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 5 5 re f")), [6]int{3: 1}},
		{
			"a shading pattern's graphics state", onePage("<< /Pattern << /P1 5 0 R >> >>", "/Pattern cs /P1 scn 0 0 10 10 re f", "",
				"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 10 0] "+
					"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> /ExtGState << /Type /ExtGState /CA 0.5 /BM /Multiply >> >>"),
			[6]int{},
		},
		{"an appearance without resources setting a page state", onePage("<< /ExtGState << /GS1 "+multiply+" >> >>", "0 0 m 10 10 l S", " /Annots [5 0 R]",
			"<< "+square+" /AP << /N 6 0 R >> >>", exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{"an appearance state setting a state", onePage("<< >>", "0 0 m 10 10 l S", " /Annots [5 0 R]",
			"<< "+square+" /AP << /N << /On 6 0 R /Off 7 0 R >> >> /AS /On >>", exponentStream(form+" /Resources << >>", "0 0 m 5 5 l S"),
			exponentStream(form+" /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 m 5 5 l S")), [6]int{3: 1}},
		{
			"an appearance under a key other than N, R and D", onePage("<< >>", "0 0 m 10 10 l S", " /Annots [5 0 R]",
				"<< "+square+" /AP << /N 6 0 R /X 7 0 R >> >>", exponentStream(form+" /Resources << >>", "0 0 m 5 5 l S"),
				exponentStream(form+" /Group << /S /Transparency >> /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 m 5 5 l S")),
			[6]int{},
		},
		{"a glyph of a font without resources setting a page state", onePage("<< /Font << /F1 5 0 R >> /ExtGState << /GS1 "+multiply+" >> >>",
			"BT /F1 12 Tf (a) Tj ET", "", type3(""), exponentStream("", "10 0 d0 /GS1 gs 0 0 5 5 re f")), [6]int{3: 1}},
		{"a Type3 font no text is shown with", onePage("<< /Font << /F1 5 0 R >> >>", "BT /F1 12 Tf ET", "",
			type3(" /Resources << /ExtGState << /GS1 "+multiply+" >> >>"), exponentStream("", "10 0 d0 /GS1 gs 0 0 5 5 re f")), [6]int{}},
		{"a Type3 font whose resources define a state its glyph never sets", onePage("<< /Font << /F1 5 0 R >> >>", "BT /F1 12 Tf (a) Tj ET", "",
			type3(" /Resources << /ExtGState << /GS1 "+multiply+" /GS2 << /Type /ExtGState /BM /Screen >> >> >>"),
			exponentStream("", "10 0 d0 /GS1 gs 0 0 5 5 re f")), [6]int{3: 1}},
		{"a glyph's own resources before its font's", onePage("<< /Font << /F1 5 0 R >> >>", "BT /F1 12 Tf (a) Tj ET", "",
			type3(" /Resources << /ExtGState << /GS1 << /Type /ExtGState /BM /Normal >> >> >>"),
			exponentStream("/Resources << /ExtGState << /GS1 "+multiply+" >> >>", "10 0 d0 /GS1 gs 0 0 5 5 re f")), [6]int{3: 1}},
		{"a glyph's own resources in its font's stead", onePage("<< /Font << /F1 5 0 R >> >>", "BT /F1 12 Tf (a) Tj ET", "",
			type3(" /Resources << /ExtGState << /GS1 "+multiply+" >> >>"),
			exponentStream("/Resources << /ExtGState << /GS2 << /Type /ExtGState /BM /Normal >> >> >>", "10 0 d0 /GS1 gs 0 0 5 5 re f")), [6]int{}},
		{"a glyph's own resources falling back to the page's", onePage("<< /Font << /F1 5 0 R >> /ExtGState << /GS1 "+multiply+" >> >>",
			"BT /F1 12 Tf (a) Tj ET", "", type3(""),
			exponentStream("/Resources << /ExtGState << /GS2 << /Type /ExtGState /BM /Normal >> >> >>", "10 0 d0 /GS1 gs 0 0 5 5 re f")), [6]int{3: 1}},
		{
			"a state only a soft mask group sets", onePage("<< /ExtGState << /GS0 << /Type /ExtGState /SMask << /Type /Mask /S /Luminosity /G 5 0 R >> >> >> >>",
				"/GS0 gs 0 0 m 10 10 l S", "",
				exponentStream(form+" /Group << /S /Transparency /CS /DeviceGray >> /Resources << /ExtGState << /GS1 "+multiply+" >> >>", "/GS1 gs 0 0 5 5 re f")),
			[6]int{0: 1},
		},
	}
	rules := []Rule{
		Rule_19005_1_6_4_t01, Rule_19005_1_6_4_t02, Rule_19005_1_6_4_t03,
		Rule_19005_1_6_4_t04, Rule_19005_1_6_4_t05, Rule_19005_1_6_4_t06,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := transparencyCounts(t, tc.objs, models.PDFA1b, rules...)
			for i, rule := range rules {
				if got[i] != tc.want[i] {
					t.Errorf("6.4 test %d fired %d times, want %d", rule.TestNumber, got[i], tc.want[i])
				}
			}
		})
	}
}
