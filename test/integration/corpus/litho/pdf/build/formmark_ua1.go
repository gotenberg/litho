package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("formmark_ua1", formmarkUa1) }

// formmarkUa1 builds formmark_ua1.pdf: form XObjects judged by what they draw.
//
// ISO 14289-1, 7.1 asks that real content be tagged and everything else be
// marked as an artifact (test 3), and that neither sit inside the other (tests 1
// and 2). veraPDF builds its content items from what a form XObject draws, in the
// marked-content state around the Do (ISO 32000-1, 14.6), so a form that paints
// nothing is no content and the sequences a form opens are judged as the page's
// are. litho counted every Do of a form as one untagged content item and read no
// marked content inside a form, and no corpus file drew a form that paints
// nothing inside a tagged document, or opened a sequence inside a form.
//
// One tagged page paints a square as a /P and draws six forms:
//
//   - /Empty, a form with an empty content stream, outside marked content.
//   - /ArtForm, painting a square inside its own /Artifact sequence.
//   - /TagForm, with /StructParents 1, painting a square inside a /P sequence
//     whose /MCID 3 its ParentTree entry maps to a /P element. The page's entry
//     maps MCIDs 0 and 1 only, so the MCID resolves through the form's
//     /StructParents or not at all.
//   - /ObjrForm, with /StructParent 2, painting a square outside any marked
//     content: the /Figure its ParentTree entry names, through an OBJR, tags
//     everything it draws.
//   - /Chain, drawing /Inner, whose content stream is empty.
//   - /NestForm, drawn inside the page's /P sequence with /MCID 1. It has no
//     /StructParents, so its own /MCID 0 resolves nowhere and its square takes
//     the page's sequence as its tag. It then paints a second square inside an
//     /Artifact sequence, which opens inside tagged content: 7.1 tests 1 and 2.
//
// No form paints untagged content, so 7.1 test 3 and ISO 14289-2, 8.2.2 test 1
// stay silent, and the two findings come from inside a form. The page shows no
// text, so no font clause fires. The XMP packet claims PDF/UA-1.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formmarkUa1() ([]byte, error) {
	pdf := pdfgen.New()

	const square = "0 0 10 10 re f\n"

	// form is a form XObject over a 20 by 20 box, with its own /Resources.
	form := func(content string, extra ...any) *pdfgen.Stream {
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		hasResources := false
		for i := 0; i < len(extra); i += 2 {
			if extra[i] == "Resources" {
				hasResources = true
			}
		}
		if !hasResources {
			s.Set("Resources", d())
		}
		for i := 0; i < len(extra); i += 2 {
			s.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(s)
		return s
	}

	empty := form("")
	artForm := form("/Artifact BMC\n" + square + "EMC\n")
	tagForm := form("/P <</MCID 3>> BDC\n"+square+"EMC\n", "StructParents", 1)
	objrForm := form(square, "StructParent", 2)
	inner := form("")
	chain := form("/Inner Do\n", "Resources", d("XObject", d("Inner", inner)))
	nestForm := form(
		"/Span <</MCID 0>> BDC\n" + square + "EMC\n" +
			"/Artifact BMC\n0 10 10 10 re f\nEMC\n",
	)

	content := "/P <</MCID 0>> BDC\n50 700 100 20 re f\nEMC\n" +
		"q 1 0 0 1 50 660 cm /Empty Do Q\n" +
		"q 1 0 0 1 50 620 cm /ArtForm Do Q\n" +
		"q 1 0 0 1 50 580 cm /TagForm Do Q\n" +
		"q 1 0 0 1 50 540 cm /ObjrForm Do Q\n" +
		"q 1 0 0 1 50 500 cm /Chain Do Q\n" +
		"/P <</MCID 1>> BDC\nq 1 0 0 1 50 460 cm /NestForm Do Q\nEMC\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"XObject", d(
				"Empty", empty,
				"ArtForm", artForm,
				"TagForm", tagForm,
				"ObjrForm", objrForm,
				"Chain", chain,
				"NestForm", nestForm,
			),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	elem := func(tag pdfgen.Name, k any, extra ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{
			"Type", n("StructElem"), "S", tag, "P", document, "Pg", page, "K", k,
		}, extra...)...))
	}

	p0 := elem(n("P"), 0)
	p1 := elem(n("P"), 1)
	pForm := elem(n("P"), d("Type", n("MCR"), "MCID", 3, "Stm", tagForm))
	figure := elem(
		n("Figure"),
		d("Type", n("OBJR"), "Obj", objrForm),
		"Alt", text("a square"),
	)
	document.Set("K", a(p0, p1, pForm, figure))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(p0, p1),
			1,
			a(nil, nil, nil, pForm),
			2,
			figure,
		),
	)))
	structRoot.Set("ParentTreeNextKey", 3)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
