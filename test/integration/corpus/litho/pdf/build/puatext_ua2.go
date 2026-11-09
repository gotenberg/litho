package build

import (
	"unicode/utf16"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("puatext_ua2", puatextUa2) }

// puatextUa2 builds puatext_ua2.pdf: Private Use Area characters in the strings that describe.
//
// ISO 14289-2 forbids a Private Use Area code point in text a consumer reads out,
// because nothing outside the producing system knows what it means. The clause is
// split across three veraPDF objects, so a file reaches each one only by putting a
// PUA character in that exact position:
//
// | Construct                                | Rule       |
// | ---------------------------------------- | ---------- |
// | `/ActualText` on a structure element     | `8.4.3/2`  |
// | `/Alt` on a structure element            | `8.4.3/3`  |
// | an outline item `/Title`                 | `8.6/1`    |
//
// The same `/ActualText` also answers ISO 19005-4 `6.2.10.8/1`, which is the PDF/A-4
// spelling of the first row.
//
// Three defects on three independent objects, which is the masking probe: a check
// that reports the first PUA it finds and stops reports one rule where three are
// due. Each sits beside a conforming twin carrying the same words without the PUA
// character, so a check that condemns every `/ActualText`, every `/Alt` or every
// title fires on the control.
//
// Where the PUA character goes was measured rather than assumed. `CosTextString`
// is the loosest-sounding of the three objects, but a document information `/Title`
// and an annotation `/Contents` were both silent; an outline item's `/Title` is
// what reaches it. The controls prove the same positions, so a silent result there
// means the value passed rather than the position going unread.
//
// U+E000 is the first Private Use Area code point. It rides in a UTF-16BE string
// with a byte order mark, which is how a PDF text string carries anything outside
// PDFDocEncoding.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func puatextUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const pua = ""

	// utf16Text is a PDF text string, UTF-16BE with a byte order mark.
	utf16Text := func(value string) pdfgen.String {
		units := utf16.Encode([]rune(value))
		out := make([]byte, 0, 2+2*len(units))
		out = append(out, 0xfe, 0xff)
		for _, u := range units {
			out = append(out, byte(u>>8), byte(u)) //nolint:gosec // splitting a UTF-16 unit into its two bytes.
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
			"/P <</MCID 0>> BDC\n"+
				"BT /F1 12 Tf 50 750 Td (described text) Tj ET\nEMC\n"+
				"/P <</MCID 1>> BDC\n"+
				"BT /F1 12 Tf 50 720 Td (more described text) Tj ET\nEMC\n"+
				"/P <</MCID 2>> BDC\n"+
				"BT /F1 12 Tf 50 690 Td (still more) Tj ET\nEMC\n"+
				"/P <</MCID 3>> BDC\n"+
				"BT /F1 12 Tf 50 660 Td (and the last) Tj ET\nEMC\n",
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

	// 8.4.3 t2: a Private Use Area character in the text that replaces the content.
	actualBad := paragraph(0, "ActualText", utf16Text("described "+pua+" text"))
	// The control: the same words, no PUA character.
	actualOK := paragraph(1, "ActualText", utf16Text("described text"))

	// 8.4.3 t3: the same character in the alternate description.
	altBad := paragraph(2, "Alt", utf16Text("an alternate "+pua+" description"))
	// The control.
	altOK := paragraph(3, "Alt", utf16Text("an alternate description"))

	kids := []any{actualBad, actualOK, altBad, altOK}
	document.Set("K", a(kids...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(kids...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// 8.6 t1: a text string a consumer reads out. An outline item's /Title reaches
	// CosTextString; a document information /Title and an annotation /Contents were
	// both measured silent, so the outline is what carries this one.
	outlineBad := pdf.Indirect(d("Title", utf16Text("a "+pua+" heading")))
	outlineOK := pdf.Indirect(d("Title", utf16Text("a plain heading")))
	outlineBad.Set("Next", outlineOK)
	outlineOK.Set("Prev", outlineBad)
	outlines := pdf.Indirect(d("Type", n("Outlines"), "First", outlineBad, "Last", outlineOK, "Count", 2))
	outlineBad.Set("Parent", outlines)
	outlineOK.Set("Parent", outlines)
	pdf.Root.Set("Outlines", outlines)

	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">private use text</rdf:li></rdf:Alt></dc:title>
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
