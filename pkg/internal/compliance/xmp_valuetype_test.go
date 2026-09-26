package compliance

import (
	"fmt"
	"strings"
	"testing"
)

// extSchemaProbe is one extension schema entry of a test packet: the
// properties it declares and the value types it defines.
type extSchemaProbe struct {
	ns, prefix string
	props      [][2]string // name, valueType
	types      []extTypeProbe
}

// extTypeProbe is one pdfaSchema:valueType entry.
type extTypeProbe struct {
	name, ns, prefix string
	fields           [][2]string // name, valueType
}

// block renders the entry as the pdfaExtension:schemas bag holding it.
func (s extSchemaProbe) block() string {
	var b strings.Builder
	b.WriteString(`<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">`)
	fmt.Fprintf(&b, `<pdfaSchema:schema>probe</pdfaSchema:schema><pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI><pdfaSchema:prefix>%s</pdfaSchema:prefix>`, s.ns, s.prefix)
	b.WriteString(`<pdfaSchema:property><rdf:Seq>`)
	for _, p := range s.props {
		fmt.Fprintf(&b, `<rdf:li rdf:parseType="Resource"><pdfaProperty:name>%s</pdfaProperty:name><pdfaProperty:valueType>%s</pdfaProperty:valueType><pdfaProperty:category>internal</pdfaProperty:category><pdfaProperty:description>d</pdfaProperty:description></rdf:li>`, p[0], p[1])
	}
	b.WriteString(`</rdf:Seq></pdfaSchema:property>`)
	if len(s.types) > 0 {
		b.WriteString(`<pdfaSchema:valueType><rdf:Seq>`)
		for _, t := range s.types {
			fmt.Fprintf(&b, `<rdf:li rdf:parseType="Resource"><pdfaType:type>%s</pdfaType:type><pdfaType:namespaceURI>%s</pdfaType:namespaceURI><pdfaType:prefix>%s</pdfaType:prefix><pdfaType:description>d</pdfaType:description>`, t.name, t.ns, t.prefix)
			if len(t.fields) > 0 {
				b.WriteString(`<pdfaType:field><rdf:Seq>`)
				for _, f := range t.fields {
					fmt.Fprintf(&b, `<rdf:li rdf:parseType="Resource"><pdfaField:name>%s</pdfaField:name><pdfaField:valueType>%s</pdfaField:valueType><pdfaField:description>d</pdfaField:description></rdf:li>`, f[0], f[1])
				}
				b.WriteString(`</rdf:Seq></pdfaType:field>`)
			}
			b.WriteString(`</rdf:li>`)
		}
		b.WriteString(`</rdf:Seq></pdfaSchema:valueType>`)
	}
	b.WriteString(`</rdf:li></rdf:Bag></pdfaExtension:schemas>`)
	return b.String()
}

// extProbePacket renders a packet declaring schema in one rdf:Description and
// using usage, with the namespace bindings usageNS, in another.
func extProbePacket(schema extSchemaProbe, usageNS, usage string) []byte {
	return xmpPacket(`<rdf:Description rdf:about=""` +
		` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
		` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
		` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` +
		` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
		` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">` + schema.block() + `</rdf:Description>` +
		`<rdf:Description rdf:about=""` + usageNS + `>` + usage + `</rdf:Description>`)
}

