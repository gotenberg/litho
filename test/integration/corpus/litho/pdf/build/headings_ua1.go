package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("headings_ua1", headingsUa1) }

// headingsUa1 builds headings_ua1.pdf: heading structure under PDF/UA-1.
//
// The coverage scan showed ISO 14289-1, 7.4.4 unreached. It asks two things of a
// document's headings: no node may hold more than one `H` child, and a document
// is either weakly structured with `H` or strongly structured with `H1` to `H6`,
// never both.
//
// The second requirement is stated twice, once per object: test 2 against `SEH`
// and test 3 against `SEHn`. A document carrying both kinds therefore fails both,
// because each kind offends from the other's point of view. That is measured on
// both tools, and it is what litho got wrong: it picked a structure "mode" from
// whichever heading came first and reported only the other kind, which is half
// the defect. Either kind alone agreed; only a document mixing them revealed it.
//
// Three defects sit in one tag tree and they are independent:
//
// | Element | Construct                                  | Rule       |
// | ------- | ------------------------------------------ | ---------- |
// | Sect1   | two `H` children under one node            | `7.4.4/1`  |
// | the `H` elements | a generic `H` beside numbered ones | `7.4.4/2`  |
// | H1      | a numbered heading beside generic ones     | `7.4.4/3`  |
//
// `Sect2` holds exactly one `H` child, which is the control for test 1: the
// verdict belongs to the node holding two, not to every node holding any.
//
// Beyond the clause the file carries two independent defects, no XMP packet and a
// non-embedded font, so a heading verdict is visibly separate from them.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func headingsUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	labels := []string{
		"first heading",
		"second heading",
		"lone heading",
		"numbered heading",
		"body text",
	}
	var content []byte
	for i, label := range labels {
		content = fmt.Appendf(content, "/P <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n", i, 740-i*24, label)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream(content),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"Lang", text("en-US"),
	))

	leaves := a()
	nextMCID := 0

	// element is one structure element owning the next marked content
	// sequence.
	element := func(kind string, parent *pdfgen.Dict) *pdfgen.Dict {
		made := pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n(kind),
			"P", parent,
			"Pg", page,
			"K", nextMCID,
		))
		nextMCID++
		leaves.Append(made)
		return made
	}

	section := func(children ...string) *pdfgen.Dict {
		s := pdf.Indirect(d("Type", n("StructElem"), "S", n("Sect"), "P", document, "Pg", page))
		k := a()
		for _, kind := range children {
			k.Append(element(kind, s))
		}
		s.Set("K", k)
		return s
	}

	// Two generic headings under one node, which the clause caps at one (t1).
	sectTwo := section("H", "H")
	// Exactly one generic heading: the control for t1.
	sectOne := section("H")
	// A numbered heading beside the generic ones, which mixes the two structure
	// styles and fails both halves of the split (t2 and t3).
	numbered := element("H1", document)
	body := element("P", document)

	document.Set("K", a(sectTwo, sectOne, numbered, body))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, leaves))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /Metadata, and the font is not embedded.
	return save(pdf, "1.7")
}
