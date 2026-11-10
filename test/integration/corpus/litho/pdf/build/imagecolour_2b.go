package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("imagecolour_2b", imagecolour2b) }

// imagecolour2b builds imagecolour_2b.pdf: images whose colour no other corpus file states.
//
// No corpus file holds an image over the colorant None, nor a JPXDecode image
// whose data names CIELab, ROMM-RGB or e-sRGB. The litho corpus runs the Writer
// mutations, so the redaction of page 1 decodes every image here.
//
// | Image      | Colour                                             | Reading            |
// | ---------- | -------------------------------------------------- | ------------------ |
// | `SepNone`  | `[/Separation /None /DeviceGray f]`                | paints nothing     |
// | `DevNone`  | `[/DeviceN [/None /None] /DeviceGray f2]`          | paints nothing     |
// | `JLabDef`  | JPX, enumerated CIELab, no EP fields               | T.42 default Lab   |
// | `JLabEP`   | JPX, enumerated CIELab, EP fields, illuminant D50  | stated Lab         |
// | `JROMM`    | JPX, enumerated ROMM-RGB                           | ISO 22028-2        |
// | `JESRGB`   | JPX, enumerated e-sRGB                             | DeviceRGB fallback |
//
// ISO 32000-1, 8.6.6.4 says the colorant None never produces visible output,
// and 8.6.6.5 says the same of a DeviceN space naming only None. The JPX images
// carry no /ColorSpace, so ISO 32000-1, 7.4.9 takes their colour from the data,
// and ISO 19005-2, 6.2.8.3 test 4 forbids only CIEJab among the enumerated
// spaces.
//
// The document has no /Metadata and no output intent, so the metadata and
// colour clauses answer from elsewhere in the specification.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func imagecolour2b() ([]byte, error) {
	pdf := pdfgen.New()

	// ISO/IEC 15444-2, Table M.25.
	const (
		enumCIELab = 14
		enumESRGB  = 20
		enumROMM   = 21
	)
	// ISO/IEC 15444-2, M.11.7.4.1: IL of CIE illuminant D50.
	const ilD50 = 0x00443530

	// labColr is an enumerated CIELab colour specification, with its EP fields
	// if any.
	labColr := func(ep ...int) []byte {
		payload := jp2U32([]byte{1, 0, 1}, enumCIELab)
		for _, v := range ep {
			payload = jp2U32(payload, v)
		}
		return jp2Box("colr", payload)
	}

	// tinted is an 8x1 image in a Separation or DeviceN space.
	tinted := func(samples []byte, colorSpace *pdfgen.Array) *pdfgen.Stream {
		stream := pdf.Stream(samples)
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", 8)
		stream.Set("Height", 1)
		stream.Set("ColorSpace", colorSpace)
		stream.Set("BitsPerComponent", 8)
		pdf.MakeIndirect(stream)
		return stream
	}

	// jpx is one JPXDecode image XObject wrapping a JP2 container, no
	// /ColorSpace.
	jpx := func(data []byte) *pdfgen.Stream {
		stream := pdf.Stream(data)
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", jp2Size)
		stream.Set("Height", jp2Size)
		stream.Set("Filter", n("JPXDecode"))
		pdf.MakeIndirect(stream)
		return stream
	}

	// Tint 0 paints paper white and full tint black, as a real ink would.
	tint := pdf.Indirect(d("FunctionType", 2, "Domain", a(0, 1), "Range", a(0, 1),
		"C0", a(1), "C1", a(0), "N", 1))
	tint2 := pdf.Stream([]byte{255, 128, 128, 0})
	tint2.Set("FunctionType", 0)
	tint2.Set("Domain", a(0, 1, 0, 1))
	tint2.Set("Range", a(0, 1))
	tint2.Set("Size", a(2, 2))
	tint2.Set("BitsPerSample", 8)
	pdf.MakeIndirect(tint2)

	var ramp []byte
	for v := 0; v < 256; v += 32 {
		ramp = append(ramp, byte(v))
	}
	pairs := make([]byte, 0, 2*len(ramp))
	for _, v := range ramp {
		pairs = append(pairs, v, 255-v)
	}
	type namedImage struct {
		name string
		ref  *pdfgen.Stream
	}
	images := []namedImage{
		{"SepNone", tinted(ramp, a(n("Separation"), n("/None"), n("DeviceGray"), tint))},
		{"DevNone", tinted(pairs,
			a(n("DeviceN"), a(n("/None"), n("/None")), n("DeviceGray"), tint2))},
		{"JLabDef", jpx(jp2Container(jp2Three, 3, [][]byte{labColr()}, nil))},
		{"JLabEP", jpx(jp2Container(jp2Three, 3, [][]byte{labColr(100, 0, 255, 128, 255, 128, ilD50)}, nil))},
		{"JROMM", jpx(jp2Container(jp2Three, 3, [][]byte{jp2Colr(1, 1, enumROMM)}, nil))},
		{"JESRGB", jpx(jp2Container(jp2Three, 3, [][]byte{jp2Colr(1, 1, enumESRGB)}, nil))},
	}

	xobjects := d()
	lines := make([]string, 0, len(images))
	y := 700
	for _, im := range images {
		xobjects.Set(im.name, im.ref)
		lines = append(lines, fmt.Sprintf("q 80 0 0 40 50 %d cm /%s Do Q", y, im.name))
		y -= 60
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("XObject", xobjects),
		"Contents", pdf.Stream([]byte(strings.Join(lines, "\n")+"\n"))))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
