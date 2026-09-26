package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// defectiveInvoicePDF embeds factur-x.xml with every container defect
// RepairFacturX handles: no catalog /AF, no /AFRelationship, /F without
// /UF, wrong stream /Subtype.
func defectiveInvoicePDF() []byte {
	return defectiveInvoicePDFWith("<x/>\n")
}

// defectiveInvoicePDFWith is defectiveInvoicePDF with payload as the invoice.
func defectiveInvoicePDFWith(payload string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(factur-x.xml) 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (factur-x.xml) /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Type /EmbeddedFile /Subtype /application#2Foctet-stream /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
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

func TestWriter_RepairFacturX_RoundTrip(t *testing.T) {
	ctx := context.Background()
	data := defectiveInvoicePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	report, err := w.RepairFacturX(ctx)
	if err != nil {
		t.Fatalf("RepairFacturX: %v", err)
	}
	if len(report.Fixed) == 0 {
		t.Fatal("expected container fixes")
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()

	errs, err := r2.ValidateFacturX(ctx)
	if err != nil {
		t.Fatalf("ValidateFacturX: %v", err)
	}
	for _, e := range errs {
		msg := e.Error()
		// Exact contexts of the three repaired defects; other rules (the
		// PDF/A baseline, fx: XMP) legitimately remain.
		for _, fixed := range []string{"catalog has no /AF array", "missing /AFRelationship", "/Subtype = "} {
			if strings.Contains(msg, fixed) {
				t.Errorf("container violation survived repair: %v", e)
			}
		}
	}
}

// TestWriter_RepairFacturX_NoInvoice also pins that a remediation that fails
// ends nothing: the Writer drops the file it wrote the queued edits to, keeps
// them queued, and accepts further edits, which name the same pages.
func TestWriter_RepairFacturX_NoInvoice(t *testing.T) {
	dir := isolateTempDir(t)
	ctx := context.Background()
	data := minimalPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	defer w.Close()
	w.SetMetadata(Metadata{Title: "Queued before"})
	if _, err := w.RepairFacturX(ctx); !errors.Is(err, ErrNoInvoiceAttachment) {
		t.Errorf("err = %v, want ErrNoInvoiceAttachment", err)
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("the failed repair left %q behind", got)
	}
	if err := w.RotatePages(90, 1); err != nil {
		t.Fatalf("RotatePages after the failed repair: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := rotationsOf(t, out.Bytes()); len(got) != 1 || got[0] != 90 {
		t.Errorf("the output pages have rotations %v, want [90]", got)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()
	meta, err := got.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Queued before" {
		t.Errorf("the output /Title is %q, want the edit queued before the repair", meta["Title"])
	}
}
