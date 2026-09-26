package tagging

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// formCyclePDF builds a page whose resources list two forms whose own
// resources list each other. /X1 is form A, which lists B and C and draws C;
// /X2 is form B, which lists A and draws it; C shows text. The page draws only
// /X2, so its text reaches the page through B, A and C, and neither A nor B
// shows text of its own.
//
// No form invokes itself, which ISO 32000-1, 8.10.1 forbids: A lists B without
// drawing it. The resources still form a cycle, and the scan follows the forms
// a form's resources list.
func formCyclePDF() []byte {
	a := "/C Do"
	b := "/A Do"
	c := "BT /F1 9 Tf 0 0 Td (Louise Anne Farrar) Tj ET"
	page := "BT /F1 9 Tf 0 20 Td (Header) Tj ET /X2 Do"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 8 0 R >> /XObject << /X1 5 0 R /X2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page)+1, page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /XObject << /B 6 0 R /C 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(a)+1, a),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /XObject << /A 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(b)+1, b),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+
			"/Resources << /Font << /F1 8 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(c)+1, c),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return buf.Bytes()
}

// TestTextBearingXObjects_CycleIndependentOfOrder pins that the forms judged to
// paint text do not depend on the order the resource dictionaries are walked
// in (CONTRIBUTING "Deterministic output").
//
// The scan memoised a form's "no text" answer even when a form on the current
// path had cut its search short: reached from A, B met A on the path, took no
// text from it, and was recorded as decoration although it draws C's text
// through A. Whether that happened depended on Go's map order, whether the
// page's /X1 came before /X2 and B before C in A's resources. Go does not
// start the walk of a small map at a uniformly random entry, so about three
// runs in four swept /X2, and the name it draws, into an /Artifact.
func TestTextBearingXObjects_CycleIndependentOfOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := formCyclePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	// Each run draws a fresh map order, and the orders the scan failed on come
	// up in most of them, so 200 runs cannot all miss them.
	const runs = 200
	for i := range runs {
		got := textBearingXObjects(ctx, nil, doc, pages[0])
		if !got["X1"] || !got["X2"] || len(got) != 2 {
			t.Fatalf("run %d: text-bearing forms %v, want X1 and X2: both reach C's text, "+
				"and the answer must not depend on the order the resources are walked in", i, got)
		}
	}
}
