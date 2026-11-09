package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("idfields_ua2", idfieldsUa2) }

// idfieldsUa2 builds idfields_ua2.pdf: identification prefixes, destinations and text fields.
//
// Four ISO 14289-2 rules the coverage scan showed unreached, on four unrelated
// parts of the document, which is what makes the file a masking probe rather than
// four separate documents:
//
// | Construct                                          | Rule          |
// | -------------------------------------------------- | ------------- |
// | `part` bound to a prefix other than `pdfuaid`      | `5/3`         |
// | `rev` bound to a prefix other than `pdfuaid`       | `5/4`         |
// | an outline destination naming a page               | `8.8/1`       |
// | a text field whose `/RV` disagrees with its `/V`   | `8.10.3.3/1`  |
//
// The two identification defects are the load-bearing pair. XMP identifies a
// schema by NAMESPACE, not by prefix, so declaring a second prefix on the PDF/UA
// identification namespace and hanging `part` and `rev` off it keeps the file
// identifiable as PDF/UA-2 while making both prefixes wrong. If it stopped being
// identifiable the other rules would go unevaluated and the file would report
// almost nothing, so those two rules firing alongside the rest is itself the proof
// the trick worked.
//
// `8.8` splits by object: test 1 judges a `PDDestination` and test 2 a
// `PDGoToAction`. Only test 1 is planted here, on an outline item's `/Dest`;
// `annotstruct_ua2.pdf` already covers test 2 through link annotations. Beside it
// sits an outline item whose `/Dest` does name a structure element, which is the
// control.
//
// `8.10.3.3/1` reads `containsRV == false || (V != null && RV == V)`, so a field
// carrying rich text must carry the plain equivalent and the two must agree. The
// control carries both, equal.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func idfieldsUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	appearance := func(w, h int) *pdfgen.Stream {
		ap := pdf.Stream(fmt.Appendf(nil, "0.9 0.9 0.9 rg 0 0 %d %d re f\n", w, h))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, w, h))
		pdf.MakeIndirect(ap)
		return ap
	}

	// 8.10.3.3 t1: /RV present and disagreeing with /V, beside one where they agree.
	const rvBad = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>something else</p></body>"
	const rvOK = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>agreed text</p></body>"

	fieldBad := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 600, 170, 620),
		"F", 4,
		"FT", n("Tx"),
		"T", text("disagreeing"),
		"TU", text("a field whose rich text disagrees"),
		"V", text("the plain text"),
		"RV", text(rvBad),
		"AP", d("N", appearance(120, 20)),
		"StructParent", 1,
	))
	fieldOK := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 560, 170, 580),
		"F", 4,
		"FT", n("Tx"),
		"T", text("agreeing"),
		"TU", text("a field whose rich text agrees"),
		"V", text("agreed text"),
		"RV", text(rvOK),
		"AP", d("N", appearance(120, 20)),
		"StructParent", 2,
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(
			"/P <</MCID 0>> BDC\n"+
				"BT /F1 12 Tf 50 750 Td (identification and destinations) Tj ET\nEMC\n",
		)),
		"Annots", a(fieldBad, fieldOK),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	pdf.Root.Set("AcroForm", d(
		"Fields", a(fieldBad, fieldOK),
		"DA", text("/Helv 0 Tf 0 g"),
		"DR", d("Font", d("Helv", font)),
	))

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

	// formElement is a Form element enclosing one widget, which ISO 14289-2
	// 8.10.1 requires.
	formElement := func(widget *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n("Form"),
			"P", document,
			"NS", ns20,
			"Pg", page,
			"K", a(d("Type", n("OBJR"), "Obj", widget)),
		))
	}

	formBad := formElement(fieldBad)
	formOK := formElement(fieldOK)

	document.Set("K", a(paragraph, formBad, formOK))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(paragraph), 1, formBad, 2, formOK)),
	))
	structRoot.Set("ParentTreeNextKey", 3)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// 8.8 t1: an outline destination naming a page rather than a structure element,
	// beside one that names the Document element and is the control.
	outlineBad := pdf.Indirect(
		d("Title", text("a page destination"), "Dest", a(page, n("Fit"))),
	)
	outlineOK := pdf.Indirect(d(
		"Title", text("a structure destination"), "Dest", a(document, n("Fit")),
	))
	outlineBad.Set("Next", outlineOK)
	outlineOK.Set("Prev", outlineBad)
	outlines := pdf.Indirect(
		d("Type", n("Outlines"), "First", outlineBad, "Last", outlineOK, "Count", 2),
	)
	outlineBad.Set("Parent", outlines)
	outlineOK.Set("Parent", outlines)
	pdf.Root.Set("Outlines", outlines)

	// 5 t3 and t4: a second prefix declared on the PDF/UA identification namespace,
	// with part and rev hanging off it. The namespace is what identifies the schema,
	// so the file stays a PDF/UA-2 file while both prefixes are wrong.
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
    xmlns:ua="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">identification prefixes</rdf:li></rdf:Alt></dc:title>
   <ua:part>2</ua:part>
   <ua:rev>2024</ua:rev>
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
