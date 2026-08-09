package writer

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestAllocAt_RefusesObjectZero pins that nothing is ever registered at object
// number 0.
//
// Entry zero of a cross-reference table is always the head of the free list and
// carries generation 65535 (ISO 32000-1, 7.5.4), so an object written there is
// unreachable. A page inlined in a /Kids array resolves with object number 0,
// and rotate, stamp-text and autotag each wrote it back at that number: the
// output carried a "0 0 obj" body and an in-use xref entry for object 0, the
// edit was invisible to every reader, and the xref was invalid.
func TestAllocAt_RefusesObjectZero(t *testing.T) {
	alloc := NewObjAllocator(10)

	if alloc.AllocAt(0, ast.PdfDict{"Type": ast.PdfName("Page")}) {
		t.Error("AllocAt(0) reported success; object 0 is the free-list head")
	}
	if _, present := alloc.Pending()[0]; present {
		t.Error("object 0 was registered, so the writer will emit \"0 0 obj\"")
	}
	if alloc.AllocAt(-1, ast.PdfDict{}) {
		t.Error("AllocAt(-1) reported success")
	}
	if _, present := alloc.Pending()[-1]; present {
		t.Error("a negative object number was registered")
	}

	// A real object number still works, and still advances /Size.
	if !alloc.AllocAt(12, ast.PdfDict{"Type": ast.PdfName("Page")}) {
		t.Fatal("AllocAt(12) refused a usable object number")
	}
	if _, present := alloc.Pending()[12]; !present {
		t.Error("object 12 was not registered")
	}
	if got := alloc.NextObjNum(); got != 13 {
		t.Errorf("NextObjNum() = %d, want 13", got)
	}
}
