package annotation

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// multiSelectFormPDF builds a document whose interactive form holds one
// single-selection list box and one multi-selection list box, the latter with
// an array /V. The third field selects nothing.
func multiSelectFormPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R 7 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Ch /T (single) /Rect [0 0 10 10]" +
			" /Opt [(Alpha) (Beta)] /V (Beta) >>",
		"<< /Type /Annot /Subtype /Widget /FT /Ch /T (multi) /Rect [0 0 10 10] /Ff 2097152" +
			" /Opt [(Alpha) (Beta) (Gamma)] /V [(Alpha) (Gamma)] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Ch /T (none) /Rect [0 0 10 10] /Opt [(Alpha)] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Ch /T (indirect) /Rect [0 0 10 10]" +
			" /Opt [(Alpha) (Beta)] /V [8 0 R (Beta)] >>",
		"(Alpha)",
	}
	return assembleFormPDF(objs)
}

func assembleFormPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestMultiSelectChoiceFieldValue pins that a list box allowing multiple
// selection reports its selections.
//
// Per ISO 32000-1, 12.7.4.4, Table 230 a choice field's /V is a text string, or
// an array of them when more than one option is selected. Only the scalar forms
// were read, so a multi-select list box reported an empty value while pikepdf
// and every viewer show its selections.
//
// # Corpus evidence
//
//   - pdfjs annotation-choice-widget.pdf: holds
//     ["Ipsum" "Amet" "Consectetur" "Elit"], and litho reported an empty
//     value for that field while pikepdf and every viewer show four
//     selections.
func TestMultiSelectChoiceFieldValue(t *testing.T) {
	t.Parallel()

	data := multiSelectFormPDF()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	fields, err := ExtractFormFields(context.Background(), doc)
	if err != nil {
		t.Fatalf("ExtractFormFields: %v", err)
	}

	got := make(map[string]string, len(fields))
	for _, f := range fields {
		got[f.Name] = f.Value
	}
	for name, want := range map[string]string{
		"single":   "Beta",
		"multi":    "Alpha, Gamma",
		"none":     "",
		"indirect": "Alpha, Beta",
	} {
		if got[name] != want {
			t.Errorf("field %q value = %q, want %q", name, got[name], want)
		}
	}
}

// TestCheckboxValueIsAlwaysValidUTF8 pins that a field value written as a name
// reaches the caller as a usable string.
//
// A name is a byte sequence (ISO 32000-1, 7.3.5), and a producer can give a
// checkbox a UTF-16BE one. Returning the raw bytes put a string that is not
// valid UTF-8 into models.FormField, and the JSON encoder rendered it as
// replacement characters. ASCII appearance states must stay byte-identical, so
// they keep matching the /AP key that names them.
//
// # Corpus evidence
//
//   - pdfjs issue19753.pdf: gives a checkbox a UTF-16BE name.
func TestCheckboxValueIsAlwaysValidUTF8(t *testing.T) {
	t.Parallel()

	data := assembleFormPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (ascii) /Rect [0 0 10 10] /V /Yes >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (off) /Rect [0 0 10 10] /V /Off >>",
		// /V is the UTF-16BE encoding of "はい", written with #-escapes.
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (utf16) /Rect [0 0 10 10]" +
			" /V /#FE#FF#30#6F#30#44 >>",
	})

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	fields, err := ExtractFormFields(context.Background(), doc)
	if err != nil {
		t.Fatalf("ExtractFormFields: %v", err)
	}

	got := make(map[string]string, len(fields))
	for _, f := range fields {
		if !utf8.ValidString(f.Value) {
			t.Errorf("field %q value is not valid UTF-8: %q", f.Name, f.Value)
		}
		got[f.Name] = f.Value
	}
	for name, want := range map[string]string{
		"ascii": "Yes",
		"off":   "Off",
		"utf16": "はい",
	} {
		if got[name] != want {
			t.Errorf("field %q value = %q, want %q", name, got[name], want)
		}
	}
}
