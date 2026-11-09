package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("realglyphs_ua1", realglyphsUa1) }

// realglyphsUa1 builds realglyphs_ua1.pdf: one Type 3 glyph shown in real content and beside it.
//
// veraPDF 1.30.2 keys a Glyph by the stream it is shown in being real content.
// A PDF/UA or WTPDF validation parses a page's content, and the forms it draws,
// as semantic content (real), and a tiling cell, a Type 3 glyph procedure, an
// annotation appearance and a form one of those draws as plain content (not
// real). A glyph with no Unicode value shown in both is two glyphs there, and
// one at PDF/A, which parses nothing as semantic. A cell starts in the text
// rendering mode the stream selecting its pattern starts in, so a Type 3 font
// can show its own glyphs again in a cell one of its procedures selects. No
// corpus file the gate validates reaches these shapes, so this fixture plants
// one per page, each through Type 3 fonts of its own whose glyph names carry no
// Unicode value:
//
// | Page | The page shows the glyph, and so does                                 | ISO 14289-1, 7.21.7 t1 | ISO 19005-2, 6.2.11.7.2 t1 |
// | ---- | ---------------------------------------------------------------------- | ---------------------- | -------------------------- |
// | 1    | a cell the page selects                                                | 2                      | 1                          |
// | 2    | an annotation appearance                                               | 2                      | 1                          |
// | 3    | a procedure of a second Type 3 font the page shows                     | 3                      | 2                          |
// | 4    | a form a cell draws                                                    | 2                      | 1                          |
// | 5    | a form the page draws (control)                                        | 1                      | 1                          |
// | 6    | nothing; a cell the glyph's own procedure selects shows the other glyph | 2                      | 2                          |
// | 7    | a cell the glyph's own procedure selects, the page showing it in mode 2 | 2                      | 1                          |
//
// Page 3 counts the second font's glyph too. Page 5 is the control: a form the
// page draws is real content, so its glyph is the page's. Pages 6 and 7 hold the
// same cycle, a procedure selecting a cell that shows its own font: the cell's
// other glyph is judged as any other (page 6), and the cell starting in the
// procedure's rendering mode 2 makes the page's glyph the cell's (page 7).
//
// A CMYK OutputIntent covers the DeviceGray every glyph paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func realglyphsUa1() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as
	// nestedpattern_2b.go builds it.
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

	const plain = "0 0 1000 1000 re f\n"

	type glyph struct{ name, body string }

	// type3 is a Type 3 font over codes A, B, ... whose glyphs (name,
	// procedure body) set their colour (d0). The font's /Resources start
	// empty.
	type3 := func(glyphs []glyph) *pdfgen.Dict {
		procs := d()
		names := a(65)
		widths := a()
		for _, g := range glyphs {
			procs.Set(g.name, pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n"+g.body))))
			names.Append(n(g.name))
			widths.Append(1000)
		}
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", procs,
			"Encoding", d("Type", n("Encoding"), "Differences", names),
			"FirstChar", 65,
			"LastChar", 64+len(glyphs),
			"Widths", widths,
			"Resources", d(),
		))
	}

	// tiling is a coloured tiling pattern carrying its own /Resources.
	tiling := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", resources)
		pdf.MakeIndirect(pat)
		return pat
	}

	// form is a form XObject carrying its own /Resources.
	form := func(content string, resources *pdfgen.Dict) *pdfgen.Stream {
		fm := pdf.Stream([]byte(content))
		fm.Set("Type", n("XObject"))
		fm.Set("Subtype", n("Form"))
		fm.Set("BBox", a(0, 0, 20, 20))
		fm.Set("Resources", resources)
		pdf.MakeIndirect(fm)
		return fm
	}

	addPage := func(content string, resources *pdfgen.Dict, annots *pdfgen.Array) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		)
		if annots != nil {
			page.Set("Annots", annots)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	const show = "BT /T 10 Tf 10 10 Td (A) Tj ET\n"
	const fill = "q /Pattern cs /P scn 0 0 100 100 re f Q\n"
	const cell = "BT /T 4 Tf (A) Tj ET\n"

	// Page 1: the page and a cell it selects.
	t1 := type3([]glyph{{"q1", plain}})
	addPage(show+fill, d("Font", d("T", t1), "Pattern", d("P", tiling(cell, d("Font", d("T", t1))))), nil)

	// Page 2: the page and an annotation appearance.
	t2 := type3([]glyph{{"q2", plain}})
	appearance := form(cell, d("Font", d("T", t2)))
	annot := pdf.Indirect(
		d("Type", n("Annot"), "Subtype", n("Square"), "Rect", a(10, 10, 30, 30), "F", 4, "AP", d("N", appearance)),
	)
	addPage(show, d("Font", d("T", t2)), a(annot))

	// Page 3: the page and a procedure of a second Type 3 font the page shows.
	t3 := type3([]glyph{{"q3", plain}})
	t3b := type3([]glyph{{"h3", cell}})
	t3b.Set("Resources", d("Font", d("T", t3)))
	addPage(show+"BT /U 10 Tf 50 50 Td (A) Tj ET\n", d("Font", d("T", t3, "U", t3b)), nil)

	// Page 4: the page and a form a cell draws.
	t4 := type3([]glyph{{"q4", plain}})
	cell4 := tiling("/Fm Do\n", d("XObject", d("Fm", form(cell, d("Font", d("T", t4))))))
	addPage(show+fill, d("Font", d("T", t4), "Pattern", d("P", cell4)), nil)

	// Page 5 (control): the page and a form the page draws.
	t5 := type3([]glyph{{"q5", plain}})
	addPage(show+"/Fm Do\n", d("Font", d("T", t5), "XObject", d("Fm", form(cell, d("Font", d("T", t5))))), nil)

	// Page 6: the glyph's own procedure selects a cell showing the font's other glyph.
	t6 := type3([]glyph{{"q6", "/Pattern cs /P scn " + plain}, {"r6", plain}})
	t6.Set("Resources", d("Pattern", d("P", tiling("BT /T 4 Tf (B) Tj ET\n", d("Font", d("T", t6))))))
	addPage(show, d("Font", d("T", t6)), nil)

	// Page 7: the same cycle, the page showing the glyph in rendering mode 2.
	t7 := type3([]glyph{{"q7", "/Pattern cs /P scn " + plain}})
	t7.Set("Resources", d("Pattern", d("P", tiling(cell, d("Font", d("T", t7))))))
	addPage("BT 2 Tr /T 10 Tf 10 10 Td (A) Tj ET\n", d("Font", d("T", t7)), nil)

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
