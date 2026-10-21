package mutations

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestRedactPageRegions_ReadsADirectStreamInTheContentsArray covers a page
// whose /Contents array holds a stream written inline, as the litho corpus's
// directstream_2b does. ISO 32000-1, 7.3.8.1 makes every stream indirect, but
// litho reads one written inline, and so does every viewer: the redaction has
// to see what it draws. Skipping it reported success with nothing removed and
// left the text inside the region on the page.
func TestRedactPageRegions_ReadsADirectStreamInTheContentsArray(t *testing.T) {
	ctx := context.Background()
	keep := "BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	secret := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents [6 0 R << /Length " +
		strconv.Itoa(len(secret)) + " >>\nstream\n" + secret + "\nendstream\n] >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(keep)) + " >>\nstream\n" + keep + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
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
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Fatal("the redaction removed nothing: the inline stream's text was never read")
	}
	got := newPageContents(t, alloc, pages[0].Ref.ObjectNum)
	if bytes.Contains(got, []byte("SECRET")) {
		t.Errorf("the covered text is still in the page content: %q", got)
	}
	if !bytes.Contains(got, []byte("KEEP")) {
		t.Errorf("text outside the region was removed too: %q", got)
	}
}
