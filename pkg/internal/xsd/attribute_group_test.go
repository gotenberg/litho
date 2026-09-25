package xsd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// attributeGroupXSD incorporates attribute groups (XML Schema Part 1, 3.6)
// by reference in each place a complex type holds attribute declarations:
// the simpleContent extension of AmountType, the complexContent extension
// of LineType, and the attribute declarations of PartyType itself. Common
// nests Inner, which references a top-level declaration.
const attributeGroupXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:attribute name="level" type="xs:integer"/>
  <xs:attributeGroup name="Common">
    <xs:attribute name="id" type="xs:token" use="required"/>
    <xs:attributeGroup ref="Inner"/>
  </xs:attributeGroup>
  <xs:attributeGroup name="Inner">
    <xs:attribute name="unit">
      <xs:simpleType>
        <xs:restriction base="xs:token">
          <xs:enumeration value="EA"/>
          <xs:enumeration value="KG"/>
        </xs:restriction>
      </xs:simpleType>
    </xs:attribute>
    <xs:attribute ref="level"/>
  </xs:attributeGroup>
  <xs:complexType name="AmountType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute name="currencyID" type="xs:token"/>
        <xs:attributeGroup ref="Common"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="BaseType">
    <xs:sequence>
      <xs:element name="Amount" type="AmountType"/>
    </xs:sequence>
  </xs:complexType>
  <xs:complexType name="LineType">
    <xs:complexContent>
      <xs:extension base="BaseType">
        <xs:attributeGroup ref="Common"/>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
  <xs:complexType name="PartyType">
    <xs:attributeGroup ref="Inner"/>
  </xs:complexType>
  <xs:element name="Line" type="LineType"/>
  <xs:element name="Party" type="PartyType"/>
</xs:schema>`

func validateDoc(t *testing.T, s *Schema, doc string) []Violation {
	t.Helper()
	vs, err := Validate(context.Background(), s, []byte(doc))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return vs
}

// TestValidate_AttributeGroup pins XML Schema Part 1, 3.4.2 clause 2 and
// 3.6.2: the {attribute uses} of the groups a complex type references join
// its own, those of a nested group included, and each use keeps its type,
// its required flag and, for a reference, its top-level declaration.
func TestValidate_AttributeGroup(t *testing.T) {
	s := loadTestSchema(t, attributeGroupXSD)
	const line = `<Line xmlns="urn:t" xmlns:t="urn:t" id="L1" `
	for doc, want := range map[string]string{
		line + `unit="KG" t:level="2"><Amount id="A1" currencyID="EUR" unit="EA">1</Amount></Line>`: "",
		`<Party xmlns="urn:t" xmlns:t="urn:t" unit="EA" t:level="1"/>`:                              "",
		`<Party xmlns="urn:t"/>`:                                  "",
		`<Line xmlns="urn:t"><Amount id="A1">1</Amount></Line>`:   "required attribute missing",
		line + `><Amount>1</Amount></Line>`:                       "required attribute missing",
		line + `unit="LB"><Amount id="A1">1</Amount></Line>`:      "enumeration",
		line + `t:level="high"><Amount id="A1">1</Amount></Line>`: "integer",
		line + `><Amount id="A1" level="2">1</Amount></Line>`:     "not declared",
		`<Party xmlns="urn:t" id="P1"/>`:                          "not declared",
	} {
		vs := validateDoc(t, s, doc)
		if want == "" {
			if len(vs) != 0 {
				t.Errorf("%s: got %v, want none", doc, vs)
			}
			continue
		}
		if len(vs) != 1 || vs[0].Rule != RuleAttribute || !strings.Contains(vs[0].Reason, want) {
			t.Errorf("%s: got %v, want one attribute violation naming %q", doc, vs, want)
		}
	}
}

// attributeGroupRestrictionXSD restricts a simple content type whose
// attributes come from a group. The restriction prohibits b among its own
// children, and references a group that prohibits a.
const attributeGroupRestrictionXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:attributeGroup name="Pair">
    <xs:attribute name="a" type="xs:token"/>
    <xs:attribute name="b" type="xs:token"/>
  </xs:attributeGroup>
  <xs:attributeGroup name="NoA">
    <xs:attribute name="a" use="prohibited"/>
  </xs:attributeGroup>
  <xs:complexType name="BaseType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attributeGroup ref="Pair"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="NarrowType">
    <xs:simpleContent>
      <xs:restriction base="BaseType">
        <xs:attribute name="b" use="prohibited"/>
        <xs:attributeGroup ref="NoA"/>
      </xs:restriction>
    </xs:simpleContent>
  </xs:complexType>
  <xs:element name="Narrow" type="NarrowType"/>
</xs:schema>`

