package semantics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// misrootedXMPPDF assembles a one-page document whose catalog is object 2 and
// carries an XMP /Metadata stream, while the trailer /Root points at the /Info
// dictionary in object 1. rootObjNum selects which object /Root names, so the
// same bytes serve as fixture and control.
func misrootedXMPPDF(rootObjNum int) []byte {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
		`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">XmpOnlyTitle</rdf:li></rdf:Alt></dc:title>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

	objs := []string{
		"<< /Producer (Aspose) >>",
		"<< /Type /Catalog /Pages 3 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R /Info 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, rootObjNum, start)
	return buf.Bytes()
}

func xmpTitleFound(t *testing.T, rootObjNum int) bool {
	t.Helper()

	data := misrootedXMPPDF(rootObjNum)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	open, err := XMPOpen(context.Background(), doc)
	if err != nil {
		t.Fatalf("XMPOpen: %v", err)
	}
	if open == nil {
		return false
	}
	rc, err := open(context.Background())
	if err != nil {
		t.Fatalf("opening XMP stream: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading XMP stream: %v", err)
	}
	return strings.Contains(string(b), "XmpOnlyTitle")
}

// TestXMPOpenFollowsTheRecoveredCatalog pins that the XMP packet is found on a
// document whose trailer /Root names a non-catalog.
//
// Per ISO 32000-1, 7.5.5, Table 15 /Root shall be the catalog, so such a file is
// malformed and the reader recovers by scanning. XMPOpen read the raw /Root, so
// pdf.Reader.XMPMetadata reported no XMP at all on a document whose catalog
// carries a valid /Metadata stream. The sibling resolveXMPMetadata found that
// same packet through its object-pool fallback, so the two disagreed about
// whether the document had XMP.
func TestXMPOpenFollowsTheRecoveredCatalog(t *testing.T) {
	t.Parallel()

	if !xmpTitleFound(t, 2) {
		t.Fatal("control: XMP must be found when /Root names the catalog")
	}
	if !xmpTitleFound(t, 1) {
		t.Error("XMP not found when /Root names the /Info dictionary; the catalog still carries it")
	}
}
