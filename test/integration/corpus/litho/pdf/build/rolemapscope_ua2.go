package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("rolemapscope_ua2", rolemapscopeUa2) }

// rolemapscopeUa2 builds rolemapscope_ua2.pdf: private structure types the root /RoleMap maps
// to standard ones, read through the namespace each element declares.
//
// The structure tree root's legacy /RoleMap governs a type in no namespace, or in
// the PDF 1.7 namespace when that namespace declares no /RoleMapNS (ISO 32000-2,
// 14.8.6). A type an element declares in the PDF 2.0 namespace resolves through
// that namespace alone, and the PDF 2.0 namespace defines no private type. No
// corpus file declares a namespace on an element whose type only the legacy map
// maps, so this file does, beside the two placements the map does reach:
//
//   - the root's single child is `Workbook`, in no namespace, which the /RoleMap
//     maps to `Document`: the PDF 1.7 Document, so ISO 14289-2, 8.2.5.2 test 2
//     reports a root Document outside the PDF 2.0 namespace. Remediation to
//     PDF/UA-2 names it a PDF 2.0 Document;
//   - a `Para` declaring the PDF 2.0 namespace, which the /RoleMap maps to `P`
//     and which no mapping reaches there: ISO 14289-2, 8.2.4 test 1;
//   - a `Para` in no namespace and one declaring the PDF 1.7 namespace, both
//     mapped to `P` by the /RoleMap, which must not fire.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func rolemapscopeUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const pdf17NS = "http://iso.org/pdf/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoRoleMapScope"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRoleMapScope"),
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

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	ns17 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf17NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	workbook := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("/Workbook"), "P", structRoot),
	)

	// elem: ns nil, Python's default None, leaves /NS out.
	elem := func(s string, mcid int, ns *pdfgen.Dict) *pdfgen.Dict {
		dd := d(
			"Type", n("StructElem"), "S", n("/"+s), "P", workbook, "Pg", page, "K", mcid,
		)
		if ns != nil {
			dd.Set("NS", ns)
		}
		return pdf.Indirect(dd)
	}

	title := elem("H1", 0, ns20)
	paraPDF2 := elem("Para", 1, ns20)
	paraNone := elem("Para", 2, nil)
	paraPDF17 := elem("Para", 3, ns17)
	workbook.Set("K", a(title, paraPDF2, paraNone, paraPDF17))

	structRoot.Set("K", a(workbook))
	structRoot.Set("Namespaces", a(ns20, ns17))
	structRoot.Set("RoleMap", d("Workbook", n("Document"), "Para", n("P")))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(title, paraPDF2, paraNone, paraPDF17))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
