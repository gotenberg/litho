package xsd

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// loadTestSchema is a small helper that compiles an inline XSD into a
// linked Schema. Each validation test builds its own schema rather
// than sharing one so the assertions stay close to the structure
// under test.
func loadTestSchema(t *testing.T, body string) *Schema {
	t.Helper()
	fsys := fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: []byte(body)}}
	s, err := Load(context.Background(), fsys, "schema.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

const invoiceXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="Currency">
    <xs:restriction base="xs:string">
      <xs:enumeration value="EUR"/>
      <xs:enumeration value="USD"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:complexType name="InvoiceType">
    <xs:sequence>
      <xs:element name="ID" type="xs:string"/>
      <xs:element name="Date" type="xs:date"/>
      <xs:element name="Currency" type="Currency"/>
      <xs:element name="Note" type="xs:string" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func TestValidate_HappyPath(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-001</ID>
  <Date>2026-05-17</Date>
  <Currency>EUR</Currency>
</Invoice>`)
	got, err := Validate(context.Background(), s, xmlBytes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d violations, want 0: %v", len(got), got)
	}
}

func TestValidate_WrongRoot(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?><Ghost xmlns="urn:t"/>`)
	got, err := Validate(context.Background(), s, xmlBytes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !containsRule(got, RuleRoot) {
		t.Errorf("expected RuleRoot violation, got %v", got)
	}
}

func TestValidate_MissingRequiredElement(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-001</ID>
  <Date>2026-05-17</Date>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRuleAtPath(got, RuleCardinality, "Currency") {
		t.Errorf("expected RuleCardinality on Currency, got %v", got)
	}
}

func TestValidate_OutOfOrderElement(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <Date>2026-05-17</Date>
  <ID>INV-001</ID>
  <Currency>EUR</Currency>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleStructure) {
		t.Errorf("expected RuleStructure on misordered children, got %v", got)
	}
}

func TestValidate_UnknownChildElement(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-001</ID>
  <Date>2026-05-17</Date>
  <Currency>EUR</Currency>
  <Bogus/>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleStructure) {
		t.Errorf("expected RuleStructure on Bogus child, got %v", got)
	}
}

func TestValidate_EnumerationFailure(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-001</ID>
  <Date>2026-05-17</Date>
  <Currency>BTC</Currency>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleFacet) {
		t.Errorf("expected RuleFacet on enumeration miss, got %v", got)
	}
}

func TestValidate_DateLexicalFailure(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-001</ID>
  <Date>17-05-2026</Date>
  <Currency>EUR</Currency>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleLexical) {
		t.Errorf("expected RuleLexical on bad date, got %v", got)
	}
}

func TestValidate_RepeatedOptionalElement(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	// Note has minOccurs=0 maxOccurs=unbounded; three of them should pass.
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-001</ID>
  <Date>2026-05-17</Date>
  <Currency>EUR</Currency>
  <Note>line A</Note>
  <Note>line B</Note>
  <Note>line C</Note>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if len(got) != 0 {
		t.Errorf("got %d violations, want 0: %v", len(got), got)
	}
}

func TestValidate_NilSchemaReturnsError(t *testing.T) {
	_, err := Validate(context.Background(), nil, []byte("<root/>"))
	if err == nil {
		t.Error("got nil error, want non-nil for nil schema")
	}
}

func TestValidate_MalformedXML(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	_, err := Validate(context.Background(), s, []byte(`<not-closed`))
	if err == nil {
		t.Error("got nil error, want parse failure")
	}
}

func TestValidate_ContextCancellation(t *testing.T) {
	s := loadTestSchema(t, invoiceXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>x</ID>
</Invoice>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Validate(ctx, s, xmlBytes)
	if err == nil {
		t.Error("got nil error, want ctx error after cancel")
	}
}

func containsRule(vs []Violation, rule string) bool {
	for _, v := range vs {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

func containsRuleAtPath(vs []Violation, rule, pathFragment string) bool {
	for _, v := range vs {
		if v.Rule == rule && strings.Contains(v.Path, pathFragment) {
			return true
		}
	}
	return false
}
