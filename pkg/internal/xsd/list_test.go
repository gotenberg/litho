package xsd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// listXSD derives lists by xs:list (XML Schema Part 2, 2.5.1.2): Codes
// names its itemType, Sizes holds an xs:simpleType child, ShortCodes
// restricts Codes with length facets and a pattern, Flags lists a union,
// and Tokens names the built-in NMTOKENS.
const listXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="Code">
    <xs:restriction base="xs:token">
      <xs:enumeration value="EA"/>
      <xs:enumeration value="KG"/>
      <xs:enumeration value="LTR"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:simpleType name="Codes">
    <xs:list itemType="Code"/>
  </xs:simpleType>
  <xs:simpleType name="Sizes">
    <xs:list>
      <xs:simpleType>
        <xs:restriction base="xs:integer">
          <xs:minInclusive value="1"/>
        </xs:restriction>
      </xs:simpleType>
    </xs:list>
  </xs:simpleType>
  <xs:simpleType name="ShortCodes">
    <xs:restriction base="Codes">
      <xs:minLength value="1"/>
      <xs:maxLength value="2"/>
      <xs:pattern value="([A-Z]{2} ?)*"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:simpleType name="PairOfSizes">
    <xs:restriction base="Sizes">
      <xs:length value="2"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:simpleType name="Flags">
    <xs:list>
      <xs:simpleType>
        <xs:union memberTypes="xs:boolean xs:date"/>
      </xs:simpleType>
    </xs:list>
  </xs:simpleType>
  <xs:complexType name="Root">
    <xs:sequence>
      <xs:element name="Codes" type="Codes" minOccurs="0"/>
      <xs:element name="Sizes" type="Sizes" minOccurs="0"/>
      <xs:element name="ShortCodes" type="ShortCodes" minOccurs="0"/>
      <xs:element name="PairOfSizes" type="PairOfSizes" minOccurs="0"/>
      <xs:element name="Flags" type="Flags" minOccurs="0"/>
      <xs:element name="Tokens" type="xs:NMTOKENS" minOccurs="0"/>
    </xs:sequence>
    <xs:attribute name="sizes" type="Sizes"/>
  </xs:complexType>
  <xs:element name="Root" type="Root"/>
</xs:schema>`

func validateList(t *testing.T, body string) []Violation {
	t.Helper()
	vs, err := Validate(context.Background(), loadTestSchema(t, listXSD), []byte(`<Root xmlns="urn:t"`+body+`</Root>`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return vs
}

// TestValidate_ListValid pins the values XML Schema Part 2, 2.5.1.2 admits
// for a list: white-space-separated items each valid for the itemType,
// under the collapsed white space 4.3.6 fixes for every list, and facets
// that count items.
func TestValidate_ListValid(t *testing.T) {
	for name, body := range map[string]string{
		"named itemType":         `><Codes>EA KG LTR</Codes>`,
		"white space collapses":  ">\n<Codes>\n  EA\t\tKG \n</Codes>",
		"empty list":             `><Codes></Codes>`,
		"inline itemType":        `><Sizes>1 20 300</Sizes>`,
		"items counted":          `><ShortCodes>EA KG</ShortCodes>`,
		"exact length":           `><PairOfSizes> 4  5 </PairOfSizes>`,
		"union items":            `><Flags>true 2024-01-31 0</Flags>`,
		"built-in NMTOKENS":      `><Tokens>a-1 b.2</Tokens>`,
		"list-typed attribute":   ` sizes="1 2 3">`,
		"list-typed attr, blank": ` sizes="  ">`,
	} {
		if vs := validateList(t, body); len(vs) != 0 {
			t.Errorf("%s: got %v, want none", name, vs)
		}
	}
}

// TestValidate_ListInvalid pins the list values XML Schema Part 2, 2.5.1.2
// rejects: an item its itemType rejects, and a count or literal the facets
// of a restriction of the list reject. A pattern matches the literal of
// the whole list, and the length facets count items rather than
// characters. An item fails as a value of its itemType does, "two" both
// the lexical space and the range of its integer type.
func TestValidate_ListInvalid(t *testing.T) {
	for body, want := range map[string]string{
		`><Codes>EA XX</Codes>`:              `list item "XX"`,
		`><Sizes>1 0</Sizes>`:                `list item "0"`,
		`><Sizes>1 two</Sizes>`:              `list item "two"`,
		`><ShortCodes></ShortCodes>`:         "0 list items below minimum 1",
		`><ShortCodes>EA KG EA</ShortCodes>`: "3 list items above maximum 2",
		`><ShortCodes>LTR</ShortCodes>`:      "pattern",
		`><PairOfSizes>1 2 3</PairOfSizes>`:  "exactly 2",
		`><Flags>yes</Flags>`:                `list item "yes"`,
		`><Tokens></Tokens>`:                 "below minimum 1",
		` sizes="1 x">`:                      `list item "x"`,
	} {
		vs := validateList(t, body)
		if len(vs) == 0 {
			t.Errorf("%s: got no violation, want one naming %q", body, want)
		}
		for _, v := range vs {
			if !strings.Contains(v.Reason, want) {
				t.Errorf("%s: got %v, want violations naming %q", body, v, want)
			}
		}
	}
}

// selfListXSD declares a list whose itemType is the list itself, which
// XML Schema Part 2, 4.1.5 (cos-list-of-atomic) forbids and the loader
// does not reject: the item type of a list must be atomic or a union.
const selfListXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="L">
    <xs:list itemType="L"/>
  </xs:simpleType>
  <xs:element name="Root" type="L"/>
</xs:schema>`

// TestValidate_SelfListTerminates pins that validating against a list of
// itself stops at [maxTypeDerivationDepth] and blames nothing on the
// instance, as a truncated derivation walk does.
func TestValidate_SelfListTerminates(t *testing.T) {
	vs, err := Validate(context.Background(), loadTestSchema(t, selfListXSD), []byte(`<Root xmlns="urn:t">a b c</Root>`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("got %v, want none", vs)
	}
}

// TestLoad_ListNeedsOneItemType pins XML Schema Part 1, 3.14.3
// (src-list-itemType-or-simpleType): an xs:list carries an itemType
// attribute or an xs:simpleType child, never both nor neither.
func TestLoad_ListNeedsOneItemType(t *testing.T) {
	for name, list := range map[string]string{
		"neither": `<xs:list/>`,
		"both":    `<xs:list itemType="xs:token"><xs:simpleType><xs:restriction base="xs:token"/></xs:simpleType></xs:list>`,
	} {
		body := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">` +
			`<xs:simpleType name="L">` + list + `</xs:simpleType></xs:schema>`
		_, err := Load(context.Background(), fstest.MapFS{"s.xsd": &fstest.MapFile{Data: []byte(body)}}, "s.xsd")
		if !errors.Is(err, ErrSchemaParse) {
			t.Errorf("%s: Load error %v, want ErrSchemaParse", name, err)
		}
	}
}
