package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("treekids_ua1", treekidsUa1) }

// treekidsUa1 builds treekids_ua1.pdf: structure parent and ID trees whose /Kids arrays
// are objects of their own, and text past the crop box, under PDF/UA-1.
//
// ISO 32000-1, 7.9.6 and 7.9.7 make a name or number tree's /Kids an array of
// indirect references to its nodes; nothing stops the array itself being written
// as an object of its own and named by reference. Here the /ParentTree (ISO
// 32000-1, 14.7.4.4) and the /IDTree (Table 322) each hold their one leaf
// directly in such an array. A walk that marks only the nodes it met, never the
// arrays it read, reads that array again wherever a direct node names it, and a
// file whose direct nodes each name their own array again doubles the work per
// level. This file names each array once, which every tool reads to the end: it
// gives the split, merge and flatten rewrites, which read both trees whole to
// carry the structure, the shape to read.
//
// | Tree        | Root                | /Kids                  | Leaf, written directly in /Kids |
// | ----------- | ------------------- | ---------------------- | ------------------------------- |
// | /ParentTree | an object, no /Nums | an array, by reference | /Nums for keys 0 and 1          |
// | /IDTree     | an object, no /Names| an array, by reference | /Names for p1, p2 and p3        |
//
// Page 1 inherits its /MediaBox [0 0 612 792] from the page tree node and states
// /CropBox [0 0 300 792]. Its second paragraph is drawn at x = 400, past the crop
// box: a viewer shows the crop box and hides it, a reader that ignores the crop
// box shows it. A redaction of page 1 clears it all the same, since it answers
// for every reader of the file. Page 2 draws one paragraph.
//
// The font is Helvetica, not embedded (ISO 14289-1, 7.21.4.1 t1), and the XMP
// packet carries no dc:title (7.1 t9): both are independent of the trees.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func treekidsUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	para := func(mcid, x, y int, s string) string {
		return fmt.Sprintf("/P <</MCID %d>> BDC\nBT /F1 12 Tf %d %d Td (%s) Tj ET\nEMC\n", mcid, x, y, s)
	}

	pdf.Pages.Set("MediaBox", a(0, 0, 612, 792))
	resources := d("Font", d("F1", font))
	page1 := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Pages,
		"CropBox", a(0, 0, 300, 792),
		"Resources", resources,
		"Contents", pdf.MakeIndirect(
			pdf.Stream([]byte(para(0, 50, 700, "inside the crop box")+para(1, 400, 700, "past the crop box"))),
		),
		"StructParents", 0,
	))
	page2 := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Pages,
		"Resources", resources,
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(para(0, 50, 700, "page two")))),
		"StructParents", 1,
	))
	pdf.Pages.Set("Kids", a(page1, page2))
	pdf.Pages.Set("Count", 2)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "P", structRoot, "Lang", text("en-US")),
	)
	p1 := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page1, "K", 0, "ID", text("p1")),
	)
	p2 := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page1, "K", 1, "ID", text("p2")),
	)
	p3 := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page2, "K", 0, "ID", text("p3")),
	)
	document.Set("K", a(p1, p2, p3))
	structRoot.Set("K", a(document))

	parentKids := pdf.MakeIndirect(
		a(d("Limits", a(0, 1), "Nums", a(0, a(p1, p2), 1, a(p3)))),
	)
	structRoot.Set("ParentTree", pdf.Indirect(d("Kids", parentKids)))
	structRoot.Set("ParentTreeNextKey", 2)

	idKids := pdf.MakeIndirect(
		a(
			d(
				"Limits", a(text("p1"), text("p3")),
				"Names", a(text("p1"), p1, text("p2"), p2, text("p3"), p3),
			),
		),
	)
	structRoot.Set("IDTree", pdf.Indirect(d("Kids", idKids)))

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
