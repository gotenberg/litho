package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestType3PaintBounds pins the bounds a Type 3 glyph description paints in
// glyph space: through the CTM its cm operators set (ISO 32000-1, 8.4.4), an
// image as the unit square through that CTM (8.9.5), a stroke widened by half
// its line width, and nothing for a path that is only clipped or ended. A
// mark the walk cannot bound reports it incomplete.
func TestType3PaintBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		data     string
		want     *[4]float64
		complete bool
	}{
		{"filled rectangle", "0 0 500 700 re f", &[4]float64{0, 0, 500, 700}, true},
		{"bitmap image under cm", "q 5 0 0 7 1 -2 cm BI /W 1 /H 1 /BPC 1 /IM true ID \x00 EI Q", &[4]float64{1, -2, 6, 5}, true},
		{"path under a scaling cm", "q 10 0 0 10 0 0 cm 0 0 m 1 2 l f Q 0 0 m 1 1 l f", &[4]float64{0, 0, 10, 20}, true},
		{"rotated curve", "q 0 1 -1 0 0 0 cm 0 0 m 0 10 10 10 10 0 c f Q", &[4]float64{-7.5, 0, 0, 10}, true},
		{"stroke widened by half its width", "4 w 0 0 m 10 0 l S", &[4]float64{-2, -2, 12, 2}, true},
		{"clip only", "0 0 10 10 re W n", nil, true},
		{"form XObject", "q 3 0 0 3 0 0 cm /Fm1 Do Q", &[4]float64{0, 0, 3, 3}, false},
		{"shading", "/Sh1 sh", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops, err := content.ParseStreamData(ctx, []byte(tc.data), content.SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData: %v", err)
			}
			got, complete := type3PaintBounds(ops)
			if complete != tc.complete {
				t.Errorf("complete = %v, want %v", complete, tc.complete)
			}
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if got == nil {
				return
			}
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("got %v, want %v", *got, *tc.want)
				}
			}
		})
	}
}

// TestType3GlyphInkBounds pins how a Type 3 glyph's ink combines what its
// description paints with the box its d1 states and, when the description is
// not all bounded, the font's /FontBBox (ISO 32000-1, 9.6.5).
func TestType3GlyphInkBounds(t *testing.T) {
	t.Parallel()
	fontBBox := &[4]float64{-10, -20, 900, 800}
	for _, tc := range []struct {
		name string
		g    type3Glyph
		want [4]float64
		ok   bool
	}{
		{"paint and d1 box", type3Glyph{paintBox: &[4]float64{0, 0, 500, 700}, d1Box: &[4]float64{-5, 0, 400, 750}, paintComplete: true}, [4]float64{-5, 0, 500, 750}, true},
		{"degenerate d1 box", type3Glyph{paintBox: &[4]float64{0, 0, 500, 700}, d1Box: &[4]float64{0, 0, 0, 0}, paintComplete: true}, [4]float64{0, 0, 500, 700}, true},
		{"paints nothing", type3Glyph{paintComplete: true}, [4]float64{}, true},
		{"unbounded mark", type3Glyph{paintBox: &[4]float64{0, 0, 1, 1}}, [4]float64{-10, -20, 900, 800}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := type3GlyphInkBounds(tc.g, fontBBox)
			if ok != tc.ok || got != tc.want {
				t.Errorf("got %v, %v, want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := type3GlyphInkBounds(type3Glyph{}, &[4]float64{}); ok {
		t.Error("an unbounded mark in a font with an empty /FontBBox is bounded, want unbounded")
	}
}
