package annotation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// widgetFormPDF builds a document with one filled text widget on the page. When
// registered is false the interactive form does not list it, and when there is
// no form dictionary at all the catalog omits /AcroForm entirely.
func widgetFormPDF(registered, hasAcroForm bool) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	switch {
	case hasAcroForm && registered:
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>"
	case hasAcroForm:
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] >> >>"
	}

	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (username) /V (alice) /Rect [0 0 10 10] >>",
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

func fieldsOf(t *testing.T, data []byte) []models_FormField {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	got, err := ExtractFormData(ctx, doc)
	if err != nil {
		t.Fatalf("ExtractFormData: %v", err)
	}
	out := make([]models_FormField, len(got))
	for i, f := range got {
		out[i] = models_FormField{Name: f.Name, Value: f.Value}
	}
	return out
}

// models_FormField mirrors the two fields these tests assert on, so the
// assertions stay readable.
type models_FormField struct {
	Name  string
	Value string
}

// TestFieldFoundWhenOnlyOnThePage pins that a widget the interactive form does
// not list is still reported.
//
// ISO 32000-1, 12.7.2 puts fields in /AcroForm and 12.5.6.19 puts the widget on
// the page. Producers write the widget without registering the field, and
// `litho forms` reported no such widget.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: carries 257 such widgets, and `litho forms`
//     reported none of them.
//   - pdfjs issue14203.pdf, issue15597.pdf, issue17069.pdf, bug1723568.pdf:
//     `litho forms` reported none of their unregistered widgets either.
func TestFieldFoundWhenOnlyOnThePage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		hasAcroForm bool
	}{
		{"empty /Fields", true},
		{"no /AcroForm at all", false},
	} {
		got := fieldsOf(t, widgetFormPDF(false, tc.hasAcroForm))
		if len(got) != 1 {
			t.Errorf("%s: found %d fields %v, want 1", tc.name, len(got), got)
			continue
		}
		if got[0].Name != "username" || got[0].Value != "alice" {
			t.Errorf("%s: got %+v, want username=alice", tc.name, got[0])
		}
	}
}

// TestRegisteredFieldReportedOnce pins that a field listed in the form and
// present on the page is not counted twice.
func TestRegisteredFieldReportedOnce(t *testing.T) {
	t.Parallel()

	got := fieldsOf(t, widgetFormPDF(true, true))
	if len(got) != 1 {
		t.Errorf("found %d fields %v, want 1: the page walk duplicated the registered field",
			len(got), got)
	}
}

// TestNonFieldWidgetIsIgnored is the control: an annotation that names no field
// type is decoration, not a form field.
func TestNonFieldWidgetIsIgnored(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /T (no-type) /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] >>",
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

	if got := fieldsOf(t, buf.Bytes()); len(got) != 0 {
		t.Errorf("found %d fields %v, want 0", len(got), got)
	}
}

// TestWidgetSharingAParentFieldReportedOnce pins that several widgets
// presenting one field report that field once, which is why the walk reports
// the field a widget belongs to rather than the widget.
func TestWidgetSharingAParentFieldReportedOnce(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [0 20 10 30] >>",
		"<< /FT /Tx /T (shared) /V (once) /Kids [4 0 R 5 0 R] >>",
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

	got := fieldsOf(t, buf.Bytes())
	if len(got) != 1 {
		t.Fatalf("found %d fields %v, want 1: two widgets present one field", len(got), got)
	}
	if got[0].Name != "shared" || got[0].Value != "once" {
		t.Errorf("got %+v, want shared=once", got[0])
	}
}
