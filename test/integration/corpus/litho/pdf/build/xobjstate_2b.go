package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("xobjstate_2b", xobjstate2b) }

// xobjstate2b builds xobjstate_2b.pdf: Form XObject keys, blend modes and page actions.
//
// The coverage scan showed four ISO 19005-2 rules unreached that judge different
// parts of a page: what a Form XObject dictionary may carry, what a graphics state
// may name as a blend mode, whether a page may act on being opened, and whether
// two Separations agreeing on a colorant name agree on how to render it.
//
// | Construct                                        | Rule         |
// | ------------------------------------------------ | ------------ |
// | a Form XObject carrying `/OPI`                   | `6.2.9/1`    |
// | an `ExtGState` naming a blend mode outside PDF   | `6.2.10/1`   |
// | a page carrying `/AA`                            | `6.5.2/2`    |
// | two Separations sharing a colorant, disagreeing  | `6.2.4.4/2`  |
//
// Four independent axes in one file, which is the masking probe: a check that
// abandons a page after its first complaint reports one rule where four are due.
//
// Each defect has a conforming neighbour, because every one of these tests turns
// on a value rather than on a kind: a second Form XObject with no `/OPI`, a second
// graphics state naming `/Multiply`, and a third Separation whose colorant name is
// its own. A firing against any of them is over-suppression.
//
// `6.2.9/1` reads `(Subtype2 == null || Subtype2 != "PS") && containsPS == false
// && containsOPI == false`, so three different keys break one test. Only `/OPI` is
// planted: the other two would collapse onto the same rule and prove nothing extra,
// while `/OPI` is the one a real producer emits.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xobjstate2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// form is a Form XObject painting a small square. Extra keys ride on the
	// dict.
	form := func(_ string, extra ...any) *pdfgen.Stream {
		stream := pdf.Stream([]byte("0 0 1 rg 0 0 20 20 re f\n"))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 20, 20))
		stream.Set("Resources", d())
		for i := 0; i < len(extra); i += 2 {
			stream.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(stream)
		return stream
	}

	// 6.2.9 t1: /OPI is forbidden on a Form XObject. The neighbour carries none.
	formOPI := form("FmO", "OPI", d("1.3", d("F", text("linked.tif"))))
	formOK := form("FmC")

	// 6.2.10 t1: the blend mode must be one PDF defines. /Foo is not, and the
	// neighbour names /Multiply, which is.
	gsBad := pdf.Indirect(d("Type", n("ExtGState"), "BM", n("/Foo")))
	gsOK := pdf.Indirect(d("Type", n("ExtGState"), "BM", n("Multiply")))

	// tint is a PostScript tint transform from one input to three RGB
	// components.
	tint := func(expr string) *pdfgen.Stream {
		stream := pdf.Stream([]byte(expr))
		stream.Set("FunctionType", 4)
		stream.Set("Domain", a(0, 1))
		stream.Set("Range", a(0, 1, 0, 1, 0, 1))
		pdf.MakeIndirect(stream)
		return stream
	}

	// 6.2.4.4 t2: two Separations naming the same colorant shall render it the same
	// way. These disagree, one painting it red and the other green. The third names
	// a colorant of its own, so it has nothing to disagree with.
	sepA := pdf.MakeIndirect(
		a(n("Separation"), n("/Spot"), n("DeviceRGB"), tint("{ dup 0 mul exch dup 0 mul exch 0 mul }")),
	)
	sepB := pdf.MakeIndirect(
		a(n("Separation"), n("/Spot"), n("DeviceRGB"), tint("{ dup 0 mul exch dup 1 mul exch 0 mul }")),
	)
	sepOK := pdf.MakeIndirect(
		a(n("Separation"), n("/Solo"), n("DeviceRGB"), tint("{ dup 0 mul exch dup 0 mul exch 1 mul }")),
	)

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"XObject", d("FmO", formOPI, "FmC", formOK),
			"ExtGState", d("GsB", gsBad, "GsC", gsOK),
			"ColorSpace", d("CsA", sepA, "CsB", sepB, "CsC", sepOK),
		),
		"Contents", pdf.Stream([]byte(
			"BT /F1 12 Tf 50 700 Td (form keys, blend modes and actions) Tj ET\n"+
				"q /GsB gs 1 0 0 1 60 600 cm /FmO Do Q\n"+
				"q /GsC gs 1 0 0 1 120 600 cm /FmC Do Q\n"+
				"q /CsA cs 1 scn 60 500 20 20 re f Q\n"+
				"q /CsB cs 1 scn 100 500 20 20 re f Q\n"+
				"q /CsC cs 1 scn 140 500 20 20 re f Q\n",
		)),
		// 6.5.2 t2: a page shall carry no additional-actions dictionary.
		"AA", d("O", d("S", n("Named"), "N", n("NextPage"))),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata, both independent of the
	// clauses above.
	return save(pdf, "1.7")
}
