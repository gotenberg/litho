package odp

import (
	"image"
	imgcolor "image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

func TestAdjusted_ColorModes(t *testing.T) {
	t.Parallel()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, imgcolor.NRGBA{R: 200, G: 40, B: 40, A: 255})
	im.SetNRGBA(1, 0, imgcolor.NRGBA{R: 40, G: 200, B: 200, A: 128})
	pic := &layout.Picture{Width: 2, Height: 1, Source: &compose.ImageSource{Image: im}}
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, err := odf.ParseXML(t.Context(), []byte(`<s xmlns:draw="`+odf.NSDraw+`" `+tt.props+`/>`))
			if err != nil {
				t.Fatal(err)
			}
			got := adjusted(pic, odf.PropsOf(root))
			out, ok := got.Source.Image.(*image.NRGBA)
			if !ok {
				t.Fatalf("picture %+v", got.Source)
			}
			for i, w := range tt.want {
				if c := out.NRGBAAt(i, 0); c != w {
					t.Errorf("pixel %d %+v, want %+v", i, c, w)
				}
			}
		})
	}
	if got := adjusted(pic, odf.Props{}); got != pic {
		t.Errorf("a standard picture is redrawn")
	}
}
