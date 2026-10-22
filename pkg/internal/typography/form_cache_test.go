package typography

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// formCacheText extracts the text of the first page of the document
// [formThenImagePDF] builds, with the given objects staged on the machine.
func formCacheText(ctx context.Context, t *testing.T, doc *document.Document, staged map[int]ast.PdfObject) string {
	t.Helper()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	machine.Staged = staged
	elements, err := ExtractElements(ctx, doc, machine, pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	var b strings.Builder
	for _, el := range elements {
		b.WriteString(el.Text)
	}
	return b.String()
}

// TestFormContentCache pins that text extraction parses a form the file
// defines once per document and reads the same text from the cached parse,
// keeps a whole-stream parse apart from the default one, and never caches or
// reads the cache for a form an edit staged under the same object number.
func TestFormContentCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := formThenImagePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := formCacheText(ctx, t, doc, nil); got != "Z" {
		t.Fatalf("first extraction = %q, want %q", got, "Z")
	}
	if _, ok := doc.CachedFormContent(7, formOpsVariantText); !ok {
		t.Fatalf("form 7 was not cached by the first extraction")
	}
	if _, ok := doc.CachedFormContent(7, formOpsVariantTextWhole); ok {
		t.Errorf("a default extraction cached the whole-stream variant")
	}
	if got := formCacheText(ctx, t, doc, nil); got != "Z" {
		t.Errorf("extraction from the cached form = %q, want %q", got, "Z")
	}

	if got := formCacheText(content.WithWholeStreams(ctx), t, doc, nil); got != "Z" {
		t.Errorf("whole-stream extraction = %q, want %q", got, "Z")
	}
	if _, ok := doc.CachedFormContent(7, formOpsVariantTextWhole); !ok {
		t.Errorf("a whole-stream extraction did not cache its own variant")
	}

	form := "BT /F1 12 Tf (Y) Tj ET"
	staged := map[int]ast.PdfObject{7: ast.NewStream(ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"BBox":      ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(50), ast.PdfInt(50)},
		"Resources": ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 4}}},
	}, []byte(form))}
	if got := formCacheText(ctx, t, doc, staged); got != "Y" {
		t.Errorf("extraction with form 7 staged = %q, want the staged form's %q", got, "Y")
	}
	if got := formCacheText(ctx, t, doc, nil); got != "Z" {
		t.Errorf("extraction after a staged run = %q, want the file's %q", got, "Z")
	}
}

// TestFormContentCacheConcurrentReads pins that extractions running at once
// on one document share the cached form under -race: each reads the same
// operations, and none edits them.
func TestFormContentCacheConcurrentReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := formThenImagePDF()
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
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}

	const readers = 8
	got := make([]string, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Go(func() {
			machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
			elements, err := ExtractElements(ctx, doc, machine, pages[0], ops)
			errs[i] = err
			for _, el := range elements {
				got[i] += el.Text
			}
		})
	}
	wg.Wait()
	for i := range readers {
		if errs[i] != nil || got[i] != "Z" {
			t.Errorf("reader %d extracted %q (%v), want %q", i, got[i], errs[i], "Z")
		}
	}
}
