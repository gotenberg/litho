package xsd

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
)

const minimalXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test" elementFormDefault="qualified">
  <xs:simpleType name="MyID">
    <xs:restriction base="xs:string">
      <xs:maxLength value="50"/>
      <xs:pattern value="[A-Z0-9]+"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:complexType name="InvoiceType">
    <xs:sequence>
      <xs:element name="ID" type="MyID"/>
      <xs:element name="Date" type="xs:date"/>
      <xs:element name="Comment" type="xs:string" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="currency" type="xs:string" use="required"/>
  </xs:complexType>
  <xs:element name="Invoice" type="InvoiceType"/>
</xs:schema>`

func TestLoad_MinimalSchema(t *testing.T) {
	fsys := fstest.MapFS{
		"schema.xsd": &fstest.MapFile{Data: []byte(minimalXSD)},
	}
	s, err := Load(context.Background(), fsys, "schema.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.TargetNamespace != "urn:test" {
		t.Errorf("TargetNamespace: got %q, want %q", s.TargetNamespace, "urn:test")
	}

	invQN := QName{Namespace: "urn:test", Local: "Invoice"}
	inv, ok := s.Elements[invQN]
	if !ok {
		t.Fatalf("Invoice element not registered, have %v", s.Elements)
	}
	ct, ok := inv.Type.(*ComplexType)
	if !ok {
		t.Fatalf("Invoice type: got %T, want *ComplexType", inv.Type)
	}
	seq, ok := ct.Content.(Sequence)
	if !ok {
		t.Fatalf("Invoice content: got %T, want Sequence", ct.Content)
	}
	if len(seq.Particles) != 3 {
		t.Errorf("sequence particles: got %d, want 3", len(seq.Particles))
	}
	id := seq.Particles[0].Element
	if id == nil || id.Name.Local != "ID" {
		t.Fatalf("first particle: got %+v, want element ID", seq.Particles[0])
	}
	myID, ok := id.Type.(*SimpleType)
	if !ok {
		t.Fatalf("ID type: got %T, want *SimpleType", id.Type)
	}
	if myID.Name != (QName{Namespace: "urn:test", Local: "MyID"}) {
		t.Errorf("MyID QName: got %v, want urn:test/MyID", myID.Name)
	}
	if myID.Base == nil {
		t.Error("MyID base unresolved")
	} else if bt, ok := myID.Base.(*BuiltinType); !ok || bt.Name.Local != "string" {
		t.Errorf("MyID base: got %T %v, want *BuiltinType xs:string", myID.Base, myID.Base)
	}
	// Date references a built-in type; the link pass should have
	// resolved it.
	date := seq.Particles[1].Element
	if date.Type == nil {
		t.Error("Date type unresolved")
	}
	if _, ok := date.Type.(*BuiltinType); !ok {
		t.Errorf("Date type: got %T, want *BuiltinType", date.Type)
	}
	// Comment has unbounded maxOccurs.
	comment := seq.Particles[2].Element
	if comment.MaxOccurs != MaxUnbounded {
		t.Errorf("Comment MaxOccurs: got %d, want MaxUnbounded", comment.MaxOccurs)
	}
	// Attribute use parsed correctly.
	if len(ct.Attributes) != 1 {
		t.Fatalf("attributes: got %d, want 1", len(ct.Attributes))
	}
	if !ct.Attributes[0].Required {
		t.Error("currency attribute: got Required=false, want true")
	}
}

func TestLoad_FacetsCollected(t *testing.T) {
	fsys := fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: []byte(minimalXSD)}}
	s, err := Load(context.Background(), fsys, "schema.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	myID := s.Types[QName{Namespace: "urn:test", Local: "MyID"}].(*SimpleType)
	if len(myID.Facets) != 2 {
		t.Fatalf("facets: got %d, want 2", len(myID.Facets))
	}
	// Order follows the XSD source: maxLength then pattern.
	if ml, ok := myID.Facets[0].(MaxLength); !ok || ml.Value != 50 {
		t.Errorf("first facet: got %+v, want MaxLength{50}", myID.Facets[0])
	}
	if p, ok := myID.Facets[1].(Pattern); !ok || p.Value != "[A-Z0-9]+" {
		t.Errorf("second facet: got %+v, want Pattern{[A-Z0-9]+}", myID.Facets[1])
	}
}

const baseXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:lib" elementFormDefault="qualified">
  <xs:complexType name="PartyType">
    <xs:sequence>
      <xs:element name="Name" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
</xs:schema>`

const importerXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           xmlns:lib="urn:lib"
           targetNamespace="urn:app" elementFormDefault="qualified">
  <xs:import namespace="urn:lib" schemaLocation="base.xsd"/>
  <xs:element name="Owner" type="lib:PartyType"/>
</xs:schema>`

func TestLoad_ImportCrossSchemaRef(t *testing.T) {
	fsys := fstest.MapFS{
		"importer.xsd": &fstest.MapFile{Data: []byte(importerXSD)},
		"base.xsd":     &fstest.MapFile{Data: []byte(baseXSD)},
	}
	s, err := Load(context.Background(), fsys, "importer.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := s.Imports["urn:lib"]; !ok {
		t.Fatalf("imports missing urn:lib, have %v", s.Imports)
	}
	owner, ok := s.Elements[QName{Namespace: "urn:app", Local: "Owner"}]
	if !ok {
		t.Fatalf("Owner element missing")
	}
	if owner.Type == nil {
		t.Fatal("Owner type unresolved across import")
	}
	ct, ok := owner.Type.(*ComplexType)
	if !ok {
		t.Fatalf("Owner type: got %T, want *ComplexType", owner.Type)
	}
	if ct.Name != (QName{Namespace: "urn:lib", Local: "PartyType"}) {
		t.Errorf("Owner type name: got %v, want urn:lib/PartyType", ct.Name)
	}
}

const missingTypeXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">
  <xs:element name="Bad" type="GhostType"/>
</xs:schema>`

func TestLoad_UnresolvedRef(t *testing.T) {
	fsys := fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: []byte(missingTypeXSD)}}
	_, err := Load(context.Background(), fsys, "schema.xsd")
	if !errors.Is(err, ErrUnresolvedRef) {
		t.Errorf("err: got %v, want errors.Is(_, ErrUnresolvedRef)", err)
	}
}

const malformedXSD = `<?xml version="1.0"?><not-a-schema/>`

func TestLoad_MalformedSchema(t *testing.T) {
	fsys := fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: []byte(malformedXSD)}}
	_, err := Load(context.Background(), fsys, "schema.xsd")
	if !errors.Is(err, ErrSchemaParse) {
		t.Errorf("err: got %v, want errors.Is(_, ErrSchemaParse)", err)
	}
}

const extensionXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:complexType name="BaseType">
    <xs:sequence>
      <xs:element name="A" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
  <xs:complexType name="DerivedType">
    <xs:complexContent>
      <xs:extension base="BaseType">
        <xs:sequence>
          <xs:element name="B" type="xs:string"/>
        </xs:sequence>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
  <xs:element name="Wrap" type="DerivedType"/>
</xs:schema>`

func TestLoad_ComplexExtension(t *testing.T) {
	fsys := fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: []byte(extensionXSD)}}
	s, err := Load(context.Background(), fsys, "schema.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	derived := s.Types[QName{Namespace: "urn:t", Local: "DerivedType"}].(*ComplexType)
	if derived.Base == nil {
		t.Fatal("Derived.Base unresolved")
	}
	base, ok := derived.Base.(*ComplexType)
	if !ok || base.Name.Local != "BaseType" {
		t.Errorf("Derived.Base: got %T %v, want BaseType", derived.Base, derived.Base)
	}
	// Only the extension's own particle is captured; merging with the
	// base happens at validation time (PR3 territory), not at load.
	seq, ok := derived.Content.(Sequence)
	if !ok {
		t.Fatalf("Derived content: got %T, want Sequence", derived.Content)
	}
	if len(seq.Particles) != 1 || seq.Particles[0].Element.Name.Local != "B" {
		t.Errorf("Derived particles: got %+v, want [B]", seq.Particles)
	}
}
