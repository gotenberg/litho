package xsd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// unionXSD mirrors ZUGFeRD 1.0's DateMandatoryDateTimeType, a union of
// xs:dateTime and xs:date, and adds a union with an inline member, a
// restriction of that union, a union nesting another, and an attribute of
// union type.
const unionXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="DateMandatoryDateTimeType">
    <xs:union memberTypes="xs:dateTime xs:date"/>
  </xs:simpleType>
  <xs:simpleType name="IntOrNA">
    <xs:union memberTypes="xs:integer">
      <xs:simpleType>
        <xs:restriction base="xs:token">
          <xs:enumeration value="N/A"/>
        </xs:restriction>
      </xs:simpleType>
    </xs:union>
  </xs:simpleType>
  <xs:simpleType name="SmallSet">
    <xs:restriction base="IntOrNA">
      <xs:enumeration value="1"/>
      <xs:enumeration value="N/A"/>
      <xs:maxLength value="0"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:simpleType name="Nested">
    <xs:union memberTypes="DateMandatoryDateTimeType xs:boolean"/>
  </xs:simpleType>
  <xs:complexType name="StampType">
    <xs:simpleContent>
      <xs:extension base="xs:token">
        <xs:attribute name="at" type="DateMandatoryDateTimeType"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="InvoiceType">
    <xs:sequence>
      <xs:element name="IssueDateTime" type="DateMandatoryDateTimeType" minOccurs="0"/>
      <xs:element name="Count" type="IntOrNA" minOccurs="0"/>
      <xs:element name="Small" type="SmallSet" minOccurs="0"/>
      <xs:element name="Nested" type="Nested" minOccurs="0"/>
      <xs:element name="Stamp" type="StampType" minOccurs="0"/>
      <xs:element name="Due" type="DateMandatoryDateTimeType" fixed="2026-01-01" minOccurs="0"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func validateUnion(t *testing.T, body string) []Violation {
	t.Helper()
	vs, err := Validate(context.Background(), loadTestSchema(t, unionXSD), []byte(`<Invoice xmlns="urn:t">`+body+`</Invoice>`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return vs
}

// TestValidate_UnionAcceptsAnyMember pins XML Schema Part 2, 2.5.1.3: a
// value is valid for a union when it is valid for at least one member
// type, normalized as that member normalizes it, and the facets a
// restriction of the union adds apply to it (4.1.5).
func TestValidate_UnionAcceptsAnyMember(t *testing.T) {
	for name, body := range map[string]string{
		"dateTime member":             `<IssueDateTime>2026-01-01T10:00:00</IssueDateTime>`,
		"date member":                 `<IssueDateTime>2026-01-01</IssueDateTime>`,
		"member collapses its space":  `<IssueDateTime> 2026-01-01 </IssueDateTime>`,
		"memberTypes member":          `<Count>12</Count>`,
		"inline member":               `<Count>N/A</Count>`,
		"restriction, first member":   `<Small> 1 </Small>`,
		"restriction, inline member":  `<Small>N/A</Small>`,
		"nested union":                `<Nested>2026-01-01</Nested>`,
		"nested union, other member":  `<Nested>true</Nested>`,
		"attribute of union type":     `<Stamp at="2026-01-01T10:00:00Z">x</Stamp>`,
		"fixed value, same value":     `<Due>2026-01-01</Due>`,
		"fixed value, empty takes it": `<Due/>`,
	} {
		if vs := validateUnion(t, body); len(vs) != 0 {
			t.Errorf("%s: got %v, want none", name, vs)
		}
	}
}

// TestValidate_UnionRejectsEveryMemberMiss pins that a value no member
// type validates is outside the union's lexical space, and that a
// restriction of a union keeps its enumeration.
func TestValidate_UnionRejectsEveryMemberMiss(t *testing.T) {
	for _, tc := range []struct {
		body, rule, reason string
	}{
		{`<IssueDateTime>20260101</IssueDateTime>`, RuleLexical, "none of the union's member types"},
		{`<IssueDateTime>2026-02-30</IssueDateTime>`, RuleLexical, "none of the union's member types"},
		{`<IssueDateTime/>`, RuleLexical, "none of the union's member types"},
		{`<Count>many</Count>`, RuleLexical, "none of the union's member types"},
		{`<Small>2</Small>`, RuleFacet, "enumeration"},
		{`<Small>many</Small>`, RuleLexical, "none of the union's member types"},
		{`<Nested>maybe</Nested>`, RuleLexical, "none of the union's member types"},
		{`<Stamp at="yesterday">x</Stamp>`, RuleAttribute, "none of the union's member types"},
		{`<Due>2026-01-02</Due>`, RuleFixed, "fixed value"},
	} {
		vs := validateUnion(t, tc.body)
		if len(vs) != 1 || vs[0].Rule != tc.rule || !strings.Contains(vs[0].Reason, tc.reason) {
			t.Errorf("%s: got %v, want one %s violation naming %q", tc.body, vs, tc.rule, tc.reason)
		}
	}
}

// TestLoad_UnionMembers pins XML Schema Part 1, 3.14.2: the members are
// the memberTypes, in order, then the xs:simpleType children.
func TestLoad_UnionMembers(t *testing.T) {
	s := loadTestSchema(t, unionXSD)
	u, ok := s.Types[QName{Namespace: "urn:t", Local: "IntOrNA"}].(*SimpleType)
	if !ok || !u.Union {
		t.Fatalf("IntOrNA: got %#v, want a union", s.Types[QName{Namespace: "urn:t", Local: "IntOrNA"}])
	}
	if len(u.Members) != 2 || u.Members[0].QName().Local != "integer" {
		t.Fatalf("members: got %v, want xs:integer then the inline type", u.Members)
	}
	if inline, ok := u.Members[1].(*SimpleType); !ok || inline.Base == nil {
		t.Errorf("inline member: got %#v, want a linked *SimpleType", u.Members[1])
	}

	broken := strings.Replace(unionXSD, `memberTypes="xs:dateTime xs:date"`, `memberTypes="xs:dateTime Missing"`, 1)
	_, err := Load(context.Background(), fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: []byte(broken)}}, "schema.xsd")
	if !errors.Is(err, ErrUnresolvedRef) {
		t.Errorf("unresolved member: got %v, want ErrUnresolvedRef", err)
	}
}

// TestValidateValue_PathologicalUnionsTerminate pins the anti-DoS bounds on
// union descent: a union that names itself, and unions nested 30 deep
// with four members each, which would cost 4^30 member checks unbounded,
// both return, accepting the value as a truncated walk does.
func TestValidateValue_PathologicalUnionsTerminate(t *testing.T) {
	self := &SimpleType{Union: true}
	self.Members = []Type{self}
	if got := ValidateValue("x", self); len(got) != 0 {
		t.Errorf("self-referencing union: got %v, want none", got)
	}

	var b strings.Builder
	b.WriteString(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">`)
	const levels = 30
	for i := range levels {
		member := fmt.Sprintf("U%d", i+1)
		if i == levels-1 {
			member = "xs:boolean"
		}
		fmt.Fprintf(&b, `<xs:simpleType name="U%d"><xs:union memberTypes="%s %s %s %s"/></xs:simpleType>`,
			i, member, member, member, member)
	}
	b.WriteString(`</xs:schema>`)
	s := loadTestSchema(t, b.String())
	if got := ValidateValue("maybe", s.Types[QName{Namespace: "urn:t", Local: "U0"}]); len(got) != 0 {
		t.Errorf("wide nested unions: got %v, want the value accepted once the budget is spent", got)
	}
}
