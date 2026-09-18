package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// doubledStructKidsLen is deep enough that an unguarded walk cannot finish:
// each element names its single child twice, so a walk that does not remember
// its verdicts follows 2^(n-1) paths.
const doubledStructKidsLen = 30

// doubledStructKidsPDF builds a tagged two-page document whose every structure
// element lists the same /K child twice. The chain sits well inside
// maxStructTreeDepth, so the depth bound never fires.
//
// The deepest element sits on page 1, the page the delete removes, so the prune
// has real work to do rather than bailing out early.
func doubledStructKidsPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	const firstElem = 8
	lastElem := firstElem + doubledStructKidsLen - 1
	parentTree := lastElem + 1

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /StructParents 0 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /StructParents 1 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /StructTreeRoot /K [%d 0 R %d 0 R] /ParentTree %d 0 R >>",
			firstElem, firstElem, parentTree),
	}
	for i := range doubledStructKidsLen {
		objNum := firstElem + i
		if i == doubledStructKidsLen-1 {
			objs = append(objs, "<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0 >>")
			continue
		}
		child := objNum + 1
		objs = append(objs, fmt.Sprintf(
			"<< /Type /StructElem /S /Div /P 7 0 R /K [%d 0 R %d 0 R] >>", child, child))
	}
	objs = append(objs, fmt.Sprintf("<< /Nums [0 [%d 0 R] 1 []] >>", lastElem))

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// TestDeletePages_DoubledStructKidsStayLinear pins that deleting a page from a
// document whose structure tree fans out cannot force exponential work.
//
// The prune walks /K through three functions that call one another, so the
// bound counts levels while the walk multiplies paths. Remembering each
// element's verdict holds it to one visit per element; the output is whatever
// the unguarded walk produced, so only the running time can show the difference.
func TestDeletePages_DoubledStructKidsStayLinear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newMutationDoc(t, doubledStructKidsPDF())
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- DeletePages(ctx, alloc, doc, pages, []int{1})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeletePages: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("DeletePages did not finish: a doubled /K is being pruned along every path")
	}
}
