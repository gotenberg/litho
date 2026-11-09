package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("inlinekids_ua2", inlinekidsUa2) }

// inlinekidsUa2 builds inlinekids_ua2.pdf: Figures of their own that inline elements list.
//
// ISO 32000-1, 14.7.2, Table 323 lets an element's /K hold a structure element
// directly, with no object number of its own, and lets that inline element's /K
// list elements that do have one. ISO 14289-2, 8.2.5.28.2 and ISO 14289-1, 7.3
// ask each Figure for alternate text wherever it sits, and aggressive
// remediation takes out one that encloses nothing: a Figure only an inline
// element lists leaves that element's /K in the object holding it. No corpus
// file lists an element of its own from an inline one, so this file packs the
// places such a Figure sits into one page:
//
//   - F1, the /K of a Div written inline in the Document's /K array, holds
//     MCID 1, a drawing.
//   - F2, in the /K array of a Part written inline, holds MCID 9, which the page
//     does not mark: it encloses nothing.
//   - F3, alone in a /K array written as an object of its own that an inline
//     Sect names, has no kid.
//   - F4, the /K of a Figure itself written inline, holds the null object: both
//     enclose nothing, the cascade running through Figures.
//   - F5, the /K of an inline NonStruct that also names it in /Ref (ISO
//     32000-2, Table 355), has no kid.
//
// Each inline holder has a structure type of its own: veraPDF gives every
// element written inline the identity 0 0 obj and reads two inline elements of
// one type as one, so two inline Divs would hide the first one's Figure from it.
//
// A P marks MCID 0 around a filled rectangle. The page's /ParentTree array names
// the P and F1.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func inlinekidsUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

	content := "/P <</MCID 0>> BDC\n0 g 50 700 100 20 re f\nEMC\n" +
		"/Figure <</MCID 1>> BDC\n50 600 m 150 650 l S\nEMC\n"
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

	inline := func(s string, k any, extra ...any) *pdfgen.Dict {
		dd := d(append([]any{"Type", n("StructElem"), "S", n(s), "P", document, "NS", ns20, "Pg", page}, extra...)...)
		if k != nil {
			dd.Set("K", k)
		}
		return dd
	}

	figure := func(k any) *pdfgen.Dict {
		// An element's /P is an indirect reference (ISO 32000-1, Table 323), so
		// each Figure names the nearest element that has an object number.
		dd := d("Type", n("StructElem"), "S", n("Figure"), "P", document, "NS", ns20, "Pg", page)
		if k != nil {
			dd.Set("K", k)
		}
		return pdf.Indirect(dd)
	}

	para := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", 0))
	f1 := figure(1)
	f2 := figure(9)
	f3 := figure(nil)
	f4 := figure(a(nil))
	f5 := figure(nil)

	document.Set("K", a(
		para,
		inline("Div", f1),
		inline("Part", a(f2)),
		inline("Sect", pdf.MakeIndirect(a(f3))),
		inline("Figure", f4),
		inline("NonStruct", f5, "Ref", a(f5)),
	))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(para, f1)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