// TestValidate_AttributeGroupProhibited pins XML Schema Part 1, 3.4.2
// clause 3.2 and 3.2.2: a restriction drops a base attribute use that an
// xs:attribute among its own children prohibits, while a prohibited
// xs:attribute inside a group corresponds to nothing and drops none.
func TestValidate_AttributeGroupProhibited(t *testing.T) {
	s := loadTestSchema(t, attributeGroupRestrictionXSD)
	if vs := validateDoc(t, s, `<Narrow xmlns="urn:t" a="x">1</Narrow>`); len(vs) != 0 {
		t.Errorf("a stays declared: got %v, want none", vs)
	}
	vs := validateDoc(t, s, `<Narrow xmlns="urn:t" b="x">1</Narrow>`)
	if len(vs) != 1 || !strings.Contains(vs[0].Reason, "not declared") {
		t.Errorf("b is prohibited: got %v, want one undeclared attribute", vs)
	}
}

// attributeGroupWildcardXSD carries xs:anyAttribute inside attribute
// groups. Other admits any other namespace, skipping it; Named admits
// urn:b and no namespace, laxly; Deep nests Other under its own strict
// wildcard for urn:b and urn:c, so it admits urn:b and urn:c strictly.
// StrictFirst and LaxFirst reference Deep and Named in both orders, and
// LocalLax narrows its own lax wildcard with Deep.
const attributeGroupWildcardXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" xmlns:b="urn:b" elementFormDefault="qualified">
  <xs:attributeGroup name="Other">
    <xs:anyAttribute namespace="##other" processContents="skip"/>
  </xs:attributeGroup>
  <xs:attributeGroup name="Named">
    <xs:anyAttribute namespace="urn:b ##local" processContents="lax"/>
  </xs:attributeGroup>
  <xs:attributeGroup name="Deep">
    <xs:attributeGroup ref="Other"/>
    <xs:anyAttribute namespace="urn:b urn:c" processContents="strict"/>
  </xs:attributeGroup>
  <xs:complexType name="GroupOnly">
    <xs:sequence/>
    <xs:attributeGroup ref="Other"/>
  </xs:complexType>
  <xs:complexType name="LocalAndGroup">
    <xs:sequence/>
    <xs:attributeGroup ref="Other"/>
    <xs:anyAttribute processContents="skip"/>
  </xs:complexType>
  <xs:complexType name="TwoGroups">
    <xs:sequence/>
    <xs:attributeGroup ref="Other"/>
    <xs:attributeGroup ref="Named"/>
  </xs:complexType>
  <xs:complexType name="Nested">
    <xs:sequence/>
    <xs:attributeGroup ref="Deep"/>
  </xs:complexType>
  <xs:complexType name="StrictFirst">
    <xs:sequence/>
    <xs:attributeGroup ref="Deep"/>
    <xs:attributeGroup ref="Named"/>
  </xs:complexType>
  <xs:complexType name="LaxFirst">
    <xs:sequence/>
    <xs:attributeGroup ref="Named"/>
    <xs:attributeGroup ref="Deep"/>
  </xs:complexType>
  <xs:complexType name="LocalLax">
    <xs:sequence/>
    <xs:attributeGroup ref="Deep"/>
    <xs:anyAttribute processContents="lax"/>
  </xs:complexType>
  <xs:element name="Root">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="GroupOnly" type="GroupOnly" minOccurs="0"/>
        <xs:element name="LocalAndGroup" type="LocalAndGroup" minOccurs="0"/>
        <xs:element name="TwoGroups" type="TwoGroups" minOccurs="0"/>
        <xs:element name="Nested" type="Nested" minOccurs="0"/>
        <xs:element name="StrictFirst" type="StrictFirst" minOccurs="0"/>
        <xs:element name="LaxFirst" type="LaxFirst" minOccurs="0"/>
        <xs:element name="LocalLax" type="LocalLax" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

