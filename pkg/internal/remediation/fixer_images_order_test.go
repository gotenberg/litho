package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// twoInterpolatedImagesPDF builds a one-page document with two image XObjects
// that both need the same repair, so the fixer produces one note per image.
//
// The source text declares /ImB before /ImA on purpose, so sorted order is not
// the order the file lists them in. A parsed dictionary is a Go map and carries
// no insertion order to compare against, but writing the names this way keeps
// the expected result from coinciding with the order a reader of the fixture
// would assume.
func twoInterpolatedImagesPDF(t *testing.T) []byte {
	t.Helper()

	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	if _, err := zw.Write([]byte{0, 255, 128, 64}); err != nil {
		t.Fatalf("deflating pixels: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	pixels := deflated.Bytes()

	const content = "q 100 0 0 100 50 600 cm /ImA Do Q q 100 0 0 100 50 400 cm /ImB Do Q\n"

	// Both images are byte-identical: the test is about the order the notes
	// come back in, not about telling the two pictures apart.
	image := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray"+
		" /BitsPerComponent 8 /Interpolate true /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
		len(pixels), pixels)

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /XObject << /ImB 6 0 R /ImA 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		image,
		image,
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

// The fix descriptions name the image they describe, and they reach the caller
// in the order the fixer produced them: `litho remediate` prints them as lines
// and serialises them as the JSON "fixed" array, neither of which is sorted
// downstream. Ranging the /XObject dictionary directly would therefore hand two
// different orders to two identical runs.
//
// Repeated rather than run once on purpose. Go randomises map iteration, so a
// single pass over two keys has an even chance of matching by luck; a mutation
// that removed the sort would survive such a test half the time. Twenty passes
// reduce that to about one in a million.
func TestFixImageIssues_NoteOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := twoInterpolatedImagesPDF(t)

	const runs = 20
	var first []string

	for i := range runs {
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("run %d: NewDocument: %v", i, err)
		}
		alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

		notes, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, "")
		if err != nil {
			t.Fatalf("run %d: fixImageIssues: %v", i, err)
		}
		if len(notes) != 2 {
			t.Fatalf("run %d: got %d notes, want 2: both images carry /Interpolate true", i, len(notes))
		}

		if i == 0 {
			first = notes
			// ImA before ImB, which is sorted order rather than the order the
			// /XObject dictionary declares them (ImB is written first).
			if !strings.Contains(notes[0], "ImA") || !strings.Contains(notes[1], "ImB") {
				t.Fatalf("notes = %q, want the /ImA note first: the dict declares /ImB first, so this is sorted order", notes)
			}
			continue
		}
		for n := range notes {
			if notes[n] != first[n] {
				t.Fatalf("run %d note %d = %q, want %q: the order varies between identical runs",
					i, n, notes[n], first[n])
			}
		}
	}
}
