package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("directfig_ua2", directfigUa2) }

// directfigUa2 builds directfig_ua2.pdf: Figures written directly in a structure /K.
//
// ISO 32000-1, 14.7.2, Table 323 lets an element's /K hold a structure element
// directly, with no object number of its own. ISO 14289-2, 8.2.5.28.2 and ISO
// 14289-1, 7.3 ask such a Figure for alternate text like any other, so
// remediation writes it into the object holding the Figure, and aggressive
// remediation takes out one that encloses nothing. No corpus file writes a
// Figure directly, so this file packs the places one can sit into one page:
//
//   - D1, directly in the Document's /K array, holds MCID 1, a drawing.
//   - D2, directly in the /K array of a Div itself written inline in the
//     Document's /K array, holds MCID 2, a drawing.
//   - D3, directly in a /K array written as an object of its own that a Sect
//     names, holds MCID 3, a drawing.
//   - D4, directly in the Document's /K array, holds MCID 9, which the page
//     does not mark: it encloses nothing.
//   - D5, in the Sect's array beside D3, holds D6 alone, and D6 the null object:
//     both enclose nothing, the cascade running through Figures.
//
// A P marks MCID 0 around a filled rectangle. The page's /ParentTree array names,
// for each identifier, the nearest element written as an object of its own.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func directfigUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

	content := "/P <</MCID 0>> BDC\n0 g 50 700 100 20 re f\nEMC\n" +
		"/Figure <</MCID 1>> BDC\n50 600 m 150 650 l S\nEMC\n" +
		"/Figure <</MCID 2>> BDC\n50 500 m 150 550 l S\nEMC\n" +
		"/Figure <</MCID 3>> BDC\n50 400 m 150 450 l S\nEMC\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)
	page = pdf.Page(0)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot))

	direct := func(s string, parent *pdfgen.Dict, k any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(s), "P", parent, "NS", ns20, "Pg", page)
		if k != nil {
			dd.Set("K", k)
		}
		return dd
	}

	para := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", 0))
	sect := pdf.Indirect(d("Type", n("StructElem"), "S", n("Sect"), "P", document, "NS", ns20))
	sect.Set("K", pdf.MakeIndirect(a(
		direct("Figure", sect, 3),
		direct("Figure", sect, a(direct("Figure", sect, a(nil)))),
	)))

	document.Set("K", a(
		para,
		direct("Figure", document, 1),
		direct("Div", document, a(direct("Figure", document, 2))),
		sect,
		direct("Figure", document, 9),
	))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(para, document, document, sect))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
