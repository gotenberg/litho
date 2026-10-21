package pdf

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"testing"
)

// annotatedPDF is a 100 by 50 page with two square annotations: a red one
// printed but hidden on screen (/F 36, Print and NoView) at the left, a blue
// one shown but not printed (/F 0) at the right.
func annotatedPDF() []byte {
	ap := func(c string) string {
		data := c + " rg 0 0 20 20 re f"
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length %d >>\nstream\n%s\nendstream", len(data), data)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 50] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 30 30] /F 36 /AP << /N 6 0 R >> >>",
		"<< /Type /Annot /Subtype /Square /Rect [70 10 90 30] /F 0 /AP << /N 7 0 R >> >>",
		ap("1 0 0"),
		ap("0 0 1"),
	})
}

func TestRenderPageTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, annotatedPDF(), Limits{})
	white := color.NRGBA{255, 255, 255, 255}
	red := color.NRGBA{255, 0, 0, 255}
	blue := color.NRGBA{0, 0, 255, 255}
	for _, tc := range []struct {
		name        string
		opts        []RenderOption
		left, right color.NRGBA
	}{
		{"default", nil, white, blue},
		{"view", []RenderOption{WithRenderTarget(RenderForView)}, white, blue},
		{"print", []RenderOption{WithRenderTarget(RenderForPrint)}, red, white},
	} {
		img, err := r.RenderPage(ctx, 1, append(tc.opts, WithRenderDPI(72))...)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := nrgbaAt(t, img, 20, 30); got != tc.left {
			t.Errorf("%s: left annotation pixel %v, want %v", tc.name, got, tc.left)
		}
		if got := nrgbaAt(t, img, 80, 30); got != tc.right {
			t.Errorf("%s: right annotation pixel %v, want %v", tc.name, got, tc.right)
		}
	}
	if _, err := r.RenderPage(ctx, 1, WithRenderTarget(RenderTarget(9))); !errors.Is(err, ErrInvalidRenderOptions) {
		t.Errorf("target 9: %v, want ErrInvalidRenderOptions", err)
	}
}
