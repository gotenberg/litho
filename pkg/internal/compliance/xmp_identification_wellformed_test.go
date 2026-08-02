package compliance

import (
	"fmt"
	"testing"
)

// pdfaIDPacket returns an XMP packet claiming PDF/A-1b, with tail appended
// after the pdfaid fields so a defect there sits behind them.
func pdfaIDPacket(tail string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
   <pdfaid:part>1</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>` + tail + `
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
}

func pdfaIDPDF(packet string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(packet), packet),
	)
}

// TestPDFAIdentificationNeedsAWellFormedPacket covers what litho may recover
// from a packet whose defect sits AFTER the pdfaid fields. veraPDF's XMPCore
// parse fails outright, Identification_size reads 0, and it reports the
// missing-identification rule without evaluating any field. litho's lenient
// scan used to read pdfaid:part out of such a packet, so a packet it had just
// reported as an invalid serialization still counted as identified.
func TestPDFAIdentificationNeedsAWellFormedPacket(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tail string
		want bool
	}{
		{
			name: "a raw < after the identification loses it",
			tail: `<dc:x xmlns:dc="urn:d">a < b</dc:x>`,
			want: true,
		},
		{
			name: "a duplicate attribute after the identification loses it",
			tail: `<dc:x xmlns:dc="urn:d" q="1" q="2">a</dc:x>`,
			want: true,
		},
		{
			name: "an unclosed element after the identification loses it",
			tail: `<dc:x xmlns:dc="urn:d">a`,
			want: true,
		},
		{
			name: "a well-formed tail keeps the identification",
			tail: `<dc:x xmlns:dc="urn:d">a &lt; b</dc:x>`,
			want: false,
		},
		{
			name: "a control character keeps it, as veraPDF repairs those",
			tail: "<dc:x xmlns:dc=\"urn:d\">a\x07b</dc:x>",
			want: false,
		},
		{
			name: "no tail at all keeps it",
			tail: "",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := firesRule(t, pdfaIDPDF(pdfaIDPacket(tc.tail)), "6.7.11", 1)
			if got != tc.want {
				t.Errorf("missing-identification fired = %v, want %v", got, tc.want)
			}
		})
	}
}
