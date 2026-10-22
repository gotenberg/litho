package blend

import (
	"context"
	"image"
	"testing"
)

// fuzzSide is the side, in pixels, of the groups [FuzzComposite] paints.
const fuzzSide = 4

// fuzzTolerance absorbs the float rounding of compositing a stack of
// elements, the most a channel of a valid composite may stray past [0, 1].
const fuzzTolerance = 1e-4

// FuzzComposite paints elements the fuzzed bytes describe into a group of a
// blending colour space and attributes they choose, derives alpha and
// luminosity soft masks from it, composites it into a page group through
// the mask, and reads the page back in sRGB and, in CMYK, as inks. The
// first byte picks the space and attributes; every 8 bytes after it are one
// element: blend mode, four components, shape, alpha and position, each
// value a byte over 255. Nothing panics, and every channel read back stays
// in [0, 1].
func FuzzComposite(f *testing.F) {
	f.Add([]byte{0, 0, 255, 0, 0, 0, 255, 255, 5, 1, 0, 0, 255, 0, 128, 200, 9})
	f.Add([]byte{6, 3, 255, 128, 0, 64, 255, 128, 0, 15, 10, 20})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		ctx := context.Background()
		kind := Kind(data[0] % 3)
		space := NewSpace(kind, "fuzz", nil)
		attrs := Attributes{Isolated: data[0]&4 != 0, Knockout: data[0]&8 != 0, Space: space}
		page, err := NewPageIn(fuzzSide, fuzzSide, space, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		page.Paint(1, 1, Normal, space.White(), 1, 0.5)
		g, err := NewGroup(image.Rect(0, 0, fuzzSide, fuzzSide), page.Backdrop(), attrs, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		unit := func(b byte) float32 { return float32(b) / 255 }
		for e := data[1:]; len(e) >= 8; e = e[8:] {
			c := Components{unit(e[1]), unit(e[2]), unit(e[3]), unit(e[4])}
			pos := int(e[7]) % (fuzzSide * fuzzSide)
			g.Paint(pos%fuzzSide, pos/fuzzSide, Mode(e[0]%16), c, unit(e[5]), unit(e[6]))
		}
		alpha, err := AlphaMask(ctx, g, nil, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		lum, err := LuminosityMask(ctx, g, space.White(), nil, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []*Mask{alpha, lum} {
			if err := g.Into(ctx, page, Params{Mode: Mode(data[0] % 16), SoftMask: m}); err != nil {
				t.Fatal(err)
			}
		}
		canvas, err := page.RGB(ctx, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		for i, v := range canvas.Pix {
			if !(v >= -fuzzTolerance && v <= 1+fuzzTolerance) {
				t.Fatalf("sRGB value %d is %v", i, v)
			}
		}
		if kind == CMYK {
			if _, err := page.CMYK(ctx); err != nil {
				t.Fatal(err)
			}
		}
	})
}
