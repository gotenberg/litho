package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// inflatedSizePDF builds a small document whose trailer declares far more
// objects than its byte count could define, and whose catalog points at a page
// tree numbered in that upper range.
//
// # Corpus evidence
//
//   - pdfjs issue16091.pdf: the shape this fixture reproduces.
func inflatedSizePDF(declaredSize int) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 90000 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", declaredSize, start)
	return b.Bytes()
}

// TestHighestObjectNumberHonoursAnInflatedTrailerSize pins that the next object
// number litho writes clears the trailer's /Size even when the file is far too
// small to have defined that many objects.
//
// /Size is one greater than the highest object number USED in the file (ISO
// 32000-1, Table 15), which includes objects an incremental update references
// but this index never resolved. Rejecting it as implausible, the way the index
// rightly rejects such entries, dropped the estimate back to the index and the
// next allocation landed on top of a live object.
//
// # Corpus evidence
//
//   - pdfjs issue16091.pdf: the catalog names its page tree 156325 while the
//     trailer declares /Size 156342 in a 107 KB file, and `litho attach` wrote
//     the update's own cross-reference stream at 156325. poppler read the input
//     fine and then refused the output with "Top-level pages object is wrong
//     type (stream)".
func TestHighestObjectNumberHonoursAnInflatedTrailerSize(t *testing.T) {
	t.Parallel()

	const declared = 90001
	data := inflatedSizePDF(declared)
	if int64(len(data)) >= declared {
		t.Fatalf("fixture is wrong: the file (%d bytes) must be smaller than /Size %d", len(data), declared)
	}

	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := doc.HighestObjectNumber(context.Background()); got < declared-1 {
		t.Errorf("HighestObjectNumber = %d, want at least %d: a new object would be written on top of one the file references",
			got, declared-1)
	}
}

// TestHighestObjectNumberRejectsAnAbsurdTrailerSize pins the anti-DoS bound. A
// corrupt /Size must not send object numbers somewhere unusable; past the cap
// the index alone decides.
func TestHighestObjectNumberRejectsAnAbsurdTrailerSize(t *testing.T) {
	t.Parallel()

	data := inflatedSizePDF(maxTrailerObjectCount + 1)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := doc.HighestObjectNumber(context.Background()); got > maxTrailerObjectCount {
		t.Errorf("HighestObjectNumber = %d, want no more than the %d cap", got, maxTrailerObjectCount)
	}
}
