package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestXRefTableFormatViolationsMatchTheirClass pins the sentinel class of each
// cross-reference table violation, and the exact message it keeps. Compliance
// picks ISO 19005-1, 6.1.4 t1 or t2 by class; it matched "EOL" and "not
// followed by" in the message before.
//
// White space between the keyword and the header is of the EOL class, as
// veraPDF states it: measured with veraPDF 1.30, LF then a space, LF then a tab
// and CR+LF then a space each fail 6.1.4 t2 alone at PDF/A-1b and PDF/A-2b.
// With surplus EOL markers before it, it is the same single defect.
func TestXRefTableFormatViolationsMatchTheirClass(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		header string
		class  error
		want   string
	}{
		{"no EOL after the keyword", " 0 1\n", ErrXRefEOLMarkers, "compliance: xref keyword not followed by EOL"},
		{"surplus EOL", "\n\n0 1\n", ErrXRefEOLMarkers, "compliance: xref keyword and subsection header separated by multiple EOL markers"},
		{"white space before the header", "\n 0 1\n", ErrXRefEOLMarkers, "compliance: xref keyword and subsection header separated by extra whitespace"},
		{"a tab before the header", "\n\t0 1\n", ErrXRefEOLMarkers, "compliance: xref keyword and subsection header separated by extra whitespace"},
		{"CR+LF then white space", "\r\n 0 1\n", ErrXRefEOLMarkers, "compliance: xref keyword and subsection header separated by extra whitespace"},
		{"surplus EOL then white space", "\n\n 0 1\n", ErrXRefEOLMarkers, "compliance: xref keyword and subsection header separated by multiple EOL markers"},
		{"separator not a space", "\n0\t1\n", ErrXRefSubsectionSpacing, "compliance: xref subsection header separator is not a single space (0x09)"},
		{"wide separator", "\n0  1\n", ErrXRefSubsectionSpacing, "compliance: xref subsection header has 2 spaces between start and count (must be 1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "xref" + tt.header + "0000000000 65535 f \r\ntrailer\n<< /Size 1 >>\n"
			got := (&Document{r: bytes.NewReader([]byte(body))}).ValidateXRefTableFormat(0)
			if len(got) != 1 {
				t.Fatalf("got %d violations, want 1: %v", len(got), got)
			}
			v := got[0]
			if !errors.Is(v, tt.class) || !errors.Is(v, ErrXRefFormat) {
				t.Errorf("%q does not match its class %v under ErrXRefFormat", v, tt.class)
			}
			for _, other := range []error{ErrXRefEOLMarkers, ErrXRefSubsectionSpacing, ErrObjectHeaderFormat} {
				if other != tt.class && errors.Is(v, other) {
					t.Errorf("%q also matches %v", v, other)
				}
			}
			if v.Error() != tt.want {
				t.Errorf("message = %q, want %q", v.Error(), tt.want)
			}
		})
	}
}

// TestObjectHeaderFormatViolationsMatchTheirClass pins that the object header
// and endobj violations match [ErrObjectHeaderFormat] and keep their message.
// Compliance drops them from the parse-time record by that class; it matched
// "obj" and "header" in the message before, which every xref subsection
// message also contains.
func TestObjectHeaderFormatViolationsMatchTheirClass(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	one := buf.Len()
	buf.WriteString("1  0 obj\n<< /Type /Catalog /Pages 2 0 R >>endobj\n")
	two := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 3\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n", one, two)
	fmt.Fprintf(&buf, "trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	data := buf.Bytes()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	got := doc.ValidateObjectHeaderFormat(int64(one), 1)
	want := []string{
		"compliance: obj header 1: 2 white-space characters between object number and generation (must be 1)",
		"compliance: obj 1: endobj not preceded by EOL marker (0x3E)",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d violations, want %d: %v", len(got), len(want), got)
	}
	for i, v := range got {
		if !errors.Is(v, ErrObjectHeaderFormat) {
			t.Errorf("%q does not match ErrObjectHeaderFormat", v)
		}
		if errors.Is(v, ErrXRefFormat) {
			t.Errorf("%q matches ErrXRefFormat", v)
		}
		if v.Error() != want[i] {
			t.Errorf("message = %q, want %q", v.Error(), want[i])
		}
	}
}
