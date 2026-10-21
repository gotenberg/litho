package imgpaint

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

const testMaxPixels = 1 << 24

func newCanvas(t testing.TB, w, h int) *surface.Canvas {
	t.Helper()
	c, err := surface.NewCanvas(w, h, testMaxPixels)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rgbImage builds a w × h opaque image from rows of 0xRRGGBB values.
func rgbImage(w, h int, px ...uint32) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, v := range px {
		img.SetRGBA(i%w, i/w, color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255})
	}
	return img
}

func greyImage(w, h int, px ...uint8) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	copy(img.Pix, px)
	return img
}

// upright maps the unit square onto the device rectangle [x, x+w] × [y, y+h]
// with image row 0 at the top, as a page matrix with a y flip does.
func upright(x, y, w, h float64) geom.Matrix {
	return geom.Matrix{w, 0, 0, -h, x, y + h}
}

// pixel returns the unpremultiplied colour and alpha of (x, y) on 0 to 255.
func pixel(c *surface.Canvas, x, y int) [4]int {
	r, g, b, a := c.At(x, y)
	if a == 0 {
		return [4]int{}
	}
	q := func(v float32) int { return int(math.Round(float64(v) * 255)) }
	return [4]int{q(r / a), q(g / a), q(b / a), q(a)}
}

func wantPixel(t *testing.T, c *surface.Canvas, x, y int, want [4]int) {
	t.Helper()
	got := pixel(c, x, y)
	for k := range got {
		if d := got[k] - want[k]; d < -1 || d > 1 {
			t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
			return
		}
	}
}

var opaque = Params{}

func with(p Params, m geom.Matrix) Params {
	p.Matrix = m
	return p
}

func TestDrawPlacement(t *testing.T) {
	img := rgbImage(2, 2, 0xff0000, 0x00ff00, 0x0000ff, 0xffffff)
	red, green, blue, white := [4]int{255, 0, 0, 255}, [4]int{0, 255, 0, 255}, [4]int{0, 0, 255, 255}, [4]int{255, 255, 255, 255}
	tests := []struct {
		name string
		m    geom.Matrix
		want [2][2][4]int // [y][x]
	}{
		{"identity", upright(0, 0, 2, 2), [2][2][4]int{{red, green}, {blue, white}}},
		{"flipped vertically", geom.Matrix{2, 0, 0, 2, 0, 0}, [2][2][4]int{{blue, white}, {red, green}}},
		{"flipped horizontally", geom.Matrix{-2, 0, 0, -2, 2, 2}, [2][2][4]int{{green, red}, {white, blue}}},
		// X = 2v, Y = 2 − 2u: device (x, y) shows sample column 1−y, row 1−x.
		{"rotated", geom.Matrix{0, -2, 2, 0, 0, 2}, [2][2][4]int{{white, green}, {blue, red}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCanvas(t, 3, 3)
			if err := Draw(context.Background(), c, img, Masks{}, with(opaque, tt.m)); err != nil {
				t.Fatal(err)
			}
			for y := range 2 {
				for x := range 2 {
					wantPixel(t, c, x, y, tt.want[y][x])
				}
			}
			// The image covers [0, 2)²: the third row and column stay empty.
			for i := range 3 {
				wantPixel(t, c, 2, i, [4]int{})
				wantPixel(t, c, i, 2, [4]int{})
			}
		})
	}
}

