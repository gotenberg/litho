package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// unlistedCatalogPDF assembles a document whose trailer names no /Root and
// whose catalog, object 1, sits in the body but not in the cross-reference
// table, which lists the page tree (2) and its page (3) alone. Resolving the
// listed objects never misses the index, so nothing runs the raw-byte
// augmentation that would add the catalog.
func unlistedCatalogPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Lang (en-GB) >>\nendobj\n")
	offsets := [4]int{}
	offsets[2] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	offsets[3] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n")
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 1\n0000000000 65535 f \r\n2 2\n%010d 00000 n \r\n%010d 00000 n \r\n", offsets[2], offsets[3])
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 >>\nstartxref\n%d\n%%%%EOF\n", start)
	return buf.Bytes()
}

// TestCatalogRecoveryIgnoresEarlierAugmentation pins that catalog recovery
// finds a catalog the file's table leaves out whether or not something ran the
// raw-byte augmentation before it.
//
// The scan ranged the index as it stood, so it found object 1 only after an
// earlier lookup had missed: a fresh document synthesised a catalog over the
// page tree and lost /Lang, and one whose index had been augmented recovered
// the file's own. PDF.js recovers the catalog from an index of every object
// the bytes define, which is what the scan now reads.
func TestCatalogRecoveryIgnoresEarlierAugmentation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := unlistedCatalogPDF()

	for _, augmentFirst := range []bool{false, true} {
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		for objNum := range doc.OpenedXRefEntries() {
			if objNum == 1 {
				t.Fatal("fixture: the table lists the catalog")
			}
		}
		if augmentFirst {
			doc.AugmentXRef(ctx)
		}
		catalog, err := ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("augmentFirst=%v: ResolveCatalog: %v", augmentFirst, err)
		}
		if lang := catalog.Dict["Lang"]; lang != ast.PdfString("en-GB") {
			t.Errorf("augmentFirst=%v: recovered catalog has /Lang %v, want the file's (en-GB)", augmentFirst, lang)
		}
		if ref, ok := doc.CatalogRef(ctx); !ok || ref.ObjectNum != 1 {
			t.Errorf("augmentFirst=%v: CatalogRef = %v, %v, want object 1", augmentFirst, ref, ok)
		}
	}
}
