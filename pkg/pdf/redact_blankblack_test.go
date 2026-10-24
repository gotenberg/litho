package pdf

import (
	"bytes"
	"context"
	"image"
	"testing"
)

// TestRedact_BlanksImagesToTheirBlack redacts the litho fixture
// blankblack_2b whole and in its centred quarter, and checks that every image
// sample the region covers renders black. The fixture's third image is in a
// YCC-like ICC space whose zero samples show a saturated blue: blanked to
// zero, the pixel check refused the redaction, and with the check off the
// region rendered blue. The other images hold the spaces blanking rewrites in
// sRGB or whose inverted /Decode it folds into the samples.
func TestRedact_BlanksImagesToTheirBlack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := readLithoFixture(t, "blankblack_2b.pdf")
	// The builder draws image i 112 x 288 points from (16 + 120i, 43) on a
	// 612 x 392 page. x0, x1, y0 and y1 bound the region on the page, in
	// points; at 72 dpi a point is a pixel, with y flipped.
	cases := map[string]struct {
		region [4]float64
		images []int
		x0, x1 int
		y0, y1 int
	}{
		"whole":   {[4]float64{-1e6, -1e6, 1e6, 1e6}, []int{0, 1, 2, 3, 4}, 0, 612, 43, 331},
		"quarter": {[4]float64{153, 98, 459, 294}, []int{1, 2, 3}, 153, 459, 98, 294},
		// The redact-render cell's band, 5% of the page high through its
		// middle, holds the centres of the images' ninth sample row.
		"band": {[4]float64{-1e6, 187, 1e6, 205}, []int{0, 1, 2, 3, 4}, 0, 612, 187, 205},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: tc.region}})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.BlankedImages) != len(tc.images) {
				t.Fatalf("blanked %d images, want %d", len(report.BlankedImages), len(tc.images))
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			after, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("Open output: %v", err)
			}
			defer after.Close()
			img, err := after.RenderPage(ctx, 1, WithRenderDPI(72))
			if err != nil {
				t.Fatalf("RenderPage: %v", err)
			}
			for _, i := range tc.images {
				// Two pixels in from every edge of the image and the region,
				// clear of the anti-aliased boundary.
				x0, x1 := max(16+120*i, tc.x0)+2, min(128+120*i, tc.x1)-2
				y0, y1 := 392-min(331, tc.y1)+2, 392-max(43, tc.y0)-2
				if bright, at := brightPixels(img, x0, y0, x1, y1); bright > 0 {
					t.Errorf("image %d: %d covered pixels brighter than black, one at %v", i, bright, at)
				}
			}
		})
	}
}

// brightPixels counts the pixels of [x0, x1) x [y0, y1) whose brightest
// channel passes the 8-bit level 80, the redaction pixel check's dark level,
// and returns one of them.
func brightPixels(img image.Image, x0, y0, x1, y1 int) (int, image.Point) {
	n := 0
	var at image.Point
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if max(r, g, b)>>8 > redactCheckDarkLevel {
				if n == 0 {
					at = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, at
}
