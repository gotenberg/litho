package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("formflat_ua1", formflatUa1) }

// formflatUa1 builds formflat_ua1.pdf: tagged widgets with no /AcroForm, for the form mutations.
//
// Flattening a tagged form turns each widget's appearance into page content, and
// ISO 14289-1, 7.1 requires that content to be tagged or an artifact. litho hands
// the content to the structure element that enclosed the widget, and a Form
// element that now holds content instead of its widget takes a PrintField /Role
// (ISO 32000-1, Table 348), which ISO 14289-1, 7.18.4 test 2 asks of a Form
// element that holds anything but one widget reference. A widget with nothing to
// draw goes with the form, and an element left holding nothing goes with it.
//
// Three widgets on one tagged page, each the only kid of its own Form element:
//
//   - "empty", a text field with no value and no appearance: it draws nothing, so
//     flattening drops it and its Form element with it.
//   - the kid of "choice", a radio button that is on: its Form element states no
//     /Pg, so the flattened content is named by a marked-content reference that
//     carries the page, and the element takes the role rb.
//   - "go", a push button: its Form element takes the role pb.
//
// The catalog has no /AcroForm, a shape readers accept and real files have (pdfjs
// issue17794.pdf), so moving a page gives the document a form, which has to list
// the fields its own page already presents as well as the copies.
//
// Every appearance paints a rectangle and no text, and the page's text shows only
// A and B in the conforming embedded TrueType of ttcmap_2b.pdf, so no font clause
// fires. The file has no XMP packet.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formflatUa1() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoFlatSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	const first, last = 0x41, 0x42
	widths := a()
	for range last - first + 1 {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoFlatSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	// appearance is a 20 by 20 Form XObject appearance painting content.
	appearance := func(content string) *pdfgen.Stream {
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		pdf.MakeIndirect(s)
		return s
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 740 Td (AB) Tj ET\nEMC\n")),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	rect := func(y int) *pdfgen.Array {
		return a(50, y, 70, y+20)
	}

	// A text field with no value and no appearance: nothing to draw.
	empty := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", rect(700),
		"F", 4,
		"P", page,
		"FT", n("Tx"),
		"T", text("empty"),
		"TU", text("Empty"),
		"StructParent", 1,
	))

	// A radio button group whose one kid is on (ISO 32000-1, Table 226: bit 16 is
	// Radio, bit 15 NoToggleToOff).
	choice := pdf.Indirect(d(
		"FT", n("Btn"),
		"Ff", (1<<15)|(1<<14),
		"T", text("choice"),
		"TU", text("Choice"),
		"V", n("/On"),
	))
	radio := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", rect(660),
		"F", 4,
		"P", page,
		"Parent", choice,
		"AS", n("/On"),
		"AP", d(
			"N", d(
				"On", appearance("0 0 0 rg 5 5 10 10 re f\n"),
				"Off", appearance("0 0 0 RG 1 1 18 18 re S\n"),
			),
		),
		"StructParent", 2,
	))
	choice.Set("Kids", a(radio))

	// A push button (Table 226: bit 17 is Pushbutton).
	push := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", rect(620),
		"F", 4,
		"P", page,
		"FT", n("Btn"),
		"Ff", 1<<16,
		"T", text("go"),
		"TU", text("Go"),
		"AP", d("N", appearance("0.5 g 0 0 20 20 re f\n")),
		"StructParent", 3,
	))
	page.Set("Annots", a(empty, radio, push))

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))
	paragraph := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0))

	// form is a Form element whose only kid is an object reference to widget.
	form := func(widget *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("StructElem"),
			"S", n("Form"),
			"P", document,
			"K", d("Type", n("OBJR"), "Obj", widget),
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	formEmpty := form(empty, "Pg", page)
	formRadio := form(radio) // no /Pg: the flattened content names its page
	formPush := form(push, "Pg", page)

	document.Set("K", a(paragraph, formEmpty, formRadio, formPush))
	structRoot.Set("K", document)
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(0, a(paragraph), 1, formEmpty, 2, formRadio, 3, formPush),
	)))
	structRoot.Set("ParentTreeNextKey", 4)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /AcroForm and no /Metadata.
	return save(pdf, "1.7")
}
