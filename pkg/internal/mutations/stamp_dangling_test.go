package mutations

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// danglingContentsPDF builds a one-page document whose /Contents holds
// streams, given in order, the last of which may end inside a string.
func danglingContentsPDF(t *testing.T, streams ...string) []byte {
	t.Helper()

	refs := make([]string, len(streams))
	for i := range streams {
		refs[i] = fmt.Sprintf("%d 0 R", 5+i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> >>",
		"[" + strings.Join(refs, " ") + "]",
	}
	for _, s := range streams {
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s))
	}
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	if len(streams) != 2 {
		t.Fatalf("danglingContentsPDF numbers the font 7 0 R, so it takes two streams, got %d", len(streams))
	}

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
	return buf.Bytes()
}

// TestAppendPageContents_DanglingString pins that content appended after a
// page's own is read as operators. ISO 32000-1, 7.8.2 reads a /Contents array
// as one stream, and a literal or hex string the page never terminates
// (7.3.4.2, 7.3.4.3) runs to its end, so the restoring Q and the stamp written
// after it were read as string bytes and the page showed no stamp. Viewers
// discard the unterminated token at the end of the content, so ending the
// page's content before it renders the page as before.
func TestAppendPageContents_DanglingString(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const stamp = "BT /F1 12 Tf 10 10 Td (STAMP) Tj ET\n"
	for name, streams := range map[string][]string{
		"literal string in the last stream": {"BT /F1 24 Tf 50 400 Td (BODY) Tj ET\n", "0 0 m 10 10 l S\n(dangling ( string"},
		"hex string in the last stream":     {"BT /F1 24 Tf 50 400 Td (BODY) Tj ET\n", "0 0 m 10 10 l S\n<0A1B"},
		"string swallowing a later stream":  {"BT /F1 24 Tf 50 400 Td (BODY) Tj ET\n0 0 m 10 10 l S\n(dangling", "0 0 m 20 20 l S\n"},
		"terminated content, the control":   {"BT /F1 24 Tf 50 400 Td (BODY) Tj ET\n", "0 0 m 10 10 l S\n(closed) pop\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := danglingContentsPDF(t, streams...)
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
			before, err := collectPageContents(ctx, doc, pages[0])
			if err != nil {
				t.Fatalf("collectPageContents: %v", err)
			}
			wantPainted, err := content.ParseStreamData(ctx, before)
			if err != nil {
				t.Fatalf("parse source content: %v", err)
			}

			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx) + 1)
			stampRef := alloc.Alloc(ast.NewStream(ast.PdfDict{}, []byte(stamp)))
			contents := appendPageContents(ctx, doc, alloc, pages[0].Dict, newContentBrackets(alloc), stampRef)
			after, err := collectContents(ctx, alloc, doc, ast.PdfDict{"Contents": contents})
			if err != nil {
				t.Fatalf("collectContents: %v", err)
			}
			ops, err := content.ParseStreamData(ctx, after)
			if err != nil {
				t.Fatalf("parse stamped content: %v", err)
			}

			operators := make([]string, len(ops))
			for i, op := range ops {
				operators[i] = op.Operator
			}
			// The page's own operators, bracketed in q/Q, then the stamp's.
			want := []string{"q"}
			for _, op := range wantPainted {
				want = append(want, op.Operator)
			}
			want = append(want, "Q", "BT", "Tf", "Td", "Tj", "ET")
			if !slices.Equal(operators, want) {
				t.Errorf("stamped content reads operators %v, want %v:\n%s", operators, want, after)
			}
		})
	}
}
