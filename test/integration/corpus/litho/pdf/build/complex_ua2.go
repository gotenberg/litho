package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("complex_ua2", complexUa2) }

// complexUa2 builds complex_ua2.pdf: a PDF/UA-2 fixture built around part-2 specifics.
//
// ISO 14289-2 moves the goalposts from ISO 14289-1 in ways no single-rule
// corpus file exercises together: PDF 2.0 structure namespaces, a pdfuaid:rev
// of 2024 alongside pdfuaid:part 2, Unicode Private Use Area handling, and
// structure destinations. Every defect below is independent of the others, so
// a rule that short-circuits shows up as a missing class rather than a
// different verdict.
//
// Four must-not-fire controls guard against over-suppression:
//   - a Figure carrying /Alt next to one that carries none,
//   - a struct element with a well-formed /Lang next to one with a bad tag,
//   - a Link annotation properly enclosed in a Link structure element,
//   - an invisible annotation outside the structure tree, which 8.9.2.2
//     exempts because it has no enclosing structure element.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complexUa2() ([]byte, error) {
	const pua = ""

	pdf := pdfgen.New()

	// F1 renders ordinary text and carries no font program (ISO 14289-2, 8.4.5.5.1).
	f1 := pdf.Indirect(unembeddedTrueType())

	// F2 maps code 65 onto a Unicode PUA value. ISO 14289-2, 8.4.3 then demands an
	// /Alt or /ActualText on the enclosing structure element, and the P below has
	// neither.
	f2 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
		"FirstChar", 65,
		"LastChar", 65,
		"Widths", a(722),
		"ToUnicode", toUnicodeCMap(pdf, map[int]int{65: 0xE000}),
	))

	img := rgbImage(pdf, false)

	content := `/H1 <</MCID 0>> BDC
BT /F1 18 Tf 50 720 Td (Heading One) Tj ET
EMC
/P <</MCID 1>> BDC
BT /F1 12 Tf 50 690 Td (Body text with a bad language tag) Tj ET
EMC
/P <</MCID 2>> BDC
BT /F1 12 Tf 50 665 Td (Body text with a good language tag) Tj ET
EMC
/Figure <</MCID 3>> BDC
q 80 0 0 50 50 600 cm /Im1 Do Q
EMC
/Figure <</MCID 4>> BDC
q 80 0 0 50 150 600 cm /Im1 Do Q
EMC
/P <</MCID 5>> BDC
BT /F2 12 Tf 50 570 Td (A) Tj ET
EMC
/Alpha <</MCID 6>> BDC
BT /F1 12 Tf 50 545 Td (Namespaced custom type) Tj ET
EMC
/MyType <</MCID 7>> BDC
BT /F1 12 Tf 50 520 Td (Legacy custom type) Tj ET
EMC
/Gamma <</MCID 8>> BDC
BT /F1 12 Tf 50 495 Td (Unmapped namespaced type) Tj ET
EMC
/P <</MCID 9>> BDC
BT /F1 12 Tf 50 470 Td (Orphan element, no parent entry) Tj ET
EMC
/Link <</MCID 10>> BDC
BT /F1 12 Tf 50 445 Td (Jump to a page destination) Tj ET
EMC
BT /F1 12 Tf 50 415 Td (Untagged real content) Tj ET
q 1 0 0 RG 50 400 m 250 400 l S Q
`

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"StructParents", 0,
		"Resources", d(
			"Font", d("F1", f1, "F2", f2), "XObject", d("Im1", img),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// PDF 2.0 structure namespaces (ISO 32000-2, 14.7.4). The standard structure
	// namespace is what 8.2.5.2/2 wants on the Document element; the custom one
	// carries a /RoleMapNS entry that maps one of its own types onto another type
	// in the same namespace, which 8.2.4/3 forbids.
	ssn := pdf.Indirect(d("Type", n("Namespace"), "NS", text("http://iso.org/pdf2/ssn")))
	customNS := pdf.Indirect(d("Type", n("Namespace"), "NS", text("http://litho.example/ns")))
	customNS.Set("RoleMapNS", d("Alpha", a(n("/Beta"), customNS)))

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ssn,
		"Lang", text("en-US"),
	))

	// elem is a structure element under parent (nil leaves /P out), with the
	// extra keys set on it.
	elem := func(stype string, mcid int, parent *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(stype), "Pg", page, "K", mcid)
		if parent != nil {
			dd.Set("P", parent)
		}
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	h1 := elem("H1", 0, document)
	badLang := elem("P", 1, document, "Lang", text("english!"))
	goodLang := elem("P", 2, document, "Lang", text("en-US"))
	figureBad := elem("Figure", 3, document)
	figureOK := elem("Figure", 4, document, "Alt", text("A red, green and blue swatch"))
	puaPara := elem("P", 5, document)
	alpha := elem("Alpha", 6, document, "NS", customNS)
	mytype := elem("MyType", 7, document)
	// Gamma sits in the same non-standard namespace but carries no /RoleMapNS
	// entry at all, so it is unambiguously unmapped for ISO 14289-2, 8.2.4 t1.
	gamma := elem("Gamma", 8, document, "NS", customNS)
	// ISO 14289-2, 8.2.1 requires the /P entry on every structure element.
	orphan := elem("P", 9, nil)

	// Link annotation reached through a GoTo action whose destination is an
	// explicit page destination rather than a structure destination
	// (ISO 14289-2, 8.8). Its /Contents carries a PUA code point (8.6).
	linkAnnot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(50, 440, 250, 460),
		"F", 4,
		"Border", a(0, 0, 0),
		"Contents", text("Jump "+pua),
		"A", d(
			"Type", n("Action"),
			"S", n("GoTo"),
			"D", a(page, n("XYZ"), 0, 792, 0),
		),
		"StructParent", 1,
	))
	link := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Link"),
		"P", document,
		"Pg", page,
		"K", a(10, d("Type", n("OBJR"), "Obj", linkAnnot)),
	))

	// Invisible annotation inside the structure tree: ISO 14289-2, 8.9.2.2
	// requires it to be an artifact.
	invisible := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 700, 500, 750),
		"F", 1,
		"Contents", text("An invisible square"),
		"StructParent", 2,
	))
	invisibleElem := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Annot"),
		"P", document,
		"Pg", page,
		"K", d("Type", n("OBJR"), "Obj", invisible),
	))

	// Control: the same Invisible flag on an annotation with no enclosing
	// structure element. 8.9.2.2 exempts it, so a firing here is a false
	// positive that would hide the scoping of the rule above.
	ghost := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(400, 600, 500, 650),
		"F", 1,
		"Border", a(0, 0, 0),
	))

	pdf.Page(0).Set("Annots", a(linkAnnot, invisible, ghost))
	// Deliberately no /Tabs on the page (ISO 14289-2, 8.9.3.3).

	mcidOwners := []any{
		h1,
		badLang,
		goodLang,
		figureBad,
		figureOK,
		puaPara,
		alpha,
		mytype,
		gamma,
		orphan,
		link,
	}
	document.Set("K", a(append(append([]any{}, mcidOwners...), invisibleElem)...))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ssn, customNS))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(0, a(mcidOwners...), 1, link, 2, invisibleElem),
	)))
	structRoot.Set("ParentTreeNextKey", 3)

	// Circular role map for the legacy (PDF 1.7 namespace) custom type:
	// /MyType resolves to /Foo, which loops back through /Bar.
	structRoot.Set("RoleMap", d("MyType", n("/Foo"), "Foo", n("/Bar"), "Bar", n("/Foo")))

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	// /MarkInfo is deliberately correct: ISO 14289-2, 6.2 is the gate that every
	// clause-8 rule sits behind, so breaking it here would mask the whole fixture.

	// Optional content configurations (ISO 14289-2, 8.7). The default config
	// carries an /AS array, which t2 forbids outright. t1 wants a non-empty
	// /Name on every configuration, but veraPDF only reaches it when an
	// alternate /Configs array exists, so one is supplied.
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer")))
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(ocg),
		"D", d("Order", a(), "AS", a()),
		"Configs", a(d("Order", a())),
	))

	// Deliberately no /Lang on the catalog (ISO 14289-2, 8.4.4) and no
	// /ViewerPreferences, so /DisplayDocTitle is absent (8.11.2).

	// pdfuaid:part is 1 and pdfuaid:rev is 2023: two independent clause-5
	// defects in one packet. Catching both at once is what exposed the
	// early-return masking fixed in d0db11d.
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1, UARev: 2023}))

	return save(pdf, "2.0")
}
