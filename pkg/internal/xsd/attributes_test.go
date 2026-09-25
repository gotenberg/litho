package xsd

import (
	"context"
	"strings"
	"testing"
)

// inlineAttributeXSD declares attributes whose type is an xs:simpleType
// child: an enumerated token, and a restriction of a named type, whose base
// the link pass must still resolve.
const inlineAttributeXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="Code">
    <xs:restriction base="xs:token">
      <xs:maxLength value="3"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:complexType name="PriceType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute name="unit">
          <xs:simpleType>
            <xs:restriction base="xs:token">
              <xs:enumeration value="EA"/>
              <xs:enumeration value="KG"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:attribute>
        <xs:attribute name="list">
          <xs:simpleType>
            <xs:restriction base="Code">
              <xs:pattern value="[A-Z]+"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:attribute>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:element name="Price" type="PriceType"/>
</xs:schema>`

func validateInlineAttributes(t *testing.T, doc string) []Violation {
	t.Helper()
	vs, err := Validate(context.Background(), loadTestSchema(t, inlineAttributeXSD), []byte(doc))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return vs
}

// TestValidate_InlineAttributeTypeApplies pins XML Schema Part 1, 3.2.2: an
// attribute's xs:simpleType child is its {type definition}, so its facets
// and those of the named type it restricts judge the value.
func TestValidate_InlineAttributeTypeApplies(t *testing.T) {
	if vs := validateInlineAttributes(t, `<Price xmlns="urn:t" unit=" KG " list="ABC">1</Price>`); len(vs) != 0 {
		t.Errorf("valid attributes: got %v, want none", vs)
	}
	for doc, want := range map[string]string{
		`<Price xmlns="urn:t" unit="LB">1</Price>`:   "enumeration",
		`<Price xmlns="urn:t" list="abc">1</Price>`:  "pattern",
		`<Price xmlns="urn:t" list="ABCD">1</Price>`: "maximum 3",
	} {
		vs := validateInlineAttributes(t, doc)
		if len(vs) != 1 || vs[0].Rule != RuleAttribute || !strings.Contains(vs[0].Reason, want) {
			t.Errorf("%s: got %v, want one attribute violation naming %q", doc, vs, want)
		}
	}
}

// TestLoad_InlineAttributeTypeKept pins that the link pass keeps an
// attribute's xs:simpleType child rather than replacing it with
// xs:anySimpleType, and resolves the child's own base.
func TestLoad_InlineAttributeTypeKept(t *testing.T) {
	s := loadTestSchema(t, inlineAttributeXSD)
	ct, ok := s.Types[QName{Namespace: "urn:t", Local: "PriceType"}].(*ComplexType)
	if !ok {
		t.Fatal("PriceType is not a complex type")
	}
	for _, use := range ct.Attributes {
		st, ok := use.Type.(*SimpleType)
		if !ok {
			t.Errorf("attribute %s: type %T, want its inline *SimpleType", use.Name.Local, use.Type)
			continue
		}
		if st.Base == nil {
			t.Errorf("attribute %s: the inline type's base is unresolved", use.Name.Local)
		}
	}
}
