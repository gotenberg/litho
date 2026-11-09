package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("grouping_ua2", groupingUa2) }

// groupingUa2 builds grouping_ua2.pdf: the two halves of an ISO/TS 32005 Table 5 row.
//
// Table 5 states most rows twice, as two rules sharing a clause and differing only
// by test number: one governs the parent "when used as a grouping element", the
// other "when used as a non-grouping element". Which half applies is not a
// property of the type name. ISO 32000-2:2020, 14.8.4.2 lets `Link` sit at block
// or inline level, so veraPDF decides per element, in
// `GFPDStructElem.getisGrouping`: a type that groups by name always groups, a
// block-or-inline type groups when it carries no direct content and no inline
// child, and anything else never does.
//
// A coverage scan over 5170 corpus files found both grouping-dependent kinds
// unreached: no file anywhere held a `Link`, `Annot`, `Reference` or `FENote` used
// as a container. litho therefore decided the question from a static type set and
// answered both halves wrongly, reporting test 2 for a `Link` holding one
// `Caption` (veraPDF reports nothing) and missing test 1 for a `Link` holding two.
//
// Five `Link` elements sit side by side, none carrying a content item of its own,
// so the only thing that varies is what each contains:
//
// | Construct                                   | Rule                    |
// | ------------------------------------------- | ----------------------- |
// | grouping `Link`, two `Caption` kids         | `Table 5. Link-Caption/1` |
// | non-grouping `Link`, `Span` + `H1` kids     | `Table 5. Link-Hn/1`      |
// | non-grouping `Link`, `Span` + `Caption` kids | `Table 5. Link-Caption/2` |
//
// The `Span` is what makes the last two non-grouping, and it is the whole point of
// planting them beside the first: the same `Caption` kid is legal under a grouping
// `Link` and forbidden under a non-grouping one, so a checker that keys on the
// type name cannot pass both. Two controls complete the pair, and each is a
// construct the wrong half would condemn:
//
// | Control                                | Why it must stay silent                    |
// | -------------------------------------- | ------------------------------------------ |
// | grouping `Link`, one `Caption` kid     | "at most one" is satisfied                 |
// | grouping `Link`, one `H1` kid          | the `Hn` row governs non-grouping use only |
//
// Every firing above was measured on veraPDF 1.30.2 and litho, one construct per
// file, before being planted together.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func groupingUa2() ([]byte, error) {
	pdf := pdfgen.New()

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
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (grouping by use) Tj ET\n")),
	))
	pdf.AddPage(page)

	// elem is one structure element carrying no content item of its own.
	//
	// No /K of an integer or an /MCR anywhere below: a direct content item is what
	// takes a block-or-inline type out of the grouping role, so planting one would
	// silently flip every construct here to its other half.
	elem := func(structType string, parent *pdfgen.Dict, kids []any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(structType), "Pg", page)
		if kids != nil {
			dd.Set("K", a(kids...))
		}
		obj := pdf.Indirect(dd)
		if parent != nil {
			obj.Set("P", parent)
		}
		return obj
	}

	// linkWith is a Link holding one fresh child per named type, in order.
	linkWith := func(children ...string) *pdfgen.Dict {
		link := elem("Link", nil, nil)
		k := a()
		for _, t := range children {
			k.Append(elem(t, link, nil))
		}
		link.Set("K", k)
		return link
	}

	// Order matters only for readability; each element is judged on its own.
	links := []any{
		linkWith("Caption", "Caption"), // grouping, two captions -> t1
		linkWith("Span", "H1"),         // non-grouping, heading   -> Hn t1
		linkWith("Span", "Caption"),    // non-grouping, caption   -> t2
		linkWith("Caption"),            // control: at most one is met
		linkWith("H1"),                 // control: grouping, Hn row does not apply
	}

	document := elem("Document", nil, links)
	for _, link := range links {
		link.(*pdfgen.Dict).Set("P", document)
	}

	structRoot := pdf.Indirect(d(
		"Type", n("StructTreeRoot"),
		"K", a(document),
		"ParentTree", pdf.Indirect(d("Nums", a())),
	))
	document.Set("P", structRoot)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))

	return save(pdf, "2.0")
}
