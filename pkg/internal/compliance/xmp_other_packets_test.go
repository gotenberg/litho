package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// xmpScopePacket wraps body in a minimal, well-formed XMP packet.
func xmpScopePacket(body string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
		`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" ` +
		`xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/" ` +
		`xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/" ` +
		`xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
		`<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>` +
		body +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
}

// twoPacketPDF builds a PDF whose catalog carries catalogBody as its XMP and
// whose page resources hang a second metadata stream carrying otherBody off a
// marked-content property list.
//
// # Corpus evidence
//
//   - pdfcpu adobe_errata.pdf: uses this shape.
func twoPacketPDF(catalogBody, otherBody string) []byte {
	cat := xmpScopePacket(catalogBody)
	other := xmpScopePacket(otherBody)
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Metadata 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Properties << /MC0 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length 10 >>\nstream\n0 0 0 rg\nendstream",
		"<< /Metadata 7 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(cat), cat),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(other), other),
	)
}

// firesXMPProperty reports whether validating data raises §6.6.2.3.1 test.
func firesXMPProperty(t *testing.T, data []byte, test int) bool {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.6.2.3.1" && v.Rule.TestNumber == test {
			return true
		}
	}
	return false
}

// TestXMPOtherPackets_PropertyAudits pins that the property-level XMP audits
// reach a packet that is not the catalog's.
//
// ISO 19005-2 §6.6.2.3.1 constrains the properties of "all metadata streams
// present in the PDF", but litho ran the predefined-schema and property-type
// audits over the catalog packet alone; every other packet got only the
// packet-level tests.
//
// # Corpus evidence
//
//   - pdfcpu adobeImplOfPDFSpec.pdf, adobe_errata.pdf: each hangs a 23 KB
//     packet off a marked-content property list carrying photoshop:History,
//     which is not in the XMP 2005 Photoshop schema, and veraPDF reports it
//     under t1 and t2 on both.
func TestXMPOtherPackets_PropertyAudits(t *testing.T) {
	t.Parallel()

	const bad = `<photoshop:History>x</photoshop:History>`

	t.Run("an undefined property in a non-catalog packet fires", func(t *testing.T) {
		t.Parallel()
		if !firesXMPProperty(t, twoPacketPDF("", bad), 1) {
			t.Error("photoshop:History is not in the XMP 2005 Photoshop schema")
		}
	})

	t.Run("a clean non-catalog packet does not", func(t *testing.T) {
		t.Parallel()
		if firesXMPProperty(t, twoPacketPDF("", `<photoshop:City>Paris</photoshop:City>`), 1) {
			t.Error("photoshop:City is predefined and must not fire")
		}
	})

	t.Run("the catalog packet is still audited", func(t *testing.T) {
		t.Parallel()
		if !firesXMPProperty(t, twoPacketPDF(bad, ""), 1) {
			t.Error("the catalog packet must keep firing as it always did")
		}
	})
}

// TestXMPPropertyTypes_EmptyResourceStruct pins that rdf:parseType="Resource"
// marks a struct whether or not it carries a field.
//
// That attribute is the RDF/XML shorthand making an element's content a struct
// (RDF/XML Syntax Specification, 2.3.3), which the XMP Specification uses for
// ResourceRef values such as xmpMM:DerivedFrom. litho decided struct-ness from
// the presence of a child element, so an empty one read as a plain text value.
//
// # Corpus evidence
//
//   - govdocs thread_100_100847.pdf: writes
//     <xapMM:DerivedFrom rdf:parseType="Resource"></xapMM:DerivedFrom>, and
//     veraPDF, whose run on it passes 1.99M checks, reports nothing.
func TestXMPPropertyTypes_EmptyResourceStruct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			"an empty parseType Resource struct is a struct",
			`<xmpMM:DerivedFrom rdf:parseType="Resource"></xmpMM:DerivedFrom>`,
			false,
		},
		{
			"a populated parseType Resource struct is too",
			`<xmpMM:DerivedFrom rdf:parseType="Resource">` +
				`<xmpMM:DocumentID>uuid:1</xmpMM:DocumentID></xmpMM:DerivedFrom>`,
			false,
		},
		{
			"a genuine plain text value still fires",
			`<xmpMM:DerivedFrom>just text</xmpMM:DerivedFrom>`,
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesXMPProperty(t, twoPacketPDF(tc.body, ""), 2); got != tc.want {
				t.Errorf("§6.6.2.3.1 t2 violation = %v, want %v", got, tc.want)
			}
		})
	}
}
