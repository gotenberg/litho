package xsd

import (
	"context"
	"encoding/xml"
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

// anyAttributeXSD exercises xs:anyAttribute (XML Schema Part 1, 3.10) and
// its derivation (3.4.2): Open admits other namespaces laxly, Skip admits
// anything, Strict requires a top-level declaration, Listed names its
// namespaces, Wide extends Listed with a second wildcard, Narrow restricts
// an open simple content type with none, UrExtension extends xs:anyType,
// and Closed declares no wildcard.
const anyAttributeXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:attribute name="level" type="xs:integer"/>
  <xs:complexType name="Open">
    <xs:sequence/>
    <xs:attribute name="id" type="xs:token"/>
    <xs:anyAttribute namespace="##other" processContents="lax"/>
  </xs:complexType>
  <xs:complexType name="Skip">
    <xs:sequence/>
    <xs:anyAttribute processContents="skip"/>
  </xs:complexType>
  <xs:complexType name="Strict">
    <xs:sequence/>
    <xs:anyAttribute namespace="##targetNamespace"/>
  </xs:complexType>
  <xs:complexType name="Listed">
    <xs:sequence/>
    <xs:anyAttribute namespace="##local urn:a" processContents="skip"/>
  </xs:complexType>
  <xs:complexType name="Wide">
    <xs:complexContent>
      <xs:extension base="Listed">
        <xs:anyAttribute namespace="urn:b" processContents="skip"/>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
  <xs:complexType name="OpenAmount">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:anyAttribute processContents="skip"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="Narrow">
    <xs:simpleContent>
      <xs:restriction base="OpenAmount"/>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="UrExtension">
    <xs:complexContent>
      <xs:extension base="xs:anyType">
        <xs:attribute name="id" type="xs:token"/>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
  <xs:complexType name="Closed">
    <xs:sequence/>
    <xs:attribute name="id" type="xs:token"/>
  </xs:complexType>
  <xs:complexType name="Root">
    <xs:sequence>
      <xs:element name="Open" type="Open" minOccurs="0"/>
      <xs:element name="Skip" type="Skip" minOccurs="0"/>
      <xs:element name="Strict" type="Strict" minOccurs="0"/>
      <xs:element name="Listed" type="Listed" minOccurs="0"/>
      <xs:element name="Wide" type="Wide" minOccurs="0"/>
      <xs:element name="OpenAmount" type="OpenAmount" minOccurs="0"/>
      <xs:element name="Narrow" type="Narrow" minOccurs="0"/>
      <xs:element name="UrExtension" type="UrExtension" minOccurs="0"/>
      <xs:element name="Closed" type="Closed" minOccurs="0"/>
      <xs:element name="Code" type="xs:token" minOccurs="0"/>
      <xs:element name="Any" minOccurs="0"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Root" type="Root"/>
</xs:schema>`

// validateAnyAttribute validates body as the content of a Root that
// declares the prefixes t (the target namespace), a, b and xsi.
func validateAnyAttribute(t *testing.T, body string) []Violation {
	t.Helper()
	doc := `<Root xmlns="urn:t" xmlns:t="urn:t" xmlns:a="urn:a" xmlns:b="urn:b"` +
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` + body + `</Root>`
	vs, err := Validate(context.Background(), loadTestSchema(t, anyAttributeXSD), []byte(doc))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return vs
}

// TestValidate_AttributeWildcardAdmits pins the attributes an attribute
// wildcard admits, per XML Schema Part 1, 3.10.4 and 3.10.1, and the
// derivation of the wildcard, per 3.4.2.
func TestValidate_AttributeWildcardAdmits(t *testing.T) {
	for name, body := range map[string]string{
		"##other lax, undeclared":          `<Open id="x" a:foo="1"/>`,
		"skip, any namespace":              `<Skip foo="1" t:bar="2" a:baz="3"/>`,
		"strict, declared at top level":    `<Strict t:level="7"/>`,
		"list, ##local and a named one":    `<Listed foo="1" a:bar="2"/>`,
		"extension unites the wildcards":   `<Wide foo="1" a:bar="2" b:baz="3"/>`,
		"simple content extension":         `<OpenAmount foo="1">1</OpenAmount>`,
		"ur-type admits anything":          `<Any foo="1" a:bar="2"/>`,
		"extending the ur-type inherits":   `<UrExtension id="x" foo="1"/>`,
		"exempt xsi attributes everywhere": `<Closed xsi:schemaLocation="urn:t t.xsd"/><Code xsi:type="xs:token">A</Code>`,
	} {
		if vs := validateAnyAttribute(t, body); len(vs) != 0 {
			t.Errorf("%s: got %v, want none", name, vs)
		}
	}
}

// TestValidate_AttributeWildcardRejects pins the attributes XML Schema
// Part 1, 3.4.4 (cvc-complex-type clause 3.2) and 3.3.4 (cvc-type clause
// 3.1.1) reject: one no use declares and no wildcard admits, one a strict
// wildcard admits with no top-level declaration, one whose top-level
// declaration its value fails, and any attribute of a simple-typed
// element.
func TestValidate_AttributeWildcardRejects(t *testing.T) {
	for body, want := range map[string]string{
		`<Closed foo="1"/>`:          "not declared",
		`<Open foo="1"/>`:            "not declared", // ##other excludes no namespace.
		`<Open t:foo="1"/>`:          "not declared", // ##other excludes the target namespace.
		`<Listed b:foo="1"/>`:        "not declared",
		`<Narrow foo="1">1</Narrow>`: "not declared", // A restriction drops the base wildcard.
		`<Strict t:other="1"/>`:      "no top-level declaration",
		`<Strict t:level="high"/>`:   "integer",
		`<Code foo="1">A</Code>`:     "simple type",
	} {
		vs := validateAnyAttribute(t, body)
		if len(vs) != 1 || vs[0].Rule != RuleAttribute || !strings.Contains(vs[0].Reason, want) {
			t.Errorf("%s: got %v, want one attribute violation naming %q", body, vs, want)
		}
	}
}

// TestValidate_LaxWildcardValidatesADeclaredAttribute pins XML Schema Part
// 1, 3.10.1: a lax wildcard validates an attribute whose top-level
// declaration it finds, and admits one it does not.
func TestValidate_LaxWildcardValidatesADeclaredAttribute(t *testing.T) {
	v := &validator{schema: loadTestSchema(t, anyAttributeXSD)}
	w := &attributeWildcardSet{
		alternatives:    []*AttributeWildcard{{Namespace: "##any"}},
		processContents: "lax",
	}
	attr := xml.Attr{Name: xml.Name{Space: "urn:t", Local: "level"}, Value: "x"}
	if vs := v.undeclaredAttribute(attr, w, "/Root"); len(vs) != 1 {
		t.Errorf("declared attribute, bad value: got %v, want one violation", vs)
	}
	attr.Name.Local = "unknown"
	if vs := v.undeclaredAttribute(attr, w, "/Root"); len(vs) != 0 {
		t.Errorf("undeclared attribute: got %v, want none", vs)
	}
}
