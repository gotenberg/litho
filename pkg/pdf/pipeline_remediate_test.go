package pdf

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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

// TestPipelineRemediateAfterDeleteIsNotAFalseRegression pins that remediation
// judges itself against the document the pipeline staged.
//
// The integrity baseline came from the source Reader, so a delete queued before
// remediation left the gate comparing a page count remediation never had:
// `litho pipe f delete -pages 1 remediate -profile PDF/A-2b` failed outright
// with "remediation broke the PDF beyond the configured regression tolerance:
// page count changed from 41 to 40" and rolled back a remediation that had done
// nothing wrong.
func TestPipelineRemediateAfterDeleteIsNotAFalseRegression(t *testing.T) {
	t.Parallel()

	p := NewPipeline().Open(openThreePage(t)).
		Apply(func(_ context.Context, w *Writer) error { return w.DeletePages(1) }).
		Apply(func(ctx context.Context, w *Writer) error {
			_, err := w.RemediatePDFA(ctx, models.PDFA2b)
			return err
		})

	var out bytes.Buffer
	if _, err := p.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	r, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening output: %v", err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	}()

	n, err := r.PageCount(context.Background())
	if err != nil {
		t.Fatalf("reading page count: %v", err)
	}
	if n != 2 {
		t.Errorf("output has %d pages, want 2: the delete asked for one fewer than the three it started with", n)
	}
}

// TestIncrementalWriteComposesOverridesWithMutations pins the ordering on the
// incremental write path, where remediation does not force a full rewrite.
//
// The path applied the queued mutations and then seeded the remediation
// overrides "last so they win", so a mutation touching the same page was
// discarded.
//
// Driving w.rewriteOverrides directly keeps the test on the ordering itself
// rather than on which fixers a given profile happens to run.
//
// # Corpus evidence
//
//   - pdfjs bug1708041.pdf: came out of
//     `litho pipe f remediate -profile PDF/A-2b rotate -angle 90` with no
//     rotation on any of its six pages, while the two commands run separately
//     rotated it.
func TestIncrementalWriteComposesOverridesWithMutations(t *testing.T) {
	t.Parallel()

	// A fixture with correct xref offsets: threePagePDF is hand-written and its
	// offsets force a rebuild, which forces the full-rewrite path and never
	// reaches the branch under test.
	data := validXrefPDF(t)
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	if r.doc.Rebuilt {
		t.Fatal("fixture xref was rebuilt, so the write would take the full-rewrite path")
	}
	w := NewWriter(r)

	// Stand in for a remediation fix on page 1 (object 3 in the fixture): a
	// key no mutation writes, so its survival is unambiguous.
	page, err := document.ResolveDict(context.Background(), r.doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolving page 1: %v", err)
	}
	fixed := make(ast.PdfDict, len(page)+1)
	maps.Copy(fixed, page)
	fixed["UserUnit"] = ast.PdfInt(2)
	w.rewriteOverrides = map[int]ast.PdfObject{3: fixed}

	if err := w.RotatePages(90, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	got, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening output: %v", err)
	}
	defer func() {
		if closeErr := got.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	}()

	final, err := document.ResolveDict(context.Background(), got.doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolving page 1 of the output: %v", err)
	}
	if unit, _ := final["UserUnit"].(ast.PdfInt); unit != 2 {
		t.Errorf("/UserUnit = %v, want 2: the mutation discarded the staged fix", final["UserUnit"])
	}
	if rotate, _ := final["Rotate"].(ast.PdfInt); rotate != 90 {
		t.Errorf("/Rotate = %v, want 90: the staged fix discarded the mutation", final["Rotate"])
	}
}

// validXrefPDF builds a three-page document with correctly computed xref
// offsets, so the reader does not rebuild and the writer stays incremental.
func validXrefPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> >>",
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
