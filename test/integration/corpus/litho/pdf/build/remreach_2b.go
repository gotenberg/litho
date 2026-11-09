package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("remreach_2b", remreach2b) }

// remreach2b builds remreach_2b.pdf: constructs compliance judges below a page's own
// resources, where the repairs of their rules did not reach.
//
// Six DeviceN colour spaces over one spot colorant each carry no /Colorants,
// which ISO 19005-2, 6.2.4.4 t1 fails wherever a colour link reaches the space:
//
// | Spot    | Where the DeviceN stands                                     |
// | ------- | ------------------------------------------------------------ |
// | `SpotA` | the `/ColorSpace` resources of a form the page draws         |
// | `SpotB` | the colour space of an image the page draws                  |
// | `SpotC` | the resources of a coloured tiling pattern the page fills    |
// | `SpotD` | the base of an `/Indexed` space the page selects             |
// | `SpotE` | the resources of the Type 3 font whose glyph the page shows  |
// | `SpotF` | the resources of an annotation's normal appearance           |
//
// The pattern's cell also draws an image with /Interpolate true (6.2.8 t3). The
// catalog's /Names /EmbeddedFiles tree names a file specification typed /F, which
// ISO 32000-1, 7.11.4 makes a file specification by position, and no /AF lists
// it (ISO 19005-3, 6.8 t4).
//
// The /Colorants repair walked the page's /ColorSpace entries and shadings, the
// XObject repair the page's /XObject resources and the forms they name, and the
// association repair the file specifications typed /Filespec, so each of these
// kept its finding through remediation. A CMYK OutputIntent covers the DeviceCMYK
// alternates.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func remreach2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccMinimalProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	tint := pdf.Indirect(d(
		"FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(0, 0.5, 1, 0), "N", 1))

	// devicen is a DeviceN over one spot colorant with no /Colorants.
	devicen := func(spot string) *pdfgen.Array {
		return a(n("DeviceN"), a(n(spot)), n("DeviceCMYK"), tint)
	}

	const fill = "/CS cs 1 scn 0 0 5 5 re f"

	resources := func(spot string, extra ...any) *pdfgen.Dict {
		dd := d("ColorSpace", d("CS", devicen(spot)))
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return dd
	}

	form := func(body string, res *pdfgen.Dict) *pdfgen.Stream {
		stream := pdf.Stream([]byte(body))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 20, 20))
		stream.Set("Resources", res)
		pdf.MakeIndirect(stream)
		return stream
	}

	image := func(sample string, colourSpace any, extra ...any) *pdfgen.Stream {
		stream := pdf.Stream([]byte(sample))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Image"))
		stream.Set("Width", 1)
		stream.Set("Height", 1)
		stream.Set("ColorSpace", colourSpace)
		stream.Set("BitsPerComponent", 8)
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	fm := form(fill, resources("SpotA"))
	im := image("\x80", devicen("SpotB"))

	interpolated := image("\x40", n("DeviceGray"), "Interpolate", true)
	cell := pdf.Stream([]byte(fill + " q 5 0 0 5 5 5 cm /Im Do Q"))
	cell.Set("Type", n("Pattern"))
	cell.Set("PatternType", 1)
	cell.Set("PaintType", 1)
	cell.Set("TilingType", 1)
	cell.Set("BBox", a(0, 0, 10, 10))
	cell.Set("XStep", 10)
	cell.Set("YStep", 10)
	cell.Set("Resources", resources("SpotC", "XObject", d("Im", interpolated)))
	pattern := pdf.MakeIndirect(cell)

	glyph := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0 /CS cs 1 scn 0 0 1000 1000 re f")))
	t3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("a", glyph),
		"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("a"))),
		"FirstChar", 97,
		"LastChar", 97,
		"Widths", a(1000),
		"Resources", resources("SpotE"),
	))

	appearance := form(fill, resources("SpotF"))
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(100, 100, 120, 120),
		"F", 4,
		"AP", d("N", appearance),
	))

	content := "/Fm0 Do\n" +
		"q 10 0 0 10 20 20 cm /Im0 Do Q\n" +
		"q /Pattern cs /P0 scn 30 0 50 50 re f Q\n" +
		"q /CSI cs 1 sc 90 0 10 10 re f Q\n" +
		"BT /T3 12 Tf 10 150 Td (a) Tj ET\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(
			"XObject", d("Fm0", fm, "Im0", im),
			"Pattern", d("P0", pattern),
			"Font", d("T3", t3),
			"ColorSpace", d("CSI", a(n("Indexed"), devicen("SpotD"), 1, str("\x00\xff"))),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		"Annots", a(annot),
	))
	pdf.AddPage(page)

	embedded := pdf.Stream([]byte("litho\n"))
	embedded.Set("Type", n("EmbeddedFile"))
	embedded.Set("Subtype", n("/text/plain"))
	spec := pdf.Indirect(d(
		"Type", n("F"),
		"F", text("x.txt"),
		"UF", text("x.txt"),
		"AFRelationship", n("Unspecified"),
		"EF", d("F", pdf.MakeIndirect(embedded)),
	))
	pdf.Root.Set("Names", d("EmbeddedFiles", d("Names", a(text("x.txt"), spec))))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))

	return save(pdf, "1.7")
}
