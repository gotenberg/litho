package document

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// cyclicPrevPDF returns a PDF whose xref trailer /Prev points back at the xref
// table's own offset (109), forming a self-referential chain. The trailer sits
// after the xref, so adding /Prev leaves the offset unchanged. Reproduces pdfjs
// bug1393476, which hung the reader before the /Prev cycle guard.
func cyclicPrevPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"xref\n0 3\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n" +
			"trailer\n<< /Size 3 /Root 1 0 R /Prev 109 >>\nstartxref\n109\n%%EOF\n")
}

// TestNewDocument_CyclicPrevTerminates pins the /Prev cycle guard in
// parseXRefChain: a self-referential (or mutually cyclic) /Prev must not loop
// forever. The document still opens, keeping the entries parsed before the
// cycle is detected. Without the guard this hangs; the timeout catches a
// regression instead of hanging the whole suite.
func TestNewDocument_CyclicPrevTerminates(t *testing.T) {
	data := cyclicPrevPDF()

	done := make(chan error, 1)
	go func() {
		_, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("NewDocument on cyclic /Prev returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("NewDocument hung on a cyclic /Prev chain: cycle guard missing")
	}
}
