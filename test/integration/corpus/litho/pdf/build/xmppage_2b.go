package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("xmppage_2b", xmppage2b) }

// xmppage2b builds xmppage_2b.pdf: the extension-schema tests on the XMP packets pages carry.
//
// ISO 19005-2, 6.6.2.3 constrains the properties and the extension schemas of
// every metadata stream in the file, not only the catalog's. veraPDF 1.30.2
// models each page's packet as an XMPPackage whose properties resolve through
// its own extension schemas and the catalog packet's, and whose schemas answer
// the structure tests as the catalog packet's do. Each page's packet plants one
// kind of defect, and each rule of 6.6.2.3 the file fires comes from one page:
//
// | Page | Packet                                                          | Rule at PDF/A-2b                        |
// | ---- | --------------------------------------------------------------- | --------------------------------------- |
// | 1    | `eu:foo`, a namespace no packet declares                        | `6.6.2.3.1/1` and `/2`                  |
// | 1    | `ex:pt`, declared by the catalog packet alone                   | silent                                  |
// | 2    | a property with no description, one of category `sideways`      | `6.6.2.3.3/10`, `6.6.2.3.3/9`           |
// | 2    | an entry with no prefix, a value type with no description       | `6.6.2.3.3/4`, `6.6.2.3.3/14`           |
// | 2    | a `dc:stray` field inside a schema entry                        | `6.6.2.3.2/1`                           |
// | 3    | `pdfaExtension:schemas` written as an `rdf:Seq`                 | `6.6.2.3.3/1`                           |
// | 4    | `ex:pa` of type `MainT`, which the catalog's `ex` entry defines | silent                                  |
// | 4    | an entry with no property array, a field of type `Dimensions`   | `6.6.2.3.3/17`                          |
//
// Page 4's entry for `ex` resolves `MainT` through the catalog packet's entry
// for the same namespace, as veraPDF builds a page entry's validators on a copy
// of the catalog entry's, so neither `6.6.2.3.3/8` nor `6.6.2.3.1` fires there.
// Its entry with no property array registers nothing, so its field types are
// judged against the basic types alone, which hold no `Dimensions`.
//
// No property name repeats across packets: veraPDF checks one property per
// namespace, prefix and name in a document, the first its traversal meets, and a
// repeated one would make the verdict depend on that order.
//
// ISO 19005-1 builds no package for a page's metadata stream, so the page
// packets are silent at PDF/A-1b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xmppage2b() ([]byte, error) {
	const ex = "http://example.com/xmppage/ex/"
	const mt = "http://example.com/xmppage/mt/"

	ns := fmt.Sprintf(` xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`+
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"`+
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"`+
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"`+
		` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"`+
		` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#"`+
		` xmlns:dc="http://purl.org/dc/elements/1.1/"`+
		` xmlns:xmp="http://ns.adobe.com/xap/1.0/"`+
		` xmlns:ex="%s" xmlns:mt="%s"`+
		` xmlns:pa="http://example.com/xmppage/pa/"`+
		` xmlns:pd="http://example.com/xmppage/pd/"`+
		` xmlns:eu="http://example.com/xmppage/eu/"`, ex, mt)

	// prop is one extension schema property, with a description when asked.
	prop := func(name, valueType, category string, description bool) string {
		desc := ""
		if description {
			desc = fmt.Sprintf("<pdfaProperty:description>%s</pdfaProperty:description>", name)
		}
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaProperty:name>%s</pdfaProperty:name>"+
			"<pdfaProperty:valueType>%s</pdfaProperty:valueType>"+
			"<pdfaProperty:category>%s</pdfaProperty:category>%s"+
			"</rdf:li>", name, valueType, category, desc)
	}

	valueType := func(name, uri, prefix string, fields [][2]string, description bool) string {
		desc := ""
		if description {
			desc = fmt.Sprintf("<pdfaType:description>%s</pdfaType:description>", name)
		}
		var items strings.Builder
		for _, f := range fields {
			fmt.Fprintf(&items, `<rdf:li rdf:parseType="Resource"><pdfaField:name>%s</pdfaField:name>`+
				"<pdfaField:valueType>%s</pdfaField:valueType>"+
				"<pdfaField:description>%s</pdfaField:description></rdf:li>", f[0], f[1], f[0])
		}
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaType:type>%s</pdfaType:type>"+
			"<pdfaType:namespaceURI>%s</pdfaType:namespaceURI>"+
			"<pdfaType:prefix>%s</pdfaType:prefix>%s"+
			"<pdfaType:field><rdf:Seq>%s</rdf:Seq></pdfaType:field>"+
			"</rdf:li>", name, uri, prefix, desc, items.String())
	}

	// entry is one extension schema; props nil leaves its property list out.
	entry := func(uri, prefix string, props []string, types, extra string) string {
		pfx := ""
		if prefix != "" {
			pfx = fmt.Sprintf("<pdfaSchema:prefix>%s</pdfaSchema:prefix>", prefix)
		}
		properties := ""
		if props != nil {
			properties = fmt.Sprintf("<pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>", strings.Join(props, ""))
		}
		valuetypes := ""
		if types != "" {
			valuetypes = fmt.Sprintf("<pdfaSchema:valueType><rdf:Seq>%s</rdf:Seq></pdfaSchema:valueType>", types)
		}
		return fmt.Sprintf(`<rdf:li rdf:parseType="Resource">`+
			"<pdfaSchema:schema>xmppage</pdfaSchema:schema>"+
			"<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>"+
			"%s%s%s%s</rdf:li>", uri, pfx, properties, valuetypes, extra)
	}

	// packet is a page XMP packet whose extension schemas sit in an rdf
	// container, Bag or Seq.
	packet := func(head, schemas, container, body string) []byte {
		ext := ""
		if schemas != "" {
			ext = fmt.Sprintf("<pdfaExtension:schemas><rdf:%s>%s</rdf:%s></pdfaExtension:schemas>", container, schemas, container)
		}
		return []byte(fmt.Sprintf("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n"+
			"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n"+
			" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n"+
			"  <rdf:Description rdf:about=\"\"%s>\n"+
			"   %s%s\n"+
			"  </rdf:Description>\n"+
			"  <rdf:Description rdf:about=\"\"%s>\n"+
			"   %s\n"+
			"  </rdf:Description>\n"+
			" </rdf:RDF>\n"+
			"</x:xmpmeta>\n"+
			"<?xpacket end=\"w\"?>", ns, head, ext, ns, body))
	}

	metadata := func(pdf *pdfgen.Doc, data []byte) *pdfgen.Stream {
		meta := pdf.Stream(data)
		meta.Set("Type", n("Metadata"))
		meta.Set("Subtype", n("XML"))
		return pdf.MakeIndirect(meta).(*pdfgen.Stream)
	}

	catalog := packet(
		"<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>",
		entry(ex, "ex", []string{prop("pt", "Text", "internal", true), prop("pm", "MainT", "internal", true)},
			valueType("MainT", mt, "mt", [][2]string{{"a", "Text"}}, true), ""),
		"Bag",
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">xmppage</rdf:li></rdf:Alt></dc:title>`,
	)

	pages := [][]byte{
		packet("", "", "Bag", "<eu:foo>bar</eu:foo><ex:pt>declared by the catalog packet</ex:pt><xmp:CreatorTool>litho</xmp:CreatorTool>"),
		packet(
			"",
			entry(
				"http://example.com/xmppage/pa/",
				"pa",
				[]string{prop("p1", "Text", "internal", false), prop("p2", "Text", "sideways", true)},
				"",
				"<dc:stray>x</dc:stray>",
			)+
				entry("http://example.com/xmppage/pb/", "", []string{prop("q", "Text", "internal", true)}, "", "")+
				entry(
					"http://example.com/xmppage/pc/",
					"pc",
					[]string{prop("r", "T", "internal", true)},
					valueType("T", "http://example.com/xmppage/pct/", "pct", [][2]string{{"a", "Text"}}, false),
					"",
				),
			"Bag",
			"<pa:p1>x</pa:p1>",
		),
		packet(
			"",
			entry("http://example.com/xmppage/pd/", "pd", []string{prop("s", "Text", "internal", true)}, "", ""),
			"Seq",
			"<pd:s>x</pd:s>",
		),
		packet(
			"",
			entry(ex, "ex", []string{prop("pa", "MainT", "internal", true)}, "", "")+
				entry(
					"http://example.com/xmppage/pe/",
					"pe",
					nil,
					valueType("TW", "http://example.com/xmppage/tw/", "tw", [][2]string{{"d", "Dimensions"}}, true),
					"",
				),
			"Bag",
			`<ex:pa rdf:parseType="Resource"><mt:a>x</mt:a></ex:pa>`,
		),
	}

	pdf := pdfgen.New()
	for _, data := range pages {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d(),
			"Contents", pdf.Stream([]byte("0 0 m\n")),
			"Metadata", metadata(pdf, data),
		))
		pdf.AddPage(page)
	}
	pdf.Root.Set("Metadata", metadata(pdf, catalog))

	return save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)
}
