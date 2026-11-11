package pptx

import (
	"context"
	"image"
	imgcolor "image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

func TestBlipEffects(t *testing.T) {
	t.Parallel()
	im := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	im.SetNRGBA(0, 0, imgcolor.NRGBA{R: 200, G: 40, B: 40, A: 255})
	pic := &layout.Picture{Width: 1, Height: 1, Source: source(t, im)}
	fc := &fillCtx{}
	tests := []struct {
		name, effects string
		want          imgcolor.NRGBA
	}{
		{"grey", `<a:grayscl/>`, imgcolor.NRGBA{R: 87, G: 87, B: 87, A: 255}},
		{"black at a low threshold's other side", `<a:biLevel thresh="50000"/>`, imgcolor.NRGBA{A: 255}},
		{"white past a low threshold", `<a:biLevel thresh="25000"/>`, imgcolor.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		// Luminance 87 of 255 blends black into white.
		{"duotone", `<a:duotone><a:srgbClr val="000000"/><a:srgbClr val="FFFFFF"/></a:duotone>`, imgcolor.NRGBA{R: 87, G: 87, B: 87, A: 255}},
		{"brighter", `<a:lum bright="10000"/>`, imgcolor.NRGBA{R: 226, G: 66, B: 66, A: 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blip, err := ooxml.Parse(context.Background(), []byte(`<a:blip xmlns:a="`+ooxml.NSA+`">`+tt.effects+`</a:blip>`), nil)
			if err != nil {
				t.Fatal(err)
			}
			got := blipEffects(context.Background(), pic, blip, fc.colorChoice)
			if got == pic {
				t.Fatal("not redrawn")
			}
			out := decoded(t, got.Source)
			if c := out.NRGBAAt(0, 0); c != tt.want {
				t.Errorf("pixel %+v, want %+v", c, tt.want)
			}
		})
	}
	blip, _ := ooxml.Parse(context.Background(), []byte(`<a:blip xmlns:a="`+ooxml.NSA+`"/>`), nil)
	if blipEffects(context.Background(), pic, blip, fc.colorChoice) != pic {
		t.Error("a blip of no effect redraws its picture")
	}
}

// source returns img encoded as an image source, failing t when it does
// not encode.
func source(t testing.TB, img image.Image) *compose.ImageSource {
	t.Helper()
	src, err := compose.EncodeImage(context.Background(), img)
	if err != nil {
		t.Fatalf("EncodeImage: %v", err)
	}
	return src
}

// decoded returns the pixels of src as an NRGBA picture, failing t when
// they do not decode to one.
func decoded(t testing.TB, src *compose.ImageSource) *image.NRGBA {
	t.Helper()
	img, err := src.Decode(context.Background())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	n, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded picture is a %T, want *image.NRGBA", img)
	}
	return n
}

// TestStretched checks that an SVG picture is drawn stretched over its box,
// its own aspect ratio ignored.
func TestStretched(t *testing.T) {
	var drawn compose.Rect
	pic := &layout.Picture{Width: 10, Height: 10, Vector: func(_ context.Context, r compose.Rect) []compose.Fragment {
		drawn = r
		return []compose.Fragment{ooxml.RectPath(r)}
	}}
	out := stretched(pic).Vector(context.Background(), compose.Rect{X: 5, Y: 7, Width: 40, Height: 20})
	g, ok := out[0].(*compose.Group)
	if len(out) != 1 || !ok {
		t.Fatalf("fragments %v", out)
	}
	if want := (compose.Matrix{A: 4, D: 2, E: 5, F: 7}); g.Transform != want || drawn != (compose.Rect{Width: 10, Height: 10}) {
		t.Errorf("transform %+v drawing %+v, want %+v over the natural size", g.Transform, drawn, want)
	}
	if stretched(nil) != nil {
		t.Error("a missing picture stretches to something")
	}
}
