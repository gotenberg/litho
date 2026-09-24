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

func TestWriter_RepairFacturX_NoInvoice(t *testing.T) {
	ctx := context.Background()
	data := minimalPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if _, err := w.RepairFacturX(ctx); !errors.Is(err, ErrNoInvoiceAttachment) {
		t.Errorf("err = %v, want ErrNoInvoiceAttachment", err)
	}
}
