package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("docinfo_1b", docinfo1b) }

// docinfo1b builds docinfo_1b.pdf: document information dictionary versus XMP.
//
// ISO 19005-1, 6.7.3 requires every entry of the document information
// dictionary that is present to equal its XMP analogue. The clause holds eight
// tests, one per entry, all scoped to the information dictionary itself. No
// other fixture in this corpus carries an information dictionary beside an XMP
// packet, so a coverage scan found all eight unreached.
//
// This fixture is the masking probe for that clause. All eight entries are
// mismatched at once, so a check that reports the first offending entry and
// stops reports one rule where eight are due.
//
// Each mismatch is a different kind, so the fixture also probes how the two
// values are compared rather than only whether they are compared:
//
//	t1  /CreationDate  same wall clock, a different UTC offset
//	t2  /Title         the same letters in a different case
//	t3  /Author        the value matches, but dc:creator holds two entries
//	t4  /Subject       an unrelated value
//	t5  /Keywords      the same keywords in a different order
//	t6  /Creator       present here, absent from the packet
//	t7  /Producer      one trailing space
//	t8  /ModDate       same wall clock, two different non-UTC offsets
//
// t3 is the one test that is not a plain inequality. Its condition is
// `Author == XMPCreator && XMPCreatorSize == 1`, so dc:creator carries /Author
// verbatim as its first entry and a second entry behind it: only the size
// condition fails, which a check that compares values alone cannot see.
//
// Three must-not-fire controls guard against over-firing. All three sit
// outside the eight tests, because reaching all eight requires all eight to
// mismatch:
//   - /Trapped matches pdf:Trapped exactly, so a firing on it means the check
//     generalised past the entries 6.7.3 names,
//   - /Company has no XMP analogue at all and no test governs it, so a firing
//     on it means the check condemns every unmatched entry,
//   - the extension schema below is conforming in every field, so a firing on
//     it is a false positive on a legal declaration.
//
// The extension schema is what the /Trapped control costs. pdf:Trapped is not
// one of the three properties the predefined Adobe PDF schema carries, so
// ISO 19005-1, 6.7.9 requires a declaration for it. Without one the control
// would drag two XMP schema classes into a fixture about synchronisation.
//
// The page is deliberately empty. Colour, font and content rules would add
// classes that say nothing about metadata synchronisation and would drown the
// eight tests in the cross-profile comparison.
//
// The dates are content, not build metadata, so they are hard coded. Reading a
// clock here would make the fixture rebuild differently every run.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func docinfo1b() ([]byte, error) {
	pdf := pdfgen.New()

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.Stream([]byte{}),
	)
	pdf.AddPage(pdf.Indirect(page))

	// t1: 10:30 at +01:00 is 09:30 UT, so the two dates name different instants
	// while sharing every digit of their wall clock.
	docinfo(pdf).Set("CreationDate", text("D:20240115103000+01'00'"))

	// t8: the same trick between two non-UTC offsets, which keeps the test off
	// the Z spelling t1 already exercises.
	docinfo(pdf).Set("ModDate", text("D:20240220154500+02'00'"))

	// t2: dc:title carries the same words title-cased.
	docinfo(pdf).Set("Title", text("litho corpus docinfo fixture"))

	// t3: dc:creator repeats this value verbatim and then adds a second entry.
	docinfo(pdf).Set("Author", text("Gotenberg"))

	// t4: dc:description says something else entirely.
	docinfo(pdf).Set("Subject", text("Document information dictionary synchronisation"))

	// t5: pdf:Keywords lists the same three keywords reversed.
	docinfo(pdf).Set("Keywords", text("metadata, docinfo, xmp"))

	// t6: the packet defines no xmp:CreatorTool.
	docinfo(pdf).Set("Creator", text("litho corpus builder"))

	// t7: pdf:Producer carries one trailing space.
	docinfo(pdf).Set("Producer", text("litho 0.1.0"))

	// Control: pdf:Trapped holds exactly this value. No test of 6.7.3 names
	// /Trapped, and the pair matches anyway, so silence is required twice over.
	// The extension schema at the end of the packet declares the property.
	docinfo(pdf).Set("Trapped", n("/False"))

	// Control: no XMP analogue exists for /Company and no test governs it.
	docinfo(pdf).Set("Company", text("Gotenberg"))

	packet := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"  <rdf:Description rdf:about=\"\"\n" +
		"    xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n" +
		"    xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n" +
		"    xmlns:pdf=\"http://ns.adobe.com/pdf/1.3/\"\n" +
		"    xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\">\n" +
		"   <pdfaid:part>1</pdfaid:part>\n" +
		"   <pdfaid:conformance>B</pdfaid:conformance>\n" +
		"   <dc:title><rdf:Alt>" +
		"<rdf:li xml:lang=\"x-default\">Litho Corpus Docinfo Fixture</rdf:li>" +
		"</rdf:Alt></dc:title>\n" +
		"   <dc:creator><rdf:Seq>" +
		"<rdf:li>Gotenberg</rdf:li><rdf:li>litho corpus</rdf:li>" +
		"</rdf:Seq></dc:creator>\n" +
		"   <dc:description><rdf:Alt>" +
		"<rdf:li xml:lang=\"x-default\">An unrelated description</rdf:li>" +
		"</rdf:Alt></dc:description>\n" +
		"   <pdf:Keywords>xmp, docinfo, metadata</pdf:Keywords>\n" +
		"   <pdf:Producer>litho 0.1.0 </pdf:Producer>\n" +
		"   <pdf:Trapped>False</pdf:Trapped>\n" +
		"   <xmp:CreateDate>2024-01-15T10:30:00Z</xmp:CreateDate>\n" +
		"   <xmp:ModifyDate>2024-02-20T15:45:00-05:00</xmp:ModifyDate>\n" +
		"  </rdf:Description>\n" +
		"  <rdf:Description rdf:about=\"\"\n" +
		"    xmlns:pdfaExtension=\"http://www.aiim.org/pdfa/ns/extension/\"\n" +
		"    xmlns:pdfaSchema=\"http://www.aiim.org/pdfa/ns/schema#\"\n" +
		"    xmlns:pdfaProperty=\"http://www.aiim.org/pdfa/ns/property#\">\n" +
		"   <pdfaExtension:schemas><rdf:Bag>" +
		"<rdf:li rdf:parseType=\"Resource\">" +
		"<pdfaSchema:schema>Adobe PDF</pdfaSchema:schema>" +
		"<pdfaSchema:namespaceURI>http://ns.adobe.com/pdf/1.3/</pdfaSchema:namespaceURI>" +
		"<pdfaSchema:prefix>pdf</pdfaSchema:prefix>" +
		"<pdfaSchema:property><rdf:Seq>" +
		"<rdf:li rdf:parseType=\"Resource\">" +
		"<pdfaProperty:name>Trapped</pdfaProperty:name>" +
		"<pdfaProperty:valueType>Text</pdfaProperty:valueType>" +
		"<pdfaProperty:category>internal</pdfaProperty:category>" +
		"<pdfaProperty:description>trapping state of the document" +
		"</pdfaProperty:description>" +
		"</rdf:li>" +
		"</rdf:Seq></pdfaSchema:property>" +
		"</rdf:li>" +
		"</rdf:Bag></pdfaExtension:schemas>\n" +
		"  </rdf:Description>\n" +
		" </rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>\n")

	// Writing the packet as a plain stream never touches /Info, so every
	// mismatch above stays.
	attachXMP(pdf, packet)

	return save(pdf, "1.4")
}
