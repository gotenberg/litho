package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestObjectsEqual_UncomparableComposites pins the panic fix: a Separation
// array's /AlternateSpace or tint-function slot can be an inline array, dict,
// or stream. objectsEqual must treat those as unequal instead of reaching a
// bare == that panics with "comparing uncomparable type".
//
// # Corpus evidence
//
//   - pdfjs issue6127 and issue21089: the test reproduces both at PDF/A-2b
//     aggressive remediation.
func TestObjectsEqual_UncomparableComposites(t *testing.T) {
	dict := ast.PdfDict{"Type": ast.PdfName("ICCBased")}
	arr := ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 5}}
	stream := ast.PdfStream{Dict: ast.PdfDict{}}

	// Each pair shares an uncomparable dynamic type, so the pre-fix bare ==
	// panicked. The canonicaliser conservatively declines to merge: unequal.
	composites := []struct {
		name string
		a, b ast.PdfObject
	}{
		{"dict/dict", dict, ast.PdfDict{"Type": ast.PdfName("ICCBased")}},
		{"array/array", arr, ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 5}}},
		{"stream/stream", stream, ast.PdfStream{Dict: ast.PdfDict{}}},
	}
	for _, tc := range composites {
		if objectsEqual(tc.a, tc.b) {
			t.Errorf("%s: objectsEqual = true, want false (composites decline to merge)", tc.name)
		}
	}

	// Comparable operands still compare by value.
	if !objectsEqual(ast.PdfName("Sep"), ast.PdfName("Sep")) {
		t.Error("equal names: objectsEqual = false, want true")
	}
	if objectsEqual(ast.PdfName("A"), ast.PdfName("B")) {
		t.Error("distinct names: objectsEqual = true, want false")
	}
	if !objectsEqual(ast.PdfIndirectRef{ObjectNum: 7}, ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Error("equal refs: objectsEqual = false, want true")
	}
	// A composite paired with a scalar must also not panic.
	if objectsEqual(dict, ast.PdfName("ICCBased")) {
		t.Error("dict/name: objectsEqual = true, want false")
	}
}
