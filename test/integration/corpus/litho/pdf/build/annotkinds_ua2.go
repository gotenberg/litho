package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("annotkinds_ua2", annotkindsUa2) }

// annotkindsUa2 builds annotkinds_ua2.pdf: annotation subtypes under PDF/UA-2.
//
// The coverage scan showed ISO 14289-2 8.9.2.4 almost wholly unreached: it judges
// annotations subtype by subtype, and each of its tests is scoped to one veraPDF
// object, so a file reaches them only by carrying that exact annotation kind.
//
// Eleven annotations sit on one page, eight defective and three conforming, which
// is the masking probe: the clause splits one idea, "an annotation must be
// describable or be an artifact", across nine independent tests, so a check that
// reports the first offending annotation and stops reports one rule where eight
// are due, and a check that condemns every annotation of a kind fires on the
// conforming neighbours.
//
// | Annotation                             | Rule           |
// | -------------------------------------- | -------------- |
// | `Sound`, any                           | `8.9.2.4.11/1` |
// | `TrapNet`, any                         | `8.9.2.4.15/1` |
// | `Screen` with no `/Contents`           | `8.9.2.4.12/1` |
// | `3D` with no `/Contents`               | `8.9.2.4.19/1` |
// | `RichMedia` with no `/Contents`        | `8.9.2.4.19/2` |
// | `Ink` with no `/Contents`              | `8.9.2.4.8/1`  |
// | `Stamp` with neither `/Name` nor `/Contents` | `8.9.2.4.7/1` |
// | `PrinterMark` inside the structure tree | `8.9.2.4.14/1` |
// | `Screen` with `/Contents`              | silent         |
// | `Ink` with `/Contents`                 | silent         |
// | `Stamp` with `/Name`                   | silent         |
//
// Two of the eleven need no defect at all. `Sound` and `TrapNet` are forbidden
// outright, so their tests read simply `false`: the annotation's presence is the
// violation. They are the reason the file cannot be reduced to "annotations
// missing a description".
//
// `PrinterMark` is the opposite shape. Its rule is satisfied when the annotation
// is outside the structure tree or marked as an artifact, so reaching it means
// putting the annotation *into* the tree, wired the way a real one is: a
// `/StructParent` on the annotation, an `/OBJR` in the enclosing element's `/K`,
// and a `ParentTree` entry mapping the number back. Get that wiring wrong and the
// annotation looks unparented, which silences the test rather than firing it.
//
// The three controls are each the conforming twin of a defect beside them, so a
// firing against any of them is over-suppression rather than a missing case. The
// `Stamp` control uses `/Name` rather than `/Contents` deliberately: the test
// accepts either, and only the `/Name` branch is otherwise unexercised.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotkindsUa2() ([]byte, error) {
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
		"Contents", pdf.Stream([]byte(
			"/P <</MCID 0>> BDC\n"+
				"BT /F1 12 Tf 50 740 Td (annotation subtypes) Tj ET\nEMC\n",
		)),
		"StructParents", 0,
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

	appearance := func(w, h int) *pdfgen.Stream {
		s := pdf.Stream([]byte(fmt.Sprintf("0 0 0 rg 0 0 %d %d re f\n", w, h)))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, w, h))
		return pdf.MakeIndirect(s).(*pdfgen.Stream)
	}

	const printFlag = 4
	annots := a()

	// annot is one annotation carrying a print flag and an appearance stream.
	annot := func(subtype string, y int, extra ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", n("/"+subtype),
			"Rect", a(50, y, 70, y+20),
			"F", printFlag,
			"AP", d("N", appearance(20, 20)),
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		made := pdf.Indirect(dd)
		annots.Append(made)
		return made
	}

	// Forbidden outright: the presence of either annotation is the violation.
	annot("Sound", 700)
	annot("TrapNet", 670)

	// Missing the description each subtype requires.
	annot("Screen", 640)
	annot("3D", 610)
	annot("RichMedia", 580)
	annot("Ink", 550, "InkList", a(a(50, 550, 70, 570)))

	// A rubber stamp offering neither of the two entries its test accepts.
	annot("Stamp", 520)

	// The conforming twins. Each is the same subtype as a defect above, so a firing
	// here is over-suppression rather than a missing case.
	annot("Screen", 490, "Contents", text("a described screen annotation"))
	annot("Ink", 460, "Contents", text("a described ink annotation"),
		"InkList", a(a(50, 460, 70, 480)))
	annot("Stamp", 430, "Name", n("Approved"))

	// A printer's mark must be an artifact or sit outside the structure tree. This
	// one is inside it, which is what the test judges, so it needs the full wiring:
	// /StructParent here, an /OBJR below, and a ParentTree entry mapping it back.
	printerMark := annot("PrinterMark", 400, "StructParent", 1)

	paragraph := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", 0),
	)
	markHolder := pdf.Indirect(
		d(
			"Type", n("StructElem"),
			"S", n("P"),
			"P", document,
			"NS", ns20,
			"Pg", page,
			"K", a(d("Type", n("OBJR"), "Obj", printerMark)),
		),
	)

	page.Set("Annots", annots)

	document.Set("K", a(paragraph, markHolder))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(paragraph), 1, markHolder)),
	))
	structRoot.Set("ParentTreeNextKey", 2)

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
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">annotation subtypes</rdf:li></rdf:Alt></dc:title>
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
