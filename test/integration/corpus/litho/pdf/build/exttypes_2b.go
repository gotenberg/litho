package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("exttypes_2b", exttypes2b) }

// exttypes2b builds exttypes_2b.pdf: the value types an extension schema entry resolves.
//
// veraPDF 1.30.2 builds one validators container per pdfaSchema entry: the
// part's predefined types plus the types that entry defines, where a type with
// fields and no pdfaType:namespaceURI registers nothing. An entry counts only
// with a namespace URI and a property array, the last one per URI replacing the
// others. A pdfaProperty:valueType or pdfaField:valueType is judged defined
// against the container of the entry that counts for its entry's URI, and
// against the basic types alone when none counts. A property of a namespace no
// entry declares, one only a value type names, is undeclared. litho resolved
// every type against the types of every entry, and the predefined ones against
// any entry.
//
// Four entries, each construct measured on veraPDF 1.30.2 with a crafted file:
//
// | Construct                                                       | Rule           |
// | --------------------------------------------------------------- | -------------- |
// | entry 1 declares `ex:old` of type `Pair`, which it defines;     | `6.6.2.3.3/8`  |
// | entry 2, same URI, replaces it and defines nothing              |                |
// | entry 3 has no property array; its type `Box` has fields of     | `6.6.2.3.3/17` |
// | type Dimensions, Locale and Box, and one of type Text           | (three times)  |
// | `et:a` written at top level, `et` naming only value types       | `6.6.2.3.1/1`, |
// |                                                                 | `6.6.2.3.1/2`  |
//
// See ../FIXTURES.md for the authoritative expected firing set.
func exttypes2b() ([]byte, error) {
	const ex = "http://example.com/exttypes/ex/"
	const ey = "http://example.com/exttypes/ey/"
	const et = "http://example.com/exttypes/et/"

	pdf := pdfgen.New()

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.Stream([]byte("0 0 m\n")),
	))
	pdf.AddPage(page)

	type pair struct{ name, valueType string }

	prop := func(p pair) string {
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaProperty:name>%s</pdfaProperty:name>"+
			"<pdfaProperty:valueType>%s</pdfaProperty:valueType>"+
			"<pdfaProperty:category>internal</pdfaProperty:category>"+
			"<pdfaProperty:description>%s</pdfaProperty:description>"+
			"</rdf:li>", p.name, p.valueType, p.name)
	}

	field := func(p pair) string {
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaField:name>%s</pdfaField:name>"+
			"<pdfaField:valueType>%s</pdfaField:valueType>"+
			"<pdfaField:description>%s</pdfaField:description>"+
			"</rdf:li>", p.name, p.valueType, p.name)
	}

	valueType := func(name string, fields []pair) string {
		var fs strings.Builder
		for _, f := range fields {
			fs.WriteString(field(f))
		}
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaType:type>%s</pdfaType:type>"+
			"<pdfaType:namespaceURI>%s</pdfaType:namespaceURI>"+
			"<pdfaType:prefix>et</pdfaType:prefix>"+
			"<pdfaType:description>%s</pdfaType:description>"+
			"<pdfaType:field><rdf:Seq>%s</rdf:Seq></pdfaType:field>"+
			"</rdf:li>", name, et, name, fs.String())
	}

	entry := func(uri, prefix string, props []pair, types []string) string {
		body := fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaSchema:schema>exttypes</pdfaSchema:schema>"+
			"<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>"+
			"<pdfaSchema:prefix>%s</pdfaSchema:prefix>", uri, prefix)
		if props != nil {
			var ps strings.Builder
			for _, p := range props {
				ps.WriteString(prop(p))
			}
			body += fmt.Sprintf("<pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>", ps.String())
		}
		if types != nil {
			body += fmt.Sprintf("<pdfaSchema:valueType><rdf:Seq>%s</rdf:Seq></pdfaSchema:valueType>", strings.Join(types, ""))
		}
		return body + "</rdf:li>"
	}

	entries := entry(ex, "ex", []pair{{"old", "Pair"}}, []string{valueType("Pair", []pair{{"a", "Text"}})}) +
		entry(ex, "ex", []pair{{"kept", "Text"}}, nil) +
		entry(
			ey,
			"ey",
			nil,
			[]string{valueType("Box", []pair{{"d", "Dimensions"}, {"l", "Locale"}, {"s", "Box"}, {"t", "Text"}})},
		)

	xmp := fmt.Sprintf("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n"+
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
    xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
    xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
    xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
    xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
    xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
   <pdfaid:part>2</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>
   <pdfaExtension:schemas><rdf:Bag>%s</rdf:Bag></pdfaExtension:schemas>
  </rdf:Description>
  <rdf:Description rdf:about=""
    xmlns:ex="%s"
    xmlns:et="%s">
   <ex:kept>k</ex:kept>
   <et:a>x</et:a>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`,
		entries,
		ex,
		et,
	)

	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)
}
