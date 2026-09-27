package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

// graphicsReachExtras is what [graphicsReachPDF] adds to its objects: gs to the
// /Resources of the page, the Type 3 font, the pattern and the first
// appearance, paint to the content of the page, the glyph and the pattern
// cell, apPaint to the first appearance's, and mark to the dictionaries of the
// font, the pattern and the first appearance.
type graphicsReachExtras struct {
	gs, paint, apPaint, mark string
}

// graphicsReachPDF builds a one-page document whose images and forms the XObject
// rules judge outside the page's own /XObject resources: an image with
// /Interpolate true drawn by glyph a of the Type 3 font T3 the page shows, one
// drawn by the cell of the tiling pattern P0 the page fills with, and one
// drawn by the normal appearance of a Square annotation, a form with no
// /Subtype carrying /OPI; a second annotation's /On appearance carries /Ref.
func graphicsReachPDF(x graphicsReachExtras) []byte {
	const draw = "q 10 0 0 10 0 0 cm /Im Do Q"
	image := func(sample string) string {
		return streamBody("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray"+
			" /BitsPerComponent 8 /Interpolate true", sample)
	}
	res := func(image string) string {
		return "<< /XObject << /Im " + image + " >>" + x.gs + " >>"
	}
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Annots [9 0 R 11 0 R]"+
			" /Resources << /Font << /T3 5 0 R >> /Pattern << /P0 7 0 R >>"+x.gs+" >> >>",
		streamBody("", x.paint+"q BT /T3 12 Tf 10 10 Td (a) Tj ET Q q /Pattern cs /P0 scn 0 0 50 50 re f Q"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /a 12 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>"+
			" /FirstChar 97 /LastChar 97 /Widths [1000] /Resources "+res("6 0 R")+x.mark+" >>",
		image("A"),
		streamBody("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
			" /XStep 10 /YStep 10 /Resources "+res("8 0 R")+x.mark, x.paint+draw),
		image("B"),
		"<< /Type /Annot /Subtype /Square /Rect [60 10 80 30] /F 4 /AP << /N 10 0 R >> >>",
		streamBody("/BBox [0 0 20 20] /OPI << /2.0 << /Type /OPI /Version 2.0 /MainImage (x.tif) >> >>"+
			" /Resources "+res("13 0 R")+x.mark, x.apPaint+draw),
		"<< /Type /Annot /Subtype /Square /Rect [90 10 110 30] /F 4 /AS /On /AP << /N << /On 14 0 R >> >> >>",
		streamBody("", "1000 0 d0 "+x.paint+"q 1000 0 0 1000 0 0 cm /Im Do Q"),
		image("C"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Ref << /F (other.pdf) /Page 0 >>", "0 0 10 10 re f"),
	)
}

// graphicsReachRules are the XObject rules graphicsReachPDF breaks at
// PDF/A-2b, with the number of times each fails on the source.
var graphicsReachRules = []struct {
	rule compliance.Rule
	want int
}{
	{compliance.Rule_19005_2_6_2_8_t03, 3},
	{compliance.Rule_19005_2_6_2_9_t01, 1},
	{compliance.Rule_19005_2_6_2_9_t02, 1},
}

// TestFixGraphics_ReachesTheStreamsTheXObjectRulesJudge pins that the XObject
// repairs reach every stream compliance judges the XObject rules through: a
// Type 3 glyph procedure, a tiling pattern cell and an annotation appearance,
// which is itself a form whatever its /Subtype says (ISO 32000-1, 12.5.5).
//
// The repair walked the page's /XObject resources and the forms they name, so
// each of these kept ISO 19005-2, 6.2.8 t3 or 6.2.9 t1 and t2 through a
// PDF/A-2b remediation, on litho and veraPDF alike.
func TestFixGraphics_ReachesTheStreamsTheXObjectRulesJudge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, graphicsReachPDF(graphicsReachExtras{}))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, r := range graphicsReachRules {
		if n := len(failures(toViolations(errs), r.rule)); n != r.want {
			t.Fatalf("control: the source fails %s t%d %d times, want %d", r.rule.Clause, r.rule.TestNumber, n, r.want)
		}
	}

	if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b)); err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}

	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	for _, r := range graphicsReachRules {
		for _, v := range failures(after, r.rule) {
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}

// TestRemediatePDFA_GraphicsReachKeepsThePeersWrites pins that the repairs
// below the page build on the fonts, patterns and appearances the peers write
// around them, and they on the repairs.
//
// The page, the glyph and the pattern cell paint in DeviceCMYK and DeviceRGB,
// so output-intent, ahead of graphics, adds a CMYK OutputIntent and injects
// /DefaultRGB into the resources of the page and of the Type 3 font, whose
// glyphs resolve their colour spaces against it (ISO 19005-2, 6.2.4.3). The
// font, the pattern and the first appearance each hold a graphics state
// written inline with a /TR (6.2.5), which graphics repairs by writing them,
// and a name longer than 127 bytes (6.1.13), which name-utf8 repairs after it
// by writing them again. A write rebuilt from the parsed object would put back
// what the other repaired, and the output would fail its rule again.
func TestRemediatePDFA_GraphicsReachKeepsThePeersWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, graphicsReachPDF(graphicsReachExtras{
		gs:      " /ExtGState << /G0 << /TR /Identity >> >>",
		paint:   "/G0 gs 0 0 0 1 k 1 0 0 rg 0 0 5 5 re f ",
		apPaint: "/G0 gs ",
		mark:    " /LithoMark /" + strings.Repeat("A", 130),
	}))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, clause := range []string{"6.1.13", "6.2.4.3", "6.2.5"} {
		n := 0
		for _, v := range toViolations(errs) {
			if v.Rule.Clause == clause {
				n++
			}
		}
		if n < 3 {
			t.Fatalf("control: the source fails %s %d times, want one for each stream", clause, n)
		}
	}
	if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA2b, Options{}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	for _, v := range after {
		switch v.Rule.Clause {
		case "6.1.13", "6.2.4.3", "6.2.5", "6.2.8", "6.2.9":
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}
