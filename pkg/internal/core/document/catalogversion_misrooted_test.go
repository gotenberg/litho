package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// misrootedVersionPDF assembles a document whose header declares 1.4 while the
// catalog in object 2 declares /Version /2.0, with the trailer /Root naming
// rootObjNum. Object 1 is the /Info dictionary, so naming it reproduces a
// damaged incremental update.
func misrootedVersionPDF(rootObjNum int) []byte {
	objs := []string{
		"<< /Producer (Aspose) >>",
		"<< /Type /Catalog /Pages 3 0 R /Version /2.0 >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
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

// TestCatalogVersionFollowsTheRecoveredCatalog pins that the catalog /Version
// override is honoured on a document whose trailer /Root names a non-catalog.
//
// Per ISO 32000-1 §7.2.2 the catalog /Version takes precedence over the header,
// and per §7.5.5 Table 15 /Root shall be the catalog, so a file naming
// something else is malformed and [Document.CatalogRef] recovers from it.
// Reading the raw /Root meant such a file silently kept its header version:
// `litho info` reported 1.4 for a document declaring 2.0. The reported version
// drives profile decisions, so the error does not stay cosmetic.
func TestCatalogVersionFollowsTheRecoveredCatalog(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		rootObj int
	}{
		{"control: /Root names the catalog", 2},
		{"/Root names the /Info dictionary", 1},
	} {
		data := misrootedVersionPDF(tc.rootObj)
		doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		if doc.Version != "2.0" {
			t.Errorf("%s: Version = %q, want \"2.0\" from the catalog override", tc.name, doc.Version)
		}
	}
}
