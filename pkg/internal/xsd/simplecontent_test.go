package xsd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// simpleContentXSD mirrors the CII data types: an amount extends
// xs:decimal with an attribute, a code extends a restricted token, a
// qualified code extends the unqualified one, and restrictions narrow an
// amount with facets, an inline xs:simpleType and attribute changes.
const simpleContentXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:complexType name="AmountType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute name="currencyID" type="xs:token" use="required"/>
        <xs:attribute name="scale" type="xs:integer"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:simpleType name="CodeContentType">
    <xs:restriction base="xs:token">
      <xs:maxLength value="3"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:complexType name="CodeType">
    <xs:simpleContent>
      <xs:extension base="CodeContentType">
        <xs:attribute name="listID" type="xs:token" fixed="ISO 4217"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="QualifiedCodeType">
    <xs:simpleContent>
      <xs:extension base="CodeType">
        <xs:attribute name="listVersionID" type="xs:token" use="required"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="SmallAmountType">
    <xs:simpleContent>
      <xs:restriction base="AmountType">
        <xs:maxInclusive value="100"/>
        <xs:attribute name="currencyID" type="xs:token" use="optional"/>
        <xs:attribute name="scale" use="prohibited"/>
      </xs:restriction>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="EuroAmountType">
    <xs:simpleContent>
      <xs:restriction base="AmountType">
        <xs:simpleType>
          <xs:restriction base="xs:decimal">
            <xs:fractionDigits value="2"/>
          </xs:restriction>
        </xs:simpleType>
        <xs:minInclusive value="0"/>
      </xs:restriction>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="InvoiceType">
    <xs:sequence>
      <xs:element name="Amount" type="AmountType" minOccurs="0"/>
      <xs:element name="Code" type="CodeType" minOccurs="0"/>
      <xs:element name="QualifiedCode" type="QualifiedCodeType" minOccurs="0"/>
      <xs:element name="Small" type="SmallAmountType" minOccurs="0"/>
      <xs:element name="Euro" type="EuroAmountType" minOccurs="0"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func validateSimpleContent(t *testing.T, body string) []Violation {
	t.Helper()
	s := loadTestSchema(t, simpleContentXSD)
	got, err := Validate(context.Background(), s, []byte(`<Invoice xmlns="urn:t">`+body+`</Invoice>`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return got
}

func TestLoad_SimpleContent(t *testing.T) {
	t.Parallel()

	s := loadTestSchema(t, simpleContentXSD)
	amount, ok := s.Types[QName{Namespace: "urn:t", Local: "AmountType"}].(*ComplexType)
	if !ok {
		t.Fatalf("AmountType: got %T, want *ComplexType", s.Types[QName{Namespace: "urn:t", Local: "AmountType"}])
	}
	content, ok := amount.Content.(SimpleContent)
	if !ok {
		t.Fatalf("AmountType content: got %T, want SimpleContent", amount.Content)
	}
	if content.Restriction {
		t.Error("AmountType derives by extension, got Restriction")
	}
	if base, ok := amount.Base.(*BuiltinType); !ok || base.Name.Local != "decimal" {
		t.Errorf("AmountType base: got %v, want xs:decimal", amount.Base)
	}
	if len(amount.Attributes) != 2 {
		t.Errorf("AmountType attributes: got %d, want 2", len(amount.Attributes))
	}

	euro, ok := s.Types[QName{Namespace: "urn:t", Local: "EuroAmountType"}].(*ComplexType)
	if !ok {
		t.Fatal("EuroAmountType not registered as a complex type")
	}
	restricted, ok := euro.Content.(SimpleContent)
	if !ok || !restricted.Restriction {
		t.Fatalf("EuroAmountType content: got %#v, want a SimpleContent restriction", euro.Content)
	}
	if restricted.Inline == nil || restricted.Inline.Base == nil {
		t.Errorf("EuroAmountType inline simpleType: got %#v, want a linked one", restricted.Inline)
	}
	if len(restricted.Facets) != 1 {
		t.Errorf("EuroAmountType facets: got %d, want 1", len(restricted.Facets))
	}
}

func TestLoad_SimpleContentMalformed(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"no derivation": `<xs:simpleContent/>`,
		"no base":       `<xs:simpleContent><xs:extension/></xs:simpleContent>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			schema := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:complexType name="T">` + body + `</xs:complexType>
</xs:schema>`
			fsys := fstest.MapFS{"s.xsd": &fstest.MapFile{Data: []byte(schema)}}
			if _, err := Load(context.Background(), fsys, "s.xsd"); !errors.Is(err, ErrSchemaParse) {
				t.Errorf("Load: got %v, want ErrSchemaParse", err)
			}
		})
	}
}

