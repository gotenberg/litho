package document

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// directKidsPDF builds a page tree holding a page at every place ISO 32000-1,
// Table 29 wants a reference and a producer can write a dictionary instead:
//
//   - page A (object 3) is listed by reference, as the table asks;
//   - page B is written directly in the /Kids of node K (object 4);
//   - page C is written directly in the /Kids of node N, itself written
//     directly in the /Kids of K;
//   - page D is written directly in array 6, the /Kids node L (object 5)
//     names by reference.
func directKidsPDF() []byte {
	return buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 4 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>",
		"<< /Type /Pages /Parent 2 0 R /Count 3 /Kids [<< /Type /Page /Parent 4 0 R /MediaBox [0 0 100 100] >> "+
			"<< /Type /Pages /Count 1 /Kids [<< /Type /Page /MediaBox [0 0 100 100] >>] >> 5 0 R] >>",
		"<< /Type /Pages /Parent 4 0 R /Count 1 /Kids 6 0 R >>",
		"[<< /Type /Page /Parent 5 0 R /MediaBox [0 0 100 100] >>]",
	)
}

// openPromoteDoc opens data for the promotion tests.
func openPromoteDoc(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// pageObjectNums returns the object number of every page doc reads, in order.
func pageObjectNums(t *testing.T, doc *Document) []int {
	t.Helper()
	ctx := context.Background()
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	nums := make([]int, len(pages))
	for i, p := range pages {
		nums[i] = p.Ref.ObjectNum
	}
	return nums
}

// TestPromoteDirectKids_NumbersEveryKidWrittenInKids pins that every page tree
// node written directly in a /Kids array gets an object number past the file's,
// in the order the walk reaches it, and that the node or the array listing it
// names it by reference from then on, for the document and its forks.
//
// ISO 32000-1, 7.7.3.2, Table 29 makes /Kids an array of indirect references. A
// page written directly in it read as object 0, so every writer path that
// replaces a page at its object number dropped the edit.
func TestPromoteDirectKids_NumbersEveryKidWrittenInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := openPromoteDoc(t, directKidsPDF())
	if got := pageObjectNums(t, doc); !slices.Equal(got, []int{3, 0, 0, 0}) {
		t.Fatalf("before the promotion the pages read as objects %v, want [3 0 0 0]", got)
	}

	// The page tree was walked above, so the promotion must win over what the
	// object cache holds for the nodes it rewrites.
	doc = openPromoteDoc(t, directKidsPDF())
	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 4}); err != nil {
		t.Fatalf("Resolve(4): %v", err)
	}
	if err := doc.PromoteDirectKids(ctx); err != nil {
		t.Fatalf("PromoteDirectKids: %v", err)
	}
	if got, want := pageObjectNums(t, doc), []int{3, 7, 9, 10}; !slices.Equal(got, want) {
		t.Errorf("the pages read as objects %v, want %v", got, want)
	}
	if got := doc.HighestObjectNumber(ctx); got != 10 {
		t.Errorf("HighestObjectNumber = %d, want 10, the last promoted kid", got)
	}

	nums := slices.Sorted(maps.Keys(doc.PromotedObjects()))
	if want := []int{4, 6, 7, 8, 9, 10}; !slices.Equal(nums, want) {
		t.Errorf("staged objects %v, want %v: node K, array 6 and the four kids", nums, want)
	}
	k, err := ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("ResolveDict(4): %v", err)
	}
	want := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 7}, ast.PdfIndirectRef{ObjectNum: 8}, ast.PdfIndirectRef{ObjectNum: 5}}
	if kids, _ := k["Kids"].(ast.PdfArray); !slices.Equal(kids, want) {
		t.Errorf("node K lists %v, want %v", k["Kids"], want)
	}
	if b, err := ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 7}); err != nil || b["Parent"] != (ast.PdfIndirectRef{ObjectNum: 4}) {
		t.Errorf("page B reads as %v (%v), want its dictionary with /Parent 4 0 R", b, err)
	}

	before := doc.PromotedObjects()
	if err := doc.PromoteDirectKids(ctx); err != nil {
		t.Fatalf("second PromoteDirectKids: %v", err)
	}
	if got := doc.PromotedObjects(); len(got) != len(before) {
		t.Errorf("a second promotion staged %d objects, want the %d of the first", len(got), len(before))
	}
	if got, want := pageObjectNums(t, doc.Fork()), []int{3, 7, 9, 10}; !slices.Equal(got, want) {
		t.Errorf("a fork reads the pages as objects %v, want %v", got, want)
	}
}

// TestPromoteDirectKids_LeavesAnIndirectTreeAlone pins that a page tree whose
// kids are all references stages nothing and keeps its numbering.
func TestPromoteDirectKids_LeavesAnIndirectTreeAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := openPromoteDoc(t, buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>",
	))
	if err := doc.PromoteDirectKids(ctx); err != nil {
		t.Fatalf("PromoteDirectKids: %v", err)
	}
	if got := doc.PromotedObjects(); got != nil {
		t.Errorf("staged %v, want nothing", got)
	}
	if got := doc.HighestObjectNumber(ctx); got != 3 {
		t.Errorf("HighestObjectNumber = %d, want 3", got)
	}
}

// TestPromoteDirectKids_RunsAgainAfterACancelledRun pins that a done ctx stops
// the promotion before it stages anything, and that a later call promotes.
func TestPromoteDirectKids_RunsAgainAfterACancelledRun(t *testing.T) {
	t.Parallel()

	doc := openPromoteDoc(t, directKidsPDF())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := doc.PromoteDirectKids(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("PromoteDirectKids under a cancelled ctx = %v, want context.Canceled", err)
	}
	if got := doc.PromotedObjects(); got != nil {
		t.Fatalf("a cancelled promotion staged %v", got)
	}
	if err := doc.PromoteDirectKids(context.Background()); err != nil {
		t.Fatalf("PromoteDirectKids: %v", err)
	}
	if got, want := pageObjectNums(t, doc), []int{3, 7, 9, 10}; !slices.Equal(got, want) {
		t.Errorf("the pages read as objects %v, want %v", got, want)
	}
}
