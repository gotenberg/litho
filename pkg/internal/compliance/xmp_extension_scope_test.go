package compliance

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// extScopeNS binds every prefix the extension-scope probes write.
const extScopeNS = ` xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"` +
	` xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"` +
	` xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"` +
	` xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"` +
	` xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"` +
	` xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#"` +
	` xmlns:dc="http://purl.org/dc/elements/1.1/"` +
	` xmlns:xmp="http://ns.adobe.com/xap/1.0/"` +
	` xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"` +
	` xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#"` +
	` xmlns:stDim="http://ns.adobe.com/xap/1.0/sType/Dimensions#"` +
	` xmlns:stFnt="http://ns.adobe.com/xap/1.0/sType/Font#"` +
	` xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#"` +
	` xmlns:stVer="http://ns.adobe.com/xap/1.0/sType/Version#"` +
	` xmlns:xmpG="http://ns.adobe.com/xap/1.0/g/"` +
	` xmlns:xmpGImg="http://ns.adobe.com/xap/1.0/g/img/"` +
	` xmlns:xmpDM="http://ns.adobe.com/xmp/1.0/DynamicMedia/"` +
	` xmlns:ex="` + extScopeEx + `" xmlns:ey="` + extScopeEy + `" xmlns:et="` + extScopeEt + `"`

// The namespaces the probes declare properties and value types in.
const (
	extScopeEx = "http://example.com/ex/"
	extScopeEy = "http://example.com/ey/"
	extScopeEt = "http://example.com/et/"
)

// extScopeEntry renders one pdfaSchema entry of pdfaExtension:schemas. An
// empty uri leaves pdfaSchema:namespaceURI out and a nil props leaves
// pdfaSchema:property out; types are rendered [extScopeType] entries.
func extScopeEntry(uri, prefix string, props [][2]string, types ...string) string {
	var b strings.Builder
	b.WriteString(`<rdf:li rdf:parseType="Resource"><pdfaSchema:schema>s</pdfaSchema:schema>`)
	if uri != "" {
		fmt.Fprintf(&b, `<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>`, uri)
	}
	fmt.Fprintf(&b, `<pdfaSchema:prefix>%s</pdfaSchema:prefix>`, prefix)
	if props != nil {
		b.WriteString(`<pdfaSchema:property><rdf:Seq>`)
		for _, p := range props {
			fmt.Fprintf(&b, `<rdf:li rdf:parseType="Resource"><pdfaProperty:name>%s</pdfaProperty:name>`+
				`<pdfaProperty:valueType>%s</pdfaProperty:valueType><pdfaProperty:category>internal</pdfaProperty:category>`+
				`<pdfaProperty:description>d</pdfaProperty:description></rdf:li>`, p[0], p[1])
		}
		b.WriteString(`</rdf:Seq></pdfaSchema:property>`)
	}
	if len(types) > 0 {
		b.WriteString(`<pdfaSchema:valueType><rdf:Seq>` + strings.Join(types, "") + `</rdf:Seq></pdfaSchema:valueType>`)
	}
	b.WriteString(`</rdf:li>`)
	return b.String()
}

// extScopeType renders one pdfaSchema:valueType entry. An empty ns leaves
// pdfaType:namespaceURI out and a nil fields leaves pdfaType:field out.
func extScopeType(name, ns string, fields [][2]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<rdf:li rdf:parseType="Resource"><pdfaType:type>%s</pdfaType:type>`, name)
	if ns != "" {
		fmt.Fprintf(&b, `<pdfaType:namespaceURI>%s</pdfaType:namespaceURI>`, ns)
	}
	b.WriteString(`<pdfaType:prefix>et</pdfaType:prefix><pdfaType:description>d</pdfaType:description>`)
	if fields != nil {
		b.WriteString(`<pdfaType:field><rdf:Seq>`)
		for _, f := range fields {
			fmt.Fprintf(&b, `<rdf:li rdf:parseType="Resource"><pdfaField:name>%s</pdfaField:name>`+
				`<pdfaField:valueType>%s</pdfaField:valueType><pdfaField:description>d</pdfaField:description></rdf:li>`, f[0], f[1])
		}
		b.WriteString(`</rdf:Seq></pdfaType:field>`)
	}
	b.WriteString(`</rdf:li>`)
	return b.String()
}

// extScopePacket renders a packet carrying entries in pdfaExtension:schemas
// and props in a second rdf:Description. part > 0 claims PDF/A-<part>B.
func extScopePacket(part int, entries, props string) string {
	var head strings.Builder
	if part > 0 {
		fmt.Fprintf(&head, `<pdfaid:part>%d</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>`, part)
	}
	if entries != "" {
		head.WriteString(`<pdfaExtension:schemas><rdf:Bag>` + entries + `</rdf:Bag></pdfaExtension:schemas>`)
	}
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""` + extScopeNS + `>` + head.String() + `</rdf:Description>` +
		`<rdf:Description rdf:about=""` + extScopeNS + `>` + props + `</rdf:Description>` +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
}

