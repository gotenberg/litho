package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// nonStringTitlePacket carries the dc:title a producer wrote next to a /Title
// that holds no string.
const nonStringTitlePacket = `<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>B</pdfaid:conformance>
<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Test title</rdf:li></rdf:Alt></dc:title>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// nonStringTitlePDF builds a one-page document with [nonStringTitlePacket] and
// an /Info whose /Title is title, which may reference object 6, a stream.
func nonStringTitlePDF(title string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Title " + title + " >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(nonStringTitlePacket), nonStringTitlePacket),
		"<< /Length 10 >>\nstream\nTest title\nendstream",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 4 0 R /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// TestRemediatePDFA_DropsANonStringInfoEntry pins the PDF/A-1 repair of an
// /Info text entry that holds no string: the entry goes, and the analogue
// keeps what the packet says.
//
// veraPDF 1.30 compares such a value as the text its Java object renders, so
// a /Title referencing a stream fails 6.7.3 test 2 against any dc:title, an
// empty one included, and only its absence passes. The sync read the stream
// as an empty /Title and blanked dc:title, and the output failed veraPDF while
// litho, reading it the same way, passed it.
func TestRemediatePDFA_DropsANonStringInfoEntry(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, title string
	}{
		{name: "a stream", title: "6 0 R"},
		{name: "a dictionary", title: "<< /A 1 >>"},
		{name: "a number", title: "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, nonStringTitlePDF(tc.title))
			before, err := compliance.ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			flagged := false
			for _, v := range toViolations(before) {
				flagged = flagged || (v.Rule.Clause == "6.7.3" && v.Rule.TestNumber == 2)
			}
			if !flagged {
				t.Fatalf("precondition: the source does not fail 6.7.3 test 2; violations = %v", before)
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
				if v.Rule.Clause == "6.7.3" {
					t.Errorf("after one round: %v", v)
				}
			}
			info, err := document.ResolveDict(ctx, out, out.Index.Trailer["Info"])
			if err != nil {
				t.Fatalf("resolving the output /Info: %v", err)
			}
			if title, ok := info["Title"]; ok {
				t.Errorf("the output /Info keeps /Title %v", title)
			}
			if got, _ := analogueText(catalogPacket(t, out), "dc:title"); got != "Test title" {
				t.Errorf("dc:title = %q, want the packet's %q", got, "Test title")
			}
		})
	}
}

// TestFixXMP_KeepsANonStringInfoEntryOutsidePartOne pins the other side: no
// part but ISO 19005-1 compares /Info with XMP, so a PDF/A-2b run leaves the
// entry, and the sync leaves dc:title as the packet wrote it rather than
// blanking it for a value that holds no text.
func TestFixXMP_KeepsANonStringInfoEntryOutsidePartOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, nonStringTitlePDF("6 0 R"))
	fixes, err := fixXMP(ctx, alloc, doc, Options{}, nil, string(models.PDFA2b))
	if err != nil {
		t.Fatalf("fixXMP: %v", err)
	}
	if _, queued := alloc.Pending()[4]; queued {
		t.Errorf("a PDF/A-2b run rewrote /Info; fixes = %q", fixes)
	}
	ref, ok := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})["Metadata"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatal("the live catalog has no indirect /Metadata")
	}
	stream, err := resolveStreamFromRef(ctx, alloc, doc, ref)
	if err != nil {
		t.Fatalf("reading the live packet: %v", err)
	}
	if got, _ := analogueText(stream, "dc:title"); got != "Test title" {
		t.Errorf("dc:title = %q, want the packet's %q", got, "Test title")
	}
}

// resolveStreamFromRef returns the decoded bytes of the stream at ref, queued
// or in the source.
func resolveStreamFromRef(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ([]byte, error) {
	if s, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream); ok {
		return s.Bytes(ctx)
	}
	s, err := document.ResolveStream(ctx, doc, ref)
	if err != nil {
		return nil, err
	}
	return doc.DecodeStream(ctx, s)
}