func TestValidate_SimpleContentValid(t *testing.T) {
	t.Parallel()

	got := validateSimpleContent(t, `
  <Amount currencyID="EUR" scale="2">12.50</Amount>
  <Code listID=" ISO 4217 ">EUR</Code>
  <QualifiedCode listVersionID="1">USD</QualifiedCode>
  <Small>99</Small>
  <Euro currencyID="EUR">0.10</Euro>`)
	if len(got) != 0 {
		t.Errorf("got %d violations, want 0: %v", len(got), got)
	}
}

func TestValidate_SimpleContentChecksText(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		rule string
		path string
	}{
		{"extension of a built-in", `<Amount currencyID="EUR">19.00e</Amount>`, RuleLexical, "/Amount"},
		{"extension of a restricted simple type", `<Code>EURO</Code>`, RuleFacet, "/Code"},
		{"extension of a complex type", `<QualifiedCode listVersionID="1">EURO</QualifiedCode>`, RuleFacet, "/QualifiedCode"},
		{"restriction facet", `<Small>100.5</Small>`, RuleFacet, "/Small"},
		{"restriction keeps the base's lexical space", `<Small>1,5</Small>`, RuleLexical, "/Small"},
		{"restriction of an inline simple type", `<Euro currencyID="EUR">1.005</Euro>`, RuleFacet, "/Euro"},
		{"restriction facet over an inline simple type", `<Euro currencyID="EUR">-1</Euro>`, RuleFacet, "/Euro"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := validateSimpleContent(t, tc.body)
			if !containsRuleAtPath(got, tc.rule, tc.path) {
				t.Errorf("got %v, want a %s violation at %s", got, tc.rule, tc.path)
			}
		})
	}
}

func TestValidate_SimpleContentChecksAttributes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		body   string
		path   string
		reason string
	}{
		{"required attribute missing", `<Amount>1</Amount>`, "/Amount/@currencyID", "required"},
		{"attribute outside its type", `<Amount currencyID="EUR" scale="two">1</Amount>`, "/Amount/@scale", "integer"},
		{"fixed attribute mismatch", `<Code listID="ISO 3166">FR</Code>`, "/Code/@listID", "fixed"},
		{"inherited fixed attribute", `<QualifiedCode listVersionID="1" listID="X">FR</QualifiedCode>`, "/QualifiedCode/@listID", "fixed"},
		{"extension's own required attribute", `<QualifiedCode>FR</QualifiedCode>`, "/QualifiedCode/@listVersionID", "required"},
		{"inherited required attribute", `<Euro>1</Euro>`, "/Euro/@currencyID", "required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := validateSimpleContent(t, tc.body)
			saw := false
			for _, v := range got {
				if v.Rule == RuleAttribute && strings.HasSuffix(v.Path, tc.path) && strings.Contains(v.Reason, tc.reason) {
					saw = true
				}
			}
			if !saw {
				t.Errorf("got %v, want a %q attribute violation at %s", got, tc.reason, tc.path)
			}
		})
	}
}

