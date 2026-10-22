package compliance

import (
	"context"
	"strconv"
	"testing"
)

// TestClaimsAccessibility pins which metadata [ClaimsAccessibility] reads as a
// PDF/UA or WTPDF claim: a pdfuaid:part of any value (ISO 14289-1 and ISO
// 14289-2, 5) or a PDF Declaration naming a WTPDF 1.0 level, and nothing
// else.
func TestClaimsAccessibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name   string
		packet string
		want   bool
	}{
		{name: "no metadata", want: false},
		{name: "a PDF/A claim", packet: `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"><pdfaid:part>2</pdfaid:part></rdf:Description></rdf:RDF></x:xmpmeta>`, want: false},
		{name: "pdfuaid:part 1", packet: `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"><pdfuaid:part>1</pdfuaid:part></rdf:Description></rdf:RDF></x:xmpmeta>`, want: true},
		{name: "pdfuaid:part 2 as an attribute", packet: `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/" pdfuaid:part="2"/></rdf:RDF></x:xmpmeta>`, want: true},
		{name: "a WTPDF declaration", packet: string(wtpdfPacket(`<pdfd:declarations><rdf:Bag>` + wtpdfDeclarationItem(wtpdfReuseURI) + `</rdf:Bag></pdfd:declarations>`)), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [] /Count 0 >>"}
			if tc.packet != "" {
				objs[0] = "<< /Type /Catalog /Pages 2 0 R /Metadata 3 0 R >>"
				objs = append(objs, "<< /Type /Metadata /Subtype /XML /Length "+strconv.Itoa(len(tc.packet))+" >>\nstream\n"+tc.packet+"\nendstream")
			}
			doc, catalog := objsDocPDF(t, objs...)
			if got := ClaimsAccessibility(ctx, doc, catalog); got != tc.want {
				t.Errorf("ClaimsAccessibility = %v, want %v", got, tc.want)
			}
		})
	}
}
