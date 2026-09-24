package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// sparseObjectNumberPDF builds a small document whose objects carry numbers far
// above the file's byte length. The trailer /Size is deliberately wrong, so the
// object numbers themselves are the only truth about how high the file reaches.
//
// # Corpus evidence
//
//   - pdfjs issue4573.pdf: the shape this fixture reproduces, including the
//     wrong trailer /Size 9.
func sparseObjectNumberPDF() []byte {
	const (
		catalogNum = 8797
		pagesNum   = 8798
		pageNum    = 8799
	)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := map[int]int{}

	write := func(num int, body string) {
		offs[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	write(catalogNum, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	write(pagesNum, fmt.Sprintf("<< /Type /Pages /Count 1 /Kids [%d 0 R] >>", pageNum))
	write(pageNum, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 200 50] >>", pagesNum))

	start := buf.Len()
	buf.WriteString("xref\n0 1\n0000000000 65535 f \n")
	for _, num := range []int{catalogNum, pagesNum, pageNum} {
		fmt.Fprintf(&buf, "%d 1\n%010d 00000 n \n", num, offs[num])
	}
	// /Size 9 is wrong: the objects run to 8799. A reader that believes it
	// cannot reach the page tree.
	fmt.Fprintf(&buf, "trailer\n<< /Size 9 /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", catalogNum, start)
	return buf.Bytes()
}

// TestHighestObjectNumberCountsSparseNumbers pins that an object numbered above
// the file's byte length still counts.
//
// An object number is a sparse identifier, not an index (ISO 32000-1, 7.3.10),
// so a short file may number its objects high. Bounding the number by the byte
// length dropped them. An allocator seeded from that hands out numbers that
// collide with objects already in the bytes, and the incremental trailer went
// out with a /Size below the page tree, which poppler then could not resolve.
//
// # Corpus evidence
//
//   - pdfjs issue4573.pdf: 8474 bytes reaching object 8801, for which litho
//     answered 380.
func TestHighestObjectNumberCountsSparseNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := sparseObjectNumberPDF()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := doc.FileSize(); got >= 8799 {
		t.Fatalf("fixture is %d bytes, too large to exercise the bound", got)
	}
	if got := doc.HighestObjectNumber(); got < 8799 {
		t.Errorf("HighestObjectNumber = %d, want at least 8799: an allocator seeded"+
			" from a lower number overwrites objects the file already defines", got)
	}
}

// TestHighestObjectNumberStillRejectsAbsurdNumbers pins that the anti-DoS cap
// survives: a corrupt xref must not send allocation somewhere unreachable.
func TestHighestObjectNumberStillRejectsAbsurdNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := sparseObjectNumberPDF()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.objectNumberFits(maxTrailerObjectCount + 1) {
		t.Error("an object number past the anti-DoS cap was accepted")
	}
	if doc.objectNumberFits(-1) {
		t.Error("a negative object number was accepted")
	}
}
