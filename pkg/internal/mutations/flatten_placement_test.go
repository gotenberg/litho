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

// placementFormPDF builds a page with one widget whose appearance carries the
// given /BBox and /Matrix entries, written as PDF source.
func placementFormPDF(rect, form string) []byte {
	appearance := "0 0 1 rg 0 0 10 10 re f"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 300] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /F 4 /Rect " + rect + " /AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form %s /Length %d >>\nstream\n%s\nendstream", form, len(appearance), appearance),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestFlattenFormPlacesByAlgorithm81 pins that a flattened widget's appearance
// lands where a viewer draws it: ISO 32000-1, 12.5.5, Algorithm 8.1 transforms
// the form's /BBox by its /Matrix and maps the box it gets onto /Rect. A plain
// translation to the first corner /Rect names drew an offset box away from the
// widget, a smaller box unscaled, and a rotated box beside it. A /Rect with no
// area scales the appearance to nothing, so the widget leaves drawing nothing.
func TestFlattenFormPlacesByAlgorithm81(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, rect, form string
		// cm is the matrix the drawing is placed with, empty when nothing is
		// drawn.
		cm string
	}{
		{name: "box at the origin sized to the rectangle", rect: "[20 200 100 260]", form: "/BBox [0 0 80 60]", cm: "1 0 0 1 20 200"},
		{name: "offset box", rect: "[120 200 200 260]", form: "/BBox [200 100 280 160]", cm: "1 0 0 1 -80 100"},
		{name: "box half the rectangle", rect: "[220 200 300 260]", form: "/BBox [0 0 40 30]", cm: "2 0 0 2 220 200"},
		{name: "box turned a quarter", rect: "[20 40 80 120]", form: "/BBox [0 0 80 60] /Matrix [0 1 -1 0 0 0]", cm: "1 0 0 1 80 40"},
		{name: "reversed corners and a translating matrix", rect: "[300 120 220 40]", form: "/BBox [0 0 80 80] /Matrix [1 0 0 1 40 -30]", cm: "1 0 0 1 180 70"},
		{name: "rectangle with no area", rect: "[350 50 350 50]", form: "/BBox [0 0 20 20]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := placementFormPDF(tc.rect, tc.form)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
			if err := FlattenForm(ctx, alloc, doc); err != nil {
				t.Fatalf("FlattenForm: %v", err)
			}

			var drawn []string
			for _, obj := range alloc.Pending() {
				s, ok := obj.(ast.PdfStream)
				if !ok {
					continue
				}
				body, err := s.Bytes(ctx)
				if err != nil {
					t.Fatalf("Bytes: %v", err)
				}
				if strings.Contains(string(body), " Do Q") {
					drawn = append(drawn, string(body))
				}
			}
			page, ok := alloc.Pending()[3].(ast.PdfDict)
			if !ok {
				t.Fatalf("page 3 not rewritten: %T", alloc.Pending()[3])
			}
			if _, kept := page["Annots"]; kept {
				t.Errorf("/Annots = %v, want the widget gone", page["Annots"])
			}

			if tc.cm == "" {
				if len(drawn) != 0 {
					t.Fatalf("drew %q, want nothing", drawn)
				}
				return
			}
			if len(drawn) != 1 {
				t.Fatalf("got %d drawings, want 1: %q", len(drawn), drawn)
			}
			if want := "q " + tc.cm + " cm /FlatAP"; !strings.Contains(drawn[0], want) {
				t.Errorf("drawing = %q, want it placed with %q", drawn[0], want)
			}
		})
	}
}
