package pdf_test

import (
	"bytes"
	"context"
	"image"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// renderFirstPage draws page 1 of data at 72 dpi.
func renderFirstPage(t *testing.T, data []byte) image.Image {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	img, err := r.RenderPage(ctx, 1, pdf.WithRenderDPI(72))
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	return img
}

// TestRemediatePDFAType3ScopeKeepsEachScopesBinding pins that remediation
// gives a Type 3 font with no /Resources the names of each stream showing it.
// t3scope_2b's font paints /Bar, which its page and its form bind to a short
// and a tall bar (ISO 32000-1, 9.6.5, Table 112). Copying the page's names
// into the font passed veraPDF 1.30.2 at PDF/A-2b but drew the page's short
// bar where the form shows the glyphs, 6,144 pixels apart at 72 dpi; the
// output now draws as its source, in the default and the aggressive mode.
func TestRemediatePDFAType3ScopeKeepsEachScopesBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/t3scope_2b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	want := renderFirstPage(t, data)

	for mode, opts := range map[string][]pdf.RemediateOption{
		"default":    nil,
		"aggressive": {pdf.WithAggressiveFixes()},
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := pdf.NewWriter(r)
			defer w.Close()
			if _, err := w.RemediatePDFA(ctx, models.PDFA2b, opts...); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			got := renderFirstPage(t, out.Bytes())
			if got.Bounds() != want.Bounds() {
				t.Fatalf("remediated page is %v, source %v", got.Bounds(), want.Bounds())
			}
			differ := 0
			b := want.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if got.At(x, y) != want.At(x, y) {
						differ++
					}
				}
			}
			if differ > 0 {
				t.Errorf("%d pixels of the remediated page differ from the source", differ)
			}

			remediated, err := pdf.Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("Open remediated: %v", err)
			}
			defer remediated.Close()
			vs, err := remediated.ValidatePDFAReport(ctx, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			for _, v := range vs {
				if v.Clause == "6.2.2" {
					t.Errorf("remediated output still reports %s test %d: %s", v.Clause, v.TestNumber, v.Error())
				}
			}
		})
	}
}
