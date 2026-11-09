package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("extpredef_2b", extpredef2b) }

// extpredef2b builds extpredef_2b.pdf: values veraPDF judges through extension declarations.
//
// veraPDF 1.30.2 resolves a property through the extension schema entries of its
// packet first, then those of the main packet, and only then through the
// predefined schemas, so a predefined property an entry declares is judged
// against the declared type, and one no entry declares against its predefined
// type even when an entry extends its namespace. A structure type the part
// predefines is judged field by field, closed choices read as their base type,
// and an XPath value is compiled. litho judged a predefined property in an
// extended namespace against nothing, a predefined structure for being a
// structure only, and an XPath for being simple only.
//
// Each construct fails ISO 19005-2, 6.6.2.3.1 test 2 on its own, measured on
// veraPDF 1.30.2 with a crafted file:
//
// | Property              | Declaration                   | Value                          |
// | --------------------- | ----------------------------- | ------------------------------ |
// | `dc:title`            | `Text`                        | a Lang Alt                     |
// | `xmp:CreateDate`      | `Integer`                     | a date                         |
// | `xmpRights:Marked`    | none; xmpRights extended      | `yes`, not a Boolean           |
// | `ex:dim`              | `Dimensions`                  | `stDim:w` holding `abc`        |
// | `ex:ref`              | `ResourceRef`                 | `stRef:filePath`, a later member |
// | `ex:evt`              | `seq ResourceEvent`           | `stEvt:when` holding `notadate` |
// | `ex:path`             | `XPath`                       | `///`                          |
//
// See ../FIXTURES.md for the authoritative expected firing set.
func extpredef2b() ([]byte, error) {
	const ex = "http://example.com/extpredef/ex/"

	pdf := pdfgen.New()

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.Stream([]byte("0 0 m\n")),
	))
	pdf.AddPage(page)

	prop := func(name, valueType string) string {
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaProperty:name>%s</pdfaProperty:name>"+
			"<pdfaProperty:valueType>%s</pdfaProperty:valueType>"+
			"<pdfaProperty:category>internal</pdfaProperty:category>"+
			"<pdfaProperty:description>%s</pdfaProperty:description>"+
			"</rdf:li>", name, valueType, name)
	}

	entry := func(uri, prefix string, props [][2]string) string {
		var all strings.Builder
		for _, p := range props {
			all.WriteString(prop(p[0], p[1]))
		}
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaSchema:schema>extpredef</pdfaSchema:schema>"+
			"<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>"+
			"<pdfaSchema:prefix>%s</pdfaSchema:prefix>"+
			"<pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>"+
			"</rdf:li>", uri, prefix, all.String())
	}

	entries := entry("http://purl.org/dc/elements/1.1/", "dc", [][2]string{{"title", "Text"}}) +
		entry("http://ns.adobe.com/xap/1.0/", "xmp", [][2]string{{"CreateDate", "Integer"}}) +
		entry("http://ns.adobe.com/xap/1.0/rights/", "xmpRights", [][2]string{{"Owner2", "Text"}}) +
		entry(
			ex,
			"ex",
			[][2]string{
				{"dim", "Dimensions"},
				{"ref", "ResourceRef"},
				{"evt", "seq ResourceEvent"},
				{"path", "XPath"},
			},
		)

	xmp := fmt.Sprintf("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>"+`
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
    xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
    xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
    xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
   <pdfaid:part>2</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>
   <pdfaExtension:schemas><rdf:Bag>%s</rdf:Bag></pdfaExtension:schemas>
  </rdf:Description>
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:xmpRights="http://ns.adobe.com/xap/1.0/rights/"
    xmlns:ex="%s"
    xmlns:stDim="http://ns.adobe.com/xap/1.0/sType/Dimensions#"
    xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#"
    xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">extpredef</rdf:li></rdf:Alt></dc:title>
   <xmp:CreateDate>2020-01-01</xmp:CreateDate>
   <xmpRights:Marked>yes</xmpRights:Marked>
   <xmpRights:Owner2>o</xmpRights:Owner2>
   <ex:dim rdf:parseType="Resource"><stDim:w>abc</stDim:w></ex:dim>
   <ex:ref rdf:parseType="Resource"><stRef:filePath>a.pdf</stRef:filePath></ex:ref>
   <ex:evt><rdf:Seq><rdf:li rdf:parseType="Resource"><stEvt:when>notadate</stEvt:when></rdf:li></rdf:Seq></ex:evt>
   <ex:path>///</ex:path>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`,
		entries,
		ex,
	)

	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)
}
