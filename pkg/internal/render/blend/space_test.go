package blend

import (
	"context"
	"image"
	"testing"
)

func nearComponents(a, b Components, n int) bool {
	for i := range n {
		if !near32(a[i], b[i]) {
			return false
		}
	}
	return true
}

// TestSubtractiveSeparableBlend checks ISO 32000-1, 11.3.5: in a subtractive
// space the separable blend functions apply to the complements of the
// components, so Multiply of two inks adds them as a press would (cyan over
// magenta is cyan and magenta) where Multiply of the raw values would give
// no ink.
func TestSubtractiveSeparableBlend(t *testing.T) {
	cmyk := NewSpace(CMYK, "cmyk", nil)
	g, err := NewGroup(image.Rect(0, 0, 1, 1), Backdrop{}, Attributes{Isolated: true, Space: cmyk}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	g.Paint(0, 0, Normal, Components{1, 0, 0, 0.2}, 1, 1)
	g.Paint(0, 0, Multiply, Components{0, 1, 0, 0.5}, 1, 1)
	got, a := g.Components(0, 0)
	// 1 - (1 - 0.2)(1 - 0.5) = 0.6 of black.
	if want := (Components{1, 1, 0, 0.6}); a != 1 || !nearComponents(got, want, 4) {
		t.Errorf("Multiply in CMYK = %v at alpha %g, want %v", got, a, want)
	}

	g.Paint(0, 0, Screen, Components{0, 0.5, 0, 0}, 1, 1)
	got, _ = g.Components(0, 0)
	// Screen of complements is Multiply of inks: magenta 1 x 0.5.
	if want := (Components{0, 0.5, 0, 0}); !nearComponents(got, want, 4) {
		t.Errorf("Screen in CMYK = %v, want %v", got, want)
	}
}

// TestNonSeparableBlackComponent checks ISO 32000-1, 11.3.5.3: in CMYK a
// non-separable mode takes K from the backdrop for Hue, Saturation and Color
// and from the source for Luminosity.
func TestNonSeparableBlackComponent(t *testing.T) {
	cb, cs := Components{0.2, 0.4, 0.6, 0.3}, Components{0.5, 0.1, 0.3, 0.8}
	cmyk := NewSpace(CMYK, "cmyk", nil)
	for _, tc := range []struct {
		mode Mode
		k    float32
	}{{Hue, 0.3}, {Saturation, 0.3}, {Color, 0.3}, {Luminosity, 0.8}} {
		out := cmyk.load(tc.mode.blendIn(CMYK, cmyk.store(cb), cmyk.store(cs)))
		if !near32(out[3], tc.k) {
			t.Errorf("%v: K = %g, want %g", tc.mode, out[3], tc.k)
		}
	}
}

// TestGreyNonSeparable checks that a grey colour reads as the RGB colour of
// three equal components under the non-separable modes: Luminosity takes the
// source's grey and the others the backdrop's.
func TestGreyNonSeparable(t *testing.T) {
	for _, tc := range []struct {
		mode Mode
		want float32
	}{{Hue, 0.25}, {Saturation, 0.25}, {Color, 0.25}, {Luminosity, 0.75}} {
		if got := tc.mode.blendIn(Gray, Components{0.25}, Components{0.75})[0]; !near32(got, tc.want) {
			t.Errorf("%v: %g, want %g", tc.mode, got, tc.want)
		}
	}
}

// TestRGBSpaceIsSRGB checks that an RGB space is sRGB itself, a nil *Space,
// so an RGB group keeps the canvas the renderer paints on directly.
func TestRGBSpaceIsSRGB(t *testing.T) {
	if NewSpace(RGB, "x", nil) != nil {
		t.Fatal("an RGB space is not sRGB")
	}
	g, err := NewGroup(image.Rect(0, 0, 2, 2), Backdrop{}, Attributes{Isolated: true, Space: NewSpace(Gray, "g", nil)}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if g.Canvas() != nil || g.Direct() != nil {
		t.Error("a grey group hands out an sRGB canvas")
	}
}

// TestIntoConvertsSpaces checks that a group composited into a parent of
// another blending space reaches it in the parent's space (Table 147, /CS),
// and into one of the same space unconverted.
func TestIntoConvertsSpaces(t *testing.T) {
	ctx := context.Background()
	cmyk := NewSpace(CMYK, "cmyk", nil)
	child, err := NewGroup(image.Rect(0, 0, 1, 1), Backdrop{}, Attributes{Isolated: true, Space: cmyk}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	child.Paint(0, 0, Normal, Components{0, 0, 0, 1}, 1, 1)

	page, err := NewPage(1, 1, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Into(ctx, page, Params{}); err != nil {
		t.Fatal(err)
	}
	if got := page.Canvas().Pix; !near32(got[0], 0) || !near32(got[3], 1) {
		t.Errorf("black ink into sRGB = %v, want opaque black", got)
	}

	same, err := NewPageIn(1, 1, NewSpace(CMYK, "cmyk", nil), testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Into(ctx, same, Params{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := same.Components(0, 0); !nearComponents(got, Components{0, 0, 0, 1}, 4) {
		t.Errorf("black ink into the same CMYK space = %v, want it unconverted", got)
	}
}

// TestUnderlayInCMYK checks that a CMYK page group over white paper leaves no
// ink where nothing painted and keeps a half-transparent black as half
// black.
func TestUnderlayInCMYK(t *testing.T) {
	cmyk := NewSpace(CMYK, "cmyk", nil)
	g, err := NewPageIn(2, 1, cmyk, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	g.Paint(1, 0, Normal, Components{0, 0, 0, 1}, 1, 0.5)
	g.Underlay(cmyk.White(), 1)
	if got, a := g.Components(0, 0); a != 1 || !nearComponents(got, Components{}, 4) {
		t.Errorf("paper = %v at alpha %g, want no ink", got, a)
	}
	if got, a := g.Components(1, 0); a != 1 || !nearComponents(got, Components{0, 0, 0, 0.5}, 4) {
		t.Errorf("half black over paper = %v at alpha %g, want K 0.5", got, a)
	}
}

// TestLuminosityMaskInGrey checks a grey group's luminosity mask (ISO
// 32000-1, 11.5.3): the grey itself, composited over /BC.
func TestLuminosityMaskInGrey(t *testing.T) {
	gray := NewSpace(Gray, "g", nil)
	g, err := NewGroup(image.Rect(0, 0, 2, 1), Backdrop{}, Attributes{Isolated: true, Space: gray}, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	g.Paint(0, 0, Normal, Components{0.5}, 1, 1)
	g.Paint(0, 0, Multiply, Components{0.5}, 1, 1)
	m, err := LuminosityMask(context.Background(), g, Components{0.8}, nil, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.At(0, 0); !near32(got, 0.25) {
		t.Errorf("mask over the multiplied greys = %g, want 0.25", got)
	}
	if got := m.At(1, 0); !near32(got, 0.8) {
		t.Errorf("mask over the backdrop = %g, want 0.8", got)
	}
}
