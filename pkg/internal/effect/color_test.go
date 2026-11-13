package effect

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestFloodTintAlpha_Values(t *testing.T) {
	ctx := context.Background()
	s := newSurface(t, RGBA, 2, 1)
	if err := Flood(ctx, s, Color{255, 128, 0, 128}); err != nil {
		t.Fatal(err)
	}
	if want := []uint8{128, 64, 0, 128, 128, 64, 0, 128}; !slices.Equal(s.Pix, want) {
		t.Errorf("flood = %v, want %v", s.Pix, want)
	}
	cov := newSurface(t, Coverage, 3, 1)
	copy(cov.Pix, []uint8{0, 128, 255})
	tint, err := Tint(ctx, cov, Color{0, 0, 255, 102})
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint8{0, 0, 0, 0, 0, 0, 51, 51, 0, 0, 102, 102}; !slices.Equal(tint.Pix, want) {
		t.Errorf("tint = %v, want %v", tint.Pix, want)
	}
	a, err := AlphaOf(ctx, tint)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint8{0, 51, 102}; !slices.Equal(a.Pix, want) {
		t.Errorf("alpha = %v, want %v", a.Pix, want)
	}
	if err := Threshold(a, 52); err != nil {
		t.Fatal(err)
	}
	if want := []uint8{0, 0, 255}; !slices.Equal(a.Pix, want) {
		t.Errorf("threshold = %v, want %v", a.Pix, want)
	}
	if got := ColorOf(typeset.Color{R: 1, G: 0.5, B: math.NaN(), A: 2}); got != (Color{255, 128, 0, 255}) {
		t.Errorf("ColorOf = %v", got)
	}
}

// TestColorMatrix_HandValues applies matrices to colours whose results
// are worked by hand from Filter Effects 1, 9.6.
func TestColorMatrix_HandValues(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		m    Matrix
		in   []uint8
		want []uint8
	}{
		{"identity", IdentityMatrix, []uint8{100, 50, 25, 200}, []uint8{100, 50, 25, 200}},
		// Swap red and blue, halve alpha: (100, 50, 25) over 200 is (128,
		// 64, 32) unpremultiplied, swapped (32, 64, 128), over 100 (13, 25,
		// 50).
		{"swap and halve", Matrix{0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0.5, 0}, []uint8{100, 50, 25, 200}, []uint8{13, 25, 50, 100}},
		// The constant column adds 0.2 to red: 0 + 51.
		{"offset", Matrix{1, 0, 0, 0, 0.2, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0}, []uint8{0, 0, 0, 255}, []uint8{51, 0, 0, 255}},
		// Saturate 0 of pure red: every channel 0.213 times 255 = 54.3.
		{"saturate 0", SaturateMatrix(0), []uint8{255, 0, 0, 255}, []uint8{54, 54, 54, 255}},
		// Luminance to alpha of white: 0.2125 + 0.7154 + 0.0721 = 1.
		{"luminance to alpha", LuminanceToAlphaMatrix(), []uint8{255, 255, 255, 255}, []uint8{0, 0, 0, 255}},
		// Grayscale 1 of pure green: 0.7152 times 255 = 182.4.
		{"grayscale", GrayscaleMatrix(1), []uint8{0, 255, 0, 255}, []uint8{182, 182, 182, 255}},
		// Sepia 1 of white: rows sum to 1.351, 1.203, 0.937.
		{"sepia", SepiaMatrix(1), []uint8{255, 255, 255, 255}, []uint8{255, 255, 239, 255}},
		{"hue rotate 0", HueRotateMatrix(0), []uint8{10, 200, 30, 255}, []uint8{10, 200, 30, 255}},
		{"hue rotate 360", HueRotateMatrix(360), []uint8{10, 200, 30, 255}, []uint8{10, 200, 30, 255}},
		// Hue rotate 180 of red: (0.213 - 0.787, 0.213 + 0.213, 0.213 +
		// 0.213) = (-0.574, 0.426, 0.426), clamped.
		{"hue rotate 180", HueRotateMatrix(180), []uint8{255, 0, 0, 255}, []uint8{0, 109, 109, 255}},
	} {
		s := newSurface(t, RGBA, 1, 1)
		copy(s.Pix, tc.in)
		if err := ColorMatrix(ctx, s, tc.m); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(s.Pix, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, s.Pix, tc.want)
		}
	}
}

func TestTransfer_Tables(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    TransferFunc
		at   []int
		want []uint8
	}{
		{"identity", TransferFunc{}, []int{0, 77, 255}, []uint8{0, 77, 255}},
		{"table", TransferFunc{Kind: TransferTable, Values: []float64{0, 1, 0}}, []int{0, 64, 128, 191, 255}, []uint8{0, 128, 254, 128, 0}},
		{"discrete", TransferFunc{Kind: TransferDiscrete, Values: []float64{0.2, 0.8}}, []int{0, 127, 128, 255}, []uint8{51, 51, 204, 204}},
		{"linear", TransferFunc{Kind: TransferLinear, Slope: 2, Intercept: -0.5}, []int{0, 128, 255}, []uint8{0, 129, 255}},
		{"gamma", TransferFunc{Kind: TransferGamma, Amplitude: 1, Exponent: 2}, []int{0, 128, 255}, []uint8{0, 64, 255}},
		{"empty table", TransferFunc{Kind: TransferTable}, []int{10}, []uint8{10}},
		{"brightness", BrightnessTransfer(0.5), []int{200}, []uint8{100}},
		{"contrast", ContrastTransfer(3), []int{50, 100, 200}, []uint8{0, 45, 255}},
		{"invert", InvertTransfer(1), []int{0, 255}, []uint8{255, 0}},
		{"opacity", OpacityTransfer(0.5), []int{255}, []uint8{128}},
	} {
		lut := tc.f.Table()
		for i, v := range tc.at {
			if lut[v] != tc.want[i] {
				t.Errorf("%s(%d) = %d, want %d", tc.name, v, lut[v], tc.want[i])
			}
		}
	}
	s := newSurface(t, RGBA, 1, 1)
	copy(s.Pix, []uint8{100, 0, 50, 200})
	inv := InvertTransfer(1).Table()
	if err := Transfer(context.Background(), s, &inv, &IdentityLUT, &IdentityLUT, &IdentityLUT); err != nil {
		t.Fatal(err)
	}
	// Red 100 over 200 is 128 unpremultiplied, inverted 127, times 200.
	if want := []uint8{100, 0, 50, 200}; !slices.Equal(s.Pix, want) {
		t.Errorf("transfer = %v, want %v", s.Pix, want)
	}
}
