package odf

import (
	"context"
	"image"
	imgcolor "image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// The adjustments LibreOffice adds for a watermark picture (vcl's
// WATERMARK_LUM_OFFSET and WATERMARK_CON_OFFSET), in percent.
const (
	watermarkLuminance = 50
	watermarkContrast  = -70
)

// AdjustPicture returns the picture pic as the graphic properties p draw
// it (ODF 1.3, Part 3, 20.62 draw:color-mode, 20.97 draw:luminance, 20.69
// draw:contrast, 20.130 draw:red, 20.94 draw:green, 20.58 draw:blue):
// greyscale turns it grey, mono black and white at half luminance,
// watermark lightens it and flattens its contrast, a luminance or a
// contrast shifts every channel and a channel percentage its own channel,
// as LibreOffice adjusts a graphic (vcl's Bitmap::Convert to N8BitGreys
// and N1BitThreshold, then Bitmap::Adjust); pic itself when nothing
// applies. A raster picture is redrawn; a vector picture has the flat
// colours of its paths and text mapped alike, its shadings and images
// kept.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf127901's greyscale, mono and watermark
//     pictures print grey, black and white, and pale.
//   - libreoffice/odt: odfexport/DUMMY2B.odt prints its 2 by 2 picture of
//     luminance 50% and contrast 50% with each channel v at about 1.98 v +
//     1.5 (74 at 148, 99 at 197, 186 at 255), and its black-stroked SVG of
//     luminance 0%, contrast 0% and red 100% stroked red, where
//     odfexport/DUMMY2.odt prints the same pictures unadjusted.
func AdjustPicture(ctx context.Context, pic *layout.Picture, p Props) *layout.Picture {
	if pic == nil {
		return nil
	}
	mode := p.Get("draw:color-mode")
	percent := func(name string) float64 {
		v, _ := Percent(p.Get(name))
		return max(-100, min(float64(v*100), 100))
	}
	lum, con := percent("draw:luminance"), percent("draw:contrast")
	if mode == "watermark" {
		lum, con = lum+watermarkLuminance, con+watermarkContrast
	}
	lum, con = max(-100, min(lum, 100)), max(-100, min(con, 100))
	ch := [3]float64{percent("draw:red"), percent("draw:green"), percent("draw:blue")}
	grey := mode == "greyscale" || mode == "mono"
	if !grey && lum == 0 && con == 0 && ch == [3]float64{} {
		return pic
	}
	// Bitmap::Adjust's linear map of each channel.
	scale := 1.0
	if con < 0 {
		scale = (128 + float64(1.28*con)) / 128
	} else if con > 0 {
		scale = 128 / (128 - float64(1.27*con))
	}
	offset := float64(lum*2.55) + 128 - float64(scale*128)
	var table [3][256]uint8
	for c := range table {
		off := float64(ch[c]*2.55) + offset
		for v := range table[c] {
			table[c][v] = uint8(math.Round(max(0, min(255, float64(float64(v)*scale)+off))))
		}
	}
	mapRGB := func(r, g, b uint8) (uint8, uint8, uint8) {
		if grey {
			// vcl's luminance: (76 R + 151 G + 29 B) / 256.
			l := uint8((uint32(r)*76 + uint32(g)*151 + uint32(b)*29) >> 8) //nolint:gosec // at most 255: the weights sum to 256
			if mode == "mono" && l >= 128 {
				l = 255
			} else if mode == "mono" {
				l = 0
			}
			r, g, b = l, l, l
		}
		return table[0][r], table[1][g], table[2][b]
	}
	if pic.Vector != nil {
		draw := pic.Vector
		out := *pic
		out.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
			return recolored(draw(ctx, r), mapRGB, 0)
		}
		return &out
	}
	if pic.Source == nil {
		return pic
	}
	src, err := pic.Source.Decode(ctx)
	if err != nil {
		return pic
	}
	b := src.Bounds()
	if err := typeset.ChargeImage(ctx, b.Dx(), b.Dy(), imgcolor.NRGBAModel); err != nil {
		// The refusal stops the conversion, which reports it.
		typeset.ReleaseImage(ctx, src)
		return pic
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := imgcolor.NRGBAModel.Convert(src.At(x, y)).(imgcolor.NRGBA)
			c.R, c.G, c.B = mapRGB(c.R, c.G, c.B)
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, c)
		}
	}
	enc, err := typeset.EncodeImage(ctx, out)
	typeset.ReleaseImage(ctx, src)
	if err != nil {
		return pic
	}
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: pic.NoRatio, Source: enc}
}

// recolored returns copies of frags whose flat fill, stroke and text
// colours go through m, groups followed to [layout.MaxVectorDepth].
func recolored(frags []typeset.Fragment, m func(r, g, b uint8) (uint8, uint8, uint8), depth int) []typeset.Fragment {
	col := func(c typeset.Color) typeset.Color {
		r, g, b := m(channel(c.R), channel(c.G), channel(c.B))
		return typeset.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: c.A}
	}
	out := make([]typeset.Fragment, len(frags))
	for i, f := range frags {
		switch v := f.(type) {
		case *typeset.Path:
			p := *v
			if v.Fill != nil && v.Fill.Shading == nil && v.Fill.Tiling == nil {
				fl := *v.Fill
				fl.Color = col(fl.Color)
				p.Fill = &fl
			}
			if v.Stroke != nil {
				st := *v.Stroke
				st.Color = col(st.Color)
				p.Stroke = &st
			}
			out[i] = &p
		case *typeset.Text:
			t := *v
			t.Color = col(t.Color)
			out[i] = &t
		case *typeset.Group:
			g := *v
			if depth < layout.MaxVectorDepth {
				g.Content = recolored(v.Content, m, depth+1)
			}
			out[i] = &g
		default:
			out[i] = f
		}
	}
	return out
}

// channel returns the colour component v of [0, 1] in 8 bits.
func channel(v float64) uint8 {
	return uint8(math.Round(max(0, min(1, v)) * 255))
}
