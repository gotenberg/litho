package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

const xmpTestExtNS = "http://www.custom.com/ns/machines/2/"

// xmpExtPacket builds a packet declaring one extension property for
// [xmpTestExtNS] and using one property of that namespace. Either declaration
// field is omitted when empty.
func xmpExtPacket(propName, propVT, used string) []byte {
	name, vt := "", ""
	if propName != "" {
		name = fmt.Sprintf("<pdfaProperty:name>%s</pdfaProperty:name>", propName)
	}
	if propVT != "" {
		vt = fmt.Sprintf("<pdfaProperty:valueType>%s</pdfaProperty:valueType>", propVT)
	}
	return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
      xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
      xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
      <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>Custom schema</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>` + xmpTestExtNS + `</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>cs</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          ` + name + vt + `
          <pdfaProperty:category>internal</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
      </rdf:li></rdf:Bag></pdfaExtension:schemas>
    </rdf:Description>
    <rdf:Description rdf:about="" xmlns:cs="` + xmpTestExtNS + `"><cs:` + used + `>v</cs:` + used + `></rdf:Description>`)
}

// TestExtensionSchema_DeclarationIsPerName pins that an extension schema
// legitimises the property NAMES it declares, not its whole namespace
// (ISO 19005-1 §6.7.9 t2, ISO 19005-2/3 §6.6.2.3.1 t1: veraPDF's
// isDefinedInCurrentPackage).
//
// # Corpus evidence
//
//   - verapdf 6-6-2-3-2-t01-pass-d.pdf: appending a cs:CProp usage passes
//     only because one of its page packets declares CProp; renaming that
//     declaration makes veraPDF report the usage, and a property in a
//     namespace nothing declares reports as well.
func TestExtensionSchema_DeclarationIsPerName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		propName string
		propVT   string
		used     string
		fires    bool
	}{
		{"the declared name is used", "CustomNumber", "Text", "CustomNumber", false},
		{"another name is used", "CustomNumber", "Text", "CProp", true},
		{"the declaration states no name", "", "Text", "CProp", true},
		{"the declaration states an undefined value type", "CProp", "NoSuchType", "CProp", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := xmpExtPacket(tc.propName, tc.propVT, tc.used)
			for part, rule := range map[int]Rule{
				1: Rule_19005_1_6_7_9_t02,
				2: Rule_19005_2_6_6_2_3_1_t01,
			} {
				pooled := xmpPooledDeclaredProperties(context.Background(),
					nopDoc(), ast.PdfDict{}, data, part)
				var got []Violation
				xmpCheckPredefinedProperties(context.Background(), data, part, nil, pooled, &got)
				if fired := hasRule(got, rule); fired != tc.fires {
					t.Errorf("part %d: undeclared-property violation = %v, want %v (%v)",
						part, fired, tc.fires, got)
				}
			}
		})
	}
}

// TestXMPPooledDeclaredProperties_ReplacementWithinAPacket pins that a later
// schema entry replaces an earlier one carrying the same namespace URI, the
// rule [xmpCheckExtensionPropertyResolution] already records for the type map.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__5-t05-fail-a.pdf: declares pdfuaid:part in its first
//     entry and pdfuaid:corr in a second for the same URI, and veraPDF
//     reports part. Deleting the second entry makes it report corr instead,
//     while part passes.
func TestXMPPooledDeclaredProperties_ReplacementWithinAPacket(t *testing.T) {
	t.Parallel()

	entry := func(prop string) string {
		return `<rdf:li rdf:parseType="Resource">
        <pdfaSchema:schema>Custom schema</pdfaSchema:schema>
        <pdfaSchema:namespaceURI>` + xmpTestExtNS + `</pdfaSchema:namespaceURI>
        <pdfaSchema:prefix>cs</pdfaSchema:prefix>
        <pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType="Resource">
          <pdfaProperty:name>` + prop + `</pdfaProperty:name>
          <pdfaProperty:valueType>Text</pdfaProperty:valueType>
          <pdfaProperty:category>internal</pdfaProperty:category>
          <pdfaProperty:description>d</pdfaProperty:description>
        </rdf:li></rdf:Seq></pdfaSchema:property>
      </rdf:li>`
	}
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
      xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
      xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">
      <pdfaExtension:schemas><rdf:Bag>` + entry("First") + entry("Second") +
		`</rdf:Bag></pdfaExtension:schemas></rdf:Description>`)

	pooled := xmpPooledDeclaredProperties(context.Background(), nopDoc(), ast.PdfDict{}, data, 2)
	props := pooled[xmpTestExtNS]
	if props == nil {
		t.Fatalf("namespace not pooled at all, got %v", pooled)
	}
	if props["First"] {
		t.Errorf("the replaced entry's property must not count, got %v", props)
	}
	if !props["Second"] {
		t.Errorf("the last entry's property must count, got %v", props)
	}
}

// TestXMPPooledDeclaredProperties_PoolsAcrossPackets pins that a declaration in
// any metadata packet legitimises a usage in another.
//
// # Corpus evidence
//
//   - verapdf 6-6-2-3-2-t01-pass-d.pdf: appending a cs:CProp usage to its
//     catalog packet passes because a page packet declares CProp; renaming
//     that declaration makes veraPDF report it.
func TestXMPPooledDeclaredProperties_PoolsAcrossPackets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	page := xmpExtPacket("CProp", "Text", "CProp")
	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 4 0 R >>"),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(page), page),
	)

	// The catalog packet declares nothing, yet the page packet's declaration
	// must legitimise the same name.
	catalogPacket := xmpPacket(
		`<rdf:Description rdf:about="" xmlns:cs="` + xmpTestExtNS + `"><cs:CProp>v</cs:CProp></rdf:Description>`)

	pooled := xmpPooledDeclaredProperties(ctx, doc, catalog, catalogPacket, 2)
	if !pooled[xmpTestExtNS]["CProp"] {
		t.Fatalf("a page packet's declaration must pool, got %v", pooled)
	}

	var got []Violation
	xmpCheckPredefinedProperties(ctx, catalogPacket, 2, map[string]bool{xmpTestExtNS: true}, pooled, &got)
	if hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("a pooled declaration must legitimise the usage, got %v", got)
	}
}
