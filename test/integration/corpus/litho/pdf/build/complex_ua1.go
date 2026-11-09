package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("complex_ua1", complexUa1) }

// complexUa1 builds complex_ua1.pdf: a PDF/UA-1 fixture packing many structure defects.
//
// The structure tree is deliberately well-formed enough to reach every rule:
// a missing /StructTreeRoot would mask most of ISO 14289-1 clause 7, which is
// exactly the masking this corpus exists to catch. Marked content, the parent
// tree and the role map are all wired, and the defects are planted one per
// construct.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complexUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(unembeddedTrueType("/Helvetica"))
	img := rgbImage(pdf, false)

	content := `/H1 <</MCID 0>> BDC
BT /F1 18 Tf 50 720 Td (Heading One) Tj ET
EMC
/H3 <</MCID 1>> BDC
BT /F1 14 Tf 50 690 Td (Skipped To Three) Tj ET
EMC
/P <</MCID 2>> BDC
BT /F1 12 Tf 50 660 Td (Body text) Tj ET
EMC
/Figure <</MCID 3>> BDC
q 100 0 0 60 50 580 cm /Im1 Do Q
EMC
/MyType <</MCID 4>> BDC
BT /F1 12 Tf 50 550 Td (Custom type) Tj ET
EMC
BT /F1 12 Tf 50 520 Td (Untagged real content) Tj ET
q 1 0 0 RG 50 500 m 200 500 l S Q
`

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"StructParents", 0,
		"Resources", d(
			"Font", d("F1", font), "XObject", d("Im1", img),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	elem := func(stype string, mcid int, parent *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n(stype),
			"P", parent,
			"Pg", page,
			"K", mcid,
		))
	}

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	h1 := elem("H1", 0, document)
	h3 := elem("H3", 1, document)
	para := elem("P", 2, document)
	figure := elem("Figure", 3, document) // no /Alt, no /ActualText
	custom := elem("MyType", 4, document)

	// Link structure element wrapping a link annotation carrying no /Contents.
	linkAnnot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(50, 470, 200, 490),
		"F", 4,
		"Border", a(0, 0, 0),
		"A", d("Type", n("Action"), "S", n("URI"), "URI", text("https://example.org")),
		"StructParent", 1,
	))
	link := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Link"),
		"P", document,
		"Pg", page,
		"K", d("Type", n("OBJR"), "Obj", linkAnnot),
	))
	pdf.Page(0).Set("Annots", a(linkAnnot))

	document.Set("K", a(h1, h3, para, figure, custom, link))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(h1, h3, para, figure, custom), 1, link)),
	))
	structRoot.Set("ParentTreeNextKey", 2)

	// Circular role map: /MyType resolves to /Foo, which loops back through /Bar.
	// ISO 14289-1, 7.1 requires every non-standard type to map to a standard one.
	structRoot.Set("RoleMap", d("MyType", n("/Foo"), "Foo", n("/Bar"), "Bar", n("/Foo")))

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	// Deliberately no /Lang on the catalog (ISO 14289-1, 7.2).
	// /DisplayDocTitle absent from /ViewerPreferences (ISO 14289-1, 7.1).
	pdf.Root.Set("ViewerPreferences", d("FitWindow", true))

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
