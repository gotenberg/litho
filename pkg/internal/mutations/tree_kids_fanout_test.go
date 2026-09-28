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
		{"readNameTree", func(t *testing.T) error {
			doc, alloc := newMutationDoc(t, sharedKidsTreePDF())
			entries, ok := readNameTree(context.Background(), alloc, doc, root)
			if !ok || len(entries) != 1 {
				return fmt.Errorf("read %d entries, ok %v; want 1, true", len(entries), ok)
			}
			return nil
		}},
		{"readParentTree", func(t *testing.T) error {
			doc, alloc := newMutationDoc(t, sharedKidsTreePDF())
			entries, maxKey, ok := readParentTree(context.Background(), alloc, doc, root)
			if !ok || len(entries) != 1 || maxKey != 0 {
				return fmt.Errorf("read %d entries, highest key %d, ok %v; want 1, 0, true", len(entries), maxKey, ok)
			}
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

// layeredKidsTreePDF builds a document whose object 4 is a name and number
// tree root over layers /Kids arrays, each written as an object of its own
// holding two direct nodes that name the next layer's array, the last holding
// one leaf mapping (k) and 0 to the page. The tree holds no cycle and stays
// under every depth cap, so only marking the arrays keeps a walk from fanning
// out 2^layers.
func layeredKidsTreePDF(layers int) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Kids 5 0 R >>",
	}
	for i := range layers {
		next := 6 + i
		if i == layers-1 {
			objs = append(objs, "[<< /Names [(k) 3 0 R] /Nums [0 [3 0 R]] >>]")
			continue
		}
		objs = append(objs, fmt.Sprintf("[<< /Kids %d 0 R >> << /Kids %d 0 R >>]", next, next))
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

// TestTreeReads_LayeredKidsArraysFanOut pins that the structure name and
// parent tree reads visit each /Kids array once: 40 layers of shared arrays,
// walked anew wherever reached, are 2^40 visits.
func TestTreeReads_LayeredKidsArraysFanOut(t *testing.T) {
	t.Parallel()
	root := ast.PdfIndirectRef{ObjectNum: 4}
	tests := []struct {
		name string
		read func(ctx context.Context, t *testing.T) (int, bool)
	}{
		{"readNameTree", func(ctx context.Context, t *testing.T) (int, bool) {
			doc, alloc := newMutationDoc(t, layeredKidsTreePDF(40))
			entries, ok := readNameTree(ctx, alloc, doc, root)
			return len(entries), ok
		}},
		{"readParentTree", func(ctx context.Context, t *testing.T) (int, bool) {
			doc, alloc := newMutationDoc(t, layeredKidsTreePDF(40))
			entries, _, ok := readParentTree(ctx, alloc, doc, root)
			return len(entries), ok
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			n, ok := tt.read(ctx, t)
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
			if !ok || n != 1 {
				t.Errorf("read %d entries, ok %v; want 1, true", n, ok)
			}
		})
	}
}
