package tagging

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestAutoTag_SharedContents pins that pages naming one /Contents object, and
// marking it alike, name one marked stream after tagging, each page resolving
// its MCIDs through a /StructParents of its own (ISO 32000-1, 14.7.4.4). A
// marked stream for each page gave a marked form the shared content draws a
// second reference, which ISO 14289-1, 7.20 t2 forbids and veraPDF, parsing a
// content stream once for the document, did not report before tagging.
func TestAutoTag_SharedContents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const body = "BT /F1 12 Tf 72 700 Td (Shared paragraph.) Tj ET\n0 0 m 100 0 l S\n"
	for _, tc := range []struct {
		name   string
		page2  string
		shared bool
	}{
		{name: "one stream two pages name", page2: "5 0 R", shared: true},
		{name: "streams of their own", page2: "6 0 R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stream := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
			page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents %s >>"
			data := assemblePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R 7 0 R] /Count 2 >>",
				fmt.Sprintf(page, "5 0 R"),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				stream,
				stream,
				fmt.Sprintf(page, tc.page2),
			})
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(7)
			res, err := AutoTag(ctx, alloc, doc)
			if err != nil || res.Skipped != nil {
				t.Fatalf("AutoTag: %v, skipped %v", err, res.Skipped)
			}
			p1 := mustDict(t, alloc.Pending()[3], "page 1")
			p2 := mustDict(t, alloc.Pending()[7], "page 2")
			if p1["StructParents"] == p2["StructParents"] {
				t.Errorf("pages share /StructParents %v, want a key each", p1["StructParents"])
			}
			for i, p := range []ast.PdfDict{p1, p2} {
				ref := mustRef(t, p["Contents"], fmt.Sprintf("page %d /Contents", i+1))
				s, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
				if !ok {
					t.Fatalf("page %d /Contents %v: got %T, want a marked stream", i+1, ref, alloc.Pending()[ref.ObjectNum])
				}
				marked, err := s.Bytes(ctx)
				if err != nil {
					t.Fatalf("page %d marked stream: %v", i+1, err)
				}
				if !bytes.Contains(marked, []byte("/MCID 0")) {
					t.Errorf("page %d reads %q, want MCID 0 marked", i+1, marked)
				}
			}
			if same := p1["Contents"] == p2["Contents"]; same != tc.shared {
				t.Errorf("pages name /Contents %v and %v, shared = %v, want %v", p1["Contents"], p2["Contents"], same, tc.shared)
			}
		})
	}
}
