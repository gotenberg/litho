package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("annotstruct_ua2", annotstructUa2) }

// annotstructUa2 builds annotstruct_ua2.pdf: annotations judged by their place in the tree.
//
// `annotkinds_ua2.pdf` covers the annotation rules that turn on the subtype alone.
// This file covers the rest of ISO 14289-2 clause 8.9, which turn on where the
// annotation sits: almost every test here is satisfied by `structParentType ==
// null`, so an annotation only reaches the test by being wired into the structure
// tree, and reaching a test is the hard part rather than failing it.
//
// Wiring means three things agreeing: a `/StructParent` on the annotation, an
// `/OBJR` in the enclosing element's `/K`, and a `ParentTree` entry mapping the
// number back to that element. Miss one and the annotation reads as unparented,
// which silences the test rather than firing it.
//
// | Annotation                                        | Rule            |
// | ------------------------------------------------- | --------------- |
// | `Link` under a `/P`                               | `8.2.5.20/1`    |
// | `NoView` set without `ToggleNoView`               | `8.9.2.2/2`     |
// | `Square` under a `/P`                             | `8.9.2.3/1`     |
// | `Square` whose `/RC` differs from `/Contents`     | `8.9.2.3/2`     |
// | `Popup` inside the tree                           | `8.9.2.4.9/1`   |
// | `FileAttachment` with `/FS`, no `/AFRelationship` | `8.9.2.4.10/1`  |
// | zero-area `Widget` inside the tree                | `8.9.2.4.13/1`  |
// | `Watermark` under a `/P`                          | `8.9.2.4.16/1`  |
// | `/Contents` against the element's differing `/Alt` | `8.9.4.2/1`    |
// | a field widget with `/AA` and no `/Contents`      | `8.10.2.3/2`    |
//
// Two of these are placed where measurement put them rather than where the entry
// name suggests. `/AFRelationship` is a file specification entry, so an
// annotation-level one leaves `8.9.2.4.10/1` firing and does not make a control.
// `8.9.4.2/1` compares the annotation's `/Contents` with the `/Alt` of the element
// enclosing it, so an annotation-level `/Alt` never reaches the test at all.
//
// The enclosing element's type is the discriminator for four of these, so the file
// carries elements of several types on purpose: a `/Link` element, an `/Annot`
// element and plain `/P` elements. An annotation moved between them changes verdict
// without changing one byte of the annotation itself, which is why a check that
// reads only the annotation dictionary cannot pass this file.
//
// Controls sit beside the defects wherever the test turns on an entry rather than
// on position: a `Link` under a `/Link` element, a `Square` under an `/Annot`
// element, a `Square` whose `/RC` matches its `/Contents`, a `FileAttachment`
// carrying `/AFRelationship`, an annotation whose `/Alt` equals its `/Contents`,
// a full-area widget, and a `Popup` left outside the tree. veraPDF reports a rule
// once however many annotations break it, so rule-set agreement alone cannot see
// these; the per-rule failed-check counts are what prove them.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotstructUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

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
		"Contents", pdf.Stream([]byte("/P <</MCID 0>> BDC\n"+
			"BT /F1 12 Tf 50 750 Td (annotations in the structure tree) Tj ET\nEMC\n")),
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

	paragraph := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", 0))

	var annots []any
	docKids := []any{paragraph}
	// ParentTree keys start after the page's own StructParents of 0.
	nums := []any{0, a(paragraph)}
	nextKey := 1

	appearance := func(w, h int) pdfgen.Object {
		s := pdf.Stream([]byte(fmt.Sprintf("0 0 0 rg 0 0 %d %d re f\n", max(w, 1), max(h, 1))))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, max(w, 1), max(h, 1)))
		return pdf.MakeIndirect(s)
	}

	// annot is one annotation carrying a print flag and an appearance stream.
	annot := func(subtype string, rect []int, extra ...any) *pdfgen.Dict {
		w := rect[2] - rect[0]
		h := rect[3] - rect[1]
		dd := d(
			"Type", n("Annot"),
			"Subtype", n(subtype),
			"Rect", a(rect[0], rect[1], rect[2], rect[3]),
			"F", 4,
			"AP", d("N", appearance(w, h)),
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		made := pdf.Indirect(dd)
		annots = append(annots, made)
		return made
	}

	// enclose wires target into the tree under a new element of the given type.
	//
	// The three parts have to agree: the /StructParent written here, the /OBJR in
	// the element's /K, and the ParentTree entry mapping the number back. Extra
	// keys land on the element rather than the annotation, which is where /Alt
	// belongs.
	enclose := func(kind string, target *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		key := nextKey
		nextKey++
		fields := d(
			"Type", n("StructElem"),
			"S", n(kind),
			"P", document,
			"NS", ns20,
			"Pg", page,
			"K", a(d("Type", n("OBJR"), "Obj", target)),
		)
		for i := 0; i < len(extra); i += 2 {
			fields.Set(extra[i].(string), extra[i+1])
		}
		element := pdf.Indirect(fields)
		target.Set("StructParent", key)
		docKids = append(docKids, element)
		nums = append(nums, key, element)
		return element
	}

	// y gives row coordinates, laid out downward so nothing overlaps.
	y := func(row int) []int {
		top := 720 - row*30
		return []int{50, top, 90, top + 20}
	}

	// 8.2.5.20 t1: a Link whose enclosing element is neither Link nor Reference.
	linkBad := annot("Link", y(0), "A", d("S", n("GoTo"), "D", a(page, n("Fit"))))
	enclose("P", linkBad)

	// The control: the same annotation under a /Link element.
	linkOK := annot("Link", y(1), "A", d("S", n("GoTo"), "D", a(page, n("Fit"))))
	enclose("Link", linkOK)

	// 8.2.5.20 t2: two Links under one /Link element aiming at different targets.
	linkPairA := annot("Link", y(2), "A", d("S", n("GoTo"), "D", a(page, n("Fit"))))
	linkPairB := annot("Link", y(3), "A", d("S", n("GoTo"), "D", a(page, n("XYZ"), 0, 700, 0)))
	pairElement := enclose("Link", linkPairA)
	linkPairB.Set("StructParent", nextKey)
	pairElement.Get("K").(*pdfgen.Array).Append(d("Type", n("OBJR"), "Obj", linkPairB))
	nums = append(nums, nextKey, pairElement)
	nextKey++

	// 8.9.2.2 t2: NoView set, ToggleNoView clear. Print | NoView is 4 | 32.
	noview := annot("Square", y(4), "F", 4|32, "Contents", text("a hidden square"))
	enclose("Annot", noview)

	// The control: the same flags plus ToggleNoView, which the test permits.
	noviewOK := annot("Square", y(5), "F", 4|32|256, "Contents", text("a toggling square"))
	enclose("Annot", noviewOK)

	// 8.9.2.3 t1: a markup annotation whose enclosing element is not /Annot.
	markupBad := annot("Square", y(6), "Contents", text("a misplaced square"))
	enclose("P", markupBad)

	// 8.9.2.3 t2: /RC present and disagreeing with /Contents.
	const rcHTML = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'>" +
		"<p>something else entirely</p></body>"
	rcBad := annot("Square", y(7), "Contents", text("the plain text"), "RC", text(rcHTML))
	enclose("Annot", rcBad)

	// The control: /RC whose text matches /Contents.
	rcOK := annot(
		"Square",
		y(8),
		"Contents", text("agreeing text"),
		"RC", text("<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'>"+
			"<p>agreeing text</p></body>"),
	)
	enclose("Annot", rcOK)

	// 8.9.2.4.9 t1: a Popup inside the tree. Its parent markup annotation is the
	// one carrying the /RC control above, so the pair is well formed apart from the
	// structure membership the test judges.
	popupBad := annot("Popup", y(9), "Parent", rcOK, "Open", false)
	enclose("Annot", popupBad)

	// The control: a Popup left outside the tree entirely, which the test permits.
	annot("Popup", y(10), "Parent", markupBad, "Open", false)

	// 8.9.2.4.10 t1: a file attachment carrying /FS but no /AFRelationship.
	embeddedStream := pdf.Stream([]byte("attachment payload\n"))
	embeddedStream.Set("Type", n("EmbeddedFile"))
	embeddedStream.Set("Subtype", n("/text#2Fplain"))
	embedded := pdf.MakeIndirect(embeddedStream)

	// filespec is a file specification, optionally declaring how the file relates.
	//
	// ISO 32000-2 defines /AFRelationship on the file specification dictionary,
	// not on the annotation, and veraPDF reads it from there: an annotation-level
	// /AFRelationship leaves the test firing.
	filespec := func(name string, relationship any) *pdfgen.Dict {
		dd := d(
			"Type", n("Filespec"),
			"F", text(name),
			"UF", text(name),
			"EF", d("F", embedded),
		)
		if relationship != nil {
			dd.Set("AFRelationship", relationship)
		}
		return pdf.Indirect(dd)
	}

	attachBad := annot("FileAttachment", y(11), "FS", filespec("plain.txt", nil), "Contents", text("an attachment"))
	enclose("Annot", attachBad)

	// The control: the same annotation whose file spec declares the relationship.
	attachOK := annot(
		"FileAttachment",
		y(12),
		"FS", filespec("related.txt", n("Supplement")),
		"Contents", text("a described attachment"),
	)
	enclose("Annot", attachOK)

	// 8.9.2.4.13 t1: a widget of zero width and zero height inside the tree.
	zeroWidget := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 380, 50, 380),
		"F", 4,
		"FT", n("Btn"),
		"T", text("zeroarea"),
		"Contents", text("a zero area widget"),
		"AP", d("N", appearance(1, 1)),
	))
	annots = append(annots, zeroWidget)
	enclose("Form", zeroWidget)

	// 8.10.2.3 t2: a field widget with additional actions and no /Contents.
	aaWidget := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 340, 110, 360),
		"F", 4,
		"FT", n("Btn"),
		"T", text("acts"),
		"AA", d("U", d("S", n("Named"), "N", n("NextPage"))),
		"AP", d("N", appearance(60, 20)),
	))
	annots = append(annots, aaWidget)
	enclose("Form", aaWidget)

	// The control: additional actions beside a /Contents that describes them.
	aaWidgetOK := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 300, 110, 320),
		"F", 4,
		"FT", n("Btn"),
		"T", text("describedacts"),
		"Contents", text("a described acting widget"),
		"AA", d("U", d("S", n("Named"), "N", n("NextPage"))),
		"AP", d("N", appearance(60, 20)),
	))
	annots = append(annots, aaWidgetOK)
	enclose("Form", aaWidgetOK)

	// 8.9.2.4.16 t1: a watermark whose enclosing element is not /Annot.
	watermark := annot("Watermark", y(15), "Contents", text("a watermark"))
	enclose("P", watermark)

	// 8.9.4.2 t1: the annotation's /Contents against the /Alt of the element that
	// encloses it. The two describe the same thing to different consumers, so they
	// have to agree. /Alt rides on the structure element, not on the annotation; an
	// annotation-level /Alt is invisible to the test.
	altBad := annot("Square", y(16), "Contents", text("one description"))
	enclose("Annot", altBad, "Alt", text("a different description"))

	// The control: the two agreeing exactly.
	altOK := annot("Square", y(17), "Contents", text("same words"))
	enclose("Annot", altOK, "Alt", text("same words"))

	page.Set("Annots", a(annots...))
	pdf.Root.Set("AcroForm", d(
		"Fields", a(zeroWidget, aaWidget, aaWidgetOK),
		"DA", text("/Helv 0 Tf 0 g"),
		"DR", d("Font", d("Helv", font)),
	))

	document.Set("K", a(docKids...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(nums...))))
	structRoot.Set("ParentTreeNextKey", nextKey)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	const xmp = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">annotations in the tree</rdf:li></rdf:Alt></dc:title>
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
