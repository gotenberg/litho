package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// transparentPDF draws a page whose transparency the tests flatten: a blue
// backdrop, a half-transparent red square in a transparency group form that
// also shows text, a multiply-blended green square, and text over it all.
func transparentPDF() []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " +
			"/Group << /S /Transparency /CS /DeviceRGB >> " +
			"/Resources << /Font << /F1 5 0 R >> /ExtGState << /Half 6 0 R /Mul 7 0 R >> /XObject << /Fm 8 0 R >> >> >>",
		colorStream("", "0 0 1 rg 0 0 200 100 re f /Fm Do q /Mul gs 0 1 0 rg 120 60 60 60 re f Q "+
			"BT /F1 14 Tf 0 g 20 150 Td (Above everything) Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
		"<< /Type /ExtGState /BM /Multiply >>",
		colorStream("/Type /XObject /Subtype /Form /BBox [0 0 200 200] /Group << /S /Transparency /K true >> "+
			"/Resources << /ExtGState << /G 6 0 R >> /Font << /F 5 0 R >> >>",
			"q /G gs 1 0 0 rg 20 40 80 80 re f Q BT /F 12 Tf 30 60 Td (In the form) Tj ET"),
	})
}

// renderRGBA draws page 1 of r at dpi.
func renderRGBA(t *testing.T, r *Reader, dpi float64) image.Image {
	t.Helper()
	img, err := r.RenderPage(context.Background(), 1, WithRenderDPI(dpi))
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	return img
}

// pixelsApart returns the share of pixels of a and b whose channels differ
// by more than tolerance 8-bit levels.
func pixelsApart(a, b image.Image, tolerance float64) float64 {
	if a.Bounds() != b.Bounds() {
		return 1
	}
	n, total := 0, 0
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			total++
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			for _, d := range []float64{float64(r1) - float64(r2), float64(g1) - float64(g2), float64(b1) - float64(b2)} {
				if math.Abs(d)/257 > tolerance {
					n++
					break
				}
			}
		}
	}
	return float64(n) / float64(total)
}

// flattenPDF flattens data with opts and returns the report and the output.
func flattenPDF(t *testing.T, data []byte, opts ...FlattenOption) (*FlattenReport, []byte) {
	t.Helper()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, data))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.FlattenTransparency(ctx, opts...)
	if err != nil {
		t.Fatalf("FlattenTransparency: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return report, out.Bytes()
}

func TestFlattenTransparency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := transparentPDF()
	report, out := flattenPDF(t, in)
	if len(report.Pages) != 1 {
		t.Fatalf("report %+v", report)
	}
	p := report.Pages[0]
	if p.Page != 1 || p.Regions == 0 || p.DPI != 300 || !p.GroupRemoved {
		t.Errorf("page report %+v", p)
	}

	// The composite is resampled where it is drawn: an edge 300 dpi puts
	// inside a pixel blurs a pixel of the 72 dpi page by a few dozen levels.
	src, dst := openColorPDF(t, in), openColorPDF(t, out)
	for _, dpi := range []float64{72, 144} {
		if d := pixelsApart(renderRGBA(t, src, dpi), renderRGBA(t, dst, dpi), 40); d > 0.01 {
			t.Errorf("at %v dpi %.2f%% of the pixels differ", dpi, 100*d)
		}
	}
	before, err := src.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	after, err := dst.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || !strings.Contains(after, "In the form") {
		t.Errorf("text %q, want %q", after, before)
	}

	// ISO 19005-1, 6.4 forbids every transparency construct: what it finds
	// in the output is transparency left behind.
	for _, doc := range []struct {
		name string
		r    *Reader
	}{{"source", src}, {"output", dst}} {
		violations, err := doc.r.ValidatePDFAReport(ctx, models.PDFA1b)
		if err != nil {
			t.Fatal(err)
		}
		var transparency []string
		for _, v := range violations {
			if strings.HasPrefix(v.Clause, "6.4") {
				transparency = append(transparency, v.Error())
			}
		}
		if doc.name == "source" && len(transparency) == 0 {
			t.Fatal("the source shows no transparency to PDF/A-1b: the check cannot see it")
		}
		if doc.name == "output" && len(transparency) > 0 {
			t.Errorf("transparency left: %v", transparency)
		}
	}
}

func TestFlattenTransparencyOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, dpi := range []float64{0, -1, math.Inf(1), math.NaN()} {
		w := NewWriter(openColorPDF(t, transparentPDF()))
		if _, err := w.FlattenTransparency(ctx, WithFlattenDPI(dpi)); !errors.Is(err, ErrInvalidFlattenOptions) {
			t.Errorf("dpi %v: %v, want ErrInvalidFlattenOptions", dpi, err)
		}
		_ = w.Close()
	}
	report, _ := flattenPDF(t, transparentPDF(), WithFlattenDPI(150))
	if got := report.Pages[0].DPI; got != 150 {
		t.Errorf("DPI %v, want 150", got)
	}
}

func TestFlattenTransparencyLeavesOpaqueDocumentsAlone(t *testing.T) {
	t.Parallel()
	in := recolorPDF("")
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, in))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.FlattenTransparency(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Pages) != 0 || w.forceRewrite {
		t.Errorf("report %+v, rewrite %v; want nothing changed", report, w.forceRewrite)
	}
}

func TestFlattenTransparencyAfterStagedEdits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, transparentPDF()))
	t.Cleanup(func() { _ = w.Close() })
	// A semi-transparent stamp queued first is flattened with the rest.
	if err := w.StampText(TextStamp{Text: "DRAFT", FontSize: 30, Opacity: 0.4}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.FlattenTransparency(ctx); err != nil {
		t.Fatalf("FlattenTransparency: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	dst := openColorPDF(t, out.Bytes())
	violations, err := dst.ValidatePDFAReport(ctx, models.PDFA1b)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if strings.HasPrefix(v.Clause, "6.4") {
			t.Errorf("transparency left: %v", v)
		}
	}
}
