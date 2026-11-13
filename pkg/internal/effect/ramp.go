package effect

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// AlphaRamp scales every pixel of s by an opacity that runs linearly
// from a0 at (x0, y0) to a1 at (x1, y1), in pixels, along the line
// between them and constant across it, held at a0 before the start and
// a1 past the end: the fading of an Office reflection (ECMA-376 Part 1,
// 20.1.8.50 reflection: stA at stPos, endA at endPos, along fadeDir).
// Positions are rounded to 1/256 pixel and opacities, clamped to [0, 1],
// to 2^-16. A start equal to the end, or a position that is not finite,
// scales everything by a0.
func AlphaRamp(ctx context.Context, s *Surface, x0, y0, x1, y1, a0, a1 float64) error {
	q0, q1 := int64(math.Round(unit(a0)*unitQ)), int64(math.Round(unit(a1)*unitQ)) // fits int64: within [0, 2^16]
	pos := func(v float64) (int64, bool) {
		if !numeric.Finite(v) {
			return 0, false
		}
		return int64(math.Round(min(max(v, -maxPixelCoordinate), maxPixelCoordinate) * shiftUnit)), true // fits int64: within 2^28
	}
	px0, ok0 := pos(x0)
	py0, ok1 := pos(y0)
	px1, ok2 := pos(x1)
	py1, ok3 := pos(y1)
	dx, dy := px1-px0, py1-py0
	den := dx*dx + dy*dy // fits int64: 2^29 squared, twice
	flat := !ok0 || !ok1 || !ok2 || !ok3 || den == 0
	ch := int(s.F)
	stride := s.stride()
	for y := range s.H {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		cy := int64(2*y+1)*shiftUnit/2 - py0
		row := s.Pix[y*stride : (y+1)*stride]
		for x := range s.W {
			f := q0
			if !flat {
				cx := int64(2*x+1)*shiftUnit/2 - px0
				// t in [0, 1] as 2^-16: the projection on the ramp over its
				// squared length, in 128 bits, the product past 64.
				num := cx*dx + cy*dy // fits int64: 2^29 times 2^29, twice
				var t int64
				switch {
				case num <= 0:
				case num >= den:
					t = unitQ
				default:
					t = mulDiv(num, unitQ, den)
				}
				f = q0 + floorDiv((q1-q0)*t+unitQ/2, unitQ)
			}
			p := row[x*ch : x*ch+ch]
			for c := range p {
				p[c] = uint8((int64(p[c])*f + unitQ/2) >> 16) //nolint:gosec // a byte times a fraction of 1
			}
		}
	}
	return nil
}
