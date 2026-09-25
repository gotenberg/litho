package xsd

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// chameleonXSD has no targetNamespace and binds no default namespace, so
// each of its unprefixed references is in no namespace: a type, a base, an
// itemType, a memberTypes entry, an attribute ref and an attribute group
// ref. Its Short element is local and qualified, and PartType admits any
// attribute in the target namespace.
const chameleonXSD = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:simpleType name="Code"><xs:restriction base="xs:token"><xs:maxLength value="3"/></xs:restriction></xs:simpleType>
  <xs:simpleType name="Codes"><xs:list itemType="Code"/></xs:simpleType>
  <xs:simpleType name="CodeOrNumber"><xs:union memberTypes="Code xs:integer"/></xs:simpleType>
  <xs:simpleType name="Short"><xs:restriction base="Code"><xs:length value="2"/></xs:restriction></xs:simpleType>
  <xs:attribute name="flag" type="Code"/>
  <xs:attributeGroup name="PartAttributes">
    <xs:attribute name="unit" type="Code"/>
    <xs:attribute ref="flag"/>
  </xs:attributeGroup>
  <xs:complexType name="PartType">
    <xs:sequence>
      <xs:element name="Code" type="Code"/>
      <xs:element name="Codes" type="Codes"/>
      <xs:element name="Either" type="CodeOrNumber"/>
      <xs:element name="Short" type="Short" form="qualified"/>
    </xs:sequence>
    <xs:attributeGroup ref="PartAttributes"/>
    <xs:anyAttribute namespace="##targetNamespace" processContents="skip"/>
  </xs:complexType>
  <xs:element name="Part" type="PartType"/>
