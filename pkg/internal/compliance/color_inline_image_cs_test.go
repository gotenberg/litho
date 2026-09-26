package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestInlineImageColourSpaces pins the colour space veraPDF gives an inline
// image that is no mask: the one its /CS names or writes out, and DeviceGray
// when it gives none or one veraPDF cannot read, such as an array
// abbreviating Indexed to I (see [walkColorSpaceLinksWith]). Every count was
// measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixture.
func TestInlineImageColourSpaces(t *testing.T) {
	t.Parallel()

	const (
		image   = "BI /W 1 /H 1 /BPC 8 "
		noCS    = image + "ID a EI"
		fill    = "0 0 10 10 re f"
		indexed = "1 <000000FFFFFF>]"
	)
	form := func(content string) string {
		return linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", content)
	}
	tests := []struct {
		name      string
		data      []byte
		rgb, gray int
	}{
		{"an image with no colour space", linkPagePDF("<< >>", noCS), 0, 1},
		{"one painted twice", linkPagePDF("<< >>", noCS+"\nq Q\n"+noCS), 0, 2},
		{"one in a form painted twice", linkPagePDF("<< /XObject << /Fm0 5 0 R >> >>", "/Fm0 Do /Fm0 Do", form(noCS)), 0, 1},
		{"a gray one in a form painted twice", linkPagePDF("<< /XObject << /Fm0 5 0 R >> >>", "/Fm0 Do /Fm0 Do", form(image+"/CS /G ID a EI")), 0, 1},
		{"one writing an indexed space out", linkPagePDF("<< >>", image+"/CS [/Indexed /DeviceRGB "+indexed+" ID a EI"), 1, 0},
		{"one abbreviating Indexed", linkPagePDF("<< >>", image+"/CS [/I /DeviceRGB "+indexed+" ID a EI"), 0, 1},
		{"one abbreviating its base", linkPagePDF("<< >>", image+"/CS [/Indexed /RGB "+indexed+" ID a EI"), 0, 1},
		{"one naming an indexed entry", linkPagePDF("<< /ColorSpace << /CS0 [/Indexed /DeviceRGB "+indexed+" >> >>", image+"/CS /CS0 ID a EI"), 1, 0},
		{"an entry abbreviating Indexed", linkPagePDF("<< /ColorSpace << /CS0 [/I /DeviceRGB "+indexed+" >> >>", "/CS0 cs 0 scn "+fill), 0, 3},
		{"one in an uncoloured cell", linkPagePDF("<< /ColorSpace << /CSp [/Pattern /DeviceGray] >> /Pattern << /P2 5 0 R >> >>",
			"/CSp cs 0.5 /P2 scn "+fill,
			linkStreamObj("/Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", noCS)), 0, 0},
	}
	profiles := []struct {
		profile   models.PDFAProfile
		clause    string
		rgb, gray int
	}{
		{models.PDFA1b, "6.2.3.3", 1, 3},
		{models.PDFA2b, "6.2.4.3", 2, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range profiles {
				if got := ruleContexts(t, tt.data, p.profile, p.clause, p.rgb); len(got) != tt.rgb {
					t.Errorf("%s: DeviceRGB %s t%d fired %d times, want %d", p.profile, p.clause, p.rgb, len(got), tt.rgb)
				}
				if got := ruleContexts(t, tt.data, p.profile, p.clause, p.gray); len(got) != tt.gray {
					t.Errorf("%s: DeviceGray %s t%d fired %d times, want %d", p.profile, p.clause, p.gray, len(got), tt.gray)
				}
			}
		})
	}
}
