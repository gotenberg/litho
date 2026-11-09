package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("uaid_ua1", uaidUa1) }

// uaidUa1 builds uaid_ua1.pdf: the PDF/UA Identification Schema under PDF/UA-1.
//
// The coverage scan showed ISO 14289-1, clause 5 tests 2 to 5 unreached. Test 1
// asks whether the identification is present at all, which `complex_ua1.pdf`
// already reaches by omitting it; the remaining four constrain what the schema
// says once it is there. All of it is XMP text.
//
// Three defects sit in one identification schema: `part` carries the wrong
// number, and `amd` and `corr` each carry the wrong namespace prefix. Planting
// them together is the masking probe, because a check that reports the first
// defective property and stops reports one rule where three are due. The prefix
// verdict is per property, which `idprefix_2b.pdf` establishes for the PDF/A
// identification schema and which holds here too: `part` keeps its canonical
// prefix and is judged on its value alone.
//
// Test 3 is deliberately absent, and it cannot be added here. It fires when
// `part` carries a non-canonical prefix, and a mis-prefixed `part` then has no
// value veraPDF will read, so test 2 stays silent: the two are mutually
// exclusive in one file. Measured on both tools, a packet whose only defect is
// `xua:part` reports test 3 alone, and one whose `part` is canonical with the
// value 2 reports test 2 alone. This fixture takes test 2, since the part number
// is the substantive conformance claim.
//
// The page carries a wired structure tree so the file is a plausible PDF/UA-1
// document rather than a bare metadata carrier, and a second independent defect,
// a figure with no alternate description, so a clause-5 verdict is visibly
// separate from the structure checks.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func uaidUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// One marked content sequence per structure element: a paragraph and a figure.
	content := "/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 700 Td (identification schema) Tj ET\nEMC\n" +
		"/Figure <</MCID 1>> BDC\n0 0 0 rg 50 640 60 30 re f\nEMC\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"Lang", text("en-US"),
	))
	para := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0),
	)
	// ISO 14289-1, 7.3 requires an alternate description on a figure. This one
	// carries none, so it fires beside the clause-5 defects.
	figure := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Figure"), "P", document, "Pg", page, "K", 1),
	)
	document.Set("K", a(para, figure))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(para, figure))),
	))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// `xua` is bound to the identification namespace, so amd and corr resolve
	// correctly and only their prefix is wrong. `part` keeps the canonical prefix
	// and carries 2, which ISO 14289-1 clause 5 test 2 rejects for a part 1 file.
	packet := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>" +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""` +
		` xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">identification schema` +
		"</rdf:li></rdf:Alt></dc:title>" +
		"</rdf:Description>" +
		`<rdf:Description rdf:about=""` +
		` xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"` +
		` xmlns:xua="http://www.aiim.org/pdfua/ns/id/">` +
		"<pdfuaid:part>2</pdfuaid:part>" +
		"<xua:amd>2014</xua:amd>" +
		"<xua:corr>1</xua:corr>" +
		"</rdf:Description>" +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)

	meta := pdf.Stream(packet)
	meta.Set("Type", n("Metadata"))
	meta.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(meta))

	return save(pdf, "1.7")
}
