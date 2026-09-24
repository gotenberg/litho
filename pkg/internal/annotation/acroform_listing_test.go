package annotation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// partlyFilledFormPDF builds a one-page document whose interactive form has one
// checkbox carrying a value and two text fields nobody filled in.
func partlyFilledFormPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (form) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [7 0 R 8 0 R 9 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /Annots [7 0 R 8 0 R 9 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (checked) /V /Yes /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (empty one) /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (empty two) /Rect [0 0 10 10] >>",
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

func openForm(t *testing.T) *document.Document {
	t.Helper()

	data := partlyFilledFormPDF(t)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestExtractFormFieldsListsUnfilledFields pins that listing a form reports the
// fields nobody filled in.
//
// [ExtractFormData] records a field only when it carries a /V, which is right
// for "what did the user type" and wrong for "what fields are there": it hid
// the unfilled fields from `litho forms`, and left no way to discover the
// names to pass to set-form.
//
// # Corpus evidence
//
//   - pdfjs issue14301.pdf: has 660 named fields, and `litho forms` printed
//     "(no form fields)" for it, because only its 320 checkboxes hold a
//     value.
func TestExtractFormFieldsListsUnfilledFields(t *testing.T) {
	t.Parallel()

	fields, err := ExtractFormFields(context.Background(), openForm(t))
	if err != nil {
		t.Fatalf("ExtractFormFields: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("listed %d fields, want 3: the two unfilled text fields belong in the list", len(fields))
	}

	byName := make(map[string]string, len(fields))
	for _, f := range fields {
		byName[f.Name] = f.Value
	}
	if byName["checked"] != "Yes" {
		t.Errorf("checked = %q, want %q", byName["checked"], "Yes")
	}
	for _, name := range []string{"empty one", "empty two"} {
		v, listed := byName[name]
		if !listed {
			t.Errorf("field %q is missing from the listing", name)
		} else if v != "" {
			t.Errorf("field %q has value %q, want empty", name, v)
		}
	}
}

// TestExtractFormDataKeepsOnlyFilledFields is the control: the "what did the
// user type" answer must not gain empty entries.
func TestExtractFormDataKeepsOnlyFilledFields(t *testing.T) {
	t.Parallel()

	fields, err := ExtractFormData(context.Background(), openForm(t))
	if err != nil {
		t.Fatalf("ExtractFormData: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("reported %d fields, want 1: only the checkbox carries a value", len(fields))
	}
	if fields[0].Name != "checked" || fields[0].Value != "Yes" {
		t.Errorf("got %+v, want the checked field", fields[0])
	}
}

// TestFormListingOrderIsStable pins that the listing does not depend on Go map
// iteration order, which the writer's determinism rule forbids and which made
// repeated `litho forms` runs disagree with each other.
func TestFormListingOrderIsStable(t *testing.T) {
	t.Parallel()

	var first []string
	for range 8 {
		fields, err := ExtractFormFields(context.Background(), openForm(t))
		if err != nil {
			t.Fatalf("ExtractFormFields: %v", err)
		}
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}
		if first == nil {
			first = names
			continue
		}
		for i := range names {
			if names[i] != first[i] {
				t.Fatalf("field order changed between runs: %v then %v", first, names)
			}
		}
	}
}
