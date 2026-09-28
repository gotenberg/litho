package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// nestedPageTreePDF builds a four-page document whose page tree has an
// intermediate /Pages node, and whose /Resources and /MediaBox live on that node
// for the pages to inherit (ISO 32000-1, 7.7.3.4). Each page carries text of its
// own so a leak is legible in the output bytes.
//
// withBeads threads an article through the document (ISO 32000-1, 12.4.3): page
// 1 carries a bead whose /N chains to a bead on page 3, which the split leaves
// out.
func nestedPageTreePDF(t *testing.T, withBeads bool) []byte {
	t.Helper()

	contents := []string{
		"BT /F1 12 Tf 20 700 Td (alpha page one) Tj ET\n",
		"BT /F1 12 Tf 20 700 Td (bravo page two) Tj ET\n",
		"BT /F1 12 Tf 20 700 Td (SECRET page three) Tj ET\n",
		"BT /F1 12 Tf 20 700 Td (SECRET page four) Tj ET\n",
	}

	firstPage := "<< /Type /Page /Parent 3 0 R /Contents 8 0 R >>"
	if withBeads {
		firstPage = "<< /Type /Page /Parent 3 0 R /Contents 8 0 R /B [13 0 R] >>"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 4 >>",
		// The intermediate node holds the inheritable attributes.
		"<< /Type /Pages /Parent 2 0 R /Kids [4 0 R 5 0 R 6 0 R 7 0 R] /Count 4" +
			" /MediaBox [0 0 612 792] /Resources << /Font << /F1 12 0 R >> >> >>",
		firstPage,
		"<< /Type /Page /Parent 3 0 R /Contents 9 0 R >>",
		"<< /Type /Page /Parent 3 0 R /Contents 10 0 R >>",
		"<< /Type /Page /Parent 3 0 R /Contents 11 0 R >>",
	}
	for _, c := range contents {
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
	}
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	if withBeads {
		objs = append(objs,
			"<< /P 4 0 R /N 14 0 R /V 14 0 R /R [0 0 100 100] >>",
			// The bead on page 3, the page the split leaves out.
			"<< /P 6 0 R /N 13 0 R /V 13 0 R /R [0 0 100 100] >>",
		)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// splitFirstTwoPages runs Split over the fixture and returns the output bytes.
func splitFirstTwoPages(t *testing.T, withBeads bool) []byte {
	t.Helper()

	ctx := context.Background()
	data := nestedPageTreePDF(t, withBeads)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) != 4 {
		t.Fatalf("fixture has %d pages, want 4", len(pages))
	}

	var out bytes.Buffer
	if err := Split(ctx, doc, pages, []int{1, 2}, &out, "", false, CompactOptions{}, BookmarkConfig{Handling: BookmarkNone}, nil, nil); err != nil {
		t.Fatalf("Split: %v", err)
	}
	return out.Bytes()
}

// TestSplitDoesNotCarryUnextractedPages pins that extracting a page subset
// leaves the rest of the document behind.
//
// A page's /Parent references its /Pages node, and the copier follows every
// reference, so copying one page used to copy the node, its /Kids, and through
// them every page of the source.
//
// # Corpus evidence
//
//   - govdocs thread_100_100852.pdf: extracting nine pages wrote all 56 into
//     the output: 47 pages the caller never asked for, 510 KB of content that
//     read back as plain text from a file that was supposed to hold pages 2
//     to 10.
func TestSplitDoesNotCarryUnextractedPages(t *testing.T) {
	t.Parallel()

	out := splitFirstTwoPages(t, false)

	if bytes.Contains(out, []byte("SECRET")) {
		t.Error("the output carries content from a page the split did not extract")
	}

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(out), int64(len(out)), nil)
	if err != nil {
		t.Fatalf("reading the split output: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog on the output: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages on the output: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("the output has %d pages, want 2", len(pages))
	}

	inTree := make(map[int]bool, len(pages))
	for _, page := range pages {
		inTree[page.Ref.ObjectNum] = true
	}
	for num := 1; num <= doc.HighestObjectNumber(ctx); num++ {
		obj, resolveErr := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
		if resolveErr != nil {
			continue
		}
		dict, isDict := obj.(ast.PdfDict)
		if !isDict {
			continue
		}
		if kind, _ := dict["Type"].(ast.PdfName); kind == "Page" && !inTree[num] {
			t.Errorf("object %d is a /Page outside the page tree: an unextracted page was written anyway", num)
		}
	}
}

// TestSplitKeepsInheritedAttributes is the control for cutting /Parent: the
// attributes a page inherited through it must be materialised onto the page.
//
// Without /Resources an inheriting page loses its fonts and its text stops
// being extractable, so dropping the link without materialising first would
// trade a leak for silent data loss.
func TestSplitKeepsInheritedAttributes(t *testing.T) {
	t.Parallel()

	out := splitFirstTwoPages(t, false)

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(out), int64(len(out)), nil)
	if err != nil {
		t.Fatalf("reading the split output: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog on the output: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages on the output: %v", err)
	}

	for i, page := range pages {
		if page.Resources == nil {
			t.Errorf("page %d lost the /Resources it inherited, so its font is gone", i+1)
		}
		if page.MediaBox == nil {
			t.Errorf("page %d lost the /MediaBox it inherited", i+1)
		}
	}
	if !bytes.Contains(out, []byte("alpha page one")) || !bytes.Contains(out, []byte("bravo page two")) {
		t.Error("the extracted pages lost their content")
	}
}

