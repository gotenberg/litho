package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// pastAttachBound nests past the attachment walk's bound.
// mutations.maxAttachmentTreeDepth is 64 and unexported, so a literal past it
// is the only way to express this from the public package.
const pastAttachBound = 70

// atAttachBound is the deepest tree the walk still reads to the bottom.
const atAttachBound = 64

// attachDepthPDF builds a one-page document whose /Names/EmbeddedFiles tree has
// a shallow leaf at depth 1 and a second branch `chain` nodes deep.
//
// A name-tree node carries /Kids or /Names, never both (the walk returns after
// /Kids), so the two attachments hang off separate branches of the root. The
// root is examined at depth 0 and its kids at depth 1, putting the deep leaf at
// depth `chain`.
func attachDepthPDF(chain int) []byte {
	const pageContent = "0 0 1 RG 10 10 m 20 20 l S\n"
	const shallowData = "SHALLOWATTACHDATA"
	const deepData = "DEEPATTACHDATA"

	chainFirst := 9
	deepFilespec := chainFirst + chain
	deepStream := deepFilespec + 1

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		fmt.Sprintf("<< /Kids [6 0 R %d 0 R] >>", chainFirst),
		"<< /Names [(shallow.txt) 7 0 R] >>",
		"<< /Type /Filespec /F (shallow.txt) /EF << /F 8 0 R >> >>",
		fmt.Sprintf("<< /Type /EmbeddedFile /Length %d >>\nstream\n%s\nendstream",
			len(shallowData), shallowData),
	}
	for i := range chain {
		objNum := chainFirst + i
		if i == chain-1 {
			objs = append(objs, fmt.Sprintf("<< /Names [(deep.txt) %d 0 R] >>", deepFilespec))
			continue
		}
		objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R] >>", objNum+1))
	}
	objs = append(objs,
		fmt.Sprintf("<< /Type /Filespec /F (deep.txt) /EF << /F %d 0 R >> >>", deepStream),
		fmt.Sprintf("<< /Type /EmbeddedFile /Length %d >>\nstream\n%s\nendstream",
			len(deepData), deepData),
	)

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

// attachNamesIn lists the attachment names the output document carries. It asks
// the reader rather than scanning bytes: attachment edits use an incremental
// update, which keeps every superseded object in the file, so a byte scan
// cannot see a removal however well it worked.
func attachNamesIn(t *testing.T, out *bytes.Buffer) string {
	t.Helper()

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	files, err := r.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("EmbeddedFiles: %v", err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// writeWith opens the fixture, applies mutate, and writes.
func writeWith(t *testing.T, chain int, mutate func(*Writer)) (*bytes.Buffer, error) {
	t.Helper()

	ctx := context.Background()
	data := attachDepthPDF(chain)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	mutate(w)
	var out bytes.Buffer
	return &out, w.WriteTo(ctx, &out)
}

// TestAddAttachment_DeepTreeRefused pins that adding to a tree the walk cannot
// read whole is refused rather than performed.
//
// AddAttachmentWithOptions rebuilds the /EmbeddedFiles tree from what the walk
// collected, so an entry below the bound was absent from the rebuild and was
// destroyed. Measured before the guard: adding one attachment to a 70-deep tree
// left [new.txt,shallow.txt], having silently erased deep.txt.
func TestAddAttachment_DeepTreeRefused(t *testing.T) {
	t.Parallel()

	out, err := writeWith(t, pastAttachBound, func(w *Writer) {
		w.AddAttachment("new.txt", []byte("NEWDATA"), "text/plain")
	})
	if !errors.Is(err, ErrAttachmentTreeIncomplete) {
		t.Fatalf("WriteTo error = %v, want ErrAttachmentTreeIncomplete", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes; a refused edit must write nothing", out.Len())
	}
}

// TestRemoveAttachment_DeepTreeRefused pins the two silent outcomes a truncated
// read had: removing the shallow file erased the deep one as well (the rebuilt
// tree omitted it), and removing the deep file matched nothing so the removal
// reported success having removed nothing.
func TestRemoveAttachment_DeepTreeRefused(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"shallow.txt", "deep.txt"} {
		out, err := writeWith(t, pastAttachBound, func(w *Writer) {
			w.RemoveAttachment(target)
		})
		if !errors.Is(err, ErrAttachmentTreeIncomplete) {
			t.Fatalf("removing %q: WriteTo error = %v, want ErrAttachmentTreeIncomplete", target, err)
		}
		if out.Len() != 0 {
			t.Errorf("removing %q: wrote %d bytes; a refused edit must write nothing", target, out.Len())
		}
	}
}

// TestSplit_DeepAttachmentTreeRefused pins the carry path. Split rebuilds the
// destination's tree from the source walk, so a truncated read carried none of
// the entries past the bound into the output.
func TestSplit_DeepAttachmentTreeRefused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := attachDepthPDF(pastAttachBound)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := Split(ctx, r, []int{1}, &out); !errors.Is(err, ErrAttachmentTreeIncomplete) {
		t.Fatalf("Split error = %v, want ErrAttachmentTreeIncomplete", err)
	}
}

// TestMerge_DeepAttachmentTreeRefused pins the worst mode: a merge rebuilds one
// tree from every source, so a truncated read dropped the deep attachment from
// BOTH of them. This is the path whose own comment records losing the
// ZUGFeRD invoice that makes a document a Factur-X document.
func TestMerge_DeepAttachmentTreeRefused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := attachDepthPDF(pastAttachBound)
	r1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	defer r1.Close()
	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}
	defer r2.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); !errors.Is(err, ErrAttachmentTreeIncomplete) {
		t.Fatalf("Merge error = %v, want ErrAttachmentTreeIncomplete", err)
	}
}

