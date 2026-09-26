package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// reachImage is an image object whose /Intent is outside the four ISO 32000-1
// names, so every place the XObject rules reach it fails ISO 19005-1, 6.2.9
// t1 and ISO 19005-2, 6.2.6 t1 once.
func reachImage(sample string) string {
	return exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray "+
		"/BitsPerComponent 8 /Intent /Bogus", sample)
}

// reachForm is a Form XObject whose /Resources are res, none when res is empty.
func reachForm(res, body string) string {
	dict := "/Type /XObject /Subtype /Form /BBox [0 0 10 10]"
	if res != "" {
		dict += " /Resources " + res
	}
	return exponentStream(dict, body)
}

// reachPage is a page whose /Resources are res and /Contents contents, with
// the annotations annots when it is not empty.
func reachPage(res, contents, annots string) string {
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents " + contents
	if annots != "" {
		page += " /Annots [" + annots + "]"
	}
	return page + " >>"
}

// reachAnnot is a printed Square annotation whose normal appearance is the
// stream ap references.
func reachAnnot(ap string) string {
	return "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N " + ap + " >> >>"
}

// reachType3 is a Type 3 font, object 50, with glyphs a and b, objects 51 and
// 52, whose /Resources are res.
func reachType3(res string) string {
	return "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << /a 51 0 R /b 52 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
		"/FirstChar 97 /LastChar 98 /Widths [1000 1000] /Resources " + res + " >>"
}

const (
	reachDraw      = "q 10 0 0 10 0 0 cm /I1 Do Q"
	reachPlainProc = "1000 0 d0 0 0 1000 1000 re f"
	reachImageProc = "1000 0 d0 q 1000 0 0 1000 0 0 cm /I1 Do Q"
	reachInlineI   = "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /CS /G /BPC 8 /I true ID \x80 EI Q"
)

