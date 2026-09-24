package bookmarks

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// structDoc assembles a minimal tagged one-page document from object bodies
// (1-indexed). Object 1 is the catalog, object 3 the only page, and object 4
// the /StructTreeRoot.
func structDoc(t *testing.T, objs []string) (context.Context, *document.Document) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc
}

func structBookmarks(t *testing.T, objs []string) []string {
	t.Helper()

	ctx, doc := structDoc(t, objs)
	refs := map[ast.PdfIndirectRef]int{{ObjectNum: 3}: 1}
	mcid := map[int]map[int]string{1: {0: "Hello"}}

	bms, err := GenerateFromStructTree(ctx, doc, refs, mcid, Options{})
	if err != nil {
		t.Fatalf("generating from struct tree: %v", err)
	}

	// Flatten, because buildBookmarkTree returns ROOTS only: an H2 after an H1
	// becomes the H1's child. Counting roots cannot tell a correctly nested
	// tree from one where the second heading lost its text and was dropped for
	// being empty, since both leave exactly one root.
	var titles []string
	var walk func([]models.Bookmark)
	walk = func(bs []models.Bookmark) {
		for _, b := range bs {
			titles = append(titles, b.Title)
			walk(b.Children)
		}
	}
	walk(bms)
	return titles
}

// TestGenerateFromStructTree_NonHeadingCycleTerminates pins the mutual
// recursion: [walkStructElem] hands /K to [walkStructK], which hands any dict
// carrying /S back to [walkStructElem].
//
// The element is deliberately NOT a heading. [walkStructElem] calls
// [collectStructText] at the heading branch BEFORE it descends, so a heading
// element with the same cycle dies inside [collectTextFromK] instead and never
// reaches this one. Measured before the guard: frames alternating
// [walkStructK] <-> [walkStructElem] until "fatal error: stack overflow".
func TestGenerateFromStructTree_NonHeadingCycleTerminates(t *testing.T) {
	t.Parallel()

	structBookmarks(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /K 5 0 R >>",
		"<< /S /Div /K 5 0 R >>",
	})
}

// TestGenerateFromStructTree_HeadingCycleTerminates pins the OTHER recursion.
// [collectTextFromK] walks nested /K to gather a heading's text and recursed
// without a visited set of its own: measured before the guard as frames of
// [collectTextFromK] alone, never reaching the walk above it.
func TestGenerateFromStructTree_HeadingCycleTerminates(t *testing.T) {
	t.Parallel()

	structBookmarks(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /K 5 0 R >>",
		"<< /S /H1 /Pg 3 0 R /K 5 0 R >>",
	})
}

// TestGenerateFromStructTree_AcyclicTreeStillYieldsBookmark is the control. A
// guard that refused to descend would satisfy both tests above while producing
// no bookmark for any real tagged document.
func TestGenerateFromStructTree_AcyclicTreeStillYieldsBookmark(t *testing.T) {
	t.Parallel()

	titles := structBookmarks(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /K 5 0 R >>",
		"<< /S /H1 /Pg 3 0 R /K 6 0 R >>",
		"<< /MCID 0 >>",
	})

	if len(titles) != 1 {
		t.Fatalf("bookmarks = %d, want 1: the walk no longer reaches the heading", len(titles))
	}
	if titles[0] != "Hello" {
		t.Errorf("title = %q, want %q", titles[0], "Hello")
	}
}

// TestGenerateFromStructTree_SharedKidStillRead covers the shape a visited set
// could plausibly break: one content node legitimately referenced by two
// elements. Both headings must still read its text.
//
// This is why [collectStructText] allocates a fresh set per heading instead of
// sharing the walk's. With a shared set the second element would find object 7
// already marked, collect nothing, and be dropped for having empty text, and
// the assertion below is what separates that from correct nesting: the H2 is a
// CHILD of the H1, so both cases leave exactly one root.
func TestGenerateFromStructTree_SharedKidStillRead(t *testing.T) {
	t.Parallel()

	titles := structBookmarks(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /K [5 0 R 6 0 R] >>",
		"<< /S /H1 /Pg 3 0 R /K 7 0 R >>",
		"<< /S /H2 /Pg 3 0 R /K 7 0 R >>",
		"<< /MCID 0 >>",
	})

	if len(titles) != 2 {
		t.Fatalf("headings = %d (%q), want 2: a shared kid cost an element its text", len(titles), titles)
	}
	for i, got := range titles {
		if got != "Hello" {
			t.Errorf("heading %d = %q, want %q", i, got, "Hello")
		}
	}
}
