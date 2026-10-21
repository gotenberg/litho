package remediation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// directKidPDF builds two pages that draw the same content, a red square at
// constant alpha 0.5 over a blue one: object 3, and one written directly in
// the /Kids array, which has no object number to write a flattened page back
// to (ISO 32000-1, 7.7.3.2).
func directKidPDF() []byte {
	const content = "0 0 1 rg 0 0 100 100 re f /H gs 1 0 0 rg 20 20 50 50 re f"
	const page = "/Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /ExtGState << /H 5 0 R >> >>"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R << " + page + " >>] /Count 2 >>",
		"<< " + page + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /ExtGState /ca 0.5 >>",
	})
}

// TestFixTransparencyFlattenLeavesARefusedPageToTheFallback pins that a page
// flattening refuses leaves the other pages flattened, and itself as it was
// for the opaque normalization: [flatten.Document] fails the whole run on the
// first page it refuses, with the pages before it already queued.
func TestFixTransparencyFlattenLeavesARefusedPageToTheFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, directKidPDF())

	fixes, err := fixTransparencyFlatten(ctx, alloc, doc, Options{PreserveContent: true}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixTransparencyFlatten: %v", err)
	}
	want := []string{
		"flattened transparency on 1 page(s): 1 region(s) composited into images at 300 dpi, 0 text object(s) kept as text; 1 page(s) refused, their transparency set opaque",
	}
	if !slices.Equal(fixes, want) {
		t.Fatalf("fixes = %q, want %q", fixes, want)
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) != 2 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	flat, err := mutations.LatestPageContents(ctx, alloc, doc, pages[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flat), "/FlatIm") {
		t.Errorf("page 1 draws no composite: %q", flat)
	}
	// The refused page keeps the source's content stream and resources.
	if pages[1].Ref.ObjectNum != 0 {
		t.Fatalf("precondition: page 2 must be written directly in /Kids")
	}
	kept, err := mutations.LatestPageContents(ctx, alloc, doc, pages[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(kept), "/FlatIm") || !strings.Contains(string(kept), "/H gs") {
		t.Errorf("page 2 changed: %q", kept)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Errorf("the graphics state page 2 draws with was rewritten: %v", alloc.Pending()[5])
	}
}

// TestFixTransparencyFlattenRunsAtPart1Only pins that flattening answers ISO
// 19005-1, 6.4 alone: parts 2 and later permit transparency.
func TestFixTransparencyFlattenRunsAtPart1Only(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, directKidPDF())
	fixes, err := fixTransparencyFlatten(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil || fixes != nil || len(alloc.Pending()) != 0 {
		t.Fatalf("fixes = %q, err = %v, %d objects queued", fixes, err, len(alloc.Pending()))
	}
}
