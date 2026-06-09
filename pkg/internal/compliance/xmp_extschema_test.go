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
