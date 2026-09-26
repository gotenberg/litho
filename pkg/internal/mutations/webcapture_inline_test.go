package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestDeletePagesPrunesInlineWebCaptureSets pins that a Web Capture content
// set written inline stops naming a deleted page, wherever the name tree
// holding it is written.
//
// A content set lists what one capture produced in /O (ISO 32000-1, Table
// 356), so one naming a deleted page keeps that page in the output. The prune
// rewrote a set, or its /O array, only at an object number of its own, and a
// set written inline in the tree, or a tree written inline in the catalog's
// /Names, had none: the set kept naming the page and the rewrite carried it.
func TestDeletePagesPrunesInlineWebCaptureSets(t *testing.T) {
	t.Parallel()

	const sets = "(both) << /Type /SPS /S /SPS /O [3 0 R 4 0 R] >> (one) << /Type /SPS /S /SPS /O [3 0 R] >>"
	page := func(word string) string {
		content := fmt.Sprintf("BT /F1 12 Tf 20 700 Td (%s) Tj ET\n", word)
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	for _, tc := range []struct {
		name    string
		catalog string
		extra   []string
	}{
		{
			name:    "a tree written inline in the catalog's /Names",
			catalog: "<< /Type /Catalog /Pages 2 0 R /Names << /IDS << /Names [" + sets + "] >> >> >>",
		},
		{
			name:    "sets written inline in a tree of its own",
			catalog: "<< /Type /Catalog /Pages 2 0 R /Names 8 0 R >>",
			extra:   []string{"<< /IDS 9 0 R >>", "<< /Names [" + sets + "] >>"},
		},
		{
			name:    "sets written inline under an inline root with kids",
			catalog: "<< /Type /Catalog /Pages 2 0 R /Names << /URLS << /Kids [8 0 R] >> >> >>",
			extra:   []string{"<< /Limits [(a) (z)] /Names [" + sets + "] >>"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			objs := append([]string{
				tc.catalog,
				"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
					" /Resources << /Font << /F1 7 0 R >> >> >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
					" /Resources << /Font << /F1 7 0 R >> >> >>",
				page("deletedpage"),
				page("keptpage"),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			}, tc.extra...)
			var buf bytes.Buffer
			buf.WriteString("%PDF-1.7\n")
			offs := make([]int, len(objs)+1)
			for i, o := range objs {
				offs[i+1] = buf.Len()
				fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
			}
			xref := buf.Len()
			fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
			for i := 1; i <= len(objs); i++ {
				fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
			}
			fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
			data := buf.Bytes()

			ctx := context.Background()
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
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
			if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}

			var out bytes.Buffer
			if err := FullRewrite(ctx, &out, FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
				t.Fatalf("FullRewrite: %v", err)
			}
			if bytes.Contains(out.Bytes(), []byte("deletedpage")) {
				t.Error("the output holds the deleted page's content")
			}
			if !bytes.Contains(out.Bytes(), []byte("(both)")) {
				t.Error("the content set that produced the kept page too left the tree")
			}
			if bytes.Contains(out.Bytes(), []byte("(one)")) {
				t.Error("the content set that produced only the deleted page stayed in the tree")
			}
		})
	}
}
