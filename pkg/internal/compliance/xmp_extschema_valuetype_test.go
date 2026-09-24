package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestExtensionSchemaPrefix_ValueTypeFieldReported pins the last field of the
// PDF/A Schema value type. ISO 19005-1 §6.7.8 t7 and ISO 19005-2/3 §6.6.2.3.3
// t6 require pdfaSchema:valueType to carry the canonical prefix exactly as the
// schema, namespaceURI, prefix and property fields do.
//
// [TestExtensionSchemaPrefix_TypeFieldFieldReported] covers the sibling gap in
// the pdfaType level, found by auditing every field list against its switch.
//
// The field was listed in [xmpExtensionLevels] but [xmpExtSchemaRule] mapped no
// rule to it, so litho was silent on valueType alone.
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-7-8-t06-fail-a.pdf: writes every
//     field under "nonpdfaSchema", and litho reported four violations where
//     veraPDF reports five, silent on valueType alone.
func TestExtensionSchemaPrefix_ValueTypeFieldReported(t *testing.T) {
	t.Parallel()

	// Every pdfaSchema field carried by a non-canonical prefix.
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:nonpdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <nonpdfaSchema:schema>s</nonpdfaSchema:schema>
        <nonpdfaSchema:namespaceURI>http://x/</nonpdfaSchema:namespaceURI>
        <nonpdfaSchema:prefix>x</nonpdfaSchema:prefix>
        <nonpdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaProperty:name>N</pdfaProperty:name>
          <pdfaProperty:valueType>T</pdfaProperty:valueType>
          <pdfaProperty:category>external</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></nonpdfaSchema:property>
        <nonpdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaType:type>T</pdfaType:type>
          <pdfaType:namespaceURI>http://t/</pdfaType:namespaceURI>
          <pdfaType:prefix>tp</pdfaType:prefix>
          <pdfaType:description>d</pdfaType:description>
        </rdf:li></rdf:Seq></nonpdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	for _, tc := range []struct {
		part int
		rule Rule
	}{
		{1, Rule_19005_1_6_7_8_t07},
		{2, Rule_19005_2_6_6_2_3_3_t06},
		{3, Rule_19005_3_6_6_2_3_3_t06},
	} {
		t.Run(fmt.Sprintf("part %d", tc.part), func(t *testing.T) {
			t.Parallel()
			var got []Violation
			xmpCheckExtensionSchemas(context.Background(), data, tc.part, &got)
			if !hasRule(got, tc.rule) {
				t.Errorf("valueType under a non-canonical prefix must report %s %s t%d, got %v",
					tc.rule.Specification, tc.rule.Clause, tc.rule.TestNumber, got)
			}
		})
	}

	// The canonical prefix keeps every field silent, so the new case cannot
	// fire on a well-formed extension schema.
	clean := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>s</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>http://x/</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>x</pdfaSchema:prefix>
        <pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaType:type>T</pdfaType:type>
          <pdfaType:namespaceURI>http://t/</pdfaType:namespaceURI>
          <pdfaType:prefix>tp</pdfaType:prefix>
          <pdfaType:description>d</pdfaType:description>
        </rdf:li></rdf:Seq></pdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	var clean1 []Violation
	xmpCheckExtensionSchemas(context.Background(), clean, 1, &clean1)
	if hasRule(clean1, Rule_19005_1_6_7_8_t07) {
		t.Errorf("a canonical pdfaSchema:valueType must not fire, got %v", clean1)
	}
}

// TestExtensionSchemaPrefix_TypeFieldFieldReported pins the pdfaType level's
// last field. ISO 19005-1 §6.7.8 t16 and ISO 19005-2/3 §6.6.2.3.3 t15 require
// pdfaType:field to carry the canonical prefix, exactly as type, namespaceURI,
// prefix and description do.
//
// [xmpExtensionLevels] listed "field" for the pdfaType level but
// [xmpExtValueTypeRule] mapped no rule to it, the same dead-mapping shape that
// hid pdfaSchema:valueType. Both were found by comparing each level's field
// list against the cases of the function that maps them.
func TestExtensionSchemaPrefix_TypeFieldFieldReported(t *testing.T) {
	t.Parallel()

	// The pdfaType elements carry a non-canonical prefix; every other level
	// stays canonical so only the pdfaType rules can fire.
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:wrongType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>s</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>http://x/</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>x</pdfaSchema:prefix>
        <pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource">
          <wrongType:type>T</wrongType:type>
          <wrongType:namespaceURI>http://t/</wrongType:namespaceURI>
          <wrongType:prefix>tp</wrongType:prefix>
          <wrongType:description>d</wrongType:description>
          <wrongType:field><rdf:Seq><rdf:li rdf:parseType="Resource">
            <pdfaField:name>f</pdfaField:name>
            <pdfaField:valueType>Text</pdfaField:valueType>
            <pdfaField:description>d</pdfaField:description>
          </rdf:li></rdf:Seq></wrongType:field>
        </rdf:li></rdf:Seq></pdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	for _, tc := range []struct {
		part int
		rule Rule
	}{
		{1, Rule_19005_1_6_7_8_t16},
		{2, Rule_19005_2_6_6_2_3_3_t15},
		{3, Rule_19005_3_6_6_2_3_3_t15},
	} {
		t.Run(fmt.Sprintf("part %d", tc.part), func(t *testing.T) {
			t.Parallel()
			var got []Violation
			xmpCheckExtensionSchemas(context.Background(), data, tc.part, &got)
			if !hasRule(got, tc.rule) {
				t.Errorf("pdfaType:field under a non-canonical prefix must report %s %s t%d, got %v",
					tc.rule.Specification, tc.rule.Clause, tc.rule.TestNumber, got)
			}
		})
	}
}
