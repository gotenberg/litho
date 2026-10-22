package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"testing"
)

// boxedPDF is a page whose media box [0 0 200 100] holds a crop box
// [50 10 150 90], a bleed box [40 0 160 100] and a trim box [55 15 145 85],
// and no art box. A red band fills x 0 to 45, outside the crop box and
// inside the media box, its last 5 units inside the bleed box.
func boxedPDF(rotate int) []byte {
	content := "1 0 0 rg 0 0 45 100 re f"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /CropBox [50 10 150 90] "+
			"/BleedBox [40 0 160 100] /TrimBox [55 15 145 85] /Rotate %d /Contents 4 0 R >>", rotate),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

// TestRenderPageBox draws each page boundary at its size, the art box the
// page does not state as its crop box, and content outside the crop box
// where a wider box reaches it.
func TestRenderPageBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, boxedPDF(0), Limits{})
	red := color.NRGBA{255, 0, 0, 255}
	white := color.NRGBA{255, 255, 255, 255}
	for _, tc := range []struct {
		name string
		opts []RenderOption
		size image.Point
		// left is the pixel at (2, 50), red where the box reaches the band.
		left color.NRGBA
	}{
		{"default", nil, image.Pt(100, 80), white},
		{"crop", []RenderOption{WithRenderBox(RenderCropBox)}, image.Pt(100, 80), white},
		{"media", []RenderOption{WithRenderBox(RenderMediaBox)}, image.Pt(200, 100), red},
		{"bleed", []RenderOption{WithRenderBox(RenderBleedBox)}, image.Pt(120, 100), red},
		{"trim", []RenderOption{WithRenderBox(RenderTrimBox)}, image.Pt(90, 70), white},
		{"art", []RenderOption{WithRenderBox(RenderArtBox)}, image.Pt(100, 80), white},
	} {
		img, err := r.RenderPage(ctx, 1, append(tc.opts, WithRenderDPI(72))...)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := img.Bounds().Size(); got != tc.size {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.size)
		}
		if got := nrgbaAt(t, img, 2, 50); got != tc.left {
			t.Errorf("%s: pixel (2, 50) %v, want %v", tc.name, got, tc.left)
		}
	}
	if _, err := r.RenderPage(ctx, 1, WithRenderBox(RenderBox(9))); !errors.Is(err, ErrInvalidRenderOptions) {
		t.Errorf("box 9: %v, want ErrInvalidRenderOptions", err)
	}
}

// TestRenderPageBoxRotated turns the selected boundary by the page's
// /Rotate as it turns the crop box.
func TestRenderPageBoxRotated(t *testing.T) {
	t.Parallel()
	r := openLimited(t, boxedPDF(90), Limits{})
	img, err := r.RenderPage(context.Background(), 1, WithRenderDPI(72), WithRenderBox(RenderBleedBox))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := img.Bounds().Size(), image.Pt(100, 120); got != want {
		t.Errorf("rotated bleed box %v, want %v", got, want)
	}
	// Turned clockwise, the band at the bleed box's left lands at its top.
	if got, want := nrgbaAt(t, img, 50, 2), (color.NRGBA{255, 0, 0, 255}); got != want {
		t.Errorf("pixel (50, 2) %v, want %v", got, want)
	}
}
