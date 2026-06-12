package compliance

import (
	"context"
	"testing"
)

// TestExtensionSchemaPrefix_UnusedDecoyBindingIgnored pins the
// usage-gated prefix check: an xmlns declaration aliasing the pdfaType
// URI to a non-canonical prefix is harmless while no element uses it
// (decoy shape of the veraPDF 6-6-2-3-3 suite, and the leftover shape
// remediation produces when it empties a defective rdf:Description).
// Firing on the bare declaration produced a 266-cell-per-profile
// litho-only pool; veraPDF checks prefixes per element.
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
