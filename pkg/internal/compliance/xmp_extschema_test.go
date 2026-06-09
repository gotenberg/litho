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
