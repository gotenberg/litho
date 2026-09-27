package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// xmpComponentCase is one component-packet probe with the findings veraPDF
// 1.30.2 reports for it, in [extScopeRules] form, at PDF/A-1b and at PDF/A-2b.
// PDF/A-3b reports what PDF/A-2b does, under the part-3 rules.
type xmpComponentCase struct {
	name                  string
	entries, props        string
	pageEntries, pageProp string
	want1b, want2b        string
}

// xmpComponentPageEntry renders a pdfaSchema entry whose prefix is optional
// and whose properties are rendered pdfaProperty items, so a case can plant a
// defect extScopeEntry cannot write.
func xmpComponentPageEntry(uri, prefix, props, extra string) string {
	entry := `<rdf:li rdf:parseType="Resource"><pdfaSchema:schema>s</pdfaSchema:schema>` +
		`<pdfaSchema:namespaceURI>` + uri + `</pdfaSchema:namespaceURI>`
	if prefix != "" {
		entry += `<pdfaSchema:prefix>` + prefix + `</pdfaSchema:prefix>`
	}
	return entry + `<pdfaSchema:property><rdf:Seq>` + props + `</rdf:Seq></pdfaSchema:property>` + extra + `</rdf:li>`
}

// xmpComponentProperty renders one pdfaProperty item. An empty description
// leaves pdfaProperty:description out.
func xmpComponentProperty(name, valueType, category, description string) string {
	item := `<rdf:li rdf:parseType="Resource"><pdfaProperty:name>` + name + `</pdfaProperty:name>` +
		`<pdfaProperty:valueType>` + valueType + `</pdfaProperty:valueType>` +
		`<pdfaProperty:category>` + category + `</pdfaProperty:category>`
	if description != "" {
		item += `<pdfaProperty:description>` + description + `</pdfaProperty:description>`
	}
	return item + `</rdf:li>`
}

// xmpComponentCases are the probes of [TestXMPComponentPackets], each measured
// with a crafted file on veraPDF 1.30.2 at PDF/A-1b, PDF/A-2b and PDF/A-3b.
func xmpComponentCases() []xmpComponentCase {
	const unknown = `<eu:foo xmlns:eu="http://example.com/eu/">bar</eu:foo>`
	declarePA := extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "Text"}})
	return []xmpComponentCase{
		{
			name:     "a namespace no packet declares",
			pageProp: unknown,
			want2b:   "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1",
		},
		{
			name:     "a namespace the catalog packet declares",
			entries:  declarePA,
			pageProp: `<ex:pa>x</ex:pa>`,
		},
		{
			name:        "a namespace its own packet declares",
			pageEntries: declarePA,
			pageProp:    `<ex:pa>x</ex:pa>`,
		},
		{
			name: "defects in its extension schema",
			pageEntries: xmpComponentPageEntry(extScopeEx, "ex",
				xmpComponentProperty("pa", "Text", "internal", "")+
					xmpComponentProperty("pb", "Nope", "internal", "d")+
					xmpComponentProperty("pc", "Text", "sideways", "d"), "") +
				xmpComponentPageEntry(extScopeEy, "", xmpComponentProperty("q", "Text", "internal", "d"), "") +
				xmpComponentPageEntry("http://example.com/ez/", "ez", xmpComponentProperty("r", "Text", "internal", "d"),
					`<pdfaSchema:valueType><rdf:Seq><rdf:li rdf:parseType="Resource"><pdfaType:type>T</pdfaType:type>`+
						`<pdfaType:namespaceURI>http://example.com/ezt/</pdfaType:namespaceURI><pdfaType:prefix>ezt</pdfaType:prefix>`+
						`<pdfaType:field><rdf:Seq><rdf:li rdf:parseType="Resource"><pdfaField:name>a</pdfaField:name>`+
						`<pdfaField:valueType>Text</pdfaField:valueType><pdfaField:description>d</pdfaField:description></rdf:li>`+
						`</rdf:Seq></pdfaType:field></rdf:li></rdf:Seq></pdfaSchema:valueType><dc:stray>x</dc:stray>`),
			pageProp: `<ex:pa>x</ex:pa>`,
			want2b: "6.6.2.3.2-t01:1,6.6.2.3.3-t04:1,6.6.2.3.3-t08:1,6.6.2.3.3-t09:1," +
				"6.6.2.3.3-t10:1,6.6.2.3.3-t14:1",
		},
		{
			name: "types its entry resolves through the catalog's",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pm", "MainT"}},
				extScopeType("MainT", extScopeEt, [][2]string{{"a", "Text"}})),
			pageEntries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "MainT"}}) +
				extScopeEntry(extScopeEy, "ey", nil, extScopeType("TW", extScopeEt, [][2]string{{"d", "Dimensions"}})),
			want2b: "6.6.2.3.3-t17:1",
		},
	}
}

// TestXMPComponentPackets pins the extension-schema tests on a metadata stream
// other than the catalog's. ISO 19005-2/3, 6.6.2.3.1 constrains the properties
// of every metadata stream in the file, and veraPDF 1.30.2 models each such
// stream as an XMPPackage with its XMPProperty and ExtensionSchemasContainer
// objects (GFPDMetadata.getXMPPackage), whose properties resolve through the
// packet's own extension schemas and the catalog packet's
// (AXLXMPPackage.getCurrentSchemasDefinitionPDFA_2_3). litho judged the
// properties of a component packet by name alone, so a namespace no packet
// declares passed, and never judged its extension schemas' structure. Part 1
// builds no package for such a stream, so PDF/A-1b stays silent.
func TestXMPComponentPackets(t *testing.T) {
	t.Parallel()

	for _, tc := range xmpComponentCases() {
		for _, p := range []struct {
			part    int
			profile models.PDFAProfile
			want    string
		}{
			{1, models.PDFA1b, tc.want1b},
			{2, models.PDFA2b, tc.want2b},
			{3, models.PDFA3b, tc.want2b},
		} {
			t.Run(fmt.Sprintf("%s/%s", tc.name, p.profile), func(t *testing.T) {
				t.Parallel()
				got := extScopeRules(t, extScopePDF(p.part, tc.entries, tc.props, tc.pageEntries, tc.pageProp), p.profile)
				if got != p.want {
					t.Errorf("got [%s], veraPDF reports [%s]", got, p.want)
				}
			})
		}
	}
}
