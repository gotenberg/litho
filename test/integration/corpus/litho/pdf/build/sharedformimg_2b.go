package build

import (
	"bytes"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharedformimg_2b", sharedformimg2b) }

// sharedformimg2b builds sharedformimg_2b.pdf: an image forms draw on both pages, at PDF/A-2b.
//
// A form's content is in its own stream, drawn by every Do naming the form, so
// an image a form paints cannot be blanked for one placement by repointing a Do
// on the page. Where the regions do not cover the same samples at every
// placement, litho copies the form, and each form between the page and the
// image, for the covered placement: the copy names a blanked copy of the image,
// and the page draws the copy. No file in the litho corpus drew one image
// through a form in two places, so no cell reached the form copy.
//
// The image is 4 by 4 DeviceGray, which the RGB output intent covers. Both pages
// name the forms through the one /Resources dictionary they share:
//
// | Form    | Draws                         | Page 1                     | Page 2        |
// | ------- | ----------------------------- | -------------------------- | ------------- |
// | `Card`  | `Logo` (0x80) over its square | at (50, 250) and (150, 50) | at (50, 250)  |
// | `Frame` | `Card` at its origin          | at (250, 250)              | at (250, 250) |
//
// Every placement paints a 100-point square on a 400 by 400 page. A redaction of
// all of page 1 covers its three placements whole and none on page 2, so each of
// the three gets a copy of the forms above it: one Card copy for each direct
// placement, and a Frame copy drawing a Card copy for the nested one. The
// centred quarter of page 1, [100 100 300 300], covers a different corner of
// each placement.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedformimg2b() ([]byte, error) {
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

	logoStream := pdf.Stream(bytes.Repeat([]byte{0x80}, 16))
	logoStream.Set("Type", n("XObject"))
	logoStream.Set("Subtype", n("Image"))
	logoStream.Set("Width", 4)
	logoStream.Set("Height", 4)
	logoStream.Set("ColorSpace", n("DeviceGray"))
	logoStream.Set("BitsPerComponent", 8)
	logo := pdf.MakeIndirect(logoStream)

	// form is a form XObject over the 100-point square drawing body.
	form := func(body string, xobjects ...any) obj {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 100, 100))
		stream.Set("Resources", d("XObject", d(xobjects...)))
		return pdf.MakeIndirect(stream)
	}

	card := form("q 100 0 0 100 0 0 cm /Logo Do Q\n", "Logo", logo)
	frame := form("/Card Do\n", "Card", card)
	resources := pdf.Indirect(d("XObject", d("Card", card, "Frame", frame)))

	place := func(name string, x, y int) string {
		return fmt.Sprintf("q 1 0 0 1 %d %d cm /%s Do Q\n", x, y, name)
	}

	one := place("Card", 50, 250) + place("Frame", 250, 250) + place("Card", 150, 50)
	two := place("Card", 50, 250) + place("Frame", 250, 250)
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
