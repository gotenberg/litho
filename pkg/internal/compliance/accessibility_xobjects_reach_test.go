package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestReferenceXObjectReachUA1 pins where PDF/UA-1 judges a reference
// XObject: at every Do that draws the form, in every content stream veraPDF
// parses, as the PDF/A form rules do, and never where a /Resources only lists
// it. See [checkFormXObjectRestrictions]. Measured with veraPDF 1.30.2 at
// PDF/UA-1 on each case, with ISO 19005-2, 6.2.9 t2 stating the same count at
// PDF/A-2b.
func TestReferenceXObjectReachUA1(t *testing.T) {
	t.Parallel()

	const (
		ref   = "/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Ref << /F (x.pdf) /Page 0 >>"
		form  = "/Type /XObject /Subtype /Form /BBox [0 0 10 10]"
		cell  = "/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"
		paint = "/Pattern cs /P1 scn 0 0 10 10 re f"
		show  = "BT /T3 10 Tf (a) Tj ET"
	)
	refForm := exponentStream(ref, "")
	onePage := func(res, content string, extra map[int]string) []string {
		extra[20] = exponentStream("", content)
		return drawnImageObjs([]string{reachPage(res, "20 0 R", "")}, extra)
	}

	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{
			"a form the page lists and never draws",
			onePage("<< /XObject << /R 41 0 R >> >>", "0 0 1 1 re f", map[int]string{41: refForm}),
			0,
		},
		{
			"a form the page draws twice",
			onePage("<< /XObject << /R 41 0 R >> >>", "/R Do /R Do", map[int]string{41: refForm}),
			2,
		},
		{
			"a form two pages draw",
			drawnImageObjs([]string{
				reachPage("<< /XObject << /R 41 0 R >> >>", "20 0 R", ""),
				reachPage("<< /XObject << /R 41 0 R >> >>", "21 0 R", ""),
			}, map[int]string{20: exponentStream("", "/R Do"), 21: exponentStream("", "/R Do"), 41: refForm}),
			2,
		},
		{
			"a form the content two pages share draws under resources of each",
			drawnImageObjs([]string{
				reachPage("<< /XObject << /R 41 0 R >> >>", "20 0 R", ""),
				reachPage("<< /XObject << /R 41 0 R >> /ExtGState << /G0 << /Type /ExtGState >> >> >>", "20 0 R", ""),
			}, map[int]string{20: exponentStream("", "/R Do"), 41: refForm}),
			1,
		},
		{
			"a form a drawn form lists and never draws",
			onePage("<< /XObject << /O 40 0 R >> >>", "/O Do", map[int]string{
				40: exponentStream(form+" /Resources << /XObject << /R 41 0 R >> >>", "0 0 1 1 re f"), 41: refForm,
			}),
			0,
		},
		{
			"a form a drawn form draws",
			onePage("<< /XObject << /O 40 0 R >> >>", "/O Do", map[int]string{
				40: exponentStream(form+" /Resources << /XObject << /R 41 0 R >> >>", "/R Do"), 41: refForm,
			}),
			1,
		},
		{
			"a form a resourceless form draws through the page's name",
			onePage("<< /XObject << /O 40 0 R /R 41 0 R >> >>", "/O Do", map[int]string{
				40: exponentStream(form, "/R Do"), 41: refForm,
			}),
			1,
		},
		{
			"a form a tiling cell draws",
			onePage("<< /Pattern << /P1 40 0 R >> >>", paint, map[int]string{
				40: exponentStream(cell+" /Resources << /XObject << /R 41 0 R >> >>", "/R Do"), 41: refForm,
			}),
			1,
		},
		{
			"a form a tiling cell lists and never draws",
			onePage("<< /Pattern << /P1 40 0 R >> >>", paint, map[int]string{
				40: exponentStream(cell+" /Resources << /XObject << /R 41 0 R >> >>", "0 0 1 1 re f"), 41: refForm,
			}),
			0,
		},
		{
			"a form the cell of a pattern no scn selects draws",
			onePage("<< /Pattern << /P1 40 0 R >> >>", "0 0 10 10 re f", map[int]string{
				40: exponentStream(cell+" /Resources << /XObject << /R 41 0 R >> >>", "/R Do"), 41: refForm,
			}),
			0,
		},
		{
			"a form a cell two pages select draws twice",
			drawnImageObjs([]string{
				reachPage("<< /Pattern << /P1 40 0 R >> >>", "20 0 R", ""),
				reachPage("<< /Pattern << /P1 40 0 R >> >>", "21 0 R", ""),
			}, map[int]string{
				20: exponentStream("", paint), 21: exponentStream("", paint),
				40: exponentStream(cell+" /Resources << /XObject << /R 41 0 R >> >>", "/R Do /R Do"), 41: refForm,
			}),
			2,
		},
		{
			"a form a Type 3 glyph the page shows draws",
			onePage("<< /Font << /T3 50 0 R >> >>", show, map[int]string{
				50: reachType3("<< /XObject << /R 41 0 R >> >>"), 51: exponentStream("", "1000 0 d0 /R Do"),
				52: exponentStream("", "1000 0 d0"), 41: refForm,
			}),
			1,
		},
		{
			"a form a Type 3 font lists and no glyph draws",
			onePage("<< /Font << /T3 50 0 R >> >>", show, map[int]string{
				50: reachType3("<< /XObject << /R 41 0 R >> >>"), 51: exponentStream("", "1000 0 d0"),
				52: exponentStream("", "1000 0 d0"), 41: refForm,
			}),
			0,
		},
		{
			"a form the glyph of a font a Tf only selects draws",
			onePage("<< /Font << /T3 50 0 R >> >>", "BT /T3 10 Tf ET", map[int]string{
				50: reachType3("<< /XObject << /R 41 0 R >> >>"), 51: exponentStream("", "1000 0 d0 /R Do"),
				52: exponentStream("", "1000 0 d0"), 41: refForm,
			}),
			0,
		},
		{
			"a form a Type 3 glyph a tiling cell shows draws",
			onePage("<< /Pattern << /P1 40 0 R >> >>", paint, map[int]string{
				40: exponentStream(cell+" /Resources << /Font << /T3 50 0 R >> >>", show),
				50: reachType3("<< /XObject << /R 41 0 R >> >>"), 51: exponentStream("", "1000 0 d0 /R Do"),
				52: exponentStream("", "1000 0 d0"), 41: refForm,
			}),
			1,
		},
		{
			"a form an annotation appearance draws",
			drawnImageObjs([]string{reachPage("<< >>", "20 0 R", "42 0 R")}, map[int]string{
				20: exponentStream("", "0 0 1 1 re f"), 42: reachAnnot("40 0 R"),
				40: exponentStream(form+" /Resources << /XObject << /R 41 0 R >> >>", "/R Do"), 41: refForm,
			}),
			1,
		},
		{
			"a form the cell of a pattern a form paints with draws",
			onePage("<< /XObject << /O 43 0 R >> >>", "/O Do", map[int]string{
				43: exponentStream(form+" /Resources << /Pattern << /P1 40 0 R >> >>", paint),
				40: exponentStream(cell+" /Resources << /XObject << /R 41 0 R >> >>", "/R Do"), 41: refForm,
			}),
			1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_20_t01); got != tc.want {
				t.Errorf("7.20 test 1 fired %d times, want %d", got, tc.want)
			}
		})
	}
}

