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
	pic := &layout.Picture{Width: 1, Height: 1, Source: &compose.ImageSource{Image: im}}
	tests := []struct {
		name, effects string
		want          imgcolor.NRGBA
	}{
		{"grey", `<a:grayscl/>`, imgcolor.NRGBA{R: 87, G: 87, B: 87, A: 255}},
		{"black at a low threshold's other side", `<a:biLevel thresh="50000"/>`, imgcolor.NRGBA{A: 255}},
		{"white past a low threshold", `<a:biLevel thresh="25000"/>`, imgcolor.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{"brighter", `<a:lum bright="10000"/>`, imgcolor.NRGBA{R: 226, G: 66, B: 66, A: 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blip, err := ooxml.Parse(context.Background(), []byte(`<a:blip xmlns:a="`+ooxml.NSA+`">`+tt.effects+`</a:blip>`), nil)
			if err != nil {
				t.Fatal(err)
			}
			out, ok := blipEffects(pic, blip).Source.Image.(*image.NRGBA)
			if !ok {
				t.Fatal("not redrawn")
			}
			if c := out.NRGBAAt(0, 0); c != tt.want {
				t.Errorf("pixel %+v, want %+v", c, tt.want)
			}
		})
	}
	blip, _ := ooxml.Parse(context.Background(), []byte(`<a:blip xmlns:a="`+ooxml.NSA+`"/>`), nil)
	if blipEffects(pic, blip) != pic {
		t.Error("a blip of no effect redraws its picture")
	}
}
