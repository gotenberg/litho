package xsd

import (
	"context"
	"strings"
	"testing"
)

const attrXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:complexType name="AmountType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute name="currencyID" type="xs:string" use="required"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  <xs:complexType name="InvoiceType">
    <xs:sequence>
      <xs:element name="ID" type="xs:string"/>
    </xs:sequence>
    <xs:attribute name="version" type="xs:string" use="required"/>
    <xs:attribute name="format" type="xs:string" use="optional" fixed="CII"/>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func TestValidate_Attributes_RequiredPresent(t *testing.T) {
	s := loadTestSchema(t, attrXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t" version="1.0">
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if len(got) != 0 {
		t.Errorf("got %d violations, want 0: %v", len(got), got)
	}
}

func TestValidate_Attributes_RequiredMissing(t *testing.T) {
	s := loadTestSchema(t, attrXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleAttribute) {
		t.Errorf("expected RuleAttribute for missing version, got %v", got)
	}
	// Locate the @version path fragment.
	saw := false
	for _, v := range got {
		if v.Rule == RuleAttribute && strings.Contains(v.Path, "@version") {
			saw = true
			break
		}
	}
	if !saw {
		t.Errorf("expected violation on @version, got %v", got)
	}
}

func TestValidate_Attributes_FixedMismatch(t *testing.T) {
	s := loadTestSchema(t, attrXSD)
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t" version="1.0" format="UBL">
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	saw := false
	for _, v := range got {
		if v.Rule == RuleAttribute && strings.Contains(v.Reason, "fixed attribute") {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected fixed-attribute violation, got %v", got)
	}
}

func TestValidate_Attributes_OptionalAbsent(t *testing.T) {
	s := loadTestSchema(t, attrXSD)
	// format is optional and fixed; absent is fine.
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t" version="1.0">
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	for _, v := range got {
		if v.Rule == RuleAttribute && strings.Contains(v.Path, "@format") {
			t.Errorf("optional absent attribute should not fire: %v", v)
		}
	}
}

const extensionContentXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:complexType name="HeaderType">
    <xs:sequence>
      <xs:element name="Title" type="xs:string"/>
    </xs:sequence>
    <xs:attribute name="kind" type="xs:string" use="required"/>
  </xs:complexType>
  <xs:complexType name="InvoiceType">
    <xs:complexContent>
      <xs:extension base="HeaderType">
        <xs:sequence>
          <xs:element name="ID" type="xs:string"/>
        </xs:sequence>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func TestValidate_Extension_MergesBaseParticles(t *testing.T) {
	s := loadTestSchema(t, extensionContentXSD)
	// Base's Title comes before the derived ID. Order matters
	// because we expect base-first semantics.
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t" kind="A">
  <Title>An invoice</Title>
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if len(got) != 0 {
		t.Errorf("got %d violations, want 0: %v", len(got), got)
	}
}

func TestValidate_Extension_DropsBaseElement(t *testing.T) {
	s := loadTestSchema(t, extensionContentXSD)
	// Title is required by the base. Omitting it should trip the
	// merged matcher, demonstrating the base content is enforced.
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t" kind="A">
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleStructure) && !containsRule(got, RuleCardinality) {
		t.Errorf("expected base-content enforcement violation, got %v", got)
	}
}

func TestValidate_Extension_MergesBaseAttributes(t *testing.T) {
	s := loadTestSchema(t, extensionContentXSD)
	// kind is required on the base. The derived type must inherit
	// that requirement; omitting kind should trip RuleAttribute.
	xmlBytes := []byte(`<?xml version="1.0"?>
<Invoice xmlns="urn:t">
  <Title>An invoice</Title>
  <ID>INV-1</ID>
</Invoice>`)
	got, _ := Validate(context.Background(), s, xmlBytes)
	if !containsRule(got, RuleAttribute) {
		t.Errorf("expected RuleAttribute from inherited base attribute, got %v", got)
	}
}
