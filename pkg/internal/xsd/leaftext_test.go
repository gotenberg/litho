package xsd

import (
	"context"
	"testing"
)

const leafTextXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="ShortString">
    <xs:restriction base="xs:string"><xs:maxLength value="3"/></xs:restriction>
  </xs:simpleType>
  <xs:simpleType name="ShortToken">
    <xs:restriction base="xs:token"><xs:maxLength value="3"/></xs:restriction>
  </xs:simpleType>
  <xs:complexType name="AmountType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal"/>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="PartyType">
    <xs:sequence>
      <xs:element name="Name" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
  <xs:complexType name="InvoiceType">
    <xs:sequence>
      <xs:element name="Decimal" type="xs:decimal" minOccurs="0"/>
      <xs:element name="String" type="xs:string" minOccurs="0"/>
      <xs:element name="Token" type="xs:token" minOccurs="0"/>
      <xs:element name="ShortString" type="ShortString" minOccurs="0"/>
      <xs:element name="ShortToken" type="ShortToken" minOccurs="0"/>
      <xs:element name="Amount" type="AmountType" minOccurs="0"/>
      <xs:element name="Rate" type="xs:decimal" default="0" minOccurs="0"/>
      <xs:element name="Nillable" type="xs:decimal" nillable="true" minOccurs="0"/>
      <xs:element name="Party" type="PartyType" nillable="true" minOccurs="0"/>
      <xs:element name="Version" type="xs:decimal" fixed="1.0" minOccurs="0"/>
      <xs:element name="Unit" type="xs:token" fixed="EA" minOccurs="0"/>
      <xs:element name="Flag" type="xs:boolean" fixed="true" minOccurs="0"/>
      <xs:element name="FixedAmount" type="AmountType" fixed="5" minOccurs="0"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func validateLeafText(t *testing.T, body string) []Violation {
	t.Helper()
	s := loadTestSchema(t, leafTextXSD)
	doc := `<Invoice xmlns="urn:t" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` + body + `</Invoice>`
	got, err := Validate(context.Background(), s, []byte(doc))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return got
}

func TestValidate_LeafTextValid(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"empty string":             `<String/>`,
		"empty token":              `<Token></Token>`,
		"padded decimal collapses": "<Decimal>\n  12.50\n</Decimal>",
		"padded token collapses before maxLength": `<ShortToken>  abc  </ShortToken>`,
		"empty element takes its default":         `<Rate/>`,
		"nilled simple element":                   `<Nillable xsi:nil="true"/>`,
		"nilled complex element skips content":    `<Party xsi:nil="true"/>`,
		"xsi:nil false validates the content":     `<Nillable xsi:nil="false">1</Nillable>`,
		"empty element takes its fixed value":     `<Version/>`,
		"fixed decimal, equal value":              `<Version>1</Version>`,
		"fixed decimal, trailing zeros":           `<Version>01.00</Version>`,
		"fixed token, collapsed":                  `<Unit> EA </Unit>`,
		"fixed boolean, other literal":            `<Flag>1</Flag>`,
		"fixed simple content":                    `<FixedAmount>5.0</FixedAmount>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := validateLeafText(t, body); len(got) != 0 {
				t.Errorf("got %v, want no violation", got)
			}
		})
	}
}

func TestValidate_LeafTextViolations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		rule string
		path string
	}{
		{"empty decimal", `<Decimal/>`, RuleLexical, "/Decimal"},
		{"blank decimal", `<Decimal>  </Decimal>`, RuleLexical, "/Decimal"},
		{"empty simple content decimal", `<Amount></Amount>`, RuleLexical, "/Amount"},
		{"string keeps its spaces", `<ShortString> ab </ShortString>`, RuleFacet, "/ShortString"},
		{"blank content is not empty", `<Rate> </Rate>`, RuleLexical, "/Rate"},
		{"xsi:nil false keeps the check", `<Nillable xsi:nil="false"/>`, RuleLexical, "/Nillable"},
		{"nilled element with content", `<Nillable xsi:nil="true">1</Nillable>`, RuleStructure, "/Nillable"},
		{"nilled element with a child", `<Party xsi:nil="true"><Name>x</Name></Party>`, RuleStructure, "/Party/"},
		{"xsi:nil on an element that is not nillable", `<Decimal xsi:nil="true"/>`, RuleAttribute, "/Decimal/@xsi:nil"},
		{"xsi:nil that is not a boolean", `<Nillable xsi:nil="yes"/>`, RuleAttribute, "/Nillable/@xsi:nil"},
		{"fixed decimal, other value", `<Version>1.5</Version>`, RuleFixed, "/Version"},
		{"fixed token, other value", `<Unit>KG</Unit>`, RuleFixed, "/Unit"},
		{"fixed token, blank content", `<Unit> </Unit>`, RuleFixed, "/Unit"},
		{"fixed boolean, other value", `<Flag>false</Flag>`, RuleFixed, "/Flag"},
		{"fixed simple content, other value", `<FixedAmount>6</FixedAmount>`, RuleFixed, "/FixedAmount"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := validateLeafText(t, tc.body)
			if !containsRuleAtPath(got, tc.rule, tc.path) {
				t.Errorf("got %v, want a %s violation at %s", got, tc.rule, tc.path)
			}
		})
	}
}
