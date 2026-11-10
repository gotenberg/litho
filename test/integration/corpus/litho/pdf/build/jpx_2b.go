package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("jpx_2b", jpx2b) }

// jpx2b builds jpx_2b.pdf: the five tests ISO 19005-2, 6.2.8.3 states over JPEG 2000.
//
// A coverage scan showed the whole 6.2.8.3 cluster unreached, twenty-five cells
// across the five profiles that state it. Nothing blocks it but authorship: every
// test reads a box of the JP2 container, not the entropy-coded data, so a fixture
// plants each defect by naming a value.
//
// | Image      | Container                            | Answers          |
// | ---------- | ------------------------------------ | ---------------- |
// | `ImOK`     | 3 channels, one sRGB `colr`          | nothing          |
// | `ImChan`   | 2 channels                           | `6.2.8.3/1`      |
// | `ImSpecs`  | two `colr` boxes, both APPROX 1      | `6.2.8.3/2`      |
// | `ImMeth`   | one `colr` box, METH 4               | `6.2.8.3/3`      |
// | `ImJab`    | one `colr` box, enumerated CIEJab    | `6.2.8.3/4`      |
// | `ImDepth`  | a `bpcc` box declaring 40 bits       | `6.2.8.3/5`      |
// | `ImWaived` | METH 4, and a `/ColorSpace` entry    | nothing          |
//
// The five defects are independent, which is the masking probe: a checker that
// reports the first offending image and stops reports one test where five are
// due. All seven images are painted.
//
// `ImSpecs` carries two specifications that both set APPROX to 1. Test 2 reads
// "there shall be exactly one colour space specification that has the value 0x01
// in the APPROX field", so two of them fail it, while test 3 still finds an
// effective METH of 1 and stays silent. Two specifications with no APPROX 1 at
// all would answer both tests at once and the split would be invisible. That was
// measured, on both engines, before the fixture settled on this shape.
//
// `ImWaived` is the exemption planted as a control. ISO 19005-2, 6.2.8.3 hands
// the colour tests to the image dictionary's `/ColorSpace` when it has one, so an
// image whose `colr` box states a forbidden METH conforms as long as the PDF
// names the colour space itself. A check reading the container unconditionally
// fires on it.
//
// `ImOK` conforms on all five tests, so a verdict attached to the wrong image
// answers on it too.
//
// The document has no `/Metadata` and no output intent, so the metadata and
// colour clauses answer from elsewhere in the specification.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func jpx2b() ([]byte, error) {
	pdf := pdfgen.New()

	// ISO 19005-2, 6.2.8.3 test 5 permits 1 to 38 bits per component.
	const forbiddenDepth = 40
	const forbiddenMeth = 4

	images := []struct {
		name       string
		data       []byte
		colorSpace bool
	}{
		{"ImOK", jp2Container(jp2Three, 3, [][]byte{jp2Colr(1, 1, jp2EnumSRGB)}, nil), false},
		{"ImChan", jp2Container(jp2Two, 2, [][]byte{jp2Colr(1, 1, jp2EnumSRGB)}, nil), false},
		{"ImSpecs", jp2Container(jp2Three, 3,
			[][]byte{jp2Colr(1, 1, jp2EnumSRGB), jp2Colr(1, 1, jp2EnumSRGB)}, nil), false},
		{"ImMeth", jp2Container(jp2Three, 3, [][]byte{jp2Colr(forbiddenMeth, 1, 0)}, nil), false},
		{"ImJab", jp2Container(jp2Three, 3, [][]byte{jp2Colr(1, 1, jp2EnumCIEJab)}, nil), false},
		{"ImDepth", jp2Container(jp2Three, 3, [][]byte{jp2Colr(1, 1, jp2EnumSRGB)},
			[]int{forbiddenDepth, forbiddenDepth, forbiddenDepth}), false},
		{"ImWaived", jp2Container(jp2Three, 3, [][]byte{jp2Colr(forbiddenMeth, 1, 0)}, nil), true},
	}

	// image is one JPXDecode image XObject wrapping a JP2 container.
	image := func(data []byte, colorSpace bool) obj {
		stream := pdf.Stream(data,
			"Type", n("XObject"), "Subtype", n("Image"),
			"Width", jp2Size, "Height", jp2Size, "Filter", n("JPXDecode"))
		if colorSpace {
			stream.Set("ColorSpace", n("DeviceRGB"))
		}
		return pdf.MakeIndirect(stream)
	}

	xobjects := d()
	var lines []string
	y := 700
	for _, im := range images {
		xobjects.Set(im.name, image(im.data, im.colorSpace))
		lines = append(lines, fmt.Sprintf("q 40 0 0 40 50 %d cm /%s Do Q", y, im.name))
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