// TestXMPCheckExtensionValueTypes pins the value of a property an extension
// schema declares against the type the declaration gives it, each case
// measured with a crafted file on veraPDF 1.30.2 at PDF/A-1b (6.7.9 t3) and
// PDF/A-2b (6.6.2.3.1 t2), which agree on every one.
//
// veraPDF validates the value as its ValidatorsContainer does: a container
// type the node's array form, item by item; a simple type a simple node, with
// the Boolean, Integer and Date types checking the text; a type the entry
// defines with fields a structure whose every field is one of them, in the
// type's namespace, and matches the field's type.
func TestXMPCheckExtensionValueTypes(t *testing.T) {
	t.Parallel()

	const (
		exNS  = "http://example.com/probe/"
		exsNS = "http://example.com/probe/s#"
		usage = ` xmlns:ex="` + exNS + `" xmlns:exs="` + exsNS + `"`
	)
	sType := []extTypeProbe{{name: "S", ns: exsNS, prefix: "exs", fields: [][2]string{{"a", "Text"}, {"b", "Text"}}}}
	// A type defined with no field, which veraPDF registers as Text.
	fieldless := []extTypeProbe{{name: "CVT", ns: exsNS, prefix: "exs"}}
	sDesc := `<ex:p><rdf:Bag><rdf:li><rdf:Description><exs:a>1</exs:a><exs:b>x</exs:b></rdf:Description></rdf:li></rdf:Bag></ex:p>`
	sRes := `<ex:p><rdf:Bag><rdf:li rdf:parseType="Resource"><exs:a>1</exs:a><exs:b>x</exs:b></rdf:li></rdf:Bag></ex:p>`
	decl := func(vt string, types ...extTypeProbe) extSchemaProbe {
		return extSchemaProbe{ns: exNS, prefix: "ex", props: [][2]string{{"p", vt}}, types: types}
	}

	cases := []struct {
		name   string
		schema extSchemaProbe
		usage  string
		want   bool
	}{
		{"bag Text of Description structures", decl("bag Text"), sDesc, true},
		{"bag of a defined type, Description structures", decl("bag S", sType...), sDesc, false},
		{"bag Text of parseType structures", decl("bag Text"), sRes, true},
		{"bag of a defined type, parseType structures", decl("bag S", sType...), sRes, false},
		{"Text holding a bag", decl("Text"), `<ex:p><rdf:Bag><rdf:li>x</rdf:li></rdf:Bag></ex:p>`, true},
		{"Integer holding letters", decl("Integer"), `<ex:p>abc</ex:p>`, true},
		{"Boolean holding yes", decl("Boolean"), `<ex:p>yes</ex:p>`, true},
		{"seq Text holding a bag", decl("seq Text"), `<ex:p><rdf:Bag><rdf:li>x</rdf:li></rdf:Bag></ex:p>`, true},
		{"Text holding a structure", decl("Text"), `<ex:p rdf:parseType="Resource"><exs:a>1</exs:a></ex:p>`, true},
		{"Lang Alt item without xml:lang", decl("Lang Alt"), `<ex:p><rdf:Alt><rdf:li>x</rdf:li></rdf:Alt></ex:p>`, true},
		{"bag Text of text", decl("bag Text"), `<ex:p><rdf:Bag><rdf:li>x</rdf:li></rdf:Bag></ex:p>`, false},
		{"bag Text holding a simple value", decl("bag Text"), `<ex:p>x</ex:p>`, true},
		{"Date holding a word", decl("Date"), `<ex:p>yesterday</ex:p>`, true},
		{"bag Text, empty bag", decl("bag Text"), `<ex:p><rdf:Bag/></ex:p>`, false},
		{"defined type, one declared field", decl("S", sType...), `<ex:p rdf:parseType="Resource"><exs:a>1</exs:a></ex:p>`, false},
		{"defined type, undeclared field", decl("S", sType...), `<ex:p rdf:parseType="Resource"><exs:z>1</exs:z></ex:p>`, true},
		{"bag of a defined type holding text", decl("bag S", sType...), `<ex:p><rdf:Bag><rdf:li>x</rdf:li></rdf:Bag></ex:p>`, true},
		{"bag Text, item a structure written as attributes", decl("bag Text"), `<ex:p><rdf:Bag><rdf:li exs:a="1"/></rdf:Bag></ex:p>`, true},
		{"bag Text, item a Description written as attributes", decl("bag Text"), `<ex:p><rdf:Bag><rdf:li><rdf:Description exs:a="1"/></rdf:li></rdf:Bag></ex:p>`, true},
		{"bag ProperName of structures", decl("bag ProperName"), sDesc, true},
		{"fieldless type holding text", decl("CVT", fieldless...), `<ex:p>text</ex:p>`, false},
		{"fieldless type holding a structure", decl("CVT", fieldless...), `<ex:p rdf:parseType="Resource"><exs:a>1</exs:a></ex:p>`, true},
		{"bag of a fieldless type holding text", decl("bag CVT", fieldless...), `<ex:p><rdf:Bag><rdf:li>x</rdf:li></rdf:Bag></ex:p>`, false},
	}

	for _, part := range []int{1, 2} {
		rule := Rule_19005_1_6_7_9_t03
		if part == 2 {
			rule = Rule_19005_2_6_6_2_3_1_t02
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("part %d, %s", part, tc.name), func(t *testing.T) {
				t.Parallel()
				var results []Violation
				xmpCheckExtensionValueTypes(extProbePacket(tc.schema, usage, tc.usage), part, nil, nil, &results)
				got := 0
				for _, v := range results {
					if v.Rule == rule {
						got++
					}
				}
				want := 0
				if tc.want {
					want = 1
				}
				if got != want {
					t.Errorf("%s fired %d times, want %d (%v)", rule.Clause, got, want, results)
				}
			})
		}
	}
}

