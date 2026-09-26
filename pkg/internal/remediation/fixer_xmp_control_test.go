package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// controlInfoPacket is a packet whose pdf:Producer already drops the control
// character /Producer holds.
const controlInfoPacket = `<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>B</pdfaid:conformance>
<pdf:Producer> version 2.51-b</pdf:Producer>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// controlInfoPDF builds a one-page document whose /Info is info, with
// [controlInfoPacket] as its catalog packet when withPacket is set.
func controlInfoPDF(info string, withPacket bool) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if withPacket {
		catalog = "<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>"
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		info,
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(controlInfoPacket), controlInfoPacket),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 4 0 R /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// TestRemediatePDFA_InfoControlCharacter pins the PDF/A-1 repair of an /Info
// text entry holding a control character, which no XMP analogue can equal:
// veraPDF 1.30.2 reads an undefined PDFDocEncoding byte as the control
// character it is, and reads one written into a packet, raw or referenced, as a
// space. The repair drops the character from the entry and writes the same
// text to the analogue, so the two agree in veraPDF and the packet stays
// well-formed XML.
//
// govdocs thread_100_100230.pdf is the first shape: a /Producer of
// "\003 version 2.51-b" and no packet. Its PDF/A-1b output failed 6.7.3 test 7
// on veraPDF, default and aggressive, while litho passed it.
func TestRemediatePDFA_InfoControlCharacter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, producer string
		withPacket     bool
		want           string
	}{
		{"undefined PDFDocEncoding byte, no packet", `(\003 version 2.51-b)`, false, " version 2.51-b"},
		{"undefined PDFDocEncoding byte, packet already without it", `(\003 version 2.51-b)`, true, " version 2.51-b"},
		{"DEL", `(v\1772)`, false, "v2"},
		{"form feed", `(a\014b)`, false, "ab"},
		{"UTF-16 control character", `<FEFF0061000300620062>`, false, "abb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, controlInfoPDF("<< /Producer "+tc.producer+" >>", tc.withPacket))
			before, err := compliance.ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			flagged := false
			for _, v := range toViolations(before) {
				flagged = flagged || (v.Rule.Clause == "6.7.3" && v.Rule.TestNumber == 7)
			}
			if !flagged {
				t.Fatalf("precondition: the source does not fail 6.7.3 test 7; violations = %v", before)
			}

			if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA1b, Options{}); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			out := rewriteAndReopen(t, doc, alloc)
			errs, err := compliance.ValidatePDFA(ctx, out, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA on the output: %v", err)
			}
			for _, v := range toViolations(errs) {
				if v.Rule.Clause == "6.7.3" || v.Rule.Clause == "6.7.2" {
					t.Errorf("after one round: %v", v)
				}
			}
			info, err := document.ResolveDict(ctx, out, out.Index.Trailer["Info"])
			if err != nil {
				t.Fatalf("resolving the output /Info: %v", err)
			}
			raw, ok := info["Producer"].(ast.PdfString)
			if !ok {
				t.Fatalf("the output /Producer is %v, want a string", info["Producer"])
			}
			if got := document.DecodePdfString(raw); got != tc.want || document.ContainsUndefinedPDFDocBytes(raw) {
				t.Errorf("/Producer = %q (bytes %q), want %q", got, string(raw), tc.want)
			}
			packet := catalogPacket(t, out)
			if !compliance.XMPWellFormed(packet) {
				t.Errorf("the output packet is not well-formed XML")
			}
			if got, _ := analogueText(packet, "pdf:Producer"); got != tc.want {
				t.Errorf("pdf:Producer = %q, want %q", got, tc.want)
			}
			if strings.ContainsFunc(string(packet), func(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' && r != '\r' }) {
				t.Errorf("the output packet holds a raw control character")
			}
		})
	}
}
