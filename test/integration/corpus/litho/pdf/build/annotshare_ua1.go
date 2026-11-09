package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("annotshare_ua1", annotshareUa1) }

// annotshareUa1 builds annotshare_ua1.pdf: annotation objects two pages list, identification
// declared on its own element, and page boundaries short of four numbers.
//
// veraPDF judges an annotation object once, on the first page listing it, however
// many pages list it (its validator skips an object key it has met). litho judged
// it once per listing, so every per-annotation rule counted twice. Page 1 lists
// four annotation objects, the Square twice, and page 2 lists the Link, the Text
// and the Widget again:
//
//   - a Link without /Contents outside the structure tree: 7.18.5 t1 and t2,
//     7.18.1 t2, once each;
//   - a Text annotation with no flags, /Contents or appearance: 7.18.1 t1 and t2,
//     and the part-2 flag and appearance tests, once each;
//   - a text field Widget with no /TU and no appearance: 7.18.1 t3, 7.18.4 t1;
//   - a Square under an Annot element whose /Alt differs from its /Contents:
//     ISO 14289-2, 8.9.4.2 t1 once, though page 1 lists it twice.
//
// The isOutsideCropBox exemption of ISO 14289-1, 7.18 reads the crop box veraPDF
// reads (PDPage.getCropBox): page 2 inherits its media box from the page tree and
// lists a Link lying beyond it, and page 3 states a crop box of three numbers,
// [0 0 300], whose missing corner reads as 0, so the Link it lists lies above a
// box of no height. Both Links are exempt. litho read the page dictionary alone
// and held both to the rules.
//
// Page 3 also states an art box of [0 0 400 null]. veraPDF reads a missing corner
// and one that is no number as 0 (GFCosBBox.getRequiredValue), so the crop and art
// boxes both fail ISO 19005-2 and -3, 6.1.13 t11, which litho skipped.
//
// The XMP packet declares every namespace on the property element that uses it,
// as pikepdf's metadata API writes it: pdfuaid:part 1, pdfaid:part 2 and
// conformance B, dc:title, and a PDF/A extension schema declaring the pdfuaid
// namespace, every element of which declares its own namespace too. litho read
// pdfuaid:part through a literal "pdfuaid:part>1<", which met only the closing
// tag, and reported ISO 14289-1, 5 t2 on a packet claiming part 1.
//
// No text is drawn, so the pages read the same under every mutation.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annotshareUa1() ([]byte, error) {
	const (
		pdfuaidNS  = "http://www.aiim.org/pdfua/ns/id/"
		pdfaidNS   = "http://www.aiim.org/pdfa/ns/id/"
		extNS      = "http://www.aiim.org/pdfa/ns/extension/"
		schemaNS   = "http://www.aiim.org/pdfa/ns/schema#"
		propertyNS = "http://www.aiim.org/pdfa/ns/property#"
		dcNS       = "http://purl.org/dc/elements/1.1/"
	)

	// el is an element declaring its own namespace, as pikepdf writes one.
	el := func(prefix, uri, local, inner string) string {
		return fmt.Sprintf(`<%s:%s xmlns:%s="%s">%s</%s:%s>`, prefix, local, prefix, uri, inner, prefix, local)
	}

	extensionSchema := func() string {
		prop := `<rdf:li rdf:parseType="Resource">` +
			el("pdfaProperty", propertyNS, "name", "part") +
			el("pdfaProperty", propertyNS, "valueType", "Integer") +
			el("pdfaProperty", propertyNS, "category", "internal") +
			el("pdfaProperty", propertyNS, "description", "The part of ISO 14289 the file conforms to") +
			"</rdf:li>"
		schema := `<rdf:li rdf:parseType="Resource">` +
			el("pdfaSchema", schemaNS, "schema", "PDF/UA identification schema") +
			el("pdfaSchema", schemaNS, "namespaceURI", pdfuaidNS) +
			el("pdfaSchema", schemaNS, "prefix", "pdfuaid") +
			el("pdfaSchema", schemaNS, "property", "<rdf:Seq>"+prop+"</rdf:Seq>") +
			"</rdf:li>"
		return el("pdfaExtension", extNS, "schemas", "<rdf:Bag>"+schema+"</rdf:Bag>")
	}

	packet := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\" x:xmptk=\"pikepdf\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		" <rdf:Description rdf:about=\"\">" +
		el("pdfuaid", pdfuaidNS, "part", "1") +
		el("pdfaid", pdfaidNS, "part", "2") +
		el("pdfaid", pdfaidNS, "conformance", "B") +
		"</rdf:Description><rdf:Description rdf:about=\"\">" +
		el("dc", dcNS, "title", `<rdf:Alt><rdf:li xml:lang="x-default">litho corpus fixture</rdf:li></rdf:Alt>`) +
		"</rdf:Description><rdf:Description rdf:about=\"\">" +
		extensionSchema() +
		"</rdf:Description></rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>\n")

	pdf := pdfgen.New()

	tree := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", tree))
	annotElem := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Annot"), "P", document, "Alt", text("another description")),
	)

	link := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(50, 700, 200, 720),
		"F", 4,
		"Border", a(0, 0, 0),
		"A", d("S", n("URI"), "URI", text("https://example.com/")),
	))
	textAnnot := pdf.Indirect(d("Type", n("Annot"), "Subtype", n("Text"), "Rect", a(50, 650, 70, 670)))
	widget := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"FT", n("Tx"),
		"T", text("name"),
		"Rect", a(50, 600, 200, 620),
		"F", 4,
	))
	square := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(50, 550, 100, 580),
		"F", 4,
		"Contents", text("a square"),
		"StructParent", 0,
	))
	offPage := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(700, 700, 800, 720),
		"F", 4,
		"Border", a(0, 0, 0),
		"A", d("S", n("URI"), "URI", text("https://example.com/off")),
	))
	aboveCrop := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(50, 700, 200, 720),
		"F", 4,
		"Border", a(0, 0, 0),
		"A", d("S", n("URI"), "URI", text("https://example.com/above")),
	))

	annotElem.Set("K", d("Type", n("OBJR"), "Obj", square))
	document.Set("K", a(annotElem))
	tree.Set("K", document)
	tree.Set("ParentTree", pdf.Indirect(d("Nums", a(0, annotElem))))
	tree.Set("ParentTreeNextKey", 1)

	page := func(entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Page"), "Resources", d(), "Contents", pdf.Stream([]byte{}), "Tabs", n("S"))
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	page1 := page("MediaBox", a(0, 0, 612, 792), "Annots", a(link, textAnnot, widget, square, square))
	page2 := page("Annots", a(link, textAnnot, widget, offPage))
	page3 := page(
		"MediaBox", a(0, 0, 612, 792),
		"CropBox", a(0, 0, 300),
		"ArtBox", a(0, 0, 400, nil),
		"Annots", a(aboveCrop),
	)

	pages := pdf.Pages
	pages.Set("MediaBox", a(0, 0, 612, 792))
	for _, p := range []*pdfgen.Dict{page1, page2, page3} {
		p.Set("Parent", pages)
	}
	pages.Set("Kids", a(page1, page2, page3))
	pages.Set("Count", 3)

	widget.Set("P", page1)
	pdf.Root.Set("AcroForm", d("Fields", a(widget)))
	pdf.Root.Set("StructTreeRoot", tree)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, packet)

	return save(pdf, "1.7", noFixMetadataVersion, noPreservePDFA)
}
