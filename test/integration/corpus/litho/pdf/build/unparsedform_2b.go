package build

import (
	"bytes"
	"slices"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("unparsedform_2b", unparsedform2b) }

// unparsedform2b builds unparsedform_2b.pdf: an image a form litho cannot parse also draws,
// at PDF/A-2b.
//
// A redaction blanks an image in place only when every placement of it had the
// same samples covered. It measures the placements by running the page through
// the forms it draws, and a form whose content does not parse used to read as
// painting nothing: an image such a form also names was blanked in place, and a
// viewer that reads the form shows the blank where no region reached. No corpus
// file drew a form litho cannot parse, so no cell reached the measurement.
//
// The image is 5 by 5 DeviceGray, which the RGB output intent covers:
//
// | Form     | Draws                                        | Placement                 |
// | -------- | -------------------------------------------- | ------------------------- |
// | `Card`   | `Logo` (0x80) over its 100-point square      | at (150, 150), 100 points |
// | `Broken` | `Logo` over its square, then a 10 MiB string | at (340, 340), 10 points  |
//
// `Broken`'s content holds a string literal past litho's 10 MiB lexer bound,
// after the image's Do, so litho parses none of it while poppler draws the
// image. Its placement is small enough that the render cell's
// page and ink bars hold without it. The centred quarter of the 400 by 400
// page, [100 100 300 300], covers `Card` whole and misses `Broken`; the band
// through the centre cuts `Card` and misses `Broken`; the whole page covers
// both.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func unparsedform2b() ([]byte, error) {
	pdf := pdfgen.New()
	// A document language: litho's PDF/UA-1 language check reads a stream it
	// cannot parse as one that may show text with no language (ISO 14289-1, 7.2),
	// where veraPDF reads Broken through its string and finds none.
	pdf.Root.Set("Lang", text("en"))

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

	logo := pdf.Stream(bytes.Repeat([]byte{0x80}, 25))
	logo.Set("Type", n("XObject"))
	logo.Set("Subtype", n("Image"))
	logo.Set("Width", 5)
	logo.Set("Height", 5)
	logo.Set("ColorSpace", n("DeviceGray"))
	logo.Set("BitsPerComponent", 8)
	pdf.MakeIndirect(logo)

	draw := []byte("q 100 0 0 100 0 0 cm /Logo Do Q\n")
	// One byte past the lexer's maxStringAccum, 10 << 20.
	oversized := slices.Concat([]byte("("), bytes.Repeat([]byte("A"), (10<<20)+1), []byte(")\n"))

	// form is a form XObject over the 100-point square drawing body.
	form := func(body []byte, compressed bool) *pdfgen.Stream {
		data := body
		if compressed {
			data = zdeflate.Compress(body, 9)
		}
		stream := pdf.Stream(data)
		if compressed {
			stream.Set("Filter", n("FlateDecode"))
		}
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 100, 100))
		stream.Set("Resources", d("XObject", d("Logo", logo)))
		pdf.MakeIndirect(stream)
		return stream
	}

	card := form(draw, false)
	broken := form(slices.Concat(draw, oversized), true)

	body := []byte("q 1 0 0 1 150 150 cm /Card Do Q\nq 0.1 0 0 0.1 340 340 cm /Broken Do Q\n")
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", d("XObject", d("Card", card, "Broken", broken)),
		"Contents", pdf.MakeIndirect(pdf.Stream(body)),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
