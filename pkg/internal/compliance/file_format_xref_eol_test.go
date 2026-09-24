package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// xrefHeaderDoc builds a two-object document whose cross-reference table
// starts with "xref" followed by head, then the "0 3" subsection header.
func xrefHeaderDoc(t *testing.T, head string) *document.Document {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	one := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	two := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref%s0 3\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n", head, one, two)
	fmt.Fprintf(&buf, "trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestXRefHeaderWhiteSpaceIsAnEOLDefect pins that white space between the xref
// keyword and the subsection header answers to the EOL marker test, ISO
// 19005-1, 6.1.4 t2 (ISO 19005-2, 6.1.4 t2), not to the subsection header
// spacing of ISO 19005-1, 6.1.4 t1, which parts 2 and up dropped. veraPDF
// reads one EOL marker after the keyword and fails xrefEOLMarkersComplyPDFA
// when no digit follows it: measured with veraPDF 1.30, each of these tables
// fails 6.1.4 t2 alone at PDF/A-1b and PDF/A-2b.
func TestXRefHeaderWhiteSpaceIsAnEOLDefect(t *testing.T) {
	t.Parallel()

	for _, head := range []string{"\n ", "\n\t", "\r\n ", "\n\n "} {
		for _, tc := range []struct {
			profile string
			want    Rule
		}{
			{"PDF/A-1b", Rule_19005_1_6_1_4_t02},
			{"PDF/A-2b", Rule_19005_2_6_1_4_t02},
		} {
			t.Run(fmt.Sprintf("%q at %s", head, tc.profile), func(t *testing.T) {
				t.Parallel()
				got := checkXRefTableByteFormat(xrefHeaderDoc(t, head), tc.profile)
				if len(got) != 1 || got[0].Rule != tc.want {
					t.Fatalf("got %v, want one %s test %d", got, tc.want.Clause, tc.want.TestNumber)
				}
			})
		}
	}

	// The control: a single EOL marker then the header states nothing.
	for _, profile := range []string{"PDF/A-1b", "PDF/A-2b"} {
		if got := checkXRefTableByteFormat(xrefHeaderDoc(t, "\n"), profile); len(got) != 0 {
			t.Errorf("a well-formed table at %s: got %v, want nothing", profile, got)
		}
	}
}
