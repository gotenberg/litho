package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"
)

// TestXMPLoadMetadata_DecodesFilteredStream guards that the catalog /Metadata
// stream is decoded (its filters applied) before the XMP checks run. A
// Flate-encoded metadata packet is legal in PDF/A parts 2+, and veraPDF
// validates the decoded XML; reading the raw compressed bytes made a valid
// packet look like malformed XML, a spurious §6.6.2.1 t4/t5 (seen on qpdf's
// lin8/lin6/compressed-metadata fixtures).
func TestXMPLoadMetadata_DecodesFilteredStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	xmp := "<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>\n" +
		"<x:xmpmeta xmlns:x='adobe:ns:meta/'><rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'></rdf:RDF></x:xmpmeta>\n" +
		"<?xpacket end='w'?>"
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write([]byte(xmp)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	compressed := zbuf.Bytes()

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(compressed), string(compressed)),
	)

	var results []Violation
	data, _ := xmpLoadMetadata(ctx, doc, catalog, 2, &results)
	if !bytes.HasPrefix(data, []byte("<?xpacket")) {
		t.Fatalf("metadata was not decoded: got %q", string(data[:min(len(data), 24)]))
	}
	if !xmpWellFormed(data) {
		t.Error("decoded metadata should be well-formed XML")
	}
}
