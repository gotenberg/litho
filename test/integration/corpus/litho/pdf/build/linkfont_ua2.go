package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("linkfont_ua2", linkfontUa2) }

// linkfontUa2 builds linkfont_ua2.pdf: link targets and a symbolic font's character map.
//
// Two ISO 14289-2 rules the coverage scan showed unreached, on unrelated parts of
// the document:
//
// | Construct                                              | Rule           |
// | ------------------------------------------------------ | -------------- |
// | two links under one `Link` element onto different targets | `8.2.5.20/2` |
// | a symbolic TrueType with neither a (3,0) nor a (1,0) `cmap` | `8.4.5.7/4` |
//
// `8.2.5.20/2` is the one worth the file. veraPDF only compares link targets that
// are STRUCTURE destinations, and it compares them by object identity alone: a
// destination array's first element must be a dictionary carrying `/S`, and the
// view parameters that follow it are ignored. A page destination, a URI action, or
// a destination naming no structure element leaves nothing to compare and the rule
// is never evaluated at all.
//
// So the file carries three groups, and only the first offends:
//
//	bad   two links under one `Link` element, with `/SD` naming two DIFFERENT
//	      structure elements. Fires.
//	same  two links under one `Link` element, both naming the SAME structure
//	      element but with different view parameters. Silent, and it is the
//	      control that matters most: comparing whole destination arrays rather
//	      than target identity reports this pair as disagreeing.
//	page  two links under one `Link` element with ordinary page destinations,
//	      differing from each other. Silent, because a page destination is not a
//	      structure destination and never reaches the comparison.
//
// `8.4.5.7/4` reads `isSymbolic == false || cmap30Present == true || cmap10Present
// == true`, so it needs a font that is symbolic AND carries neither subtable. The
// program here holds a (3,1) subtable only. Beside it sits the same program
// declared non-symbolic, which the first branch excuses, so the discriminator is
// the flag rather than the character map.
//
// That symbolic font is painted in rendering mode 3, and the reason is a divergence
// rather than a preference. A symbolic TrueType with neither subtable cannot map
// its codes at all, so veraPDF reports every code it paints as a glyph that is not
// present (`8.4.5.5.1/2`); litho leaves a code it cannot map unchecked, on the
// ground that presence is then unresolvable, and reports nothing. Mode 3 exempts
// the glyphs on both sides, which keeps this file about the character map while
// that question stays open. It is a real litho false negative, recorded rather
// than planted: reaching it needs the font painted visibly, and the fix means
// turning an unresolvable code into a firing one, which is the direction that has
// produced false positives on real-world corpora before and wants a wide sweep
// first.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func linkfontUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const symbolic = 4
	const nonsymbolic = 32
	latin := map[int]string{0x41: "A", 0x42: "B"}

	helvetica := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// truetype is a simple TrueType whose program carries a (3,1) subtable
	// and no other.
	//
	// A non-symbolic font needs an /Encoding for its codes to have a defined
	// meaning; a symbolic one must not declare one, so the caller chooses.
	truetype := func(name string, flags int, encoding string) *pdfgen.Dict {
		program := ttBuild([]ttSubtable{{3, 1, latin}}, nil)
		fontFile := pdf.Stream(program)
		fontFile.Set("Length1", len(program))
		descriptor := pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n(name),
			"Flags", flags,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		))
		font := d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n(name),
			"FirstChar", 0x41,
			"LastChar", 0x42,
			"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
			"FontDescriptor", descriptor,
		)
		if encoding != "" {
			font.Set("Encoding", n(encoding))
		}
		return pdf.Indirect(font)
	}

	// 8.4.5.7 t4: symbolic, and the program carries neither (3,0) nor (1,0). The
	// non-symbolic twin beside it is excused by the test's first branch.
	fontSymbolic := truetype("LithoSymNoCmap", symbolic, "")
	fontPlain := truetype("LithoPlainCmap", nonsymbolic, "WinAnsiEncoding")

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", helvetica, "TS", fontSymbolic, "TP", fontPlain),
		),
		"Contents", pdf.Stream([]byte(
			"/P <</MCID 0>> BDC\n"+
				"BT /F1 12 Tf 50 750 Td (link targets and character maps) Tj ET\n"+
				"BT 3 Tr /TS 12 Tf 50 720 Td (AB) Tj ET\n"+
				"BT /TP 12 Tf 50 690 Td (AB) Tj ET\nEMC\n",
		)),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot"), "Namespaces", a(ns20)))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ns20,
		"Lang", text("en-US"),
	))
	paragraph := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", 0),
	)

	// The two structure elements a structure destination can name. They hold no
	// content of their own; only their identity matters to the comparison.
	targetA := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Sect"), "P", document, "NS", ns20, "Pg", page),
	)
	targetB := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Sect"), "P", document, "NS", ns20, "Pg", page),
	)

	var annots []any
	docKids := []any{paragraph, targetA, targetB}
	nums := []any{0, a(paragraph)}
	nextKey := 1

	appearance := func() *pdfgen.Stream {
		ap := pdf.Stream([]byte("0 0 1 RG 0 0 40 20 re S\n"))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 40, 20))
		pdf.MakeIndirect(ap)
		return ap
	}

	// link is a Link annotation whose GoTo action names dest.
	//
	// A structure destination rides on /SD, which ISO 32000-2 12.6.4.2 gives
	// precedence over /D; an ordinary page destination rides on /D.
	link := func(y int, dest *pdfgen.Array, structured bool) *pdfgen.Dict {
		action := d("S", n("GoTo"))
		if structured {
			action.Set("SD", dest)
		} else {
			action.Set("D", dest)
		}
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

	// linkGroup wires a set of Link annotations under one shared Link
	// element.
	linkGroup := func(links []*pdfgen.Dict) *pdfgen.Dict {
		k := a()
		for _, lk := range links {
			k.Append(d("Type", n("OBJR"), "Obj", lk))
		}
		element := pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n("Link"),
			"P", document,
			"NS", ns20,
			"Pg", page,
			"K", k,
		))
		for _, lk := range links {
			lk.Set("StructParent", nextKey)
			nums = append(nums, nextKey, element)
			nextKey++
			annots = append(annots, lk)
		}
		docKids = append(docKids, element)
		return element
	}

	// The defect: two structure destinations naming different elements.
	linkGroup([]*pdfgen.Dict{
		link(650, a(targetA, n("Fit")), true),
		link(620, a(targetB, n("Fit")), true),
	})

	// The control that matters: the same element, different view parameters. A
	// comparison that reads the whole destination array reports these as disagreeing.
	linkGroup([]*pdfgen.Dict{
		link(590, a(targetA, n("Fit")), true),
		link(560, a(targetA, n("XYZ"), 0, 700, 0), true),
	})

	// The second control: ordinary page destinations, differing from each other.
	// They are not structure destinations, so they never reach the comparison.
	linkGroup([]*pdfgen.Dict{
		link(530, a(page, n("Fit")), false),
		link(500, a(page, n("XYZ"), 0, 700, 0), false),
	})

	page.Set("Annots", a(annots...))

	document.Set("K", a(docKids...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(nums...))))
	structRoot.Set("ParentTreeNextKey", nextKey)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">link targets and character maps</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>2</pdfuaid:part>
   <pdfuaid:rev>2024</pdfuaid:rev>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "2.0")
}
