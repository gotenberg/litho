package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestValidateEndobjFormat_SupersededObjectBytes pins that the endobj format
// check judges only the object it was given.
//
// The scan window ends at the next offset the cross-reference table lists, and
// an incremental update can leave that far past the object: a superseded
// definition keeps its bytes in the file but loses its xref entry, so the
// window runs on through it and the backward scan finds the dead object's
// "endobj".
//
// # Corpus evidence
//
//   - pdfjs issue15096.pdf: supersedes object 264, whose abandoned copy ends
//     "endobj " (a space, not an EOL), and litho reported that against object
//     263 under ISO 19005-2 §6.1.9 t1. veraPDF, which reads only the object in
//     use, reports nothing.
func TestValidateEndobjFormat_SupersededObjectBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 2 is defined twice: an abandoned copy whose endobj is followed by
	// a space, then the copy the xref points at, which is well formed.
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := map[int]int{}

	offs[1] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 3 0 R >>\nendobj\n")

	// The superseded copy of object 2. Its offset is deliberately not recorded.
	buf.WriteString("2 0 obj\n<< /Superseded true >>\nendobj \n")

	offs[2] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Superseded false >>\nendobj\n")

	offs[3] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 4\n0000000000 65535 f\r\n")
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

	data := buf.Bytes()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	// Object 1's window reaches the superseded copy of object 2, because the
	// next listed offset is the live copy well past it.
	if v := doc.validateEndobjFormat(int64(offs[1]), 1); len(v) != 0 {
		t.Errorf("object 1 reported %v; the defect belongs to a superseded object 2", v)
	}
	if v := doc.validateEndobjFormat(int64(offs[2]), 2); len(v) != 0 {
		t.Errorf("the live object 2 is well formed, got %v", v)
	}
}

// TestValidateEndobjFormat_StillReportsOwnDefect pins that narrowing the window
// did not silence the rule: an object whose own endobj lacks its EOL marker is
// still reported (ISO 19005-2 §6.1.9 t1, ISO 32000-1 7.5.5).
func TestValidateEndobjFormat_StillReportsOwnDefect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	one := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj \n")
	two := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 3\n0000000000 65535 f\r\n%010d 00000 n\r\n%010d 00000 n\r\n", one, two)
	fmt.Fprintf(&buf, "trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

	data := buf.Bytes()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	v := doc.validateEndobjFormat(int64(one), 1)
	if len(v) != 1 || !strings.Contains(v[0].Error(), "not followed by EOL") {
		t.Errorf("violations = %v, want the object's own missing EOL", v)
	}
}

// TestNextObjectHeader pins the window bound itself.
func TestNextObjectHeader(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data string
		want bool // whether a following header is found
	}{
		{"one object only", "1 0 obj\n<< >>\nendobj\n", false},
		{"a following header ends the window", "1 0 obj\n<< >>\nendobj\n2 0 obj\n<< >>\nendobj\n", true},
		{"a lax producer's spaces are stepped over", "1 0 obj\n<< >>\nendobj \r\n12 0 obj\n<< >>\n", true},
		// Digits inside stream data must not end the window: only a header that
		// follows an endobj counts.
		{"digits in stream data do not", "1 0 obj\n<< >>\nstream\n9 0 obj junk\nendstream\nendobj\n", false},
		{"a header with no preceding endobj does not", "1 0 obj\n<< >>\n2 0 obj\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nextObjectHeader([]byte(tc.data)) > 0; got != tc.want {
				t.Errorf("nextObjectHeader found=%v, want %v", got, tc.want)
			}
		})
	}
}
