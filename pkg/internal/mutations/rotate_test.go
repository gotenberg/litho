package mutations

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func newTestPages(refs ...int) []*document.Page {
	pages := make([]*document.Page, len(refs))
	for i, n := range refs {
		pages[i] = &document.Page{
			Ref:  ast.PdfIndirectRef{ObjectNum: n},
			Dict: ast.PdfDict{"Type": ast.PdfName("Page")},
		}
	}
	return pages
}

func TestRotatePages_AppliesRotateField(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	pages := newTestPages(3, 4, 5)

	if err := RotatePages(context.Background(), alloc, pages, 90, []int{1, 3}); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	pending := alloc.Pending()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending mutations, got %d", len(pending))
	}

	for _, objNum := range []int{3, 5} {
		obj, ok := pending[objNum]
		if !ok {
			t.Errorf("page %d not in pending", objNum)
			continue
		}
		dict, ok := obj.(ast.PdfDict)
		if !ok {
			t.Errorf("page %d: got %T, want PdfDict", objNum, obj)
			continue
		}
		if rot, _ := dict["Rotate"].(ast.PdfInt); int(rot) != 90 {
			t.Errorf("page %d: Rotate = %v, want 90", objNum, dict["Rotate"])
		}
	}

	// Page 4 (object num 4) should not have been touched.
	if _, ok := pending[4]; ok {
		t.Error("page 2 (object 4) should not have been mutated")
	}
}

func TestRotatePages_OutOfBounds(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	pages := newTestPages(3, 4)

	tests := []int{0, -1, 3, 100}
	for _, p := range tests {
		err := RotatePages(context.Background(), alloc, pages, 90, []int{p})
		if err == nil {
			t.Errorf("page %d: expected out-of-bounds error", p)
			continue
		}
		if !strings.Contains(err.Error(), "out of bounds") {
			t.Errorf("page %d: expected 'out of bounds' error, got %q", p, err.Error())
		}
	}
}

func TestRotatePages_PreservesExistingFields(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	page := &document.Page{
		Ref: ast.PdfIndirectRef{ObjectNum: 7},
		Dict: ast.PdfDict{
			"Type":     ast.PdfName("Page"),
			"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		},
	}

	if err := RotatePages(context.Background(), alloc, []*document.Page{page}, 180, []int{1}); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	dict := alloc.Pending()[7].(ast.PdfDict)
	if dict["Type"] != ast.PdfName("Page") {
		t.Errorf("Type field lost: got %v", dict["Type"])
	}
	if _, ok := dict["MediaBox"]; !ok {
		t.Error("MediaBox field lost during rotate")
	}
	if int(dict["Rotate"].(ast.PdfInt)) != 180 {
		t.Errorf("Rotate: got %v, want 180", dict["Rotate"])
	}
}

func TestRotatePages_ContextCancel(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	pages := newTestPages(3, 4, 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RotatePages(ctx, alloc, pages, 90, []int{1})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestLatestPageDict_PrefersAllocator(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	page := &document.Page{
		Ref:  ast.PdfIndirectRef{ObjectNum: 5},
		Dict: ast.PdfDict{"Type": ast.PdfName("Page"), "Original": ast.PdfBool(true)},
	}

	// No pending entry yet → falls back to page.Dict.
	got := latestPageDict(alloc, page)
	if got["Original"] != ast.PdfBool(true) {
		t.Error("expected fallback to page.Dict")
	}

	// Patch the page in the allocator with a fresh dict; latestPageDict
	// should now return the pending version.
	patched := ast.PdfDict{"Type": ast.PdfName("Page"), "Patched": ast.PdfBool(true)}
	alloc.AllocAt(5, patched)

	got = latestPageDict(alloc, page)
	if got["Patched"] != ast.PdfBool(true) {
		t.Error("expected allocator pending version to win")
	}
	if _, hasOriginal := got["Original"]; hasOriginal {
		t.Error("did not expect original-only field in pending dict")
	}
}
