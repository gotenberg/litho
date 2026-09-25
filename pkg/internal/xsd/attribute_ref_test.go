package xsd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// attributeRefXSD references top-level attribute declarations with
// xs:attribute ref (XML Schema Part 1, 3.2.2): level from an imported
// schema, required, and unit and version from its own, version fixed on
// its declaration.
const attributeRefXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           xmlns:c="urn:common"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:import namespace="urn:common" schemaLocation="common.xsd"/>
  <xs:attribute name="unit">
    <xs:simpleType>
      <xs:restriction base="xs:token">
        <xs:enumeration value="EA"/>
        <xs:enumeration value="KG"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:attribute>
  <xs:attribute name="version" type="xs:decimal" fixed="1.0"/>
  <xs:complexType name="QuantityType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute ref="c:level" use="required"/>
        <xs:attribute ref="unit"/>
        <xs:attribute ref="version"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:element name="Quantity" type="QuantityType"/>
</xs:schema>`

// commonXSD declares the attribute attributeRefXSD imports.
const commonXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:common">
  <xs:attribute name="level" type="xs:integer"/>
</xs:schema>`

// localAttributeRefXSD references a top-level declaration of its own
// schema, whose fixed value binds the use, and needs no import.
const localAttributeRefXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:attribute name="version" type="xs:decimal" fixed="1.0"/>
  <xs:complexType name="AmountType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute ref="version" use="required"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:element name="Amount" type="AmountType"/>
</xs:schema>`

// TestValidate_LocalAttributeRef pins a reference resolved in the schema
// that makes it: the required use of a qualified, fixed attribute.
func TestValidate_LocalAttributeRef(t *testing.T) {
	s := loadTestSchema(t, localAttributeRefXSD)
	const open = `<Amount xmlns="urn:t" xmlns:t="urn:t" `
	for doc, want := range map[string]string{
		open + `t:version="1">1</Amount>`: "",
		open + `t:version="2">1</Amount>`: "fixed",
		open + `version="1">1</Amount>`:   "required attribute missing",
	} {
		vs, err := Validate(context.Background(), s, []byte(doc))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if want == "" && len(vs) != 0 || want != "" && (len(vs) == 0 || !strings.Contains(vs[0].Reason, want)) {
			t.Errorf("%s: got %v, want a first violation naming %q", doc, vs, want)
		}
	}
}

func loadAttributeRefSchema(t *testing.T, root string) (*Schema, error) {
	t.Helper()
	return Load(context.Background(), fstest.MapFS{
		"schema.xsd": &fstest.MapFile{Data: []byte(root)},
		"common.xsd": &fstest.MapFile{Data: []byte(commonXSD)},
	}, "schema.xsd")
}

// TestValidate_AttributeRef pins XML Schema Part 1, 3.2.2: an xs:attribute
// ref is a use of the top-level declaration it names. The attribute takes
// that declaration's name, in its target namespace, and its type; the use
// states whether it is required; and a fixed value binds from either side
// (3.5.4 cvc-au, 3.2.4 cvc-attribute clause 4).
func TestValidate_AttributeRef(t *testing.T) {
	s, err := loadAttributeRefSchema(t, attributeRefXSD)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const open = `<Quantity xmlns="urn:t" xmlns:t="urn:t" xmlns:c="urn:common" `
	for doc, want := range map[string]string{
		open + `c:level="7" t:unit="KG" t:version="1.00">1</Quantity>`: "",
		open + `c:level="7">1</Quantity>`:                              "",
		open + `t:unit="KG">1</Quantity>`:                              "required attribute missing",
		open + `c:level="high">1</Quantity>`:                           "integer",
		open + `c:level="7" t:unit="LB">1</Quantity>`:                  "enumeration",
		open + `c:level="7" t:version="2">1</Quantity>`:                "fixed",
		open + `c:level="7" unit="KG">1</Quantity>`:                    "not declared",
	} {
		vs, err := Validate(context.Background(), s, []byte(doc))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
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

// TestLoad_AttributeRefErrors pins the schemas the loader rejects: per XML
// Schema Part 1, 3.2.3 (src-attribute clauses 3.1 and 3.2) a reference
// carries no name, type, form or xs:simpleType; per 3.2.2 a top-level
// xs:attribute has a name and no ref; and a ref that names no top-level
// declaration does not resolve.
func TestLoad_AttributeRefErrors(t *testing.T) {
	const head = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">` +
		`<xs:attribute name="unit" type="xs:token"/>`
	for name, tc := range map[string]struct {
		body string
		want error
	}{
		"ref and name":   {`<xs:complexType name="T"><xs:attribute ref="unit" name="u"/></xs:complexType>`, ErrSchemaParse},
		"ref and type":   {`<xs:complexType name="T"><xs:attribute ref="unit" type="xs:token"/></xs:complexType>`, ErrSchemaParse},
		"ref and form":   {`<xs:complexType name="T"><xs:attribute ref="unit" form="qualified"/></xs:complexType>`, ErrSchemaParse},
		"top-level ref":  {`<xs:attribute ref="unit"/>`, ErrSchemaParse},
		"unresolved ref": {`<xs:complexType name="T"><xs:attribute ref="other"/></xs:complexType>`, ErrUnresolvedRef},
	} {
		_, err := loadAttributeRefSchema(t, head+tc.body+`</xs:schema>`)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Load error %v, want %v", name, err, tc.want)
		}
	}
}
