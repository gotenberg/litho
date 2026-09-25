package xsd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// includeLibXSD declares Code, at most three characters, in urn:lib, a
// namespace only the included documents below import.
const includeLibXSD = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:lib">
  <xs:simpleType name="Code"><xs:restriction base="xs:token"><xs:maxLength value="3"/></xs:restriction></xs:simpleType>
</xs:schema>`

// includePartXSD is included into urn:t. It imports urn:lib, which the
// including document does not, and it references Note, which the including
// document declares.
const includePartXSD = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:l="urn:lib"
           targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
  <xs:complexType name="PartType">
    <xs:sequence>
      <xs:element name="Code" type="l:Code"/>
      <xs:element name="Note" type="Note"/>
    </xs:sequence>
    <xs:attributeGroup ref="PartAttributes"/>
    <xs:attribute ref="flag"/>
  </xs:complexType>
  <xs:attributeGroup name="PartAttributes"><xs:attribute name="unit" type="l:Code"/></xs:attributeGroup>
  <xs:attribute name="flag" type="l:Code"/>
</xs:schema>`

// includeRootXSD includes part.xsd, or the document that includes it, and
// declares the Note part.xsd references.
func includeRootXSD(include string) string {
	return `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">
  <xs:include schemaLocation="` + include + `"/>
  <xs:simpleType name="Note"><xs:restriction base="xs:string"><xs:maxLength value="5"/></xs:restriction></xs:simpleType>
  <xs:element name="Doc" type="PartType"/>
</xs:schema>`
}

// TestLoad_IncludedReferencesResolveInTheirDocument pins how a reference
// that an included document writes resolves. Per XML Schema Part 1, 4.2.1
// an xs:include brings the components of the included document into the
// including schema, so a QName in the target namespace resolves against
// every component of that schema. Per 3.15.3 (src-resolve clause 4) a QName
// in another namespace resolves when the schema document that carries it
// imports that namespace, whether or not the including one does.
func TestLoad_IncludedReferencesResolveInTheirDocument(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"direct include": {
			"root.xsd": &fstest.MapFile{Data: []byte(includeRootXSD("part.xsd"))},
			"part.xsd": &fstest.MapFile{Data: []byte(includePartXSD)},
			"lib.xsd":  &fstest.MapFile{Data: []byte(includeLibXSD)},
		},
		"nested include": {
			"root.xsd": &fstest.MapFile{Data: []byte(includeRootXSD("mid.xsd"))},
			"mid.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:include schemaLocation="part.xsd"/>
</xs:schema>`)},
			"part.xsd": &fstest.MapFile{Data: []byte(includePartXSD)},
			"lib.xsd":  &fstest.MapFile{Data: []byte(includeLibXSD)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Load(context.Background(), files, "root.xsd")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			const open = `<Doc xmlns="urn:t" xmlns:t="urn:t" `
			for doc, want := range map[string]string{
				open + `unit="kg" t:flag="on"><Code>ab</Code><Note>hi</Note></Doc>`:     "",
				open + `unit="kg" t:flag="on"><Code>abcd</Code><Note>hi</Note></Doc>`:   "length",
				open + `unit="kg" t:flag="on"><Code>ab</Code><Note>hello!</Note></Doc>`: "length",
				open + `unit="kgs." t:flag="on"><Code>ab</Code><Note>hi</Note></Doc>`:   "length",
				open + `unit="kg" t:flag="off!"><Code>ab</Code><Note>hi</Note></Doc>`:   "length",
			} {
				vs := validateDoc(t, s, doc)
				if want == "" && len(vs) != 0 || want != "" && (len(vs) != 1 || !strings.Contains(vs[0].Reason, want)) {
					t.Errorf("%s: got %v, want one violation naming %q", doc, vs, want)
				}
			}
		})
	}
}

// TestValidate_RootResolvesInEveryReachableDocument pins XML Schema Part 1,
// 5.2 and 3.15.4 (QName resolution (Instance)): the document element takes
// its declaration from the {element declarations} of the schema, which per
// 4.2.1 and 4.2.3 holds those of every document the validated one includes
// or imports, however deep. lib.xsd declares the Code element in urn:lib;
// the root document reaches it through an included document's import, or
// through an import of an import, and never imports urn:lib itself.
func TestValidate_RootResolvesInEveryReachableDocument(t *testing.T) {
	const lib = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:lib">
  <xs:element name="Code"><xs:simpleType><xs:restriction base="xs:token"><xs:maxLength value="3"/></xs:restriction></xs:simpleType></xs:element>
</xs:schema>`
	for name, files := range map[string]fstest.MapFS{
		"import of an included document": {
			"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:include schemaLocation="part.xsd"/>
</xs:schema>`)},
			"part.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
</xs:schema>`)},
			"lib.xsd": &fstest.MapFile{Data: []byte(lib)},
		},
		"import of an import": {
			"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:import namespace="urn:mid" schemaLocation="mid.xsd"/>
</xs:schema>`)},
			"mid.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:mid">
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
</xs:schema>`)},
			"lib.xsd": &fstest.MapFile{Data: []byte(lib)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Load(context.Background(), files, "root.xsd")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			for doc, want := range map[string]string{
				`<l:Code xmlns:l="urn:lib">ab</l:Code>`:   "",
				`<l:Code xmlns:l="urn:lib">abcd</l:Code>`: RuleFacet,
				`<l:Name xmlns:l="urn:lib">ab</l:Name>`:   RuleRoot,
			} {
				vs := validateDoc(t, s, doc)
				if want == "" && len(vs) != 0 {
					t.Errorf("%s: got %v, want none", doc, vs)
				}
				if want != "" && (len(vs) != 1 || vs[0].Rule != want) {
					t.Errorf("%s: got %v, want one %s violation", doc, vs, want)
				}
			}
		})
	}
}

