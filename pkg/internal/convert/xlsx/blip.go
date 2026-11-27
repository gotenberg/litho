package xlsx

import (
	"image"
	imgcolor "image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
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

// recolor returns the raster picture pic with the colour effects of its
// blip applied as Excel prints them, in the order the blip lists them
// (ECMA-376 Part 1, 20.1.8.34 grayscl, 20.1.8.42 lum, 20.1.8.23 duotone,
// 20.1.8.11 biLevel), every pixel's alpha kept. Each reads a pixel's
// luma, 0.2126 R + 0.7152 G + 0.0722 B of its sRGB channels, rounded:
// grayscl paints it grey at its luma; duotone blends the first colour
// into the second by it; biLevel paints it white at a luma of thresh or
// more, black below; lum scales each channel about its middle by the
// contrast's factor k (1 / (1 - contrast) above 0, 1 + contrast below)
// and adds the brightness times (1 + k) / 2, the result held to [0, 1]
// and truncated to a level. It returns pic itself when the blip asks for
// none of them, pic is a vector, or pic does not decode.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel for the web prints eight bands (black, white,
//     grey 128, red, green 160, blue, FFC800, 40B4DC) under grayscl as
//     00, FF, 80, 36, 72, 12, C5 and 9E; under duotone from black to
//     FF8000 as 000000, FF8000, 804000, 361B00, 723900, 120900, C56300,
//     9E4F00; under biLevel at 50 per cent the black, red, green and blue
//     bands black and the four others white, and at 25 per cent the green
//     band white too; under lum bright 30 per cent the black band 4C4C4C and grey
//     CCCCCC; under lum contrast 50 per cent green 160 as C0 and 40B4DC as
//     00E8FF; under lum bright 70 and contrast -70 per cent (Washout)
//     black as CDCDCD, grey F3F3F3, green CDFDCD and 40B4DC as E0FFFF.
//   - openxmlsdk/xlsx: O14ISOStrict/Excel/Recolor_Graphics_mode_-O12-XL-
//     PictureProperties.xlsx holds a photograph under grayscl, a duotone,
//     Washout and biLevel.
func (r *reader) recolor(pic *layout.Picture, blip *ooxml.Element) *layout.Picture {
	if pic == nil || pic.Source == nil || blip == nil {
		return pic
	}
	var steps []func(c imgcolor.NRGBA) imgcolor.NRGBA
	for _, k := range blip.Kids() {
		if k.Space != ooxml.NSA {
			continue
		}
		switch k.Local {
		case "grayscl":
			steps = append(steps, func(c imgcolor.NRGBA) imgcolor.NRGBA {
				l := luma(c)
				return imgcolor.NRGBA{R: l, G: l, B: l, A: c.A}
			})
		case "biLevel":
			thresh := max(0, min(percentAttr(k, "thresh", 0), 1))
			steps = append(steps, func(c imgcolor.NRGBA) imgcolor.NRGBA {
				if float64(luma(c))/255 >= thresh {
					return imgcolor.NRGBA{R: 255, G: 255, B: 255, A: c.A}
				}
				return imgcolor.NRGBA{A: c.A}
			})
		case "duotone":
			var duo []css.Color
			for _, kid := range k.Kids() {
				// SchemeColor reads the colour a container holds: each of
				// the duotone's two colours is one.
				if col, ok := ooxml.SchemeColor(&ooxml.Element{Children: []*ooxml.Element{kid}}, r.theme); ok && len(duo) < 2 {
					duo = append(duo, col)
				}
			}
			if len(duo) != 2 {
				continue
			}
			from, to := duo[0], duo[1]
			steps = append(steps, func(c imgcolor.NRGBA) imgcolor.NRGBA {
				t := float64(luma(c)) / 255
				mix := func(a, b float64) uint8 {
					return uint8(math.Round(max(0, min(a+float64((b-a)*t), 1)) * 255)) // at most 255: held to [0, 1]
				}
				return imgcolor.NRGBA{R: mix(from.R, to.R), G: mix(from.G, to.G), B: mix(from.B, to.B), A: c.A}
			})
		case "lum":
			bright := max(-1, min(percentAttr(k, "bright", 0), 1))
			contrast := max(-1, min(percentAttr(k, "contrast", 0), 1))
			if bright == 0 && contrast == 0 {
				continue
			}
			scale := 1 + contrast
			if contrast > 0 {
				scale = 1 / max(1-contrast, 1e-6)
			}
			shift := 0.5 + float64(bright*(1+scale)/2)
			level := func(v uint8) uint8 {
				x := float64(scale*(float64(v)/255-0.5)) + shift
				return uint8(math.Floor(float64(max(0, min(x, 1))*255) + 1e-9)) // at most 255: held to [0, 1]
			}
			steps = append(steps, func(c imgcolor.NRGBA) imgcolor.NRGBA {
				return imgcolor.NRGBA{R: level(c.R), G: level(c.G), B: level(c.B), A: c.A}
			})
		}
	}
	if len(steps) == 0 {
		return pic
	}
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
			for _, step := range steps {
				c = step(c)
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

// luma returns the Rec. 709 luma of c's sRGB channels, rounded to a
// level, as Excel reads a pixel's brightness for its colour effects.
func luma(c imgcolor.NRGBA) uint8 {
	l := float64(0.2126*float64(c.R)) + float64(0.7152*float64(c.G)) + float64(0.0722*float64(c.B))
	return uint8(math.Round(max(0, min(l, 255)))) // at most 255: held to [0, 255]
}

// percentAttr returns the ST_PositiveFixedPercentage or ST_FixedPercentage
// attribute local of e as a fraction (thousandths of a per cent, 100000
// for 1), def when e does not state it or it does not parse.
func percentAttr(e *ooxml.Element, local string, def float64) float64 {
	v, ok := e.Attr(local)
	if !ok {
		return def
	}
	n, ok := ooxml.Int(v)
	if !ok {
		return def
	}
	return float64(n) / 100000
}
