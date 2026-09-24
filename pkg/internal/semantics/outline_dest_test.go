package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// actionInDestPDF builds a two-page document whose outline items put a /GoTo
// action dictionary in /Dest instead of a destination array.
//
// ISO 32000-1, Table 153 allows an array, a name or a byte string there, and an
// action belongs in /A. Producers write the action anyway.
//
// # Corpus evidence
//
//   - govdocs thread_300_300865.pdf: the shape this fixture reproduces.
func actionInDestPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Outlines /First 8 0 R /Last 9 0 R /Count 2 >>",
		"<< /Title (via action) /Parent 7 0 R /Next 9 0 R" +
			" /Dest << /Type /Action /S /GoTo /D [4 0 R /Fit] >> >>",
		"<< /Title (via array) /Parent 7 0 R /Prev 8 0 R /Dest [3 0 R /Fit] >>",
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

// TestOutlineFollowsAnActionInDest pins that a bookmark still resolves when the
// producer wraps its destination in an action dictionary.
//
// litho accepted only an array, a name or a byte string, so such outlines
// reported no page at all for every single bookmark: `litho bookmarks` printed
// page 0 throughout while every other reader resolved them.
//
// # Corpus evidence
//
//   - govdocs: four outlines reported no page for every bookmark.
func TestOutlineFollowsAnActionInDest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := actionInDestPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	marks, err := ResolveOutline(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveOutline: %v", err)
	}
	if len(marks) != 2 {
		t.Fatalf("resolved %d bookmarks, want 2", len(marks))
	}
	if marks[0].PageNumber != 2 {
		t.Errorf("the action-wrapped destination resolved to page %d, want 2", marks[0].PageNumber)
	}
	// The control: an ordinary array destination must still work.
	if marks[1].PageNumber != 1 {
		t.Errorf("the plain array destination resolved to page %d, want 1", marks[1].PageNumber)
	}
}
