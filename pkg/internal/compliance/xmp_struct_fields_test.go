package compliance

import "testing"

// extSchemaFieldlessType declares property1 of a fieldless custom value type.
// The optional usage string appends a property usage block.
//
// # Corpus evidence
//
//   - verapdf 6-6-2-3-3-t03-fail-b: the fixture this models. Its original
//     uses fs:property1 and its remediated output drops the usage.
func extSchemaFieldlessType(usage string) []byte {
	return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#">
      <pdfaExtension:schemas>
        <rdf:Bag>
          <rdf:li rdf:parseType="Resource">
            <pdfaSchema:namespaceURI>http://www.999.org/</pdfaSchema:namespaceURI>
            <pdfaSchema:prefix>fs</pdfaSchema:prefix>
            <pdfaSchema:property>
              <rdf:Seq>
                <rdf:li rdf:parseType="Resource">
                  <pdfaProperty:name>property1</pdfaProperty:name>
                  <pdfaProperty:valueType>CustomValueType</pdfaProperty:valueType>
                </rdf:li>
              </rdf:Seq>
            </pdfaSchema:property>
            <pdfaSchema:valueType>
              <rdf:Seq>
                <rdf:li rdf:parseType="Resource">
                  <pdfaType:type>CustomValueType</pdfaType:type>
                  <pdfaType:namespaceURI>http://www.custom.com/1/</pdfaType:namespaceURI>
                  <pdfaType:prefix>cvt</pdfaType:prefix>
                </rdf:li>
              </rdf:Seq>
            </pdfaSchema:valueType>
          </rdf:li>
        </rdf:Bag>
      </pdfaExtension:schemas>
    </rdf:Description>` + usage)
}

// TestXMPExtensionPropertyTypes_FieldlessTypeUnused pins Bug A: §6.6.2.3.1 t2 is
// a usage rule, so a fieldless custom value type whose property is never used
// must not be flagged.
//
// # Corpus evidence
//
//   - verapdf 6-6-2-3-3-t03-fail-b: veraPDF judges the remediated output, where
//     the property is never used, compliant.
func TestXMPExtensionPropertyTypes_FieldlessTypeUnused(t *testing.T) {
	var got []Violation
	packet := extSchemaFieldlessType("")
	xmpCheckUndeclaredExtensionProperties(packet, 2, nil, nil, &got)
	xmpCheckExtensionValueTypes(packet, 2, nil, &got)
	if hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("unused fieldless value type must not fail §6.6.2.3.1 t2: %v", got)
	}
}

// TestXMPExtensionPropertyTypes_FieldlessTypeUsed is the positive control: when
// fs:property1 is used as a structure, the fieldless type, which is Text,
// cannot satisfy the usage and the property fails §6.6.2.3.1 t2, stated once
// by the two checks together.
//
// # Corpus evidence
//
//   - verapdf 6-6-2-3-3-t03-fail-b: the original uses fs:property1, and
//     veraPDF's verdict on it is non-compliant.
func TestXMPExtensionPropertyTypes_FieldlessTypeUsed(t *testing.T) {
	usage := `
    <rdf:Description rdf:about=""
      xmlns:fs="http://www.999.org/" xmlns:cvt="http://www.custom.com/1/">
      <fs:property1 rdf:parseType="Resource">
        <cvt:field1>True</cvt:field1>
      </fs:property1>
    </rdf:Description>`
	var got []Violation
	packet := extSchemaFieldlessType(usage)
	xmpCheckUndeclaredExtensionProperties(packet, 2, nil, nil, &got)
	xmpCheckExtensionValueTypes(packet, 2, nil, &got)
	if n := countRule(got, Rule_19005_2_6_6_2_3_1_t02); n != 1 {
		t.Errorf("used fieldless value type must fail §6.6.2.3.1 t2 once, got %d: %v", n, got)
	}
}

// TestXMPStructFieldNames_ResourceEventBadField pins Bug B: a ResourceEvent
// struct field misspelled as stEvt:When (the member is stEvt:when) makes the
// enclosing xmpMM:History property not correspond to its type, which veraPDF
// flags under §6.6.2.3.1 t2 for parts 2/3. Part 4 drops the requirement.
func TestXMPStructFieldNames_ResourceEventBadField(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li rdf:parseType="Resource">
            <stEvt:action>created</stEvt:action>
            <stEvt:When>2016-04-05T13:19:21+01:00</stEvt:When>
          </rdf:li>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, nil, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("stEvt:When (member is stEvt:when) must fail §6.6.2.3.1 t2: %v", got)
	}

	var got4 []Violation
	xmpCheckStructFieldNames(data, 4, nil, &got4)
	if len(got4) != 0 {
		t.Errorf("part 4 drops struct-field validation: %v", got4)
	}
}

// TestXMPStructFieldNames_ValidStructsPass guards against false positives: a
// ResourceEvent with only valid members and a ResourceRef-typed direct struct
// (xmpMM:DerivedFrom) with valid members must not be flagged.
func TestXMPStructFieldNames_ValidStructsPass(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#"
      xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li rdf:parseType="Resource">
            <stEvt:action>created</stEvt:action>
            <stEvt:when>2016-04-05T13:19:21+01:00</stEvt:when>
            <stEvt:softwareAgent>litho</stEvt:softwareAgent>
            <stEvt:instanceID>uuid:1</stEvt:instanceID>
          </rdf:li>
        </rdf:Seq>
      </xmpMM:History>
      <xmpMM:DerivedFrom rdf:parseType="Resource">
        <stRef:instanceID>uuid:2</stRef:instanceID>
        <stRef:documentID>uuid:3</stRef:documentID>
      </xmpMM:DerivedFrom>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, nil, &got)
	if len(got) != 0 {
		t.Errorf("valid struct fields must not be flagged: %v", got)
	}
}

// TestXMPStructFieldNames_UnknownNamespaceIgnored guards the scope: fields in a
// namespace that is not a recognized XMP value type are not validated here
// (they are governed by other checks), so they never raise a struct-field
// violation regardless of name.
func TestXMPStructFieldNames_UnknownNamespaceIgnored(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:custom="http://example.com/custom/"
      xmlns:cvt="http://www.custom.com/1/">
      <custom:thing rdf:parseType="Resource">
        <cvt:AnythingGoes>x</cvt:AnythingGoes>
      </custom:thing>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, nil, &got)
	if len(got) != 0 {
		t.Errorf("non-value-type namespace fields must be ignored here: %v", got)
	}
}
