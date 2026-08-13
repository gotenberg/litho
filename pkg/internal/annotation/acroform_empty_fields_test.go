package annotation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// acroFormPDF assembles a one-page document whose catalog carries the given
// interactive form dictionary verbatim.
func acroFormPDF(acroForm string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm " + acroForm + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	}

	var buf []byte
	buf = append(buf, "%PDF-1.7\n"...)
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = len(buf)
		buf = append(buf, fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body)...)
	}
	start := len(buf)
	buf = append(buf, fmt.Sprintf("xref\n0 %d\n", len(objs)+1)...)
	buf = append(buf, "0000000000 65535 f \n"...)
	for i := 1; i <= len(objs); i++ {
		buf = append(buf, fmt.Sprintf("%010d 00000 n \n", offsets[i])...)
	}
	buf = append(buf, fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)...)
	return buf
}

func acroFormDoc(t *testing.T, acroForm string) *document.Document {
	t.Helper()
	data := acroFormPDF(acroForm)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestEmptyFieldsArrayIsNotAnError pins that an interactive form declaring no
// root fields reports no fields instead of failing.
//
// ISO 32000-1, Table 218 permits an empty /Fields: a form may carry only /DA,
// /DR or /NeedAppearances. An empty array resolves to a nil slice, which the
// code rejected as "fields is not an array", so `litho forms` failed outright on
// a conformant document.
func TestEmptyFieldsArrayIsNotAnError(t *testing.T) {
	t.Parallel()

	fields, err := ExtractFormData(context.Background(), acroFormDoc(t, "<< /Fields [] /NeedAppearances true >>"))
	if err != nil {
		t.Fatalf("an empty /Fields array must not be an error, got: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("fields = %v, want none", fields)
	}
}

// TestMissingFieldsKeyIsNotAnError keeps the neighbouring case covered: a form
// with no /Fields entry at all is equally valid.
func TestMissingFieldsKeyIsNotAnError(t *testing.T) {
	t.Parallel()

	fields, err := ExtractFormData(context.Background(), acroFormDoc(t, "<< /NeedAppearances true >>"))
	if err != nil {
		t.Fatalf("a form without /Fields must not be an error, got: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("fields = %v, want none", fields)
	}
}

// TestNonArrayFieldsStillReportsTheTypeError checks the leniency is bounded:
// /Fields holding a name is malformed and must still surface, not be silently
// reported as an empty form.
func TestNonArrayFieldsStillReportsTheTypeError(t *testing.T) {
	t.Parallel()

	_, err := ExtractFormData(context.Background(), acroFormDoc(t, "<< /Fields /NotAnArray >>"))
	if err == nil {
		t.Fatal("a non-array /Fields must report an error")
	}
	if !strings.Contains(err.Error(), "expected PdfArray") {
		t.Errorf("error = %v, want it to name the type mismatch", err)
	}
}
