package pdf

import (
	"bytes"
	"context"
	"testing"
)

// formPDF builds a minimal PDF with an AcroForm containing two text fields.
func formPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			// Catalog with AcroForm
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>\nendobj\n" +
			// Pages
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			// Page
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R 6 0 R] >>\nendobj\n" +
			// Font
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			// Field 1: name
			"5 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 700 300 720] >>\nendobj\n" +
			// Field 2: email
			"6 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (email) /V (john@example.com) /Rect [100 660 300 680] >>\nendobj\n" +
			// AcroForm
			"7 0 obj\n<< /Fields [5 0 R 6 0 R] >>\nendobj\n" +
			// xref
			"xref\n0 8\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000076 00000 n \r\n" +
			"0000000133 00000 n \r\n" +
			"0000000252 00000 n \r\n" +
			"0000000325 00000 n \r\n" +
			"0000000440 00000 n \r\n" +
			"0000000564 00000 n \r\n" +
			"trailer\n<< /Size 8 /Root 1 0 R >>\n" +
			"startxref\n608\n%%EOF\n")
}

func TestWriter_SetFormField(t *testing.T) {
	data := formPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetFormField(ctx, "name", "Jane Doe")
	w.SetFormField(ctx, "email", "jane@example.com")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	fieldMap, err := r2.GetFormData(ctx)
	if err != nil {
		t.Fatalf("GetFormData: %v", err)
	}

	if fieldMap["name"] != "Jane Doe" {
		t.Errorf("name: got %q, want %q", fieldMap["name"], "Jane Doe")
	}
	if fieldMap["email"] != "jane@example.com" {
		t.Errorf("email: got %q, want %q", fieldMap["email"], "jane@example.com")
	}
}

func TestWriter_SetFormField_NotFound(t *testing.T) {
	data := formPDF()
	ctx := context.Background()

	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	w.SetFormField(ctx, "nonexistent", "value")

	var out bytes.Buffer
	err := w.WriteTo(ctx, &out)
	if err == nil {
		t.Error("expected error for nonexistent field")
	}
}

func TestWriter_FlattenForm(t *testing.T) {
	data := formPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.FlattenForm(ctx)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify AcroForm is removed.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open flattened: %v", err)
	}

	if r2.HasAcroForm(ctx) {
		t.Error("expected AcroForm to be removed after flatten")
	}
}