// TestLoad_IncludedDocumentImportsItsOwnNamespaces pins XML Schema Part 1,
// 3.15.3 (src-resolve clause 4.2.2) from the other side: a QName an
// included document writes does not resolve through an xs:import of the
// including document.
func TestLoad_IncludedDocumentImportsItsOwnNamespaces(t *testing.T) {
	_, err := Load(context.Background(), fstest.MapFS{
		"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t">
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
  <xs:include schemaLocation="part.xsd"/>
  <xs:element name="Doc" type="PartType"/>
</xs:schema>`)},
		"part.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:l="urn:lib" targetNamespace="urn:t">
  <xs:complexType name="PartType"><xs:sequence><xs:element name="Code" type="l:Code"/></xs:sequence></xs:complexType>
</xs:schema>`)},
		"lib.xsd": &fstest.MapFile{Data: []byte(includeLibXSD)},
	}, "root.xsd")
	if !errors.Is(err, ErrUnresolvedRef) {
		t.Errorf("Load: got %v, want %v", err, ErrUnresolvedRef)
	}
}

// TestValidate_WildcardAttributeResolvesInEveryReachableDocument pins XML
// Schema Part 1, 3.4.4 (cvc-complex-type clause 3.2) and 3.15.4: an
// attribute a lax or strict attribute wildcard admits is assessed against
// the declaration of its name among the {attribute declarations} of the
// schema, which hold those of every document the validated one reaches.
// lib.xsd declares the code attribute in urn:lib, which the root document
// reaches only through an import of an import. A strict wildcard reported it
// undeclared, and a lax one let a value past its maxLength through.
func TestValidate_WildcardAttributeResolvesInEveryReachableDocument(t *testing.T) {
	for _, process := range []string{"strict", "lax"} {
		t.Run(process, func(t *testing.T) {
			s, err := Load(context.Background(), fstest.MapFS{
				"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:import namespace="urn:mid" schemaLocation="mid.xsd"/>
  <xs:element name="Doc"><xs:complexType><xs:anyAttribute namespace="urn:lib" processContents="` + process + `"/></xs:complexType></xs:element>
</xs:schema>`)},
				"mid.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:mid">
  <xs:import namespace="urn:lib" schemaLocation="lib.xsd"/>
</xs:schema>`)},
				"lib.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:lib">
  <xs:attribute name="code"><xs:simpleType><xs:restriction base="xs:token"><xs:maxLength value="3"/></xs:restriction></xs:simpleType></xs:attribute>
</xs:schema>`)},
			}, "root.xsd")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			const open = `<t:Doc xmlns:t="urn:t" xmlns:l="urn:lib" `
			for doc, want := range map[string]string{
				open + `l:code="abc"/>`:  "",
				open + `l:code="abcd"/>`: "length",
			} {
				vs := validateDoc(t, s, doc)
				if want == "" && len(vs) != 0 {
					t.Errorf("%s: got %v, want none", doc, vs)
				}
				if want != "" && (len(vs) != 1 || !strings.Contains(vs[0].Reason, want)) {
					t.Errorf("%s: got %v, want one violation naming %q", doc, vs, want)
				}
			}
		})
	}
}

// TestLoad_IncludeCycle pins that documents that include each other load.
// XML Schema Part 1, 4.2.1 does not forbid an include cycle, and per
// src-include clause 3 the schema of each document on it holds the
// components of all of them. a.xsd and b.xsd each reference a type the other
// declares. The loader copied an included document's components as the
// include returned, so the document the cycle closed on lacked those of the
// one still loading, and the load failed on an unresolved reference.
func TestLoad_IncludeCycle(t *testing.T) {
	files := fstest.MapFS{
		"a.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:include schemaLocation="b.xsd"/>
  <xs:complexType name="AType"><xs:sequence><xs:element name="B" type="BType"/></xs:sequence></xs:complexType>
  <xs:element name="Doc" type="AType"/>
</xs:schema>`)},
		"b.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns="urn:t" elementFormDefault="qualified">
  <xs:include schemaLocation="a.xsd"/>
  <xs:complexType name="BType"><xs:sequence><xs:element name="C" type="Code"/></xs:sequence></xs:complexType>
  <xs:simpleType name="Code"><xs:restriction base="xs:token"><xs:maxLength value="3"/></xs:restriction></xs:simpleType>
  <xs:element name="Other" type="AType"/>
</xs:schema>`)},
	}
	for _, root := range []string{"a.xsd", "b.xsd"} {
		t.Run(root, func(t *testing.T) {
			s, err := Load(context.Background(), files, root)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			for doc, want := range map[string]string{
				`<Doc xmlns="urn:t"><B><C>abc</C></B></Doc>`:     "",
				`<Other xmlns="urn:t"><B><C>abc</C></B></Other>`: "",
				`<Doc xmlns="urn:t"><B><C>abcd</C></B></Doc>`:    RuleFacet,
			} {
				vs := validateDoc(t, s, doc)
				if want == "" && len(vs) != 0 {
					t.Errorf("%s: got %v, want none", doc, vs)
				}
				if want != "" && (len(vs) != 1 || vs[0].Rule != want) {
					t.Errorf("%s: got %v, want one %s violation", doc, vs, want)
				}
			}
		})
	}
}

// TestLoad_IncludedTargetNamespace pins XML Schema Part 1, 4.2.1
// (src-include clause 2.1): an included document with a targetNamespace
// must name the one of its includer. The loader accepted any, and the
// included components joined the includer's schema under a namespace no
// reference to them names. A document without one is a chameleon include
// and takes its includer's (clause 2.3), and a document it includes in turn
// may name that namespace, as libxml2 accepts it.
func TestLoad_IncludedTargetNamespace(t *testing.T) {
	const part = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:other">
  <xs:simpleType name="Code"><xs:restriction base="xs:token"><xs:maxLength value="2"/></xs:restriction></xs:simpleType>
</xs:schema>`
	for name, tc := range map[string]struct {
		files   fstest.MapFS
		wantErr error
	}{
		"another namespace": {
			files: fstest.MapFS{
				"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t">
  <xs:include schemaLocation="part.xsd"/>
</xs:schema>`)},
				"part.xsd": &fstest.MapFile{Data: []byte(part)},
			},
			wantErr: ErrSchemaParse,
		},
		"an includer without one": {
			files: fstest.MapFS{
				"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:include schemaLocation="part.xsd"/>
</xs:schema>`)},
				"part.xsd": &fstest.MapFile{Data: []byte(part)},
			},
			wantErr: ErrSchemaParse,
		},
		"the namespace a chameleon includer takes": {
			files: fstest.MapFS{
				"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:other" xmlns="urn:other">
  <xs:include schemaLocation="mid.xsd"/>
  <xs:element name="Doc" type="Code"/>
</xs:schema>`)},
				"mid.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:include schemaLocation="part.xsd"/>
</xs:schema>`)},
				"part.xsd": &fstest.MapFile{Data: []byte(part)},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Load(context.Background(), tc.files, "root.xsd")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("Load: got %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if vs := validateDoc(t, s, `<Doc xmlns="urn:other">abc</Doc>`); len(vs) != 1 || vs[0].Rule != RuleFacet {
				t.Errorf("got %v, want one %s violation", vs, RuleFacet)
			}
		})
	}
}
