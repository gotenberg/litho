package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// doubledFieldKidsLen is deep enough that an unguarded walk cannot finish: each
// field names its single child twice, so a walk without a visited set follows
// 2^(n-1) paths. It stays inside maxFieldParentDepth, so the depth bound never
// fires.
const doubledFieldKidsLen = 30

// doubledFieldKidsPDF builds a two-page form whose every field lists the same
// /Kids child twice.
//
// The shape is what fieldKidOverrides needs before it walks at all: the page
// being deleted carries an annotation, so droppedAnnots is not empty; that
// widget sits on the deleted page only, or droppedPageAnnots would subtract it
// again; and the root field in /Fields is a different object, or the walk skips
// it as a merged field-widget.
func doubledFieldKidsPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	const rootField = 7

	objs := []string{
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [%d 0 R] >> >>", rootField),
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Annots [6 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /P 3 0 R >>",
	}
	for i := range doubledFieldKidsLen {
		objNum := rootField + i
		if i == doubledFieldKidsLen-1 {
			objs = append(objs, "<< /FT /Tx /T (leaf) >>")
			continue
		}
		child := objNum + 1
		objs = append(objs, fmt.Sprintf("<< /T (f) /Kids [%d 0 R %d 0 R] >>", child, child))
	}

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

// TestDeletePages_DoubledFieldKidsStayLinear pins that deleting a page from a
// document whose form field tree fans out cannot force exponential work.
//
// The field walk descends /Kids under maxFieldParentDepth, and a depth bound
// counts levels rather than paths, so a field naming one child twice costs
// 2^29 while nesting only 30 deep. The output is the same either way, since a
// repeat visit recomputes identical overrides under the same object numbers,
// so only the running time can show the difference.
func TestDeletePages_DoubledFieldKidsStayLinear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newMutationDoc(t, doubledFieldKidsPDF())
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
		t.Fatal("DeletePages did not finish: a doubled /Kids is being walked along every path")
	}
}
