package compliance

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestNumberTreeIndex_AnswersAsLookups pins that the index of a number tree
// answers every key, present or absent, with what [numberTreeValue] answers
// from the root, whether the read was whole included: on duplicate keys, where
// the first entry a depth-first read meets wins, on a /Kids array two nodes
// share, on a cycle, on a tree deeper than [maxEFNameTreeDepth], and on keys
// written as references or past the int range.
func TestNumberTreeIndex_AnswersAsLookups(t *testing.T) {
	t.Parallel()
	deep := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [] /Count 0 >>"}
	// Objects 3 to 72 chain 70 levels down, each holding its depth as a key.
	for n := 3; n <= 72; n++ {
		kids := ""
		if n < 72 {
			kids = fmt.Sprintf(" /Kids [%d 0 R]", n+1)
		}
		deep = append(deep, fmt.Sprintf("<< /Nums [%d (d%d)]%s >>", n, n, kids))
	}
	trees := []struct {
		name string
		objs []string
		root int
	}{
		{"duplicates", []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			"<< /Nums [0 (a) 1 (b) 1 (b2)] /Kids [4 0 R 5 0 R << /Nums [3 (direct) 7 (g)] >>] >>",
			"<< /Nums [1 (c) 2 (d)] >>",
			"<< /Nums [2 (e) 3 (f) 6 0 R (ref) 99999999999999999999 (huge)] /Kids [3 0 R] >>",
			"4",
		}, 3},
		{"shared kids array", []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			"<< /Kids 4 0 R >>",
			"[<< /Kids 4 0 R >> << /Kids 4 0 R /Nums [1 (x)] >> << /Nums [0 [5 0 R]] >>]",
			"<< /Type /Filespec /F (a.txt) >>",
		}, 3},
		{"deeper than the bound", deep, 3},
	}
	for _, tree := range trees {
		t.Run(tree.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tree.objs...)
			ctx := withParentTreeIndexes(context.Background(), doc)
			ref := ast.PdfIndirectRef{ObjectNum: tree.root}
			root, err := document.ResolveDict(ctx, doc, ref)
			if err != nil {
				t.Fatal(err)
			}
			index := parentTreeIndexOf(ctx, doc, ref, root)
			if index == nil {
				t.Fatal("no index")
			}
			for key := -2; key <= 80; key++ {
				gotV, gotComplete := index.value(key)
				wantV, wantComplete := numberTreeValue(context.Background(), doc, root, key, 0, make(map[int]bool))
				if fmt.Sprintf("%#v", gotV) != fmt.Sprintf("%#v", wantV) || gotComplete != wantComplete {
					t.Errorf("key %d: index (%#v, %v), lookup (%#v, %v)", key, gotV, gotComplete, wantV, wantComplete)
				}
			}
		})
	}
}

// TestParentTreeIndexOf_ReadOnceAcrossGoroutines pins that the check groups
// of a run, asking for one tree at once, share one index, and that a run of
// another document, or a tree written directly, gets none.
func TestParentTreeIndexOf_ReadOnceAcrossGoroutines(t *testing.T) {
	t.Parallel()
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /Nums [0 (a)] >>",
	)
	ctx := withParentTreeIndexes(context.Background(), doc)
	ref := ast.PdfIndirectRef{ObjectNum: 3}
	root, err := document.ResolveDict(ctx, doc, ref)
	if err != nil {
		t.Fatal(err)
	}
	indexes := make([]*numberTreeIndex, 8)
	var wg sync.WaitGroup
	for i := range indexes {
		wg.Go(func() { indexes[i] = parentTreeIndexOf(ctx, doc, ref, root) })
	}
	wg.Wait()
	for i, index := range indexes {
		if index == nil || index != indexes[0] {
			t.Fatalf("goroutine %d got index %p, want the shared %p", i, index, indexes[0])
		}
	}
	if parentTreeIndexOf(ctx, doc, root, root) != nil {
		t.Error("a tree written directly is indexed")
	}
	other, _ := objsDocPDF(t, "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [] /Count 0 >>", "<< /Nums [0 (a)] >>")
	if parentTreeIndexOf(ctx, other, ref, root) != nil {
		t.Error("the run of one document indexes a tree of another")
	}
}
