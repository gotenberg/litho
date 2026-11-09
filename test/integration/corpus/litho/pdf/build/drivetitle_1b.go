package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("drivetitle_1b", drivetitle1b) }

// drivetitle1b builds drivetitle_1b.pdf: document information text that begins like a PDF
// date, at PDF/A-1b.
//
// ISO 19005-1, 6.7.3 makes each document information entry equal to its XMP
// analogue, and veraPDF compares a text entry as a string ("Title == XMPTitle").
// Only /CreationDate and /ModDate are dates (ISO 32000-1, Table 317). litho read
// every Info value that starts with "D:" as a date, on the Info side alone, so a
// /Title naming a Windows path on drive D failed against its own copy in
// dc:title. No file of the litho or veraPDF corpora reaches it; the facturx and
// govdocs files that do are not remediated by the gate.
//
// Three entries, each equal to its XMP analogue, so no 6.7.3 test fires:
//
// | Info entry      | Value                      | XMP property      |
// | --------------- | -------------------------- | ----------------- |
// | `/Title`        | `D:\openkm\temp\okm25.pdf` | `dc:title`     |
// | `/Subject`      | `D:20200101`, date-shaped  | `dc:description`  |
// | `/CreationDate` | `D:20200101120000Z`, a date | `xmp:CreateDate` |
//
// /Title and /Subject are the two probes, on independent tests (6.7.3 t2 and
// t4), so a check that latches on the first cannot hide the second.
// /CreationDate is the control: a real date still compares as an instant, so
// "2020-01-01T12:00:00Z" matches it.
//
// The file is otherwise a conforming PDF/A-1b document, built as
// iccv4intent_1b.pdf with a v2 CMYK destination profile.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func drivetitle1b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go
	// builds it.
	iccProfile := iccMinimalProfile

	destination := pdf.Stream(iccProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(
			d(
				"Type", n("OutputIntent"),
				"S", n("GTS_PDFA1"),
				"OutputConditionIdentifier", text("litho destination"),
				"DestOutputProfile", pdf.MakeIndirect(destination),
			),
		),
	))

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoBareSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoBareSans"),
		"FirstChar", 0x41,
		"LastChar", 0x42,
		"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("BT /F1 24 Tf 20 100 Td (AB) Tj ET\n"))),
	))
	pdf.AddPage(page)

	const title = `D:\openkm\temp\okm25.pdf`
	const subject = "D:20200101"
	pdf.Trailer.Set("Info", pdf.Indirect(d(
		"Title", text(title),
		"Subject", text(subject),
		"CreationDate", text("D:20200101120000Z"),
	)))

	xmp := fmt.Sprintf("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n"+`<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:dc="http://purl.org/dc/elements/1.1/">
   <pdfaid:part>1</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:title>
   <dc:description><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:description>
   <xmp:CreateDate>2020-01-01T12:00:00Z</xmp:CreateDate>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>
`, title, subject)

	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.4", noFixMetadataVersion)
}