// TestXMPCheckStructFieldNames_ExtensionDeclared pins that a property an
// extension schema declares is judged by the type its declaration gives it,
// not by the standard structure type its fields' namespace names: veraPDF
// 1.30.2 passes the PDF/A-1b remediation of pdfjs cmykjpeg.pdf, whose
// xmpMM:Manifest, absent from the XMP 2004 catalog, is declared with a type
// holding stRef:filePath, a field the 2004 ResourceRef does not define.
func TestXMPCheckStructFieldNames_ExtensionDeclared(t *testing.T) {
	t.Parallel()

	const refNS = "http://ns.adobe.com/xap/1.0/sType/ResourceRef#"
	schema := extSchemaProbe{
		ns: "http://example.com/probe/", prefix: "ex",
		props: [][2]string{{"ref", "RefStruct"}},
		types: []extTypeProbe{{name: "RefStruct", ns: refNS, prefix: "stRef", fields: [][2]string{{"filePath", "Text"}}}},
	}
	packet := extProbePacket(schema, ` xmlns:ex="http://example.com/probe/" xmlns:stRef="`+refNS+`"`,
		`<ex:ref rdf:parseType="Resource"><stRef:filePath>a.pdf</stRef:filePath></ex:ref>`)

	var undeclared []Violation
	xmpCheckStructFieldNames(packet, 1, nil, &undeclared)
	if !hasRule(undeclared, Rule_19005_1_6_7_9_t03) {
		t.Fatalf("precondition: stRef:filePath is no member of the 2004 ResourceRef, yet nothing fired: %v", undeclared)
	}

	var declared []Violation
	xmpCheckStructFieldNames(packet, 1, xmpExtensionDeclared(1, packet, nil), &declared)
	xmpCheckExtensionValueTypes(packet, 1, nil, nil, &declared)
	if len(declared) != 0 {
		t.Errorf("a property its extension schema declares, holding its declared type, fired: %v", declared)
	}
}

// TestXMPCheckExtensionValueTypes_ComponentArrayScope pins an array type in a
// component packet, measured on veraPDF 1.30.2 at PDF/A-2b with a page packet
// declaring xmpMM:Ingredients a bag of a type it defines itself. veraPDF
// passes it when the main packet has no extension schema entry for xmpMM, and
// fails it under 6.6.2.3.1 t2 when it has one: the component entry's validators
// are a copy of the main entry's, whose array validators look their item type
// up in the main entry, where the component's type is unknown.
func TestXMPCheckExtensionValueTypes_ComponentArrayScope(t *testing.T) {
	t.Parallel()

	const (
		mmNS  = "http://ns.adobe.com/xap/1.0/mm/"
		refNS = "http://ns.adobe.com/xap/1.0/sType/ResourceRef#"
	)
	component := extProbePacket(extSchemaProbe{
		ns: mmNS, prefix: "xmpMM",
		props: [][2]string{{"Ingredients", "bag IngStruct"}},
		types: []extTypeProbe{{name: "IngStruct", ns: refNS, prefix: "stRef", fields: [][2]string{{"filePath", "Text"}}}},
	}, ` xmlns:xmpMM="`+mmNS+`" xmlns:stRef="`+refNS+`"`,
		`<xmpMM:Ingredients><rdf:Bag><rdf:li rdf:parseType="Resource"><stRef:filePath>/a.png</stRef:filePath></rdf:li></rdf:Bag></xmpMM:Ingredients>`)
	mainWithMM := extProbePacket(extSchemaProbe{ns: mmNS, prefix: "xmpMM", props: [][2]string{{"OriginalDocumentID", "Text"}}}, "", "")

	for _, tc := range []struct {
		name string
		main []byte
		want bool
	}{
		{"main packet without an xmpMM entry", nil, false},
		{"main packet with an xmpMM entry", mainWithMM, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			xmpCheckExtensionValueTypes(component, 2, xmpMainExtensionEntries(2, tc.main), nil, &results)
			if got := hasRule(results, Rule_19005_2_6_6_2_3_1_t02); got != tc.want {
				t.Errorf("6.6.2.3.1 t2 fired = %v, want %v (%v)", got, tc.want, results)
			}
			if got := XMPExtensionPropertyChecker(component, tc.main, 2)(mmNS, "Ingredients"); got == tc.want {
				t.Errorf("the property checker reports the declaration answered = %v, want %v", got, !tc.want)
			}
		})
	}
}