func TestDrawUpscaleNearest(t *testing.T) {
	img := greyImage(2, 2, 0, 255, 255, 0)
	c := newCanvas(t, 8, 8)
	if err := Draw(context.Background(), c, img, Masks{}, with(opaque, upright(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	for y := range 8 {
		for x := range 8 {
			v := 0
			if (x < 4) != (y < 4) {
				v = 255
			}
			wantPixel(t, c, x, y, [4]int{v, v, v, 255})
		}
	}
}

func TestDrawUpscaleBilinear(t *testing.T) {
	img := greyImage(2, 1, 0, 255)
	c := newCanvas(t, 4, 1)
	p := with(opaque, upright(0, 0, 4, 1))
	p.Interpolate = true
	if err := Draw(context.Background(), c, img, Masks{}, p); err != nil {
		t.Fatal(err)
	}
	// Pixel centres fall at sample coordinates 0.25, 0.75, 1.25 and 1.75;
	// between the sample centres 0.5 and 1.5 the value is linear, and outside
	// them the edge sample holds.
	for x, v := range []int{0, 64, 191, 255} {
		wantPixel(t, c, x, 0, [4]int{v, v, v, 255})
	}
}

func TestDrawDownscaleBox(t *testing.T) {
	img := greyImage(4, 2, 0, 255, 255, 255, 0, 0, 255, 0)
	c := newCanvas(t, 2, 1)
	if err := Draw(context.Background(), c, img, Masks{}, with(opaque, upright(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	// Each device pixel covers a 2 × 2 block: (0+255+0+0)/4 and (255+255+255+0)/4.
	wantPixel(t, c, 0, 0, [4]int{64, 64, 64, 255})
	wantPixel(t, c, 1, 0, [4]int{191, 191, 191, 255})
}

func TestDrawDownscaleThirds(t *testing.T) {
	img := greyImage(6, 1, 0, 0, 255, 255, 255, 30)
	c := newCanvas(t, 2, 1)
	if err := Draw(context.Background(), c, img, Masks{}, with(opaque, upright(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	wantPixel(t, c, 0, 0, [4]int{85, 85, 85, 255})
	wantPixel(t, c, 1, 0, [4]int{180, 180, 180, 255})
}

func TestDrawDownscaleAreaWeights(t *testing.T) {
	// Ten samples across nine pixels: each pixel's footprint is 10/9 of a
	// sample, so it lies mostly over one sample and slightly over the next.
	// A box filter weighs each sample by the share of the footprint it
	// covers; two evenly spaced taps would weigh the sliver as much as the
	// sample and blur the image as a 2:1 reduction does.
	img := greyImage(10, 1, 255, 255, 255, 255, 0, 255, 255, 255, 255, 255)
	c := newCanvas(t, 9, 1)
	if err := Draw(context.Background(), c, img, Masks{}, with(opaque, upright(0, 0, 9, 1))); err != nil {
		t.Fatal(err)
	}
	// Pixel 3 covers samples [3.33, 4.44): 0.67 of sample 3 (255) and 0.44
	// of sample 4 (0), so 255 × 0.6. Pixel 4 covers [4.44, 5.56): half of
	// each. Pixel 2 covers [2.22, 3.33): only 255s.
	wantPixel(t, c, 2, 0, [4]int{255, 255, 255, 255})
	wantPixel(t, c, 3, 0, [4]int{153, 153, 153, 255})
	wantPixel(t, c, 4, 0, [4]int{128, 128, 128, 255})
	wantPixel(t, c, 5, 0, [4]int{255, 255, 255, 255})
}

func TestDrawNearIdentityStaysSharp(t *testing.T) {
	// Two samples across 1.99 pixels: a footprint of 1.005 samples is not a
	// reduction, so no neighbour is averaged in.
	img := greyImage(2, 1, 0, 255)
	c := newCanvas(t, 2, 1)
	if err := Draw(context.Background(), c, img, Masks{}, with(opaque, upright(0, 0, 1.99, 1))); err != nil {
		t.Fatal(err)
	}
	wantPixel(t, c, 0, 0, [4]int{0, 0, 0, 255})
	wantPixel(t, c, 1, 0, [4]int{255, 255, 255, 255})
}

func TestDrawStencil(t *testing.T) {
	// The decoder reads an image mask with /Decode applied: 0 paints.
	mask := StencilMask(greyImage(2, 2, 0, 255, 255, 0), false)
	c := newCanvas(t, 2, 2)
	if err := DrawStencil(context.Background(), c, mask, 1, 0, 0, with(opaque, upright(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	wantPixel(t, c, 0, 0, [4]int{255, 0, 0, 255})
	wantPixel(t, c, 1, 0, [4]int{})
	wantPixel(t, c, 0, 1, [4]int{})
	wantPixel(t, c, 1, 1, [4]int{255, 0, 0, 255})
}

func TestDrawStencilMaskOwnGrid(t *testing.T) {
	// A 1×1 image under a 2×2 /Mask: ISO 32000-1, 8.9.6.3 maps both onto the
	// unit square whatever their dimensions.
	img := rgbImage(1, 1, 0x0000ff)
	mask := StencilMask(greyImage(2, 2, 255, 0, 0, 255), false)
	c := newCanvas(t, 4, 4)
	if err := Draw(context.Background(), c, img, Masks{Mask: mask}, with(opaque, upright(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	for y := range 4 {
		for x := range 4 {
			want := [4]int{}
			if (x < 2) != (y < 2) {
				want = [4]int{0, 0, 255, 255}
			}
			wantPixel(t, c, x, y, want)
		}
	}
}

func TestDrawColorKeyNoBleed(t *testing.T) {
	data := []byte{255, 0, 0, 0, 255, 0}
	key, err := ColorKey(data, 2, 1, 3, 8, []int{0, 0, 250, 255, 0, 0}, testMaxPixels)
	if err != nil {
		t.Fatal(err)
	}
	img := rgbImage(2, 1, 0xff0000, 0x00ff00)
	c := newCanvas(t, 4, 1)
	p := with(opaque, upright(0, 0, 4, 1))
	p.Interpolate = true
	if err := Draw(context.Background(), c, img, Masks{Mask: key}, p); err != nil {
		t.Fatal(err)
	}
	// The keyed green sample is folded in premultiplied: it lowers the
	// alpha of its neighbours and never tints them.
	wantPixel(t, c, 0, 0, [4]int{255, 0, 0, 255})
	wantPixel(t, c, 1, 0, [4]int{255, 0, 0, 191})
	wantPixel(t, c, 2, 0, [4]int{255, 0, 0, 64})
	wantPixel(t, c, 3, 0, [4]int{})
}

func TestDrawSoftMaskAndConstantAlpha(t *testing.T) {
	img := rgbImage(1, 1, 0xff0000)
	smask := SoftMask(greyImage(1, 1, 128), false)
	c := newCanvas(t, 1, 1)
	c.Fill(1, 1, 1, 1)
	p := with(opaque, upright(0, 0, 1, 1))
	p.Opacity = surface.NewOpacity(0.5)
	if err := Draw(context.Background(), c, img, Masks{SMask: smask}, p); err != nil {
		t.Fatal(err)
	}
	// α = 128/255 × 0.5 ≈ 0.251 of red over white.
	g := int(math.Round((1 - 128.0/255*0.5) * 255))
	wantPixel(t, c, 0, 0, [4]int{255, g, g, 255})
}

func TestDrawMatte(t *testing.T) {
	// ISO 32000-1, 11.6.5.3: c' = m + α(c − m). Colour 1 at α 0.5 against a
	// black matte is stored as 0.5 and painted as 1 at α 0.5.
	img := greyImage(1, 1, 128)
	smask := SoftMask(greyImage(1, 1, 128), false)
	c := newCanvas(t, 1, 1)
	masks := Masks{SMask: smask, Matte: []float32{0, 0, 0}}
	if err := Draw(context.Background(), c, img, masks, with(opaque, upright(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	wantPixel(t, c, 0, 0, [4]int{255, 255, 255, 128})

	if err := Draw(context.Background(), c, img, Masks{Matte: []float32{0, 0, 0}}, with(opaque, upright(0, 0, 1, 1))); !errors.Is(err, ErrBadMask) {
		t.Errorf("a matte without a soft mask: err = %v, want ErrBadMask", err)
	}
}

func TestDrawClip(t *testing.T) {
	img := rgbImage(1, 1, 0x000000)
	clip, err := surface.NewMask(2, 1, testMaxPixels)
	if err != nil {
		t.Fatal(err)
	}
	clip.Set(1, 0, 0.5)
	c := newCanvas(t, 2, 1)
	p := with(opaque, upright(0, 0, 2, 1))
	p.Clip = clip
	if err := Draw(context.Background(), c, img, Masks{}, p); err != nil {
		t.Fatal(err)
	}
	wantPixel(t, c, 0, 0, [4]int{})
	wantPixel(t, c, 1, 0, [4]int{0, 0, 0, 128})

	small, err := surface.NewMask(1, 1, testMaxPixels)
	if err != nil {
		t.Fatal(err)
	}
	p.Clip = small
	if err := Draw(context.Background(), c, img, Masks{}, p); !errors.Is(err, ErrBadClip) {
		t.Errorf("mismatched clip: err = %v, want ErrBadClip", err)
	}
}

func TestDrawPaintsNothing(t *testing.T) {
	img := rgbImage(1, 1, 0x000000)
	for name, p := range map[string]Params{
		"singular matrix": with(opaque, geom.Matrix{1, 1, 1, 1, 0, 0}),
		"zero alpha":      {Matrix: upright(0, 0, 2, 2), Opacity: surface.NewOpacity(0)},
		"off canvas":      with(opaque, upright(-10, -10, 2, 2)),
		"not finite":      with(opaque, geom.Matrix{math.Inf(1), 0, 0, 1, 0, 0}),
	} {
		c := newCanvas(t, 2, 2)
		if err := Draw(context.Background(), c, img, Masks{}, p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, v := range c.Pix {
			if v != 0 {
				t.Fatalf("%s painted the canvas", name)
			}
		}
	}
}

func TestDrawHugeMatrix(t *testing.T) {
	// A matrix far larger than the canvas paints every pixel once and stays
	// within the canvas bounds.
	img := rgbImage(1, 1, 0x00ff00)
	c := newCanvas(t, 3, 3)
	if err := Draw(context.Background(), c, img, Masks{}, with(opaque, upright(-1e100, -1e100, 2e100, 2e100))); err != nil {
		t.Fatal(err)
	}
	wantPixel(t, c, 2, 2, [4]int{0, 255, 0, 255})
}

func TestDrawCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newCanvas(t, 2, 2)
	err := Draw(ctx, c, rgbImage(1, 1, 0), Masks{}, with(opaque, upright(0, 0, 2, 2)))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDrawBadPlane(t *testing.T) {
	c := newCanvas(t, 1, 1)
	bad := &Alpha{Width: 2, Height: 2, A: []uint8{1}}
	if err := Draw(context.Background(), c, rgbImage(1, 1, 0), Masks{Mask: bad}, with(opaque, upright(0, 0, 1, 1))); !errors.Is(err, ErrBadMask) {
		t.Errorf("Draw: err = %v, want ErrBadMask", err)
	}
	if err := DrawStencil(context.Background(), c, bad, 0, 0, 0, with(opaque, upright(0, 0, 1, 1))); !errors.Is(err, ErrBadMask) {
		t.Errorf("DrawStencil: err = %v, want ErrBadMask", err)
	}
}

// BenchmarkDrawFullPage paints a US Letter page-filling RGB scan at 150 dpi
// (1275 × 1650 pixels), once from a 300 dpi image (2:1 box filter) and once
// from a 100 dpi image upscaled with /Interpolate.
func BenchmarkDrawFullPage(b *testing.B) {
	for _, bc := range []struct {
		name        string
		w, h        int
		interpolate bool
	}{
		{"downscale300dpi", 2550, 3300, false},
		{"upscale100dpiInterpolate", 850, 1100, true},
		{"identity150dpi", 1275, 1650, false},
	} {
		b.Run(bc.name, func(b *testing.B) {
			img := image.NewRGBA(image.Rect(0, 0, bc.w, bc.h))
			for i := range img.Pix {
				img.Pix[i] = uint8(i * 31)
			}
			c := newCanvas(b, 1275, 1650)
			p := with(opaque, upright(0, 0, 1275, 1650))
			p.Interpolate = bc.interpolate
			b.ResetTimer()
			for b.Loop() {
				if err := Draw(context.Background(), c, img, Masks{}, p); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
