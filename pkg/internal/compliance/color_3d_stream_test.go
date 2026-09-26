package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// threeDPDF returns a one-page document whose page resources are res and
// whose one annotation is a 3D annotation holding dd as /3DD, with extra
// ending its dictionary. The 3D stream, whose dictionary is streamDict, is
// object 6, and more objects follow from object 7.
func threeDPDF(res, dd, extra, streamDict string, more ...string) []byte {
	objs := append([]string{
		"<< /Type /Annot /Subtype /3D /Rect [0 0 10 10] /3DD " + dd + extra + " >>",
		defaultStream(streamDict, "U3D"),
	}, more...)
	return defaultSpacePDF("/Annots [5 0 R]", res, "", objs...)
}

// threeDCounts returns the counts of a rule at PDF/A-1b, PDF/A-2b and PDF/A-4:
// clause1 and test1 under ISO 19005-1, clause2 and test2 under ISO 19005-2 and
// ISO 19005-4.
func threeDCounts(clause1 string, test1 int, clause2 string, test2, want int) []namesCount {
	return []namesCount{{models.PDFA1b, clause1, test1, want}, {models.PDFA2b, clause2, test2, want}, {models.PDFA4, clause2, test2, want}}
}

// TestThreeDStreamColourSpace pins the colour space veraPDF builds for the 3D
// stream of a 3D annotation (see [colorLinkWalk.judge3D]): the stream's
// /ColorSpace alone, a name read through the page's resources, any other array
// read with no resources. Every count was measured with veraPDF 1.30 at
// PDF/A-1b, PDF/A-2b and PDF/A-4 on the same fixture.
func TestThreeDStreamColourSpace(t *testing.T) {
	t.Parallel()

	const d3 = "/Type /3D /Subtype /U3D"
	abstractRGB := defaultStream("/N 3", iccHeaderOnly("abst", "RGB "))
	rgb := func(want int) []namesCount { return threeDCounts("6.2.3.3", 1, "6.2.4.3", 2, want) }
	cmyk := func(want int) []namesCount { return threeDCounts("6.2.3.3", 2, "6.2.4.3", 3, want) }
	gray := func(want int) []namesCount { return threeDCounts("6.2.3.3", 3, "6.2.4.3", 4, want) }
	profile := func(want int) []namesCount { return threeDCounts("6.2.3.2", 1, "6.2.4.2", 1, want) }
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "a device family",
			data:   threeDPDF("", "6 0 R", "", d3+" /ColorSpace /DeviceRGB"),
			counts: rgb(1),
		},
		{
			name:   "a device family in a one-element array",
			data:   threeDPDF("", "6 0 R", "", d3+" /ColorSpace [/DeviceRGB]"),
			counts: rgb(1),
		},
		{
			name:   "an ICCBased space over an abstract profile",
			data:   threeDPDF("", "6 0 R", "", d3+" /ColorSpace [/ICCBased 7 0 R]", abstractRGB),
			counts: joinNamesCounts(profile(1), rgb(0)),
		},
		{
			name:   "an indirect ICCBased space",
			data:   threeDPDF("", "6 0 R", "", d3+" /ColorSpace 7 0 R", "[/ICCBased 8 0 R]", abstractRGB),
			counts: profile(1),
		},
		{
			name:   "an /Indexed space over DeviceRGB, the page's DefaultRGB notwithstanding",
			data:   threeDPDF("/ColorSpace << /DefaultRGB [/ICCBased 7 0 R] >>", "6 0 R", "", d3+" /ColorSpace [/Indexed /DeviceRGB "+namesIdx+"]", abstractRGB),
			counts: joinNamesCounts(rgb(1), profile(0)),
		},
		{
			name:   "a device family the page's DefaultRGB replaces",
			data:   threeDPDF("/ColorSpace << /DefaultRGB [/ICCBased 7 0 R] >>", "6 0 R", "", d3+" /ColorSpace /DeviceRGB", abstractRGB),
			counts: joinNamesCounts(rgb(0), profile(1)),
		},
		{
			name:   "a Separation",
			data:   threeDPDF("", "6 0 R", "", d3+" /ColorSpace [/Separation /Foo /DeviceCMYK "+namesFn1+"]"),
			counts: cmyk(1),
		},
		{
			name:   "a name of a page's /ColorSpace entry",
			data:   threeDPDF("/ColorSpace << /CS0 [/Separation /Foo /DeviceCMYK "+namesFn1+"] >>", "6 0 R", "", d3+" /ColorSpace /CS0"),
			counts: joinNamesCounts(cmyk(1), gray(0)),
		},
		{
			name:   "a name nothing defines",
			data:   threeDPDF("", "6 0 R", "", d3+" /ColorSpace /CS9"),
			counts: gray(0),
		},
		{
			name:   "an /Indexed base naming an entry, read with no resources",
			data:   threeDPDF("/ColorSpace << /CS0 /DeviceRGB >>", "6 0 R", "", d3+" /ColorSpace [/Indexed /CS0 "+namesIdx+"]"),
			counts: joinNamesCounts(gray(1), rgb(0)),
		},
		{
			name:   "a colour space of the stream's /Resources",
			data:   threeDPDF("", "6 0 R", "", d3+" /Resources << /ColorSpace << /C1 /DeviceRGB >> >>"),
			counts: rgb(0),
		},
		{
			name: "the /3DD of a /3DV view",
			data: threeDPDF("", "6 0 R", " /3DV << /3DD 7 0 R >>", d3,
				defaultStream(d3+" /ColorSpace /DeviceRGB", "U3D")),
			counts: rgb(0),
		},
		{
			name:   "a stream with no /Type /3D",
			data:   threeDPDF("", "6 0 R", "", "/Subtype /U3D /ColorSpace /DeviceRGB"),
			counts: rgb(0),
		},
		{
			name:   "the stream of a 3D reference dictionary",
			data:   threeDPDF("", "7 0 R", "", d3+" /ColorSpace /DeviceRGB", "<< /Type /3DRef /3DD 6 0 R >>"),
			counts: rgb(1),
		},
	})
}
