package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("rolecycle_ua1", rolecycleUa1) }

// rolecycleUa1 builds rolecycle_ua1.pdf: standard structure types inside a /RoleMap cycle.
//
// veraPDF 1.30.2 judges a circular mapping (ISO 14289-1, 7.1 test 6; ISO 19005-1,
// 6.8.3.4 test 2; ISO 19005-2 and -3, 6.7.3.4 test 2) per structure element: it
// follows the root /RoleMap from the element's own type, by name, without
// stopping on a standard type, and reports the element when the chain revisits a
// type. Under a root `Document`, the /RoleMap sends `Sect` to `Book` and `Book`
// back to `Sect`, and:
//
//   - a `Sect` sits in the cycle: a standard type whose mapping returns to it;
//   - an `Item` maps to `Sect`, so its chain enters the cycle without being in
//     it, and stops on the standard `Sect` for every reading that stops there;
//   - a `Div` nothing maps, the control.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func rolecycleUa1() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoRoleCycle"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRoleCycle"),
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
		marked("P", 2, 660, "BABA") +
		marked("P", 3, 620, "BBAA")
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

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	elem := func(s string, parent *pdfgen.Dict, k any) *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("StructElem"), "S", n(s), "P", parent, "K", k))
	}

	para := func(parent *pdfgen.Dict, mcid int) *pdfgen.Dict {
		p := elem("P", parent, mcid)
		p.Set("Pg", page)
		return p
	}

	title := elem("H1", document, 0)
	title.Set("Pg", page)
	sect := elem("Sect", document, a())
	item := elem("Item", document, a())
	div := elem("Div", document, a())
	paras := []*pdfgen.Dict{para(sect, 1), para(item, 2), para(div, 3)}
	sect.Set("K", a(paras[0]))
	item.Set("K", a(paras[1]))
	div.Set("K", a(paras[2]))
	document.Set("K", a(title, sect, item, div))

	structRoot.Set("K", a(document))
	structRoot.Set("RoleMap", d("Sect", n("/Book"), "Book", n("Sect"), "Item", n("Sect")))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(title, paras[0], paras[1], paras[2])))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
