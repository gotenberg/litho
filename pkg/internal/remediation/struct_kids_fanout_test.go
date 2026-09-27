package remediation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestEmptyFigures_SharedKArrayFanOut pins that the empty-figure walk reads a
// /K array written as an object of its own once. Here object 5 heads a chain
// of 40 such arrays, each holding two direct Div elements that name the next
// one as their /K, and the last holding a Figure: no array is reached through
// a cycle, so the depth cap never ends the walk, and read anew wherever it is
// reached the chain fans out 2^40. The Figure is still found.
func TestEmptyFigures_SharedKArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const chain = 40
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [] >> >>",
	}
	for i := range chain {
		next := 6 + i
		objs = append(objs, fmt.Sprintf("[<< /Type /StructElem /S /Div /K %d 0 R >> << /Type /StructElem /S /Div /K %d 0 R >>]", next, next))
	}
	figure := 5 + chain + 1
	objs = append(objs, fmt.Sprintf("[%d 0 R]", figure), "<< /Type /StructElem /S /Figure /P 4 0 R >>")
	doc, alloc := newTestDoc(t, buildStructPDF(objs...))
	f := newEmptyFigures(ctx, alloc, doc, "PDF/UA-1")
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if f == nil || !f.figures[figure] {
		t.Fatalf("the Figure lacking alternate text, object %d, was not found", figure)
	}
}

// TestCollectPageRefs_SharedKidsArrayFanOut pins that the remediation page tree
// walk reads a /Kids array written as an object of its own once, as the reader
// does, and still finds the page it holds.
func TestCollectPageRefs_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids 4 0 R /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[3 0 R << /Type /Pages /Kids 4 0 R /Count 1 >> << /Type /Pages /Kids 4 0 R /Count 1 >>]",
	))
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 2}, &pages, make(map[int]bool), 0)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if len(pages) != 1 || pages[0].ref.ObjectNum != 3 {
		t.Errorf("collected %d pages, want the one page, object 3", len(pages))
	}
}
