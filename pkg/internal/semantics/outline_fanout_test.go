package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildCyclePDF assembles a minimal PDF from object bodies (1-indexed).
func buildCyclePDF(objs ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestResolveOutlineNextCycle pins that a bookmark outline whose /Next entries
// form a cycle cannot loop forever. The sibling walk had no visited-set, so
// [ResolveOutline] spun until the caller's context deadline (a denial of
// service through the public Bookmarks API).
func TestResolveOutlineNextCycle(t *testing.T) {
	t.Parallel()
	data := buildCyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Outlines 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Outlines /First 6 0 R /Last 7 0 R /Count 2 >>",
		"<< /Title (A) /Parent 5 0 R /Next 7 0 R >>",
		"<< /Title (B) /Parent 5 0 R /Next 6 0 R >>",
	)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		bm, err := ResolveOutline(context.Background(), doc)
		ch <- result{len(bm), err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("ResolveOutline errored on a cyclic outline: %v", r.err)
		}
		if r.n != 2 {
			t.Errorf("got %d bookmarks, want 2 (each cycle node once)", r.n)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ResolveOutline did not complete: /Next cycle not bounded")
	}
}
