package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// formCyclePDF builds an untagged page whose resources list two forms whose own
// resources list each other: /X1 is form A, which lists B and C and draws C;
// /X2 is form B, which lists A and draws it; C shows text. The page shows a
// line of text and then draws /X2, so auto-tagging decides whether the region
// drawing /X2, and the name C shows through it, may become an /Artifact.
func formCyclePDF() []byte {
	a := "/C Do"
	b := "/A Do"
	c := "BT /F1 9 Tf 0 0 Td (Louise Anne Farrar) Tj ET"
	page := "BT /F1 9 Tf 0 20 Td (Header) Tj ET /X2 Do"
	form := func(resources, body string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << %s >> /Length %d >>\nstream\n%s\nendstream",
			resources, len(body)+1, body)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 8 0 R >> /XObject << /X1 5 0 R /X2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page)+1, page),
		form("/XObject << /B 6 0 R /C 7 0 R >>", a),
		form("/XObject << /A 5 0 R >>", b),
		form("/Font << /F1 8 0 R >>", c),
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

// TestRemediation_FormCycleWritesTheSameBytes pins that remediating the same
// input with the /ID seam pinned writes the same bytes every run, on a file
// whose forms list each other back through their resources ([formCyclePDF])
// (CONTRIBUTING "Deterministic output").
//
// Auto-tagging, which PDF/UA-1 and PDF/A-2a remediation run on an untagged
// file, keeps a form that paints text out of an /Artifact. Its scan cached a
// "no text" for B when it met B from A, with A still on its path, and whether
// it met B that way depended on Go's map order. About three runs in four swept
// /X2, and the name it draws, into an /Artifact, and the integrity probe then
// refused the remediation with [ErrRemediationBrokePDF], since the name no
// longer extracted; the other runs wrote the tagged file. Each run draws a
// fresh map order, so 16 runs that all agree by chance happen about once in a
// hundred on the scan that depended on it.
func TestRemediation_FormCycleWritesTheSameBytes(t *testing.T) {
	t.Parallel()

	data := formCyclePDF()
	ctx := context.Background()
	remediations := []struct {
		name string
		run  func(*Writer) (*RemediationReport, error)
	}{
		{"PDF/UA-1", func(w *Writer) (*RemediationReport, error) {
			return w.RemediatePDFUA(ctx, models.PDFUA1, WithDocumentID([]byte("0123456789abcdef")))
		}},
		{"PDF/A-2a", func(w *Writer) (*RemediationReport, error) {
			return w.RemediatePDFA(ctx, models.PDFA2a, WithDocumentID([]byte("0123456789abcdef")))
		}},
	}
	for _, rem := range remediations {
		t.Run(rem.name, func(t *testing.T) {
			t.Parallel()

			remediate := func() []byte {
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				w := NewWriter(r)
				defer w.Close()
				rep, err := rem.run(w)
				if err != nil {
					t.Fatalf("remediating: %v", err)
				}
				// Control: auto-tagging must have run, or the scan this test
				// pins was never reached.
				tagged := false
				for _, fix := range rep.Fixed {
					if bytes.Contains([]byte(fix), []byte("structure tree")) {
						tagged = true
					}
				}
				if !tagged {
					t.Fatalf("remediation did not auto-tag the page, fixes %q", rep.Fixed)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				return out.Bytes()
			}

			first := remediate()
			const runs = 16
			for run := 1; run < runs; run++ {
				if got := remediate(); !bytes.Equal(got, first) {
					t.Fatalf("run %d wrote %d bytes that differ from run 0's %d: the output depends on map order",
						run, len(got), len(first))
				}
			}
		})
	}
}
