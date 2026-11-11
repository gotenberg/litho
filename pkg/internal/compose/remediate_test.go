package compose_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// remediate runs fix over out and returns the report and the written file.
func remediate(t *testing.T, out []byte, fix func(context.Context, *pdf.Writer) (*pdf.RemediationReport, error)) (*pdf.RemediationReport, []byte) {
	t.Helper()
	ctx := context.Background()
	w := pdf.NewWriter(open(t, out))
	report, err := fix(ctx, w)
	if err != nil {
		t.Fatalf("remediating: %v", err)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return report, buf.Bytes()
}

// mixedDoc paints every fragment kind, with transparency, on two pages.
func mixedDoc(t *testing.T) *compose.Document {
	t.Helper()
	pic := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for i := range pic.Pix {
		pic.Pix[i] = 0x80
	}
	gray := image.NewGray(image.Rect(0, 0, 2, 2))
	gray.SetGray(0, 0, color.Gray{Y: 0x40})
	return &compose.Document{
		Metadata: compose.Metadata{Title: "Mixed", Author: "litho", Lang: "en"},
		Pages: []*compose.Page{
			letter(
				run(t, sans, 12, compose.Point{X: 40, Y: 60}, "Plain text"),
				&compose.Group{Opacity: 0.5, Transform: compose.Matrix{A: 2, D: 2}, Clip: &compose.Path{Ops: rect(0, 0, 200, 200)}, Content: []compose.Fragment{
					&compose.Path{Ops: rect(20, 40, 30, 30), Fill: &compose.Fill{Color: compose.Color{R: 1, A: 0.8}}, Stroke: &compose.Stroke{Color: compose.Black, Width: 1}},
					&compose.Image{Rect: compose.Rect{X: 60, Y: 40, Width: 30, Height: 30}, Source: encode(t, pic)},
				}},
			),
			{Width: 400, Height: 400, Content: []compose.Fragment{
				run(t, serif, 10, compose.Point{X: 40, Y: 60}, "Serif"),
				&compose.Image{Rect: compose.Rect{X: 40, Y: 80, Width: 20, Height: 20}, Source: encode(t, gray)},
			}, Links: []compose.Link{{Rect: compose.Rect{X: 40, Y: 50, Width: 30, Height: 12}, Dest: &compose.Dest{Page: 1}}}},
		},
		Outline: []compose.Outline{{Title: "Start", Dest: compose.Dest{Page: 1}}},
	}
}

func TestWriteRemediatesToPDFA2b(t *testing.T) {
	t.Parallel()
	report, fixed := remediate(t, write(t, mixedDoc(t)), func(ctx context.Context, w *pdf.Writer) (*pdf.RemediationReport, error) {
		return w.RemediatePDFA(ctx, models.PDFA2b)
	})
	for _, fix := range report.Fixed {
		t.Logf("fixed: %s", fix)
	}
	for _, v := range report.Residual {
		t.Errorf("residual: %v", v)
	}
	violations, err := open(t, fixed).ValidatePDFA(context.Background(), models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, v := range violations {
		t.Errorf("remediated output: %v", v)
	}
}

func TestWriteRemediatesToPDFUA1(t *testing.T) {
	t.Parallel()
	report, fixed := remediate(t, write(t, taggedDoc(t)), func(ctx context.Context, w *pdf.Writer) (*pdf.RemediationReport, error) {
		return w.RemediatePDFUA(ctx, models.PDFUA1)
	})
	for _, fix := range report.Fixed {
		t.Logf("fixed: %s", fix)
	}
	for _, v := range report.Residual {
		t.Errorf("residual: %v", v)
	}
	r := open(t, fixed)
	violations, err := r.ValidatePDFUA(context.Background(), models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, v := range violations {
		t.Errorf("remediated output: %v", v)
	}
	if tagged, err := r.IsTagged(context.Background()); err != nil || !tagged {
		t.Errorf("remediated output IsTagged = %v, %v", tagged, err)
	}
}
