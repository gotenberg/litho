package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// untaggedTextPDF builds a single untagged page with a heading and a body line,
// so autotag has real text to synthesize a structure tree from.
func untaggedTextPDF() []byte {
	cs := "BT /F1 24 Tf 72 700 Td (Document Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 678 Td (A body line of text for the paragraph.) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \r\n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)
	return buf.Bytes()
}

// TestAutotag_TagsUntaggedPDF runs the autotag command on an untagged text page
// and confirms it produces a tagged output: a non-empty PDF carrying the
// synthesized /StructTreeRoot and the /MarkInfo marker.
func TestAutotag_TagsUntaggedPDF(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "tagged.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"autotag", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected a non-empty tagged output")
	}
	got := string(data)
	if !bytes.Contains(data, []byte("/StructTreeRoot")) {
		t.Errorf("output is missing /StructTreeRoot; autotag did not synthesize a tree:\n%s", got)
	}
	if !bytes.Contains(data, []byte("/MarkInfo")) {
		t.Errorf("output is missing /MarkInfo; document not marked as tagged")
	}
}

// TestAutotag_MissingOutput reports a usage error when -o is absent.
func TestAutotag_MissingOutput(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"autotag", path}, &stdout, &stderr)
	if rc != exitError {
		t.Errorf("exit code: got %d, want %d", rc, exitError)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Usage: litho autotag")) {
		t.Errorf("expected autotag usage on stderr, got: %q", stderr.String())
	}
}
