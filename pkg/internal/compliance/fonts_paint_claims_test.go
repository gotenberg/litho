package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// paintClaimObjs returns the objects of [glyphPagesObjs] over
// [programScopeType0Font], one page whose content is page and whose resources
// add the Type 3 font /T3 (object 41) and the tiling pattern /P1 (object 30).
// The pattern's cell runs cell and the Type 3 glyph a runs proc, both naming
// the Type0 font /F1 in their own resources.
func paintClaimObjs(t *testing.T, page, cell, proc string) []string {
	t.Helper()
	objs := glyphPagesObjs(programScopeType0Font(t), page)
	objs[9] = strings.Replace(objs[9], "/Resources << /Font << /F1 5 0 R >> >>",
		"/Resources << /Font << /F1 5 0 R /T3 41 0 R >> /Pattern << /P1 30 0 R >> /XObject << /X1 31 0 R >> >>", 1)
	objs = withObject(objs, 30, tilingPattern(1, "<< /Font << /F1 5 0 R >> >>", cell))
	objs = withObject(objs, 31, exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
		"/Resources << /Font << /F1 5 0 R >> /Pattern << /P1 30 0 R >> >>", paintClaimFill))
	objs = withObject(objs, 41, "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] "+
		"/CharProcs << /a 42 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> "+
		"/FirstChar 97 /LastChar 97 /Widths [500] /Resources << /Font << /F1 5 0 R >> /Pattern << /P1 30 0 R >> >> >>")
	return withObject(objs, 42, exponentStream("", "500 0 d0 "+proc))
}

// Content for [paintClaimObjs]: a fill with /P1, and a Type 3 glyph shown in
// rendering mode 0.
const (
	paintClaimFill = " /Pattern cs /P1 scn 0 0 50 50 re f "
	paintClaimT3   = " BT /T3 12 Tf 10 10 Td (a) Tj ET "
)

// TestPatternFontsClaimedWherePainted pins that a tiling pattern claims the
// program reads of the fonts its cell shows where an operator paints with it,
// without its glyphs being judged by them; see
// [fontJudgements.claimPatternReads]. The cell shows CID 256 and the page CID
// 257, both absent from the program.
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestPatternFontsClaimedWherePainted(t *testing.T) {
	t.Parallel()

	const cell = "BT /F1 12 Tf 2 2 Td <0100> Tj ET"
	missing := glyphText("<0101> Tj")
	for _, tc := range []struct {
		name string
		page string
		cell string
		want glyphRuleCounts
	}{
		{"a fill alone", paintClaimFill, cell, glyphRuleCounts{0, 0, 1}},
		{"a fill before the page text", paintClaimFill + missing, cell, glyphRuleCounts{0, 0, 2}},
		{"a fill after the page text", missing + paintClaimFill, cell, glyphRuleCounts{1, 1, 2}},
		{"a cell showing the CID in rendering mode 2", paintClaimFill + missing, "BT /F1 12 Tf 2 2 Td 2 Tr <0100> Tj ET", glyphRuleCounts{1, 1, 2}},
		{"a pattern selected and never painted", "/Pattern cs /P1 scn", cell, glyphRuleCounts{1, 1, 1}},
		{"page text painted with the pattern", "/Pattern cs /P1 scn " + missing, "BT /F1 12 Tf 2 2 Td 2 Tr <0100> Tj ET", glyphRuleCounts{1, 1, 2}},
		{"a path ended with n", "/Pattern cs /P1 scn 0 0 50 50 re n " + missing, cell, glyphRuleCounts{1, 1, 2}},
		{"a clip", "/Pattern cs /P1 scn 0 0 50 50 re W n " + missing, cell, glyphRuleCounts{1, 1, 2}},
		{"a stroke with a stroking pattern", "/Pattern CS /P1 SCN 0 0 50 50 re S " + missing, cell, glyphRuleCounts{0, 0, 2}},
		{"a stroke with a filling pattern", "/Pattern cs /P1 scn 0 0 50 50 re S " + missing, cell, glyphRuleCounts{1, 1, 2}},
		{"a fill with a stroking pattern", "/Pattern CS /P1 SCN 0 0 50 50 re f " + missing, cell, glyphRuleCounts{1, 1, 2}},
		{"an even-odd fill", "/Pattern cs /P1 scn 0 0 50 50 re f* " + missing, cell, glyphRuleCounts{0, 0, 2}},
		{"a fill and stroke", "/Pattern cs /P1 scn 0 0 50 50 re B " + missing, cell, glyphRuleCounts{0, 0, 2}},
		{"a fill inside q and Q", "/Pattern cs /P1 scn q 0 0 50 50 re f Q " + missing, cell, glyphRuleCounts{0, 0, 2}},
		{"a fill in a form drawn before the page text", "/X1 Do " + missing, cell, glyphRuleCounts{0, 0, 2}},
		{"a fill in a Type 3 glyph shown before the page text", paintClaimT3 + missing, cell, glyphRuleCounts{0, 0, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			proc := ""
			if strings.Contains(tc.page, "/T3") {
				proc = paintClaimFill
			}
			checkGlyphRuleCounts(t, paintClaimObjs(t, tc.page, tc.cell, proc), tc.want)
		})
	}
}

