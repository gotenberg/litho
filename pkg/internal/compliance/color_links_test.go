package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// linkPagePDF builds a one-page PDF whose /Resources is res and whose content
// is content (object 4). Extra objects follow from object 5.
func linkPagePDF(res, content string, extra ...string) []byte {
	return cyclePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}, extra...)...)
}

// linkStreamObj returns a stream object whose dictionary holds dict.
func linkStreamObj(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// TestDeviceColorCountsEachLink pins that the device colour tests fail once per
// link veraPDF makes to a device colour space (see [colorLinkWalk]). Every
// count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same
// fixture.
func TestDeviceColorCountsEachLink(t *testing.T) {
	t.Parallel()

	const (
		fill   = "0 0 10 10 re f"
		calRGB = "/DefaultRGB [/CalRGB << /WhitePoint [0.9505 1 1.089] >>]"
		gray   = 3
		rgb    = 1
		cmyk   = 2
	)
	image := linkStreamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "abc")
	mask := linkStreamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ImageMask true /BitsPerComponent 1", "\x00")
	form := func(content string) string {
		return linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >>", content)
	}
	indexed := "[/Indexed /DeviceRGB 1 <000000FFFFFF>]"
	shading := "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 10 0] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >> >>"
	tiling := "/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 50 50] /XStep 50 /YStep 50 /Resources << >>"
	type3 := "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << /a 6 0 R /b 7 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
		"/FirstChar 97 /LastChar 98 /Widths [1000 1000] >>"
	inlineMask := "BI /W 1 /H 1 /IM true ID \x00 EI"

	tests := []struct {
		name   string
		data   []byte
		family int
		want   int
	}{
		{"rg and the f after it", linkPagePDF("<< >>", "1 0 0 rg "+fill), rgb, 2},
		{"a bare fill in the initial DeviceGray", linkPagePDF("<< >>", fill), gray, 1},
		{"g, scn and f, but not sc", linkPagePDF("<< >>", "0.5 g 0.3 scn 0.2 sc "+fill), gray, 3},
		{"k and the f after it", linkPagePDF("<< >>", "0 0 0 1 k "+fill), cmyk, 2},
		{"a family name cs selects", linkPagePDF("<< >>", "/DeviceRGB cs 1 0 0 scn "+fill), rgb, 3},
		{"stroked text after RG", linkPagePDF("<< /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>",
			"1 0 0 RG BT 1 Tr /F1 12 Tf (a) Tj (b) Tj ET"), rgb, 3},
		{"an image painted twice", linkPagePDF("<< /XObject << /Im0 5 0 R >> >>", "/Im0 Do /Im0 Do", image), rgb, 1},
		{"an inline image painted twice", linkPagePDF("<< >>",
			"BI /W 1 /H 1 /BPC 8 /CS /RGB ID abc EI\nq Q\nBI /W 1 /H 1 /BPC 8 /CS /RGB ID abc EI"), rgb, 2},
		{"an image mask first painted after rg", linkPagePDF("<< /XObject << /Im0 5 0 R >> >>", "q 1 0 0 rg /Im0 Do Q /Im0 Do", mask), rgb, 2},
		{"no DeviceGray for a mask first painted after rg", linkPagePDF("<< /XObject << /Im0 5 0 R >> >>", "q 1 0 0 rg /Im0 Do Q /Im0 Do", mask), gray, 0},
		{"a form painted twice", linkPagePDF("<< /XObject << /Fm0 5 0 R >> >>", "/Fm0 Do /Fm0 Do", form("1 0 0 rg 0 0 1 1 re f")), rgb, 2},
		{"a form filling with the page's rg", linkPagePDF("<< /XObject << /Fm0 5 0 R >> >>", "1 0 0 rg /Fm0 Do", form("0 0 1 1 re f")), rgb, 2},
		{"a direct indexed space", linkPagePDF("<< /ColorSpace << /CS0 "+indexed+" >> >>", "/CS0 cs 0 scn "+fill), rgb, 3},
		{"an indirect indexed space", linkPagePDF("<< /ColorSpace << /CS0 5 0 R >> >>", "/CS0 cs 0 scn "+fill, indexed), rgb, 1},
		{"a direct shading painted twice", linkPagePDF("<< /Shading << /Sh0 "+shading+" >> >>", "/Sh0 sh /Sh0 sh"), rgb, 2},
		{"an indirect shading painted twice", linkPagePDF("<< /Shading << /Sh0 5 0 R >> >>", "/Sh0 sh /Sh0 sh", shading), rgb, 1},
		{"a shading pattern the page's DefaultRGB leaves", linkPagePDF("<< /ColorSpace << "+calRGB+" >> /Pattern << /P0 5 0 R >> >>",
			"/Pattern cs /P0 scn "+fill, "<< /Type /Pattern /PatternType 2 /Shading "+shading+" >>"), rgb, 1},
		{"a form without resources the page's DefaultRGB leaves", linkPagePDF("<< /ColorSpace << "+calRGB+" >> /XObject << /Fm0 5 0 R >> >>",
			"/Fm0 Do", linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "1 0 0 rg 0 0 1 1 re f")), rgb, 2},
		{"a tiling cell a form paints after the page's rg", linkPagePDF("<< /XObject << /Fm0 5 0 R >> >>", "1 0 0 rg /Fm0 Do",
			linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /Pattern << /P0 6 0 R >> >>", "/Pattern cs /P0 scn 0 0 5 5 re f"),
			linkStreamObj(tiling, "5 5 40 40 re f")), rgb, 2},
		{"a Type 3 glyph filling with the text's colour", linkPagePDF("<< /Font << /T3 5 0 R >> >>", "1 0 0 rg BT /T3 12 Tf (a) Tj ET",
			type3, linkStreamObj("", "1000 0 d0 0 0 1 1 re f"), linkStreamObj("", "1000 0 d0")), rgb, 3},
		{"an inline mask past a d1", linkPagePDF("<< /Font << /T3 5 0 R >> >>", "BT /T3 12 Tf (a) Tj ET",
			type3, linkStreamObj("", "1000 0 0 0 1 1 d1 "+inlineMask), linkStreamObj("", "1000 0 0 0 1 1 d1 1 0 0 rg 0 0 1 1 re f")), gray, 2},
		{"a null colour space entry", linkPagePDF("<< /ColorSpace << /CS0 null >> >>", "/CS0 cs 0 scn "+fill), gray, 3},
		{"an undefined colour space name", linkPagePDF("<< >>", "/CS0 cs 0 scn "+fill), gray, 3},
	}
	profiles := []struct {
		profile models.PDFAProfile
		clause  string
		tests   [4]int
	}{
		{models.PDFA1b, "6.2.3.3", [4]int{0, 1, 2, 3}},
		{models.PDFA2b, "6.2.4.3", [4]int{0, 2, 3, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range profiles {
				if got := ruleContexts(t, tt.data, p.profile, p.clause, p.tests[tt.family]); len(got) != tt.want {
					t.Errorf("%s: %s t%d fired %d times, want %d", p.profile, p.clause, p.tests[tt.family], len(got), tt.want)
				}
			}
		})
	}
}

