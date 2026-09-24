package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// unreadableObjStmPDF returns a document whose catalog lives in an object
// stream that will not inflate: the stream declares /FlateDecode over bytes
// that are not a Flate stream. The file opens, so the CLI gets as far as
// creating the output, and the mutation then fails while writing.
func unreadableObjStmPDF() []byte {
	return []byte(
		"%PDF-1.5\n" +
			"1 0 obj\n<< /Type /ObjStm /N 1 /First 8 /Length 9 /Filter /FlateDecode >>\n" +
			"stream\nnotflate!\nendstream\nendobj\n" +
			"2 0 obj\n<< /Type /XRef /Size 3 /W [1 2 1] /Root 3 0 R /Length 12 >>\n" +
			"stream\n\x01\x00\x09\x00\x02\x00\x00\x00\x01\x00\x00\x00\nendstream\nendobj\n" +
			"trailer\n<< /Size 3 /Root 3 0 R >>\n" +
			"startxref\n9\n%%EOF\n")
}

// TestFailedMutationLeavesNoOutputFile pins that a mutation that fails while
// writing does not leave its half-written output behind.
//
// createOutput truncates the destination the moment it opens it, so a failure
// partway through left a short or empty file exactly where the caller asked for
// the result. It is not a PDF and it looks like one, and the failure showed
// only through the exit code.
//
// # Corpus evidence
//
//   - pdfjs issue19484_1.pdf: its object stream will not inflate, and
//     `litho flatten-form -o out.pdf` on it left a zero-byte out.pdf behind
//     and reported failure only through the exit code.
func TestFailedMutationLeavesNoOutputFile(t *testing.T) {
	t.Parallel()

	in := writeTempPDF(t, unreadableObjStmPDF())
	out := filepath.Join(filepath.Dir(in), "out.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"flatten-form", in, "-o", out}, &stdout, &stderr)
	if rc == exitOK {
		t.Skip("the input no longer fails to write; the test needs a different fixture")
	}
	if _, err := os.Stat(out); err == nil {
		info, _ := os.Stat(out)
		t.Errorf("a failed mutation left %s behind (%d bytes)", out, info.Size())
	} else if !os.IsNotExist(err) {
		t.Errorf("stat %s: %v", out, err)
	}
}

// TestAbandonOutputRemovesOnlyRealFiles pins the helper's two cases: it
// withdraws a file it was given, and it does nothing for stdout, where there is
// no file to withdraw.
func TestAbandonOutputRemovesOnlyRealFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "partial.pdf")
	if err := os.WriteFile(path, []byte("half a pdf"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	closed := false
	abandonOutput(path, func() error { closed = true; return nil })
	if !closed {
		t.Error("the destination was not closed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the partial file survived: %v", err)
	}

	// Stdout, and a nil closer, must both be safe.
	for _, name := range []string{"", "-"} {
		abandonOutput(name, nil)
	}

	// Removing a path that is already gone is not an error either.
	abandonOutput(path, nil)
}
