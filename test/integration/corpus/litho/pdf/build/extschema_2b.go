package build

import (
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("extschema_2b", extschema2b) }

// extschema2b builds extschema_2b.pdf: XMP extension schema structure defects.
//
// The coverage scan showed ISO 19005-2, 6.6.2.3.3 as the largest unreached
// cluster in the corpus at 18 rules. It describes the shape an extension schema
// must have: which fields are required on a schema, a property and a value
// type, and which namespace prefix each must carry. All of it is XMP text, so
// it costs nothing to plant.
//
// Three schema entries sit in the same bag. The first is defective in its own
// fields, the second has defective properties, and the third conforms
// completely. Sibling entries are the point: a check that reports the first bad
// schema and stops never reaches the second, and a check that condemns the bag
// once a defect is seen wrongly reports the third.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func extschema2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (extension schemas) Tj ET\n")),
	)
	pdf.AddPage(pdf.Indirect(page))

	// tag appends <name>v</name> to parts unless v is nil, Python's None.
	tag := func(parts []string, name string, v any) []string {
		if v == nil {
			return parts
		}
		return append(parts, "<"+name+">"+v.(string)+"</"+name+">")
	}
	li := func(parts []string) string {
		return `<rdf:li rdf:parseType="Resource">` + strings.Join(parts, "") + "</rdf:li>"
	}

	// prop is one pdfaProperty rdf:li. Any argument may be nil to omit that
	// field.
	prop := func(name, valueType, category, description any) string {
		var parts []string
		parts = tag(parts, "pdfaProperty:name", name)
		parts = tag(parts, "pdfaProperty:valueType", valueType)
		parts = tag(parts, "pdfaProperty:category", category)
		parts = tag(parts, "pdfaProperty:description", description)
		return li(parts)
	}

	// field is one pdfaField rdf:li inside a value type. nil omits that
	// field.
	field := func(name, valueType, description any) string {
		var parts []string
		parts = tag(parts, "pdfaField:name", name)
		parts = tag(parts, "pdfaField:valueType", valueType)
		parts = tag(parts, "pdfaField:description", description)
		return li(parts)
	}

	// valueType is one pdfaType rdf:li. Any argument may be nil to omit that
	// field.
	valueType := func(typeName, uri, prefix, description any, fields []string) string {
		var parts []string
		parts = tag(parts, "pdfaType:type", typeName)
		parts = tag(parts, "pdfaType:namespaceURI", uri)
		parts = tag(parts, "pdfaType:prefix", prefix)
		parts = tag(parts, "pdfaType:description", description)
		if len(fields) > 0 {
			parts = append(parts,
				"<pdfaType:field><rdf:Seq>"+strings.Join(fields, "")+"</rdf:Seq></pdfaType:field>")
		}
		return li(parts)
	}

	schema := func(name, uri, prefix any, props, valueTypes []string) string {
		var parts []string
		parts = tag(parts, "pdfaSchema:schema", name)
		parts = tag(parts, "pdfaSchema:namespaceURI", uri)
		parts = tag(parts, "pdfaSchema:prefix", prefix)
		parts = append(parts,
			"<pdfaSchema:property><rdf:Seq>"+strings.Join(props, "")+"</rdf:Seq></pdfaSchema:property>")
		if len(valueTypes) > 0 {
			parts = append(parts,
				"<pdfaSchema:valueType><rdf:Seq>"+
					strings.Join(valueTypes, "")+
					"</rdf:Seq></pdfaSchema:valueType>")
		}
		return li(parts)
	}

	// Schema one omits pdfaSchema:prefix (6.6.2.3.3 t4). Its single property is
	// well formed, so the defect is confined to the schema's own fields.
	s1 := schema(
		"Litho Alpha Schema",
		"http://example.org/litho/alpha#",
		nil,
		[]string{prop("alphaOne", "Text", "external", "first alpha property")},
		nil,
	)

	// Schema two is well formed itself; its properties carry the defects. One has
	// a category outside {external, internal} (t9), one omits its description
	// (t10), and one omits its name (t7). Three sibling properties, three
	// independent rules.
	s2 := schema(
		"Litho Beta Schema",
		"http://example.org/litho/beta#",
		"lbeta",
		[]string{
			prop("betaOne", "Text", "sideways", "category is not external or internal"),
			prop("betaTwo", "Text", "external", nil),
			prop(nil, "Text", "external", "this property has no name"),
		},
		nil,
	)

	// Schema three carries value type definitions, the half of 6.6.2.3.3 the
	// property entries never reach. One type omits its pdfaType:description, one
	// of its fields omits pdfaField:name, and a second type is conforming
	// throughout. Its properties stay conforming so the defects are attributable
	// to the value types alone.
	s3 := schema(
		"Litho Gamma Schema",
		"http://example.org/litho/gamma#",
		"lgamma",
		[]string{prop("gammaOne", "GammaRecord", "internal", "uses the custom type below")},
		[]string{
			valueType(
				"GammaRecord",
				"http://example.org/litho/gamma/record#",
				"grec",
				nil,
				[]string{
					field("recordOne", "Text", "first field, conforming"),
					field(nil, "Text", "this field has no name"),
				},
			),
			valueType(
				"GammaOther",
				"http://example.org/litho/gamma/other#",
				"goth",
				"fully conforming value type",
				[]string{field("otherOne", "Text", "conforming field")},
			),
		},
	)

	packet := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>" +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
		"<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>" +
		"</rdf:Description>" +
		`<rdf:Description rdf:about=""` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` +
		` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
		` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">` +
		"<pdfaExtension:schemas><rdf:Bag>" + s1 + s2 + s3 + "</rdf:Bag></pdfaExtension:schemas>" +
		"</rdf:Description>" +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)

	meta := pdf.Stream(packet)
	meta.Set("Type", n("Metadata"))
	meta.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(meta))

	// Deliberately no /OutputIntents, so the colour rules stay reachable too.
	return save(pdf, "1.7")
}
