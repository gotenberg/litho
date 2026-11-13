package effect

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// shiftUnit is the step an offset is rounded to: 1/256 pixel.
const shiftUnit = 256

// Offset moves the content of s by dx pixels right and dy down, in
// place (Filter Effects 1, 9.18 feOffset). A whole-pixel offset copies; a
// fractional one is resampled bilinearly, each pixel the weighted mean of
// the two source pixels it straddles along each axis, the weights in
// 1/256. The offset is rounded to 1/256 pixel. What moves past the edge
// is lost; what comes in is transparent black. An offset that is not
// finite leaves s alone.
func Offset(ctx context.Context, s *Surface, dx, dy float64) error {
	if !numeric.Finite(dx) || !numeric.Finite(dy) {
		return nil
	}
	qx := int64(math.Round(min(max(dx, -maxSide), maxSide) * shiftUnit)) // fits int64: within 2^24
	qy := int64(math.Round(min(max(dy, -maxSide), maxSide) * shiftUnit)) // fits int64: within 2^24
	if qx == 0 && qy == 0 {
		return nil
	}
	ch := int(s.F)
	n := max(s.W, s.H)
	var sc scratch
	defer sc.release()
	if err := sc.take(ctx, 2*int64(n)*int64(ch)); err != nil {
		return err
	}
	line := make([]uint8, n*ch)
	col := make([]uint8, s.H*ch)
	stride := s.stride()
	if qx != 0 {
		for y := range s.H {
			if y%64 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			row := s.Pix[y*stride : (y+1)*stride]
			copy(line, row)
			shift(row, line[:stride], s.W, ch, ch, qx)
		}
	}
	if qy != 0 {
		for x := range s.W {
			if x%64 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			for y := range s.H {
				copy(col[y*ch:(y+1)*ch], s.Pix[y*stride+x*ch:y*stride+x*ch+ch])
			}
			copy(line, col)
			shift(col, line[:s.H*ch], s.H, ch, ch, qy)
			for y := range s.H {
				copy(s.Pix[y*stride+x*ch:y*stride+x*ch+ch], col[y*ch:(y+1)*ch])
			}
		}
	}
	return nil
}

