package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

// deviceNReachExtras is what [deviceNReachPDF] adds: gs to the /Resources of
// the form, the pattern, the Type 3 font and the appearance, paint to the
// content of the page and of each of those, mark to the dictionaries of the
// form, the pattern, the font, the appearance and the image, and image to the
// image's dictionary.
type deviceNReachExtras struct {
	gs, paint, mark, image string
}

// deviceNReachPDF builds a one-page document whose DeviceN spaces, each over
// one spot colorant and short of /Colorants, stand where compliance judges
// them outside the page's own /ColorSpace entries: in the resources of a form
// the page draws (SpotA), as the colour space of an image it draws (SpotB),
// in the resources of a coloured tiling pattern it fills with (SpotC), as the
// base of an /Indexed space it selects (SpotD), in the resources of the Type 3
// font whose glyph it shows (SpotE) and in those of an annotation's normal
// appearance (SpotF). Object 4 is the tint transform every DeviceN shares.
func deviceNReachPDF(x deviceNReachExtras) []byte {
	devn := func(spot string) string { return "[/DeviceN [/" + spot + "] /DeviceGray 4 0 R]" }
	res := func(spot string) string {
		return "<< /ColorSpace << /CS " + devn(spot) + " >>" + x.gs + " >>"
	}
	const fill = "/CS cs 1 scn 0 0 5 5 re f"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 5 0 R /Annots [10 0 R]"+
			" /Resources << /XObject << /Fm0 6 0 R /Im0 7 0 R >> /Pattern << /P0 8 0 R >> /Font << /T3 9 0 R >>"+
			" /ColorSpace << /CSI [/Indexed "+devn("SpotD")+" 1 <00ff>] >> >> >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
		streamBody("", x.paint+"/Fm0 Do q 5 0 0 5 0 0 cm /Im0 Do Q /Pattern cs /P0 scn 0 0 50 50 re f"+
			" /CSI cs 1 sc 0 0 5 5 re f BT /T3 12 Tf 10 10 Td (a) Tj ET"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources "+res("SpotA")+x.mark, x.paint+fill),
		streamBody("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace "+
			devn("SpotB")+x.mark+x.image, "A"),
		streamBody("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
			" /Resources "+res("SpotC")+x.mark, x.paint+fill),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /a 12 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>"+
			" /FirstChar 97 /LastChar 97 /Widths [1000] /Resources "+res("SpotE")+x.mark+" >>",
		"<< /Type /Annot /Subtype /Square /Rect [60 10 80 30] /F 4 /AP << /N 11 0 R >> >>",
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Resources "+res("SpotF")+x.mark, x.paint+fill),
		streamBody("", "1000 0 d0 "+x.paint+fill),
	)
}

// TestFixDeviceNColorants_ReachesEveryDeviceNCompliancejudges pins that the
// /Colorants repair completes every DeviceN ISO 19005-2, 6.2.4.4 t1 judges,
// wherever the space a colour link reaches stands.
//
// The repair walked the page's /ColorSpace entries and its shadings, so a
// DeviceN in the resources of a form, a tiling pattern, a Type 3 font or an
// annotation appearance, one an image is painted in and one an /Indexed space
// is built on kept the finding through remediation. Completing /Colorants
// adds a dictionary to the space's attributes and leaves its components as
// they are (ISO 32000-1, 8.6.6.5), so an image's samples and an /Indexed
// lookup table read the same.
func TestFixDeviceNColorants_ReachesEveryDeviceNCompliancejudges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNReachPDF(deviceNReachExtras{}))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	before := failures(toViolations(errs), compliance.Rule_19005_2_6_2_4_4_t01)
	for _, spot := range []string{"SpotA", "SpotB", "SpotC", "SpotD", "SpotE", "SpotF"} {
		found := false
		for _, v := range before {
			found = found || strings.Contains(v.Context, spot)
		}
		if !found {
			t.Fatalf("control: the source fails 6.2.4.4 t1 for no DeviceN over %s: %v", spot, before)
		}
	}

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b)); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}

	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	for _, v := range after {
		if v.Rule.Clause == "6.2.4.4" {
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}

// TestRemediatePDFA_DeviceNReachKeepsThePeersWrites pins that completing the
// DeviceN spaces below the page builds on the objects the peers ahead of
// devicen rewrote, and that the peers after it build on the completion.
//
// Each container holding a DeviceN, the form, the pattern, the Type 3 font
// and the appearance, holds a graphics state written inline with a /TR (ISO
// 19005-2, 6.2.5), which graphics repairs by writing the container, and a
// name longer than 127 bytes (6.1.13), which name-utf8 repairs by writing it
// again, both ahead of devicen; the image carries /Interpolate true (6.2.8),
// which graphics repairs in the image's dictionary, where the image's DeviceN
// is written inline. A completion rebuilt from the parsed object would put
// back what they repaired.
func TestRemediatePDFA_DeviceNReachKeepsThePeersWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNReachPDF(deviceNReachExtras{
		gs:    " /ExtGState << /G0 << /TR /Identity >> >>",
		paint: "/G0 gs ",
		mark:  " /LithoMark /" + strings.Repeat("A", 130),
		image: " /Interpolate true",
	}))
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, clause := range []string{"6.1.13", "6.2.4.4", "6.2.5", "6.2.8"} {
		n := 0
		for _, v := range toViolations(errs) {
			if v.Rule.Clause == clause {
				n++
			}
		}
		if n == 0 {
			t.Fatalf("control: the source fails no %s, so there is no peer write to lose", clause)
		}
	}
	if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA2b, Options{}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	for _, v := range after {
		switch v.Rule.Clause {
		case "6.1.13", "6.2.4.4", "6.2.5", "6.2.8":
			t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}
