package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
)

// embeddedFilePDF builds a one-page document carrying one embedded file, so a
// lazy accessor can be held past the reader's lifetime.
func embeddedFilePDF() []byte {
	payload := "HELLO-EMBEDDED-PAYLOAD"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(probe.txt) 5 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Type /EmbeddedFile /Subtype /text#2Fplain /Length %d >>\nstream\n%s\nendstream",
			len(payload), payload),
		"<< /Type /Filespec /F (probe.txt) /UF (probe.txt) /EF << /F 4 0 R >> >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestLazyAccessorAfterCloseErrorsCleanly pins that using an EmbeddedFile.Open
// accessor after the reader is closed fails with an error rather than panicking
// or handing back wrong bytes.
//
// Resource streaming means these accessors read from the source on demand
// ([OpenFile] hands the *os.File straight to [Open]), so a caller who keeps one
// past [Reader.Close] is reading through a closed descriptor. That is a
// use-after-free shaped hazard, and the panic boundary every public entry point
// installs is what has to hold.
//
// The test must NOT read through the accessor before closing: doing so caches
// the decoded stream and the later read is served from memory, which hides what
// Close does. That confound made an earlier version of this probe report
// "works fine after Close".
func TestLazyAccessorAfterCloseErrorsCleanly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/embedded.pdf"
	if err := os.WriteFile(path, embeddedFilePDF(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	r, err := OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	files, err := r.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("EmbeddedFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("fixture produced no embedded files")
	}
	open := files[0].Open

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	defer func() {
		if p := recover(); p != nil {
			t.Errorf("accessor panicked after Close: %v", p)
		}
	}()

	rc, err := open(ctx)
	if err != nil {
		return // A clean error is the contract.
	}
	// If it does hand back a reader, it must not hand back wrong bytes.
	b, readErr := io.ReadAll(rc)
	if closeErr := rc.Close(); closeErr != nil {
		t.Logf("closing the stream: %v", closeErr)
	}
	if readErr == nil && !bytes.Contains(b, []byte("HELLO-EMBEDDED-PAYLOAD")) {
		t.Errorf("accessor returned %d bytes of non-payload data after Close: %q", len(b), b)
	}
}
