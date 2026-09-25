package xsd

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
)

// qnameLibXSD declares Code in urn:lib, a namespace the test schemas below
// import.
const qnameLibXSD = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:lib">
  <xs:simpleType name="Code"><xs:restriction base="xs:token"/></xs:simpleType>
</xs:schema>`

// TestLoad_QNameInterpretation pins XML Schema Part 1, 3.15.3 (QName
// Interpretation): a prefixed QName takes the namespace its prefix is bound
// to in scope at the element that carries it, and an unprefixed one the
// default namespace in scope there, or no namespace when none is. The target
// namespace plays no part. Each schema declares its own Code in urn:t beside
// the one of urn:lib, so a reference that resolved against the target
// namespace would land on the wrong type.
func TestLoad_QNameInterpretation(t *testing.T) {
	const head = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t"`
	const body = `>
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
  <xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>
  `
	for name, tc := range map[string]struct {
		root    string
		rootTNS string
		want    QName
		wantErr error
	}{
		"no default namespace": {
			root:    head + body + `<xs:element name="Root" type="Code"/></xs:schema>`,
			wantErr: ErrUnresolvedRef,
		},
		"default namespace is the target namespace": {
			root: head + ` xmlns="urn:t"` + body + `<xs:element name="Root" type="Code"/></xs:schema>`,
			want: QName{Namespace: "urn:t", Local: "Code"},
		},
		"default namespace is an imported one": {
			root: head + ` xmlns="urn:lib"` + body + `<xs:element name="Root" type="Code"/></xs:schema>`,
			want: QName{Namespace: "urn:lib", Local: "Code"},
		},
		"default namespace declared on the element": {
			root: head + ` xmlns="urn:t"` + body + `<xs:element name="Root" type="Code" xmlns="urn:lib"/></xs:schema>`,
			want: QName{Namespace: "urn:lib", Local: "Code"},
		},
		"default namespace undeclared on the element": {
			root:    head + ` xmlns="urn:t"` + body + `<xs:element name="Root" type="Code" xmlns=""/></xs:schema>`,
			wantErr: ErrUnresolvedRef,
		},
		"prefix declared on the element": {
			root: head + body + `<xs:element name="Root" type="l:Code" xmlns:l="urn:lib"/></xs:schema>`,
			want: QName{Namespace: "urn:lib", Local: "Code"},
		},
		"prefix out of scope": {
			root:    head + body + `<xs:element name="Root" xmlns:l="urn:lib"/><xs:element name="Other" type="l:Code"/></xs:schema>`,
			wantErr: ErrSchemaParse,
		},
		"no target namespace": {
			root: `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>
  <xs:element name="Root" type="Code"/>
</xs:schema>`,
			want: QName{Local: "Code"},
		},
		"XML Schema is the default namespace": {
			root: `<schema xmlns="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t">
  <simpleType name="Code"><restriction base="string"/></simpleType>
  <element name="Root" type="t:Code"/>
</schema>`,
			rootTNS: "urn:t",
			want:    QName{Namespace: "urn:t", Local: "Code"},
		},
		"annotation content stays as written": {
			root: head + ` xmlns="urn:t"` + body + `<xs:element name="Root" type="Code">
    <xs:annotation><xs:appinfo><xs:element name="Note" type="undeclared:Code"/></xs:appinfo></xs:annotation>
  </xs:element>
</xs:schema>`,
			want: QName{Namespace: "urn:t", Local: "Code"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Load(context.Background(), fstest.MapFS{
				"root.xsd": &fstest.MapFile{Data: []byte(tc.root)},
				"lib.xsd":  &fstest.MapFile{Data: []byte(qnameLibXSD)},
			}, "root.xsd")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Load: got %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			tns := tc.rootTNS
			if tns == "" && tc.want.Namespace != "" {
				tns = "urn:t"
			}
			root, ok := s.Elements[QName{Namespace: tns, Local: "Root"}]
			if !ok {
				t.Fatal("Root is not declared")
			}
			if got := root.Type.QName(); got != tc.want {
				t.Errorf("Root type: got %v, want %v", got, tc.want)
			}
			if st, ok := root.Type.(*SimpleType); ok && st.Base == nil {
				t.Errorf("the base of %v is unresolved", tc.want)
			}
		})
	}
}

// TestLoad_UnionMemberTypesInScope pins that each QName of a memberTypes
// list is interpreted on its own (XML Schema Part 1, 3.15.3), the
// unprefixed one against the default namespace, here not the target one.
func TestLoad_UnionMemberTypesInScope(t *testing.T) {
	s, err := Load(context.Background(), fstest.MapFS{
		"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           xmlns:t="urn:t" targetNamespace="urn:t" xmlns="urn:lib">
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
  <xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>
  <xs:simpleType name="Either"><xs:union memberTypes="t:Code  Code xs:int"/></xs:simpleType>
</xs:schema>`)},
		"lib.xsd": &fstest.MapFile{Data: []byte(qnameLibXSD)},
	}, "root.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	either, ok := s.Types[QName{Namespace: "urn:t", Local: "Either"}].(*SimpleType)
	if !ok {
		t.Fatal("Either is not a simple type")
	}
	want := []QName{
		{Namespace: "urn:t", Local: "Code"},
		{Namespace: "urn:lib", Local: "Code"},
		{Namespace: XSNamespace, Local: "int"},
	}
	if len(either.Members) != len(want) {
		t.Fatalf("members: got %d, want %d", len(either.Members), len(want))
	}
	for i, m := range either.Members {
		if m.QName() != want[i] {
			t.Errorf("member %d: got %v, want %v", i, m.QName(), want[i])
		}
	}
}
