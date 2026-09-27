package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// sharedKidsTreePDF builds a document whose object 4 is a name and number tree
// root with /Kids 5 0 R, and whose object 5 is a /Kids array of direct nodes
// that each name it again as their own /Kids, beside one leaf mapping (k) and
// 0 to the file specification 6. Walked anew wherever it is reached, the array
// fans out 2^depth.
func sharedKidsTreePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 4 0 R /Dests 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Kids 5 0 R >>",
		"[<< /Kids 5 0 R >> << /Kids 5 0 R >> << /Names [(k) 6 0 R] /Nums [0 [6 0 R]] >>]",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>",
		"<< /Type /EmbeddedFile /Length 1 >>\nstream\nx\nendstream",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestTreeWalks_SharedKidsArrayFanOut pins that the mutation walks over name
// and number trees visit a /Kids array written as an object of its own once,
// so direct nodes that name it again cannot fan out 2^depth, and that each
// walk still reaches the leaf beside them.
func TestTreeWalks_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	root := ast.PdfIndirectRef{ObjectNum: 4}
	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"CollectEmbeddedFileEntries", func(t *testing.T) error {
			doc, alloc := newMutationDoc(t, sharedKidsTreePDF())
			var out []EFEntry
			CollectEmbeddedFileEntries(context.Background(), alloc, doc, root, 0, &out)
			if len(out) != 1 {
				return fmt.Errorf("collected %d entries, want 1", len(out))
			}
			return nil
		}},
		{"collectSourceEmbeddedFiles", func(t *testing.T) error {
			doc, _ := newMutationDoc(t, sharedKidsTreePDF())
			var out []EFEntry
			collectSourceEmbeddedFiles(context.Background(), doc, root, 0, &out)
			if len(out) != 1 {
				return fmt.Errorf("collected %d entries, want 1", len(out))
			}
			return nil
		}},
		{"pruneDestNameTree", func(t *testing.T) error {
			doc, alloc := newMutationDoc(t, sharedKidsTreePDF())
			keep := func(v ast.PdfObject) (ast.PdfObject, bool) { return v, false }
			pruneDestNameTree(context.Background(), alloc, doc, root, keep, 0)
			return nil
		}},
		{"structPrune.parentTree", func(t *testing.T) error {
			doc, alloc := newMutationDoc(t, sharedKidsTreePDF())
			p := newStructPrune(alloc, doc, func(ast.PdfIndirectRef) bool { return false }, nil, false)
			p.parentTree(context.Background(), root, 0)
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			done := make(chan error, 1)
			go func() { done <- tt.run(t) }()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the walk did not end before the deadline")
			}
		})
	}
}
