package odp

import (
	"context"
	"image"
	imgcolor "image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// The adjustments Impress adds for a watermark picture (vcl's
// WATERMARK_LUM_OFFSET and WATERMARK_CON_OFFSET), in percent.
const (
	watermarkLuminance = 50
	watermarkContrast  = -70
)

// adjusted returns the raster picture pic as the graphic properties p
// draw it (ODF 1.3, Part 3, 20.62 draw:color-mode, 20.97 draw:luminance,
// 20.69 draw:contrast): greyscale turns it grey, mono black and white at
// half luminance, watermark lightens it and flattens its contrast, and a
// luminance or contrast shifts every channel, as Impress adjusts a
// graphic (vcl's Bitmap::Convert to N8BitGreys and N1BitThreshold, then
// Bitmap::Adjust); pic itself when nothing applies or it is a vector.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf127901's greyscale, mono and watermark pictures print
//     grey, black and white, and pale.
func adjusted(ctx context.Context, pic *layout.Picture, p odf.Props) *layout.Picture {
	mode := p.Get("draw:color-mode")
	lum, _ := odf.Percent(p.Get("draw:luminance"))
	con, _ := odf.Percent(p.Get("draw:contrast"))
	lum, con = lum*100, con*100
	if mode == "watermark" {
		lum, con = lum+watermarkLuminance, con+watermarkContrast
	}
	lum, con = max(-100, min(lum, 100)), max(-100, min(con, 100))
	if pic == nil || pic.Source == nil || (mode != "greyscale" && mode != "mono" && lum == 0 && con == 0) {
		return pic
	}
	src, err := pic.Source.Decode(ctx)
	if err != nil {
		return pic
	}
	// Bitmap::Adjust's linear map of every channel.
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
			switch mode {
			case "greyscale", "mono":
				// vcl's luminance: (76 R + 151 G + 29 B) / 256.
				l := uint8((uint32(c.R)*76 + uint32(c.G)*151 + uint32(c.B)*29) >> 8) //nolint:gosec // at most 255: the weights sum to 256
				if mode == "mono" && l >= 128 {
					l = 255
				} else if mode == "mono" {
					l = 0
				}
				c.R, c.G, c.B = l, l, l
			}
			c.R, c.G, c.B = table[c.R], table[c.G], table[c.B]
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, c)
		}
	}
	enc, err := compose.EncodeImage(ctx, out)
	if err != nil {
		return pic
	}
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: pic.NoRatio, Source: enc}
}