// TestImageRulesReachAppearancesAndGlyphs pins that the image rules judge an
// image wherever a content stream veraPDF parses draws it: an annotation's
// appearance stream, every glyph procedure of a Type 3 font text shows, and
// the streams those draw in turn, each image once. See [checkXObjectGroup].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, on an
// image whose /Intent is /Bogus.
func TestImageRulesReachAppearancesAndGlyphs(t *testing.T) {
	t.Parallel()

	const (
		imageRes = "<< /XObject << /I1 30 0 R >> >>"
		t3Res    = "<< /Font << /T3 50 0 R >> >>"
		blank    = "0 0 1 1 re f"
	)
	glyphFont := map[int]string{
		30: reachImage("\x80"), 50: reachType3(imageRes),
		51: exponentStream("", reachPlainProc), 52: exponentStream("", reachImageProc),
	}
	with := func(base map[int]string, more map[int]string) map[int]string {
		out := make(map[int]string, len(base)+len(more))
		for n, b := range base {
			out[n] = b
		}
		for n, b := range more {
			out[n] = b
		}
		return out
	}

	for _, tc := range []struct {
		name  string
		pages []string
		extra map[int]string
		want  int
	}{
		{
			"drawn by an annotation appearance",
			[]string{reachPage("<< >>", "20 0 R", "40 0 R")},
			map[int]string{20: exponentStream("", blank), 30: reachImage("\x80"), 40: reachAnnot("41 0 R"), 41: reachForm(imageRes, reachDraw)},
			1,
		},
		{
			"named by the page and drawn by an appearance with no resources",
			[]string{reachPage(imageRes, "20 0 R", "40 0 R")},
			map[int]string{20: exponentStream("", blank), 30: reachImage("\x80"), 40: reachAnnot("41 0 R"), 41: reachForm("", reachDraw)},
			1,
		},
		{
			"drawn by an appearance two pages' annotations share",
			[]string{reachPage("<< >>", "20 0 R", "40 0 R"), reachPage("<< >>", "20 0 R", "42 0 R")},
			map[int]string{
				20: exponentStream("", blank), 30: reachImage("\x80"),
				40: reachAnnot("41 0 R"), 41: reachForm(imageRes, reachDraw), 42: reachAnnot("41 0 R"),
			},
			1,
		},
		{
			"drawn by the page and by an appearance",
			[]string{reachPage(imageRes, "20 0 R", "40 0 R")},
			map[int]string{20: exponentStream("", reachDraw), 30: reachImage("\x80"), 40: reachAnnot("41 0 R"), 41: reachForm(imageRes, reachDraw)},
			1,
		},
		{
			"drawn by the glyph a page shows",
			[]string{reachPage(t3Res, "20 0 R", "")},
			with(glyphFont, map[int]string{20: exponentStream("", "BT /T3 12 Tf (b) Tj ET")}),
			1,
		},
		{
			"drawn by a glyph the page does not show",
			[]string{reachPage(t3Res, "20 0 R", "")},
			with(glyphFont, map[int]string{20: exponentStream("", "BT /T3 12 Tf (a) Tj ET")}),
			1,
		},
		{
			"drawn by a glyph of a font shown only in rendering mode 3",
			[]string{reachPage(t3Res, "20 0 R", "")},
			with(glyphFont, map[int]string{20: exponentStream("", "BT 3 Tr /T3 12 Tf (a) Tj ET")}),
			1,
		},
		{
			"drawn by a glyph of a font a form shows",
			[]string{reachPage("<< /XObject << /X1 31 0 R >> >>", "20 0 R", "")},
			with(glyphFont, map[int]string{20: exponentStream("", "/X1 Do"), 31: reachForm(t3Res, "BT /T3 12 Tf (a) Tj ET")}),
			1,
		},
		{
			"drawn by a glyph of a font an appearance shows",
			[]string{reachPage("<< >>", "20 0 R", "40 0 R")},
			with(glyphFont, map[int]string{20: exponentStream("", blank), 40: reachAnnot("41 0 R"), 41: reachForm(t3Res, "BT /T3 12 Tf (a) Tj ET")}),
			1,
		},
		{
			"drawn by a glyph of a font a Tf selects and nothing shows",
			[]string{reachPage(t3Res, "20 0 R", "")},
			with(glyphFont, map[int]string{20: exponentStream("", "BT /T3 12 Tf ET")}),
			0,
		},
		{
			"named by the page and drawn by a form with no resources",
			[]string{reachPage("<< /XObject << /X1 31 0 R /I1 30 0 R >> >>", "20 0 R", "")},
			map[int]string{20: exponentStream("", "/X1 Do"), 30: reachImage("\x80"), 31: reachForm("", reachDraw)},
			1,
		},
		{
			"drawn by the cell of a pattern a form paints with",
			[]string{reachPage("<< /XObject << /X1 31 0 R >> >>", "20 0 R", "")},
			map[int]string{
				20: exponentStream("", "/X1 Do"), 30: reachImage("\x80"),
				31: reachForm("<< /Pattern << /P1 32 0 R >> >>", "/Pattern cs /P1 scn 0 0 5 5 re f"),
				32: exponentStream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] "+
					"/XStep 10 /YStep 10 /Resources "+imageRes, reachDraw),
			},
			1,
		},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_9_t01},
			{models.PDFA2b, Rule_19005_2_6_2_6_t01},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, drawnImageObjs(tc.pages, tc.extra)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}

// TestInlineImageInterpolateReach pins that the /Interpolate rule judges every
// inline image a content stream veraPDF parses holds, one violation each: the
// page's content, an appearance stream and a Type 3 glyph procedure alike.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, on inline images with
// /I true.
func TestInlineImageInterpolateReach(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		pages []string
		extra map[int]string
		want  int
	}{
		{
			"two on the page",
			[]string{reachPage("<< >>", "20 0 R", "")},
			map[int]string{20: exponentStream("", reachInlineI+" "+reachInlineI)},
			2,
		},
		{
			"two in an appearance",
			[]string{reachPage("<< >>", "20 0 R", "40 0 R")},
			map[int]string{20: exponentStream("", "0 0 1 1 re f"), 40: reachAnnot("41 0 R"), 41: reachForm("<< >>", reachInlineI+" "+reachInlineI)},
			2,
		},
		{
			"one in a glyph procedure",
			[]string{reachPage("<< /Font << /T3 50 0 R >> >>", "20 0 R", "")},
			map[int]string{
				20: exponentStream("", "BT /T3 12 Tf (a) Tj ET"), 50: reachType3("<< >>"),
				51: exponentStream("", "1000 0 d0 "+reachInlineI), 52: exponentStream("", reachPlainProc),
			},
			1,
		},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_4_t03},
			{models.PDFA2b, Rule_19005_2_6_2_8_t03},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, drawnImageObjs(tc.pages, tc.extra)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}

