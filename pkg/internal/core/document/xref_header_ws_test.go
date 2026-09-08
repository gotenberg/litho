package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestValidateObjectHeaderFormat_WhiteSpaceSeparator pins that any single
// white-space character separates an object header's parts.
//
// ISO 19005-2 §6.1.9 t1 and ISO 19005-4 §6.1.8 t1 ask for "a single white-space
// character" between the object number, the generation number and the obj
// keyword, and ISO 32000-1, Table 1 names six of them. litho required a space,
// so pdfjs colors.pdf, which writes every header as "3\n0\nobj", was reported
// twice per object where veraPDF reports nothing.
func TestValidateObjectHeaderFormat_WhiteSpaceSeparator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// violations returns the header findings for the object written with the
	// given separators between its number, generation and obj keyword.
	violations := func(t *testing.T, sep1, sep2 string) []error {
		t.Helper()
		var buf bytes.Buffer
		buf.WriteString("%PDF-1.7\n")
		one := buf.Len()
		fmt.Fprintf(&buf, "1%s0%sobj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n", sep1, sep2)
		two := buf.Len()
		buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
		xref := buf.Len()
		fmt.Fprintf(&buf, "xref\n0 3\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n", one, two)
		fmt.Fprintf(&buf, "trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

		data := buf.Bytes()
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		return doc.validateObjectHeaderFormat(int64(one), 1)
	}

	cases := []struct {
		name       string
		sep1, sep2 string
		want       bool // whether a violation is expected
	}{
		{"a space separates", " ", " ", false},
		{"a line feed separates", "\n", "\n", false},
		{"a carriage return separates", "\r", "\r", false},
		{"a tab separates", "\t", "\t", false},
		{"two white-space characters do not", "  ", " ", true},
		{"CR LF is two characters and does not", "\r\n", " ", true},
		{"a non-white-space byte does not", "x", " ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := violations(t, tc.sep1, tc.sep2)
			if (len(got) > 0) != tc.want {
				t.Errorf("violations = %v, want any = %v", got, tc.want)
			}
		})
	}
}

// TestValidateObjectHeaderFormat_StillReportsRealDefects pins that the loosened
// separator did not silence the rest of the header rule.
func TestValidateObjectHeaderFormat_StillReportsRealDefects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	one := buf.Len()
	// The obj keyword must be followed by an EOL marker, not a space.
	buf.WriteString("1 0 obj << /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	two := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 3\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n", one, two)
	fmt.Fprintf(&buf, "trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

	data := buf.Bytes()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	got := doc.validateObjectHeaderFormat(int64(one), 1)
	if len(got) == 0 {
		t.Fatal("obj keyword followed by a space must still be reported")
	}
	if !strings.Contains(got[0].Error(), "obj keyword not followed by EOL") {
		t.Errorf("violation = %v, want the missing EOL after obj", got[0])
	}
}
