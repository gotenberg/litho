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
