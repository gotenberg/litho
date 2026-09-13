package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestExtensionSchema_DefectiveEntryLegitimisesNothing pins when an extension
// schema stops legitimising the properties of its namespace. veraPDF's
// isDefinedInCurrentPackage is namespace-level, not per property name, so a
// well-formed entry covers names it never lists; what switches it off is a
// DEFECTIVE declaration (ISO 19005-1 §6.7.9 t2, ISO 19005-2/3 §6.6.2.3.1 t1).
//
// Three shapes from the veraPDF test suite establish it. 6-6-2-3-3-t02-fail-c
// opens a property item with no pdfaProperty:name and 6-6-2-3-3-t03-fail-e one
// whose value type nothing defines, and veraPDF reports the used property in
// both. 6-6-2-3-2-t01-pass-d declares only CustomNumber yet uses cs:CProp, and
// veraPDF passes it.
func TestExtensionSchema_DefectiveEntryLegitimisesNothing(t *testing.T) {
	t.Parallel()

	const ns = "http://www.custom.com/ns/machines/2/"

	// packet declares one property item with the given name and value type
	// (either omitted when empty) plus the custom type ArticleNumber, then uses
	// the property named by used.
	packet := func(propName, propVT, used string) []byte {
		name, vt := "", ""
		if propName != "" {
			name = fmt.Sprintf("<pdfaProperty:name>%s</pdfaProperty:name>", propName)
		}
		if propVT != "" {
			vt = fmt.Sprintf("<pdfaProperty:valueType>%s</pdfaProperty:valueType>", propVT)
		}
		return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>Custom schema</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>` + ns + `</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>cs</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          ` + name + vt + `
          <pdfaProperty:category>internal</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
        <pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaType:type>ArticleNumber</pdfaType:type>
          <pdfaType:namespaceURI>http://www.custom.com/1/</pdfaType:namespaceURI>
          <pdfaType:prefix>an</pdfaType:prefix>
          <pdfaType:description>d</pdfaType:description>
          <pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource">
            <pdfaField:name>f</pdfaField:name>
            <pdfaField:valueType>Text</pdfaField:valueType>
            <pdfaField:description>d</pdfaField:description>
          </rdf:li></rdf:Seq></pdfaType:field>
        </rdf:li></rdf:Seq></pdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>
    <rdf:Description rdf:about="" xmlns:cs="` + ns + `"><cs:` + used + `>v</cs:` + used + `></rdf:Description>`)
	}

	cases := []struct {
		name     string
		propName string
		propVT   string
		used     string
		fires    bool
	}{
		{"well formed, same name used", "CustomNumber", "Text", "CustomNumber", false},
		{"well formed, another name used", "CustomNumber", "Text", "CProp", false},
		{"declaration with no name", "", "Text", "CProp", true},
		{"declaration with an undefined value type", "CustomNumber", "NoSuchType", "CProp", true},
		{"declaration naming the schema's own custom type", "CustomNumber", "ArticleNumber", "CProp", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for part, rule := range map[int]Rule{
				1: Rule_19005_1_6_7_9_t02,
				2: Rule_19005_2_6_6_2_3_1_t01,
			} {
				var got []Violation
				xmpCheckPredefinedProperties(context.Background(),
					packet(tc.propName, tc.propVT, tc.used), part, nil, nil, &got)
				if fired := hasRule(got, rule); fired != tc.fires {
					t.Errorf("part %d: undeclared-property violation = %v, want %v (%v)",
						part, fired, tc.fires, got)
				}
			}
		})
	}
}

// TestExtensionSchema_MainPackageTypeResolves pins the isDefinedInMainPackage
// half: a value type the catalog packet declares resolves for another packet
// that names it, so the namespace stays usable there. The veraPDF test suite's
// 6-6-2-3-2-t01-pass-d.pdf declares ArticleNumber in the catalog packet and
// names it from a page packet that declares only CValue, and veraPDF passes it.
func TestExtensionSchema_MainPackageTypeResolves(t *testing.T) {
	t.Parallel()

	const ns = "http://www.custom.com/ns/machines/2/"
	page := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>Custom schema</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>` + ns + `</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>cs</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaProperty:name>CProp</pdfaProperty:name>
          <pdfaProperty:valueType>ArticleNumber</pdfaProperty:valueType>
          <pdfaProperty:category>internal</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>
    <rdf:Description rdf:about="" xmlns:cs="` + ns + `"><cs:CProp>v</cs:CProp></rdf:Description>`)

	// Without the catalog's types, ArticleNumber resolves nowhere.
	var alone []Violation
	xmpCheckPredefinedProperties(context.Background(), page, 2, map[string]bool{ns: true}, nil, &alone)
	if !hasRule(alone, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("an unresolvable value type must fire, got %v", alone)
	}

	// The catalog packet declaring ArticleNumber makes it resolve.
	var withMain []Violation
	xmpCheckPredefinedProperties(context.Background(), page, 2,
		map[string]bool{ns: true}, map[string]bool{"articlenumber": true}, &withMain)
	if hasRule(withMain, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("a value type the catalog packet declares must resolve, got %v", withMain)
	}
}
