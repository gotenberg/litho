package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// checkXMPRDFTypePacket runs the two property audits over one packet, the pair
// veraPDF answers with 6.6.2.3.1 t1 and t2.
func checkXMPRDFTypePacket(t *testing.T, data []byte, part int) []Violation {
	t.Helper()
	ctx := context.Background()

	var got []Violation
	pooled := xmpPooledDeclaredProperties(ctx, nopDoc(), ast.PdfDict{}, data, part)
	xmpCheckPredefinedProperties(ctx, data, part, nil, pooled, &got)
	xmpCheckPropertyTypes(data, part, ast.PdfStream{}, nil, &got)
	return got
}

// TestXMPRDFType_IsAProperty pins that rdf:type written under a top-level
// rdf:Description is judged like any other property: no predefined schema
// defines it, so ISO 19005-2/3 §6.6.2.3.1 t1 reports the name and t2 reports the
// value type it therefore lacks. litho passed over the whole RDF namespace as
// structural and stayed silent.
//
// # Corpus evidence
//
//   - govdocs thread_000_000009, thread_000_000365, thread_400_400006,
//     thread_400_400364, thread_400_400723, thread_400_400724: these six
//     files write one, and veraPDF reports both tests for every one. The
//     packet below is the form thread_000_000009.pdf carries.
func TestXMPRDFType_IsAProperty(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about="">
      <rdf:type>dc:description</rdf:type>
    </rdf:Description>`)

	for _, tc := range []struct {
		part   int
		t1, t2 Rule
	}{
		{2, Rule_19005_2_6_6_2_3_1_t01, Rule_19005_2_6_6_2_3_1_t02},
		{3, Rule_19005_3_6_6_2_3_1_t01, Rule_19005_3_6_6_2_3_1_t02},
	} {
		got := checkXMPRDFTypePacket(t, data, tc.part)
		if !hasRule(got, tc.t1) {
			t.Errorf("part %d: rdf:type must report the undefined name, got %v", tc.part, got)
		}
		if !hasRule(got, tc.t2) {
			t.Errorf("part %d: rdf:type must report the missing value type, got %v", tc.part, got)
		}
	}
}

// TestXMPRDFType_StructuralRDFStaysSilent pins the boundary. Everything else the
// RDF namespace names carries the packet's structure rather than a property, and
// veraPDF reports none of it.
//
// # Corpus evidence
//
//   - All corpora, 3732 packets carrying RDF-namespace names directly under a
//     top-level rdf:Description: the scan found rdf:about 16763 times and no
//     element other than rdf:type.
func TestXMPRDFType_StructuralRDFStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			"rdf:about is RDF syntax, not a property",
			`<rdf:Description rdf:about="uuid:1"
               xmlns:dc="http://purl.org/dc/elements/1.1/">
               <dc:format>application/pdf</dc:format>
             </rdf:Description>`,
		},
		{
			"a struct's rdf:type is a field of that struct",
			`<rdf:Description rdf:about=""
               xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
               xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
               <xmpMM:DerivedFrom rdf:parseType="Resource">
                 <rdf:type rdf:resource="http://ns.adobe.com/xap/1.0/sType/ResourceRef#"/>
                 <stRef:instanceID>xmp.iid:1</stRef:instanceID>
               </xmpMM:DerivedFrom>
             </rdf:Description>`,
		},
		{
			"a container's rdf:li is RDF syntax",
			`<rdf:Description rdf:about=""
               xmlns:dc="http://purl.org/dc/elements/1.1/">
               <dc:title><rdf:Alt><rdf:li xml:lang="x-default">t</rdf:li></rdf:Alt></dc:title>
             </rdf:Description>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := checkXMPRDFTypePacket(t, xmpPacket(tc.body), 2)
			if hasRule(got, Rule_19005_2_6_6_2_3_1_t01) ||
				hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
				t.Errorf("RDF syntax must report nothing, got %v", got)
			}
		})
	}
}
