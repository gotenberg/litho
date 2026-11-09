package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("rolemapns_ua2", rolemapnsUA2) }

// rolemapnsUa2 builds rolemapns_ua2.pdf: private structure types the root /RoleMap maps,
// declared in namespaces that map does not govern.
//
// The structure tree root's legacy /RoleMap governs a type in no namespace, or in
// the PDF 1.7 namespace when that namespace declares no /RoleMapNS (ISO 32000-2,
// 14.8.6). A type declared in any other namespace resolves through that
// namespace's /RoleMapNS alone, so ISO 14289-2, 8.2.4 test 1 reports each element
// below although the /RoleMap maps its type to a standard one. No corpus file
// declares such a namespace, so this file does, one placement per repair a
// PDF/UA-2 remediation makes:
//
//   - a `Heading` and a `Para` in a custom namespace that declares no
//     /RoleMapNS, which the /RoleMap maps to `H1` and `P`;
//   - a `Para` in a second custom namespace whose /RoleMapNS, an indirect
//     object, maps only a type no element uses;
//   - a `Para` in the PDF 1.7 namespace, which declares a /RoleMapNS of its own
//     and so takes itself off the /RoleMap;
//   - a `Block` in the PDF 2.0 namespace, which the /RoleMap maps to `P` through
//     the private `Body`.
//
// The root `Document` declares the PDF 2.0 namespace. The font is embedded and
// draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func rolemapnsUA2() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		pdf20NS  = "http://iso.org/pdf2/ssn"
		pdf17NS  = "http://iso.org/pdf/ssn"
		customNS = "http://litho.example/rolemapns/a"
		mappedNS = "http://litho.example/rolemapns/b"
	)
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoRoleMapNS"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRoleMapNS"),
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
		marked("P", 3, 620, "BBAA") +
		marked("P", 4, 580, "AAAB")
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
	ns17 := pdf.Indirect(d(
		"Type", n("Namespace"),
		"NS", text(pdf17NS),
		"RoleMapNS", d("Chapter", a(n("Sect"), ns20)),
	))
	custom := pdf.Indirect(d("Type", n("Namespace"), "NS", text(customNS)))
	mapped := pdf.Indirect(d(
		"Type", n("Namespace"),
		"NS", text(mappedNS),
		"RoleMapNS", pdf.Indirect(d("Other", a(n("Span"), ns20))),
	))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot))

	elem := func(s string, mcid int, ns *pdfgen.Dict) any {
		return pdf.Indirect(d(
			"Type", n("StructElem"), "S", n(s), "NS", ns, "P", document, "Pg", page, "K", mcid,
		))
	}

	kids := []any{
		elem("Heading", 0, custom),
		elem("Para", 1, custom),
		elem("Para", 2, mapped),
		elem("Para", 3, ns17),
		elem("Block", 4, ns20),
	}
	document.Set("K", a(kids...))

	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20, ns17, custom, mapped))
	structRoot.Set("RoleMap", d(
		"Heading", n("H1"), "Para", n("P"), "Block", n("/Body"), "Body", n("P"),
	))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(kids...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
