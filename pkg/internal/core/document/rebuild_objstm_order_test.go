package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// objStm builds an uncompressed object stream holding one object.
func objStm(objStmNum, containedNum int, body string) string {
	header := fmt.Sprintf("%d 0 ", containedNum)
	payload := header + body
	dict := fmt.Sprintf("<< /Type /ObjStm /N 1 /First %d /Length %d >>", len(header), len(payload))
	return fmt.Sprintf("%d 0 obj\n%s\nstream\n%s\nendstream\nendobj\n", objStmNum, dict, payload)
}

// TestRebuild_LaterObjectStreamWins pins that a rebuild takes the definition
// that sits LATER in the file, the same rule the raw header scan follows.
//
// An incrementally updated PDF appends a new revision, so the newer object
// stream is further down the file but may carry any object number. Ordering
// the expansion by object number instead let the FIRST revision win.
//
// # Corpus evidence
//
//   - pdfjs issue14269.pdf: came back with the original /Annots array and lost
//     one of its two signature widgets.
func TestRebuild_LaterObjectStreamWins(t *testing.T) {
	// The old revision's stream is object 3 and sits early; the new revision's
	// is object 9 and sits later, which is what an incremental update produces.
	// Ordered by object number the old one is visited first and, because the
	// entry then already exists, the new revision is skipped.
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [4 0 R] /Count 1 >>\nendobj\n")
	buf.WriteString("4 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 5 0 R >>\nendobj\n")
	buf.WriteString(objStm(3, 5, "[(old)]"))
	buf.WriteString(objStm(9, 5, "[(new) (second)]"))
	// startxref past EOF forces the rebuild path.
	buf.WriteString("trailer\n<< /Size 10 /Root 1 0 R >>\nstartxref\n999999\n%%EOF\n")

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if !doc.Rebuilt {
		t.Fatal("expected the rebuild path to run")
	}

	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	arr, err := ResolveArray(ctx, doc, pages[0].Dict["Annots"])
	if err != nil {
		t.Fatalf("resolving /Annots: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("/Annots has %d entries (%v), want 2 from the later object stream", len(arr), arr)
	}
}
