package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// sharedKidsTreeDoc builds a document whose object 4 is a tree root with /Kids
// 5 0 R, and whose object 5 is a /Kids array of direct nodes that each name it
// again as their own /Kids, beside one leaf holding the key 7 and the name
// (k). Walked anew wherever it is reached, the array fans out 2^depth.
func sharedKidsTreeDoc(t *testing.T) *Document {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Kids 5 0 R >>",
		"[<< /Kids 5 0 R >> << /Kids 5 0 R >> << /Names [(k) [3 0 R /Fit]] /Nums [7 (SEVEN)] >>]",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	doc, err := NewDocument(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestTreeWalks_SharedKidsArrayFanOut pins that every name-tree and
// number-tree walk visits a /Kids array written as an object of its own once,
// so direct nodes that name it again cannot fan out 2^depth up to
// maxNameTreeDepth, and that each walk still reaches the leaf beside them.
func TestTreeWalks_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	root := ast.PdfIndirectRef{ObjectNum: 4}
	tests := []struct {
		name string
		run  func(ctx context.Context, doc *Document) (int, error)
	}{
		{"WalkNameTree", func(ctx context.Context, doc *Document) (int, error) {
			n := 0
			err := WalkNameTree(ctx, doc, root, func(string, ast.PdfObject) { n++ })
			return n, err
		}},
		{"LookupNameTree absent key", func(ctx context.Context, doc *Document) (int, error) {
			v, err := LookupNameTree(ctx, doc, root, "absent")
			if v != nil {
				return -1, err
			}
			v, err = LookupNameTree(ctx, doc, root, "k")
			if v == nil {
				return 0, err
			}
			return 1, err
		}},
		{"WalkNumberTree", func(ctx context.Context, doc *Document) (int, error) {
			n := 0
			err := WalkNumberTree(ctx, doc, root, func(int) {}, func(int, ast.PdfObject) { n++ })
			return n, err
		}},
		{"LookupNumberTree absent key", func(ctx context.Context, doc *Document) (int, error) {
			v, err := LookupNumberTree(ctx, doc, root, 8)
			if v != nil {
				return -1, err
			}
			v, err = LookupNumberTree(ctx, doc, root, 7)
			if v == nil {
				return 0, err
			}
			return 1, err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			n, err := tt.run(ctx, sharedKidsTreeDoc(t))
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
			if err != nil {
				t.Fatalf("walk: %v", err)
			}
			if n != 1 {
				t.Errorf("reached %d leaf entries, want 1", n)
			}
		})
	}
}
