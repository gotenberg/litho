package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharednested_2b", sharednested2b) }

// sharednested2b builds sharednested_2b.pdf: nested streams two pages share and bind apart.
//
// ISO 19005-2, 6.2.2 requires every content stream to have an explicitly
// associated resource dictionary, and test 2 fails a form XObject or a tiling
// pattern cell that resolves a name only through the page's. When two pages draw
// the same form, each page may bind that name to a different object, so a repair
// that copies one page's binding into the shared form repaints the other page
// with it. litho's remediation left every shared form and pattern alone; no corpus
// file draws one from two pages, so nothing measured the repair.
//
// | Object | Construct                                                    | Pages bind it |
// | ------ | ------------------------------------------------------------ | ------------- |
// | X0     | form XObject, `/Resources << >>`, `/GS0 gs`                  | apart         |
// | X1     | form XObject, `/Resources << >>`, `/GS1 gs`                  | alike         |
// | P0     | tiling pattern, `/Resources << >>`, `/GS0 gs`                | apart         |
//
// Page 1 binds `/GS0` to a graphics state setting a line width of 2, page 2 to
// one setting 4; both bind `/GS1` to the same object. The pages share one
// indirect `/XObject` dictionary, so a repair that repoints X0 there for one page
// repoints it for the other too. The pages define every name, so their own
// content fails nothing, and test 2 counts the three nested streams.
//
// A CMYK OutputIntent covers the initial DeviceGray every stream paints in, and
// the file draws no text, so no colour or font clause fires beside test 2.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharednested2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		copy(header[0:4], iccU32(nil, iccHeaderSize+len(tagTable)))
		copy(header[4:8], tag)
		binary.BigEndian.PutUint32(header[8:12], iccVersion21)
		copy(header[12:16], deviceClass)
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		return append(header, tagTable...)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	thin := pdf.Indirect(d("Type", n("ExtGState"), "LW", 2))
	thick := pdf.Indirect(d("Type", n("ExtGState"), "LW", 4))
	both := pdf.Indirect(d("Type", n("ExtGState"), "LC", 1))

	// form is one form XObject with an empty /Resources.
	form := func(content string) *pdfgen.Stream {
		xo := pdf.Stream([]byte(content))
		xo.Set("Type", n("XObject"))
		xo.Set("Subtype", n("Form"))
		xo.Set("BBox", a(0, 0, 50, 50))
		xo.Set("Resources", d())
		pdf.MakeIndirect(xo)
		return xo
	}

	pattern := pdf.Stream([]byte("/GS0 gs 0 0 5 5 re S\n"))
	pattern.Set("Type", n("Pattern"))
	pattern.Set("PatternType", 1)
	pattern.Set("PaintType", 1)
	pattern.Set("TilingType", 1)
	pattern.Set("BBox", a(0, 0, 10, 10))
	pattern.Set("XStep", 10)
	pattern.Set("YStep", 10)
	pattern.Set("Resources", d())
	pdf.MakeIndirect(pattern)

	xobjects := pdf.Indirect(d(
		"X0", form("/GS0 gs 0 0 50 50 re S\n"),
		"X1", form("/GS1 gs 0 0 50 50 re S\n"),
	))

	content := "q /Pattern cs /P0 scn 0 700 50 50 re f Q\n" +
		"q 1 0 0 1 0 600 cm /X0 Do Q\n" +
		"q 1 0 0 1 60 600 cm /X1 Do Q\n"

	// page is one page drawing every shared stream, binding /GS0 to gs0.
	page := func(gs0 *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d(
				"ExtGState", d("GS0", gs0, "GS1", both),
				"Pattern", d("P0", pattern),
				"XObject", xobjects,
			),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		))
	}

	pdf.AddPage(page(thin))
	pdf.AddPage(page(thick))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
