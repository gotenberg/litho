package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formstruct_ua2", formstructUa2) }

// formstructUa2 builds formstruct_ua2.pdf: widget annotations and Form structure under PDF/UA-2.
//
// The coverage scan showed ISO 14289-2, 8.10.1 as the last unreached cluster in
// PDF/UA-2 outside the deliberately skipped ones. It ties interactive form fields
// to the structure tree: every field widget belongs inside a `Form` structure
// element, a `Form` element holds at most one widget, and XFA is forbidden
// outright.
//
// Three defects sit in one file and are independent: two are structure
// associations, judged per annotation and per `Form` element, and the third is a
// key on the AcroForm dictionary. A check that reports the first misplaced widget
// and stops reports one rule where two are due, and a check that condemns the
// AcroForm on seeing a structure error reports three where the file earns them
// separately.
//
// Two controls sit beside them. `FormOK` encloses exactly one widget, which is
// the shape the clause asks for, and a widget with no `/StructParent` at all is
// outside the structure tree, which test 1 permits explicitly: its predicate
// passes when the structure parent type is null. That second control matters
// because the natural way to write the check, "every widget must sit under a
// Form", fires on it.
//
// Widgets reach the structure tree through `/StructParent` and an `/OBJR` entry
// in the element's `/K`, so the parent tree maps each widget's number back to the
// element that encloses it. Getting that wiring wrong makes every widget look
// unparented, which would fire test 1 everywhere and mask the rest.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formstructUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("/P <</MCID 0>> BDC\n"+
			"BT /F1 12 Tf 50 740 Td (interactive form) Tj ET\nEMC\n")),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot"), "Namespaces", a(ns20)))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ns20,
		"Lang", text("en-US"),
	))

	appearance := func() pdfgen.Object {
		s := pdf.Stream([]byte("0 0 0 rg 0 0 20 20 re f\n"))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		return pdf.MakeIndirect(s)
	}

	// widget is one text field widget. A negative structParent leaves it out of
	// the tree.
	widget := func(name string, y, structParent int) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", n("Widget"),
			"Rect", a(50, y, 70, y+20),
			"F", 4,
			"FT", n("Tx"),
			"T", text(name),
			"AP", d("N", appearance()),
		)
		if structParent >= 0 {
			dd.Set("StructParent", structParent)
		}
		return pdf.Indirect(dd)
	}

	element := func(kind string, kids ...*pdfgen.Dict) *pdfgen.Dict {
		e := pdf.Indirect(d("Type", n("StructElem"), "S", n(kind), "P", document, "NS", ns20, "Pg", page))
		k := a()
		for _, kid := range kids {
			k.Append(d("Type", n("OBJR"), "Obj", kid))
		}
		e.Set("K", k)
		return e
	}

	paragraph := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", 0))

	// Test 1: a field widget whose enclosing element is a paragraph, not a Form.
	misplaced := widget("misplaced", 700, 1)
	wrapperP := element("P", misplaced)

	// Test 2: one Form element enclosing two widgets.
	pairedA := widget("paired1", 660, 2)
	pairedB := widget("paired2", 620, 3)
	formTwo := element("Form", pairedA, pairedB)

	// Control: a Form enclosing exactly one widget.
	single := widget("single", 580, 4)
	formOne := element("Form", single)

	// Control: a widget outside the structure tree entirely, which test 1 permits
	// because its structure parent type is null.
	unparented := widget("unparented", 540, -1)

	document.Set("K", a(paragraph, wrapperP, formTwo, formOne))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0, a(paragraph),
			1, wrapperP,
			2, formTwo,
			3, formTwo,
			4, formOne,
		),
	)))
	structRoot.Set("ParentTreeNextKey", 5)

	page.Set("Annots", a(misplaced, pairedA, pairedB, single, unparented))

	// Test 3: XFA is forbidden outright.
	pdf.Root.Set("AcroForm", d(
		"Fields", a(misplaced, pairedA, pairedB, single, unparented),
		"XFA", pdf.MakeIndirect(
			pdf.Stream([]byte("<xdp:xdp xmlns:xdp='http://ns.adobe.com/xdp/'></xdp:xdp>")),
		),
	))

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /Metadata.
	return save(pdf, "2.0")
}
