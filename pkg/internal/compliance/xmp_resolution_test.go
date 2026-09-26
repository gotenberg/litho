package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// resolutionXMP builds a packet that uses ex:part and declares it via one or
// two pdfaSchema entries sharing the same namespace URI. With two entries the
// second declares only "amd". ISO 19005-2 §6.6.2.3 does not specify how to
// merge two extension-schema container entries that share a namespace URI;
// litho follows the interpretation that a later entry replaces the earlier
// one, so the value type of ex:part is no longer resolvable.
func resolutionXMP(duplicateEntry bool) []byte {
	second := ""
	if duplicateEntry {
		second = `<rdf:li rdf:parseType="Resource">` +
			`<pdfaSchema:schema>X</pdfaSchema:schema>` +
			`<pdfaSchema:namespaceURI>http://example.com/x/</pdfaSchema:namespaceURI>` +
			`<pdfaSchema:prefix>exb</pdfaSchema:prefix>` +
			`<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">` +
			`<pdfaProperty:name>amd</pdfaProperty:name>` +
			`<pdfaProperty:valueType>Text</pdfaProperty:valueType>` +
			`<pdfaProperty:category>internal</pdfaProperty:category>` +
			`<pdfaProperty:description>d</pdfaProperty:description>` +
			`</rdf:li></rdf:Seq></pdfaSchema:property></rdf:li>`
	}
	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` +
		` xmlns:ex="http://example.com/x/">` +
		`<rdf:Description rdf:about="">` +
		`<ex:part>1</ex:part>` +
		`<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">` +
		`<pdfaSchema:schema>X</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>http://example.com/x/</pdfaSchema:namespaceURI>` +
		`<pdfaSchema:prefix>ex</pdfaSchema:prefix>` +
		`<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">` +
		`<pdfaProperty:name>part</pdfaProperty:name>` +
		`<pdfaProperty:valueType>Integer</pdfaProperty:valueType>` +
		`<pdfaProperty:category>internal</pdfaProperty:category>` +
		`<pdfaProperty:description>d</pdfaProperty:description>` +
		`</rdf:li></rdf:Seq></pdfaSchema:property></rdf:li>` +
		second +
		`</rdf:Bag></pdfaExtension:schemas>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`)
}

// TestXMPExtensionPropertyResolution_DuplicateURIReplacement pins the
// extension type-map merge interpretation under ISO 19005-2 §6.6.2.3.1: a
// later pdfaSchema entry with the same namespace URI replaces the earlier one,
// so a property declared only by the replaced entry has no resolvable value
// type and fails t2.
func TestXMPExtensionPropertyResolution_DuplicateURIReplacement(t *testing.T) {
	var dup []Violation
	xmpCheckUndeclaredExtensionProperties(resolutionXMP(true), 2, nil, nil, &dup)
	if !hasRule(dup, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("duplicate same-URI entry: got %v, want §6.6.2.3.1 t2 on ex:part", dup)
	}

	var single []Violation
	xmpCheckUndeclaredExtensionProperties(resolutionXMP(false), 2, nil, nil, &single)
	if hasRule(single, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("single entry declaring the property: got %v, want no t2", single)
	}
}

// TestXMPPropertyTypes_KnownNamespacesWithoutMembers pins the empty pdfx and
// adhocwf catalogs: both namespaces are recognized but predefine no
// properties. Per ISO 19005-2 §6.6.2.3.1 a property is only valid when it
// belongs to a predefined schema or a compliant extension schema, so absent
// either, every member of these namespaces fails definedness (t1) and type
// resolution (t2).
func TestXMPPropertyTypes_KnownNamespacesWithoutMembers(t *testing.T) {
	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"` +
		` xmlns:pdfx="http://ns.adobe.com/pdfx/1.3/"` +
		` xmlns:adhocwf="http://ns.adobe.com/AcrobatAdhocWorkflow/1.0/">` +
		`<rdf:Description rdf:about="">` +
		`<pdfx:Company>ACME</pdfx:Company>` +
		`<adhocwf:state>1</adhocwf:state>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`)

	var got []Violation
	xmpCheckPropertyTypes(xmp, 2, ast.PdfStream{}, nil, &got)
	count := 0
	for _, v := range got {
		if v.Rule == Rule_19005_2_6_6_2_3_1_t02 {
			count++
		}
	}
	if count != 2 {
		t.Errorf("pdfx:Company + adhocwf:state: got %d t2 violations (%v), want 2", count, got)
	}
}