// TestValidate_SimpleContentRestrictionAttributes pins XML Schema Part 1,
// 3.4.2: a restriction replaces the base's attribute use of the same name
// and drops a prohibited one, so SmallAmountType's optional currencyID
// wins over AmountType's required one and its scale is no longer an
// attribute use: its value goes unchecked as an xs:integer, and the
// attribute is undeclared (3.4.4, cvc-complex-type clause 3.2.1).
func TestValidate_SimpleContentRestrictionAttributes(t *testing.T) {
	t.Parallel()

	if got := validateSimpleContent(t, `<Small>1</Small>`); len(got) != 0 {
		t.Errorf("without scale: got %v, want no violation", got)
	}
	got := validateSimpleContent(t, `<Small scale="two">1</Small>`)
	if len(got) != 1 || got[0].Rule != RuleAttribute || !strings.Contains(got[0].Reason, "not declared") {
		t.Errorf("with scale: got %v, want one undeclared-attribute violation", got)
	}
}

func TestValidate_SimpleContentRejectsChildElements(t *testing.T) {
	t.Parallel()

	got := validateSimpleContent(t, `<Amount currencyID="EUR">1<Nested/></Amount>`)
	if !containsRuleAtPath(got, RuleStructure, "/Amount/") {
		t.Errorf("got %v, want a structure violation under /Amount", got)
	}
}

func TestValidateValue_ComplexTypeWithSimpleContent(t *testing.T) {
	t.Parallel()

	s := loadTestSchema(t, simpleContentXSD)
	small := s.Types[QName{Namespace: "urn:t", Local: "SmallAmountType"}]
	if got := ValidateValue("42", small); len(got) != 0 {
		t.Errorf("ValidateValue(42): got %v, want none", got)
	}
	if got := ValidateValue("420", small); len(got) != 1 {
		t.Errorf("ValidateValue(420): got %v, want one maxInclusive violation", got)
	}
	// Element content has no lexical space.
	invoice := s.Types[QName{Namespace: "urn:t", Local: "InvoiceType"}]
	if got := ValidateValue("anything", invoice); len(got) != 0 {
		t.Errorf("ValidateValue on element content: got %v, want none", got)
	}
}

// mutuallyExtendingSimpleContentXSD holds two complex types whose simple
// content extends the other. Like a self-restricting simpleType it loads,
// since the loader checks only that each base resolves.
const mutuallyExtendingSimpleContentXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:complexType name="A"><xs:simpleContent><xs:extension base="B"/></xs:simpleContent></xs:complexType>
  <xs:complexType name="B">
    <xs:simpleContent>
      <xs:restriction base="A"><xs:maxLength value="5"/><xs:attribute name="a" use="required"/></xs:restriction>
    </xs:simpleContent>
  </xs:complexType>
  <xs:element name="Root" type="A"/>
</xs:schema>`

func TestValidate_CyclicSimpleContentTerminates(t *testing.T) {
	t.Parallel()

	s := loadTestSchema(t, mutuallyExtendingSimpleContentXSD)
	if _, err := Validate(context.Background(), s, []byte(`<Root xmlns="urn:t">far too long to pass</Root>`)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestCollectAllFacets_CyclicSimpleContentTerminates pins that a simple
// content chain returning to itself stays under the same bound as a
// simpleType chain: see [maxTypeDerivationDepth].
func TestCollectAllFacets_CyclicSimpleContentTerminates(t *testing.T) {
	t.Parallel()

	a := &ComplexType{Name: QName{Local: "A"}, Content: SimpleContent{Restriction: true, Facets: []Facet{MaxLength{Value: 5}}}}
	b := &ComplexType{Name: QName{Local: "B"}, Content: SimpleContent{}}
	a.Base = b
	b.Base = a

	steps, base, _ := walkDerivation(a)
	if base != nil {
		t.Errorf("base = %v, want nil: a cyclic chain reaches no built-in", base)
	}
	if len(steps) == 0 || len(steps) > maxTypeDerivationDepth {
		t.Errorf("steps = %d, want between 1 and %d", len(steps), maxTypeDerivationDepth)
	}
}
