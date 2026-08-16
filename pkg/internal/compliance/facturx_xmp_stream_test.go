package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// invoiceXMPPacket carries the four fx: properties Factur-X 1.0.07, 6 requires,
// so a reader that recovers it sees a conformant identification block.
const invoiceXMPPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
	`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:fx="urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#">` +
	`<fx:DocumentType>INVOICE</fx:DocumentType>` +
	`<fx:DocumentFileName>factur-x.xml</fx:DocumentFileName>` +
	`<fx:Version>1.0</fx:Version>` +
	`<fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>` +
	`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

// metadataStreamPDF builds a one-page document whose catalog /Metadata holds
// [invoiceXMPPacket], compressed when asked.
func metadataStreamPDF(t *testing.T, compress bool) []byte {
	t.Helper()

	xmp := []byte(invoiceXMPPacket)
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

func readInvoiceXMPFor(t *testing.T, compress bool) []byte {
	t.Helper()

	ctx := context.Background()
	data := metadataStreamPDF(t, compress)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return readInvoiceXMP(ctx, doc, catalog.Dict)
}

// TestReadInvoiceXMPDecodesCompressedMetadata pins that a filtered /Metadata
// stream is decoded before the invoice properties are looked for.
//
// ISO 32000-1, 14.3.2 permits a filter on a metadata stream, and
// [ast.PdfStream.Bytes] returns the body before filter decoding. Searching
// those compressed bytes finds none of the fx: properties, so every XMP clause
// of the Factur-X profile (tests 5 through 10) fired on a conformant invoice.
func TestReadInvoiceXMPDecodesCompressedMetadata(t *testing.T) {
	t.Parallel()

	got := readInvoiceXMPFor(t, true)
	if !bytes.Contains(got, []byte("<?xpacket")) {
		t.Fatalf("the packet was not decoded: %q", got[:min(len(got), 60)])
	}
	props := decodeHybridInvoiceXMP(got, variantFacturX)
	if !props.namespaceFound {
		t.Error("the fx: namespace was not found in the decoded packet")
	}
	if props.documentFileName != "factur-x.xml" {
		t.Errorf("documentFileName = %q, want factur-x.xml", props.documentFileName)
	}
	if props.conformanceLevel != "EN 16931" {
		t.Errorf("conformanceLevel = %q, want EN 16931", props.conformanceLevel)
	}
}

// TestReadInvoiceXMPReadsPlainMetadata is the control: the same packet stored
// without a filter, which pins that compression is the only difference.
func TestReadInvoiceXMPReadsPlainMetadata(t *testing.T) {
	t.Parallel()

	got := readInvoiceXMPFor(t, false)
	props := decodeHybridInvoiceXMP(got, variantFacturX)
	if !props.namespaceFound {
		t.Error("the fx: namespace was not found in the uncompressed packet")
	}
}
