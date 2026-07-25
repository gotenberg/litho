package compliance

import (
	"bytes"
	"context"
	"testing"
)

func TestXMPExtSchemaBag(t *testing.T) {
	ctx := context.Background()

	// An extension schema whose pdfaExtension:schemas container is an rdf:Seq
	// (it must be an rdf:Bag) fails ISO 19005-1 §6.7.8 t2.
	seqXMP := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#">` +
		`<rdf:Description rdf:about="">` +
		`<pdfaExtension:schemas><rdf:Seq><rdf:li rdf:parseType="Resource">` +
		`<pdfaSchema:schema>X</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>http://example.com/x</pdfaSchema:namespaceURI>` +
		`<pdfaSchema:prefix>x</pdfaSchema:prefix>` +
		`</rdf:li></rdf:Seq></pdfaExtension:schemas>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`)

	var got []Violation
	xmpCheckExtensionSchemas(ctx, seqXMP, 1, &got)
	if !hasRule(got, Rule_19005_1_6_7_8_t02) {
		t.Errorf("schemas as rdf:Seq: got %v, want §6.7.8 t2", got)
	}

	// The same structure with an rdf:Bag container is conformant.
	bagXMP := bytes.ReplaceAll(seqXMP, []byte("rdf:Seq"), []byte("rdf:Bag"))
	var ok []Violation
	xmpCheckExtensionSchemas(ctx, bagXMP, 1, &ok)
	if hasRule(ok, Rule_19005_1_6_7_8_t02) {
		t.Errorf("schemas as rdf:Bag: got %v, want no §6.7.8 t2", ok)
	}

	// Part 2 emits its own clause id.
	var got2 []Violation
	xmpCheckExtensionSchemas(ctx, seqXMP, 2, &got2)
	if !hasRule(got2, Rule_19005_2_6_6_2_3_3_t01) {
		t.Errorf("part 2 schemas as rdf:Seq: got %v, want §6.6.2.3.3 t1", got2)
	}
}

// extSchemaXMP wraps a schema body that declares the pdfa extension namespaces.
// valueTypeContainer and fieldContainer are "Seq" or "Bag".
func extSchemaXMP(valueTypeContainer, fieldContainer string) []byte {
	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
		` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">` +
		`<rdf:Description rdf:about="">` +
		`<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<pdfaSchema:schema>X</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>http://example.com/x</pdfaSchema:namespaceURI>` +
		`<pdfaSchema:prefix>x</pdfaSchema:prefix>` +
		`<pdfaSchema:valueType><rdf:` + valueTypeContainer + `><rdf:li rdf:parseType="Resource">` +
		`<pdfaType:type>MyType</pdfaType:type>` +
		`<pdfaType:namespaceURI>http://example.com/t</pdfaType:namespaceURI>` +
		`<pdfaType:prefix>t</pdfaType:prefix>` +
		`<pdfaType:description>d</pdfaType:description>` +
		`<pdfaType:field><rdf:` + fieldContainer + `><rdf:li rdf:parseType="Resource">` +
		`<pdfaField:name>f</pdfaField:name>` +
		`<pdfaField:valueType>Text</pdfaField:valueType>` +
		`<pdfaField:description>fd</pdfaField:description>` +
		`</rdf:li></rdf:` + fieldContainer + `></pdfaType:field>` +
		`</rdf:li></rdf:` + valueTypeContainer + `></pdfaSchema:valueType>` +
		`</rdf:li></rdf:Bag></pdfaExtension:schemas>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`)
}

// zugferdExtSchemaXMP mirrors the ZUGFeRD/Factur-X catalog XMP: one extension
// schema whose pdfaSchema:property Seq declares props in order, with every
// property then used in attribute form on a separate rdf:Description.
func zugferdExtSchemaXMP(props []string) []byte {
	const ns = "urn:ferd:pdfa:CrossIndustryDocument:invoice:1p0#"

	var decls, usage string
	for _, p := range props {
		decls += `<rdf:li rdf:parseType="Resource">` +
			`<pdfaProperty:name>` + p + `</pdfaProperty:name>` +
			`<pdfaProperty:valueType>Text</pdfaProperty:valueType>` +
			`<pdfaProperty:category>external</pdfaProperty:category>` +
			`<pdfaProperty:description>d</pdfaProperty:description>` +
			`</rdf:li>`
		usage += ` zf:` + p + `="v"`
	}

	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">` +
		`<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<pdfaSchema:schema>ZUGFeRD PDFA Extension Schema</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>` + ns + `</pdfaSchema:namespaceURI>` +
		`<pdfaSchema:prefix>zf</pdfaSchema:prefix>` +
		`<pdfaSchema:property><rdf:Seq>` + decls + `</rdf:Seq></pdfaSchema:property>` +
		`</rdf:li></rdf:Bag></pdfaExtension:schemas>` +
		`</rdf:Description>` +
		`<rdf:Description xmlns:zf="` + ns + `" rdf:about=""` + usage + `/>` +
		`</rdf:RDF></x:xmpmeta>`)
}

