package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// tableOfContentsPDF builds three pages whose first holds two links by name,
// the way a browser writes a table of contents: one to /a, which the catalog's
// /Dests dictionary maps to page 2, and one through a /GoTo action to (b),
// which the /Names /Dests tree maps to page 3 (ISO 32000-1, 12.3.2.3).
func tableOfContentsPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Dests 6 0 R /Names << /Dests << /Names [(b) [5 0 R /Fit]] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /Annots [" +
			"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Dest /a >> " +
			"<< /Type /Annot /Subtype /Link /Rect [0 20 10 30] /A << /S /GoTo /D (b) >> >>] >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /a [4 0 R /Fit] >>",
	})
}

// linkPages returns the 1-based page each link of the output's page pageNum
// opens, 0 for one that opens none.
func linkPages(t *testing.T, data []byte, pageNum int) []int {
	t.Helper()
	r := openDeclaration(t, data, false)
	defer r.Close()
	annots, err := r.ExtractAnnotations(context.Background(), pageNum)
	if err != nil {
		t.Fatalf("ExtractAnnotations(%d): %v", pageNum, err)
	}
	var pages []int
	for _, a := range annots {
		if a.Subtype == "Link" {
			pages = append(pages, a.DestPage)
		}
	}
	return pages
}

// namedDestPage returns the 1-based page the output's named destination name
// opens, 0 when it names none.
func namedDestPage(t *testing.T, data []byte, name string) int {
	t.Helper()
	ctx := context.Background()
	tree := readOutputTree(t, data)
	arr, err := document.ResolveNamedDest(ctx, tree.doc, name)
	if err != nil {
		t.Fatalf("ResolveNamedDest(%q): %v", name, err)
	}
	if len(arr) == 0 {
		return 0
	}
	ref, _ := arr[0].(ast.PdfIndirectRef)
	return document.BuildPageMap(tree.pages)[ref]
}

// assertLinks fails unless the links of page pageNum open want, in order.
func assertLinks(t *testing.T, data []byte, pageNum int, want ...int) {
	t.Helper()
	got := linkPages(t, data, pageNum)
	if len(got) != len(want) {
		t.Fatalf("page %d links open %v, want %v", pageNum, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("page %d links open %v, want %v", pageNum, got, want)
		}
	}
}

// TestMerge_KeepsNamedDestinations pins that a merge keeps the links its
// sources give by name working, and the names themselves. Merge built a fresh
// catalog without /Dests or /Names /Dests, so every such link named nothing.
// A source merged with itself names each destination twice: the names keep the
// first source's entries, and the second source's links still open its own
// pages, which they name directly.
func TestMerge_KeepsNamedDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := tableOfContentsPDF()
	a, b := openDeclaration(t, src, false), openDeclaration(t, src, false)
	defer a.Close()
	defer b.Close()
	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{a, b}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	assertLinks(t, out.Bytes(), 1, 2, 3)
	assertLinks(t, out.Bytes(), 4, 5, 6)
	if got := namedDestPage(t, out.Bytes(), "a"); got != 2 {
		t.Errorf("/a opens page %d, want 2", got)
	}
	if got := namedDestPage(t, out.Bytes(), "b"); got != 3 {
		t.Errorf("(b) opens page %d, want 3", got)
	}
}

// TestSplit_KeepsTheNamedDestinationsOfItsPages pins that a split keeps the
// named destinations of the pages it takes and drops the others, with the
// link to a page it leaves out, as it drops one naming that page directly.
func TestSplit_KeepsTheNamedDestinationsOfItsPages(t *testing.T) {
	t.Parallel()
	r := openDeclaration(t, tableOfContentsPDF(), false)
	defer r.Close()
	var out bytes.Buffer
	if err := Split(context.Background(), r, []int{1, 2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertLinks(t, out.Bytes(), 1, 2)
	if got := namedDestPage(t, out.Bytes(), "a"); got != 2 {
		t.Errorf("/a opens page %d, want 2", got)
	}
	if got := namedDestPage(t, out.Bytes(), "b"); got != 0 {
		t.Errorf("(b) opens page %d, want none: the split left page 3 out", got)
	}
}

// TestInsertPages_KeepsTheLinksOfNamedDestinations pins that inserted pages
// keep the links their source gives by name working: the target's catalog
// holds none of the source's names, so each link names its page directly.
func TestInsertPages_KeepsTheLinksOfNamedDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := openDeclaration(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R >>",
	}), false)
	defer target.Close()
	src := openDeclaration(t, tableOfContentsPDF(), false)
	defer src.Close()
	var out bytes.Buffer
	if err := writeEdited(ctx, target, &out, func(w *Writer) error { return w.InsertPages(1, src) }); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	assertLinks(t, out.Bytes(), 2, 3, 4)
}
