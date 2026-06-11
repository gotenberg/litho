package compliance

import (
	"context"
	"testing"
)

// exifDescription wraps property markup in an rdf:Description binding the
// exif prefix.
func exifDescription(body string) []byte {
	return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:exif="http://ns.adobe.com/exif/1.0/">
      ` + body + `
    </rdf:Description>`)
}

// TestXMPPredefinedProperties_MakerNoteFlagged pins the property-level
// half of ISO 19005-2 §6.6.2.3.1 t1: exif:MakerNote sits in a predefined
// namespace but the XMP 2005 Specification does not define it, so PDF/A-2
// must flag it like an unknown-namespace property.
func TestXMPPredefinedProperties_MakerNoteFlagged(t *testing.T) {
	data := exifDescription(`<exif:MakerNote>opaque</exif:MakerNote>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 2, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("exif:MakerNote under PDF/A-2: got %v, want §6.6.2.3.1 t1", got)
	}

	// Part 3 mirrors part 2 with its own rule id.
	var got3 []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 3, &got3)
	if !hasRule(got3, Rule_19005_3_6_6_2_3_1_t01) {
		t.Errorf("exif:MakerNote under PDF/A-3: got %v, want §6.6.2.3.1 t1", got3)
	}

	// XMP 2004 (PDF/A-1) predefines exif:MakerNote: no violation.
	var got1 []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 1, &got1)
	if len(got1) != 0 {
		t.Errorf("exif:MakerNote under PDF/A-1 is predefined in XMP 2004, got %v", got1)
	}
}

// TestXMPPredefinedProperties_PredefinedNotFlagged pins the negative case:
// a property the XMP 2005 Specification defines passes untouched.
func TestXMPPredefinedProperties_PredefinedNotFlagged(t *testing.T) {
	data := exifDescription(`<exif:DateTimeOriginal>2024-01-01</exif:DateTimeOriginal>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 2, &got)
	if len(got) != 0 {
		t.Errorf("exif:DateTimeOriginal is predefined, got %v", got)
	}
}

// TestXMPPredefinedProperties_Part4NeverFlags pins the ISO 19005-4 carve
// out: part 4 has no predefined-schema rule, so even exif:MakerNote stays
// silent.
func TestXMPPredefinedProperties_Part4NeverFlags(t *testing.T) {
	data := exifDescription(`<exif:MakerNote>opaque</exif:MakerNote>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 4, &got)
	if len(got) != 0 {
		t.Errorf("part 4 carries no predefined-schema rule, got %v", got)
	}
}

// TestXMPPredefinedProperties_ExtensionSchemaExtendsPredefined pins
// veraPDF's acceptance of extension schemas that extend predefined
// namespaces: once the packet declares the exif namespace in
// pdfaExtension:schemas, exif:MakerNote counts as declared.
func TestXMPPredefinedProperties_ExtensionSchemaExtendsPredefined(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:exif="http://ns.adobe.com/exif/1.0/"
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
      <exif:MakerNote>opaque</exif:MakerNote>
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>exif</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>http://ns.adobe.com/exif/1.0/</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>exif</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaProperty:name>MakerNote</pdfaProperty:name>
          <pdfaProperty:valueType>Text</pdfaProperty:valueType>
          <pdfaProperty:category>internal</pdfaProperty:category>
          <pdfaProperty:description>opaque maker data</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 2, &got)
	if hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("extension-declared exif:MakerNote must not be flagged, got %v", got)
	}
}

// TestXMPPredefinedProperties_NestedFieldsInvisible pins the top-level
// scoping: structure fields, including ones serialized through a nested
// rdf:Description, are not properties and must not be flagged even when
// their local names are absent from the predefined catalogs.
func TestXMPPredefinedProperties_NestedFieldsInvisible(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:exif="http://ns.adobe.com/exif/1.0/"
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
      <exif:Flash><rdf:Description><exif:Fired>True</exif:Fired></rdf:Description></exif:Flash>
      <xmpMM:DerivedFrom rdf:parseType="Resource">
        <stRef:instanceID>uuid:1</stRef:instanceID>
      </xmpMM:DerivedFrom>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 2, &got)
	if len(got) != 0 {
		t.Errorf("structure fields must stay invisible to the property audit, got %v", got)
	}
}

// TestXMPPredefinedProperties_AttributeForm pins attribute-form coverage:
// an undefined property serialized as an attribute on the top-level
// rdf:Description is still a property.
func TestXMPPredefinedProperties_AttributeForm(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
      pdf:Producer="litho" pdf:Trapped="False"/>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 2, &got)
	// pdf:Trapped is not predefined by the XMP 2005 catalog veraPDF uses;
	// pdf:Producer is.
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("attribute-form pdf:Trapped: got %v, want §6.6.2.3.1 t1", got)
	}
	if len(got) != 1 {
		t.Errorf("only pdf:Trapped may be flagged, got %v", got)
	}
}
