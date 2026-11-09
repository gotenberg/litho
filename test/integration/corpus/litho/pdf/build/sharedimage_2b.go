package build

import (
	"bytes"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharedimage_2b", sharedimage2b) }

// sharedimage2b builds sharedimage_2b.pdf: images page 1 shares with page 2, at PDF/A-2b.
//
// A redaction blanks the samples a region covers by rewriting the image, which
// every other placement of the image shows too. Where the regions do not cover
// the same samples at every placement, litho blanks a copy instead: the copy's
// covered samples are cleared, the copy joins page 1's resources under a new
// name, and only the covered Do paints it. No corpus file in the litho corpus
// painted one image in two places, so no cell reached the copy.
//
// Both images are 4 by 4 DeviceGray, which the RGB output intent covers. Each
// page names them through the one /Resources dictionary both pages share:
//
// | Image   | Samples | Soft mask        | Page 1                      | Page 2       |
// | ------- | ------- | ---------------- | --------------------------- | ------------ |
// | `Logo`  | 0x80    | none             | at (50, 250) and (250, 250) | at (50, 250) |
// | `Photo` | 0x40    | 4 by 4, all 0xC0 | at (150, 50)                | at (150, 50) |
//
// Every placement is a 100-point square on a 400 by 400 page. A redaction of all
// of page 1 covers both of its Logo placements whole and the Photo whole, and
// covers neither on page 2, so each of the three placements gets a copy of its
// own, the Photo's with a soft mask of its own. The centred quarter of page 1,
// [100 100 300 300], covers a different corner of each placement.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedimage2b() ([]byte, error) {
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

	// gray is a 4 by 4 DeviceGray image whose every sample is sample.
	gray := func(sample byte, extra ...any) obj {
		stream := pdf.Stream(bytes.Repeat([]byte{sample}, 16))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", 4)
		stream.Set("Height", 4)
		stream.Set("ColorSpace", n("DeviceGray"))
		stream.Set("BitsPerComponent", 8)
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		return pdf.MakeIndirect(stream)
	}

	logo := gray(0x80)
	photo := gray(0x40, "SMask", gray(0xC0))
	resources := pdf.Indirect(d("XObject", d("Logo", logo, "Photo", photo)))

	place := func(name string, x, y int) string {
		return fmt.Sprintf("q 100 0 0 100 %d %d cm /%s Do Q\n", x, y, name)
	}

	one := place("Logo", 50, 250) + place("Logo", 250, 250) + place("Photo", 150, 50)
	two := place("Logo", 50, 250) + place("Photo", 150, 50)
	for _, body := range []string{one, two} {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 400, 400),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(body))),
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
