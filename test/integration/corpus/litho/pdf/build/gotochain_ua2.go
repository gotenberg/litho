package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("gotochain_ua2", gotochainUa2) }

// gotochainUa2 builds gotochain_ua2.pdf: a tagged document whose GoTo actions run after
// other actions, through /Next.
//
// ISO 32000-1, 12.6.2, Table 193 lets an action's /Next hold one action or an
// array of actions, performed after it, so the actions an owner triggers form a
// tree. ISO 14289-2, 8.8 asks every destination inside the document to be a
// structure destination (ISO 32000-2, 12.3.2.3), a GoTo action's among them
// wherever it runs. Two corpus files chain a GoTo action after another action,
// neither tagged, and none writes /Next as an array, loops it back on itself or
// shares a chained action between two owners, so this file does, over three
// pages that each open with a heading of their own:
//
//   - an outline item whose direct GoTo action for page 1 has a direct /Next
//     array: a Named action, then a GoTo action for page 2 written as an object
//     of its own;
//   - the catalog /OpenAction, a direct GoTo action for page 1 whose /Next is
//     that same GoTo action for page 2;
//   - a Link on page 1 whose GoTo action for page 3 runs a URI action, then a
//     GoTo action for page 2, whose /Next loops back to the first, all three
//     objects of their own;
//   - a Link on page 1 whose Named action has a /Next written as an array object
//     of its own, holding a direct GoTo action for page 3 and a direct GoTo
//     action whose /SD already names page 1's heading;
//   - page 2's /AA /O, a Named action written as an object of its own whose
//     /Next is a direct GoTo action to (second), a /Names /Dests entry for page 2.
//
// Seven GoTo actions give no structure destination, each failing 8.8 test 2
// once: the shared one is one object, judged once. The one carrying /SD passes.
//
// The font is embedded and draws only `A` and `B`, which is all the text uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func gotochainUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	latin := map[int]string{0x41: "A", 0x42: "B"}

	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoGoToChain"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"Encoding", n("WinAnsiEncoding"),
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoGoToChain"),
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
		marked("H1", 0, 740, "BAAA") + marked("P", 1, 700, "BBAA"),
		marked("H1", 0, 740, "BABA") + marked("P", 1, 700, "BBBA"),
	}
	pages := make([]*pdfgen.Dict, 0, len(contents))
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

	sections := make([]*pdfgen.Dict, 0, len(pages))
	headings := make([]*pdfgen.Dict, 0, len(pages))
	paragraphs := make([]*pdfgen.Dict, 0, len(pages))
	for _, page := range pages {
		sect := elem("Sect", document, nil, nil)
		heading := elem("H1", sect, page, 0)
		para := elem("P", sect, page, 1)
		sections = append(sections, sect)
		headings = append(headings, heading)
		paragraphs = append(paragraphs, para)
	}

	goTo := func(page *pdfgen.Dict, view []any, extra ...any) *pdfgen.Dict {
		dd := d("S", n("GoTo"), "D", a(append([]any{page}, view...)...))
		for i := 0; i+1 < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return dd
	}

	shared := pdf.Indirect(goTo(pages[1], []any{n("Fit")}))

	loopFirst := pdf.Indirect(goTo(pages[2], []any{n("Fit")}))
	loopURI := pdf.Indirect(d("S", n("URI"), "URI", text("https://example.com/")))
	loopLast := pdf.Indirect(goTo(pages[1], []any{n("FitH"), 700}))
	loopFirst.Set("Next", loopURI)
	loopURI.Set("Next", loopLast)
	loopLast.Set("Next", loopFirst)

	later := pdf.MakeIndirect(a(
		goTo(pages[2], []any{n("Fit")}),
		goTo(pages[0], []any{n("Fit")}, "SD", a(headings[0], n("Fit"))),
	))

	appearance := func() *pdfgen.Stream {
		ap := pdf.Stream([]byte("0 0 1 RG 0 0 40 20 re S\n"))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 40, 20))
		pdf.MakeIndirect(ap)
		return ap
	}

	link := func(y int, action *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Link"),
			"Rect", a(50, y, 90, y+20),
			"F", 4,
			"Contents", text("a link"),
			"AP", d("N", appearance()),
			"A", action,
		))
	}

	loopLink := link(600, loopFirst)
	namedLink := link(560, d("S", n("Named"), "N", n("NextPage"), "Next", later))
	linkElems := make([]*pdfgen.Dict, 0, 2)
	for i, lk := range []*pdfgen.Dict{loopLink, namedLink} {
		lk.Set("StructParent", len(pages)+i)
		linkElems = append(linkElems, elem(
			"Link",
			sections[0],
			pages[0],
			a(d("Type", n("OBJR"), "Obj", lk)),
		))
	}
	pages[0].Set("Annots", a(loopLink, namedLink))

	k0 := a(headings[0], paragraphs[0])
	for _, le := range linkElems {
		k0.Append(le)
	}
	sections[0].Set("K", k0)
	for i := 1; i < len(sections); i++ {
		sections[i].Set("K", a(headings[i], paragraphs[i]))
	}

	pages[1].Set("AA", d(
		"O", pdf.Indirect(d(
			"S", n("Named"),
			"N", n("FirstPage"),
			"Next", d("S", n("GoTo"), "D", text("second")),
		)),
	))

	sectArr := a()
	for _, s := range sections {
		sectArr.Append(s)
	}
	document.Set("K", sectArr)
	structRoot.Set("K", a(document))
	structRoot.Set("Namespaces", a(ns20))
	nums := a()
	for i := range pages {
		nums.Append(i, a(headings[i], paragraphs[i]))
	}
	for i, le := range linkElems {
		nums.Append(len(pages)+i, le)
	}
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", nums)))
	structRoot.Set("ParentTreeNextKey", len(pages)+len(linkElems))

	pdf.Root.Set("Names", d(
		"Dests", pdf.Indirect(d(
			"Names", a(text("second"), a(pages[1], n("XYZ"), 50, 752, nil)),
		)),
	))

	outlines := pdf.Indirect(d("Type", n("Outlines")))
	item := pdf.Indirect(d(
		"Title", text("First page"),
		"Parent", outlines,
		"A", goTo(
			pages[0],
			[]any{n("Fit")},
			"Next", a(d("S", n("Named"), "N", n("NextPage")), shared),
		),
	))
	outlines.Set("First", item)
	outlines.Set("Last", item)
	outlines.Set("Count", 1)

	pdf.Root.Set("OpenAction", goTo(pages[0], []any{n("XYZ"), nil, nil, 0}, "Next", shared))
	pdf.Root.Set("Outlines", outlines)
	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 2, UARev: 2024}))

	return save(pdf, "2.0")
}
