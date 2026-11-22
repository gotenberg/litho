package xlsx

import (
	"image"
	imgcolor "image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// colorChange returns the raster picture pic with the colour change of
// its blip applied (ECMA-376 Part 1, 20.1.8.16 clrChange): every pixel of
// the clrFrom colour takes the clrTo colour, its alpha included; with
// useA false, the default, a pixel matches by its red, green and blue
// alone. It returns pic itself when the blip asks for no change, pic is
// a vector, or pic does not decode.
//
// # Corpus evidence
//
//   - openxmlsdk/xlsx O15Conformance/XL/WebExtension/Bing.xlsx and
//     Youtube.xlsx: the fallback picture of a web extension, a logo on
//     black under a clrChange from black to black of alpha 0, prints on
//     the sheet's white.
func (r *reader) colorChange(pic *layout.Picture, blip *ooxml.Element) *layout.Picture {
	change := blip.Child(ooxml.NSA, "clrChange")
	if pic == nil || pic.Source == nil || change == nil {
		return pic
	}
	from, ok := ooxml.SchemeColor(change.Child(ooxml.NSA, "clrFrom"), r.theme)
	if !ok {
		return pic
	}
	to, ok := ooxml.SchemeColor(change.Child(ooxml.NSA, "clrTo"), r.theme)
	if !ok {
		return pic
	}
	useA := ooxml.OnOff(attr(change, "useA"), false)
	level := func(v float64) uint8 { return uint8(math.Round(max(0, min(v, 1)) * 255)) } // at most 255: v is held to [0, 1]
	want := imgcolor.NRGBA{R: level(from.R), G: level(from.G), B: level(from.B), A: level(from.A)}
	put := imgcolor.NRGBA{R: level(to.R), G: level(to.G), B: level(to.B), A: level(to.A)}
	src, err := pic.Source.Decode(r.ctx)
	if err != nil {
		return pic
	}
	b := src.Bounds()
	if err := typeset.ChargeImage(r.ctx, b.Dx(), b.Dy(), imgcolor.NRGBAModel); err != nil {
		// The refusal stops the conversion, which reports it.
		typeset.ReleaseImage(r.ctx, src)
		return pic
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := imgcolor.NRGBAModel.Convert(src.At(x, y)).(imgcolor.NRGBA)
			if c.R == want.R && c.G == want.G && c.B == want.B && (!useA || c.A == want.A) {
				c = put
			}
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, c)
		}
	}
	enc, err := typeset.EncodeImage(r.ctx, out)
	typeset.ReleaseImage(r.ctx, src)
	if err != nil {
		return pic
	}
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: pic.NoRatio, Source: enc}
}