// TestImageMaskBitsByRole pins that the /BitsPerComponent rules read an image
// mask by the role it is reached in, as veraPDF builds a model object per
// role: a drawn image answers to t4 alone, /ImageMask or not, and an image's
// /Mask to t5, each role once, so one object drawn and named as a /Mask is
// judged in both. See [checkImageBitsPerComponent] and [seenXObject].
// Measured with veraPDF 1.30 at PDF/A-2b.
func TestImageMaskBitsByRole(t *testing.T) {
	t.Parallel()

	stencil := func(bits string) string {
		return exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ImageMask true /BitsPerComponent "+bits, "\x80")
	}
	const base = "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"
	page := func(res, body string, extra map[int]string) []string {
		extra[20] = exponentStream("", body)
		return drawnImageObjs([]string{reachPage(res, "20 0 R", "")}, extra)
	}

	for _, tc := range []struct {
		name   string
		objs   []string
		t4, t5 int
	}{
		{"a drawn image mask with 2 bits", page("<< /XObject << /A 30 0 R >> >>", "/A Do", map[int]string{30: stencil("2")}), 0, 0},
		{"a drawn image mask with 3 bits", page("<< /XObject << /A 30 0 R >> >>", "/A Do", map[int]string{30: stencil("3")}), 1, 0},
		{"a /Mask with 2 bits", page("<< /XObject << /B 31 0 R >> >>", "/B Do", map[int]string{30: stencil("2"), 31: exponentStream(base+" /Mask 30 0 R", "\x80")}), 0, 1},
		{
			"a /Mask with 8 bits and no /ImageMask",
			page("<< /XObject << /B 31 0 R >> >>", "/B Do", map[int]string{
				30: exponentStream(base, "\x80"), 31: exponentStream(base+" /Mask 30 0 R", "\x80"),
			}),
			0, 1,
		},
		{
			"an image mask drawn first and named as a /Mask",
			page("<< /XObject << /A 30 0 R /B 31 0 R >> >>", "/A Do /B Do", map[int]string{30: stencil("2"), 31: exponentStream(base+" /Mask 30 0 R", "\x80")}),
			0, 1,
		},
		{
			"an image mask with 2 bits a glyph procedure draws",
			page("<< /Font << /T3 50 0 R >> >>", "BT /T3 12 Tf (a) Tj ET", map[int]string{
				30: stencil("2"), 50: reachType3("<< /XObject << /I1 30 0 R >> >>"),
				51: exponentStream("", "1000 0 0 0 1000 1000 d1 /I1 Do"), 52: exponentStream("", reachPlainProc),
			}),
			0, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_8_t04); got != tc.t4 {
				t.Errorf("6.2.8 test 4 fired %d times, want %d", got, tc.t4)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_8_t05); got != tc.t5 {
				t.Errorf("6.2.8 test 5 fired %d times, want %d", got, tc.t5)
			}
		})
	}
}

// TestReferenceAppearanceUA1 pins that PDF/UA-1 judges an annotation's
// appearance stream by the reference XObject rule, as the PDF/A form rules
// do; see [checkFormXObjectRestrictions]. Measured with veraPDF 1.30 at
// PDF/UA-1: one appearance carrying /Ref that two annotations share fails ISO
// 14289-1, 7.20 t1 twice.
func TestReferenceAppearanceUA1(t *testing.T) {
	t.Parallel()

	ref := exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Ref << /F (x.pdf) /Page 0 >>", "0 0 1 1 re f")
	objs := drawnImageObjs([]string{reachPage("<< >>", "20 0 R", "40 0 R 42 0 R")}, map[int]string{
		20: exponentStream("", "0 0 1 1 re f"), 40: reachAnnot("41 0 R"), 41: ref, 42: reachAnnot("41 0 R"),
	})
	doc, _ := objsDocPDF(t, objs...)
	viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	if got := countRuleErrs(t, viols, Rule_14289_1_7_20_t01); got != 2 {
		t.Errorf("7.20 test 1 fired %d times, want 2", got)
	}
}

