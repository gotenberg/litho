package raster

import (
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// Tint returns an image of m's size in the one colour c, its alpha m's
// coverage times c's alpha: a shadow or a glow ready to place as a
// [compose.Image], whose alpha becomes the image's soft mask.
func Tint(m *image.Alpha, c compose.Color) *image.NRGBA {
	b := m.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	r, g, bl, a := channel(c.R), channel(c.G), channel(c.B), unit(c.A)
	for y := 0; y < b.Dy(); y++ {
		src := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
		dst := out.Pix[y*out.Stride:]
		for x := 0; x < b.Dx(); x++ {
			dst[4*x], dst[4*x+1], dst[4*x+2] = r, g, bl
			dst[4*x+3] = byteOf(float64(src[x]) * a)
		}
	}
	return out
}

// Paint paints the colour c through the coverage m over dst, source-over
// with straight alpha, pixel by pixel from the two images' origins. The
// images share a size; the pixels of dst outside m are left as they are.
func Paint(dst *image.NRGBA, m *image.Alpha, c compose.Color) {
	b := dst.Bounds().Intersect(m.Bounds())
	sr, sg, sb, sa := unit(c.R), unit(c.G), unit(c.B), unit(c.A)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := float64(m.Pix[m.PixOffset(x, y)]) / 255 * sa
			if a == 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			p := dst.Pix[i : i+4 : i+4]
			da := float64(p[3]) / 255
			oa := a + da*(1-a)
			mix := func(s float64, d uint8) uint8 {
				return byteOf((s*a + float64(d)/255*da*(1-a)) / oa * 255)
			}
			p[0], p[1], p[2], p[3] = mix(sr, p[0]), mix(sg, p[1]), mix(sb, p[2]), byteOf(oa*255)
		}
	}
}

// Mask scales the alpha of img by the coverage of m, pixel by pixel from
// the two images' origins, in place: img faded where m is thin. The
// images share a size; the pixels of img outside m are left as they are.
func Mask(img *image.NRGBA, m *image.Alpha) {
	b := img.Bounds().Intersect(m.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := img.PixOffset(x, y) + 3
			img.Pix[i] = uint8((uint32(img.Pix[i])*uint32(m.Pix[m.PixOffset(x, y)]) + 127) / 255) //nolint:gosec // a product of two bytes over 255 is a byte
		}
	}
}

// unit clamps v to [0, 1], NaN to 0.
func unit(v float64) float64 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	return min(v, 1)
}

// channel returns the byte of the colour channel v.
func channel(v float64) uint8 {
	return byteOf(unit(v) * 255)
}
