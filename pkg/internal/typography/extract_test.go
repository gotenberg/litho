package typography

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// minimalFontPDF is a single-page PDF that declares a Helvetica Type1
// font and a content stream drawing "Hello". Used to drive the full
// extractTextRecursive path (Tf, Tj, font resolution, text-matrix
// update, element emission) without spinning up pkg/pdf.
func minimalFontPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
			"6 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Hello) Tj ET\nendstream\nendobj\n" +
			"xref\n0 7\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000177 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000165 00000 n \r\n" +
			"0000000256 00000 n \r\n" +
			"trailer\n<< /Size 7 /Root 1 0 R >>\n" +
			"startxref\n326\n%%EOF\n")
}

// TestExtractElements_MinimalPDF runs ExtractElements against a crafted
// PDF with a known text string. The assertion is behavioral: the
// returned elements contain "Hello" somewhere in their Text fields,
// and the element count is non-zero. Exact counts, positions, and
// widths are not asserted — those vary with the implementation.
func TestExtractElements_MinimalPDF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalFontPDF()
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
	if len(pages) == 0 {
		t.Fatal("ResolvePages: no pages")
	}
	page := pages[0]

	ops, err := content.ParsePageContent(ctx, doc, page.Dict)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, err := ExtractElements(ctx, doc, machine, page, ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	if len(elements) == 0 {
		t.Fatal("ExtractElements: got 0 elements, want >0")
	}

	var joined strings.Builder
	for _, e := range elements {
		joined.WriteString(e.Text)
	}
	if !strings.Contains(joined.String(), "Hello") {
		t.Errorf("extracted text: got %q, want to contain %q", joined.String(), "Hello")
	}
}

// TestExtractText_MinimalPDF runs the higher-level ExtractText which
// reconstructs a string from the element list. Asserts the text is
// present.
func TestExtractText_MinimalPDF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalFontPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)
	page := pages[0]
	ops, _ := content.ParsePageContent(ctx, doc, page.Dict)

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	got, err := ExtractText(ctx, doc, machine, page, ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("ExtractText: got %q, want to contain %q", got, "Hello")
	}
}

// TestExtractElements_ContextCancellation honors a cancelled context.
// The per-op cancellation check in extractTextRecursive fires every
// 100 operators — for a 1-op content stream the outer ctx.Err() check
// at function entry is what catches this.
func TestExtractElements_ContextCancellation(t *testing.T) {
	t.Parallel()
	data := minimalFontPDF()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := document.ResolveCatalog(context.Background(), doc)
	pages, _ := document.ResolvePages(context.Background(), doc, catalog)
	page := pages[0]
	ops, _ := content.ParsePageContent(context.Background(), doc, page.Dict)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	// May return an error (ctx.Canceled) or partial results — either
	// is acceptable, the contract is "no panic, no block".
	_, _ = ExtractElements(ctx, doc, machine, page, ops)
}
