package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("cutcell_2b", cutcell2b) }

// cutcell2b builds cutcell_2b.pdf: an image whose samples the centred quarter's edges
// cut beside their centres, at PDF/A-2b.
//
// A sample paints the whole cell of the unit square it maps onto (ISO 32000-1,
// 8.9.4), and redaction blanked only the samples whose centres a region holds.
// A sample whose centre lies just outside the region kept painting the part of
// its cell inside it, up to half a sample deep: the pixel check refused ("pixels
// inside the region still paint removed content"), and with the check off the
// picture stayed inside the region. Corpus images are fine enough that the part
// stays within the check's two-pixel band, so no scenario of the redact-render
// cell reached it.
//
// The page is 400 by 400, its centred quarter [100 100 300 300]. One 12 by 10
// DeviceGray image, not interpolated, is drawn 360 points square at (19, 2.8),
// its samples 30 points wide and 36 high, a checker of white and light grey.
// The quarter's left edge cuts the cells of column 2 (x 79 to 109) 6 points past
// their centres and its right edge those of column 9 (289 to 319) 4 points short
// of theirs; its bottom edge cuts the cells of row 7 from the top (y 74.8 to
// 110.8) 7.2 points past their centres and its top edge those of row 1 (290.8 to
// 326.8) 8.8 points short of theirs. Each edge leaves a strip of picture 9 to 11
// points deep inside the region where blanking by centres alone keeps it. The
// band scenario, [190 210] on a page with no text, lies inside the cells of row
// 4 and holds their centres.
//
// The RGB output intent covers DeviceGray.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func cutcell2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	const width, height = 12, 10
	var samples []byte
	for y := range height {
		for x := range width {
			if (x+y)%2 == 0 {
				samples = append(samples, 0xFF)
			} else {
				samples = append(samples, 0xC0)
			}
		}
	}
	image := pdf.Stream(samples)
	image.Set("Type", n("XObject"))
	image.Set("Subtype", n("Image"))
	image.Set("Width", width)
	image.Set("Height", height)
	image.Set("ColorSpace", n("DeviceGray"))
	image.Set("BitsPerComponent", 8)

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d("XObject", d("Im", pdf.MakeIndirect(image))),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("q 360 0 0 360 19 2.8 cm /Im Do Q\n"))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