// TestFormRulesReachAppearances pins that the Form XObject rules judge an
// annotation's appearance stream as the form it is, whatever its /Subtype
// says and whether or not its content decodes, and that each rule is stated
// once per reach of a form: veraPDF states ISO 19005-2, 6.2.9 t1 as one test
// over /OPI, /PS and /Subtype2, the reference XObject rule on forms alone, and
// both at every Do and every appearance entry that reach the form. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestFormRulesReachAppearances(t *testing.T) {
	t.Parallel()

	const ps = "/OPI << >> /PS 45 0 R"
	appearance := func(ap string) []string {
		return drawnImageObjs([]string{reachPage("<< >>", "20 0 R", "40 0 R")}, map[int]string{
			20: exponentStream("", "0 0 1 1 re f"), 40: reachAnnot("41 0 R"), 41: ap, 45: exponentStream("", ""),
		})
	}
	drawn := func(xobject string) []string {
		return drawnImageObjs([]string{reachPage("<< /XObject << /X1 41 0 R >> >>", "20 0 R", "")}, map[int]string{
			20: exponentStream("", "/X1 Do"), 41: xobject, 45: exponentStream("", ""),
		})
	}
	const imageDict = "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"

	for _, tc := range []struct {
		name string
		objs []string
		// counts by profile, then rule.
		want map[models.PDFAProfile]map[Rule]int
	}{
		{
			"an appearance carrying /OPI and /PS",
			appearance(exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] "+ps, "0 0 1 1 re f")),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 1, Rule_19005_1_6_2_5_t01: 1},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 1},
			},
		},
		{
			"an appearance with no /Subtype carrying /OPI",
			appearance(exponentStream("/BBox [0 0 10 10] /OPI << >>", "0 0 1 1 re f")),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 1},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 1},
			},
		},
		{
			"an appearance that does not decode, carrying /OPI",
			appearance(exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /OPI << >> /Filter /FlateDecode", "not flate")),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 1},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 1},
			},
		},
		{
			"an appearance carrying /Ref",
			appearance(exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Ref << /F (x.pdf) /Page 0 >>", "0 0 1 1 re f")),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_6_t01: 1},
				models.PDFA2b: {Rule_19005_2_6_2_9_t02: 1},
			},
		},
		{
			"a drawn form carrying /OPI and /PS",
			drawn(exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] "+ps, "0 0 1 1 re f")),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 1, Rule_19005_1_6_2_5_t01: 1},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 1},
			},
		},
		{
			"a form carrying /OPI the page draws twice",
			drawnImageObjs([]string{reachPage("<< /XObject << /X1 41 0 R /X2 41 0 R >> >>", "20 0 R", "")}, map[int]string{
				20: exponentStream("", "/X1 Do /X1 Do /X2 Do"), 41: exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /OPI << >>", "0 0 1 1 re f"),
			}),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 3},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 3},
			},
		},
		{
			"a form carrying /OPI inside a form the page draws twice",
			drawnImageObjs([]string{reachPage("<< /XObject << /X1 42 0 R >> >>", "20 0 R", "")}, map[int]string{
				20: exponentStream("", "/X1 Do /X1 Do"), 41: exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /OPI << >>", "0 0 1 1 re f"),
				42: reachForm("<< /XObject << /In 41 0 R >> >>", "/In Do"),
			}),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 1},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 1},
			},
		},
		{
			"an appearance carrying /OPI that two annotations share, one naming it twice",
			drawnImageObjs([]string{reachPage("<< >>", "20 0 R", "40 0 R 42 0 R")}, map[int]string{
				20: exponentStream("", "0 0 1 1 re f"), 40: reachAnnot("41 0 R"),
				41: exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /OPI << >>", "0 0 1 1 re f"),
				42: "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 41 0 R /D 41 0 R >> >>",
			}),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_4_t02: 3},
				models.PDFA2b: {Rule_19005_2_6_2_9_t01: 3},
			},
		},
		{
			"a drawn image carrying /Ref",
			drawn(exponentStream(imageDict+" /Ref << /F (x.pdf) /Page 0 >>", "\x80")),
			map[models.PDFAProfile]map[Rule]int{
				models.PDFA1b: {Rule_19005_1_6_2_6_t01: 0},
				models.PDFA2b: {Rule_19005_2_6_2_9_t02: 0},
			},
		},
	} {
		for profile, rules := range tc.want {
			t.Run(string(profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
				viols, err := ValidatePDFA(context.Background(), doc, profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				for rule, want := range rules {
					if got := countRuleErrs(t, viols, rule); got != want {
						t.Errorf("%s test %d fired %d times, want %d", rule.Clause, rule.TestNumber, got, want)
					}
				}
			})
		}
	}
}