// TestSplitDoesNotFollowArticleBeads pins the second way a copied page reaches
// its neighbours.
//
// A bead chains to the next bead of its article through /N, /V and /T, and each
// bead names its page in /P (ISO 32000-1, 12.4.3), so one bead on a kept page
// walks the copier through the whole article.
//
// # Corpus evidence
//
//   - govdocs thread_100_100184.pdf: leaked 84 pages and 1.2 MB of content
//     through its article beads, from a two-page extraction, after the
//     /Parent path was already closed.
func TestSplitDoesNotFollowArticleBeads(t *testing.T) {
	t.Parallel()

	out := splitFirstTwoPages(t, true)

	if bytes.Contains(out, []byte("SECRET")) {
		t.Error("an article bead carried an unextracted page's content into the output")
	}

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(out), int64(len(out)), nil)
	if err != nil {
		t.Fatalf("reading the split output: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog on the output: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages on the output: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("the output has %d pages, want 2", len(pages))
	}
	if _, kept := pages[0].Dict["B"]; kept {
		t.Error("the copied page kept /B, whose beads reach pages the output does not have")
	}
}

// onePagePDF builds a minimal single-page document to insert into.
func onePagePDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (target) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestInsertPagesDoesNotCarryUnselectedSourcePages pins that inserting a subset
// of a source document brings only that subset.
//
// # Corpus evidence
//
//   - govdocs thread_100_100852.pdf: inserting one of its pages into a
//     four-page document produced a five-page file carrying all 55 unselected
//     source pages and 613 KB of their content, so the output was 2 MB where
//     88 KB was called for.
func TestInsertPagesDoesNotCarryUnselectedSourcePages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	targetData := onePagePDF(t)
	target, err := document.NewDocument(ctx, bytes.NewReader(targetData), int64(len(targetData)), nil)
	if err != nil {
		t.Fatalf("NewDocument (target): %v", err)
	}
	targetCatalog, err := document.ResolveCatalog(ctx, target)
	if err != nil {
		t.Fatalf("ResolveCatalog (target): %v", err)
	}
	targetPages, err := document.ResolvePages(ctx, target, targetCatalog)
	if err != nil {
		t.Fatalf("ResolvePages (target): %v", err)
	}

	srcData := nestedPageTreePDF(t, false)
	src, err := document.NewDocument(ctx, bytes.NewReader(srcData), int64(len(srcData)), nil)
	if err != nil {
		t.Fatalf("NewDocument (source): %v", err)
	}
	srcCatalog, err := document.ResolveCatalog(ctx, src)
	if err != nil {
		t.Fatalf("ResolveCatalog (source): %v", err)
	}
	srcPages, err := document.ResolvePages(ctx, src, srcCatalog)
	if err != nil {
		t.Fatalf("ResolvePages (source): %v", err)
	}

	alloc := writer.NewObjAllocator(target.HighestObjectNumber(ctx))
	if err := InsertPages(ctx, alloc, target, targetPages, 0, src, srcPages, []int{1}); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}

	pageObjects := 0
	for _, obj := range alloc.Pending() {
		if dict, isDict := obj.(ast.PdfDict); isDict {
			if kind, _ := dict["Type"].(ast.PdfName); kind == "Page" {
				pageObjects++
			}
		}
	}
	// One copied source page plus the one target page the rebuild rewrites.
	if pageObjects > 2 {
		t.Errorf("the insert queued %d page objects, want at most 2: unselected source pages came along", pageObjects)
	}
}
