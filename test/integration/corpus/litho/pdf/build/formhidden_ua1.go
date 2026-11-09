package build

import (
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formhidden_ua1", formhiddenUa1) }

// formhiddenUa1 builds formhidden_ua1.pdf: widgets a viewer never displays, under the form mutations.
//
// ISO 32000-1, Table 165: a Hidden annotation is neither displayed nor printed,
// and a NoView one is not displayed and prints only when its Print flag is set.
// Flattening the form drops such a widget with its field rather than bake its
// appearance into the page. Content that shows only in print needs an optional
// content group whose /AS applies a print usage (ISO 32000-1, 8.11.4.4), which
// ISO 14289-1, 7.10 and ISO 14289-2, 8.7 forbid, so a printed NoView widget goes
// as well. A Form element left holding nothing leaves the tree with its widget.
//
// Six text fields on one tagged page, each with an appearance painting a box and
// a /TU (ISO 14289-1, 7.18.1 test 3):
//
//   - "secret", /F 6 (Hidden, Print), the only kid of its Form element.
//   - "draft", /F 2 (Hidden), outside the structure tree: ISO 14289-1, 7.18.4
//     test 1 exempts a hidden widget from the Form tag.
//   - "stamp", /F 36 (NoView, Print), in a Form element: printed only.
//   - "note", /F 32 (NoView), in a Form element: neither displayed nor printed.
//   - "tip", /F 292 (NoView, ToggleNoView, Print), in a Form element, which
//     ISO 14289-2, 8.9.2.2 test 2 permits where it fails "stamp" and "note".
//   - "name", /F 4, displayed: flattening bakes it and tags the content under its
//     Form element, which takes the PrintField role tv.
//
// The page's text shows only A and B in the conforming embedded TrueType of
// ttcmap_2b.pdf, so no font clause fires. The XMP packet carries dc:title and
// pdfuaid:part 1.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formhiddenUa1() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoHiddenSans"),
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
	widths := make([]any, last-first+1)
	for i := range widths {
		widths[i] = ttAdvanceWidth
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoHiddenSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", a(widths...),
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

	// field is a text field merged with its widget, painting a grey box.
	field := func(name string, flags, y int, grey string, structParent any) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", n("Widget"),
			"Rect", a(50, y, 70, y+20),
			"F", flags,
			"P", page,
			"FT", n("Tx"),
			"T", text(name),
			"TU", text(strings.ToUpper(name[:1])+name[1:]),
			"V", text(name),
			"AP", d("N", appearance(grey+" g 0 0 20 20 re f\n")),
		)
		if structParent != nil {
			dd.Set("StructParent", structParent)
		}
		return pdf.Indirect(dd)
	}

	// ISO 32000-1, Table 165: bit 2 Hidden, bit 3 Print, bit 6 NoView, bit 9
	// ToggleNoView.
	secret := field("secret", 2|4, 700, "0.1", 1)
	draft := field("draft", 2, 660, "0.2", nil)
	stamp := field("stamp", 32|4, 620, "0.3", 2)
	note := field("note", 32, 580, "0.4", 3)
	tip := field("tip", 32|4|256, 540, "0.5", 4)
	name := field("name", 4, 500, "0.6", 5)
	widgets := []any{secret, draft, stamp, note, tip, name}
	page.Set("Annots", a(widgets...))
	pdf.Root.Set("AcroForm", d("Fields", a(widgets...)))

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))
	paragraph := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0))

	// form is a Form element whose only kid is an object reference to widget.
	form := func(widget *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n("Form"),
			"P", document,
			"Pg", page,
			"K", d("Type", n("OBJR"), "Obj", widget),
		))
	}

	formSecret := form(secret)
	formStamp := form(stamp)
	formNote := form(note)
	formTip := form(tip)
	formName := form(name)

	document.Set("K", a(paragraph, formSecret, formStamp, formNote, formTip, formName))
	structRoot.Set("K", document)
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(paragraph),
			1,
			formSecret,
			2,
			formStamp,
			3,
			formNote,
			4,
			formTip,
			5,
			formName,
		),
	)))
	structRoot.Set("ParentTreeNextKey", 6)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