// TestType3ProcFontsClaimedWhereShown pins that a Type 3 glyph procedure
// claims the program reads of the fonts it shows where an operator shows its
// glyph, in the rendering mode that operator shows it in, and has its glyphs
// judged by them; see [fontJudgements.claimType3Reads]. The procedure shows
// CID 256 and the page CIDs 257 and 258, all absent from the program.
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestType3ProcFontsClaimedWhereShown(t *testing.T) {
	t.Parallel()

	const proc = "BT /F1 12 Tf <0100> Tj ET"
	missing := glyphText("<01010102> Tj")
	for _, tc := range []struct {
		name string
		page string
		proc string
		want glyphRuleCounts
	}{
		{"a glyph shown alone", paintClaimT3, proc, glyphRuleCounts{1, 1, 1}},
		{"a glyph shown before the page text", paintClaimT3 + missing, proc, glyphRuleCounts{1, 1, 3}},
		{"a glyph shown after the page text", missing + paintClaimT3, proc, glyphRuleCounts{2, 2, 3}},
		{"a procedure in rendering mode 2", paintClaimT3 + missing, "BT /F1 12 Tf 2 Tr <0100> Tj ET", glyphRuleCounts{3, 3, 3}},
		{"page text in rendering mode 2", paintClaimT3 + glyphText("2 Tr <01010102> Tj"), proc, glyphRuleCounts{3, 3, 3}},
		{"a glyph shown in rendering mode 2", "BT /T3 12 Tf 10 10 Td 2 Tr (a) Tj ET " + missing, proc, glyphRuleCounts{1, 1, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, paintClaimObjs(t, tc.page, "", tc.proc), tc.want)
		})
	}
}

// TestFontsReachedThroughEveryDrawnStream pins that the glyph rules judge the
// fonts of a Type 3 glyph procedure and of a tiling pattern cell whatever
// stream draws them: a Type 3 glyph a form shows, a pattern a Type 3 glyph
// procedure paints with, and a pattern an annotation's appearance stream
// paints with; see [checkDrawnStreamFonts]. The procedure and the cell show
// CID 256, and the page CID 257, both absent from the program.
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b, PDF/A-2b and
// PDF/UA-1.
func TestFontsReachedThroughEveryDrawnStream(t *testing.T) {
	t.Parallel()

	const shows = "BT /F1 12 Tf 2 2 Td <0100> Tj ET"
	missing := glyphText("<0101> Tj")

	t3InForm := paintClaimObjs(t, "/X1 Do", "", shows)
	t3InForm[30] = exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 200 100] /Resources << /Font << /T3 41 0 R >> >>", paintClaimT3)
	checkGlyphRuleCounts(t, t3InForm, glyphRuleCounts{1, 1, 1})

	checkGlyphRuleCounts(t, paintClaimObjs(t, paintClaimT3+missing, shows, paintClaimFill), glyphRuleCounts{0, 0, 2})

	inAnnotation := paintClaimObjs(t, missing, shows, "")
	inAnnotation = onPage(inAnnotation, 1, "/Annots [43 0 R]")
	inAnnotation = withObject(inAnnotation, 43, "<< /Type /Annot /Subtype /Stamp /Rect [0 0 100 100] /F 4 /AP << /N 44 0 R >> >>")
	inAnnotation = withObject(inAnnotation, 44, exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Pattern << /P1 30 0 R >> >>", paintClaimFill))
	checkGlyphRuleCountsByPart(t, inAnnotation, glyphRuleCounts{0, 0, -1}, glyphRuleCounts{1, 1, 2})

	for _, p := range []struct {
		name string
		objs []string
		want int
	}{
		{"a Type 3 glyph a form shows", t3InForm, 1},
		{"a pattern an appearance paints", inAnnotation, 2},
	} {
		doc, _ := objsDocPDF(t, p.objs...)
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_14289_1_7_21_8_t01); got != p.want {
			t.Errorf("%s: 7.21.8 t1 fired %d times, want %d", p.name, got, p.want)
		}
		if got := countRuleErrs(t, viols, Rule_14289_1_7_21_4_1_t02); got != 0 {
			t.Errorf("%s: 7.21.4.1 t2 fired %d times, want 0", p.name, got)
		}
	}
}