// TestValidate_AttributeGroupWildcard pins the complete wildcard of XML
// Schema Part 1, 3.4.2 clause 2: a group's wildcard stands in for a missing
// xs:anyAttribute, and otherwise narrows it by intersection (3.10.6), with
// the process contents of the type's own wildcard (clause 2.2.1), or of the
// first group wildcard when it has none (clause 2.2.2). A group's own
// wildcard is itself the complete wildcard of its content (3.6.2).
func TestValidate_AttributeGroupWildcard(t *testing.T) {
	s := loadTestSchema(t, attributeGroupWildcardXSD)
	const root = `<Root xmlns="urn:t" xmlns:t="urn:t" xmlns:b="urn:b" xmlns:c="urn:c" xmlns:d="urn:d">`
	for body, want := range map[string]string{
		`<GroupOnly b:x="1"/>`:     "",
		`<GroupOnly x="1"/>`:       "not declared", // ##other excludes no namespace.
		`<GroupOnly t:x="1"/>`:     "not declared", // ##other excludes the target namespace.
		`<LocalAndGroup d:x="1"/>`: "",
		`<LocalAndGroup x="1"/>`:   "not declared", // ##any narrowed to ##other.
		`<TwoGroups b:x="1"/>`:     "",             // urn:b is in both.
		`<TwoGroups x="1"/>`:       "not declared", // ##local is not ##other.
		`<TwoGroups d:x="1"/>`:     "not declared", // urn:d is not in Named.
		`<Nested c:x="1"/>`:        "no top-level declaration",
		`<Nested d:x="1"/>`:        "not declared", // urn:d is not in Deep's own.
		`<Nested t:x="1"/>`:        "not declared",
		`<StrictFirst b:x="1"/>`:   "no top-level declaration", // strict, from Deep.
		`<StrictFirst c:x="1"/>`:   "not declared",             // urn:c is not in Named.
		`<LaxFirst b:x="1"/>`:      "",                         // lax, from Named.
		`<LocalLax c:x="1"/>`:      "",                         // lax, from the type's own.
		`<LocalLax x="1"/>`:        "not declared",             // ##any narrowed by Deep.
	} {
		vs := validateDoc(t, s, root+body+`</Root>`)
		if want == "" {
			if len(vs) != 0 {
				t.Errorf("%s: got %v, want none", body, vs)
			}
			continue
		}
		if len(vs) == 0 || vs[0].Rule != RuleAttribute || !strings.Contains(vs[0].Reason, want) {
			t.Errorf("%s: got %v, want an attribute violation naming %q", body, vs, want)
		}
	}
}

