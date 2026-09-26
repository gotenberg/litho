package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestBlendModeInEveryDrawnStream pins that the blend mode rule, ISO 19005-2,
// 6.2.10 t1 and ISO 19005-4, 6.2.9 t1, judges the graphics states every stream
// a page draws sets, and the /BM an annotation names at PDF/A-4; see
// [checkTransparencyParts2Plus], [drawnStreamBlendModes], [cosBlendMode] and
// [annotationBlendModes]. Every count was measured with veraPDF 1.30.
func TestBlendModeInEveryDrawnStream(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		form    = "/Type /XObject /Subtype /Form /BBox [0 0 10 10]"
		foo     = "<< /Type /ExtGState /BM /Foo >>"
		square  = "/Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4"
	)
	onePage := func(res, content, pageExtra string, extra ...string) []string {
		return append([]string{
			catalog, pages,
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 4 0 R" + pageExtra + " >>",
			exponentStream("", content),
		}, extra...)
	}
	// twoPages lays out two pages with the resources res, whose content
	// streams, objects 5 and 6, both hold content, and extra from object 7.
	twoPages := func(res, content, pageExtra string, extra ...string) []string {
		return append([]string{
			catalog,
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 5 0 R" + pageExtra + " >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 6 0 R" + pageExtra + " >>",
			exponentStream("", content),
			exponentStream("", content),
		}, extra...)
	}
	setsFoo := func(content string) string {
		return exponentStream(form+" /Resources << /ExtGState << /GS1 "+foo+" >> >>", content)
	}
	pageState := func(bm string) []string {
		return onePage("<< /ExtGState << /GS1 << /Type /ExtGState /BM "+bm+" >> >> >>", "/GS1 gs 0 0 m 10 10 l S", "")
	}

	cases := []struct {
		name    string
		objs    []string
		profile models.PDFAProfile
		want    int
	}{
		{"a form setting a direct state twice", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do", "",
			setsFoo("/GS1 gs /GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 2},
		{"a form painted twice setting an indirect state twice", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do /Fm1 Do", "",
			exponentStream(form+" /Resources << /ExtGState << /GS1 6 0 R >> >>", "/GS1 gs /GS1 gs 0 0 m 5 5 l S"), foo), models.PDFA2b, 1},
		{"a form painted twice setting a direct state", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do /Fm1 Do", "",
			setsFoo("/GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 1},
		{"a form two pages paint", twoPages("<< /XObject << /Fm1 7 0 R >> >>", "/Fm1 Do", "",
			setsFoo("/GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 1},
		{"a form nested in a form", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do", "",
			exponentStream(form+" /Resources << /XObject << /Fm2 6 0 R >> >>", "/Fm2 Do"),
			setsFoo("/GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 1},
		{"a form setting a state only the page defines", onePage("<< /XObject << /Fm1 5 0 R >> /ExtGState << /GS1 "+foo+" >> >>", "/Fm1 Do", "",
			exponentStream(form, "/GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 1},
		{"a form declaring a state it never sets", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do", "",
			setsFoo("0 0 m 5 5 l S")), models.PDFA2b, 0},
		{"an indirect state the page and its form set", onePage("<< /XObject << /Fm1 5 0 R >> /ExtGState << /GS1 6 0 R >> >>", "/GS1 gs /Fm1 Do", "",
			exponentStream(form+" /Resources << /ExtGState << /GSX 6 0 R >> >>", "/GSX gs 0 0 m 5 5 l S"), foo), models.PDFA2b, 1},
		{"a tiling pattern", onePage("<< /Pattern << /P1 5 0 R >> >>", "/Pattern cs /P1 scn 0 0 10 10 re f", "",
			exponentStream("/PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 "+
				"/Resources << /ExtGState << /GS1 "+foo+" >> >>", "/GS1 gs 0 0 5 5 re f")), models.PDFA2b, 1},
		{"a shading pattern's graphics state", onePage("<< /Pattern << /P1 5 0 R >> >>", "/Pattern cs /P1 scn 0 0 10 10 re f", "",
			"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 10 0] "+
				"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> /ExtGState "+foo+" >>"), models.PDFA2b, 0},
		{"a Type3 glyph", onePage("<< /Font << /F1 5 0 R >> >>", "BT /F1 12 Tf (a) Tj ET", "",
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.1 0 0 0.1 0 0] /CharProcs << /a 6 0 R >> "+
				"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [10] "+
				"/Resources << /ExtGState << /GS1 "+foo+" >> >> >>",
			exponentStream("", "10 0 d0 /GS1 gs 0 0 5 5 re f")), models.PDFA2b, 1},
		{"an annotation appearance", onePage("<< >>", "0 0 m 10 10 l S", " /Annots [5 0 R]",
			"<< "+square+" /AP << /N 6 0 R >> >>", setsFoo("/GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 1},
		{"an appearance two annotations share", onePage("<< >>", "0 0 m 10 10 l S", " /Annots [5 0 R 6 0 R]",
			"<< "+square+" /AP << /N 7 0 R >> >>", "<< "+square+" /AP << /N 7 0 R >> >>",
			setsFoo("/GS1 gs 0 0 m 5 5 l S")), models.PDFA2b, 1},
		{"a soft mask group", onePage("<< /ExtGState << /GS0 << /Type /ExtGState /SMask << /Type /Mask /S /Luminosity /G 5 0 R >> >> >> >>",
			"/GS0 gs 0 0 m 10 10 l S", "",
			exponentStream(form+" /Group << /S /Transparency /CS /DeviceGray >> /Resources << /ExtGState << /GS1 "+foo+" >> >>",
				"/GS1 gs 0 0 5 5 re f")), models.PDFA2b, 0},
		{"an array whose first name is unknown", pageState("[1 /Foo]"), models.PDFA2b, 1},
		{"an array whose first name is Normal", pageState("[/Normal /Foo]"), models.PDFA2b, 0},
		{"the empty name", pageState("/"), models.PDFA2b, 1},
		{"the empty name at PDF/A-4", pageState("/"), models.PDFA4, 1},
		{"an array at PDF/A-4", pageState("[/Foo]"), models.PDFA4, 0},
		{"a form setting a direct state twice at PDF/A-4", onePage("<< /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do", "",
			setsFoo("/GS1 gs /GS1 gs 0 0 m 5 5 l S")), models.PDFA4, 2},
		{"an annotation's blend mode", onePage("<< >>", "0 0 m 10 10 l S", " /Annots [5 0 R]",
			"<< "+square+" /BM /Foo /AP << /N 6 0 R >> >>", exponentStream(form+" /Resources << >>", "0 0 m 5 5 l S")), models.PDFA2b, 0},
		{"an annotation's blend mode at PDF/A-4", onePage("<< >>", "0 0 m 10 10 l S", " /Annots [5 0 R]",
			"<< "+square+" /BM /Foo /AP << /N 6 0 R >> >>", exponentStream(form+" /Resources << >>", "0 0 m 5 5 l S")), models.PDFA4, 1},
		{"an annotation two pages list at PDF/A-4", twoPages("<< >>", "0 0 m 10 10 l S", " /Annots [7 0 R]",
			"<< "+square+" /BM /Foo /AP << /N 8 0 R >> >>", exponentStream(form+" /Resources << >>", "0 0 m 5 5 l S")), models.PDFA4, 1},
		{"a direct annotation naming an array at PDF/A-4", onePage("<< >>", "0 0 m 10 10 l S",
			" /Annots [<< "+square+" /BM [/Foo] /AP << /N 5 0 R >> >>]",
			exponentStream(form+" /Resources << >>", "0 0 m 5 5 l S")), models.PDFA4, 0},
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
