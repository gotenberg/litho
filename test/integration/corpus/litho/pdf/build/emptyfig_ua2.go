package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("emptyfig_ua2", emptyfigUa2) }

// emptyfigUa2 builds emptyfig_ua2.pdf: a tagged document whose Figures lack alternate text,
// most of them enclosing nothing, each named by a different kind of reference.
//
// ISO 14289-2, 8.2.5.28.2 and ISO 14289-1, 7.3 ask every Figure for alternate
// text. Aggressive PDF/UA remediation removes a Figure that lacks it and encloses
// nothing, since no text could describe it, and cleans every reference to it. No
// corpus file reaches most of the forms an empty Figure takes, or most of the
// references the removal cleans, so this file packs them into two pages:
//
//   - F1's only kid is the null object.
//   - F2 lists MCID 7, which page 1 does not mark; the page's /ParentTree slot 7
//     names it.
//   - F3 holds an object reference whose /Obj names object 999, which the file
//     does not define and ISO 32000-1, 7.3.10 reads as null, and a /ParentTree
//     entry of its own, under key 5, which no annotation names. qpdf writes no
//     reference to an undefined object, so the builder patches it in, keeping
//     every offset.
//   - F4 holds F5 alone, and F5 a marked-content reference to MCID 8, which page 1
//     does not mark: both go, the cascade running through Figures.
//   - F6 carries /Alt and holds F7, which has no kid: F7 goes and F6 stays, empty.
//   - A Div holds F8 alone, whose MCID 9 page 1 does not mark: F8 goes and the Div
//     stays, empty.
//   - F9's MCID 10 is not marked either. The /IDTree names F9, the paragraph's /Ref
//     names it, and the outline item `Figure` names it in /SE and in the /SD of its
//     GoTo action to page 1, which then names the element page 1 opens on.
//   - F10's MCID 0 on page 2, which marks nothing, is the only element on page 2,
//     and the outline item `Page two` names it in the /SD of its GoTo action to
//     page 2. Removing F10 would leave that /SD no element to name and the
//     action its page destination alone, which ISO 14289-2, 8.8 rejects, so F10
//     stays.
//
// Three Figures lacking alternate text stay: F10, K2, whose MCID 2 the page
// marks around a drawing, and K3, which has no kid but which the outline item
// `Pinned` names in a destination array, where no page destination stands
// beside it.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func emptyfigUa2() ([]byte, error) {
	// Stands in for "999 0 R " until the patch below: the same eight bytes.
	const undefinedPlaceholder = "/ObjNone"
	const undefinedRef = "999 0 R "

	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoEmptyFig"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(
			d(
				"Type", n("FontDescriptor"),
				"FontName", n("/LithoEmptyFig"),
				"Flags", 32,
				"ItalicAngle", 0,
				"Ascent", 800,
				"Descent", -200,
				"CapHeight", 700,
				"StemV", 80,
				"FontBBox", a(0, -200, 550, 700),
				"FontFile2", pdf.MakeIndirect(fontFile),
			),
		),
	))

	page1Content := "/H1 <</MCID 0>> BDC\nBT /F1 12 Tf 50 740 Td (AAAB) Tj ET\nEMC\n" +
		"/P <</MCID 1>> BDC\nBT /F1 12 Tf 50 700 Td (AABA) Tj ET\nEMC\n" +
		"/Figure <</MCID 2>> BDC\n50 600 m 150 650 l S\nEMC\n"
	page2Content := "/Artifact BMC\n50 50 m 100 50 l S\nEMC\n"

	pages := make([]*pdfgen.Dict, 0, 2)
	for i, pc := range []struct {
		content   string
		resources *pdfgen.Dict
	}{
		{page1Content, d("Font", d("F1", font))},
		{page2Content, d()},
	} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", pc.resources,
			"Contents", pdf.Stream([]byte(pc.content)),
			"StructParents", i,
			"Tabs", n("S"),
		))
		pdf.AddPage(page)
		pages = append(pages, pdf.Page(i))
	}
	page1, page2 := pages[0], pages[1]

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot),
	)

	// elem is a structure element; page and k nil leave /Pg and /K out.
	elem := func(s string, parent, page, k any, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("/"+s), "P", parent, "NS", ns20)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		if page != nil {
			dd.Set("Pg", page)
		}
		if k != nil {
			dd.Set("K", k)
		}
		return pdf.Indirect(dd)
	}

	title := elem("H1", document, page1, 0)
	para := elem("P", document, page1, 1)
	k2 := elem("Figure", document, page1, 2)
	k3 := elem("Figure", document, page1, nil)

	f1 := elem("Figure", document, page1, a(nil))
	f2 := elem("Figure", document, page1, 7)
	f3 := elem(
		"Figure",
		document,
		page1,
		a(d("Type", n("OBJR"), "Obj", n(undefinedPlaceholder))),
	)
	f4 := elem("Figure", document, page1, nil)
	f5 := elem(
		"Figure",
		f4,
		nil,
		a(d("Type", n("MCR"), "Pg", page1, "MCID", 8)),
	)
	f4.Set("K", a(f5))
	f6 := elem("Figure", document, page1, nil, "Alt", text("A chart"))
	f7 := elem("Figure", f6, page1, nil)
	f6.Set("K", a(f7))
	div := elem("Div", document, nil, nil)
	f8 := elem("Figure", div, page1, 9)
	div.Set("K", a(f8))
	f9 := elem("Figure", document, page1, 10, "ID", text("fig9"))
	para.Set("Ref", a(f9))
	f10 := elem("Figure", document, page2, 0)

	document.Set("K", a(title, para, k2, k3, f1, f2, f3, f4, f6, div, f9, f10))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	structRoot.Set("IDTree", d("Names", a(text("fig9"), f9)))
	structRoot.Set("ParentTree", pdf.Indirect(
		d(
			"Nums", a(
				0,
				a(title, para, k2, nil, nil, nil, nil, f2, f5, f8, f9),
				1,
				a(f10),
				5,
				f3,
			),
		),
	))
	structRoot.Set("ParentTreeNextKey", 6)

	outlines := pdf.Indirect(d("Type", n("Outlines")))
	itemFigure := pdf.Indirect(d(
		"Title", text("Figure"),
		"Parent", outlines,
		"SE", f9,
		"A", d(
			"S", n("GoTo"), "D", a(page1, n("Fit")), "SD", a(f9, n("Fit")),
		),
	))
	itemPage2 := pdf.Indirect(d(
		"Title", text("Page two"),
		"Parent", outlines,
		"Prev", itemFigure,
		"A", d(
			"S", n("GoTo"), "D", a(page2, n("Fit")), "SD", a(f10, n("Fit")),
		),
	))
	itemPinned := pdf.Indirect(d(
		"Title", text("Pinned"),
		"Parent", outlines,
		"Prev", itemPage2,
		"Dest", a(k3, n("Fit")),
	))
	itemFigure.Set("Next", itemPage2)
	itemPage2.Set("Next", itemPinned)
	outlines.Set("First", itemFigure)
	outlines.Set("Last", itemPinned)
	outlines.Set("Count", 3)

	pdf.Root.Set("Outlines", outlines)
	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	data, err := save(pdf, "2.0")
	if err != nil {
		return nil, err
	}
	return patchOnce(data, []byte(undefinedPlaceholder), []byte(undefinedRef), "F3")
}
