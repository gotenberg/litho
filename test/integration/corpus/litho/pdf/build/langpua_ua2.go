package build

import (
	"unicode/utf16"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("langpua_ua2", langpuaUa2) }

// langpuaUa2 builds langpua_ua2.pdf: Private Use Area characters in language entries.
//
// ISO 14289-2, 8.6 t1 forbids a Private Use Area code point in a text string a
// consumer reads out. veraPDF scopes it to CosTextString, whose instances are an
// outline item's `/Title` (puatext_ua2.pdf covers it) and every CosLang: the
// catalog `/Lang`, a structure element's `/Lang`, the `/Lang` of the element an
// annotation names by `/StructParent`, and the `/Lang` of a marked-content
// property list. This file carries the two a document states most often and no
// other corpus file reaches:
//
// | Construct                                  | Rule    |
// | ------------------------------------------ | ------- |
// | `/Lang` on a structure element             | `8.6/1` |
// | `/Lang` on a BDC property list             | `8.6/1` |
//
// Each sits beside a twin whose `/Lang` is a plain tag. A PUA character is no
// letter, so both defects also break the Language-Tag grammar and fire ISO
// 14289-2, 8.4.4 t2, ISO 14289-1, 7.2 t29, 6.8.4 t1 at PDF/A-1a and 6.7.4 t1 at
// PDF/A-2a and 3a.
//
// The control is the annotation's own `/Lang`, which carries the same PUA
// character. It is no CosLang: veraPDF reads an annotation's language from the
// element its `/StructParent` names (GFPDAnnot.getLang), never from the
// annotation dictionary, so neither rule may fire on it. The Annot element
// enclosing it states no `/Lang`.
//
// U+E000 is the first Private Use Area code point. It rides in a UTF-16BE string
// with a byte order mark, which is how a PDF text string carries anything outside
// PDFDocEncoding.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func langpuaUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const pua = ""
	const puaHex = "<FEFF0065006EE000>"

	// utf16Text is a PDF text string, UTF-16BE with a byte order mark.
	utf16Text := func(value string) pdfgen.String {
		out := []byte{0xfe, 0xff}
		for _, u := range utf16.Encode([]rune(value)) {
			out = append(out, byte(u>>8), byte(u)) //nolint:gosec // the two bytes of a UTF-16 unit.
		}
		return pdfgen.String(out)
	}

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
			// 8.6 t1: the marked-content /Lang carries the PUA character.
			"/P <</MCID 0 /Lang "+puaHex+">> BDC\n"+
				"BT /F1 12 Tf 50 750 Td (marked text) Tj ET\nEMC\n"+
				// The control: a plain tag on the same kind of property list.
				"/P <</MCID 1 /Lang (en-GB)>> BDC\n"+
				"BT /F1 12 Tf 50 720 Td (more marked text) Tj ET\nEMC\n"+
				"/P <</MCID 2>> BDC\n"+
				"BT /F1 12 Tf 50 690 Td (element text) Tj ET\nEMC\n"+
				"/P <</MCID 3>> BDC\n"+
				"BT /F1 12 Tf 50 660 Td (more element text) Tj ET\nEMC\n",
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

	// paragraph is a P element over one marked-content sequence, carrying
	// extra keys.
	paragraph := func(mcid int, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("P"), "P", document, "NS", ns20, "Pg", page, "K", mcid)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	bdcBad := paragraph(0)
	bdcOK := paragraph(1)
	// 8.6 t1: the structure element's /Lang carries the PUA character.
	elemBad := paragraph(2, "Lang", utf16Text("en"+pua))
	// The control.
	elemOK := paragraph(3, "Lang", text("en-GB"))

	// The must-not-fire control: the same character in an annotation's own /Lang,
	// which no CosLang reads. The Annot element enclosing it states no /Lang.
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Text"),
		"Rect", a(50, 600, 70, 620),
		"Contents", text("a note"),
		"Lang", utf16Text("en"+pua),
		"F", 4,
		"StructParent", 1,
	))
	page.Set("Annots", a(annot))
	annotElem := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Annot"),
		"P", document,
		"NS", ns20,
		"Pg", page,
		"K", d("Type", n("OBJR"), "Obj", annot, "Pg", page),
	))

	kids := []any{bdcBad, bdcOK, elemBad, elemOK}
	document.Set("K", a(append(kids, annotElem)...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(kids...), 1, annotElem)),
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
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">private use languages</rdf:li></rdf:Alt></dc:title>
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