// extScopePDF builds a one-page document whose catalog packet claims part and
// carries entries and props. A page packet is added when pageEntries or
// pageProps is not empty.
func extScopePDF(part int, entries, props, pageEntries, pageProps string) []byte {
	catalog := extScopePacket(part, entries, props)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(catalog), catalog),
	}
	if pageEntries != "" || pageProps != "" {
		page := extScopePacket(0, pageEntries, pageProps)
		objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 5 0 R >>"
		objs = append(objs, fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(page), page))
	}
	return cyclePDF(objs...)
}

// extScopeRules validates data at profile and returns its XMP schema findings
// (ISO 19005-1, 6.7.8 and 6.7.9, ISO 19005-2, 6.6.2) as veraPDF's report
// counts them: "clause-tNN:count", sorted and comma-separated.
func extScopeRules(t *testing.T, data []byte, profile models.PDFAProfile) string {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	counts := make(map[string]int)
	for _, e := range viols {
		v, ok := e.(Violation)
		if !ok {
			continue
		}
		c := v.Rule.Clause
		if strings.HasPrefix(c, "6.6.2") || c == "6.7.8" || c == "6.7.9" {
			counts[fmt.Sprintf("%s-t%02d", c, v.Rule.TestNumber)]++
		}
	}
	out := make([]string, 0, len(counts))
	for _, k := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, fmt.Sprintf("%s:%d", k, counts[k]))
	}
	return strings.Join(out, ",")
}

// extScopeCase is one probe with the findings veraPDF 1.30.2 reports for it at
// PDF/A-1b and PDF/A-2b, in [extScopeRules] form.
type extScopeCase struct {
	name                  string
	entries, props        string
	pageEntries, pageProp string
	want1b, want2b        string
}