// TestXMPExtSchemaMultiPropertySeq pins every rdf:li of a pdfaSchema:property
// Seq as a separate declaration. Recording only at the end of the enclosing
// block kept the last item alone, so the ZUGFeRD schema resolved just
// ConformanceLevel and its three earlier properties reported a false
// ISO 19005-3 §6.6.2.3.1 t2 on all 150 Factur-X corpus files.
func TestXMPExtSchemaMultiPropertySeq(t *testing.T) {
	props := []string{"DocumentFileName", "DocumentType", "Version", "ConformanceLevel"}

	schemas := xmpParseExtensionSchemas(zugferdExtSchemaXMP(props))
	if len(schemas) != 1 {
		t.Fatalf("schema entries: got %d, want 1", len(schemas))
	}
	if len(schemas[0].propTypes) != len(props) {
		t.Errorf("declared properties: got %d %v, want %d",
			len(schemas[0].propTypes), schemas[0].propTypes, len(props))
	}
	for _, p := range props {
		if schemas[0].propTypes[p] != "Text" {
			t.Errorf("property %s: got type %q, want %q", p, schemas[0].propTypes[p], "Text")
		}
	}

	// Every declared property is used, so a dropped declaration surfaces as an
	// unresolvable value type. Rotating the Seq proves the result does not
	// depend on document order: the pre-fix parser flagged whichever three
	// properties were not last.
	for shift := range props {
		rotated := append(append([]string{}, props[shift:]...), props[:shift]...)
		for _, part := range []int{2, 3} {
			var got []Violation
			xmpCheckExtensionPropertyResolution(zugferdExtSchemaXMP(rotated), part, &got)
			if len(got) != 0 {
				t.Errorf("part %d, order %v: got %d violations %v, want none",
					part, rotated, len(got), got)
			}
		}
	}
}

// itextExtSchemaXMP mirrors the iText Factur-X catalog XMP: the schema and
// every property are written in RDF/XML attribute form (RDF/XML §2.5), on a
// nested rdf:Description and on self-closing rdf:li items, instead of as child
// elements.
func itextExtSchemaXMP(props []string) []byte {
	var decls, usage string
	for _, p := range props {
		decls += `<rdf:li pdfaProperty:name="` + p + `"` +
			` pdfaProperty:valueType="Text"` +
			` pdfaProperty:category="external"` +
			` pdfaProperty:description="d"/>`
		usage += ` fx:` + p + `="v"`
	}

	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">` +
		`<pdfaExtension:schemas><rdf:Bag><rdf:li>` +
		`<rdf:Description pdfaSchema:schema="Factur-X PDFA Extension Schema"` +
		` pdfaSchema:namespaceURI="` + itextExtNS + `"` +
		` pdfaSchema:prefix="fx">` +
		`<pdfaSchema:property><rdf:Seq>` + decls + `</rdf:Seq></pdfaSchema:property>` +
		`</rdf:Description>` +
		`</rdf:li></rdf:Bag></pdfaExtension:schemas>` +
		`</rdf:Description>` +
		`<rdf:Description xmlns:fx="` + itextExtNS + `" rdf:about=""` + usage + `/>` +
		`</rdf:RDF></x:xmpmeta>`)
}

// itextExtNS is the Factur-X extension schema namespace iText declares.
const itextExtNS = "urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"

// TestXMPExtSchemaAttributeForm pins RDF/XML §2.5 attribute-form extension
// schemas as equivalent to the child-element spelling. Reading child elements
// alone made every field of an iText-written Factur-X schema look absent, so
// one file reported 20 false violations: §6.6.2.3.3 t2-t4 and t7-t10 for the
// missing fields, plus §6.6.2.3.1 t1 because the declared namespace never
// registered. veraPDF accepts both spellings.
func TestXMPExtSchemaAttributeForm(t *testing.T) {
	ctx := context.Background()
	props := []string{"DocumentFileName", "DocumentType", "Version", "ConformanceLevel"}
	data := itextExtSchemaXMP(props)

	schemas := xmpParseExtensionSchemas(data)
	if len(schemas) != 1 {
		t.Fatalf("schema entries: got %d, want 1", len(schemas))
	}
	if schemas[0].nsURI != itextExtNS {
		t.Errorf("namespace URI: got %q, want %q", schemas[0].nsURI, itextExtNS)
	}
	for _, p := range props {
		if schemas[0].propTypes[p] != "Text" {
			t.Errorf("property %s: got type %q, want %q", p, schemas[0].propTypes[p], "Text")
		}
	}

	// An unregistered namespace makes every property that uses it unknown.
	if _, extensions := xmpExtractNamespaces(data); !extensions[itextExtNS] {
		t.Errorf("extension namespaces: got %v, want %s registered", extensions, itextExtNS)
	}

	for _, part := range []int{1, 2, 3} {
		var got []Violation
		xmpCheckExtensionSchemas(ctx, data, part, &got)
		if len(got) != 0 {
			t.Errorf("part %d required fields: got %d violations %v, want none",
				part, len(got), got)
		}
	}

	for _, part := range []int{2, 3} {
		var got []Violation
		xmpCheckExtensionPropertyResolution(data, part, &got)
		if len(got) != 0 {
			t.Errorf("part %d resolution: got %d violations %v, want none",
				part, len(got), got)
		}
	}
}

