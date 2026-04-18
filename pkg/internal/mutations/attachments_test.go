package mutations

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// minimalPDF returns a handcrafted minimal 1-page PDF for tests.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.7\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \n" +
			"0000000009 00000 n \n" +
			"0000000058 00000 n \n" +
			"0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

func TestAddAttachmentWithOptions_AFRelationship(t *testing.T) {
	ctx := context.Background()
	data := minimalPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)

	err = AddAttachmentWithOptions(ctx, alloc, doc, "invoice.xml", []byte("<?xml version=\"1.0\"?><invoice/>"),
		AttachmentOptions{
			MimeType:       "application/xml",
			AFRelationship: "Alternative",
			AddToCatalogAF: true,
		})
	if err != nil {
		t.Fatalf("AddAttachmentWithOptions: %v", err)
	}

	// Verify the filespec dict has /AFRelationship.
	foundAFRel := false
	foundCatalogAF := false
	for _, obj := range alloc.Pending() {
		s := obj.String()
		if strings.Contains(s, "AFRelationship") && strings.Contains(s, "Alternative") {
			foundAFRel = true
		}
		if strings.Contains(s, "/AF") && strings.Contains(s, "/Catalog") {
			foundCatalogAF = true
		}
	}
	if !foundAFRel {
		t.Error("expected /AFRelationship /Alternative in pending allocations")
	}
	if !foundCatalogAF {
		t.Error("expected /AF array in catalog")
	}
}

func TestAddAttachment_BackwardCompat(t *testing.T) {
	ctx := context.Background()
	data := minimalPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)

	err = AddAttachment(ctx, alloc, doc, "data.csv", []byte("a,b,c\n1,2,3"), "text/csv")
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Old API should NOT add /AF or /AFRelationship.
	for _, obj := range alloc.Pending() {
		s := obj.String()
		if strings.Contains(s, "AFRelationship") {
			t.Error("legacy AddAttachment should not set /AFRelationship")
		}
	}
}
