package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestBlendingSpaceCoverage pins which transparency blending spaces cover a
// device family under PDF/A-4 (see [blendingSpaceType] and [blendCovers]): an
// RGB ICC profile or a CalRGB space covers DeviceRGB, no blending space covers
// DeviceGray, and a group /CS naming a /ColorSpace entry reads as no colour
// space. Every count was measured with veraPDF 1.30 at PDF/A-4 on the same
// fixture.
func TestBlendingSpaceCoverage(t *testing.T) {
	t.Parallel()

	rgbProfile := defaultStream("/N 3", iccHeaderOnly("mntr", "RGB "))
	grayProfile := defaultStream("/N 1", iccHeaderOnly("mntr", "GRAY"))
	rgb := func(want int) []namesCount { return []namesCount{{models.PDFA4, "6.2.4.3", 2, want}} }
	gray := func(want int) []namesCount { return []namesCount{{models.PDFA4, "6.2.4.3", 4, want}} }
	group := func(cs, extra string) string {
		return "/Group << /Type /Group /S /Transparency /CS " + cs + extra + " >>"
	}
	isolated := func(res, body, cs string) string {
		return defaultStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources "+res+" "+group(cs, " /I true"), body)
	}
	const calRGB = "[/CalRGB << /WhitePoint [0.9505 1 1.089] >>]"
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "a page group over an RGB profile",
			data:   defaultSpacePDF(group("[/ICCBased 5 0 R]", ""), "", "1 0 0 rg "+namesFill, rgbProfile),
			counts: rgb(0),
		},
		{
			name:   "a page group naming an entry over an RGB profile",
			data:   defaultSpacePDF(group("/CS0", ""), "/ColorSpace << /CS0 [/ICCBased 5 0 R] >>", "1 0 0 rg "+namesFill, rgbProfile),
			counts: rgb(2),
		},
		{
			name:   "a page group over a gray profile",
			data:   defaultSpacePDF(group("[/ICCBased 5 0 R]", ""), "", "0.5 g "+namesFill, grayProfile),
			counts: gray(2),
		},
		{
			name:   "an isolated form group over an RGB profile",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do", isolated("<< >>", "1 0 0 rg "+namesFill, "[/ICCBased 6 0 R]"), rgbProfile),
			counts: rgb(0),
		},
		{
			name:   "an isolated form group over a CalRGB space",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do", isolated("<< >>", "1 0 0 rg "+namesFill, calRGB)),
			counts: rgb(0),
		},
		{
			name: "an isolated form group naming the form's entry",
			data: defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do",
				isolated("<< /ColorSpace << /CS0 [/ICCBased 6 0 R] >> >>", "1 0 0 rg "+namesFill, "/CS0"), rgbProfile),
			counts: rgb(2),
		},
		{
			name: "an isolated form group naming the page's entry",
			data: defaultSpacePDF("", "/XObject << /X0 5 0 R >> /ColorSpace << /CS0 [/ICCBased 6 0 R] >>", "/X0 Do",
				isolated("<< >>", "1 0 0 rg "+namesFill, "/CS0"), rgbProfile),
			counts: rgb(2),
		},
		{
			name:   "an isolated form group over a gray profile",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do", isolated("<< >>", "0.5 g "+namesFill, "[/ICCBased 6 0 R]"), grayProfile),
			counts: gray(2),
		},
		{
			name:   "an isolated form group over a CalGray space",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do", isolated("<< >>", "0.5 g "+namesFill, "[/CalGray << /WhitePoint [0.9505 1 1.089] >>]")),
			counts: gray(2),
		},
		{
			name:   "an isolated form group over a Lab space",
			data:   defaultSpacePDF("", "/XObject << /X0 5 0 R >>", "/X0 Do", isolated("<< >>", "0.5 g "+namesFill, "[/Lab << /WhitePoint [0.9505 1 1.089] >>]")),
			counts: gray(2),
		},
	})
}
