package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// baseInvoiceXMP is a PDF/A-3 XMP packet with no fx: identification block, the
// shape RepairHybridInvoice is meant to complete.
const baseInvoiceXMP = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
	`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
	`<pdfaid:part>3</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>` +
	`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

// compressedMetadataInvoicePDF builds the repairPDF container with a catalog
// /Metadata stream whose XMP packet is FlateDecode compressed, which ISO
// 32000-1, 14.3.2 permits.
func compressedMetadataInvoicePDF(t *testing.T, compress bool) []byte {
	t.Helper()

	xmp := []byte(baseInvoiceXMP)
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
		"<< /Type /Catalog /Pages 2 0 R /Metadata 6 0 R" +
			" /Names << /EmbeddedFiles << /Names [(factur-x.xml) 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (factur-x.xml) /UF (factur-x.xml) /AFRelationship /Data /EF << /F 5 0 R >> >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fxml /Length 5 >>\nstream\n<x/>\n\nendstream",
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

// TestRepairHybridInvoicePatchesCompressedXMP pins that a compressed /Metadata
// packet is decoded before the fx: block is patched into it.
//
// [ast.PdfStream.Bytes] returns the raw body, so reading a FlateDecode
// /Metadata that way yields compressed bytes. The patcher found no packet to
// amend in them, returned its input unchanged, and the repair reported success
// while the fx: identification block Factur-X 1.0.07, 6 requires was never
// written.
func TestRepairHybridInvoicePatchesCompressedXMP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := compressedMetadataInvoicePDF(t, true)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := RepairHybridInvoice(ctx, doc, alloc); err != nil {
		t.Fatalf("RepairHybridInvoice: %v", err)
	}

	queued, ok := alloc.Pending()[6].(ast.PdfStream)
	if !ok {
		t.Fatalf("the XMP was not patched: %T", alloc.Pending()[6])
	}
	body, err := queued.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading patched XMP: %v", err)
	}
	if _, filtered := queued.Dict["Filter"]; filtered {
		t.Fatal("the patched packet is written unfiltered, so it must carry no /Filter")
	}
	if !bytes.Contains(body, []byte("<?xpacket")) {
		t.Fatalf("the patched /Metadata is not an XMP packet: %q", body[:min(len(body), 80)])
	}
	if !bytes.Contains(body, []byte("pdfaid:part")) {
		t.Error("the original XMP properties were dropped by the patch")
	}
	if !bytes.Contains(body, []byte("factur-x.xml")) {
		t.Error("the fx: identification block was not patched in")
	}
}

// TestRepairHybridInvoicePatchesPlainXMP is the control: the same container
// with an uncompressed packet, which pins that the fixture's attachment is
// located and that compression is the only difference between the two runs.
func TestRepairHybridInvoicePatchesPlainXMP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := compressedMetadataInvoicePDF(t, false)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := RepairHybridInvoice(ctx, doc, alloc); err != nil {
		t.Fatalf("RepairHybridInvoice: %v", err)
	}
	queued, ok := alloc.Pending()[6].(ast.PdfStream)
	if !ok {
		t.Fatalf("the XMP was not patched: %T", alloc.Pending()[6])
	}
	body, err := queued.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading patched XMP: %v", err)
	}
	if !bytes.Contains(body, []byte("factur-x.xml")) {
		t.Error("the fx: identification block was not patched in")
	}
}