// TestType3FontShownInsideItsOwnProcedure pins that a stream reached from a
// Type 3 font's glyph procedure has none of that font's glyphs judged, while
// the other fonts it shows are; see [checkType3CharProcFonts]. The page shows
// the Type 3 glyph rect in rendering mode 2, whose procedure shows CID 257
// through a text show painted with a tiling pattern, whose cell shows rect
// again, in mode 0, and with it CID 256 or nothing.
//
// Every count was measured with veraPDF 1.30 at PDF/A-2b and PDF/A-2u.
func TestType3FontShownInsideItsOwnProcedure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		cell      string
		toUnicode int
		notdef    int
	}{
		{"the cell shows the font alone", "BT /T3 12 Tf 2 2 Td (a) Tj ET", 2, 1},
		{"the cell shows a CID too", "BT /T3 12 Tf 2 2 Td (a) Tj /F1 12 Tf <0100> Tj ET", 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objs := paintClaimObjs(t, "BT /T3 12 Tf 10 10 Td 2 Tr (a) Tj ET", tc.cell,
				"/Pattern cs /P1 scn BT /F1 12 Tf 0 Tr <0101> Tj ET")
			objs[29] = strings.Replace(objs[29], "/Resources << /Font << /F1 5 0 R >> >>", "/Resources << /Font << /F1 5 0 R /T3 41 0 R >> >>", 1)
			objs[40] = strings.Replace(objs[40], "/CharProcs << /a 42 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>",
				"/CharProcs << /rect 42 0 R >> /Encoding << /Type /Encoding /Differences [97 /rect] >>", 1)
			for _, p := range []struct {
				profile models.PDFAProfile
				rule    Rule
				want    int
			}{
				{models.PDFA2u, Rule_19005_2_6_2_11_7_2_t01, tc.toUnicode},
				{models.PDFA2b, Rule_19005_2_6_2_11_8_t01, tc.notdef},
			} {
				doc, _ := objsDocPDF(t, objs...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != p.want {
					t.Errorf("%s: %s test %d fired %d times, want %d", p.profile, p.rule.Clause, p.rule.TestNumber, got, p.want)
				}
			}
		})
	}
}

// TestPaintClaimsAtUA pins that PDF/UA claims a painted pattern's fonts and a
// Type 3 procedure's as PDF/A does.
//
// Every count was measured with veraPDF 1.30 at PDF/UA-1 and PDF/UA-2.
func TestPaintClaimsAtUA(t *testing.T) {
	t.Parallel()

	const cell = "BT /F1 12 Tf 2 2 Td <0100> Tj ET"
	for _, tc := range []struct {
		name       string
		page, proc string
		want       int
	}{
		{"a fill before the page text", paintClaimFill + glyphText("<0101> Tj"), "", 0},
		{"a fill after the page text", glyphText("<0101> Tj") + paintClaimFill, "", 1},
		{"a Type 3 glyph before the page text", paintClaimT3 + glyphText("<01010102> Tj"), cell, 1},
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
				doc, _ := objsDocPDF(t, paintClaimObjs(t, tc.page, cell, tc.proc)...)
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
