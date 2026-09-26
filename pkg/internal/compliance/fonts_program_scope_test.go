package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// programScopeType0Font returns [subsetType0Font] with a /W array giving CIDs 1
// to 37 the advances of the program, so that of the shown CIDs only one the
// program lacks, which takes the advance of glyph 0, fails the width rule.
func programScopeType0Font(t *testing.T) []string {
	t.Helper()
	font := subsetType0Font(t, "")
	font[1] = strings.Replace(font[1], "/DW 1000", "/DW 1000 /W [1 36 0 37 [652.34375]]", 1)
	return font
}

// onPage appends entries to the dictionary of the n-th page [glyphPagesObjs]
// builds.
func onPage(objs []string, n int, entries string) []string {
	objs[8+n] = strings.TrimSuffix(objs[8+n], " >>") + " " + entries + " >>"
	return objs
}

// withObject sets object num of objs to body, padding with null objects.
func withObject(objs []string, num int, body string) []string {
	for len(objs) < num {
		objs = append(objs, "null")
	}
	objs[num-1] = body
	return objs
}

// shareResources makes every page of objs name one /Resources dictionary,
// object 3.
func shareResources(objs []string) []string {
	for i := 9; i < len(objs); i++ {
		objs[i] = strings.Replace(objs[i], "/Resources << /Font << /F1 5 0 R >> >>", "/Resources 3 0 R", 1)
	}
	objs[2] = "<< /Font << /F1 5 0 R >> >>"
	return objs
}

