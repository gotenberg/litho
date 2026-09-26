package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// doubledKidsChainLen is deep enough that an unguarded walk cannot finish. Each
// node names its single child twice, so a walk without a visited set follows
// 2^(n-1) paths: at 30 that is more than half a billion.
const doubledKidsChainLen = 30

// doubledKidsTreePDF builds a name tree whose every node lists the same child
// twice. The tree is a chain of depth doubledKidsChainLen, well inside
// maxDestTreeDepth and maxStructTreeDepth, so the depth bound never fires: only
// a visited set keeps the walk linear.
//
// Object 5 is the root of the chain and object 4 is the value its leaf maps to.
func doubledKidsTreePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Annot /Subtype /Link >>",
	}
	for i := range doubledKidsChainLen {
		objNum := 5 + i
		if i == doubledKidsChainLen-1 {
			objs = append(objs, "<< /Names [(a) 4 0 R] >>")
			continue
		}
		next := objNum + 1
		objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R %d 0 R] >>", next, next))
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// TestPruneDestNameTree_DoubledKidsStayLinear pins that the walk visits each
// node once instead of following every path through it.
//
// A name tree is a strict tree (ISO 32000-1, 7.9.6), so a /Kids entry reached
// twice is malformed. maxDestTreeDepth alone does not contain it: the walk fans
// out, so the bound caps the work at branch^depth, and this chain sits entirely
// inside the bound while costing 2^29 traversals without the guard. The output
// is identical either way, since each kid is written back at its own index,
// which is why only the running time can show the difference.
func TestPruneDestNameTree_DoubledKidsStayLinear(t *testing.T) {
	t.Parallel()

	doc, alloc := newMutationDoc(t, doubledKidsTreePDF())
	ctx := context.Background()
	keepEverything := func(ast.PdfObject) bool { return false }

	done := make(chan struct{})
	go func() {
		pruneDestNameTree(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5}, keepEverything, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pruneDestNameTree did not finish: a doubled /Kids is being walked along every path")
	}
}

// TestPruneParentTree_DoubledKidsStayLinear pins the same property for the
// /ParentTree walk, which descends /Kids the same way and under the same bound.
func TestPruneParentTree_DoubledKidsStayLinear(t *testing.T) {
	t.Parallel()

	doc, alloc := newMutationDoc(t, doubledKidsTreePDF())
	ctx := context.Background()
	removed := make(map[int]bool)

	done := make(chan struct{})
	go func() {
		pruneParentTree(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5}, removed, 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pruneParentTree did not finish: a doubled /Kids is being walked along every path")
	}
}
