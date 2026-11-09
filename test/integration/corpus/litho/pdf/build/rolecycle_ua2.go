package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("rolecycle_ua2", rolecycleUa2) }

// rolecycleUa2 builds rolecycle_ua2.pdf: role-map cycles through the root /RoleMap and
// through namespaces.
//
// ISO 14289-2, 8.2.4 test 2 forbids a circular mapping. veraPDF 1.30.2 judges it
// per structure element, two ways: it follows the root /RoleMap from the
// element's type name, whatever namespace the element declares, and reports a
// chain that revisits a type; and in a PDF 2.0 document it follows the
// /RoleMapNS chain from the element's type in its namespace and reports one that
// comes back to a type it left that namespace from. Under a root `Document` in
// the PDF 2.0 namespace:
//
//   - a `Sect` in no namespace, which the /RoleMap sends to `Book` and `Book`
//     back to `Sect`;
//   - a `Callout` in a custom namespace, which its /RoleMapNS sends to `Box` in
//     a second custom namespace, whose /RoleMapNS sends `Box` back to the first
//     namespace's `Callout`;
//   - a `Panel` in a third custom namespace whose /RoleMapNS sends `Panel` to
//     its own `Frame` and `Frame` back to `Panel`: a cycle that never leaves the
//     namespace, which is test 3's, not test 2's;
//   - a PDF 2.0 /RoleMapNS entry mapping `Note` to itself, which no element
//     uses.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func rolecycleUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoRoleCycleNS"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRoleCycleNS"),
			"Flags", 32,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)),
	))

	marked := func(tag string, mcid, y int, s string) string {
		return fmt.Sprintf("/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n", tag, mcid, y, s)
	}

	content := marked("H1", 0, 740, "AABB") +
		marked("P", 1, 700, "ABAB") +
		marked("P", 2, 660, "BABA") +
		marked("P", 3, 620, "BBAA")
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)
	page = pdf.Page(0)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	one := pdf.Indirect(
		d("Type", n("Namespace"), "NS", text("http://litho.example/ns/cycle-one")),
	)
	two := pdf.Indirect(
		d("Type", n("Namespace"), "NS", text("http://litho.example/ns/cycle-two")),
	)
	three := pdf.Indirect(
		d("Type", n("Namespace"), "NS", text("http://litho.example/ns/cycle-three")),
	)
	ns20.Set("RoleMapNS", d("Note", a(n("Note"), ns20)))
	one.Set("RoleMapNS", d("Callout", a(n("/Box"), two)))
	two.Set("RoleMapNS", d("Box", a(n("/Callout"), one)))
	three.Set("RoleMapNS", d(
		"Panel", a(n("/Frame"), three),
		"Frame", a(n("/Panel"), three),
	))

	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot),
	)

	// elem: ns nil leaves /NS out.
	elem := func(s string, parent *pdfgen.Dict, ns *pdfgen.Dict, k any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("/"+s), "P", parent, "K", k)
		if ns != nil {
			dd.Set("NS", ns)
		}
		return pdf.Indirect(dd)
	}

	para := func(parent *pdfgen.Dict, mcid int) *pdfgen.Dict {
		p := elem("P", parent, ns20, mcid)
		p.Set("Pg", page)
		return p
	}

	title := elem("H1", document, ns20, 0)
	title.Set("Pg", page)
	sect := elem("Sect", document, nil, a())
	callout := elem("Callout", document, one, a())
	panel := elem("Panel", document, three, a())
	paras := []*pdfgen.Dict{para(sect, 1), para(callout, 2), para(panel, 3)}
	sect.Set("K", a(paras[0]))
	callout.Set("K", a(paras[1]))
	panel.Set("K", a(paras[2]))
	document.Set("K", a(title, sect, callout, panel))

	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20, one, two, three))
	structRoot.Set("RoleMap", d("Sect", n("/Book"), "Book", n("Sect")))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(title, paras[0], paras[1], paras[2]))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
