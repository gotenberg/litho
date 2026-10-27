package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"sync"
	"testing"
)

// renderPDF is a 100 by 50 page: a red square at the lower-left corner and a
// blue stroke across the top.
func renderPDF() []byte {
	const content = "1 0 0 rg 0 0 20 20 re f 0 0 1 RG 4 w 0 45 m 100 45 l S"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 50] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

func nrgbaAt(t *testing.T, img image.Image, x, y int) color.NRGBA {
	t.Helper()
	n, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("RenderPage returned %T, want *image.NRGBA", img)
	}
	return n.NRGBAAt(x, y)
}

func TestRenderPage(t *testing.T) {
	t.Parallel()
	r := openLimited(t, renderPDF(), Limits{})
	ctx := context.Background()

	img, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 100 || b.Dy() != 50 {
		t.Fatalf("size %v, want 100x50", b)
	}
	for _, c := range []struct {
		x, y int
		want color.NRGBA
	}{
		{5, 45, color.NRGBA{255, 0, 0, 255}},
		{50, 5, color.NRGBA{0, 0, 255, 255}},
		{50, 25, color.NRGBA{255, 255, 255, 255}},
	} {
		if got := nrgbaAt(t, img, c.x, c.y); got != c.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}

	// The default resolution is DefaultRenderDPI.
	img, err = r.RenderPage(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 209 {
		t.Errorf("default width %d, want 209, 100 units at 150 dpi rounded up", b.Dx())
	}

	// A transparent background leaves unpainted pixels transparent.
	img, err = r.RenderPage(ctx, 1, WithRenderDPI(72), WithRenderBackground(color.Transparent))
	if err != nil {
		t.Fatal(err)
	}
	if got := nrgbaAt(t, img, 50, 25); got.A != 0 {
		t.Errorf("unpainted pixel over a transparent background = %v", got)
	}
	if got := nrgbaAt(t, img, 5, 45); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("painted pixel over a transparent background = %v", got)
	}
}

func TestRenderPageErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, renderPDF(), Limits{})
	if _, err := r.RenderPage(ctx, 2); !errors.Is(err, ErrInvalidPageNumber) {
		t.Errorf("page 2: %v, want ErrInvalidPageNumber", err)
	}
	if _, err := r.RenderPage(ctx, 1, WithRenderDPI(0)); !errors.Is(err, ErrInvalidRenderOptions) {
		t.Errorf("0 dpi: %v, want ErrInvalidRenderOptions", err)
	}

	// The render budget, lowered through the public API, refuses a page past
	// it and draws one within it.
	small := openLimited(t, renderPDF(), Limits{MaxRenderPixels: 5_000})
	if _, err := small.RenderPage(ctx, 1, WithRenderDPI(73)); !errors.Is(err, ErrRenderTooLarge) {
		t.Errorf("past MaxRenderPixels: %v, want ErrRenderTooLarge", err)
	}
	if _, err := small.RenderPage(ctx, 1, WithRenderDPI(72)); err != nil {
		t.Errorf("within MaxRenderPixels: %v", err)
	}
}

func TestRenderPageConcurrent(t *testing.T) {
	t.Parallel()
	r := openLimited(t, renderPDF(), Limits{})
	ctx := context.Background()
	want, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 8 {
		wg.Go(func() {
			got, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
			if err != nil {
				errs <- err
				return
			}
			if string(got.(*image.NRGBA).Pix) != string(want.(*image.NRGBA).Pix) {
				errs <- errors.New("a concurrent render differs")
			}
			got, rep, err := r.RenderPageReport(ctx, 1, WithRenderDPI(72))
			if err != nil {
				errs <- err
				return
			}
			if string(got.(*image.NRGBA).Pix) != string(want.(*image.NRGBA).Pix) || rep != (RenderReport{}) {
				errs <- errors.New("a concurrent render with its report differs")
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
