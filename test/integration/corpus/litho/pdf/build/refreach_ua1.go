package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("refreach_ua1", refreachUA1) }

// refreachUa1 builds refreach_ua1.pdf: reference XObjects a pattern, a glyph or a page draws.
//
// ISO 14289-1, 7.20 forbids reference XObjects: a form XObject whose dictionary
// carries /Ref (ISO 32000-1, 8.10.4, Table 95). veraPDF 1.30.2 states the test on
// the form object a Do builds, as it states ISO 19005-2, 6.2.9 t2, so it judges a
// form at every Do that draws it, in every content stream it parses: the cell of
// a tiling pattern an scn selects, the glyph of a Type 3 font text is shown with,
// an annotation's appearance. A form a /Resources only lists is never judged. No
// corpus file draws a reference XObject at PDF/UA-1, so this fixture plants one
// path per page, each with a reference form of its own:
//
// | Page | Path                                             | Answers at PDF/UA-1 |
// | ---- | ------------------------------------------------ | ------------------- |
// | 1    | tiling cell, `/R Do`                             | `7.20/1` once       |
// | 2    | Type 3 glyph the page shows, `/R Do`             | `7.20/1` once       |
// | 3    | tiling cell, Type 3 glyph, `/R Do`               | `7.20/1` once       |
// | 4    | the page's content, `/R Do /R Do`                | `7.20/1` twice      |
// | 5    | annotation appearance, `/R Do`                   | `7.20/1` once       |
// | 6    | the page, a cell and a Type 3 font list R, no Do | nothing             |
//
// Page 6 is the must-not-fire control: its page resources, the cell of the
// pattern it selects, and the Type 3 font it shows text in each list a reference
// form that nothing draws, and the glyph the page shows draws none.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func refreachUA1() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		iccHeaderSize = 128
		iccVersion21  = 0x02100000
	)

	// iccProfile is a structurally minimal ICC profile, as patternglyph_2b.go
	// builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize, iccHeaderSize+len(tagTable))
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // 132.
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

	// form is a form XObject; ref makes it a reference XObject.
	form := func(content []byte, resources *pdfgen.Dict, ref bool) obj {
		xobj := pdf.Stream(content)
		xobj.Set("Type", n("XObject"))
		xobj.Set("Subtype", n("Form"))
		xobj.Set("BBox", a(0, 0, 20, 20))
		if resources != nil {
			xobj.Set("Resources", resources)
		}
		if ref {
			xobj.Set("Ref", d("F", text("target.pdf"), "Page", 0))
		}
		return pdf.MakeIndirect(xobj)
	}

	// reference is a reference XObject whose own content paints a square.
	reference := func() obj {
		return form([]byte("0 g 0 0 10 10 re f\n"), d(), true)
	}

	// tiling is a coloured tiling pattern carrying its own /Resources.
	tiling := func(content []byte, resources *pdfgen.Dict) obj {
		pat := pdf.Stream(content)
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", resources)
		return pdf.MakeIndirect(pat)
	}

	// type3 is a one-glyph Type 3 font; the glyph sets its colour (d0).
	type3 := func(glyph []byte, resources *pdfgen.Dict) *pdfgen.Dict {
		proc := pdf.MakeIndirect(pdf.Stream(append([]byte("1000 0 d0\n"), glyph...)))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("A", proc),
			"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"))),
			"FirstChar", 65,
			"LastChar", 65,
			"Widths", a(1000),
			"Resources", resources,
		))
	}

	show := []byte("q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n")
	paint := []byte("q /Pattern cs /P1 scn 0 0 100 100 re f Q\n")
	draw := []byte("/R Do\n")

	addPage := func(content []byte, resources *pdfgen.Dict, annots *pdfgen.Array) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream(content)),
		)
		if annots != nil {
			page.Set("Annots", annots)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	addPage(paint, d("Pattern", d("P1", tiling(draw, d("XObject", d("R", reference()))))), nil)

	addPage(show, d("Font", d("T3", type3(draw, d("XObject", d("R", reference()))))), nil)

	glyph3 := type3(draw, d("XObject", d("R", reference())))
	addPage(paint, d("Pattern", d("P1", tiling(show, d("Font", d("T3", glyph3))))), nil)

	addPage([]byte("/R Do /R Do\n"), d("XObject", d("R", reference())), nil)

	appearance := form(draw, d("XObject", d("R", reference())), false)
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(0, 0, 20, 20),
		"F", 4,
		"Contents", text("A square"),
		"AP", d("N", appearance),
	))
	addPage([]byte("0 g 0 0 10 10 re f\n"), d(), a(annot))

	listed := reference()
	cell6 := tiling([]byte("0 g 0 0 10 10 re f\n"), d("XObject", d("R", listed)))
	glyph6 := type3([]byte("0 0 1000 1000 re f\n"), d("XObject", d("R", listed)))
	addPage(
		append(append([]byte{}, paint...), show...),
		d("XObject", d("R", listed), "Pattern", d("P1", cell6), "Font", d("T3", glyph6)),
		nil,
	)

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
