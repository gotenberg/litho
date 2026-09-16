package compliance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestXMPMalformedPacketAnswersForItsSerializationAlone pins that a metadata
// packet which is not well-formed XML fails ISO 19005-2, 6.6.2.1 t4 and t5 and
// nothing that needs the parsed packet: the header's bytes and encoding
// attributes (t2, t3) and the property audits of 6.6.2.3.1 belong to veraPDF's
// XMPPackage and XMPProperty objects, which XMPCore builds only from a packet
// it parsed. govdocs thread_300_300234.pdf ships such a packet. Every case was
// measured against veraPDF.
func TestXMPMalformedPacketAnswersForItsSerializationAlone(t *testing.T) {
	t.Parallel()

	const body = "<x:xmpmeta xmlns:x='adobe:ns:meta/'><rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'>" +
		"<rdf:Description rdf:about='' xmlns:pdfaid='http://www.aiim.org/pdfa/ns/id/' pdfaid:part='2' pdfaid:conformance='B'" +
		" xmlns:acme='http://example.com/acme/' acme:Undeclared='x'/></rdf:RDF></x:xmpmeta>"
	malformed := strings.Replace(body, "</rdf:RDF>", "", 1)
	doc := func(header, packetBody string) []byte {
		packet := header + packetBody + "<?xpacket end='w'?>"
		const content = "q 0 0 10 10 re f Q"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(packet), packet),
		)
	}
	const (
		withBytes    = "<?xpacket begin='\ufeff' id='W5M0MpCehiHzreSzNTczkc9d' bytes='400'?>"
		withEncoding = "<?xpacket begin='\ufeff' id='W5M0MpCehiHzreSzNTczkc9d' encoding='UTF-8'?>"
	)

	for _, tc := range []struct {
		name   string
		data   []byte
		clause string
		test   int
		fires  bool
	}{
		{"a well-formed packet with bytes", doc(withBytes, body), "6.6.2.1", 2, true},
		{"a malformed packet with bytes", doc(withBytes, malformed), "6.6.2.1", 2, false},
		{"a malformed packet with encoding", doc(withEncoding, malformed), "6.6.2.1", 3, false},
		{"a malformed packet is not well-formed", doc(withBytes, malformed), "6.6.2.1", 4, true},
		{"a well-formed packet's undeclared property", doc(withEncoding, body), "6.6.2.3.1", 1, true},
		{"a malformed packet's undeclared property", doc(withEncoding, malformed), "6.6.2.3.1", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, tc.clause, tc.test); got != tc.fires {
				t.Errorf("%s t%d violation = %v, want %v", tc.clause, tc.test, got, tc.fires)
			}
		})
	}
}
