package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// prunedPageMarker is drawn only by page 1, the page these tests remove, so
// finding it in the output means a prune left a reference that kept the page.
const prunedPageMarker = "PRUNEDPAGEMARKER"

// Each bound is unexported in mutations, so a literal past it is the only way to
// express "deeper than this prune walks" from the public package:
// maxOutlineDepth and maxDestTreeDepth are 64, maxFieldParentDepth is 32.
const (
	pastOutlineBound = 70
	pastDestBound    = 70
	pastFieldBound   = 40
)

// prunePDF builds a two-page document where page 1 owns the marker content and
// page 2 owns different content, so the marker cannot be kept alive legitimately
// by the surviving page.
//
// Exactly one structure is nested at a time. Deleting a page means unhooking
// every kind of reference that reaches it, and each prune owns one kind, so a
// fixture that nested several could not say which walk failed. None of them
// carries a /StructTreeRoot either, so the structure-tree refusal cannot fire
// and mask the walk under test.
func prunePDF(outlineChain, destChain, fieldChain int) []byte {
	content1 := fmt.Sprintf("BT /F1 12 Tf 20 700 Td (%s) Tj ET\n", prunedPageMarker)
	const content2 = "BT /F1 12 Tf 20 700 Td (survivor) Tj ET\n"

	const widget = 8
	next := 9
	catalogExtra := ""
	var tail []string
	annots := ""

	if outlineChain > 0 {
		first := next
		catalogExtra += fmt.Sprintf(" /Outlines %d 0 R", first)
		tail = append(tail, fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Count 1 >>", first+1))
		for i := range outlineChain {
			objNum := first + 1 + i
			if i == outlineChain-1 {
				tail = append(tail, "<< /Title (deep) /Dest [3 0 R /Fit] >>")
				continue
			}
			tail = append(tail, fmt.Sprintf("<< /Title (n) /First %d 0 R >>", objNum+1))
		}
		next = first + 1 + outlineChain
	}

	if destChain > 0 {
		first := next
		catalogExtra += fmt.Sprintf(" /Names << /Dests %d 0 R >>", first)
		for i := range destChain {
			objNum := first + i
			if i == destChain-1 {
				tail = append(tail, "<< /Names [(deep) [3 0 R /Fit]] >>")
				continue
			}
			tail = append(tail, fmt.Sprintf("<< /Kids [%d 0 R] >>", objNum+1))
		}
		next = first + destChain
	}

	if fieldChain > 0 {
		first := next
		catalogExtra += fmt.Sprintf(" /AcroForm << /Fields [%d 0 R] >>", first)
		annots = fmt.Sprintf(" /Annots [%d 0 R]", widget)
		for i := range fieldChain {
			objNum := first + i
			if i == fieldChain-1 {
				tail = append(tail, fmt.Sprintf("<< /T (deep) /Kids [%d 0 R] >>", widget))
				continue
			}
			tail = append(tail, fmt.Sprintf("<< /T (n) /Kids [%d 0 R] >>", objNum+1))
		}
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R" + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >>" + annots + " >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content1), content1),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content2), content2),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /P 3 0 R >>",
	}
	objs = append(objs, tail...)

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

// deleteFirstOf runs the public delete path and returns the output and error.
func deleteFirstOf(t *testing.T, data []byte) (*bytes.Buffer, error) {
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

// TestDeletePages_DeepOutlineRefused pins that an outline nesting past its bound
// stops the deletion. An item below the bound keeps the /Dest and /A that name
// the deleted page, and the rewrite copies whatever the catalog reaches, so the
// page and its content would ship inside a document reporting one fewer page.
func TestDeletePages_DeepOutlineRefused(t *testing.T) {
	t.Parallel()

	out, err := deleteFirstOf(t, prunePDF(pastOutlineBound, 0, 0))
	if !errors.Is(err, ErrPageDeletionIncomplete) {
		t.Fatalf("WriteTo error = %v, want ErrPageDeletionIncomplete", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes; a refused deletion must write nothing", out.Len())
	}
}

// TestDeletePages_DeepDestNameTreeRefused pins the named-destination tree. Its
// walk serves two callers, the catalog /Names /Dests table and the Web Capture
// trees, so this covers the walk rather than one caller of it.
func TestDeletePages_DeepDestNameTreeRefused(t *testing.T) {
	t.Parallel()

	out, err := deleteFirstOf(t, prunePDF(0, pastDestBound, 0))
	if !errors.Is(err, ErrPageDeletionIncomplete) {
		t.Fatalf("WriteTo error = %v, want ErrPageDeletionIncomplete", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes; a refused deletion must write nothing", out.Len())
	}
}

// TestDeletePages_DeepFieldTreeRefused pins the field tree, whose bound is 32
// rather than 64. A widget left in its field's /Kids names the deleted page in
// /P, which is enough to carry that page into the output.
func TestDeletePages_DeepFieldTreeRefused(t *testing.T) {
	t.Parallel()

	out, err := deleteFirstOf(t, prunePDF(0, 0, pastFieldBound))
	if !errors.Is(err, ErrPageDeletionIncomplete) {
		t.Fatalf("WriteTo error = %v, want ErrPageDeletionIncomplete", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes; a refused deletion must write nothing", out.Len())
	}
}

// TestDeletePages_ShallowStructuresStillDelete is the control that keeps the
// three refusals from being blanket. A guard that refused every document would
// satisfy all of them while breaking deletion entirely, so an ordinary document
// carrying all three structures must still delete and must lose the page.
func TestDeletePages_ShallowStructuresStillDelete(t *testing.T) {
	t.Parallel()

	out, err := deleteFirstOf(t, prunePDF(3, 3, 3))
	if err != nil {
		t.Fatalf("WriteTo: %v, want success on an ordinary document", err)
	}
	if bytes.Contains(out.Bytes(), []byte(prunedPageMarker)) {
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

// TestDeletePages_AtTheBoundStillDeletes pins that the refusal starts past the
// bound rather than at it, so the guard does not cost a document the prune can
// still walk to the bottom.
func TestDeletePages_AtTheBoundStillDeletes(t *testing.T) {
	t.Parallel()

	// 64 is maxOutlineDepth and maxDestTreeDepth; 32 is maxFieldParentDepth.
	out, err := deleteFirstOf(t, prunePDF(64, 64, 32))
	if err != nil {
		t.Fatalf("WriteTo: %v, want success: these structures sit inside their bounds", err)
	}
	if bytes.Contains(out.Bytes(), []byte(prunedPageMarker)) {
		t.Error("the deleted page's content survives in the output")
	}
}

// TestSplit_DeepFieldTreeStillSplits pins a deliberate decision, not an
// oversight: split reads the same field walk through fieldKidOverrides and
// discards its truncation flag.
//
// Split copies outward from the pages it keeps, so an unextracted page is not
// reachable from the copied root however deep the field tree runs. Measured
// across chain depths well past the bound, split never carried the excluded
// page, so refusing here would break documents split handles correctly today.
func TestSplit_DeepFieldTreeStillSplits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := prunePDF(0, 0, pastFieldBound)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := Split(ctx, r, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v, want success: split does not refuse on a deep field tree", err)
	}
	if bytes.Contains(out.Bytes(), []byte(prunedPageMarker)) {
		t.Error("split carried the unextracted page's content into the output")
	}
}

// TestInsertPages_DeepFieldTreeStillInserts pins the same decision for insert,
// the third caller of fieldKidOverrides. Insert copies only the selected source
// pages, so an unselected one is unreachable from what it copies.
func TestInsertPages_DeepFieldTreeStillInserts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	target := prunePDF(0, 0, 3)
	source := prunePDF(0, 0, pastFieldBound)

	r, err := Open(ctx, bytes.NewReader(target), int64(len(target)))
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	defer r.Close()
	src, err := Open(ctx, bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer src.Close()

	w := NewWriter(r)
	if err := w.InsertPages(1, src, 2); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v, want success: insert does not refuse on a deep field tree", err)
	}
}