// withForm names, as /X1 in the first page's resources, a Form XObject whose
// own /Resources name the font as /F1 and whose content is body.
func withForm(objs []string, body string) []string {
	objs[9] = strings.Replace(objs[9], "/Font << /F1 5 0 R >>", "/Font << /F1 5 0 R >> /XObject << /X1 30 0 R >>", 1)
	return withObject(objs, 30, exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >>", body))
}

// withNestedForm names, as /X2 in the resources of the form [withForm] adds, a
// Form XObject whose own /Resources name the font as /F1 and whose content is
// body.
func withNestedForm(objs []string, body string) []string {
	objs[29] = strings.Replace(objs[29], "/Resources << /Font << /F1 5 0 R >> >>", "/Resources << /Font << /F1 5 0 R >> /XObject << /X2 31 0 R >> >>", 1)
	return withObject(objs, 31, exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >>", body))
}

// withAnnotation gives the first page an annotation whose normal appearance
// names the font as /F1 in its own /Resources and shows body.
func withAnnotation(objs []string, body string) []string {
	objs = onPage(objs, 1, "/Annots [40 0 R]")
	objs = withObject(objs, 40, "<< /Type /Annot /Subtype /Stamp /Rect [0 0 100 100] /F 4 /AP << /N 41 0 R >> >>")
	return withObject(objs, 41, exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> >>", body))
}

// type3ScopeFont returns objects 5 and 6 of [glyphPagesObjs]: a Type 3 font
// whose encoding names glyphs a and b and whose only glyph procedure is a's.
func type3ScopeFont() []string {
	return []string{
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
			"/CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
			"/FirstChar 97 /LastChar 98 /Widths [500 500] /Resources << >> >>",
		exponentStream("", "500 0 d0"),
	}
}

// glyphRuleCounts is how often the glyph-presence, width and .notdef rules
// fire; -1 leaves a rule unchecked.
type glyphRuleCounts struct{ presence, width, notdef int }

// checkGlyphRuleCounts validates objs at PDF/A-1b and PDF/A-2b and compares
// the counts of the glyph rules with want. PDF/A-1 has no .notdef rule.
func checkGlyphRuleCounts(t *testing.T, objs []string, want glyphRuleCounts) {
	t.Helper()
	checkGlyphRuleCountsByPart(t, objs, want, want)
}

// checkGlyphRuleCountsByPart is [checkGlyphRuleCounts] with the counts
// PDF/A-1b and PDF/A-2b are to give apart, want1 and want2.
func checkGlyphRuleCountsByPart(t *testing.T, objs []string, want1, want2 glyphRuleCounts) {
	t.Helper()
	for _, p := range []struct {
		profile                 models.PDFAProfile
		presence, width, notdef Rule
		want                    glyphRuleCounts
	}{
		{models.PDFA1b, Rule_19005_1_6_3_5_t01, Rule_19005_1_6_3_6_t01, Rule{}, want1},
		{models.PDFA2b, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_2_6_2_11_5_t01, Rule_19005_2_6_2_11_8_t01, want2},
	} {
		doc, _ := objsDocPDF(t, objs...)
		viols, err := ValidatePDFA(context.Background(), doc, p.profile)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, c := range []struct {
			rule Rule
			want int
		}{{p.presence, p.want.presence}, {p.width, p.want.width}, {p.notdef, p.want.notdef}} {
			if c.want < 0 || c.rule.Specification == "" {
				continue
			}
			if got := countRuleErrs(t, viols, c.rule); got != c.want {
				t.Errorf("%s: %s test %d fired %d times, want %d", p.profile, c.rule.Clause, c.rule.TestNumber, got, c.want)
			}
		}
	}
}

// TestGlyphProgramRulesJudgedInFirstScope pins that the glyph-presence and
// width rules judge a glyph only where veraPDF reads the font's program for
// it: through the resource dictionary that first shows the font object in a
// rendering mode, in the order a page draws its text and its forms, each form
// in the rendering mode its Do leaves, and through any other only when it is
// the first to show the font in some mode.
// The .notdef rule and Type 3 fonts are judged everywhere. See
// [fontJudgements.readsProgram].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestGlyphProgramRulesJudgedInFirstScope(t *testing.T) {
	t.Parallel()

	type0 := programScopeType0Font(t)
	type1 := subsetType1Font(t)
	// A's advance, 694.5, is declared; B's, 658.5, is not.
	type1[0] = strings.Replace(type1[0], "/Widths [600 600]", "/Widths [694.5 100]", 1)

	for _, tc := range []struct {
		name string
		objs func() []string
		want glyphRuleCounts
	}{
		{"a missing CID on a later page", func() []string {
			return glyphPagesObjs(type0, glyphText("<0025> Tj"), glyphText("<0100> Tj"))
		}, glyphRuleCounts{0, 0, 1}},
		{"a missing CID on a later page sharing the resources", func() []string {
			return shareResources(glyphPagesObjs(type0, glyphText("<0025> Tj"), glyphText("<0100> Tj")))
		}, glyphRuleCounts{0, 0, 1}},
		{"a missing CID on each page", func() []string {
			return glyphPagesObjs(type0, glyphText("<0100> Tj"), glyphText("<0101> Tj"))
		}, glyphRuleCounts{1, 1, 2}},
		{"a missing CID on a later page in a new rendering mode", func() []string {
			return glyphPagesObjs(type0, glyphText("<0025> Tj"), glyphText("2 Tr <0100> Tj"))
		}, glyphRuleCounts{1, 1, 1}},
		{"a later page that also shows the font in rendering mode 3", func() []string {
			return glyphPagesObjs(type0, glyphText("<0025> Tj"), glyphText("<0100> Tj 3 Tr <0025> Tj"))
		}, glyphRuleCounts{1, 1, 1}},
		// The second page meets CID 256 without reading the program, and the
		// third, which reads it, shows the same glyph again.
		{"a glyph met before a page that reads the program", func() []string {
			return glyphPagesObjs(type0, glyphText("<0025> Tj"), glyphText("<0100> Tj"), glyphText("<0100> Tj 2 Tr <0101> Tj"))
		}, glyphRuleCounts{1, 1, 2}},
		{"the same on pages with distinct structure keys", func() []string {
			objs := glyphPagesObjs(type0, glyphText("<0025> Tj"), glyphText("<0100> Tj"), glyphText("<0100> Tj 2 Tr <0101> Tj"))
			return onPage(onPage(onPage(objs, 1, "/StructParents 1"), 2, "/StructParents 2"), 3, "/StructParents 3")
		}, glyphRuleCounts{2, 2, 3}},
		{"a form drawn after the page text", func() []string {
			return withForm(glyphPagesObjs(type0, glyphText("<0025> Tj")+" /X1 Do"), glyphText("<0100> Tj"))
		}, glyphRuleCounts{0, 0, 1}},
		{"a form drawn before the page text", func() []string {
			return withForm(glyphPagesObjs(type0, "/X1 Do "+glyphText("<0025> Tj")), glyphText("<0100> Tj"))
		}, glyphRuleCounts{1, 1, 1}},
		{"the page text after a form", func() []string {
			return withForm(glyphPagesObjs(type0, "/X1 Do "+glyphText("<0100> Tj")), glyphText("<0025> Tj"))
		}, glyphRuleCounts{0, 0, 1}},
		{"a nested form before the text of the form drawing it", func() []string {
			return withNestedForm(withForm(glyphPagesObjs(type0, "/X1 Do"), "/X2 Do "+glyphText("<0100> Tj")), glyphText("<0025> Tj"))
		}, glyphRuleCounts{0, 0, 1}},
		{"a form drawn between page text in modes 3 and 0", func() []string {
			return withForm(glyphPagesObjs(type0, "BT /F1 12 Tf 3 Tr <0025> Tj 0 Tr ET /X1 Do "+glyphText("<0025> Tj")), glyphText("<0100> Tj"))
		}, glyphRuleCounts{1, 1, 1}},
		{"a form drawn in rendering mode 3", func() []string {
			return withForm(glyphPagesObjs(type0, "BT 3 Tr ET /X1 Do BT 0 Tr ET "+glyphText("<0025> Tj")), glyphText("<0100> Tj"))
		}, glyphRuleCounts{0, 0, 1}},
		{"a Type1 width on a later page", func() []string {
			return glyphPagesObjs(type1, glyphText("(A) Tj"), glyphText("(B) Tj"))
		}, glyphRuleCounts{0, 0, 0}},
		{"a Type1 width on a later page in a new rendering mode", func() []string {
			return glyphPagesObjs(type1, glyphText("(A) Tj"), glyphText("2 Tr (B) Tj"))
		}, glyphRuleCounts{0, 1, 0}},
		{"a Type3 glyph on a later page", func() []string {
			return glyphPagesObjs(type3ScopeFont(), glyphText("(a) Tj"), glyphText("(b) Tj"))
		}, glyphRuleCounts{1, -1, -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, tc.objs(), tc.want)
		})
	}
}

// TestGlyphProgramRulesJudgedAnnotationsByPart pins where a page's annotation
// appearances stand in the order veraPDF reads a font's program: before the
// page's content at PDF/A-1b, after it at PDF/A-2b; see
// [annotationsParsedFirst]. The .notdef rule, which needs no program, is
// judged either way.
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestGlyphProgramRulesJudgedAnnotationsByPart(t *testing.T) {
	t.Parallel()

	type0 := programScopeType0Font(t)
	for _, tc := range []struct {
		name         string
		objs         func() []string
		want1, want2 glyphRuleCounts
	}{
		{"a missing CID in an annotation, another on the page", func() []string {
			return withAnnotation(glyphPagesObjs(type0, glyphText("<0025> Tj")), glyphText("<0100> Tj"))
		}, glyphRuleCounts{1, 1, -1}, glyphRuleCounts{0, 0, 1}},
		{"a missing CID on the page, another in an annotation", func() []string {
			return withAnnotation(glyphPagesObjs(type0, glyphText("<0100> Tj")), glyphText("<0025> Tj"))
		}, glyphRuleCounts{0, 0, -1}, glyphRuleCounts{1, 1, 1}},
		{"a missing CID in an annotation, another in a form", func() []string {
			return withAnnotation(withForm(glyphPagesObjs(type0, "/X1 Do"), glyphText("<0025> Tj")), glyphText("<0100> Tj"))
		}, glyphRuleCounts{1, 1, -1}, glyphRuleCounts{0, 0, 1}},
		{"a missing CID in a form, another in an annotation", func() []string {
			return withAnnotation(withForm(glyphPagesObjs(type0, "/X1 Do"), glyphText("<0100> Tj")), glyphText("<0025> Tj"))
		}, glyphRuleCounts{0, 0, -1}, glyphRuleCounts{1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCountsByPart(t, tc.objs(), tc.want1, tc.want2)
		})
	}
}

// TestGlyphProgramRulesJudgedAtUA pins how PDF/UA reads a font's program: a
// form the page draws claims its rendering modes where drawn but has none of
// its glyphs judged ([pageFormReads]), and the page's annotation appearances
// are parsed before its content ([annotationsParsedFirst]).
//
// Every count was measured with veraPDF 1.30 at PDF/UA-1 and PDF/UA-2.
func TestGlyphProgramRulesJudgedAtUA(t *testing.T) {
	t.Parallel()

	type0 := programScopeType0Font(t)
	for _, tc := range []struct {
		name string
		objs func() []string
		want int
	}{
		{"a missing CID in a form alone", func() []string {
			return withForm(glyphPagesObjs(type0, "/X1 Do"), glyphText("<0100> Tj"))
		}, 0},
		{"a form before page text, each showing a missing CID", func() []string {
			return withForm(glyphPagesObjs(type0, "/X1 Do "+glyphText("<0101> Tj")), glyphText("<0100> Tj"))
		}, 0},
		{"page text before a form, each showing a missing CID", func() []string {
			return withForm(glyphPagesObjs(type0, glyphText("<0101> Tj")+" /X1 Do"), glyphText("<0100> Tj"))
		}, 1},
		{"a missing CID in an annotation, another on the page", func() []string {
			return withAnnotation(glyphPagesObjs(type0, glyphText("<0101> Tj")), glyphText("<0100> Tj <0102> Tj"))
		}, 2},
	} {
		for _, p := range []struct {
			profile models.PDFUAProfile
			rule    Rule
		}{
			{models.PDFUA1, Rule_14289_1_7_21_4_1_t02},
			{models.PDFUA2, Rule_14289_2_8_4_5_5_1_t02},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs()...)
				viols, err := ValidatePDFUA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFUA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}

// TestGlyphRulesKeyedByStructureKey pins that every glyph rule states a glyph
// once per structure key ([structureKey]) of the stream it is shown in, the
// .notdef rule included; the presence and width rules only look unkeyed
// because veraPDF judges them on the first page alone ([fontJudgements.readsProgram]).
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, on two
// pages whose /StructParents are 1 and 2.
func TestGlyphRulesKeyedByStructureKey(t *testing.T) {
	t.Parallel()

	type0 := programScopeType0Font(t)
	type1 := subsetType1Font(t)
	type1[0] = strings.Replace(type1[0], "/Encoding /WinAnsiEncoding",
		"/Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [65 /.notdef] >>", 1)
	keyed := func(objs []string) []string {
		return onPage(onPage(objs, 1, "/StructParents 1"), 2, "/StructParents 2")
	}

	for _, tc := range []struct {
		name string
		objs []string
		want glyphRuleCounts
	}{
		{"CID 0 on both pages", keyed(glyphPagesObjs(type0, glyphText("<0000> Tj"), glyphText("<0000> Tj"))), glyphRuleCounts{0, 1, 2}},
		{"a missing CID on both pages", keyed(glyphPagesObjs(type0, glyphText("<0100> Tj"), glyphText("<0100> Tj"))), glyphRuleCounts{1, 1, 2}},
		{"a missing CID on both pages, the second in a new mode too", keyed(glyphPagesObjs(type0,
			glyphText("<0100> Tj"), glyphText("<0100> Tj 2 Tr <0101> Tj"))), glyphRuleCounts{3, 3, 3}},
		{"the same with no structure keys", glyphPagesObjs(type0,
			glyphText("<0100> Tj"), glyphText("<0100> Tj 2 Tr <0101> Tj")), glyphRuleCounts{2, 2, 2}},
		{"a simple font's .notdef on both pages", keyed(glyphPagesObjs(type1, glyphText("(A) Tj"), glyphText("(A) Tj"))), glyphRuleCounts{0, -1, 2}},
		{"the same with no structure keys", glyphPagesObjs(type1, glyphText("(A) Tj"), glyphText("(A) Tj")), glyphRuleCounts{0, -1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, tc.objs, tc.want)
		})
	}
}

// TestType0ToUnicodeJudgedPerPage pins that a composite font with no
// /ToUnicode states the ToUnicode rule for the glyphs of every page that shows
// it, not of the first alone. Measured with veraPDF 1.30: a CID on each of two
// pages fails ISO 19005-2, 6.2.11.7.2 t1 at PDF/A-2u and ISO 19005-1, 6.3.8 t1
// at PDF/A-1a twice, CID 0 on the second page included.
func TestType0ToUnicodeJudgedPerPage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		bodies []string
	}{
		{"two CIDs", []string{glyphText("<0025> Tj"), glyphText("<0024> Tj")}},
		{"CID 0 on the second page", []string{glyphText("<0025> Tj"), glyphText("<0000> Tj")}},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA2u, Rule_19005_2_6_2_11_7_2_t01},
			{models.PDFA1a, Rule_19005_1_6_3_8_t01},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, glyphPagesObjs(programScopeType0Font(t), tc.bodies...)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != 2 {
					t.Errorf("%s test %d fired %d times, want 2", p.rule.Clause, p.rule.TestNumber, got)
				}
			})
		}
	}
}
