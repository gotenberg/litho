package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formimgname_2b", formimgname2b) }

// formimgname2b builds formimgname_2b.pdf: a page image and a form XObject's image under one
// resource name, at PDF/A-2b.
//
// A form XObject resolves the names its content uses in its own /Resources (ISO
// 32000-1, 8.10.1, 7.8.3), so the form's Im0 is another image than the page's
// Im0. Redaction looked every image name up in the page's resources: a
// redaction covering the form's picture blanked the page's Im0 instead, left
// the form's picture whole, and reported success. govdocs thread_000_000582 and
// thread_400_400358 have this shape, outside the render sample the redact-render
// cell draws from.
//
// | Name             | Image                     | Drawn at (page space)           |
// | ---------------- | ------------------------- | ------------------------------- |
// | page `/Im0`      | 8 x 8 grey ramp           | `[50 600 250 750]`, the page    |
// | form `/Fm0 /Im0` | 8 x 8 yellow-cyan checker | `[206 296 406 496]`, by `/Fm0`  |
//
// The form's picture sits at the centre of the page, inside the centred quarter
// and the centre band the redact-render cell redacts, and its light colours
// show above the cell's dark level once drawn over black.
//
// The file declares no OutputIntent, so DeviceRGB and DeviceGray fail.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formimgname2b() ([]byte, error) {
	pdf := pdfgen.New()

	image := func(samples []byte, colourSpace pdfgen.Name) pdfgen.Object {
		stream := pdf.Stream(samples)
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", 8)
		stream.Set("Height", 8)
		stream.Set("ColorSpace", colourSpace)
		stream.Set("BitsPerComponent", 8)
		return pdf.MakeIndirect(stream)
	}

	var ramp, checker []byte
	for range 8 {
		for x := range 8 {
			ramp = append(ramp, byte(64+16*x))
		}
	}
	yellow, cyan := []byte("\xff\xff\x00"), []byte("\x00\xff\xff")
	for y := range 8 {
		for x := range 8 {
			if (x+y)%2 == 0 {
				checker = append(checker, yellow...)
			} else {
				checker = append(checker, cyan...)
			}
		}
	}

	pageImage := image(ramp, n("DeviceGray"))
	formImage := image(checker, n("DeviceRGB"))

	formStream := pdf.Stream([]byte("q 200 0 0 200 206 296 cm /Im0 Do Q\n"))
	formStream.Set("Type", n("XObject"))
	formStream.Set("Subtype", n("Form"))
	formStream.Set("BBox", a(0, 0, 612, 792))
	formStream.Set("Resources", d("XObject", d("Im0", formImage)))
	form := pdf.MakeIndirect(formStream)

	content := pdf.MakeIndirect(pdf.Stream([]byte("q 200 0 0 150 50 600 cm /Im0 Do Q\nq /Fm0 Do Q\n")))
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("XObject", d("Im0", pageImage, "Fm0", form)),
		"Contents", content,
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
