package parser

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestParseObject_ReferencePastThirtyTwoBitsReadsAsNull pins that an indirect
// reference whose object or generation number is past 2^31 - 1 reads as null
// where int is 32 bits and where it is 64, and leaves the rest of its array
// whole: ISO 32000-1, Annex C, Table C.1 holds those numbers to 32 bits, and
// 7.3.10 reads a reference to an object that cannot exist as null.
func TestParseObject_ReferencePastThirtyTwoBitsReadsAsNull(t *testing.T) {
	t.Parallel()

	obj, err := newParser("[3000000000 0 R 4 70000000000 R 9223372036854775808 0 R 2147483647 0 R 5 0 R]").ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject: %v", err)
	}
	arr, ok := obj.(ast.PdfArray)
	if !ok || len(arr) != 5 {
		t.Fatalf("got %#v, want an array of 5", obj)
	}
	for i := range 3 {
		if _, ok := arr[i].(ast.PdfNull); !ok {
			t.Errorf("element %d = %#v, want null", i, arr[i])
		}
	}
	if ref, ok := arr[3].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 2147483647 || ref.GenNum != 0 {
		t.Errorf("element 3 = %#v, want 2147483647 0 R", arr[3])
	}
	if ref, ok := arr[4].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 5 {
		t.Errorf("element 4 = %#v, want 5 0 R", arr[4])
	}
}
