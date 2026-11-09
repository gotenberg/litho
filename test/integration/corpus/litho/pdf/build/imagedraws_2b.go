package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("imagedraws_2b", imagedraws2b) }

// imagedraws2b builds imagedraws_2b.pdf: images the XObject rules reach only through the
// streams veraPDF parses beside a page's content, and the forms an annotation
// appearance is.
//
// veraPDF judges an image wherever a content stream it parses draws it: an
// annotation's appearance, every glyph procedure of a Type 3 font text is shown
// with, and a form that names the image through the page's resources. Each image
// here carries /Interpolate true, so each reached image fails ISO 19005-2,
// 6.2.8 t3 once:
//
//	IAP  drawn by the normal appearance of a Square annotation.
//	IT3  drawn by the procedure of glyph b of the Type 3 font T3, which the page
//	     shows only glyph a of, in rendering mode 3.
//	IPF  named by the page alone and drawn by the form FmBare, which carries no
//	     /Resources.
//	BI   two inline images with /I true in the page's content, each an image.
//
// The appearance streams are forms, and the form rules judge their dictionaries:
//
//	ApOPI  the appearance of a second annotation, with no /Subtype and an /OPI
//	       key: ISO 19005-2, 6.2.9 t1.
//	ApRef  the appearance of a third annotation, with a /Ref key: ISO 19005-2,
//	       6.2.9 t2.
//
// And the image rules read an image by the role it is reached in:
//
//	ImRef   a drawn image carrying /Ref, which no rule states for an image.
//	Stencil a drawn image mask with /BitsPerComponent 2, which the value rule,
//	        t4, allows for any drawn image.
//	Base    an image whose /Mask, BaseMask, declares /BitsPerComponent 8 and no
//	        /ImageMask: ISO 19005-2, 6.2.8 t5.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func imagedraws2b() ([]byte, error) {
	pdf := pdfgen.New()

	image := func(sample string, extra ...any) pdfgen.Object {
		stream := pdf.Stream([]byte(sample))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", 1)
		stream.Set("Height", 1)
		stream.Set("ColorSpace", n("DeviceGray"))
		stream.Set("BitsPerComponent", 8)
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		return pdf.MakeIndirect(stream)
	}

	form := func(body string, resources *pdfgen.Dict, subtype bool, extra ...any) pdfgen.Object {
		stream := pdf.Stream([]byte(body))
		if subtype {
			stream.Set("Type", n("XObject"))
			stream.Set("Subtype", n("Form"))
		}
		stream.Set("BBox", a(0, 0, 100, 100))
		if resources != nil {
			stream.Set("Resources", resources)
		}
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		return pdf.MakeIndirect(stream)
	}

	annot := func(appearance pdfgen.Object, x int) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(x, 10, x+40, 50),
			"F", 4,
			"AP", d("N", appearance),
		))
	}

	const draw = "q 40 0 0 40 0 0 cm /Im Do Q"

	iAP := image("\x10", "Interpolate", true)
	iT3 := image("\x20", "Interpolate", true)
	iPF := image("\x30", "Interpolate", true)

	// The Type 3 font: glyph a paints a square, glyph b draws IT3. The page shows
	// only a, in rendering mode 3, and veraPDF still parses every procedure.
	glyphA := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0 0 0 1000 1000 re f")))
	glyphB := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0 q 1000 0 0 1000 0 0 cm /Im Do Q")))
	t3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("a", glyphA, "b", glyphB),
		"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("a"), n("b"))),
		"FirstChar", 97,
		"LastChar", 98,
		"Widths", a(1000, 1000),
		"Resources", d("XObject", d("Im", iT3)),
	))

	apImage := form(draw, d("XObject", d("Im", iAP)), true)
	apOPI := form("0 0 10 10 re f", d(), false, "OPI", d())
	apRef := form("0 0 10 10 re f", d(), true, "Ref", d("F", text("other.pdf"), "Page", 0))
	fmBare := form("q 40 0 0 40 0 0 cm /IPF Do Q", nil, true)

	imRef := image("\x40", "Ref", d("F", text("other.pdf"), "Page", 0))
	stencilStream := pdf.Stream([]byte("\x80"))
	stencilStream.Set("Type", n("XObject"))
	stencilStream.Set("Subtype", n("Image"))
	stencilStream.Set("Width", 1)
	stencilStream.Set("Height", 1)
	stencilStream.Set("ImageMask", true)
	stencilStream.Set("BitsPerComponent", 2)
	stencil := pdf.MakeIndirect(stencilStream)
	baseMask := image("\x50")
	base := image("\x60", "Mask", baseMask)

	const inline = "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /CS /G /BPC 8 /I true ID \x70 EI Q"
	content := "q 0.5 g 3 Tr BT /T3 12 Tf 10 700 Td (a) Tj ET Q\n" +
		"q /FmBare Do Q\n" +
		inline + "\n" + inline + "\n" +
		"q 40 0 0 40 100 100 cm /ImRef Do Q\n" +
		"q 0 g 40 0 0 40 200 100 cm /Stencil Do Q\n" +
		"q 40 0 0 40 300 100 cm /Base Do Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("T3", t3),
			"XObject", d("FmBare", fmBare, "IPF", iPF, "ImRef", imRef, "Stencil", stencil, "Base", base),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		"Annots", a(annot(apImage, 10), annot(apOPI, 60), annot(apRef, 110)),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