// TestXMPExtSchemaMultiValueTypeSeq pins the same per-item recording for the
// pdfaSchema:valueType Seq: each rdf:li defines one custom type, and only the
// last survived before the fix.
func TestXMPExtSchemaMultiValueTypeSeq(t *testing.T) {
	valueType := func(name string) string {
		return `<rdf:li rdf:parseType="Resource">` +
			`<pdfaType:type>` + name + `</pdfaType:type>` +
			`<pdfaType:namespaceURI>http://example.com/t</pdfaType:namespaceURI>` +
			`<pdfaType:prefix>t</pdfaType:prefix>` +
			`<pdfaType:description>d</pdfaType:description>` +
			`<pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource">` +
			`<pdfaField:name>f</pdfaField:name>` +
			`<pdfaField:valueType>Text</pdfaField:valueType>` +
			`<pdfaField:description>fd</pdfaField:description>` +
			`</rdf:li></rdf:Seq></pdfaType:field>` +
			`</rdf:li>`
	}

	data := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
		` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">` +
		`<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<pdfaSchema:schema>X</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>http://example.com/x</pdfaSchema:namespaceURI>` +
		`<pdfaSchema:prefix>x</pdfaSchema:prefix>` +
		`<pdfaSchema:valueType><rdf:Seq>` +
		valueType("TypeA") + valueType("TypeB") +
		`</rdf:Seq></pdfaSchema:valueType>` +
		`</rdf:li></rdf:Bag></pdfaExtension:schemas>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`)

	schemas := xmpParseExtensionSchemas(data)
	if len(schemas) != 1 {
		t.Fatalf("schema entries: got %d, want 1", len(schemas))
	}
	for _, name := range []string{"TypeA", "TypeB"} {
		hasFields, ok := schemas[0].typeFields[name]
		if !ok {
			t.Errorf("custom type %s: not recorded, got %v", name, schemas[0].typeFields)
			continue
		}
		if !hasFields {
			t.Errorf("custom type %s: got hasFields=false, want true", name)
		}
	}
}

func TestXMPExtValueTypeAndFieldSeq(t *testing.T) {
	ctx := context.Background()

	// valueType container as rdf:Bag (must be Seq) -> §6.7.8 t7.
	var vt []Violation
	xmpCheckExtensionSchemas(ctx, extSchemaXMP("Bag", "Seq"), 1, &vt)
	if !hasRule(vt, Rule_19005_1_6_7_8_t07) {
		t.Errorf("valueType as rdf:Bag: got %v, want §6.7.8 t7", vt)
	}
	if hasRule(vt, Rule_19005_1_6_7_8_t16) {
		t.Errorf("valueType case wrongly flagged field-Seq t16: %v", vt)
	}

	// field container as rdf:Bag (must be Seq) -> §6.7.8 t16.
	var fld []Violation
	xmpCheckExtensionSchemas(ctx, extSchemaXMP("Seq", "Bag"), 1, &fld)
	if !hasRule(fld, Rule_19005_1_6_7_8_t16) {
		t.Errorf("field as rdf:Bag: got %v, want §6.7.8 t16", fld)
	}
	if hasRule(fld, Rule_19005_1_6_7_8_t07) {
		t.Errorf("field case wrongly flagged valueType-Seq t7: %v", fld)
	}

	// Both containers as rdf:Seq -> conformant.
	var okv []Violation
	xmpCheckExtensionSchemas(ctx, extSchemaXMP("Seq", "Seq"), 1, &okv)
	if hasRule(okv, Rule_19005_1_6_7_8_t07) || hasRule(okv, Rule_19005_1_6_7_8_t16) {
		t.Errorf("valueType+field as rdf:Seq: got %v, want neither t7 nor t16", okv)
	}
}
