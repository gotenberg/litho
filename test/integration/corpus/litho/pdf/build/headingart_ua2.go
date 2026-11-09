package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("headingart_ua2", headingartUa2) }

// headingartUa2 builds headingart_ua2.pdf: generic H headings nested in Art and Sect elements,
// under PDF/UA-2.
//
// ISO 14289-2, 8.2.5.12 forbids the H structure type. ISO 32000-1, 14.8.4.3.5
// has a strongly structured document's grouping elements nest to reflect its
// organization "into articles, sections, subsections", so an Art adds a level as
// a Sect does. No corpus file nests an H in a standard Art, so this file does:
//
//	Document
//	  Art         H, P, Sect
//	    Sect      H, P
//	  Art         H, P
//
// Each Art is in the PDF 1.7 namespace, which defines Art; every other element
// is in the PDF 2.0 namespace. Each H is the first element of its parent and
// the only H there. The three H are the file's one defect: the font is embedded,
// the XMP packet claims PDF/UA-2 and every other element is a standard type.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func headingartUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const pdf17NS = "http://iso.org/pdf/ssn"

	font := latinTrueTypeFont(pdf, "/LithoHeadingArt")

	// The marked-content tags name the structure type each sequence ends up under,
	// which veraPDF does not judge.
	textRuns := [][2]string{
		{"H", "AABB"},
		{"P", "ABAB"},
		{"H", "BABA"},
		{"P", "BBAA"},
		{"H", "ABBA"},
		{"P", "BAAB"},
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
	ns17 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf17NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))

	elem := func(s string, parent, ns *pdfgen.Dict, mcid ...int) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(s), "P", parent, "NS", ns)
		if len(mcid) > 0 {
			dd.Set("Pg", page)
			dd.Set("K", mcid[0])
		}
		return pdf.Indirect(dd)
	}

	document := elem("Document", structRoot, ns20)
	artFirst := elem("Art", document, ns17)
	sect := elem("Sect", artFirst, ns20)
	artLast := elem("Art", document, ns17)

	hArt, pArt := elem("H", artFirst, ns20, 0), elem("P", artFirst, ns20, 1)
	hSect, pSect := elem("H", sect, ns20, 2), elem("P", sect, ns20, 3)
	hLast, pLast := elem("H", artLast, ns20, 4), elem("P", artLast, ns20, 5)

	document.Set("K", a(artFirst, artLast))
	artFirst.Set("K", a(hArt, pArt, sect))
	sect.Set("K", a(hSect, pSect))
	artLast.Set("K", a(hLast, pLast))

	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20, ns17))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(0, a(hArt, pArt, hSect, pSect, hLast, pLast)),
	)))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
