package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// schemaXMPPacket carries one property in a custom namespace, which the
// synthetic violation below names as schema-noncompliant.
const schemaXMPPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
	`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:acme="http://acme.example/ns/">` +
	`<acme:Widget>bad</acme:Widget>` +
	`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

// schemaMetadataPDF builds a one-page document whose catalog /Metadata holds
// [schemaXMPPacket], compressed when asked.
func schemaMetadataPDF(t *testing.T, compress bool) []byte {
	t.Helper()

	xmp := []byte(schemaXMPPacket)
	entry := "<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream"
	if compress {
		var deflated bytes.Buffer
		zw := zlib.NewWriter(&deflated)
		if _, err := zw.Write(xmp); err != nil {
			t.Fatalf("deflating XMP: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing deflate writer: %v", err)
		}
		xmp = deflated.Bytes()
		entry = "<< /Type /Metadata /Subtype /XML /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf(entry, len(xmp), xmp),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func stripSchemaProperty(t *testing.T, compress bool) (ast.PdfStream, []string) {
	t.Helper()

	ctx := context.Background()
	data := schemaMetadataPDF(t, compress)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	violations := []compliance.Violation{{
		Rule:    compliance.Rule{Clause: "6.6.2.3.1"},
		Context: "http://acme.example/ns/:Widget observed=Simple expected=Bag",
	}}
	fixes, err := fixXMPSchema(ctx, alloc, doc, Options{}, violations, "")
	if err != nil {
		t.Fatalf("fixXMPSchema: %v", err)
	}
	stream, _ := alloc.Pending()[4].(ast.PdfStream)
	return stream, fixes
}

// TestFixXMPSchemaDecodesCompressedMetadata pins that the offending property is
// stripped from a filtered /Metadata stream too.
//
// [ast.PdfStream.Bytes] returns the body before filter decoding, so the
// property search ran over compressed bytes, found nothing, and the fixer
// reported no repair: the ISO 19005-2, 6.6.2.3.1 violation stayed residual on
// every document whose XMP packet is compressed, which ISO 32000-1, 14.3.2
// allows.
func TestFixXMPSchemaDecodesCompressedMetadata(t *testing.T) {
	t.Parallel()

	stream, fixes := stripSchemaProperty(t, true)
	if len(fixes) == 0 {
		t.Fatal("no fix reported: the property was searched for in compressed bytes")
	}
	if stream.Dict == nil {
		t.Fatal("the metadata stream was not rewritten")
	}
	if _, filtered := stream.Dict["Filter"]; filtered {
		t.Error("the rewritten packet is stored decoded, so keeping /Filter leaves XMP no reader can parse")
	}
	if _, filtered := stream.Dict["DecodeParms"]; filtered {
		t.Error("/DecodeParms describes a filter chain the rewritten packet no longer uses")
	}
	body, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading rewritten packet: %v", err)
	}
	if bytes.Contains(body, []byte("acme:Widget")) {
		t.Errorf("the flagged property survived: %q", body)
	}
	if !bytes.Contains(body, []byte("<?xpacket")) {
		t.Errorf("the result is not an XMP packet: %q", body[:min(len(body), 60)])
	}
}

// TestFixXMPSchemaStripsPlainMetadata is the control: the same packet stored
// without a filter, which pins that compression is the only difference.
func TestFixXMPSchemaStripsPlainMetadata(t *testing.T) {
	t.Parallel()

	stream, fixes := stripSchemaProperty(t, false)
	if len(fixes) == 0 {
		t.Fatal("no fix reported on an uncompressed packet")
	}
	body, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading rewritten packet: %v", err)
	}
	if bytes.Contains(body, []byte("acme:Widget")) {
		t.Errorf("the flagged property survived: %q", body)
	}
}