// TestXMPCheckExtensionValueTypes_Fonts pins the xmpTPg:Fonts shapes of
// PDF/A-1, where the XMP 2004 catalog has no Fonts and no Font type, measured
// on veraPDF 1.30.2 at PDF/A-1b. A bag of stFnt structures passes 6.7.9 t3
// once the extension schema declares a bag of a type it defines with the
// stFnt fields, whatever the type is called and whether a field is declared
// Text or Boolean; it fails when the type omits a field the value holds, or
// when the declaration names another container or none.
func TestXMPCheckExtensionValueTypes_Fonts(t *testing.T) {
	t.Parallel()

	const (
		tpgNS = "http://ns.adobe.com/xap/1.0/t/pg/"
		fntNS = "http://ns.adobe.com/xap/1.0/sType/Font#"
	)
	fields := []string{"fontName", "fontFamily", "fontFace", "fontType", "versionString", "composite", "fontFileName"}
	values := []string{"Helvetica-Bold", "Helvetica", "Bold", "TrueType", "Helvetica-Bold6.0d7e1", "False", "Helvetica.dfont"}
	var desc strings.Builder
	allText := make([][2]string, 0, len(fields))
	for i, f := range fields {
		fmt.Fprintf(&desc, "<stFnt:%s>%s</stFnt:%s>", f, values[i], f)
		allText = append(allText, [2]string{f, "Text"})
	}
	usage := `<xmpTPg:Fonts><rdf:Bag><rdf:li><rdf:Description>` + desc.String() + `</rdf:Description></rdf:li></rdf:Bag></xmpTPg:Fonts>`
	boolField := append(append([][2]string{}, allText[:5]...), [2]string{"composite", "Boolean"}, [2]string{"fontFileName", "Text"})
	fontType := func(name string, f [][2]string) []extTypeProbe {
		return []extTypeProbe{{name: name, ns: fntNS, prefix: "stFnt", fields: f}}
	}
	decl := func(vt string, types []extTypeProbe) extSchemaProbe {
		return extSchemaProbe{ns: tpgNS, prefix: "xmpTPg", props: [][2]string{{"Fonts", vt}}, types: types}
	}

	for _, tc := range []struct {
		name   string
		schema extSchemaProbe
		want   bool
	}{
		{"bag Text", decl("bag Text", nil), true},
		{"bag of a defined FontX", decl("bag FontX", fontType("FontX", allText)), false},
		{"bag of a redefined Font", decl("bag Font", fontType("Font", allText)), false},
		{"type missing a field", decl("bag FontX", fontType("FontX", allText[:6])), true},
		{"a Boolean field", decl("bag FontX", fontType("FontX", boolField)), false},
		{"seq of the type", decl("seq FontX", fontType("FontX", allText)), true},
		{"the type alone", decl("FontX", fontType("FontX", allText)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			packet := extProbePacket(tc.schema, ` xmlns:xmpTPg="`+tpgNS+`" xmlns:stFnt="`+fntNS+`"`, usage)
			xmpCheckExtensionValueTypes(packet, 1, nil, nil, &results)
			got := false
			for _, v := range results {
				got = got || v.Rule == Rule_19005_1_6_7_9_t03
			}
			if got != tc.want {
				t.Errorf("6.7.9 t3 fired = %v, want %v (%v)", got, tc.want, results)
			}
		})
	}
}
