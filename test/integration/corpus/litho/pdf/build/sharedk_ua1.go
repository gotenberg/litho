package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharedk_ua1", sharedkUa1) }

// sharedkUa1 builds sharedk_ua1.pdf: a structure /K array two elements share.
//
// ISO 32000-1, 14.7.2 makes the structure tree a strict tree, and Table 323 lets
// an element's /K be an array, which ISO 32000-1, 7.3.10 lets a file write as an
// object of its own. Two Sect elements here name one such array as their /K. It
// holds a Figure and a Span written directly in it, so no object number marks
// them: a walk that remembers only the elements it met, never the arrays it
// read, reads the two again under the second Sect. Were those direct elements to
// name the array again as their own /K, each level would double the work, which
// is how a four-object file ran until the caller's timeout; this file shares the
// array once, which every tool reads to the end.
//
// The Figure has no alternate text and encloses nothing (ISO 14289-1, 7.3 t1;
// ISO 14289-2, 8.2.5.28.2 t1). litho reads the shared array once and reports it
// once, as veraPDF 1.30.2 does: the rule set and the instance count agree.
//
// A P element beside the two Sects marks a filled rectangle, so the page has
// tagged content and draws no text.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedkUa1() ([]byte, error) {
	pdf := pdfgen.New()

	content := "/P <</MCID 0>> BDC\n0 g 50 700 100 20 re f\nEMC\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot, "Lang", text("en-US")))
	para := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0))
	sectA := pdf.Indirect(d("Type", n("StructElem"), "S", n("Sect"), "P", document))
	sectB := pdf.Indirect(d("Type", n("StructElem"), "S", n("Sect"), "P", document))

	// The array both Sects name as their /K, an object of its own holding two
	// elements written directly in it. Their /P names the first Sect.
	shared := pdf.MakeIndirect(a(
		d("Type", n("StructElem"), "S", n("Figure"), "P", sectA),
		d("Type", n("StructElem"), "S", n("Span"), "P", sectA),
	))
	sectA.Set("K", shared)
	sectB.Set("K", shared)

	document.Set("K", a(para, sectA, sectB))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(para)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
