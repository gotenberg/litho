package blend

import (
	"context"
	"fmt"
	"image"
)

// CMYK returns the group's accumulated colour as 8-bit amounts of ink, the
// group composited over paper that takes none: an image of the group's
// bounds, its origin at (0, 0). Paper is the medium a CMYK device prints on,
// so where the group's alpha falls short of 1 its inks thin out in
// proportion (ISO 32000-1, 11.4.7, the page group composited with the
// medium). A group that does not blend in a CMYK space returns
// [ErrNotCMYK].
func (g *Group) CMYK(ctx context.Context) (*image.CMYK, error) {
	if g.space.Kind() != CMYK {
		return nil, ErrNotCMYK
	}
	w, h := g.bounds.Dx(), g.bounds.Dy()
	out := image.NewCMYK(image.Rect(0, 0, w, h))
	for y := range h {
		if y%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("reading a group as CMYK: %w", err)
			}
		}
		for x := range w {
			i := y*w + x
			p := g.color.Pix[g.stride*i : g.stride*(i+1)]
			a := p[4]
			o := out.Pix[4*i : 4*i+4]
			// The plane holds each ink's complement premultiplied by the
			// alpha ([Space.store]), and paper a complement of 1, so the
			// composite's complement is p + 1 - a and its ink a - p.
			for k := range 4 {
				o[k] = inkByte(a - p[k])
			}
		}
	}
	return out, nil
}

// inkByte rounds an amount of ink in [0, 1] to an 8-bit level.
func inkByte(v float32) uint8 {
	switch {
	case !(v > 0):
		return 0
	case v >= 1:
		return 255
	}
	return uint8(v*255 + 0.5)
}
