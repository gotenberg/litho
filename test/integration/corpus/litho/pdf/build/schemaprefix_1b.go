package build

import (
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("schemaprefix_1b", schemaprefix1b) }

// schemaprefix1b builds schemaprefix_1b.pdf: extension schema containers and absent fields.
//
// `extschema_2b.pdf` reaches six tests of the extension schema clause, all of
// them by omitting a required field from a schema, a property or a field. The
// clause is numbered 6.7.8 in ISO 19005-1 and 6.6.2.3.3 in ISO 19005-2/3, and
// the same content answers both, so one fixture closes both cells.
//
// Twelve tests stayed unreached because they need the other halves of the same
// conjunctions. Each test reads `isXValid... == true && ...Prefix == "pdfaN"`,
// so a test fires on a wrong RDF container type or an absent field just as it
// does on a wrong namespace prefix. This fixture takes the container and the
// absence route:
//
//   - The `pdfaExtension:schemas` container is an `rdf:Seq`, which t2 rejects.
//   - `pdfaSchema:valueType` and `pdfaType:field` carry an `rdf:Bag` where t7
//     and t16 require an `rdf:Seq`.
//   - Eight entries each drop one required field, reaching t3, t4, t9, t12, t13,
//     t14, t18 and t19.
//   - One schema entry carries a field outside the extension schema vocabulary,
//     reaching t1.
//
// Seven schema entries sit in the one container, and the value types and fields
// of the sixth are siblings inside it. That grouping is the masking probe: four
// sibling value types carry one defect each, a fifth carries the two defective
// fields, and a sixth conforms, so a check that reports the first offending type
// and stops reports one rule where four are due, and a check that condemns a
// whole `rdf:Seq` once a member fails wrongly reports the conforming sixth type
// and the conforming seventh schema entry.
//
// Deliberately unreached, with the evidence in ../FIXTURES.md:
//
//   - t6, the property container. veraPDF reports it for an `rdf:Bag` or a bare
//     literal in place of the `rdf:Seq`; litho reports neither, so planting it
//     would add a disagreeing cell rather than coverage.
//   - The wrong-prefix half of every test. litho answers a single wrong prefix
//     with a blanket report of every rule at that level and skips the rest of
//     the packet, so one prefix defect both over-reports and masks the other
//     eleven.
//
// The page shows a line of text rather than nothing. An empty content stream
// presents no text item, which is the subject of ISO 14289-1, 7.2 t34, and litho
// reports that test from the absence of a structure tree alone. One line of text
// keeps the two tools agreed on every profile so the clause under test is the
// only thing this fixture says.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func schemaprefix1b() ([]byte, error) {
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

	// In the helpers below an empty string is Python's None: that field is
	// omitted.

	// li wraps one extension schema definition as an rdf:li resource.
	li := func(inner string) string {
		return `<rdf:li rdf:parseType="Resource">` + inner + "</rdf:li>"
	}

	// tag appends <t>v</t> to parts unless v is empty.
	tag := func(parts []string, t, v string) []string {
		if v == "" {
			return parts
		}
		return append(parts, "<"+t+">"+v+"</"+t+">")
	}

	// prop is one pdfaProperty rdf:li. Any argument may be empty to omit that
	// field.
	prop := func(name, valueType, category, description string) string {
		var parts []string
		parts = tag(parts, "pdfaProperty:name", name)
		parts = tag(parts, "pdfaProperty:valueType", valueType)
		parts = tag(parts, "pdfaProperty:category", category)
		parts = tag(parts, "pdfaProperty:description", description)
		return li(strings.Join(parts, ""))
	}

	// field is one pdfaField rdf:li inside a value type. Empty omits that field.
	field := func(name, valueType, description string) string {
		var parts []string
		parts = tag(parts, "pdfaField:name", name)
		parts = tag(parts, "pdfaField:valueType", valueType)
		parts = tag(parts, "pdfaField:description", description)
		return li(strings.Join(parts, ""))
	}

	// valueType is one pdfaType rdf:li. fieldTag names the container its fields
	// sit in.
	valueType := func(typeName, uri, prefix, description string, fields []string, fieldTag string) string {
		var parts []string
		parts = tag(parts, "pdfaType:type", typeName)
		parts = tag(parts, "pdfaType:namespaceURI", uri)
		parts = tag(parts, "pdfaType:prefix", prefix)
		parts = tag(parts, "pdfaType:description", description)
		if len(fields) > 0 {
			parts = append(parts, "<pdfaType:field><"+fieldTag+">"+
				strings.Join(fields, "")+
				"</"+fieldTag+"></pdfaType:field>")
		}
		return li(strings.Join(parts, ""))
	}

	// schema is one pdfaSchema rdf:li. valueTypeTag names its value type
	// container.
	schema := func(name, uri, prefix string, props, valueTypes []string, valueTypeTag, extra string) string {
		var parts []string
		parts = tag(parts, "pdfaSchema:schema", name)
		parts = tag(parts, "pdfaSchema:namespaceURI", uri)
		parts = tag(parts, "pdfaSchema:prefix", prefix)
		parts = append(parts, "<pdfaSchema:property><rdf:Seq>"+
			strings.Join(props, "")+
			"</rdf:Seq></pdfaSchema:property>")
		if len(valueTypes) > 0 {
			parts = append(parts, "<pdfaSchema:valueType><"+valueTypeTag+">"+
				strings.Join(valueTypes, "")+
				"</"+valueTypeTag+"></pdfaSchema:valueType>")
		}
		return li(strings.Join(parts, "") + extra)
	}

	// plain is one conforming property, so a schema's defect stays where it is
	// put.
	plain := func(name string) string {
		return prop(name, "Text", "external", "conforming property")
	}

	// t3: no pdfaSchema:schema. The rest of the entry conforms, so the report is
	// attributable to the absent name alone.
	sNoschema := schema(
		"",
		"http://example.org/litho/delta#",
		"ldelta",
		[]string{plain("deltaOne")},
		nil, "rdf:Seq", "",
	)

	// t4: no pdfaSchema:namespaceURI.
	sNouri := schema(
		"Litho Epsilon Schema",
		"",
		"lepsilon",
		[]string{plain("epsilonOne")},
		nil, "rdf:Seq", "",
	)

	// t1: a field the PDF/A extension schema vocabulary does not define. The
	// decision is by namespace URI: litho:extra belongs to no pdfa namespace, so
	// it is an undefined field rather than a misprefixed one.
	sAlien := schema(
		"Litho Zeta Schema",
		"http://example.org/litho/zeta#",
		"lzeta",
		[]string{plain("zetaOne")},
		nil, "rdf:Seq",
		"<litho:extra>outside the extension schema vocabulary</litho:extra>",
	)

	// t7: the value type container is an rdf:Bag where the clause requires an
	// rdf:Seq. Its single value type conforms, so only the container fails.
	sVtbag := schema(
		"Litho Eta Schema",
		"http://example.org/litho/eta#",
		"leta",
		[]string{prop("etaOne", "EtaRecord", "internal", "uses the type below")},
		[]string{
			valueType(
				"EtaRecord",
				"http://example.org/litho/eta/record#",
				"erec",
				"conforming value type in a wrong container",
				nil, "rdf:Seq",
			),
		},
		"rdf:Bag", "",
	)

	// t9: a property with no pdfaProperty:valueType, beside a conforming sibling.
	// The sibling is the control: a check that condemns the property Seq once a
	// member fails would report it too.
	sNovt := schema(
		"Litho Theta Schema",
		"http://example.org/litho/theta#",
		"ltheta",
		[]string{
			prop("thetaOne", "", "external", "this property declares no value type"),
			plain("thetaTwo"),
		},
		nil, "rdf:Seq", "",
	)

	// t12, t13, t14, t16, t18 and t19 all live below a value type, so six sibling
	// types carry one defect each. Every property of this schema uses the built-in
	// Text type: a property pointing at one of these types would add veraPDF's
	// "value type not defined" report to the type that drops its own name.
	sTypes := schema(
		"Litho Iota Schema",
		"http://example.org/litho/iota#",
		"liota",
		[]string{plain("iotaOne")},
		[]string{
			// t12: no pdfaType:type.
			valueType(
				"",
				"http://example.org/litho/iota/one#",
				"ione",
				"value type with no name",
				nil, "rdf:Seq",
			),
			// t13: no pdfaType:namespaceURI.
			valueType(
				"IotaTwo",
				"",
				"itwo",
				"value type with no namespace URI",
				nil, "rdf:Seq",
			),
			// t14: no pdfaType:prefix.
			valueType(
				"IotaThree",
				"http://example.org/litho/iota/three#",
				"",
				"value type with no prefix",
				nil, "rdf:Seq",
			),
			// t16: the field container is an rdf:Bag where an rdf:Seq is required.
			// Its field conforms, so only the container fails.
			valueType(
				"IotaFour",
				"http://example.org/litho/iota/four#",
				"ifour",
				"fields in a wrong container",
				[]string{field("fourOne", "Text", "conforming field")},
				"rdf:Bag",
			),
			// t18 and t19: two defective fields and one conforming one, siblings
			// in the same rdf:Seq.
			valueType(
				"IotaFive",
				"http://example.org/litho/iota/five#",
				"ifive",
				"value type whose fields carry the defects",
				[]string{
					field("fiveOne", "", "this field declares no value type"),
					field("fiveTwo", "Text", ""),
					field("fiveThree", "Text", "conforming field"),
				},
				"rdf:Seq",
			),
			// Control: conforming in every respect its five siblings break.
			valueType(
				"IotaSix",
				"http://example.org/litho/iota/six#",
				"isix",
				"fully conforming value type",
				[]string{field("sixOne", "Text", "conforming field")},
				"rdf:Seq",
			),
		},
		"rdf:Seq", "",
	)

	// Control: a schema entry conforming in every field, whose property names a
	// value type this same entry defines, whose value type conforms and whose
	// field conforms. Its silence is what shows the container defect above is not
	// condemning the whole bag.
	sOK := schema(
		"Litho Kappa Schema",
		"http://example.org/litho/kappa#",
		"lkappa",
		[]string{prop("kappaOne", "KappaRecord", "external", "names the type below")},
		[]string{
			valueType(
				"KappaRecord",
				"http://example.org/litho/kappa/record#",
				"krec",
				"fully conforming value type",
				[]string{field("kappaFieldOne", "Text", "conforming field")},
				"rdf:Seq",
			),
		},
		"rdf:Seq", "",
	)

	// t2: the container is an rdf:Seq where ISO 19005-1 6.7.8 requires an rdf:Bag.
	// veraPDF still descends into the entries, so the container defect and the
	// entry defects are reported side by side rather than one hiding the other.
	packet := "<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>" +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
		"<pdfaid:part>1</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>" +
		"</rdf:Description>" +
		`<rdf:Description rdf:about=""` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` +
		` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
		` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#"` +
		` xmlns:litho="http://example.org/litho/alien#">` +
		"<pdfaExtension:schemas><rdf:Seq>" +
		sNoschema +
		sNouri +
		sAlien +
		sVtbag +
		sNovt +
		sTypes +
		sOK +
		"</rdf:Seq></pdfaExtension:schemas>" +
		"</rdf:Description>" +
		"</rdf:RDF></x:xmpmeta>" +
		`<?xpacket end="w"?>`

	meta := pdf.Stream([]byte(packet))
	meta.Set("Type", n("Metadata"))
	meta.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(meta))

	return save(pdf, "1.4")
}