// TestAttachmentEdits_ShallowTreeStillWork is the control that keeps the four
// refusals from being blanket. A guard that refused every document would
// satisfy all of them while breaking attachment editing entirely.
func TestAttachmentEdits_ShallowTreeStillWork(t *testing.T) {
	t.Parallel()

	out, err := writeWith(t, 3, func(w *Writer) {
		w.AddAttachment("new.txt", []byte("NEWDATA"), "text/plain")
	})
	if err != nil {
		t.Fatalf("add on a shallow tree: %v, want success", err)
	}
	if got := attachNamesIn(t, out); got != "deep.txt,new.txt,shallow.txt" {
		t.Errorf("after add: %q, want all three", got)
	}

	out, err = writeWith(t, 3, func(w *Writer) {
		w.RemoveAttachment("shallow.txt")
	})
	if err != nil {
		t.Fatalf("remove on a shallow tree: %v, want success", err)
	}
	if got := attachNamesIn(t, out); got != "deep.txt" {
		t.Errorf("after remove: %q, want deep.txt alone", got)
	}
}

// TestAttachmentEdits_AtTheBoundStillWork pins that the refusal starts PAST the
// bound rather than at it, so the guard costs nothing for a tree the walk can
// still read to the bottom.
func TestAttachmentEdits_AtTheBoundStillWork(t *testing.T) {
	t.Parallel()

	out, err := writeWith(t, atAttachBound, func(w *Writer) {
		w.AddAttachment("new.txt", []byte("NEWDATA"), "text/plain")
	})
	if err != nil {
		t.Fatalf("add at the bound: %v, want success", err)
	}
	if got := attachNamesIn(t, out); got != "deep.txt,new.txt,shallow.txt" {
		t.Errorf("at the bound: %q, want all three", got)
	}
}

// TestSplitMerge_ShallowAttachmentTreeStillCarry is the control for the carry
// path: losing attachments is the bug rebuildEmbeddedFiles exists to fix, so an
// ordinary document must still carry them across.
func TestSplitMerge_ShallowAttachmentTreeStillCarry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := attachDepthPDF(3)

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	var split bytes.Buffer
	if err := Split(ctx, r, []int{1}, &split); err != nil {
		t.Fatalf("Split on a shallow tree: %v, want success", err)
	}
	if got := attachNamesIn(t, &split); got != "deep.txt,shallow.txt" {
		t.Errorf("split carried %q, want both attachments", got)
	}

	m1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open m1: %v", err)
	}
	defer m1.Close()
	m2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open m2: %v", err)
	}
	defer m2.Close()
	var merged bytes.Buffer
	if err := Merge(ctx, []*Reader{m1, m2}, &merged); err != nil {
		t.Fatalf("Merge on a shallow tree: %v, want success", err)
	}
	// uniqueEFName suffixes the duplicates rather than dropping them, so a
	// merge of a document with itself keeps both payloads.
	if got := attachNamesIn(t, &merged); !strings.Contains(got, "deep.txt") {
		t.Errorf("merge carried %q, want the deep attachment kept", got)
	}
}
