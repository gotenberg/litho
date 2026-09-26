package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// deletedPageMarker is drawn only by page 1, the page these tests delete, so
// finding it in the output means the page's content survived the deletion.
const deletedPageMarker = "DELETEDPAGEMARKER"

// deeperThanPrune nests past the page prune's structure-tree bound.
// mutations.maxStructTreeDepth is 64 and unexported, so a literal past it is the
// only way to express this from the public package.
const deeperThanPrune = 70

// structDepthPDF builds a two-page tagged document. Page 1 owns a content
// stream carrying [deletedPageMarker]; page 2 owns a different one, so the
// marker cannot be kept alive legitimately by the surviving page.
//
// kChain is the length of the /K chain hanging off the structure tree root,
// whose BOTTOM element sits on page 1. ptChain is the depth of the /ParentTree
// number tree. Either one past the bound has to stop the deletion on its own:
// pruning /K alone leaves the page reachable through the parent tree, and the
// reverse holds too, so the two are exercised separately.
//
// bottomHasKids writes the bottom element with a /K entry; false omits /K
// entirely, which is what an ordinary leaf looks like and what must NOT be read
// as a truncated walk.
func structDepthPDF(kChain, ptChain int, bottomHasKids, withStructTree bool) []byte {
	content1 := fmt.Sprintf("BT /F1 12 Tf 20 700 Td (%s) Tj ET\n", deletedPageMarker)
	const content2 = "BT /F1 12 Tf 20 700 Td (survivor) Tj ET\n"

	const firstElem = 9
	lastElem := firstElem + kChain - 1
	firstPT := lastElem + 1

	structRoot := "8 0 R"
	if !withStructTree {
		structRoot = ""
	}
	catalog := "<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> >>"
	if withStructTree {
		catalog = "<< /Type /Catalog /Pages 2 0 R /StructTreeRoot " + structRoot +
			" /MarkInfo << /Marked true >> >>"
	}

	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> /StructParents 0 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> /StructParents 1 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content1), content1),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content2), content2),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	if withStructTree {
		objs = append(objs, fmt.Sprintf(
			"<< /Type /StructTreeRoot /K [%d 0 R] /ParentTree %d 0 R >>", firstElem, firstPT))

		for i := range kChain {
			objNum := firstElem + i
			parent := 8
			if i > 0 {
				parent = objNum - 1
			}
			if i == kChain-1 {
				kids := " /K 0"
				if !bottomHasKids {
					kids = ""
				}
				objs = append(objs, fmt.Sprintf(
					"<< /Type /StructElem /S /P /P %d 0 R /Pg 3 0 R%s >>", parent, kids))
				continue
			}
			objs = append(objs, fmt.Sprintf(
				"<< /Type /StructElem /S /Div /P %d 0 R /K [%d 0 R] >>", parent, objNum+1))
		}

		for i := range ptChain {
			if i == ptChain-1 {
				objs = append(objs, fmt.Sprintf("<< /Nums [0 [%d 0 R] 1 []] >>", lastElem))
				continue
			}
			objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R] >>", firstPT+i+1))
		}
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

// deletePageOne runs the public path and returns the output and the error.
func deletePageOne(t *testing.T, data []byte) (*bytes.Buffer, error) {
	t.Helper()

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.DeletePages(1); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	var out bytes.Buffer
	return &out, w.WriteTo(ctx, &out)
}

// TestDeletePages_DeepStructKidsRefused pins that a /K chain nesting past the
// prune's bound stops the deletion instead of writing a document that still
// carries the page.
//
// Measured before the guard, through Open/DeletePages/WriteTo: a 70-deep chain
// produced a file reporting one page whose bytes still contained page 1's
// content stream. The structure element below the bound keeps /Pg naming the
// deleted page, and the rewrite copies whatever the catalog reaches.
func TestDeletePages_DeepStructKidsRefused(t *testing.T) {
	t.Parallel()

	out, err := deletePageOne(t, structDepthPDF(deeperThanPrune, 2, true, true))
	if !errors.Is(err, ErrPageDeletionIncomplete) {
		t.Fatalf("WriteTo error = %v, want ErrPageDeletionIncomplete", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes; a refused deletion must write nothing", out.Len())
	}
}

// TestDeletePages_DeepParentTreeRefused pins the second truncation site. The /K
// chain here is short, so only the /ParentTree walk stops short: pruning /K
// alone leaves the entry that maps the removed page's /StructParents key back
// to its elements, and through their /Pg the page itself.
func TestDeletePages_DeepParentTreeRefused(t *testing.T) {
	t.Parallel()

	out, err := deletePageOne(t, structDepthPDF(2, deeperThanPrune, true, true))
	if !errors.Is(err, ErrPageDeletionIncomplete) {
		t.Fatalf("WriteTo error = %v, want ErrPageDeletionIncomplete", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes; a refused deletion must write nothing", out.Len())
	}
}

// TestDeletePages_ShallowStructTreeStillDeletes is the control that keeps the
// refusal from being blanket. An ordinary tagged document must still delete,
// and the deleted page's content must be gone from the output: a guard that
// refused everything would satisfy the two tests above while breaking deletion.
func TestDeletePages_ShallowStructTreeStillDeletes(t *testing.T) {
	t.Parallel()

	out, err := deletePageOne(t, structDepthPDF(3, 2, true, true))
	if err != nil {
		t.Fatalf("WriteTo: %v, want success on an ordinary tagged document", err)
	}
	if bytes.Contains(out.Bytes(), []byte(deletedPageMarker)) {
		t.Error("the deleted page's content survives in the output")
	}

	ctx := context.Background()
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r2.Close()
	if n, err := r2.PageCount(ctx); err != nil || n != 1 {
		t.Fatalf("page count = %d, err = %v; want 1", n, err)
	}
}

// TestDeletePages_ElementWithoutKidsStillDeletes pins the distinction the guard
// rests on. A structure element with no /K is a leaf, which every tagged tree
// is full of; the depth bound is a subtree the walk never read. Sharing one
// branch between the two made every leaf look truncated, which would refuse
// deletion on ordinary documents rather than on the pathological ones.
func TestDeletePages_ElementWithoutKidsStillDeletes(t *testing.T) {
	t.Parallel()

	out, err := deletePageOne(t, structDepthPDF(3, 2, false, true))
	if err != nil {
		t.Fatalf("WriteTo: %v, want success: an element with no /K is a leaf, not a truncated walk", err)
	}
	if bytes.Contains(out.Bytes(), []byte(deletedPageMarker)) {
		t.Error("the deleted page's content survives in the output")
	}
}

// TestDeletePages_NoStructTreeStillDeletes is the control for the early return:
// a document with no /StructTreeRoot has nothing to prune and must not be
// refused.
func TestDeletePages_NoStructTreeStillDeletes(t *testing.T) {
	t.Parallel()

	out, err := deletePageOne(t, structDepthPDF(0, 0, true, false))
	if err != nil {
		t.Fatalf("WriteTo: %v, want success on an untagged document", err)
	}
	if bytes.Contains(out.Bytes(), []byte(deletedPageMarker)) {
		t.Error("the deleted page's content survives in the output")
	}
}
