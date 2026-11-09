package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("remapchain_ua2", remapchainUa2) }

// remapchainUa2 builds remapchain_ua2.pdf: standard structure types role-mapped through a
// private type that is itself mapped on to a standard type.
//
// ISO 14289-2, 8.2.4 test 4 reports a standard structure type whose role mapping
// leads to a non-standard type. A mapping is followed to its end (ISO 32000-2,
// 14.8.6), so a chain that passes through a private type and ends on a standard
// one leaves the element a standard type. Under a root `Document` in the PDF 2.0
// namespace:
//
//   - a `Div` in no namespace, which the root /RoleMap sends to `Section` and
//     `Section` on to `Sect`: the legacy chain ends on the standard `Sect`;
//   - a `Sect` declaring the PDF 1.7 namespace, whose /RoleMapNS sends it to
//     `Section` in a custom namespace, whose own /RoleMapNS sends `Section` to
//     the PDF 2.0 `Sect`: the namespace chain ends on a standard type;
//   - a `Callout` in the custom namespace, which its /RoleMapNS maps straight to
//     the PDF 2.0 `Aside`: a private type correctly mapped, the control.
//
// None of the three fires 8.2.4.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func remapchainUa2() ([]byte, error) {
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
		"BaseFont", n("/LithoRemapChain"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRemapChain"),
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

	const customNS = "http://litho.example/ns/remapchain"

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	custom := pdf.Indirect(d("Type", n("Namespace"), "NS", text(customNS)))
	custom.Set("RoleMapNS", d(
		"Section", a(n("Sect"), ns20),
		"Callout", a(n("Aside"), ns20),
	))
	ns17 := pdf.Indirect(d(
		"Type", n("Namespace"),
		"NS", text(pdf17NS),
		"RoleMapNS", d("Sect", a(n("/Section"), custom)),
	))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot),
	)

	// elem: ns nil leaves /NS out.
	elem := func(s string, parent *pdfgen.Dict, ns *pdfgen.Dict, k any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("/"+s), "P", parent, "K", k)
		if ns != nil {
			dd.Set("NS", ns)
		}
		return pdf.Indirect(dd)
	}

	para := func(parent *pdfgen.Dict, mcid int) *pdfgen.Dict {
		p := elem("P", parent, ns20, mcid)
		p.Set("Pg", page)
		return p
	}

	title := elem("H1", document, ns20, 0)
	title.Set("Pg", page)
	div := elem("Div", document, nil, a())
	sectPDF17 := elem("Sect", document, ns17, a())
	callout := elem("Callout", document, custom, a())
	paras := []*pdfgen.Dict{para(div, 1), para(sectPDF17, 2), para(callout, 3)}
	div.Set("K", a(paras[0]))
	sectPDF17.Set("K", a(paras[1]))
	callout.Set("K", a(paras[2]))
	document.Set("K", a(title, div, sectPDF17, callout))

	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20, ns17, custom))
	structRoot.Set("RoleMap", d("Div", n("/Section"), "Section", n("Sect")))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(title, paras[0], paras[1], paras[2]))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
