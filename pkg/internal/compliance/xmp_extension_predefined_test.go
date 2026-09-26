package compliance

import "testing"

// TestXMPPredefinedPropertyThroughExtension pins how a property the XMP
// Specification predefines is judged when an extension schema names its
// namespace, each case measured on veraPDF 1.30.2 with a crafted file at
// PDF/A-1b and PDF/A-2b.
//
// veraPDF resolves a property through the extension schema entries of its
// packet first, then those of the main packet, and only then through the
// predefined schemas (AXLXMPProperty.getSchemasDefinition). A predefined
// property an entry declares with a type the entry knows is judged against
// that type, and one no entry declares against its predefined type, whatever
// else the entry declares for the namespace.
func TestXMPPredefinedPropertyThroughExtension(t *testing.T) {
	t.Parallel()

	const (
		dcNS        = "http://purl.org/dc/elements/1.1/"
		xmpNS       = "http://ns.adobe.com/xap/1.0/"
		titleAlt    = `<dc:title><rdf:Alt><rdf:li xml:lang="x-default">t</rdf:li></rdf:Alt></dc:title>`
		titleSimple = `<dc:title>t</dc:title>`
		mmNS        = "http://ns.adobe.com/xap/1.0/mm/"
		refNS       = "http://ns.adobe.com/xap/1.0/sType/ResourceRef#"
		derivedFrom = `<xmpMM:DerivedFrom rdf:parseType="Resource"><stRef:filePath>a</stRef:filePath></xmpMM:DerivedFrom>`
	)
	runExtScopeCases(t, []extScopeCase{
		{
			name:    "dc:title declared Text, held as a Lang Alt",
			entries: extScopeEntry(dcNS, "dc", [][2]string{{"title", "Text"}}),
			props:   titleAlt,
			want1b:  "6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t02:1",
		},
		{
			name:    "dc:title declared Text, held as text",
			entries: extScopeEntry(dcNS, "dc", [][2]string{{"title", "Text"}}),
			props:   titleSimple,
		},
		{
			name:    "dc extended with another name, dc:title held as text",
			entries: extScopeEntry(dcNS, "dc", [][2]string{{"foo", "Text"}}),
			props:   titleSimple + `<dc:foo>x</dc:foo>`,
			want1b:  "6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t02:1",
		},
		{
			name:    "dc extended with another name, dc:title held as a Lang Alt",
			entries: extScopeEntry(dcNS, "dc", [][2]string{{"foo", "Text"}}),
			props:   titleAlt + `<dc:foo>x</dc:foo>`,
		},
		{
			name:    "xmp:CreateDate declared Integer, holding a date",
			entries: extScopeEntry(xmpNS, "xmp", [][2]string{{"CreateDate", "Integer"}}),
			props:   `<xmp:CreateDate>2020-01-01</xmp:CreateDate>`,
			want1b:  "6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t02:1",
		},
		{
			name:    "xmp:CreateDate declared an unknown type, holding a date",
			entries: extScopeEntry(xmpNS, "xmp", [][2]string{{"CreateDate", "Nope"}}),
			props:   `<xmp:CreateDate>2020-01-01</xmp:CreateDate>`,
			want1b:  "6.7.8-t09:1",
			want2b:  "6.6.2.3.3-t08:1",
		},
		{
			name:    "xmp:CreateDate declared an unknown type, holding a word",
			entries: extScopeEntry(xmpNS, "xmp", [][2]string{{"CreateDate", "Nope"}}),
			props:   `<xmp:CreateDate>abc</xmp:CreateDate>`,
			want1b:  "6.7.8-t09:1,6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t02:1,6.6.2.3.3-t08:1",
		},
		{
			name:    "dc:title declared Lang Alt, held as a Lang Alt",
			entries: extScopeEntry(dcNS, "dc", [][2]string{{"title", "Lang Alt"}}),
			props:   titleAlt,
		},
		{
			name: "xmp:CreatorTool declared a structure, held as text",
			entries: extScopeEntry(xmpNS, "xmp", [][2]string{{"CreatorTool", "TB"}},
				extScopeType("TB", extScopeEt, [][2]string{{"a", "Text"}})),
			props:  `<xmp:CreatorTool>tool</xmp:CreatorTool>`,
			want1b: "6.7.9-t03:1",
			want2b: "6.6.2.3.1-t02:1",
		},
		{
			name: "xmpMM:DerivedFrom declared a type holding a later ResourceRef member",
			entries: extScopeEntry(mmNS, "xmpMM", [][2]string{{"DerivedFrom", "RefX"}},
				extScopeType("RefX", refNS, [][2]string{{"filePath", "Text"}})),
			props: derivedFrom,
		},
		{
			name:    "xmpMM extended with another name, xmpMM:DerivedFrom holding a later ResourceRef member",
			entries: extScopeEntry(mmNS, "xmpMM", [][2]string{{"Foo", "Text"}}),
			props:   derivedFrom + `<xmpMM:Foo>x</xmpMM:Foo>`,
			want1b:  "6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t02:1",
		},
		{
			name:    "an undefined dc name beside an extension of dc",
			entries: extScopeEntry(dcNS, "dc", [][2]string{{"foo", "Text"}}),
			props:   `<dc:bar>x</dc:bar>`,
			want1b:  "6.7.9-t02:1,6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1",
		},
	})
}
