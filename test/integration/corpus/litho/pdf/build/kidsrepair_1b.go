package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("kidsrepair_1b", kidsrepair1b) }

// kidsrepair1b builds kidsrepair_1b.pdf: defects in the /Resources a page inherits through
// the page tree, and in pages written directly in their node's /Kids, at
// PDF/A-1b.
//
// ISO 32000-1, 7.7.3.4 hands a page stating no /Resources the dictionary of its
// nearest ancestor in the page tree, whole, and veraPDF reads the tree through
// /Kids. ISO 32000-1, Table 29 makes each element of /Kids an indirect
// reference, yet a page written directly in the array is still a page both tools
// judge. The transparency repair wrote an inline graphics state repair through
// the page's own /Resources entry, so it dropped every one a page inherits, and
// the repairs writing a page, or the /Resources a page states, had no object
// number to write a page written directly in /Kids at.
//
// Four pages, each drawing one graphics state with a constant alpha of 0.5:
//
// | Page  | Where it sits                     | Its /Resources | Draws | Lent by |
// | ----- | --------------------------------- | -------------- | ----- | ------- |
// | `Rt`  | the root's /Kids                  | none           | `GSR` | root    |
// | `Mis` | node M's /Kids, /Parent the root  | none           | `GSM` | node M  |
// | `Kid` | written directly in node K's /Kids| none           | `GSK` | node K  |
// | `Own` | written directly in node K's /Kids| its own        | `GSO` | itself  |
//
// `GSO` also carries a transfer function, and `Own` a transparency page group.
// Every graphics state is written directly in its /ExtGState, and every
// /ExtGState directly in its /Resources, so a repair can only be written through
// the page tree node holding them. The tree is built by hand: pikepdf's page list
// flattens it and pushes inherited attributes onto each page, which would delete
// the construct the fixture exists for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func kidsrepair1b() ([]byte, error) {
	pdf := pdfgen.New()

	// alpha is a graphics state with a constant stroke alpha of 0.5.
	alpha := func(extra ...any) *pdfgen.Dict {
		return d(append([]any{"Type", n("ExtGState"), "CA", 0.5}, extra...)...)
	}

	content := func(ops string) *pdfgen.Stream {
		s := pdf.Stream([]byte(ops))
		pdf.MakeIndirect(s)
		return s
	}

	node := func(parent, resources *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("Pages"), "Kids", a(), "Count", 0, "Parent", parent, "Resources", resources))
	}

	// attach appends kid to parent's /Kids and adds its pages to every /Count
	// above.
	attach := func(parent, kid *pdfgen.Dict, pages int) {
		parent.Get("Kids").(*pdfgen.Array).Append(kid)
		nd := parent
		for {
			nd.Set("Count", int(nd.Get("Count").(pdfgen.Int))+pages)
			if !nd.Has("Parent") {
				break
			}
			nd = nd.Get("Parent").(*pdfgen.Dict)
		}
	}

	root := pdf.Pages
	root.Set("MediaBox", a(0, 0, 200, 200))
	root.Set("Resources", d("ExtGState", d("GSR", alpha())))

	// Rt: states no /Resources, so it draws with the root's.
	attach(root, pdf.Indirect(d("Type", n("Page"), "Parent", root, "Contents", content("q /GSR gs Q\n"))), 1)

	// Mis: listed by node M while its /Parent names the root.
	m := node(root, d("ExtGState", d("GSM", alpha())))
	attach(root, m, 0)
	attach(m, pdf.Indirect(d("Type", n("Page"), "Parent", root, "Contents", content("q /GSM gs Q\n"))), 1)

	// Kid and Own: written directly in node K's /Kids. Kid states no /Resources
	// and draws with K's; Own states its own, with a transfer function (ISO
	// 19005-1, 6.2.8) beside the alpha, and a transparency group (ISO 19005-1,
	// 6.4).
	k := node(root, d("ExtGState", d("GSK", alpha())))
	attach(root, k, 0)
	attach(k, d("Type", n("Page"), "Parent", k, "Contents", content("q /GSK gs Q\n")), 1)
	attach(
		k,
		d(
			"Type", n("Page"),
			"Parent", k,
			"Resources", d("ExtGState", d("GSO", alpha("TR", n("Identity")))),
			"Group", d("Type", n("Group"), "S", n("Transparency")),
			"Contents", content("q /GSO gs Q\n"),
		),
		1,
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	return save(pdf, "1.4")
}
