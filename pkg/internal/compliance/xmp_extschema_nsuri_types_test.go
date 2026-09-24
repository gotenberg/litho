package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestExtensionSchema_NamespacelessEntryDefinesNoType pins that a schema entry
// stating no pdfaSchema:namespaceURI reaches nothing, so the custom value types
// it declares resolve for nobody, its own properties included. ISO 19005-1
// §6.7.8 t9 and ISO 19005-2/3 §6.6.2.3.3 t8 both read isValueTypeDefined.
// The rule asks whether the CUSTOM type resolves, not whether the entry around
// it is well formed.
//
// # Corpus evidence
//
//   - verapdf 6-6-2-3-3-t01-fail-a.pdf: two controlled edits established the
//     shape. Restoring the namespaceURI clears the finding, and naming the
//     predefined "Text" instead of the custom type clears it as well.
//   - pdfjs bug2025674.pdf: declares a "Closed Choice of" wrapper around a
//     predefined type with no namespaceURI, and veraPDF passes it.
func TestExtensionSchema_NamespacelessEntryDefinesNoType(t *testing.T) {
	t.Parallel()

	// entry builds one pdfaSchema item declaring CustomValueType and a property
	// whose valueType names vt. nsURI is omitted entirely when empty.
	entry := func(nsURI, vt string) []byte {
		ns := ""
		if nsURI != "" {
			ns = fmt.Sprintf("<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>", nsURI)
		}
		return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>Some extension schema</pdfaSchema:schema>` + ns + `
        <pdfaSchema:prefix>fs</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaProperty:name>property1</pdfaProperty:name>
          <pdfaProperty:valueType>` + vt + `</pdfaProperty:valueType>
          <pdfaProperty:category>internal</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
        <pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaType:type>CustomValueType</pdfaType:type>
          <pdfaType:namespaceURI>http://www.custom.com/1/</pdfaType:namespaceURI>
          <pdfaType:prefix>cvt</pdfaType:prefix>
          <pdfaType:description>Custom Value Type</pdfaType:description>
          <pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource">
            <pdfaField:name>field1</pdfaField:name>
            <pdfaField:valueType>Boolean</pdfaField:valueType>
            <pdfaField:description>d</pdfaField:description>
          </rdf:li></rdf:Seq></pdfaType:field>
        </rdf:li></rdf:Seq></pdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)
	}

	cases := []struct {
		name  string
		nsURI string
		vt    string
		fires bool
	}{
		{"no namespaceURI, custom value type", "", "CustomValueType", true},
		{"namespaceURI present, custom value type", "http://www.999.org/", "CustomValueType", false},
		{"no namespaceURI, predefined value type", "", "Text", false},
		// veraPDF simplifies a choice qualifier before the lookup, so a
		// "Closed Choice of" wrapper around a predefined type still resolves.
		{"no namespaceURI, choice of a predefined type", "", "Closed Choice of Integer", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for part, rule := range map[int]Rule{
				1: Rule_19005_1_6_7_8_t09,
				2: Rule_19005_2_6_6_2_3_3_t08,
				3: Rule_19005_3_6_6_2_3_3_t08,
			} {
				var got []Violation
				xmpCheckExtensionSchemas(context.Background(), entry(tc.nsURI, tc.vt), part, &got)
				if fired := hasRule(got, rule); fired != tc.fires {
					t.Errorf("part %d: undefined-value-type violation = %v, want %v (%v)",
						part, fired, tc.fires, got)
				}
			}
		})
	}
}
