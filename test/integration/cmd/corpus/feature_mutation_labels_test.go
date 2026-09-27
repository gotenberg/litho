package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// labelledMutationPDF returns a three-page PDF, each page drawing its own line
// of text, whose catalog carries /PageLabels spelled as labels, or none when
// labels is empty.
func labelledMutationPDF(t *testing.T, labels string) []byte {
	t.Helper()
	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if labels != "" {
		catalog += " /PageLabels " + labels
	}
	objects := []string{
		catalog + " >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
	}
	for i := range 3 {
		objects = append(objects, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 6 0 R >> >> /Contents %d 0 R >>", 7+i))
	}
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	for i := range 3 {
		text := fmt.Sprintf("BT /F1 12 Tf 72 700 Td (Page %d text) Tj ET", i+1)
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text))
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// TestMutatePages_KeepsPageLabels pins the label promise of mutate-pages: the
// move keeps each page's label, and the check counts an output that shows the
// labels of the pages that used to stand where it put them.
func TestMutatePages_KeepsPageLabels(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("qpdf is not on PATH")
	}
	const labels = "<< /Nums [0 << /S /r >> 1 << /S /D /P (A-) /St 7 >>] >>"
	data := labelledMutationPDF(t, labels)

	f := newMutationFeature(specNamed(t, "pages"))
	for _, v := range runMutationFixtures(t, f, data, "PDF_A-2b") {
		if !v.Match || v.Metrics[metricPromiseBroken] != 0 {
			t.Errorf("%s: match=%t reason %q metrics %v, want the move to keep every label", v.FixtureID, v.Match, v.Reason, v.Metrics)
		}
	}

	// The input itself, read as the output of the move: every page shows the
	// label of the page that stood there, not of the page moved there.
	in := &mutationInput{data: data, pages: 3}
	stale := &mutationOutputDoc{data: data, pages: 3}
	plan := mutationPlan{pages: 3, pageSource: []int{1, 2, 0}}
	failure, ok := f.checkPageLabels(context.Background(), in, stale, plan)
	if !ok || failure.Metric != metricPromiseBroken || !strings.Contains(failure.Detail, "output page 1") {
		t.Errorf("a stale tree gave %+v (reported %t), want promise_broken on output page 1", failure, ok)
	}

	plain := labelledMutationPDF(t, "")
	if failure, ok := f.checkPageLabels(context.Background(), &mutationInput{data: plain, pages: 3},
		&mutationOutputDoc{data: data, pages: 3}, plan); !ok || !strings.Contains(failure.Detail, "no /PageLabels") {
		t.Errorf("labels invented for an unlabelled input gave %+v (reported %t)", failure, ok)
	}
	if _, ok := f.checkPageLabels(context.Background(), &mutationInput{data: plain, pages: 3},
		&mutationOutputDoc{data: plain, pages: 3}, plan); ok {
		t.Error("an unlabelled input and output reported a failure")
	}
}
