package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("stdremap_ua2", stdremapUa2) }

// stdremapUa2 builds stdremap_ua2.pdf: a standard structure type the root /RoleMap
// remaps to a private one, on an element declaring the PDF 1.7 namespace.
//
// The structure tree root's legacy /RoleMap governs a type in no namespace, or in
// the PDF 1.7 namespace when that namespace declares no /RoleMapNS (ISO 32000-2,
// 14.8.6). Here it sends `Sect` to `Book`, which no namespace defines and which
// nothing maps further, under a root `Document` in the PDF 2.0 namespace:
//
//   - a `Sect` declaring the PDF 1.7 namespace resolves through the map to
//     `Book`, so the standard type it names is remapped to a non-standard one:
//     ISO 14289-2, 8.2.4 test 4;
//   - a `Sect` declaring the PDF 2.0 namespace resolves through that namespace
//     alone, so the map does not reach it and it stays the standard `Sect`.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func stdremapUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const pdf17NS = "http://iso.org/pdf/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoStdRemap"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoStdRemap"),
			"Flags", 32,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)),
	))

	marked := func(tag string, mcid, y int, s string) string {
		return fmt.Sprintf("/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n", tag, mcid, y, s)
	}

	content := marked("H1", 0, 740, "AABB") +
		marked("P", 1, 700, "ABAB") +
		marked("P", 2, 660, "BABA")
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)
	page = pdf.Page(0)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	ns17 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf17NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot),
	)

	elem := func(s string, parent, ns *pdfgen.Dict, k any) *pdfgen.Dict {
		return pdf.Indirect(
			d("Type", n("StructElem"), "S", n("/"+s), "P", parent, "NS", ns, "K", k),
		)
	}

	title := elem("H1", document, ns20, 0)
	title.Set("Pg", page)
	sectPDF17 := elem("Sect", document, ns17, a())
	sectPDF20 := elem("Sect", document, ns20, a())
	paraPDF17 := elem("P", sectPDF17, ns20, 1)
	paraPDF17.Set("Pg", page)
	paraPDF20 := elem("P", sectPDF20, ns20, 2)
	paraPDF20.Set("Pg", page)
	sectPDF17.Set("K", a(paraPDF17))
	sectPDF20.Set("K", a(paraPDF20))
	document.Set("K", a(title, sectPDF17, sectPDF20))

	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20, ns17))
	structRoot.Set("RoleMap", d("Sect", n("/Book")))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(title, paraPDF17, paraPDF20))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