// TestDeviceNColorantsCountEachLink pins that ISO 19005-2, 6.2.4.4 t1 fails
// once per link veraPDF makes to a DeviceN naming a spot colourant its
// attributes do not list, however many it misses (see
// [checkDeviceNColorantLinks]). Every count was measured with veraPDF 1.30 at
// PDF/A-2b on the same fixture.
func TestDeviceNColorantsCountEachLink(t *testing.T) {
	t.Parallel()

	fill := "0 0 10 10 re f"
	spots := "[/DeviceN [/SpotA /SpotB /SpotC] /DeviceGray " + tintTransformObj + "]"
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"a direct DeviceN three operators link to", linkPagePDF("<< /ColorSpace << /CS0 "+spots+" >> >>", "/CS0 cs 0 0 0 scn "+fill), 3},
		{"an indirect DeviceN", linkPagePDF("<< /ColorSpace << /CS0 5 0 R >> >>", "/CS0 cs 0 0 0 scn "+fill, spots), 1},
		{"an indexed space's DeviceN base", linkPagePDF("<< /ColorSpace << /CS0 [/Indexed "+spots+" 1 <000000FFFFFF>] >> >>", "/CS0 cs 0 scn "+fill), 3},
		{"a DeviceN listing its colourants", linkPagePDF("<< /ColorSpace << /CS0 [/DeviceN [/SpotA] /DeviceGray "+tintTransformObj+
			" << /Colorants << /SpotA [/Separation /SpotA /DeviceGray "+tintTransformObj+"] >> >>] >> >>", "/CS0 cs 0 scn "+fill), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ruleContexts(t, tt.data, models.PDFA2b, "6.2.4.4", 1); len(got) != tt.want {
				t.Errorf("6.2.4.4 t1 fired %d times, want %d: %q", len(got), tt.want, got)
			}
		})
	}
}

