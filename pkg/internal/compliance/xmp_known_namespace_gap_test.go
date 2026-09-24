package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPKnownNamespace_StillNeedsPredefining pins that recognising a namespace
// is not the same as predefining its properties. litho kept a list of XMP
// namespaces it knows, and the predefined-property audit deferred to
// [xmpCheckNamespaces] for anything outside the catalog. For a namespace on that
// list, neither spoke, so the property went unjudged. The IPTC Core schema is
// the case.
//
// # Corpus evidence
//
//   - govdocs thread_000_000163.pdf: writes Iptc4xmpCore:CreatorContactInfo,
//     CountryCode and Location. The XMP Specification this part admits
//     predefines none of them, and veraPDF reports every one.
//   - pdfjs issue18896.pdf: adds Iptc4xmpExt:RecDevice, which the XMP
//     Specification does not predefine either, and veraPDF reports it.
//   - pdfjs issue21089.pdf: writes the Version value type at top level as
//     stVer:version, which the XMP Specification does not predefine either,
//     and veraPDF reports it.
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

// TestXMPKnownNamespace_TypeTestFollows pins that the value-type test answers
// for the same properties the name test does: a property no schema defines has
// no defined type either, which is why veraPDF's t2 failures always include its
// t1 failures.
func TestXMPKnownNamespace_TypeTestFollows(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:Iptc4xmpCore="http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/">
      <Iptc4xmpCore:Location>Paris</Iptc4xmpCore:Location>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("Iptc4xmpCore:Location must report the missing value type, got %v", got)
	}
}

// TestXMPKnownNamespace_QualifiersAndFieldsAreNotProperties pins the boundary
// the type test needs. Only a top-level property is a property: veraPDF reports
// neither a qualifier nor a struct field.
//
// # Corpus evidence
//
//   - verapdf 6-7-2-t07-*-j: these files hang an xmpidq:Scheme qualifier on
//     each xmp:Identifier item, and veraPDF reports none of them.
//   - verapdf 6-7-2-t03-fail-i: writes the stFnt fields of xmpTPg:Fonts, and
//     veraPDF reports none of them.
//   - verapdf: reporting them cost five false positives before this gate.
func TestXMPKnownNamespace_QualifiersAndFieldsAreNotProperties(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmp="http://ns.adobe.com/xap/1.0/"
      xmlns:xmpidq="http://ns.adobe.com/xmp/Identifier/qual/1.0/">
      <xmp:Identifier>
        <rdf:Bag>
          <rdf:li rdf:parseType="Resource">
            <rdf:value>id-1</rdf:value>
            <xmpidq:Scheme>internal</xmpidq:Scheme>
          </rdf:li>
        </rdf:Bag>
      </xmp:Identifier>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)
	for _, v := range got {
		if strings.Contains(v.Context, "Identifier/qual") {
			t.Errorf("a qualifier must not be judged as a property, got %v", v)
		}
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
