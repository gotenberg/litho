package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPKnownNamespace_StillNeedsPredefining pins that recognising a namespace
// is not the same as predefining its properties. litho kept a list of XMP
// namespaces it knows, and the predefined-property audit deferred to
// [xmpCheckNamespaces] for anything outside the catalog. For a namespace on that
// list, neither spoke, so the property went unjudged.
//
// The IPTC Core schema is the case: govdocs thread_000_000163.pdf writes
// Iptc4xmpCore:CreatorContactInfo, CountryCode and Location, pdfjs
// issue18896.pdf adds Iptc4xmpExt:RecDevice, and issue21089.pdf writes the
// Version value type at top level as stVer:version. The XMP Specification this
// part admits predefines none of them and veraPDF reports every one.
func TestXMPKnownNamespace_StillNeedsPredefining(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		prefix string
		ns     string
		prop   string
	}{
		{"IPTC Core", "Iptc4xmpCore", "http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/", "Location"},
		{"IPTC Extension", "Iptc4xmpExt", "http://iptc.org/std/Iptc4xmpExt/2008-02-29/", "RecDevice"},
		{"a value type at top level", "stVer", "http://ns.adobe.com/xap/1.0/sType/Version#", "version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := xmpPacket(`<rdf:Description rdf:about=""
          xmlns:` + tc.prefix + `="` + tc.ns + `">
          <` + tc.prefix + `:` + tc.prop + `>v</` + tc.prefix + `:` + tc.prop + `>
        </rdf:Description>`)

			var got []Violation
			pooled := xmpPooledDeclaredProperties(ctx, nopDoc(), ast.PdfDict{}, data, 2)
			xmpCheckPredefinedProperties(ctx, data, 2, nil, pooled, &got)
			if !hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
				t.Errorf("%s:%s must report the undefined name, got %v", tc.prefix, tc.prop, got)
			}
		})
	}
}

// TestXMPKnownNamespace_ExtensionVocabularyStaysSilent pins the exception.
// ISO 19005-2 §6.6.2.3.2 requires a packet to declare its extension schemas, and
// that declaration is itself written as pdfaExtension:schemas with the schema,
// property, type and field vocabulary below it. Reporting that vocabulary would
// punish a packet for obeying the neighbouring clause, and veraPDF reports none
// of it.
func TestXMPKnownNamespace_ExtensionVocabularyStaysSilent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := xmpExtPacket("CustomNumber", "Text", "CustomNumber")

	var got []Violation
	pooled := xmpPooledDeclaredProperties(ctx, nopDoc(), ast.PdfDict{}, data, 2)
	xmpCheckPredefinedProperties(ctx, data, 2, nil, pooled, &got)
	if hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("the extension schema vocabulary must stay silent, got %v", got)
	}
}
