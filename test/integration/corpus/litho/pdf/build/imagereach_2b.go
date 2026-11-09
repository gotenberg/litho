package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("imagereach_2b", imagereach2b) }

// imagereach2b builds imagereach_2b.pdf: images the XObject rules reach only through a Do.
//
// veraPDF judges an image where a Do operator draws it, once for the whole
// document, and a page content stream another page shares once, through the
// resources of the first page that draws it. Every image in this file breaks two
// image rules, an /Intent outside the four ISO 32000-1 names (ISO 19005-2, 6.2.6)
// and an /Interpolate of true (6.2.8 t3):
//
//	I1  listed by the resources of the form FmList the first page draws, whose
//	    content draws nothing. A form's resource entry is not a draw.
//	I2  listed by the resources of the form FmNest that FmList's sibling FmOuter
//	    draws, and drawn by neither.
//	I3  drawn by the form FmShared that the second page names /FmList, through
//	    the content stream it shares with the first page, which draws /FmList
//	    as the first page names it.
//
// Neither rule fails for I1 or I2, where judging the resource entries fails
// both. I3 is drawn, since the second page's /FmList resolves in the second
// page's resources (ISO 32000-1, 7.8.3): litho fails both rules once, and
// veraPDF, which reads the shared stream through the first page alone, fails
// neither.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func imagereach2b() ([]byte, error) {
	pdf := pdfgen.New()

	image := func(sample string) *pdfgen.Stream {
		stream := pdf.Stream([]byte(sample))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", 1)
		stream.Set("Height", 1)
		stream.Set("ColorSpace", n("DeviceGray"))
		stream.Set("BitsPerComponent", 8)
		stream.Set("Intent", n("/Bogus"))
		stream.Set("Interpolate", true)
		pdf.MakeIndirect(stream)
		return stream
	}

	form := func(body string, xobjects *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 100, 100))
		stream.Set("Resources", d("XObject", xobjects))
		pdf.MakeIndirect(stream)
		return stream
	}

	i1, i2, i3 := image("\x80"), image("\x40"), image("\xc0")
	fmList := form("0 0 10 10 re f", d("I1", i1))
	fmNest := form("0 0 10 10 re f", d("I2", i2))
	fmOuter := form("/FmNest Do", d("FmNest", fmNest))
	fmShared := form("q 10 0 0 10 0 0 cm /I3 Do Q", d("I3", i3))

	content := pdf.MakeIndirect(pdf.Stream([]byte("q /FmList Do Q q /FmOuter Do Q\n")))

	page := func(xobjects *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("XObject", xobjects),
			"Contents", content,
		))
	}

	pdf.AddPage(page(d("FmList", fmList, "FmOuter", fmOuter)))
	pdf.AddPage(page(d("FmList", fmShared, "FmOuter", fmOuter)))

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