</xs:schema>`

// chameleonContent is content chameleonXSD's PartType admits once its
// components sit in the namespace prefix p is bound to.
func chameleonContent(p string) string {
	return `<Code>ab</Code><Codes>ab cd</Codes><Either>12345</Either><` + p + `:Short>ab</` + p + `:Short>`
}

// TestLoad_ChameleonInclude pins XML Schema Part 1, 4.2.1 (src-include
// clauses 2.3 and 3.2): a document with no targetNamespace that a document
// with one includes takes that target namespace. Its top-level components
// and its qualified local element move into it (clause 3.2.1), so does the
// ##targetNamespace of its attribute wildcard (clause 3.2.2), and each of
// its references in no namespace resolves there (the paragraph after clause
// 3), through a chameleon document it includes too.
func TestLoad_ChameleonInclude(t *testing.T) {
	root := func(include string) []byte {
		return []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:t" targetNamespace="urn:t">
  <xs:include schemaLocation="` + include + `"/>
  <xs:element name="Doc" type="t:PartType"/>
</xs:schema>`)
	}
	for name, files := range map[string]fstest.MapFS{
		"direct include": {
			"root.xsd": &fstest.MapFile{Data: root("cham.xsd")},
			"cham.xsd": &fstest.MapFile{Data: []byte(chameleonXSD)},
		},
		"through a chameleon include": {
			"root.xsd": &fstest.MapFile{Data: root("mid.xsd")},
			"mid.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:include schemaLocation="cham.xsd"/>
</xs:schema>`)},
			"cham.xsd": &fstest.MapFile{Data: []byte(chameleonXSD)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Load(context.Background(), files, "root.xsd")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if _, ok := s.Types[QName{Namespace: "urn:t", Local: "PartType"}]; !ok {
				t.Errorf("PartType is not in urn:t")
			}
			if _, ok := s.Types[QName{Local: "PartType"}]; ok {
				t.Errorf("PartType stayed in no namespace")
			}
			open := func(attrs string) string { return `<t:Doc xmlns:t="urn:t" xmlns:o="urn:o" ` + attrs + `>` }
			const flags = `unit="kg" t:flag="on"`
			for doc, want := range map[string]string{
				open(flags) + chameleonContent("t") + `</t:Doc>`:                                                        "",
				`<t:Part xmlns:t="urn:t" ` + flags + `>` + chameleonContent("t") + `</t:Part>`:                          "",
				open(flags+` t:extra="x"`) + chameleonContent("t") + `</t:Doc>`:                                         "",
				open(flags+` o:extra="x"`) + chameleonContent("t") + `</t:Doc>`:                                         RuleAttribute,
				open(`unit="kgs." t:flag="on"`) + chameleonContent("t") + `</t:Doc>`:                                    RuleAttribute,
				open(`unit="kg" t:flag="off!"`) + chameleonContent("t") + `</t:Doc>`:                                    RuleAttribute,
				open(flags) + strings.Replace(chameleonContent("t"), ">ab<", ">abcd<", 1) + `</t:Doc>`:                  RuleFacet,
				open(flags) + strings.Replace(chameleonContent("t"), "ab cd", "ab cdef", 1) + `</t:Doc>`:                RuleFacet,
				open(flags) + strings.Replace(chameleonContent("t"), "12345", "abcd", 1) + `</t:Doc>`:                   RuleLexical,
				open(flags) + strings.Replace(chameleonContent("t"), ">ab</t:Short>", ">abc</t:Short>", 1) + `</t:Doc>`: RuleFacet,
				open(flags) + strings.ReplaceAll(chameleonContent("t"), "t:Short", "Short") + `</t:Doc>`:                RuleStructure,
			} {
				vs := validateDoc(t, s, doc)
				if want == "" && len(vs) != 0 {
					t.Errorf("%s: got %v, want none", doc, vs)
				}
				if want != "" && (len(vs) == 0 || vs[0].Rule != want) {
					t.Errorf("%s: got %v, want a %s violation first", doc, vs, want)
				}
			}
		})
	}
}

// TestLoad_ChameleonIncludePerNamespace pins that each namespace including
// a chameleon document gets components of its own from it, per XML Schema
// Part 1, 4.2.1 (src-include clause 3.2), while the same document imported
// with no namespace, or included by a document with no targetNamespace
// (clause 2.2), keeps its components in no namespace.
func TestLoad_ChameleonIncludePerNamespace(t *testing.T) {
	files := fstest.MapFS{
		"root.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:a">
  <xs:include schemaLocation="cham.xsd"/>
  <xs:import namespace="urn:b" schemaLocation="b.xsd"/>
  <xs:import schemaLocation="plain.xsd"/>
</xs:schema>`)},
		"b.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:b">
  <xs:include schemaLocation="cham.xsd"/>
</xs:schema>`)},
		"plain.xsd": &fstest.MapFile{Data: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:include schemaLocation="cham.xsd"/>
</xs:schema>`)},
		"cham.xsd": &fstest.MapFile{Data: []byte(chameleonXSD)},
	}
	s, err := Load(context.Background(), files, "root.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := s.Types[QName{Namespace: "urn:a", Local: "PartType"}]
	b := s.Imports["urn:b"].Types[QName{Namespace: "urn:b", Local: "PartType"}]
	none := s.Imports[""].Types[QName{Local: "PartType"}]
	if a == nil || b == nil || none == nil || a == b || a == none || b == none {
		t.Fatalf("PartType in urn:a, urn:b and no namespace: found %t, %t and %t, want three distinct definitions",
			a != nil, b != nil, none != nil)
	}
	for _, doc := range []string{
		`<a:Part xmlns:a="urn:a" unit="kg" a:flag="on">` + chameleonContent("a") + `</a:Part>`,
		`<b:Part xmlns:b="urn:b" unit="kg" b:flag="on">` + chameleonContent("b") + `</b:Part>`,
		`<Part unit="kg" flag="on"><Code>ab</Code><Codes>ab cd</Codes><Either>12345</Either><Short>ab</Short></Part>`,
	} {
		if vs := validateDoc(t, s, doc); len(vs) != 0 {
			t.Errorf("%s: got %v, want none", doc, vs)
		}
	}
}
