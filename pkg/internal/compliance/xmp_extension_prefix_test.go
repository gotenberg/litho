package compliance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestExtensionSchemaPrefix_UnusedDecoyBindingIgnored pins the
// usage-gated prefix check: an xmlns declaration aliasing the pdfaType
// URI to a non-canonical prefix is harmless while no element uses it
// (this is also the leftover shape remediation produces when it empties
// a defective rdf:Description). ISO 19005-2 §6.6.2.3.3 constrains the
// prefixes that extension-schema elements actually carry, not bare
// namespace declarations, so the check evaluates prefixes per element.
// Firing on the unused declaration would flag a binding the document
// never relies on.
func TestExtensionSchemaPrefix_UnusedDecoyBindingIgnored(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
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
    </rdf:Description>
    <rdf:Description rdf:about="" xmlns:nonpdfaType="http://www.aiim.org/pdfa/ns/type#"></rdf:Description>`)

	var got []Violation
	xmpCheckExtensionSchemas(context.Background(), data, 2, &got)
	if len(got) != 0 {
		t.Errorf("unused decoy binding must not fire, got %v", got)
	}
}

// TestExtensionSchemaPrefix_UsedWrongPrefixStillFires pins the positive
// side: when elements actually use the non-canonical prefix, the family
// fires (golden behaviour of the wrong-prefix fixtures).
func TestExtensionSchemaPrefix_UsedWrongPrefixStillFires(t *testing.T) {
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
        </rdf:li></rdf:Seq></pdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	var got []Violation
	xmpCheckExtensionSchemas(context.Background(), data, 2, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_3_t14) {
		t.Errorf("used wrong pdfaType prefix must fire the valueType family, got %v", got)
	}
}

// TestExtensionSchema_MultipleDefsAfterNestedContainer pins the
// container-stack walk: a second valueType definition following one
// whose pdfaType:field Seq nested another container keeps its level (a
// single-flag tracker evaluated it at the wrong level and stormed
// phantom missing-field violations). A genuinely incomplete SECOND
// definition still fires.
//
// # Corpus evidence
//
//   - All corpora: the single-flag tracker's phantom missing-field
//     violations spanned 792 cells per profile.
func TestExtensionSchema_MultipleDefsAfterNestedContainer(t *testing.T) {
	packet := func(secondDef string) []byte {
		return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>s</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>http://x/</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>x</pdfaSchema:prefix>
        <pdfaSchema:valueType><rdf:Seq>
          <rdf:li rdf:parseType="Resource">
            <pdfaType:type>First</pdfaType:type>
            <pdfaType:namespaceURI>http://t/</pdfaType:namespaceURI>
            <pdfaType:prefix>tp</pdfaType:prefix>
            <pdfaType:description>d</pdfaType:description>
            <pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource">
              <pdfaField:name>f1</pdfaField:name>
              <pdfaField:valueType>Text</pdfaField:valueType>
              <pdfaField:description>fd</pdfaField:description>
            </rdf:li></rdf:Seq></pdfaType:field>
          </rdf:li>
          <rdf:li rdf:parseType="Resource">` + secondDef + `</rdf:li>
        </rdf:Seq></pdfaSchema:valueType>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)
	}

	complete := `<pdfaType:type>Second</pdfaType:type>
            <pdfaType:namespaceURI>http://t2/</pdfaType:namespaceURI>
            <pdfaType:prefix>t2</pdfaType:prefix>
            <pdfaType:description>d2</pdfaType:description>`
	var got []Violation
	xmpCheckExtensionSchemas(context.Background(), packet(complete), 2, &got)
	if len(got) != 0 {
		t.Errorf("complete second definition must be silent, got %v", got)
	}

	incomplete := `<pdfaType:type>Second</pdfaType:type>
            <pdfaType:namespaceURI>http://t2/</pdfaType:namespaceURI>
            <pdfaType:prefix>t2</pdfaType:prefix>`
	var got2 []Violation
	xmpCheckExtensionSchemas(context.Background(), packet(incomplete), 2, &got2)
	if !hasRule(got2, Rule_19005_2_6_6_2_3_3_t14) {
		t.Errorf("description-less second definition must fire t14, got %v", got2)
	}
	for _, v := range got2 {
		if v.Rule == Rule_19005_2_6_6_2_3_3_t11 || v.Rule == Rule_19005_2_6_6_2_3_3_t12 || v.Rule == Rule_19005_2_6_6_2_3_3_t13 {
			t.Errorf("only the description is missing; got phantom %v", v)
		}
	}
}

// TestExtensionSchemaPrefix_ReportsOnlyTheFieldsCarryingIt pins two facets of
// the wrong-prefix scan that veraPDF 1.30.2 settles. Each test of ISO 19005-1
// §6.7.8 and ISO 19005-2/3 §6.6.2.3.3 names one field, so only the fields the
// packet actually carries under the wrong prefix may be reported: emitting
// every field of the level invented violations for fields that are correct.
// And a wrong prefix at one level must not stop the scan, or every other defect
// in the packet goes unreported.
func TestExtensionSchemaPrefix_ReportsOnlyTheFieldsCarryingIt(t *testing.T) {
	t.Parallel()

	// Only pdfaSchema:schema carries the non-canonical prefix; namespaceURI,
	// prefix and property are all correct, so only t2 may fire (part 2).
	oneField := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:xSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <xSchema:schema>s</xSchema:schema>
        <pdfaSchema:namespaceURI>http://x/</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>x</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaProperty:name>n</pdfaProperty:name>
          <pdfaProperty:valueType>Text</pdfaProperty:valueType>
          <pdfaProperty:category>external</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	var got []Violation
	xmpCheckExtensionSchemas(context.Background(), oneField, 2, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_3_t02) {
		t.Errorf("the misprefixed schema field must fire §6.6.2.3.3 t2: got %v", got)
	}
	for _, r := range []Rule{Rule_19005_2_6_6_2_3_3_t03, Rule_19005_2_6_6_2_3_3_t04} {
		if hasRule(got, r) {
			t.Errorf("field %s carries the canonical prefix and must not fire: got %v", r.Description, got)
		}
	}

	// Both the schema and the property level are misprefixed. Reporting the
	// first level and returning hid the second entirely.
	twoLevels := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:xSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:xProp="http://www.aiim.org/pdfa/ns/property#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <xSchema:schema>s</xSchema:schema>
        <xSchema:namespaceURI>http://x/</xSchema:namespaceURI>
        <xSchema:prefix>x</xSchema:prefix>
        <xSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <xProp:name>n</xProp:name>
          <xProp:valueType>Text</xProp:valueType>
          <xProp:category>external</xProp:category>
          <xProp:description>d</xProp:description>
        </rdf:li></rdf:Seq></xSchema:property>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	var both []Violation
	xmpCheckExtensionSchemas(context.Background(), twoLevels, 2, &both)
	if !hasRule(both, Rule_19005_2_6_6_2_3_3_t02) {
		t.Errorf("schema level must still fire: got %v", both)
	}
	if !hasRule(both, Rule_19005_2_6_6_2_3_3_t07) {
		t.Errorf("the property level must not be masked by the schema level: got %v", both)
	}

	// The same bytes must always produce the same violations. Ranging the
	// prefix map and returning on the first hit made this depend on Go's
	// randomised map iteration order.
	first := violationRuleSet(both)
	for range 32 {
		var again []Violation
		xmpCheckExtensionSchemas(context.Background(), twoLevels, 2, &again)
		if got := violationRuleSet(again); got != first {
			t.Fatalf("nondeterministic output: %q then %q", first, got)
		}
	}
}

// violationRuleSet renders a violation slice as a sorted, comparable key.
func violationRuleSet(vs []Violation) string {
	keys := make([]string, 0, len(vs))
	for _, v := range vs {
		keys = append(keys, fmt.Sprintf("%s-t%d", v.Rule.Clause, v.Rule.TestNumber))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
