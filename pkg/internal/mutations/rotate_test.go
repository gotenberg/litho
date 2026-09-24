package mutations

import (
	"context"
	"errors"
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

// TestRotatePages_EmptyPageListRotatesEveryPage pins the "default: all"
// contract the rotate command advertises for its -pages flag.
//
// The CLI leaves its page list empty when -pages is absent, so an empty list
// meaning "no pages" made rotating a whole document a silent no-op: litho
// reported success over a byte-identical file. StampText has always expanded an
// empty list this way; rotate was the outlier.
func TestRotatePages_EmptyPageListRotatesEveryPage(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	pages := newTestPages(3, 4, 5)

	if err := RotatePages(context.Background(), alloc, pages, 90, nil); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	pending := alloc.Pending()
	if len(pending) != len(pages) {
		t.Fatalf("rotated %d page(s), want all %d", len(pending), len(pages))
	}
	for _, objNum := range []int{3, 4, 5} {
		dict, ok := pending[objNum].(ast.PdfDict)
		if !ok {
			t.Errorf("page %d was not rotated", objNum)
			continue
		}
		if got, want := dict["Rotate"], ast.PdfInt(90); got != want {
			t.Errorf("page %d /Rotate = %v, want %v", objNum, got, want)
		}
	}
}

// TestRotatePages_ZeroClearsRotation pins that 0 is a real rotation, not a
// no-op: it resets a page a previous run turned.
func TestRotatePages_ZeroClearsRotation(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	pages := newTestPages(3)
	pages[0].Dict["Rotate"] = ast.PdfInt(90)

	if err := RotatePages(context.Background(), alloc, pages, 0, nil); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	dict, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("page 3 was not written")
	}
	if got, want := dict["Rotate"], ast.PdfInt(0); got != want {
		t.Errorf("/Rotate = %v, want %v", got, want)
	}
}

// TestRotatePages_NormalisesAngle pins that /Rotate is written as one of the
// four values it is documented to take.
//
// ISO 32000-1, Table 30 only asks for a multiple of 90, so 720 and -90 are
// legal and veraPDF accepts them, but emitting them contradicts what
// RotatePages promises and leaves every reader to fold a full turn or a
// negative angle on litho's behalf. Unreachable until an empty page list
// started rotating anything.
func TestRotatePages_NormalisesAngle(t *testing.T) {
	cases := []struct {
		angle int
		want  ast.PdfInt
	}{
		{0, 0},
		{90, 90},
		{180, 180},
		{270, 270},
		{360, 0},
		{450, 90},
		{720, 0},
		{-90, 270},
		{-180, 180},
		{-270, 90},
		{-360, 0},
	}
	for _, c := range cases {
		alloc := writer.NewObjAllocator(10)
		pages := newTestPages(3)
		if err := RotatePages(context.Background(), alloc, pages, c.angle, nil); err != nil {
			t.Fatalf("RotatePages(%d): %v", c.angle, err)
		}
		dict, ok := alloc.Pending()[3].(ast.PdfDict)
		if !ok {
			t.Errorf("angle %d: page was not written", c.angle)
			continue
		}
		if got := dict["Rotate"]; got != c.want {
			t.Errorf("angle %d: /Rotate = %v, want %v", c.angle, got, c.want)
		}
	}
}

// TestRotatePages_InlinePageIsReported pins that a page with no indirect
// reference produces an error rather than a rotation that never lands.
//
// Such a page is inlined in its /Kids array (ISO 32000-1, 7.7.3.2) and has no
// object number for an incremental update to replace. Writing it back at object
// 0 emitted a body every reader ignores, so litho reported success over a file
// whose /Rotate was nowhere.
//
// # Corpus evidence
//
//   - pdfjs issue9540.pdf: inlines a page in its /Kids array, and rotating it
//     reported success over a file whose /Rotate was nowhere.
func TestRotatePages_InlinePageIsReported(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	pages := []*document.Page{{Dict: ast.PdfDict{"Type": ast.PdfName("Page")}}}

	err := RotatePages(context.Background(), alloc, pages, 90, nil)
	if !errors.Is(err, ErrPageNotAddressable) {
		t.Fatalf("RotatePages on an inline page = %v, want ErrPageNotAddressable", err)
	}
	if _, present := alloc.Pending()[0]; present {
		t.Error("the page was registered at object 0")
	}
}
