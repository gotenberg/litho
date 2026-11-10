package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("annotlang_ua1", annotlangUa1) }

// annotlangUa1 builds annotlang_ua1.pdf: languages annotations name, and symbolic encodings.
//
// Two independent constructs, neither of which any file of the seven corpora
// reaches.
//
// The /Lang of the structure element an annotation or a form field names by its
// /StructParent. veraPDF reads it as a CosLang (GFPDAnnot.getLang,
// GFPDFormField.getLang) whether or not the structure tree reaches the element,
// and every rule scoped to CosLang judges it: ISO 14289-1, 7.2 t29, ISO 14289-2,
// 8.4.4 t2, ISO 19005-1, 6.8.4 t1 and ISO 19005-2 and -3, 6.7.4 t1. litho judged
// only the elements its tree walk reached. Here the tree holds two paragraphs and
// reaches no annotation, so these elements are the only source of those rules:
//
//   - A1, a square whose /Annot element carries /Lang (en_US), malformed.
//   - A2, a square whose element carries an empty /Lang, which the PDF/UA
//     grammar fails and the PDF/A rules admit.
//   - A3, the control, a square whose element carries /Lang (de-DE).
//   - A4, the control, a square carrying its own malformed /Lang beside an
//     element with none: veraPDF never reads an annotation's own /Lang.
//
// A form field names its element the same way (GFPDFormField.getLang), and a
// widget that is also a field is read twice. No widget is planted: the
// mutate-pages and mutate-flatten cells treat widgets apart, and the unit test
// TestStructParentLangValidated pins that route.
//
// Symbolic TrueType fonts whose /Encoding dictionary names no base encoding.
// ISO 14289-1, 7.21.6 t3 and its restatements (ISO 19005-1, 6.3.7 t2; ISO
// 19005-2 and -3, 6.2.11.6 t3; ISO 19005-4, 6.2.10.6 t3; ISO 14289-2, 8.4.5.7 t3)
// forbid a symbolic TrueType font an /Encoding entry. veraPDF tests
// "Encoding == null", where Encoding is the name /Encoding names or the
// /BaseEncoding of the dictionary, so both fonts here pass, and litho reported
// both. S1's dictionary lists /Differences only and S2's is empty but for its
// /Type. Both embed the symbolic program of ttcmap_2b.pdf's T4, a single
// Microsoft Symbol (3,0) subtable, and map their codes through /ToUnicode, so no
// other font clause fires. No font here fails the rule, so the rule class stays
// out of both tools' sets, and reporting either font adds it back.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotlangUa1() ([]byte, error) {
	pdf := pdfgen.New()

	const symbolic = 4 // ISO 32000-1, Table 123
	program := ttBuild([]ttSubtable{{3, 0, map[int]string{0xF041: "A", 0xF042: "B"}}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	pdf.MakeIndirect(fontFile)
	toUnicode := toUnicodeCMap(pdf, map[int]int{0x41: 0x41, 0x42: 0x42})

	// symbolicTrueType is an embedded symbolic TrueType font drawing A and B.
	symbolicTrueType := func(name string, encoding *pdfgen.Dict) *pdfgen.Dict {
		descriptor := d(
			"Type", n("FontDescriptor"),
			"FontName", n(name),
			"Flags", symbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", fontFile,
		)
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n(name),
			"FirstChar", 0x41,
			"LastChar", 0x42,
			"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
			"FontDescriptor", pdf.Indirect(descriptor),
			"Encoding", encoding,
			"ToUnicode", toUnicode,
		))
	}

	s1 := symbolicTrueType(
		"LithoSymDiff",
		d("Type", n("Encoding"), "Differences", a(0x41, n("A"), n("B"))),
	)
	s2 := symbolicTrueType("LithoSymEmpty", d("Type", n("Encoding")))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("S1", s1, "S2", s2)),
		"Contents", pdf.Stream([]byte(
			"/P <</MCID 0>> BDC BT /S1 12 Tf 50 750 Td (AB) Tj ET EMC\n"+
				"/P <</MCID 1>> BDC BT /S2 12 Tf 50 730 Td (AB) Tj ET EMC\n",
		)),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))
	var paragraphs []any
	for _, mcid := range []int{0, 1} {
		paragraphs = append(paragraphs, pdf.Indirect(
			d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", mcid),
		))
	}
	document.Set("K", a(paragraphs...))
	structRoot.Set("K", document)

	nums := a(0, a(paragraphs...))
	var annots []any

	appearance := func() *pdfgen.Stream {
		return pdf.Stream([]byte("0 0 0 RG 0 0 20 20 re S\n"),
			"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 20, 20))
	}

	// annotation is a Square annotation naming, by /StructParent key, an
	// /Annot element the tree omits.
	//
	// The element's /P names the /Document, but the /Document's /K never lists
	// it, so only the /ParentTree leads to it.
	annotation := func(key, x int, elementLang *string, extra ...any) {
		annot := d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(x, 600, x+20, 620),
			"F", 4,
			"AP", d("N", appearance()),
			"StructParent", key,
		)
		for i := 0; i < len(extra); i += 2 {
			annot.Set(extra[i].(string), extra[i+1])
		}
		pdf.Indirect(annot)
		element := d(
			"Type", n("StructElem"),
			"S", n("Annot"),
			"P", document,
			"Pg", page,
			"K", d("Type", n("OBJR"), "Obj", annot, "Pg", page),
		)
		if elementLang != nil {
			element.Set("Lang", text(*elementLang))
		}
		nums.Append(key, pdf.Indirect(element))
		annots = append(annots, annot)
	}
	lang := func(s string) *string { return &s }

	// A1 and A2: a malformed and an empty tag on elements outside the tree.
	annotation(1, 50, lang("en_US"), "Contents", text("A1"))
	annotation(2, 100, lang(""), "Contents", text("A2"))
	// A3 and A4: the controls.
	annotation(3, 150, lang("de-DE"), "Contents", text("A3"))
	annotation(4, 200, nil, "Contents", text("A4"), "Lang", text("x_y"))

	page.Set("Annots", a(annots...))
	structRoot.Set("ParentTree", d("Nums", nums))
	structRoot.Set("ParentTreeNextKey", 5)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
