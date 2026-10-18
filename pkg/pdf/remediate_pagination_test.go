package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediatePDFUAKeepsAPageNumberOnlyPage pins that a PDF/UA-1 remediation
// whose auto-tagging marks a page's number as a pagination artifact succeeds,
// and that the number then extracts with artifacts only.
func TestRemediatePDFUAKeepsAPageNumberOnlyPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := onePageTextPDF("BT /F1 12 Tf 72 700 Td (A) Tj ET\nBT /F1 9 Tf 300 30 Td (Page 1 of 1) Tj ET\n")
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if _, err := w.RemediatePDFUA(ctx, models.PDFUA1); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	res, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	plain, err := res.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	all, err := res.ExtractText(ctx, 1, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractText WithArtifacts: %v", err)
	}
	if strings.Contains(plain, "Page") || !strings.Contains(all, "Page 1 of 1") {
		t.Errorf("page number not an artifact: default %q, with artifacts %q", plain, all)
	}
}
