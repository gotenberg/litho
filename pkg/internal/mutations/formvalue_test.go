package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// buttonAndTextFormPDF builds a one-page document carrying one button field and one text
// field, each merged with its widget the way ISO 32000-1, 12.5.6.19 allows.
func buttonAndTextFormPDF() []byte {
	body := "%PDF-1.7\n"
	offs := map[int]int{}
	add := func(num int, payload string) {
		offs[num] = len(body)
		body += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", num, payload)
	}
	add(1, "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>")
	add(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	add(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R 5 0 R] >>")
	add(4, "<< /Type /Annot /Subtype /Widget /FT /Btn /T (check) /V /Off /AS /Off "+
		"/AP << /N << /On 6 0 R /Off 6 0 R >> >> /Rect [0 0 20 20] >>")
	add(5, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (old) /Rect [0 30 100 50] >>")
	add(6, "<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 0 >>\nstream\n\nendstream")

	xrefOffset := len(body)
	var table strings.Builder
	table.WriteString("xref\n0 7\n0000000000 65535 f \n")
	for num := 1; num <= 6; num++ {
		fmt.Fprintf(&table, "%010d 00000 n \n", offs[num])
	}

	return []byte(body + table.String() +
		fmt.Sprintf("trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset))
}

func setValue(t *testing.T, field, value string) ast.PdfDict {
	t.Helper()
	ctx := context.Background()
	data := buttonAndTextFormPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := SetFormField(ctx, alloc, doc, field, value); err != nil {
		t.Fatalf("SetFormField(%q, %q): %v", field, value, err)
	}
	for _, obj := range alloc.Pending() {
		dict, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		if name, _ := dict["T"].(ast.PdfString); string(name) == field {
			return dict
		}
	}
	t.Fatalf("SetFormField did not write field %q", field)
	return nil
}

// TestSetFormFieldButtonValueIsAName pins the value representation a button
// field requires. ISO 32000-1, 12.7.4.2.3 makes it the name of an appearance
// state, never a text string. Writing a string produced a checkbox litho's own
// form reader then refused to list at all, hiding every other field with it.
func TestSetFormFieldButtonValueIsAName(t *testing.T) {
	dict := setValue(t, "check", "On")

	got, ok := dict["V"].(ast.PdfName)
	if !ok {
		t.Fatalf("button /V is %T (%v), want ast.PdfName", dict["V"], dict["V"])
	}
	if got != "On" {
		t.Errorf("button /V = %q, want \"On\"", got)
	}
}

// TestSetFormFieldButtonUpdatesAppearanceState checks /AS follows /V. /AS picks
// which appearance in /AP is drawn (ISO 32000-1, 12.5.5), so leaving it behind
// renders the previous state whatever /V says.
func TestSetFormFieldButtonUpdatesAppearanceState(t *testing.T) {
	dict := setValue(t, "check", "On")

	got, ok := dict["AS"].(ast.PdfName)
	if !ok {
		t.Fatalf("button /AS is %T (%v), want ast.PdfName", dict["AS"], dict["AS"])
	}
	if got != "On" {
		t.Errorf("button /AS = %q, want \"On\" to match /V", got)
	}
}

// TestSetFormFieldTextValueStaysAString checks the button rule did not spread to
// text fields, whose /V is a text string (ISO 32000-1, Table 228).
func TestSetFormFieldTextValueStaysAString(t *testing.T) {
	dict := setValue(t, "name", "hello")

	got, ok := dict["V"].(ast.PdfString)
	if !ok {
		t.Fatalf("text /V is %T (%v), want ast.PdfString", dict["V"], dict["V"])
	}
	if document.DecodePdfString(got) != "hello" {
		t.Errorf("text /V = %q, want \"hello\"", document.DecodePdfString(got))
	}
}
