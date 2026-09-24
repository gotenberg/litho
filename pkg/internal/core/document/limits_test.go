package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// limitsDocBytes assembles a document of objs, numbered from 1, the first
// being the catalog.
func limitsDocBytes(objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestNewDocument_ReadsItsLimits pins that the budgets [WithLimits] sets reach
// the object cache and the document's stream decodes, and that a Document
// opened without it, or never opened, reads under the defaults.
func TestNewDocument_ReadsItsLimits(t *testing.T) {
	t.Parallel()

	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(make([]byte, 4096)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", z.Len(), z.Bytes()),
	}
	for range 6 {
		objs = append(objs, "<< /Filler true >>")
	}
	data := limitsDocBytes(objs...)
	ctx := context.Background()

	var zero *Document
	if got := zero.Limits(); got != limits.Default() {
		t.Errorf("a nil Document reads under %+v, want the defaults", got)
	}
	def, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := def.Limits(); got != limits.Default() {
		t.Errorf("a Document opened without WithLimits reads under %+v, want the defaults", got)
	}

	lim := limits.Limits{MaxStreamBytes: 1024, MaxCachedObjects: 2}
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, WithLimits(lim))
	if err != nil {
		t.Fatalf("NewDocument with limits: %v", err)
	}
	if got := doc.Limits(); got != lim.WithDefaults() {
		t.Errorf("Limits() = %+v, want %+v", got, lim.WithDefaults())
	}
	for n := 1; n <= len(objs); n++ {
		if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n}); err != nil {
			t.Fatalf("Resolve %d: %v", n, err)
		}
		doc.mu.RLock()
		cached := len(doc.objectCache)
		doc.mu.RUnlock()
		if cached > lim.MaxCachedObjects {
			t.Fatalf("after object %d the cache holds %d objects, past a budget of %d", n, cached, lim.MaxCachedObjects)
		}
	}

	stream, err := ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("ResolveStream: %v", err)
	}
	if out, err := def.DecodeStream(ctx, stream); err != nil || len(out) != 4096 {
		t.Errorf("default budget: %d bytes, %v; want 4096 bytes", len(out), err)
	}
	if _, err := doc.DecodeStream(ctx, stream); !errors.Is(err, filter.ErrDecompressionBomb) {
		t.Errorf("1 KiB budget: DecodeStream err = %v, want ErrDecompressionBomb", err)
	}
	if _, err := doc.NewStreamReader(ctx, stream); err != nil {
		t.Errorf("1 KiB budget: NewStreamReader of a single FlateDecode filter = %v, want the unbounded streaming read", err)
	}
}
