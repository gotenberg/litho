package mutations

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// danglingPageTreePDF builds a document whose catalog names a page tree the
// file never defines. ISO 32000-1, 7.3.9 makes that reference equivalent to
// null, which is how a damaged file reaches the rewrite: pdfjs
// poppler-937-0-fuzzed.pdf loses its /Kids array to a NUL byte and ends up
// here.
func danglingPageTreePDF() []byte {
	body := "%PDF-1.4\n"
	offs := map[int]int{}
	add := func(num int, payload string) {
		offs[num] = len(body)
		body += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", num, payload)
	}
	add(1, "<< /Type /Catalog /Pages 2 0 R >>")

	xrefOffset := len(body)
	table := fmt.Sprintf("xref\n0 2\n0000000000 65535 f \n%010d 00000 n \n", offs[1])

	return []byte(body + table +
		fmt.Sprintf("trailer\n<< /Size 2 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset))
}

// TestFullRewriteNeverWritesNullPageTree checks the rewrite refuses to emit a
// catalog whose /Pages is null. Such a file is not a document: pikepdf rejects
// it with "unable to find page tree", and litho used to produce one silently
// while reporting success.
func TestFullRewriteNeverWritesNullPageTree(t *testing.T) {
	ctx := context.Background()
	data := danglingPageTreePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	var out bytes.Buffer
	if err := FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	written := out.Bytes()

	ref := regexp.MustCompile(`/Pages (\d+) 0 R`).FindSubmatch(written)
	if ref == nil {
		t.Fatalf("output catalog has no /Pages entry:\n%s", written)
	}

	target := regexp.MustCompile(`(?s)(?m)^` + string(ref[1]) + ` 0 obj\s*(.*?)\s*endobj`).FindSubmatch(written)
	if target == nil {
		t.Fatalf("output names object %s as its page tree but never defines it:\n%s", ref[1], written)
	}
	if bytes.HasPrefix(bytes.TrimSpace(target[1]), []byte("null")) {
		t.Errorf("catalog /Pages points at object %s, written as null:\n%s", ref[1], written)
	}
	if !bytes.Contains(target[1], []byte("/Type /Pages")) && !bytes.Contains(target[1], []byte("/Type/Pages")) {
		t.Errorf("page tree root is not a /Pages dictionary: %s", target[1])
	}
}

// TestFullRewriteKeepsRealPageTree checks the guard stays out of the way when
// the source page tree is intact, so an ordinary rewrite is untouched.
func TestFullRewriteKeepsRealPageTree(t *testing.T) {
	ctx := context.Background()
	data := minimalPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	var out bytes.Buffer
	if err := FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("/Count 0")) {
		t.Errorf("rewrite replaced a healthy page tree with an empty one:\n%s", out.Bytes())
	}
	if !bytes.Contains(out.Bytes(), []byte("/Count 1")) {
		t.Errorf("rewrite lost the source page count:\n%s", out.Bytes())
	}
}