// TestMarkedFormReuseReachUA1 pins how PDF/UA-1, 7.20 t2 counts the times a
// form XObject carrying /StructParents is referenced: once per Do in every
// content stream veraPDF parses, each stream once for the document, and once
// per appearance entry naming it, each annotation once. Every reference past
// the first fails the test. See [formXObjectDrawsUA]. Measured with veraPDF
// 1.30.2 at PDF/UA-1 on each case but the one its name marks as litho's.
func TestMarkedFormReuseReachUA1(t *testing.T) {
	t.Parallel()

	const (
		marked = "/Type /XObject /Subtype /Form /BBox [0 0 10 10] /StructParents 0"
		form   = "/Type /XObject /Subtype /Form /BBox [0 0 10 10]"
		mcid   = "/P << /MCID 0 >> BDC 0 0 1 1 re f EMC"
		cell   = "/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"
		paint  = "/Pattern cs /P1 scn 0 0 10 10 re f"
		show   = "BT /T3 10 Tf (a) Tj ET"
	)
	markedForm := exponentStream(marked, mcid)
	onePage := func(res, content, annots string, extra map[int]string) []string {
		extra[20] = exponentStream("", content)
		return drawnImageObjs([]string{reachPage(res, "20 0 R", annots)}, extra)
	}
	twoPages := func(res1, res2, content1, content2 string, extra map[int]string) []string {
		extra[20] = exponentStream("", content1)
		contents2 := "21 0 R"
		if content2 == "" {
			contents2 = "20 0 R"
		} else {
			extra[21] = exponentStream("", content2)
		}
		return drawnImageObjs([]string{reachPage(res1, "20 0 R", ""), reachPage(res2, contents2, "")}, extra)
	}
	const (
		withF    = "<< /XObject << /F 41 0 R >> >>"
		withCell = "<< /Pattern << /P1 40 0 R >> >>"
		withT3   = "<< /Font << /T3 50 0 R >> >>"
		cellF    = cell + " /Resources " + withF
	)

	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{"a form the page draws once", onePage(withF, "/F Do", "", map[int]string{41: markedForm}), 0},
		{"a form the page draws twice", onePage(withF, "/F Do /F Do", "", map[int]string{41: markedForm}), 1},
		{"a form the page draws three times", onePage(withF, "/F Do /F Do /F Do", "", map[int]string{41: markedForm}), 2},
		{
			"a form the page draws under two names",
			onePage("<< /XObject << /F1 41 0 R /F2 41 0 R >> >>", "/F1 Do /F2 Do", "", map[int]string{41: markedForm}),
			1,
		},
		{"a form two pages draw", twoPages(withF, withF, "/F Do", "/F Do", map[int]string{41: markedForm}), 1},
		{
			"a form without /StructParents the page draws twice",
			onePage(withF, "/F Do /F Do", "", map[int]string{41: exponentStream(form, mcid)}),
			0,
		},
		{
			"a form with /StructParent the page draws twice",
			onePage(withF, "/F Do /F Do", "", map[int]string{41: exponentStream(form+" /StructParent 0", mcid)}),
			0,
		},
		{
			"two forms the page draws once each",
			onePage("<< /XObject << /F 41 0 R /E 42 0 R >> >>", "/F Do /E Do", "", map[int]string{41: markedForm, 42: markedForm}),
			0,
		},
		{
			"a form the page and a cell it selects draw",
			onePage("<< /XObject << /F 41 0 R >> /Pattern << /P1 40 0 R >> >>", "/F Do "+paint, "", map[int]string{
				40: exponentStream(cellF, "/F Do"), 41: markedForm,
			}),
			1,
		},
		{
			"a form a cell the page selects twice draws",
			onePage(withCell, paint+" "+paint, "", map[int]string{40: exponentStream(cellF, "/F Do"), 41: markedForm}),
			0,
		},
		{
			"a form a cell draws twice",
			onePage(withCell, paint, "", map[int]string{40: exponentStream(cellF, "/F Do /F Do"), 41: markedForm}),
			1,
		},
		{
			"a form a cell two pages select draws",
			twoPages(withCell, withCell, paint, paint, map[int]string{40: exponentStream(cellF, "/F Do"), 41: markedForm}),
			0,
		},
		{
			"a form the page and a Type 3 glyph it shows draw",
			onePage("<< /Font << /T3 50 0 R >> /XObject << /F 41 0 R >> >>", show+" /F Do", "", map[int]string{
				50: reachType3(withF), 51: exponentStream("", "1000 0 d0 /F Do"), 52: exponentStream("", "1000 0 d0"), 41: markedForm,
			}),
			1,
		},
		{
			"a form a Type 3 glyph the page shows twice draws",
			onePage(withT3, "BT /T3 10 Tf (aa) Tj (a) Tj ET", "", map[int]string{
				50: reachType3(withF), 51: exponentStream("", "1000 0 d0 /F Do"), 52: exponentStream("", "1000 0 d0"), 41: markedForm,
			}),
			0,
		},
		{
			"a form a Type 3 glyph two pages show draws",
			twoPages(withT3, withT3, show, show, map[int]string{
				50: reachType3(withF), 51: exponentStream("", "1000 0 d0 /F Do"), 52: exponentStream("", "1000 0 d0"), 41: markedForm,
			}),
			0,
		},
		{
			"a form a form the page draws twice draws",
			onePage("<< /XObject << /G 42 0 R >> >>", "/G Do /G Do", "", map[int]string{
				42: exponentStream(form+" /Resources "+withF, "/F Do"), 41: markedForm,
			}),
			0,
		},
		{
			"a form the page and a form it draws draw",
			onePage("<< /XObject << /G 42 0 R /F 41 0 R >> >>", "/G Do /F Do", "", map[int]string{
				42: exponentStream(form+" /Resources "+withF, "/F Do"), 41: markedForm,
			}),
			1,
		},
		{
			"a form a form the page draws twice draws twice",
			onePage("<< /XObject << /G 42 0 R >> >>", "/G Do /G Do", "", map[int]string{
				42: exponentStream(form+" /Resources "+withF, "/F Do /F Do"), 41: markedForm,
			}),
			1,
		},
		{
			"a form the page and a form two levels down draw",
			onePage("<< /XObject << /H 43 0 R /F 41 0 R >> >>", "/H Do /F Do", "", map[int]string{
				43: exponentStream(form+" /Resources << /XObject << /G 42 0 R >> >>", "/G Do"),
				42: exponentStream(form+" /Resources "+withF, "/F Do"), 41: markedForm,
			}),
			1,
		},
		{
			"a form a resourceless form the page draws twice draws",
			onePage("<< /XObject << /G 42 0 R /F 41 0 R >> >>", "/G Do /G Do", "", map[int]string{
				42: exponentStream(form, "/F Do"), 41: markedForm,
			}),
			0,
		},
		{
			"an appearance two annotations share",
			onePage("<< >>", "0 0 1 1 re f", "44 0 R 45 0 R", map[int]string{44: reachAnnot("41 0 R"), 45: reachAnnot("41 0 R"), 41: markedForm}),
			1,
		},
		{
			"an appearance one annotation names as N, R and D",
			onePage("<< >>", "0 0 1 1 re f", "44 0 R", map[int]string{
				44: "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 41 0 R /R 41 0 R /D 41 0 R >> >>", 41: markedForm,
			}),
			2,
		},
		{
			"a form the page draws that is an annotation's appearance",
			onePage(withF, "/F Do", "44 0 R", map[int]string{44: reachAnnot("41 0 R"), 41: markedForm}),
			1,
		},
		{
			"a form the page and an appearance draw",
			onePage(withF, "/F Do", "44 0 R", map[int]string{
				44: reachAnnot("42 0 R"), 42: exponentStream(form+" /Resources "+withF, "/F Do"), 41: markedForm,
			}),
			1,
		},
		{
			"a form an appearance two annotations share draws",
			onePage("<< >>", "0 0 1 1 re f", "44 0 R 45 0 R", map[int]string{
				44: reachAnnot("42 0 R"), 45: reachAnnot("42 0 R"), 42: exponentStream(form+" /Resources "+withF, "/F Do"), 41: markedForm,
			}),
			0,
		},
		{"a form the content two pages share draws", twoPages(withF, withF, "/F Do", "", map[int]string{41: markedForm}), 0},
		{
			"a form the content two pages share draws under resources of each",
			twoPages(withF, "<< /XObject << /F 41 0 R >> /ExtGState << /G0 << /Type /ExtGState >> >> >>", "/F Do", "", map[int]string{41: markedForm}),
			0,
		},
		{
			// litho's reading: the second page's resources map the shared
			// Do to another form, which a third page draws again. veraPDF
			// walks the shared content from the first page alone and states
			// nothing.
			"litho: a form the shared content draws through a second page's resources and a third page draws",
			drawnImageObjs([]string{
				reachPage(withF, "20 0 R", ""),
				reachPage("<< /XObject << /F 42 0 R >> >>", "20 0 R", ""),
				reachPage("<< /XObject << /F 42 0 R >> >>", "21 0 R", ""),
			}, map[int]string{20: exponentStream("", "/F Do"), 21: exponentStream("", "/F Do"), 41: markedForm, 42: markedForm}),
			1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_20_t02); got != tc.want {
				t.Errorf("7.20 test 2 fired %d times, want %d", got, tc.want)
			}
		})
	}
}
