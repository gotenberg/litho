package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("destforms_ua2", destformsUa2) }

// destformsUa2 builds destforms_ua2.pdf: a tagged document whose destinations take the
// forms PDF/UA-2 remediation gives a structure destination beyond a plain
// destination array.
//
// ISO 14289-2, 8.8 asks every destination inside the document to be a structure
// destination (ISO 32000-2, 12.3.2.3). A named destination is judged by the value
// it maps to, and a GoTo action wherever it hangs, additional actions included.
// No corpus file reaches a GoTo action under a page's /AA, or a direct one under
// an annotation's /AA, and none that the gate remediates to PDF/UA-2 names a
// destination through the catalog /Dests or writes a dictionary under /Dest, so
// this file does, over three pages that each open with a heading or a paragraph
// of their own:
//
//   - an outline item whose /Dest is (second), a /Names /Dests entry for page 2;
//   - an outline item whose GoTo action's /D is /third, a catalog /Dests entry
//     for page 3 (ISO 32000-1, 12.3.2.3);
//   - a Link on page 1 whose /Dest is /third and whose direct /AA holds a direct
//     GoTo action for page 1 under /E;
//   - a Link on page 1 whose /Dest is a GoTo action dictionary for page 2, which
//     ISO 32000-1, Table 173 does not allow and readers follow all the same, and
//     whose /AA, an object of its own, holds a GoTo action to (second) under /X;
//   - page 2's /AA, whose /O is a direct GoTo action for page 2 and whose /C is a
//     GoTo action for page 3 written as an object of its own (Table 195).
//
// No destination names a structure element, so each fails 8.8: test 1 for the
// first item and the two links' /Dest, test 2 for the five GoTo actions.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func destformsUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoDestForms"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoDestForms"),
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

	contents := []string{
		marked("H1", 0, 740, "AAAB") + marked("P", 1, 700, "AABA"),
		marked("P", 0, 740, "BAAA") + marked("P", 1, 700, "BBAA"),
		marked("H1", 0, 740, "BABA") + marked("P", 1, 700, "BBBA"),
	}
	var pages []*pdfgen.Dict
	for i, content := range contents {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream([]byte(content)),
			"StructParents", i,
			"Tabs", n("S"),
		))
		pdf.AddPage(page)
		pages = append(pages, pdf.Page(i))
	}

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "NS", ns20, "P", structRoot))

	// elem is a structure element; a nil page or k leaves /Pg or /K out.
	elem := func(s string, parent, page *pdfgen.Dict, k any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(s), "P", parent, "NS", ns20)
		if page != nil {
			dd.Set("Pg", page)
		}
		if k != nil {
			dd.Set("K", k)
		}
		return pdf.Indirect(dd)
	}

	sect1 := elem("Sect", document, nil, nil)
	title1 := elem("H1", sect1, pages[0], 0)
	intro := elem("P", sect1, pages[0], 1)

	sect2 := elem("Sect", document, nil, nil)
	cont := elem("P", sect2, pages[1], 0)
	more := elem("P", sect2, pages[1], 1)
	sect2.Set("K", a(cont, more))

	sect3 := elem("Sect", document, nil, nil)
	title3 := elem("H1", sect3, pages[2], 0)
	last := elem("P", sect3, pages[2], 1)
	sect3.Set("K", a(title3, last))

	appearance := func() pdfgen.Object {
		ap := pdf.Stream([]byte("0 0 1 RG 0 0 40 20 re S\n"))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 40, 20))
		return pdf.MakeIndirect(ap)
	}

	link := func(y int, entries ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", n("Link"),
			"Rect", a(50, y, 90, y+20),
			"F", 4,
			"Contents", text("a link"),
			"AP", d("N", appearance()),
		)
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	namedLink := link(
		600,
		"Dest", n("/third"),
		"AA", d("E", d("S", n("GoTo"), "D", a(pages[0], n("Fit")))),
	)
	dictLink := link(
		560,
		"Dest", d("Type", n("Action"), "S", n("GoTo"), "D", a(pages[1], n("Fit"))),
		"AA", pdf.Indirect(d("X", d("S", n("GoTo"), "D", text("second")))),
	)
	var linkElems []any
	for i, lk := range []*pdfgen.Dict{namedLink, dictLink} {
		key := len(pages) + i
		lk.Set("StructParent", key)
		linkElems = append(linkElems,
			elem("Link", sect1, pages[0], a(d("Type", n("OBJR"), "Obj", lk))))
	}
	pages[0].Set("Annots", a(namedLink, dictLink))
	sect1.Set("K", a(append([]any{title1, intro}, linkElems...)...))

	pages[1].Set("AA", d(
		"O", d("S", n("GoTo"), "D", a(pages[1], n("Fit"))),
		"C", pdf.Indirect(d("S", n("GoTo"), "D", a(pages[2], n("Fit")))),
	))

	document.Set("K", a(sect1, sect2, sect3))
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(title1, intro),
			1,
			a(cont, more),
			2,
			a(title3, last),
			3,
			linkElems[0],
			4,
			linkElems[1],
		),
	)))
	structRoot.Set("ParentTreeNextKey", 5)

	pdf.Root.Set("Names", d(
		"Dests", pdf.Indirect(d(
			"Names", a(text("second"), a(pages[1], n("XYZ"), 50, 752, nil)),
		)),
	))
	pdf.Root.Set("Dests", d("third", a(pages[2], n("Fit"))))

	outlines := pdf.Indirect(d("Type", n("Outlines")))
	first := pdf.Indirect(d("Title", text("Second page"), "Parent", outlines, "Dest", text("second")))
	second := pdf.Indirect(d(
		"Title", text("Third page"),
		"Parent", outlines,
		"Prev", first,
		"A", d("S", n("GoTo"), "D", n("/third")),
	))
	first.Set("Next", second)
	outlines.Set("First", first)
	outlines.Set("Last", second)
	outlines.Set("Count", 2)

	pdf.Root.Set("Outlines", outlines)
	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
