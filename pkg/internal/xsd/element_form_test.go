package xsd

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// elementFormXSD declares local elements under each form rule of XML
// Schema Part 1, 3.3.2: Line leaves its form to elementFormDefault, which
// the schema omits, so it is unqualified, and Note qualifies itself with
// form. Invoice, top-level, is in the target namespace either way.
const elementFormXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t">
  <xs:element name="Invoice">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Line" type="xs:string"/>
        <xs:element name="Note" type="xs:string" form="qualified" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

// qualifiedElementFormXSD sets elementFormDefault="qualified", which form
// unqualified overrides on Note.
const qualifiedElementFormXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:element name="Invoice">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Line" type="xs:string"/>
        <xs:element name="Note" type="xs:string" form="unqualified" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

// TestLoad_LocalElementNamespace pins the {target namespace} XML Schema
// Part 1, 3.3.2 gives an element declaration: the target namespace for a
// top-level one, and for a local one only when its form, or failing that
// elementFormDefault, is qualified.
func TestLoad_LocalElementNamespace(t *testing.T) {
	for name, tc := range map[string]struct {
		xsd  string
		want map[string]string
	}{
		"unqualified default": {elementFormXSD, map[string]string{"Line": "", "Note": "urn:t"}},
		"qualified default":   {qualifiedElementFormXSD, map[string]string{"Line": "urn:t", "Note": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			s := loadTestSchema(t, tc.xsd)
			root, ok := s.Elements[QName{Namespace: "urn:t", Local: "Invoice"}]
			if !ok {
				t.Fatal("the top-level Invoice is not indexed in the target namespace")
			}
			ct, ok := root.Type.(*ComplexType)
			if !ok {
				t.Fatal("Invoice is not of a complex type")
			}
			seq, ok := ct.Content.(Sequence)
			if !ok || len(seq.Particles) != 2 {
				t.Fatalf("Invoice content: got %#v, want a sequence of two particles", ct.Content)
			}
			for _, p := range seq.Particles {
				if want := tc.want[p.Element.Name.Local]; p.Element.Name.Namespace != want {
					t.Errorf("element %s: namespace %q, want %q", p.Element.Name.Local, p.Element.Name.Namespace, want)
				}
			}
		})
	}
}

// TestValidate_ElementQualification pins XML Schema Part 1, 3.3.4
// (cvc-elt) through the particle a child matches: an element matches a
// declaration when their namespaces are identical, so a child in the
// default namespace is not the unqualified one a schema declares, and the
// reverse.
func TestValidate_ElementQualification(t *testing.T) {
	for name, tc := range map[string]struct {
		xsd, doc string
		want     string
	}{
		"unqualified matches":              {elementFormXSD, `<t:Invoice xmlns:t="urn:t"><Line>a</Line><t:Note>b</t:Note></t:Invoice>`, ""},
		"qualified is not unqualified":     {elementFormXSD, `<Invoice xmlns="urn:t"><Line>a</Line></Invoice>`, "expected Line"},
		"form qualified wants the tns":     {elementFormXSD, `<t:Invoice xmlns:t="urn:t"><Line>a</Line><Note>b</Note></t:Invoice>`, "Note"},
		"qualified default matches":        {qualifiedElementFormXSD, `<Invoice xmlns="urn:t"><Line>a</Line><Note xmlns="">b</Note></Invoice>`, ""},
		"qualified default wants the tns":  {qualifiedElementFormXSD, `<t:Invoice xmlns:t="urn:t"><Line>a</Line></t:Invoice>`, "expected {urn:t}Line"},
		"form unqualified wants no prefix": {qualifiedElementFormXSD, `<Invoice xmlns="urn:t"><Line>a</Line><Note>b</Note></Invoice>`, "Note"},
	} {
		t.Run(name, func(t *testing.T) {
			vs, err := Validate(context.Background(), loadTestSchema(t, tc.xsd), []byte(tc.doc))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if tc.want == "" {
				if len(vs) != 0 {
					t.Errorf("got %v, want none", vs)
				}
				return
			}
			if len(vs) == 0 || vs[0].Rule != RuleStructure || !strings.Contains(vs[0].Reason, tc.want) {
				t.Errorf("got %v, want a first structure violation naming %q", vs, tc.want)
			}
		})
	}
}

// TestLoad_ElementFormPerDocument pins that elementFormDefault is read off
// the schema document a local declaration sits in (XML Schema Part 1,
// 3.3.2, "the <schema> ancestor"): the included document omits it, so its
// local elements stay unqualified inside the qualifying includer.
func TestLoad_ElementFormPerDocument(t *testing.T) {
	const root = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:include schemaLocation="lines.xsd"/>
  <xs:element name="Invoice">
    <xs:complexType><xs:sequence><xs:element name="Lines" type="LinesType"/></xs:sequence></xs:complexType>
  </xs:element>
</xs:schema>`
	const lines = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">
  <xs:complexType name="LinesType"><xs:sequence><xs:element name="Line" type="xs:string"/></xs:sequence></xs:complexType>
</xs:schema>`
	s, err := Load(context.Background(), fstest.MapFS{
		"root.xsd":  &fstest.MapFile{Data: []byte(root)},
		"lines.xsd": &fstest.MapFile{Data: []byte(lines)},
	}, "root.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for doc, valid := range map[string]bool{
		`<Invoice xmlns="urn:t"><Lines><Line xmlns="">a</Line></Lines></Invoice>`: true,
		`<Invoice xmlns="urn:t"><Lines><Line>a</Line></Lines></Invoice>`:          false,
		`<t:Invoice xmlns:t="urn:t"><Lines><Line>a</Line></Lines></t:Invoice>`:    false,
	} {
		vs, err := Validate(context.Background(), s, []byte(doc))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if (len(vs) == 0) != valid {
			t.Errorf("%s: got %v, want valid=%v", doc, vs, valid)
		}
	}
}
