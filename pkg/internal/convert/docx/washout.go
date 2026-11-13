package docx

import (
	"image"
	imgcolor "image/color"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// brightGain is how far Word lifts a channel per unit of brightness, as a
// share of full scale: a measurement of Word, which the standard leaves
// to the application (ECMA-376 Part 1, 20.1.8.42 lum).
//
// # Corpus evidence
//
//   - litho/docx doc-watermark-picture: Word prints both washed-out
//     pictures, one of lum bright 70% and contrast -70%, the other of VML
//     gain 0.3 and blacklevel 0.35, as every channel v of 255 mapped to
//     205 + 0.304 v, clamped: the contrast scales about mid-grey by 0.3,
//     the brightness adds 0.454 of full scale, 0.649 a unit.
const brightGain = 0.649

// adjustLum returns the raster picture pic with Word's brightness and
// contrast applied to every colour channel v in [0, 1]: (1 + contrast)
// (v - 0.5) + 0.5 + brightGain bright, clamped; pic itself for no change
// or a vector picture. A VML picture's gain and blacklevel (ECMA-376 Part
// 4, 14.1.2.11 imagedata) reach it as a contrast of gain minus 1 and a
// brightness of twice the black level, as Word writes the same washout
// both ways. Only the washout Word's watermark dialog writes has been
// measured.
func (d *document) adjustLum(pic *layout.Picture, bright, contrast float64) *layout.Picture {
	if pic == nil || pic.Source == nil || (bright == 0 && contrast == 0) || !numeric.Finite(bright) || !numeric.Finite(contrast) {
		return pic
	}
	src, err := pic.Source.Decode(d.ctx)
	if err != nil {
		if d.stop(err) {
			return nil
		}
		return pic
	}
	var table [256]uint8
	for v := range table {
		f := float64((1+contrast)*(float64(v)/255-0.5)) + 0.5 + float64(brightGain*bright)
		table[v] = uint8(math.Round(max(0, min(1, f)) * 255))
	}
	b := src.Bounds()
	if err := typeset.ChargeImage(d.ctx, b.Dx(), b.Dy(), imgcolor.NRGBAModel); err != nil {
		typeset.ReleaseImage(d.ctx, src)
		d.stop(err)
		return pic
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := imgcolor.NRGBAModel.Convert(src.At(x, y)).(imgcolor.NRGBA) //nolint:forcetypeassert // NRGBAModel converts to NRGBA.
			c.R, c.G, c.B = table[c.R], table[c.G], table[c.B]
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, c)
		}
	}
	enc, err := typeset.EncodeImage(d.ctx, out)
	typeset.ReleaseImage(d.ctx, src)
	if err != nil {
		d.stop(err)
		return pic
	}
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: pic.NoRatio, Source: enc}
}

// vmlLum returns the brightness and contrast of a v:imagedata's blacklevel
// and gain (Part 4, 14.1.2.11, fractions in 65536ths with an "f" suffix),
// as [document.adjustLum] reads them.
func vmlLum(img *ooxml.Element) (bright, contrast float64) {
	frac := func(name string) (float64, bool) {
		v := strings.TrimSpace(attrOr(img, name))
		if v == "" {
			return 0, false
		}
		f, ok := ooxml.Measure(strings.TrimSuffix(v, "f"), 1)
		if ok && strings.HasSuffix(v, "f") {
			f /= 65536
		}
		return f, ok && numeric.Finite(f)
	}
	if g, ok := frac("gain"); ok {
		contrast = max(-1, min(g-1, 1))
	}
	if bl, ok := frac("blacklevel"); ok {
		bright = max(-1, min(2*bl, 1))
	}
	return bright, contrast
}
