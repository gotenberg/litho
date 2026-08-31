package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// rotationsOf reads /Rotate off every page of the written document.
func rotationsOf(t *testing.T, data []byte) []int {
	t.Helper()

	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening output: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	n, err := r.PageCount(context.Background())
	if err != nil {
		t.Fatalf("reading page count: %v", err)
	}
	out := make([]int, n)
	for i := range n {
		page, pageErr := r.Page(context.Background(), i+1)
		if pageErr != nil {
			t.Fatalf("reading page %d: %v", i+1, pageErr)
		}
		out[i] = page.Rotate
	}
	return out
}

// pipeRotateAndRemediate runs the two stages in the given order through a
// pipeline and returns the bytes written.
func pipeRotateAndRemediate(t *testing.T, rotateFirst bool) []byte {
	t.Helper()

	rotate := func(_ context.Context, w *Writer) error { return w.RotatePages(90) }
	remediate := func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA2b)
		return err
	}

	p := NewPipeline().Open(openPageRewriteFixture(t))
	if rotateFirst {
		p = p.Apply(rotate).Apply(remediate)
	} else {
		p = p.Apply(remediate).Apply(rotate)
	}

	var out bytes.Buffer
	if _, err := p.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	return out.Bytes()
}

// TestPipelineRemediateKeepsAnEarlierRotation pins that remediation runs on the
// document as the caller staged it.
//
// Remediation applies its fixers eagerly against the source and its results win
// at the same object number when the write happens, so a rotation queued first
// was simply dropped: `litho pipe f rotate -angle 90 remediate -profile PDF/A-2b`
// wrote a file with no rotation while the two commands run separately rotated it.
func TestPipelineRemediateKeepsAnEarlierRotation(t *testing.T) {
	t.Parallel()

	for i, got := range rotationsOf(t, pipeRotateAndRemediate(t, true)) {
		if got != 90 {
			t.Errorf("page %d has rotation %d, want 90: remediation discarded the staged rotation", i+1, got)
		}
	}
}

// TestPipelineRotationAfterRemediateSurvives pins the other order: a mutation
// queued after remediation builds on its fixes instead of losing to them.
func TestPipelineRotationAfterRemediateSurvives(t *testing.T) {
	t.Parallel()

	for i, got := range rotationsOf(t, pipeRotateAndRemediate(t, false)) {
		if got != 90 {
			t.Errorf("page %d has rotation %d, want 90: the remediation overrides won over the later rotation", i+1, got)
		}
	}
}

// TestRemediateAloneAddsNoRotation is the control: neither change may invent a
// rotation on a document nobody asked to rotate.
func TestRemediateAloneAddsNoRotation(t *testing.T) {
	t.Parallel()

	p := NewPipeline().Open(openPageRewriteFixture(t)).Apply(func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA2b)
		return err
	})

	var out bytes.Buffer
	if _, err := p.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	for i, got := range rotationsOf(t, out.Bytes()) {
		if got != 0 {
			t.Errorf("page %d has rotation %d, want 0", i+1, got)
		}
	}
}

// pageRewriteFixture builds a one-page document whose /MediaBox is degenerate,
// so PDF/A remediation has to rewrite the page dictionary itself rather than
// only objects hanging off it. Without that the clobber never happens and a
// test of it passes against the broken code.
func pageRewriteFixture(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 20 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 0 0] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// openPageRewriteFixture opens [pageRewriteFixture] as a Reader.
func openPageRewriteFixture(t *testing.T) *Reader {
	t.Helper()

	data := pageRewriteFixture(t)
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}
