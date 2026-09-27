package tagging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// twoPagePDF is a two-page document whose pages draw first and second.
func twoPagePDF(first, second string) []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(first), first),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(second), second),
	})
}

// TestAutoTag_ReportsWhyItLeavesADocumentAlone pins that AutoTag names the
// reason it tags nothing, and the page that reason concerns, rather than
// returning the same empty result for every one: a caller that cannot tell a
// deliberate skip from a document it failed to tag cannot trust either.
func TestAutoTag_ReportsWhyItLeavesADocumentAlone(t *testing.T) {
	t.Parallel()

	const text = "BT /F1 12 Tf 72 700 Td (Body text on the page.) Tj ET\n"
	for _, tc := range []struct {
		name     string
		data     []byte
		want     error
		wantPage int
	}{
		{name: "untagged text", data: buildTaggedTestPDF(text)},
		{
			name: "a structure tree",
			data: assemblePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text),
				"<< /Type /StructTreeRoot >>",
			}),
			want: ErrAlreadyTagged,
		},
		{
			name: "/MarkInfo /Marked true",
			data: assemblePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text),
			}),
			want: ErrAlreadyTagged,
		},
		{
			name:     "marked content on the second page",
			data:     twoPagePDF(text, "/Span <</Lang (en)>> BDC\n"+text+"EMC\n"),
			want:     ErrMarkedContent,
			wantPage: 2,
		},
		{
			name: "a page whose content is not a stream",
			data: assemblePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 42 >>",
			}),
			want:     ErrUnreadableContent,
			wantPage: 1,
		},
		{
			name: "a page with no content",
			data: assemblePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			}),
			want: ErrNothingToTag,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, err := document.NewDocument(ctx, bytes.NewReader(tc.data), int64(len(tc.data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
			res, err := AutoTag(ctx, alloc, doc)
			if err != nil {
				t.Fatalf("AutoTag: %v", err)
			}

			if tc.want == nil {
				if res.Skipped != nil || len(res.Descriptions) == 0 {
					t.Fatalf("AutoTag skipped (%v) with descriptions %v, want a structure tree", res.Skipped, res.Descriptions)
				}
				return
			}
			if !errors.Is(res.Skipped, tc.want) {
				t.Errorf("Skipped = %v, want %v", res.Skipped, tc.want)
			}
			if res.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", res.Page, tc.wantPage)
			}
			if len(res.Descriptions) != 0 || alloc.Writes() != 0 {
				t.Errorf("a skip queued %d objects and described %v", alloc.Writes(), res.Descriptions)
			}
		})
	}
}
