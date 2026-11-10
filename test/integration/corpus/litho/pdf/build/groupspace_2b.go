package build

import (
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("groupspace_2b", groupspace2b) }

// groupspace2b builds groupspace_2b.pdf: pages whose transparency group blends in a
// CMYK or grey space, painting RGB colour, at PDF/A-2b.
//
// ISO 32000-1, 11.4.7 and Table 147 convert every colour painted into a page
// group into the group's /CS, so a page whose content paints with transparency
// reads in that space: an RGB colour outside the CMYK gamut reads as the CMYK
// colour nearest it, and every colour reads grey on a grey page. Flattening
// removes the group with the transparency, so what the composites leave of the
// page read in sRGB unless converted into the group's space. The corpora held
// no page whose group blends in CMYK or grey while painting colour of another
// space outside its transparent regions, so the flatten cell compared none.
//
// Each 100 x 200 page draws rows of eight 10 x 50 fills, and a 10 x 5 square at
// ca 0.5 in its top right corner, the transparency flattening composites:
//
// | Page | Group /CS                                  | Draws                       |
// | ---- | ------------------------------------------ | --------------------------- |
// | 1    | DeviceCMYK                                 | the rows below              |
// | 2    | ICCBased, version 4 CMYK (v4-cmyk-lut.icc) | the rows below              |
// | 3    | DeviceGray                                 | the rows below              |
// | 4    | DeviceCMYK                                 | form `F`, the rows below    |
// | 5    | none                                       | form `F` again, opaque      |
//
// | Row       | Draws                                                          |
// | --------- | -------------------------------------------------------------- |
// | y 140-190 | DeviceRGB fills from green to red, outside the CMYK gamut       |
// | y 80-130  | dark DeviceRGB greys, v = 0, 0.04 ... 0.28                      |
// | y 20-70   | `Im`, an 8 x 1 DeviceRGB image of the first row's colours       |
//
// Page 4 paints its rows through form `F`, which page 5 paints too: converting
// the form for page 4 would change page 5, which has no group, so page 4 keeps
// its colours (pdf.FlattenedPage.GroupSpaceUnconverted).
//
// See ../FIXTURES.md for the authoritative expected firing set.
func groupspace2b() ([]byte, error) {
	cmykProfile := "pkg/internal/graphics/icc/testdata/v4-cmyk-lut.icc"

	pdf := pdfgen.New()

	profile, err := repoFile(cmykProfile)
	if err != nil {
		return nil, err
	}
	cmykStream := pdf.Stream(zdeflate.Compress(profile, 9))
	cmykStream.Set("N", 4)
	cmykStream.Set("Filter", n("FlateDecode"))
	iccCMYK := pdf.MakeIndirect(a(n("ICCBased"), pdf.MakeIndirect(cmykStream)))

	colours := make([][3]float64, 8)
	greys := make([]float64, 8)
	for i := range 8 {
		colours[i] = [3]float64{float64(i) / 7, 1 - float64(i)/7, 0.3 * float64(i%2)}
		greys[i] = 0.04 * float64(i)
	}

	// rgbImage returns a new 8 x 1 DeviceRGB image of the colours: each page has its
	// own, which no page converted to another space shares.
	rgbImage := func() pdfgen.Object {
		var samples []byte
		for _, rgb := range colours {
			for _, c := range rgb {
				samples = append(samples, byte(math.RoundToEven(255*c)))
			}
		}
		image := pdf.Stream(samples)
		image.Set("Type", n("XObject"))
		image.Set("Subtype", n("Image"))
		image.Set("Width", len(colours))
		image.Set("Height", 1)
		image.Set("BitsPerComponent", 8)
		image.Set("ColorSpace", n("DeviceRGB"))
		return pdf.MakeIndirect(image)
	}

	var rows strings.Builder
	for i, c := range colours {
		fmt.Fprintf(&rows, "%.4f %.4f %.4f rg %d 140 10 50 re f ", c[0], c[1], c[2], 10+10*i)
	}
	rows.WriteString("q ")
	for i, v := range greys {
		fmt.Fprintf(&rows, "%.2f %.2f %.2f rg %d 80 10 50 re f ", v, v, v, 10+10*i)
	}
	rows.WriteString("Q q 80 0 0 50 10 20 cm /Im Do Q\n")
	const square = "q /H gs 0.2 0.4 0.9 rg 90 195 10 5 re f Q\n"

	states := d("H", d("Type", n("ExtGState"), "ca", 0.5))

	addPage := func(content string, resources *pdfgen.Dict, cs any) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 200),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		if cs != nil {
			page.Set("Group", d("Type", n("Group"), "S", n("Transparency"), "CS", cs))
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	for _, cs := range []any{n("DeviceCMYK"), iccCMYK, n("DeviceGray")} {
		addPage(rows.String()+square, d("ExtGState", states, "XObject", d("Im", rgbImage())), cs)
	}

	formStream := pdf.Stream([]byte(rows.String()))
	formStream.Set("Type", n("XObject"))
	formStream.Set("Subtype", n("Form"))
	formStream.Set("BBox", a(0, 0, 100, 200))
	formStream.Set("Resources", d("XObject", d("Im", rgbImage())))
	form := pdf.MakeIndirect(formStream)

	addPage("/F Do "+square, d("ExtGState", d("H", states.Get("H")), "XObject", d("F", form)), n("DeviceCMYK"))
	addPage("/F Do\n", d("XObject", d("F", form)), nil)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
