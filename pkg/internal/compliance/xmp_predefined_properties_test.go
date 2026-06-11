package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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

// TestXMPPredefinedProperties_PdfaidRev pins the pdfaid:rev gap: the
// property exists only in the PDF/A-4 identification schema. The XMP
// 2005 catalog predefines pdfaid part/conformance/amd/corr, so a
// leftover rev fails §6.6.2.3.1 t1 (name) and t2 (type, via the
// registry narrowing) under parts 2/3, exactly as veraPDF 1.30 reports
// (verified live on PDF_A-4__veraPDF_test_suite_6-1-11-t01-pass-a
// retargeted to PDF/A-2b).
func TestXMPPredefinedProperties_PdfaidRev(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
      <pdfaid:part>2</pdfaid:part>
      <pdfaid:conformance>B</pdfaid:conformance>
      <pdfaid:rev>2020</pdfaid:rev>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 2, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("pdfaid:rev under PDF/A-2: got %v, want §6.6.2.3.1 t1", got)
	}
	if len(got) != 1 {
		t.Errorf("only pdfaid:rev may be flagged, got %v", got)
	}

	var gotTypes []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, &gotTypes)
	if !hasRule(gotTypes, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("pdfaid:rev under PDF/A-2: got %v, want §6.6.2.3.1 t2", gotTypes)
	}

	// Part 4 predefines rev: both checks stay silent.
	var got4 []Violation
	xmpCheckPredefinedProperties(context.Background(), data, 4, &got4)
	xmpCheckPropertyTypes(data, 4, ast.PdfStream{}, &got4)
	if len(got4) != 0 {
		t.Errorf("pdfaid:rev is predefined for PDF/A-4, got %v", got4)
	}
}

// TestXMPPropertyTypes_SupplementalCategoriesBag pins the removed
// simple-as-Bag exemption: veraPDF 1.30 fails isValueTypeCorrect for a
// simple-valued photoshop:SupplementalCategories under parts 2/3
// (verified live on PDF_A-1b__veraPDF_test_suite_6-7-2-t13-pass-l
// retargeted to PDF/A-2b), while part 1 reads the property as plain
// Text per the 2004 XMP Specification.
func TestXMPPropertyTypes_SupplementalCategoriesBag(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/">
      <photoshop:SupplementalCategories>news</photoshop:SupplementalCategories>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("simple SupplementalCategories under PDF/A-2: got %v, want §6.6.2.3.1 t2", got)
	}

	var got1 []Violation
	xmpCheckPropertyTypes(data, 1, ast.PdfStream{}, &got1)
	if hasRule(got1, Rule_19005_1_6_7_9_t03) {
		t.Errorf("2004 XMP types SupplementalCategories as Text, got %v", got1)
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