// TestLoad_AttributeGroupAcrossDocuments pins that a reference resolves
// against an imported and an included schema document (XML Schema Part 1,
// 3.15.3), and that a group's local attributes take the form of the
// document that declares them.
func TestLoad_AttributeGroupAcrossDocuments(t *testing.T) {
	const root = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:c="urn:common"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:import namespace="urn:common" schemaLocation="common.xsd"/>
  <xs:include schemaLocation="local.xsd"/>
  <xs:complexType name="DocType">
    <xs:sequence/>
    <xs:attributeGroup ref="c:Audit"/>
    <xs:attributeGroup ref="Version"/>
  </xs:complexType>
  <xs:element name="Doc" type="DocType"/>
</xs:schema>`
	const common = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:common" attributeFormDefault="qualified">
  <xs:attributeGroup name="Audit"><xs:attribute name="by" type="xs:token" use="required"/></xs:attributeGroup>
</xs:schema>`
	const local = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">
  <xs:attributeGroup name="Version"><xs:attribute name="version" type="xs:decimal"/></xs:attributeGroup>
</xs:schema>`
	s, err := Load(context.Background(), fstest.MapFS{
		"root.xsd":   &fstest.MapFile{Data: []byte(root)},
		"common.xsd": &fstest.MapFile{Data: []byte(common)},
		"local.xsd":  &fstest.MapFile{Data: []byte(local)},
	}, "root.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const open = `<Doc xmlns="urn:t" xmlns:c="urn:common" `
	for doc, want := range map[string]string{
		open + `c:by="me" version="1.0"/>`: "",
		open + `by="me"/>`:                 "required attribute missing",
		open + `c:by="me" version="v1"/>`:  "decimal",
	} {
		vs := validateDoc(t, s, doc)
		if want == "" && len(vs) != 0 || want != "" && (len(vs) == 0 || !strings.Contains(vs[0].Reason, want)) {
			t.Errorf("%s: got %v, want a first violation naming %q", doc, vs, want)
		}
	}
}

// cyclicAttributeGroupXSD references its attribute groups in a two-step
// cycle, which XML Schema Part 1, 3.6.3 (src-attribute_group clause 3)
// rules out, from a type that is otherwise valid.
const cyclicAttributeGroupXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">
  <xs:attributeGroup name="A"><xs:attribute name="a" type="xs:token"/><xs:attributeGroup ref="B"/></xs:attributeGroup>
  <xs:attributeGroup name="B"><xs:anyAttribute/><xs:attributeGroup ref="A"/></xs:attributeGroup>
  <xs:element name="Root"><xs:complexType><xs:attributeGroup ref="A"/></xs:complexType></xs:element>
</xs:schema>`

// TestLoad_AttributeGroupErrors pins the schemas the loader rejects: per XML
// Schema Part 1, 3.6.2 a top-level xs:attributeGroup has a name and no ref,
// and a nested one a ref and nothing else; per 3.6.3 (src-attribute_group
// clause 3) no group references itself, directly or indirectly; and a ref
// that names no top-level group does not resolve.
func TestLoad_AttributeGroupErrors(t *testing.T) {
	const head = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">` +
		`<xs:attributeGroup name="G"><xs:attribute name="a" type="xs:token"/></xs:attributeGroup>`
	for name, tc := range map[string]struct {
		body string
		want error
	}{
		"self reference": {`<xs:attributeGroup name="A"><xs:attributeGroup ref="A"/></xs:attributeGroup>`, ErrSchemaParse},
		"indirect cycle": {`<xs:attributeGroup name="A"><xs:attributeGroup ref="B"/></xs:attributeGroup>` +
			`<xs:attributeGroup name="B"><xs:attributeGroup ref="G"/><xs:attributeGroup ref="A"/></xs:attributeGroup>`, ErrSchemaParse},
		"top-level ref":     {`<xs:attributeGroup ref="G"/>`, ErrSchemaParse},
		"top-level no name": {`<xs:attributeGroup><xs:attribute name="a"/></xs:attributeGroup>`, ErrSchemaParse},
		"nested with name":  {`<xs:complexType name="T"><xs:attributeGroup ref="G" name="H"/></xs:complexType>`, ErrSchemaParse},
		"nested with content": {`<xs:complexType name="T"><xs:attributeGroup ref="G">` +
			`<xs:attribute name="b"/></xs:attributeGroup></xs:complexType>`, ErrSchemaParse},
		"nested without ref": {`<xs:complexType name="T"><xs:attributeGroup/></xs:complexType>`, ErrSchemaParse},
		"unresolved ref":     {`<xs:complexType name="T"><xs:attributeGroup ref="Missing"/></xs:complexType>`, ErrUnresolvedRef},
		"unresolved nested":  {`<xs:attributeGroup name="A"><xs:attributeGroup ref="Missing"/></xs:attributeGroup>`, ErrUnresolvedRef},
	} {
		_, err := Load(context.Background(), fstest.MapFS{
			"schema.xsd": &fstest.MapFile{Data: []byte(head + tc.body + `</xs:schema>`)},
		}, "schema.xsd")
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Load error %v, want %v", name, err, tc.want)
		}
	}
	_, err := Load(context.Background(), fstest.MapFS{
		"schema.xsd": &fstest.MapFile{Data: []byte(cyclicAttributeGroupXSD)},
	}, "schema.xsd")
	if !errors.Is(err, ErrSchemaParse) {
		t.Errorf("cycle through a type: Load error %v, want %v", err, ErrSchemaParse)
	}
}

// attributeGroupLatticeDepth is deep enough that a walk visiting a group
// once per path to it, 2^depth times, would never finish.
const attributeGroupLatticeDepth = 48

// TestValidate_AttributeGroupLattice pins that the attribute uses and the
// wildcard of a type derive in time linear in its groups: every group at
// one level of the lattice references both groups of the next, so the
// deepest pair is reachable along 2^depth paths.
func TestValidate_AttributeGroupLattice(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">`)
	for level := range attributeGroupLatticeDepth {
		for _, side := range []string{"L", "R"} {
			fmt.Fprintf(&b, `<xs:attributeGroup name="%s%d">`, side, level)
			if level+1 < attributeGroupLatticeDepth {
				fmt.Fprintf(&b, `<xs:attributeGroup ref="L%d"/><xs:attributeGroup ref="R%d"/>`, level+1, level+1)
			} else {
				fmt.Fprintf(&b, `<xs:attribute name="%s" type="xs:integer"/>`, strings.ToLower(side))
			}
			fmt.Fprintf(&b, `<xs:anyAttribute namespace="##other" processContents="skip"/></xs:attributeGroup>`)
		}
	}
	b.WriteString(`<xs:element name="Root"><xs:complexType><xs:attributeGroup ref="L0"/></xs:complexType></xs:element></xs:schema>`)
	s := loadTestSchema(t, b.String())
	vs := validateDoc(t, s, `<Root xmlns="urn:t" xmlns:o="urn:o" l="1" r="x" o:any="1" undeclared="1"/>`)
	if len(vs) != 2 || !strings.Contains(vs[0].Reason, "integer") || !strings.Contains(vs[1].Reason, "not declared") {
		t.Errorf("got %v, want the value of r and the undeclared attribute reported", vs)
	}
}
