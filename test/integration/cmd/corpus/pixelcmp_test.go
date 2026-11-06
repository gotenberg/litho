package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// solid returns a w by h image of one colour.
func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// withSquare returns a white w by h image with a black s by s square at (x, y).
func withSquare(w, h, x, y, s int) *image.RGBA {
	img := solid(w, h, color.RGBA{0xff, 0xff, 0xff, 0xff})
	for dy := range s {
		for dx := range s {
			img.SetRGBA(x+dx, y+dy, color.RGBA{0, 0, 0, 0xff})
		}
	}
	return img
}

func TestComparePixels_Rules(t *testing.T) {
	t.Parallel()

	exact := pixelRule{Tolerance: 64}
	tests := []struct {
		name      string
		got, want image.Image
		rule      pixelRule
		slack     int
		diff      int
		size      bool
	}{
		{"identical", withSquare(20, 20, 5, 5, 4), withSquare(20, 20, 5, 5, 4), renderPixelRule, 0, 0, false},
		{"one pixel shift, neighbourhood 1", withSquare(20, 20, 5, 5, 4), withSquare(20, 20, 6, 5, 4), renderPixelRule, 0, 0, false},
		{"one pixel shift, neighbourhood 0", withSquare(20, 20, 5, 5, 4), withSquare(20, 20, 6, 5, 4), exact, 0, 8, false},
		{"within channel tolerance", solid(4, 4, color.RGBA{200, 200, 200, 0xff}), solid(4, 4, color.RGBA{255, 255, 255, 0xff}), exact, 0, 0, false},
		{"past channel tolerance", solid(4, 4, color.RGBA{180, 255, 255, 0xff}), solid(4, 4, color.RGBA{255, 255, 255, 0xff}), exact, 0, 16, false},
		{"size within slack", solid(5, 4, color.RGBA{0xff, 0xff, 0xff, 0xff}), solid(4, 4, color.RGBA{0xff, 0xff, 0xff, 0xff}), exact, 1, 0, false},
		{"size past slack", solid(6, 4, color.RGBA{0xff, 0xff, 0xff, 0xff}), solid(4, 4, color.RGBA{0xff, 0xff, 0xff, 0xff}), exact, 1, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := comparePixels(tc.got, tc.want, tc.rule, tc.slack)
			if d.SizeMismatch != tc.size {
				t.Fatalf("SizeMismatch = %v, want %v", d.SizeMismatch, tc.size)
			}
			if d.DiffPixels != tc.diff {
				t.Errorf("DiffPixels = %d, want %d (%s)", d.DiffPixels, tc.diff, d)
			}
		})
	}
}

func TestPixelDiff_WithinTakesTheLargerAllowance(t *testing.T) {
	t.Parallel()

	d := pixelDiff{Pixels: 10_000, DiffPixels: 150}
	rule := pixelRule{MaxDiffPerMillion: 10_000}
	if d.within(rule) {
		t.Error("150 of 10000 pixels passed a 1% rule")
	}
	rule.MaxDiffPixels = 200
	if !d.within(rule) {
		t.Error("150 differing pixels failed an allowance of 200")
	}
	if (pixelDiff{SizeMismatch: true}).within(pixelRule{MaxDiffPixels: 1 << 30}) {
		t.Error("a size mismatch passed")
	}
}

func TestWriteDiffReport_WritesThreePanels(t *testing.T) {
	t.Parallel()

	got, want := withSquare(10, 6, 1, 1, 2), withSquare(10, 6, 6, 1, 2)
	d := comparePixels(got, want, pixelRule{Tolerance: 64}, 0)
	p := filepath.Join(t.TempDir(), "sub", "report.png")
	if err := writeDiffReport(p, got, want, d, 3); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p) //nolint:gosec // test temp file
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != (3*10+2*reportGutter)*3 || h != 6*3 {
		t.Fatalf("report is %dx%d", w, h)
	}
	// The heatmap panel marks a differing pixel red: (6, 1) is black in want
	// and white in got.
	x := (2*(10+reportGutter) + 6) * 3
	if r, g, b, _ := img.At(x, 1*3).RGBA(); r>>8 != 0xff || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("heatmap pixel is %d,%d,%d, want red", r>>8, g>>8, b>>8)
	}
}