// runExtScopeCases validates every case at PDF/A-1b and PDF/A-2b, each with a
// packet claiming its own part.
func runExtScopeCases(t *testing.T, cases []extScopeCase) {
	t.Helper()
	for _, tc := range cases {
		for _, p := range []struct {
			part    int
			profile models.PDFAProfile
			want    string
		}{{1, models.PDFA1b, tc.want1b}, {2, models.PDFA2b, tc.want2b}} {
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

// TestXMPExtensionTypeScope pins how an extension schema entry resolves the
// value types it names, each case measured on veraPDF 1.30.2 with a crafted
// file at PDF/A-1b and PDF/A-2b.
//
// veraPDF builds one validators container per schema entry
// (SchemasDefinitionCreator.registerAllPropertiesFromExtensionSchemaNode): the
// part's predefined types plus the types that entry defines, where a type with
// fields and no pdfaType:namespaceURI registers nothing
// (ValidatorsContainerCreator.registerTypeNode). An entry counts only with a
// namespace URI and a property array, the last one per URI replacing the
// others, and declares a property only when its type is known there
// (SchemasDefinition.registerProperty). A pdfaProperty:valueType or
// pdfaField:valueType is judged defined against the container of the entry
// that counts for its entry's URI, and against a container of the basic types
// alone when none counts (AXLExtensionSchemasContainer). A property no entry
// declares and no schema predefines fails both tests of 6.7.9 and 6.6.2.3.1.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-6-2-3-3-t03-fail-c.pdf: defines
//     a structured type with no namespace URI; veraPDF reports its property
//     undeclared and the type undefined, and litho reported neither.
func TestXMPExtensionTypeScope(t *testing.T) {
	t.Parallel()

	typeTB := extScopeType("TB", extScopeEt, [][2]string{{"a", "Text"}})
	useTB := `<ex:pa rdf:parseType="Resource"><et:a>x</et:a></ex:pa>`

	runExtScopeCases(t, []extScopeCase{
		{
			name:    "structured type with no namespace URI",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "TB"}}, extScopeType("TB", "", [][2]string{{"a", "Text"}})),
			props:   useTB,
			want1b:  "6.7.8-t09:1,6.7.8-t13:1,6.7.9-t02:1,6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1,6.6.2.3.3-t08:1,6.6.2.3.3-t12:1",
		},
		{
			name:    "structured type with a namespace URI",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "TB"}}, typeTB),
			props:   useTB,
		},
		{
			name: "type defined by another entry",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "TB"}}) +
				extScopeEntry(extScopeEy, "ey", [][2]string{{"pb", "Text"}}, typeTB),
			props:  useTB + `<ey:pb>y</ey:pb>`,
			want1b: "6.7.8-t09:1,6.7.9-t02:1,6.7.9-t03:1",
			want2b: "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1,6.6.2.3.3-t08:1",
		},
		{
			name:    "fieldless type with no namespace URI is Text",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "Alias"}}, extScopeType("Alias", "", nil)),
			props:   `<ex:pa>x</ex:pa>`,
			want1b:  "6.7.8-t13:1",
			want2b:  "6.6.2.3.3-t12:1",
		},
		{
			name: "type defined by a replaced entry of the same URI",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "TB"}}, typeTB) +
				extScopeEntry(extScopeEx, "ex", [][2]string{{"pc", "Text"}}),
			props:  useTB + `<ex:pc>z</ex:pc>`,
			want1b: "6.7.8-t09:1,6.7.9-t02:1,6.7.9-t03:1",
			want2b: "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1,6.6.2.3.3-t08:1",
		},
		{
			name: "field types of an entry with no property array",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "Text"}}) +
				extScopeEntry(extScopeEy, "ey", nil, extScopeType("TC", extScopeEt,
					[][2]string{{"d", "Dimensions"}, {"l", "Locale"}, {"s", "TC"}, {"t", "Text"}})),
			props:  `<ex:pa>x</ex:pa>`,
			want1b: "6.7.8-t18:3",
			want2b: "6.6.2.3.3-t17:3",
		},
		{
			name: "property types of an entry with no namespace URI",
			entries: extScopeEntry("", "ex", [][2]string{
				{"pa", "Dimensions"}, {"pb", "Text"}, {"pc", "Locale"}, {"pd", "GPSCoordinate"},
			}),
			want1b: "6.7.8-t04:1,6.7.8-t09:3",
			want2b: "6.6.2.3.3-t03:1,6.6.2.3.3-t08:3",
		},
		{
			name: "field type defined by another entry",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "TB"}}, extScopeType("TB", extScopeEt, [][2]string{{"a", "TZ"}})) +
				extScopeEntry(extScopeEy, "ey", [][2]string{{"pb", "TZ"}}, extScopeType("TZ", extScopeEt, [][2]string{{"z", "Text"}})),
			want1b: "6.7.8-t18:1",
			want2b: "6.6.2.3.3-t17:1",
		},
		{
			name:    "top-level property of a namespace only a value type names",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "TB"}}, typeTB),
			props:   useTB + `<et:a>y</et:a>`,
			want1b:  "6.7.9-t02:1,6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1",
		},
		{
			name:    "property of an entry with no property array",
			entries: extScopeEntry(extScopeEx, "ex", nil, typeTB),
			props:   `<ex:pa>x</ex:pa>`,
			want1b:  "6.7.9-t02:1,6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1",
		},
		{
			name:     "page packet property its namespace's main entry does not declare",
			entries:  extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "Text"}}),
			props:    `<ex:pa>x</ex:pa>`,
			pageProp: `<ex:pz>x</ex:pz>`,
			want2b:   "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1",
		},
		{
			name:    "pdfaid:rev in an extended identification namespace",
			entries: extScopeEntry("http://www.aiim.org/pdfa/ns/id/", "pdfaid", [][2]string{{"foo", "Text"}}),
			props:   `<pdfaid:rev>2020</pdfaid:rev>`,
			want1b:  "6.7.9-t02:1,6.7.9-t03:1",
			want2b:  "6.6.2.3.1-t01:1,6.6.2.3.1-t02:1",
		},
		{
			name:    "GPSCoordinate property",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "GPSCoordinate"}}),
			props:   `<ex:pa>10,20.5N</ex:pa>`,
			want1b:  "6.7.9-t03:1",
		},
		{
			name:    "Marker property",
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "Marker"}}),
			props:   `<ex:pa rdf:parseType="Resource"><xmpDM:comment>c</xmpDM:comment></ex:pa>`,
			want1b:  "6.7.8-t09:1,6.7.9-t02:1,6.7.9-t03:1",
		},
	})
}

// TestXMPExtensionDeclarationsPart1CatalogOnly pins that a page packet's
// extension schema declares nothing at PDF/A-1b, measured on veraPDF 1.30.2:
// the catalog packet extends ex without pa and uses pa, and a page packet
// declares pa, whether or not it uses it too. veraPDF builds no XMPPackage for
// a metadata stream other than the catalog's at part 1
// (GFPDMetadata.getXMPPackage), so the catalog's pa fails 6.7.9 t2 and t3.
func TestXMPExtensionDeclarationsPart1CatalogOnly(t *testing.T) {
	t.Parallel()

	entries := extScopeEntry(extScopeEx, "ex", [][2]string{{"pb", "Text"}})
	pageEntries := extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "Text"}})
	for _, pageProps := range []string{`<ex:pa>z</ex:pa>`, ""} {
		data := extScopePDF(1, entries, `<ex:pa>x</ex:pa><ex:pb>y</ex:pb>`, pageEntries, pageProps)
		if got, want := extScopeRules(t, data, models.PDFA1b), "6.7.9-t02:1,6.7.9-t03:1"; got != want {
			t.Errorf("page packet props %q: got [%s], veraPDF reports [%s]", pageProps, got, want)
		}
	}
}
