package semantics

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func pdfaPacket(description, body string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
		`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"` + description + `>` +
		body +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
}

// TestDeclaredPDFAPart reads pdfaid:part in both forms XMP gives a simple
// value, and finds no claim where the packet makes none or another namespace
// names the property.
func TestDeclaredPDFAPart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, xmp string
		want      int
	}{
		{"attribute", pdfaPacket(` pdfaid:part="1" pdfaid:conformance="B"`, ""), 1},
		{"element", pdfaPacket("", "<pdfaid:part>2</pdfaid:part><pdfaid:conformance>U</pdfaid:conformance>"), 2},
		{"no claim", pdfaPacket("", ""), 0},
		{"no packet", "", 0},
		{"not a part", pdfaPacket("", "<pdfaid:part>9</pdfaid:part>"), 0},
		{"other namespace", pdfaPacket(` xmlns:o="http://example.com/o/" o:part="1"`, ""), 0},
	} {
		data := pdfxPDF(t, "", tc.xmp)
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		if got := DeclaredPDFAPart(context.Background(), doc); got != tc.want {
			t.Errorf("%s: part %d, want %d", tc.name, got, tc.want)
		}
	}
}
