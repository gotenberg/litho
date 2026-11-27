package odf

import (
	"context"
	"image"
	imgcolor "image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestAdjustPicture_ColorModes(t *testing.T) {
	t.Parallel()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, imgcolor.NRGBA{R: 200, G: 40, B: 40, A: 255})
	im.SetNRGBA(1, 0, imgcolor.NRGBA{R: 40, G: 200, B: 200, A: 128})
	pic := &layout.Picture{Width: 2, Height: 1, Source: source(t, im)}
	tests := []struct {
		name  string
		props string
		want  [2]imgcolor.NRGBA
	}{
		// vcl's luminance (76 R + 151 G + 29 B) / 256 of each pixel.
		{"greyscale", `draw:color-mode="greyscale"`, [2]imgcolor.NRGBA{{R: 87, G: 87, B: 87, A: 255}, {R: 152, G: 152, B: 152, A: 128}}},
		{"mono", `draw:color-mode="mono"`, [2]imgcolor.NRGBA{{A: 255}, {R: 255, G: 255, B: 255, A: 128}}},
		// Luminance +50% and contrast -70%: v 0.3 + 217.1, at most 255.
		{"watermark", `draw:color-mode="watermark"`, [2]imgcolor.NRGBA{{R: 255, G: 229, B: 229, A: 255}, {R: 229, G: 255, B: 255, A: 128}}},
		{"luminance", `draw:luminance="10%"`, [2]imgcolor.NRGBA{{R: 226, G: 66, B: 66, A: 255}, {R: 66, G: 226, B: 226, A: 128}}},
		// Luminance 50% and contrast 50%: v 128/64.5 + 1.48, at most 255.
		{"contrast", `draw:luminance="50%" draw:contrast="50%"`, [2]imgcolor.NRGBA{{R: 255, G: 81, B: 81, A: 255}, {R: 81, G: 255, B: 255, A: 128}}},
		// Red 100% adds 255 to the red channel alone.
		{"red", `draw:red="100%"`, [2]imgcolor.NRGBA{{R: 255, G: 40, B: 40, A: 255}, {R: 255, G: 200, B: 200, A: 128}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, err := ParseXML(t.Context(), []byte(`<s xmlns:draw="`+NSDraw+`" `+tt.props+`/>`))
			if err != nil {
				t.Fatal(err)
			}
			got := AdjustPicture(t.Context(), pic, PropsOf(root))
			if got == pic {
				t.Fatal("not redrawn")
			}
			out := decoded(t, got.Source)
			for i, w := range tt.want {
				if c := out.NRGBAAt(i, 0); c != w {
					t.Errorf("pixel %d %+v, want %+v", i, c, w)
				}
			}
		})
	}
	if got := AdjustPicture(t.Context(), pic, Props{}); got != pic {
		t.Errorf("a standard picture is redrawn")
	}
}

// source returns img encoded as an image source, failing t when it does
// not encode.
func source(t testing.TB, img image.Image) *typeset.ImageSource {
	t.Helper()
	src, err := typeset.EncodeImage(context.Background(), img)
	if err != nil {
		t.Fatalf("EncodeImage: %v", err)
	}
	return src
}

// decoded returns the pixels of src as an NRGBA picture, failing t when
// they do not decode to one.
func decoded(t testing.TB, src *typeset.ImageSource) *image.NRGBA {
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

// TestAdjustPicture_Vector locks that a vector picture's flat colours are
// mapped as a raster picture's channels: a black stroke of a picture of
// luminance 0%, contrast 0% and red 100% is stroked red, inside a group
// too, and its fill's alpha kept.
func TestAdjustPicture_Vector(t *testing.T) {
	t.Parallel()
	pic := &layout.Picture{Width: 10, Height: 10, Vector: func(context.Context, typeset.Rect) []typeset.Fragment {
		return []typeset.Fragment{&typeset.Group{Opacity: 1, Content: []typeset.Fragment{&typeset.Path{
			Fill:   &typeset.Fill{Color: typeset.Color{A: 0.5}},
			Stroke: &typeset.Stroke{Color: typeset.Black, Width: 3},
		}}}}
	}}
	root, err := ParseXML(t.Context(), []byte(`<s xmlns:draw="`+NSDraw+`" draw:luminance="0%" draw:contrast="0%" draw:red="100%"/>`))
	if err != nil {
		t.Fatal(err)
	}
	got := AdjustPicture(t.Context(), pic, PropsOf(root))
	frags := got.Vector(t.Context(), typeset.Rect{Width: 10, Height: 10})
	p := frags[0].(*typeset.Group).Content[0].(*typeset.Path)
	if want := (typeset.Color{R: 1, A: 1}); p.Stroke.Color != want {
		t.Errorf("stroke %+v, want %+v", p.Stroke.Color, want)
	}
	if want := (typeset.Color{R: 1, A: 0.5}); p.Fill.Color != want {
		t.Errorf("fill %+v, want %+v", p.Fill.Color, want)
	}
	again := pic.Vector(t.Context(), typeset.Rect{Width: 10, Height: 10})
	if c := again[0].(*typeset.Group).Content[0].(*typeset.Path).Stroke.Color; c != typeset.Black {
		t.Errorf("the source picture's stroke changed to %+v", c)
	}
}