// TestSeparationConsistencyCountsEachLink pins that ISO 19005-2, 6.2.4.4 t2
// fails at every link to a Separation whose colourant another Separation
// veraPDF has built by then defines differently (see [checkSeparationLinks]
// and [walkColorLinksDeclaring]). Every count was measured with veraPDF 1.30
// at PDF/A-2b on the same fixture.
func TestSeparationConsistencyCountsEachLink(t *testing.T) {
	t.Parallel()

	gray := "[/Separation /Spot1 /DeviceGray << /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>]"
	rgb := "[/Separation /Spot1 /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>]"
	use := func(name string) string { return name + " cs 1 scn 0 0 10 10 re f " }
	form := func(cs string) string {
		return linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace << /CS1 "+cs+" >> >>", use("/CS1"))
	}
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"two direct Separations", linkPagePDF("<< /ColorSpace << /CS0 "+gray+" /CS1 "+rgb+" >> >>", use("/CS0")+use("/CS1")), 6},
		{"two indirect Separations", linkPagePDF("<< /ColorSpace << /CS0 5 0 R /CS1 6 0 R >> >>", use("/CS0")+use("/CS1"), gray, rgb), 2},
		{"the same definition twice", linkPagePDF("<< /ColorSpace << /CS0 "+gray+" /CS1 "+gray+" >> >>", use("/CS0")+use("/CS1")), 0},
		{"the second in a form painted between", linkPagePDF("<< /ColorSpace << /CS0 "+gray+" >> /XObject << /Fm0 5 0 R >> >>",
			use("/CS0")+"/Fm0 Do "+use("/CS0"), form(rgb)), 9},
		{"the second an indexed space's base", linkPagePDF("<< /ColorSpace << /CS0 "+gray+" /CS1 [/Indexed "+rgb+" 1 <00FF>] >> >>",
			use("/CS0")+"/CS1 cs 0 scn 0 0 10 10 re f"), 3},
		{"the second in an annotation's appearance", cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /ColorSpace << /CS0 "+gray+" >> >> /Contents 4 0 R /Annots [6 0 R] >>",
			linkStreamObj("", use("/CS0")), form(rgb),
			"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 5 0 R >> >>"), 6},
		{"the second on a later page", cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /ColorSpace << /CS0 "+gray+" >> >> /Contents 5 0 R >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /ColorSpace << /CS0 "+rgb+" >> >> /Contents 6 0 R >>",
			linkStreamObj("", use("/CS0")), linkStreamObj("", use("/CS0")+" ")), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ruleContexts(t, tt.data, models.PDFA2b, "6.2.4.4", 2); len(got) != tt.want {
				t.Errorf("6.2.4.4 t2 fired %d times, want %d: %q", len(got), tt.want, got)
			}
		})
	}
}

// TestICCBasedProfileCountsEachLink pins that the ICCBased profile test fails
// once per link veraPDF makes to an ICCBased space over an abstract profile,
// wherever [colorLinkWalk] reaches one, and once for the document for a direct
// four-component space (see [checkICCBasedProfiles]). Every count was measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same fixture.
func TestICCBasedProfileCountsEachLink(t *testing.T) {
	t.Parallel()

	profile := func(space string, n, pad int) string {
		data := []byte(iccHeaderOnly("abst", space))
		data = append(data, make([]byte, pad)...)
		return fmt.Sprintf("<< /N %d /Length %d >>\nstream\n%s\nendstream", n, len(data), data)
	}
	rgb, cmyk, cmyk2 := profile("RGB ", 3, 0), profile("CMYK", 4, 0), profile("CMYK", 4, 4)
	fill := "0 0 10 10 re f"
	fn := "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>"
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"a direct space three operators link to", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >>", "/CS0 cs 0 0 0 scn "+fill, rgb), 3},
		{"an indirect space", linkPagePDF("<< /ColorSpace << /CS0 6 0 R >> >>", "/CS0 cs 0 0 0 scn "+fill, rgb, "[/ICCBased 5 0 R]"), 1},
		{"a Separation's alternate", linkPagePDF("<< /ColorSpace << /CS0 [/Separation /Spot [/ICCBased 5 0 R] "+fn+"] >> >>", "/CS0 cs 1 scn "+fill, rgb), 3},
		{"an indexed space's base", linkPagePDF("<< /ColorSpace << /CS0 [/Indexed [/ICCBased 5 0 R] 1 <000000FFFFFF>] >> >>", "/CS0 cs 0 scn "+fill, rgb), 3},
		{"the page group's blending space", cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R /Group << /S /Transparency /CS [/ICCBased 5 0 R] >> >>",
			"<< /Length 0 >>\nstream\n\nendstream", rgb), 1},
		{"a direct CMYK space three operators link to", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >>", "/CS0 cs 0 0 0 1 scn "+fill, cmyk), 1},
		{"two direct CMYK spaces over different profiles", linkPagePDF("<< /ColorSpace << /CS0 [/ICCBased 5 0 R] /CS1 [/ICCBased 6 0 R] >> >>",
			"/CS0 cs 0 0 0 1 scn "+fill+" /CS1 cs 0 0 0 1 scn "+fill, cmyk, cmyk2), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range []struct {
				profile models.PDFAProfile
				clause  string
			}{{models.PDFA1b, "6.2.3.2"}, {models.PDFA2b, "6.2.4.2"}} {
				if got := ruleContexts(t, tt.data, p.profile, p.clause, 1); len(got) != tt.want {
					t.Errorf("%s: %s t1 fired %d times, want %d", p.profile, p.clause, len(got), tt.want)
				}
			}
		})
	}
}
