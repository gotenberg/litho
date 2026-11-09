package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("headingnest_ua2", headingnestUa2) }

// headingnestUa2 builds headingnest_ua2.pdf: generic H headings nested in Sect elements, under
// PDF/UA-2.
//
// ISO 14289-2, 8.2.5.12 forbids the H structure type. ISO 32000-1, Table 335
// defines H as the label of the division it is the first child of, whose level a
// reader takes from the nesting of the sections. No corpus file nests an H more
// than one Sect deep, so this file does:
//
//	Document
//	  Sect        H, P, Sect
//	    Sect      H, P, Sect
//	      Sect    H, P
//	  Sect        H, P
//
// Each H is the first element of its Sect and the only H there. The four H
// are the file's one defect: the font is embedded, the XMP packet claims PDF/UA-2
// and every other element is a standard PDF 2.0 type.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func headingnestUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

	font := latinTrueTypeFont(pdf, "/LithoHeadingNest")

	// The marked-content tags name the structure type each sequence ends up under,
	// which veraPDF does not judge.
	textRuns := [][2]string{
		{"H", "AABB"},
		{"P", "ABAB"},
		{"H", "BABA"},
		{"P", "BBAA"},
		{"H", "ABBA"},
		{"P", "BAAB"},
		{"H", "AAAB"},
		{"P", "BBBA"},
	}
	var content []byte
	for mcid, t := range textRuns {
		content = fmt.Appendf(content, "/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n",
			t[0], mcid, 740-mcid*40, t[1])
	}
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream(content),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)
	page = pdf.Page(0)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))

	elem := func(s string, parent *pdfgen.Dict, mcid ...int) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(s), "P", parent, "NS", ns20)
		if len(mcid) > 0 {
			dd.Set("Pg", page)
			dd.Set("K", mcid[0])
		}
		return pdf.Indirect(dd)
	}

	document := elem("Document", structRoot)
	sectOuter := elem("Sect", document)
	sectMid := elem("Sect", sectOuter)
	sectInner := elem("Sect", sectMid)
	sectLast := elem("Sect", document)

	hOuter, pOuter := elem("H", sectOuter, 0), elem("P", sectOuter, 1)
	hMid, pMid := elem("H", sectMid, 2), elem("P", sectMid, 3)
	hInner, pInner := elem("H", sectInner, 4), elem("P", sectInner, 5)
	hLast, pLast := elem("H", sectLast, 6), elem("P", sectLast, 7)

	document.Set("K", a(sectOuter, sectLast))
	sectOuter.Set("K", a(hOuter, pOuter, sectMid))
	sectMid.Set("K", a(hMid, pMid, sectInner))
	sectInner.Set("K", a(hInner, pInner))
	sectLast.Set("K", a(hLast, pLast))

	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(hOuter, pOuter, hMid, pMid, hInner, pInner, hLast, pLast),
		),
	)))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
