package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// kTreeFixture builds [taggedFixture]'s page, MCID 0 drawing "SECRET" and
// MCID 1 drawing "KEEP", under a structure tree whose root /K reaches objects
// 10 and 11 as well as whatever parentTree says. elem10 and elem11 are the
// bodies of the two elements, so each test decides what their /K and /Pg
// assign them.
func kTreeFixture(t *testing.T, parentTree, elem10, elem11 string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	body := "/P <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (SECRET) Tj ET EMC " +
		"/P <</MCID 1>> BDC BT /F1 12 Tf 100 600 Td (KEEP) Tj ET EMC"

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 8 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R /StructParents 0 >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /StructTreeRoot /K 12 0 R /ParentTree " + parentTree + " >>\nendobj\n" +
		"10 0 obj\n" + elem10 + "\nendobj\n" +
		"11 0 obj\n" + elem11 + "\nendobj\n" +
		"12 0 obj\n<< /Type /StructElem /S /Document /P 8 0 R /K [10 0 R 11 0 R] >>\nendobj\n" +
		"trailer\n<< /Size 13 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

const (
	secretElem = "<< /Type /StructElem /S /P /P 12 0 R /Pg 3 0 R /K 0 /ActualText (SECRET) >>"
	keepElem   = "<< /Type /StructElem /S /P /P 12 0 R /Pg 3 0 R /K 1 /ActualText (KEEP) >>"
)

// clearedActualText reports whether the allocator holds object num with its
// /ActualText gone.
func clearedActualText(alloc *writer.ObjAllocator, num int) bool {
	elem, ok := alloc.Pending()[num].(ast.PdfDict)
	if !ok {
		return false
	}
	_, present := elem["ActualText"]
	return !present
}

// TestScrubStructText_FindsTheElementThroughTheTreeWhenTheParentTreeDoesNot
// pins the fallback for a /ParentTree that names no element for the removed
// content: an entry that dangles, one that is null, and a slot that is null.
// The element is still in the tree, reached from the root's /K, and a viewer
// reads its /ActualText there, so it is cleared through /K. Before, the scrub
// passed over the first two and refused the third as not addressable.
func TestScrubStructText_FindsTheElementThroughTheTreeWhenTheParentTreeDoesNot(t *testing.T) {
	t.Parallel()
	for name, parentTree := range map[string]string{
		"dangling entry": "<< /Nums [0 30 0 R] >>",
		"null slot":      "<< /Nums [0 [null 11 0 R]] >>",
		"no entry":       "<< /Nums [7 [10 0 R 11 0 R]] >>",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, page := kTreeFixture(t, parentTree, secretElem, keepElem)
			alloc := writer.NewObjAllocator(40)
			if _, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret); err != nil {
				t.Fatalf("RedactPageRegions: %v", err)
			}
			if !clearedActualText(alloc, 10) {
				t.Errorf("object 10 still holds /ActualText: %v", alloc.Pending()[10])
			}
			if _, queued := alloc.Pending()[11]; queued {
				t.Errorf("the surviving element was rewritten: %v", alloc.Pending()[11])
			}
		})
	}
}

// TestScrubStructText_TreeFallbackRefusesAnElementSharedWithSurvivors pins
// that an element the tree assigns both a removed and a surviving MCID is
// refused, as one the /ParentTree names twice is.
func TestScrubStructText_TreeFallbackRefusesAnElementSharedWithSurvivors(t *testing.T) {
	t.Parallel()
	both := "<< /Type /StructElem /S /P /P 12 0 R /Pg 3 0 R /K [0 1] /ActualText (SECRET KEEP) >>"
	doc, page := kTreeFixture(t, "<< /Nums [0 30 0 R] >>", both, "<< /Type /StructElem /S /Span >>")
	alloc := writer.NewObjAllocator(40)
	_, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Errorf("err = %v, want ErrStructElementPartlyRemoved", err)
	}
}

// TestScrubStructText_TreeFallbackReadsThePageOfEachItem pins the page test
// of the fallback: an element on another page keeps its text whatever MCID it
// names, an element that states no /Pg takes its parent's, and a
// marked-content reference states its own.
func TestScrubStructText_TreeFallbackReadsThePageOfEachItem(t *testing.T) {
	t.Parallel()
	const dangling = "<< /Nums [0 30 0 R] >>"

	otherPage := "<< /Type /StructElem /S /P /P 12 0 R /Pg 99 0 R /K 0 /ActualText (SECRET) >>"
	doc, page := kTreeFixture(t, dangling, otherPage, keepElem)
	alloc := writer.NewObjAllocator(40)
	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if _, queued := alloc.Pending()[10]; queued {
		t.Errorf("an element of another page was rewritten: %v", alloc.Pending()[10])
	}

	inherited := "<< /Type /StructElem /S /Div /P 12 0 R /Pg 3 0 R /K << /Type /StructElem /S /P /K 0 >> /ActualText (SECRET) >>"
	doc, page = kTreeFixture(t, dangling, inherited, keepElem)
	alloc = writer.NewObjAllocator(40)
	_, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if !errors.Is(err, ErrStructElementNotAddressable) {
		// The child takes the page of its parent, and being direct it cannot be
		// rewritten: reaching that refusal is what shows the page was read.
		t.Errorf("err = %v, want ErrStructElementNotAddressable for the direct child", err)
	}

	mcr := "<< /Type /StructElem /S /P /P 12 0 R /K << /Type /MCR /Pg 3 0 R /MCID 0 >> /ActualText (SECRET) >>"
	doc, page = kTreeFixture(t, dangling, mcr, keepElem)
	alloc = writer.NewObjAllocator(40)
	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !clearedActualText(alloc, 10) {
		t.Errorf("the element of a marked-content reference kept /ActualText: %v", alloc.Pending()[10])
	}
}
