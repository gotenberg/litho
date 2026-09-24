package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// untaggedHeadingPDF is a single untagged page with a heading and a two-line
// body paragraph, so AutoTag has real text structure to synthesize.
func untaggedHeadingPDF() []byte {
	cs := "BT /F1 24 Tf 72 700 Td (Document Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 678 Td (First body line of the paragraph here.) Tj ET\n" +
		"BT /F1 12 Tf 72 664 Td (Second body line continuing it here.) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \r\n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)
	return buf.Bytes()
}

// TestWriter_AutoTag_ResolvesStructureViolations is the end-to-end proof: an
// untagged page fails the PDF/UA structure and content-marking rules; after
// AutoTag and a write, re-validating reports none of them (residuals like
// /Lang, title, and the XMP identifier remain: AutoTag does not own those).
func TestWriter_AutoTag_ResolvesStructureViolations(t *testing.T) {
	ctx := context.Background()
	data := untaggedHeadingPDF()

	r0, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before, err := r0.ValidatePDFUAReport(ctx, models.PDFUA1)
	r0.Close()
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(before): %v", err)
	}
	if !hasStructureViolation(before) {
		t.Skipf("baseline has no structure/content-marking violation to resolve; got:\n%s", describeAll(before))
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.AutoTag()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r.Close()

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("re-open tagged output: %v", err)
	}
	defer r2.Close()
	after, err := r2.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(after): %v", err)
	}
	if hasStructureViolation(after) {
		t.Errorf("AutoTag did not resolve the structure/content-marking violations:\n%s", describeStructureViolations(after))
	}
}

// TestWriter_RemediatePDFUA_AutoTagsStructure proves the remediation chain
// synthesizes a real structure tree. An untagged page fails the PDF/UA structure
// and content-marking rules; after RemediatePDFUA with default options and a
// write, re-validating reports none of them, the report credits the synthesized
// tree, and the page content carries auto-tagging's per-element marks (/H1, /P)
// rather than the content-tag fixer's crude whole-page /Document wrapper.
func TestWriter_RemediatePDFUA_AutoTagsStructure(t *testing.T) {
	ctx := context.Background()
	data := untaggedHeadingPDF()

	r0, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before, err := r0.ValidatePDFUAReport(ctx, models.PDFUA1)
	r0.Close()
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(before): %v", err)
	}
	if !hasStructureViolation(before) {
		t.Skipf("baseline has no structure/content-marking violation to resolve; got:\n%s", describeAll(before))
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.RemediatePDFUA(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r.Close()

	// The fix must come from auto-tagging, not the flat whole-page wrapper.
	var autoTagged bool
	for _, fix := range report.Fixed {
		if strings.Contains(fix, "synthesized structure tree") {
			autoTagged = true
			break
		}
	}
	if !autoTagged {
		t.Errorf("report does not credit a synthesized structure tree; fixed=%v", report.Fixed)
	}

	// The written content carries per-element marks and not the content-tag
	// fixer's whole-page /Document wrapper, proving it deferred (no double-wrap).
	outStr := out.String()
	if !strings.Contains(outStr, "/H1 <</MCID") || !strings.Contains(outStr, "/P <</MCID") {
		t.Errorf("per-element auto-tag marks missing from remediated content")
	}
	if strings.Contains(outStr, "/Document <</MCID") {
		t.Errorf("content-tag whole-page wrapper present; auto-tag did not supersede it")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("re-open remediated output: %v", err)
	}
	defer r2.Close()
	after, err := r2.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(after): %v", err)
	}
	if hasStructureViolation(after) {
		t.Errorf("RemediatePDFUA did not resolve the structure/content-marking violations:\n%s", describeStructureViolations(after))
	}
}

func hasStructureViolation(vs []Violation) bool {
	return slices.ContainsFunc(vs, isStructureViolation)
}

func isStructureViolation(v Violation) bool {
	d := strings.ToLower(v.Description + " " + v.Context)
	return strings.Contains(d, "structtreeroot") ||
		strings.Contains(d, "marked") ||
		strings.Contains(d, "artifact") ||
		strings.Contains(d, "/markinfo") ||
		strings.Contains(d, "tagged")
}

func describeStructureViolations(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		if isStructureViolation(v) {
			fmt.Fprintf(&b, "  clause %s test %d: %s\n", v.Clause, v.TestNumber, v.Description)
		}
	}
	return b.String()
}

func describeAll(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "  clause %s test %d: %s\n", v.Clause, v.TestNumber, v.Description)
	}
	return b.String()
}
