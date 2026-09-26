package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// gstateTR returns the counts of the transfer function rule, ISO 19005-1,
// 6.2.8 t1 and ISO 19005-2 and ISO 19005-4, 6.2.5 t1, at PDF/A-1b, PDF/A-2b
// and PDF/A-4.
func gstateTR(want int) []namesCount {
	return []namesCount{{models.PDFA1b, "6.2.8", 1, want}, {models.PDFA2b, "6.2.5", 1, want}, {models.PDFA4, "6.2.5", 1, want}}
}

// gstateHalftone returns the counts of the halftone type rule, ISO 19005-2 and
// ISO 19005-4, 6.2.5 t4, at PDF/A-2b and PDF/A-4.
func gstateHalftone(want int) []namesCount {
	return []namesCount{{models.PDFA2b, "6.2.5", 4, want}, {models.PDFA4, "6.2.5", 4, want}}
}

// TestExtGStateJudgedWhereAGsSetsIt pins which graphics states the transfer
// function, halftone and rendering intent rules judge, and how often (see
// [checkExtGStateGraphics]): a state only where a gs sets it, one written
// directly at every gs, one written as an indirect object and an indirect
// halftone once for the whole document, and the states of every stream
// [drawnStreamWalk] reaches. Every count was measured with veraPDF 1.30 at
// PDF/A-1b, PDF/A-2b and PDF/A-4 on the same constructs.
func TestExtGStateJudgedWhereAGsSetsIt(t *testing.T) {
	t.Parallel()

	const (
		fill  = "0 0 10 10 re f"
		tr    = "<< /TR /Identity >>"
		setTR = "<< /ExtGState << /G0 " + tr + " >> >>"
		ht6   = "<< /Type /Halftone /HalftoneType 6 /Width 8 /Height 8 >>"
	)
	twoPages := func(res string, extra ...string) []byte {
		return cyclePDF(append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << " + res + " >> /Contents 5 0 R >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << " + res + " >> /Contents 6 0 R >>",
			defaultStream("", "/G0 gs /G0 gs "+fill),
			defaultStream("", "/G0 gs "+fill),
		}, extra...)...)
	}
	annot := "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 7 0 R >> >>"
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "a direct /TR state two gs set",
			data:   defaultSpacePDF("", "/ExtGState << /G0 "+tr+" >>", "/G0 gs /G0 gs "+fill),
			counts: gstateTR(2),
		},
		{
			name: "a direct /RI state two gs set",
			data: defaultSpacePDF("", "/ExtGState << /G0 << /RI /Foo >> >>", "/G0 gs /G0 gs "+fill),
			counts: []namesCount{
				{models.PDFA1b, "6.2.9", 1, 2},
				{models.PDFA2b, "6.2.6", 1, 2},
			},
		},
		{
			name:   "a direct and an indirect /TR state no gs sets",
			data:   defaultSpacePDF("", "/ExtGState << /G0 "+tr+" /G1 5 0 R >>", fill, tr),
			counts: gstateTR(0),
		},
		{
			name: "an indirect /TR2 state three gs on two pages set",
			data: twoPages("/ExtGState << /G0 7 0 R >>", "<< /TR2 /Foo >>"),
			counts: []namesCount{
				{models.PDFA1b, "6.2.8", 2, 1},
				{models.PDFA2b, "6.2.5", 2, 1},
				{models.PDFA4, "6.2.5", 2, 1},
			},
		},
		{
			name:   "a direct state a form painted twice sets once",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do /X0 Do", namesForm(setTR, "/G0 gs "+fill)),
			counts: gstateTR(1),
		},
		{
			name:   "a direct state a form no Do paints sets",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", fill, namesForm(setTR, "/G0 gs "+fill)),
			counts: gstateTR(0),
		},
		{
			name: "a direct state a form sets that a form of the page paints",
			data: defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do",
				namesForm("<< /XObject << /X1 6 0 R >> >>", "/X1 Do"), namesForm(setTR, "/G0 gs "+fill)),
			counts: gstateTR(1),
		},
		{
			name:   "a direct state a selected tiling cell sets",
			data:   defaultSpacePDF("", "/Pattern << /P0 5 0 R >>", "/Pattern cs /P0 scn "+fill, namesTiling(1, setTR, "/G0 gs "+fill)),
			counts: gstateTR(1),
		},
		{
			name: "a direct state a shown Type3 glyph sets",
			data: defaultSpacePDF("", "/Font << /F1 5 0 R >>", "BT /F1 10 Tf (a) Tj ET",
				namesType3(6, setTR), defaultStream("", "10 0 0 0 10 10 d1 /G0 gs "+fill)),
			counts: gstateTR(1),
		},
		{
			name: "a direct state a soft mask group sets",
			data: defaultSpacePDF("", "/ExtGState << /SM << /SMask << /S /Luminosity /G 5 0 R >> >> >>", "/SM gs "+fill,
				defaultStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency /CS /DeviceGray >> /Resources "+setTR, "/G0 gs "+fill)),
			counts: gstateTR(0),
		},
		{
			name:   "a direct state an appearance two annotations share sets",
			data:   defaultSpacePDF("/Annots [5 0 R 6 0 R]", "", fill, annot, annot, namesForm(setTR, "/G0 gs "+fill)),
			counts: gstateTR(1),
		},
		{
			name:   "two direct states naming one indirect halftone, each set once",
			data:   defaultSpacePDF("", "/ExtGState << /H1 << /HT 5 0 R >> /H2 << /HT 5 0 R >> >>", "/H1 gs /H2 gs "+fill, ht6),
			counts: gstateHalftone(1),
		},
		{
			name:   "a direct state holding a direct halftone two gs set",
			data:   defaultSpacePDF("", "/ExtGState << /H0 << /HT "+ht6+" >> >>", "/H0 gs /H0 gs "+fill),
			counts: gstateHalftone(2),
		},
		{
			name:   "an indirect state holding a direct halftone two gs set",
			data:   defaultSpacePDF("", "/ExtGState << /H0 5 0 R >>", "/H0 gs /H0 gs "+fill, "<< /HT "+ht6+" >>"),
			counts: gstateHalftone(1),
		},
	})
}

// TestExtGStateJudgingIsLinear pins that a content stream setting one direct
// graphics state with many gs operators fails its rule once per gs, and that
// the state is read once rather than once per operator: the stream costs
// time linear in its operators (see [checkExtGStateGraphics]).
func TestExtGStateJudgingIsLinear(t *testing.T) {
	t.Parallel()

	const sets = 200_000
	data := defaultSpacePDF("", "/ExtGState << /G0 << /TR /Identity >> >>", strings.Repeat("/G0 gs ", sets)+"0 0 10 10 re f")
	if got := countRulePDFA(t, data, models.PDFA2b, "6.2.5", 1); got != sets {
		t.Errorf("6.2.5 t1 fired %d times, want %d", got, sets)
	}

	drawn := collectDrawnResources(parseOps(t, strings.Repeat("/G0 gs ", sets)))
	if len(drawn.extGStates) != 1 || drawn.gsCount("G0") != sets {
		t.Errorf("drawn names %v, want G0 counted %d times", drawn.extGStates, sets)
	}
	if got := checkExtGStateGraphics(context.Background(), nopDoc(), nil, drawn, string(models.PDFA2b), 2, 1, map[int]bool{}); len(got) != 0 {
		t.Errorf("no resources judged %d states", len(got))
	}
}
