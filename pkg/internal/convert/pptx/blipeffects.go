package pptx

import (
	"bytes"
	"image"
	imgcolor "image/color"
	"image/jpeg"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// blipEffects returns the raster picture pic with the colour effects of
// its blip applied (ECMA-376 Part 1, 20.1.8.34 grayscl, 20.1.8.42 lum
// with its bright and contrast, 20.1.8.11 biLevel with its thresh), as
// Impress imports them: grey, or black and white at the threshold's
// luminance, then every channel shifted by the brightness and scaled by
// the contrast (vcl's Bitmap::Convert and Bitmap::Adjust); pic itself
// when the blip asks for none or pic is a vector.
//
// # Corpus evidence
//
//   - pptx: sd/tdf112209's picture fill of grayscl prints grey.
func blipEffects(pic *layout.Picture, blip *ooxml.Element) *layout.Picture {
	if pic == nil || pic.Source == nil || blip == nil {
		return pic
	}
	grey, threshold := false, -1
	var lum, con float64
	for _, k := range blip.Kids() {
		if k.Space != ooxml.NSA {
			continue
		}
		switch k.Local {
		case "grayscl":
			grey = true
		case "biLevel":
			n, _ := intAttr(k, "thresh")
			threshold = int(math.Round(max(0, min(float64(n)/100000, 1)) * 255))
		case "lum":
			b, _ := intAttr(k, "bright")
			c, _ := intAttr(k, "contrast")
			lum, con = max(-100, min(float64(b)/1000, 100)), max(-100, min(float64(c)/1000, 100))
		}
	}
	if !grey && threshold < 0 && lum == 0 && con == 0 {
		return pic
	}
	src := pic.Source.Image
	if src == nil {
		im, err := jpeg.Decode(bytes.NewReader(pic.Source.JPEG))
		if err != nil {
			return pic
		}
		src = im
	}
	scale := 1.0
	if con < 0 {
		scale = (128 + 1.28*con) / 128
	} else if con > 0 {
		scale = 128 / (128 - 1.27*con)
	}
	offset := lum*2.55 + 128 - scale*128
	var table [256]uint8
	for v := range table {
		table[v] = uint8(math.Round(max(0, min(255, float64(v)*scale+offset))))
	}
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := imgcolor.NRGBAModel.Convert(src.At(x, y)).(imgcolor.NRGBA)
			if grey || threshold >= 0 {
				// vcl's luminance: (76 R + 151 G + 29 B) / 256.
				l := uint8((uint32(c.R)*76 + uint32(c.G)*151 + uint32(c.B)*29) >> 8)
				switch {
				case threshold >= 0 && int(l) >= threshold:
					l = 255
				case threshold >= 0:
					l = 0
				}
				c.R, c.G, c.B = l, l, l
			}
			c.R, c.G, c.B = table[c.R], table[c.G], table[c.B]
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, c)
		}
	}
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: pic.NoRatio, Source: &compose.ImageSource{Image: out}}
}