// shift writes into dst the n pixels of src, ch bytes a pixel step bytes
// apart, moved forward by q/256 pixels: pixel i of dst reads src at i -
// q/256, between two pixels by their weights.
func shift(dst, src []uint8, n, ch, step int, q int64) {
	whole := floorDiv(q, shiftUnit)
	frac := uint32(q - whole*shiftUnit) //nolint:gosec // in [0, 256)
	at := func(i int64, c int) uint32 {
		if i < 0 || i >= int64(n) {
			return 0
		}
		return uint32(src[int(i)*step+c]) // fits int: within the line
	}
	for i := range n {
		// Pixel i reads (1-f) of i-whole and f of i-whole-1.
		j := int64(i) - whole
		for c := range ch {
			v := (256-frac)*at(j, c) + frac*at(j-1, c)
			dst[i*step+c] = uint8((v + 128) >> 8) //nolint:gosec // a mean of two bytes
		}
	}
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// transformUnit is the fixed point of a resampling transform's
// coefficients: 2^20, so a coefficient's rounding moves a pixel 4,096
// pixels away by under 1/256 pixel.
const transformUnit = 1 << 20

// maxTransformScale bounds a coefficient of the inverse of a resampling
// transform. Anti-DoS guard, not spec-mandated: 256 source pixels to a
// destination pixel shrink past what any effect draws, and the bound
// keeps the fixed-point products and their sum within 63 bits.
const maxTransformScale = 1 << 8

// Transform draws src into dst through the affine map m from src's
// pixels to dst's: dst's pixel whose centre is at p reads src at m^-1(p),
// interpolated bilinearly between the four pixels around it, the weights
// in 1/256 pixel; what falls outside src is transparent black. The two
// surfaces share a format; dst is overwritten. The inverse of m is
// computed once and rounded to 2^-20, so the result does not depend on
// the machine. A map that is not finite or not invertible leaves dst
// transparent.
//
// It serves the reflection of an Office shape (a flip, a scale, a
// distance), the skew and scale of an Office shadow (ECMA-376 Part 1,
// 20.1.8.45 outerShdw: sx, sy, kx, ky), and the transform of a filter's
// input.
func Transform(ctx context.Context, dst, src *Surface, m typeset.Matrix) error {
	if dst.F != src.F {
		return fmt.Errorf("transform: %w", ErrFormat)
	}
	clear(dst.Pix)
	det := float64(m.A*m.D) - float64(m.B*m.C)
	if !numeric.Finite(det) || det == 0 {
		return nil
	}
	// The inverse: x = (D (X - E) - C (Y - F)) / det, y = (A (Y - F) - B
	// (X - E)) / det.
	ia, ib, ic, id := m.D/det, -m.B/det, -m.C/det, m.A/det
	ie := -(float64(ia*m.E) + float64(ic*m.F))
	ifv := -(float64(ib*m.E) + float64(id*m.F))
	q := func(v float64, bound float64) (int64, bool) {
		if !(math.Abs(v) <= bound) {
			return 0, false
		}
		return int64(math.Round(v * transformUnit)), true // fits int64: within 2^41
	}
	qa, ok1 := q(ia, maxTransformScale)
	qb, ok2 := q(ib, maxTransformScale)
	qc, ok3 := q(ic, maxTransformScale)
	qd, ok4 := q(id, maxTransformScale)
	qe, ok5 := q(ie, 2*maxPixelCoordinate)
	qf, ok6 := q(ifv, 2*maxPixelCoordinate)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return nil
	}
	ch := int(src.F)
	ss, ds := src.stride(), dst.stride()
	at := func(x, y int64, c int) uint32 {
		if x < 0 || y < 0 || x >= int64(src.W) || y >= int64(src.H) {
			return 0
		}
		return uint32(src.Pix[int(y)*ss+int(x)*ch+c]) // fits int: within src
	}
	const half = transformUnit / 2
	for y := range dst.H {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		// The centre of the pixel, (x + 1/2, y + 1/2) in 1/65,536 pixel,
		// mapped back, less the half pixel to the centres of src's pixels.
		py := int64(2*y+1) << 15
		for x := range dst.W {
			px := int64(2*x+1) << 15
			sx := (qa*px+qc*py)>>16 + qe - half // fits int64: 2^28 times 2^33, twice
			sy := (qb*px+qd*py)>>16 + qf - half // fits int64: 2^28 times 2^33, twice
			x0, y0 := floorDiv(sx, transformUnit), floorDiv(sy, transformUnit)
			fx := uint32((sx - x0*transformUnit) >> 12) //nolint:gosec // in [0, 256)
			fy := uint32((sy - y0*transformUnit) >> 12) //nolint:gosec // in [0, 256)
			for c := range ch {
				top := (256-fx)*at(x0, y0, c) + fx*at(x0+1, y0, c)
				bot := (256-fx)*at(x0, y0+1, c) + fx*at(x0+1, y0+1, c)
				dst.Pix[y*ds+x*ch+c] = uint8(((256-fy)*top + fy*bot + 1<<15) >> 16) //nolint:gosec // a mean of bytes
			}
		}
	}
	return nil
}

// Tile fills dst with copies of the pixels of src within the rectangle
// [x0, x1) by [y0, y1), the copy at that rectangle's own place and the
// others repeated from it in every direction (Filter Effects 1, 9.20
// feTile). The two surfaces share a format and a pixel grid. An empty
// rectangle, or one not within src, leaves dst transparent.
func Tile(ctx context.Context, dst, src *Surface, x0, y0, x1, y1 int) error {
	if dst.F != src.F {
		return fmt.Errorf("tile: %w", ErrFormat)
	}
	clear(dst.Pix)
	if x0 < 0 || y0 < 0 || x1 > src.W || y1 > src.H || x0 >= x1 || y0 >= y1 {
		return nil
	}
	ch := int(src.F)
	tw, th := x1-x0, y1-y0
	ss, ds := src.stride(), dst.stride()
	for y := range dst.H {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		sy := y0 + int(mod(int64(y-y0), int64(th))) // fits int: within the rectangle
		for x := range dst.W {
			sx := x0 + int(mod(int64(x-x0), int64(tw))) // fits int: within the rectangle
			copy(dst.Pix[y*ds+x*ch:y*ds+x*ch+ch], src.Pix[sy*ss+sx*ch:sy*ss+sx*ch+ch])
		}
	}
	return nil
}

func mod(a, b int64) int64 {
	return ((a % b) + b) % b
}
