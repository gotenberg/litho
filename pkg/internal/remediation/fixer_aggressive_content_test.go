package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestReplaceContentArrayStreams verifies that a direct /Contents array is
// rewritten by writing the combined content to its first stream element and
// emptying the rest, so the concatenation is unchanged and the page dictionary
// is untouched.
func TestReplaceContentArrayStreams(t *testing.T) {
	t.Parallel()
	alloc := writer.NewObjAllocator(10)
	arr := ast.PdfArray{
		ast.PdfIndirectRef{ObjectNum: 3},
		ast.PdfIndirectRef{ObjectNum: 4},
		ast.PdfIndirectRef{ObjectNum: 5},
	}
	newData := []byte("BT /F1 12 Tf (hi) Tj ET")
	if err := replaceContentArrayStreams(alloc, arr, newData, 1); err != nil {
		t.Fatalf("replaceContentArrayStreams: %v", err)
	}

	pending := alloc.Pending()
	s3, ok := pending[3].(ast.PdfStream)
	if !ok {
		t.Fatalf("obj 3 not queued as a stream: %T", pending[3])
	}
	if b, _ := s3.Bytes(context.Background()); string(b) != string(newData) {
		t.Errorf("obj 3 body = %q, want %q", b, newData)
	}
	for _, n := range []int{4, 5} {
		s, ok := pending[n].(ast.PdfStream)
		if !ok {
			t.Fatalf("obj %d not queued as a stream: %T", n, pending[n])
		}
		if b, _ := s.Bytes(context.Background()); len(b) != 0 {
			t.Errorf("obj %d body = %q, want empty", n, b)
		}
	}
}

// TestReplaceContentArrayStreams_NoStreamRefs verifies that an array carrying no
// indirect stream references is reported as an error rather than silently
// dropping the content.
func TestReplaceContentArrayStreams_NoStreamRefs(t *testing.T) {
	t.Parallel()
	alloc := writer.NewObjAllocator(10)
	arr := ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2)}
	if err := replaceContentArrayStreams(alloc, arr, []byte("x"), 1); err == nil {
		t.Error("expected an error for a /Contents array with no stream references")
	}
}
