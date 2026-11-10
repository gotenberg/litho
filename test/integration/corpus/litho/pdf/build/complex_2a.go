package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("complex_2a", complex2a) }

// complex2a builds complex_2a.pdf: PDF/A-2a, tagging defects layered on part-2 rules.
//
// Conformance level A adds a logical structure hierarchy, a role map that has
// to resolve, and language tags that have to parse. Those live in ISO 19005-2
// 6.7.3 and 6.7.4 and sit on top of every part-2 rule the file already breaks,
// so a level-A run has to report both layers at once.
//
// Three must-not-fire controls guard against over-suppression:
//   - /MarkInfo /Marked true and a wired /StructTreeRoot, the gate every
//     level-A structure rule sits behind,
//   - a standard structure type left alone next to one that is remapped,
//   - a conforming annotation with correct flags and an appearance stream.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complex2a() ([]byte, error) {
	pdf := pdfgen.New()

	tt := pdf.Indirect(unembeddedTrueType())
	img := rgbImage(pdf, true)

	lzwForm := pdf.Stream(nil)
	lzwForm.Data = lzwEncode([]byte("0 0 1 rg 5 5 20 20 re f\n"), 1)
	lzwForm.Set("Filter", n("LZWDecode"))
	lzwForm.Set("Type", n("XObject"))
	lzwForm.Set("Subtype", n("Form"))
	lzwForm.Set("BBox", a(0, 0, 30, 30))
	lzwForm.Set("Resources", d())

	groupForm := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 50, 50),
		"Group", d("Type", n("Group"), "S", n("Transparency")), "Resources", d())

	psxobj := pdf.Stream([]byte("%!PS\n"), "Type", n("XObject"), "Subtype", n("PS"))

	content := `q
/BadIntent ri
/P <</MCID 0>> BDC
1 0 0 rg
50 700 100 50 re f
BT /TT1 12 Tf 50 680 Td (Body text) Tj ET
EMC
/Div <</MCID 1>> BDC
0 0 0 1 k
50 640 100 50 re f
EMC
/MyType <</MCID 2>> BDC
0.5 g
50 610 100 10 re f
EMC
/Unmapped <</MCID 3>> BDC
q 100 0 0 60 50 520 cm /Im1 Do Q
EMC
/P <</MCID 4>> BDC
q 1 0 0 1 50 430 cm /Fm1 Do Q
q 1 0 0 1 150 430 cm /FmLZW Do Q
q 1 0 0 1 250 430 cm /PSX Do Q
EMC
Q
`

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"StructParents", 0,
		"Resources", d(
			"Font", d("TT1", tt),
			"XObject", d(
				"Im1", img,
				"Fm1", groupForm,
				"FmLZW", lzwForm,
				"PSX", pdf.MakeIndirect(psxobj),
			),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	elem := func(stype string, mcid int, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(stype), "P", document, "Pg", page, "K", mcid)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	para := elem("P", 0)
	div := elem("Div", 1)
	mytype := elem("MyType", 2)
	unmapped := elem("Unmapped", 3)
	// A structure element whose own /Lang fails the Language-Tag grammar of
	// ISO 19005-2, 6.7.4. The catalog carries a well-formed /Lang, so the
	// grammar check has to run per object rather than once per document.
	badLang := elem("P", 4, "Lang", text("english!"))

	mcidOwners := []any{para, div, mytype, unmapped, badLang}

	// Role map defects, one per ISO 19005-2, 6.7.3.4 test:
	//   t2  /MyType resolves to /Foo, which loops back through /Bar.
	//   t3  /Div is a standard type remapped to a non-standard one.
	//   t1  /Unmapped appears in the tree and in no mapping at all.
	// /Document is deliberately left out of the map: a standard type with no
	// entry must stay silent, so a firing on it would be a false positive.
	structRoot.Set("RoleMap", d(
		"MyType", n("/Foo"),
		"Foo", n("/Bar"),
		"Bar", n("/Foo"),
		"Div", n("/Custom"),
	))

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))

	movie := d(
		"Type", n("Annot"),
		"Subtype", n("Movie"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"T", text("clip"),
		"Movie", d("F", text("clip.mov")),
	)
	// Hidden flag set and Print clear: the flags are the defect (6.3.2).
	hidden := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 600, 500, 650),
		"F", 2,
		"C", a(0, 1, 0),
		"StructParent", 1,
	)
	okAP := pdf.Stream([]byte("0 0 20 20 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 20, 20), "Resources", d())
	okAnnot := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 500, 420, 520),
		"F", 4,
		"AP", d("N", pdf.MakeIndirect(okAP)),
		"StructParent", 2,
	)
	hiddenRef := pdf.Indirect(hidden)
	okRef := pdf.Indirect(okAnnot)
	pdf.Page(0).Set("Annots", a(pdf.Indirect(movie), hiddenRef, okRef))

	// Both Square annotations are markup annotations, so each is wrapped in its
	// own /Annot structure element and registered in the parent tree. That keeps
	// the tagging of the annotations conformant and isolates the annotation
	// defects (flags, missing appearance stream) from the structure defects.
	annotElems := make([]any, 0, 2)
	for _, ref := range []*pdfgen.Dict{hiddenRef, okRef} {
		annotElems = append(annotElems, pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n("Annot"),
			"P", document,
			"Pg", page,
			"K", d("Type", n("OBJR"), "Obj", ref),
		)))
	}

	document.Set("K", a(append(append([]any{}, mcidOwners...), annotElems...)...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(0, a(mcidOwners...), 1, annotElems[0], 2, annotElems[1]),
	)))
	structRoot.Set("ParentTreeNextKey", 3)

	pdf.Root.Set("OpenAction", d(
		"Type", n("Action"), "S", n("JavaScript"), "JS", text("app.alert('litho');")))
	pdf.Root.Set("NeedsRendering", true)
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer")))
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(ocg), "D", d("Order", a(), "AS", a())))

	// The conformance claim itself is correct: part 2, level A. The defects are
	// in the file, not in the claim, so a validator cannot take the shortcut of
	// rejecting the claim and stopping there.
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "A"}))

	return save(pdf, "1.7")
}
