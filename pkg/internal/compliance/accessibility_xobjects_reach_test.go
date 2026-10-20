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
