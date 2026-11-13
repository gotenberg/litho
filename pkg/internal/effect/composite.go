package effect

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// Operator is a Porter-Duff operator of Filter Effects 1, 9.8
// feComposite (Compositing 1, 9.1), the source A put on the destination
// B.
type Operator uint8

const (
	// Over is A over B: A, and B where A is not.
	Over Operator = iota
	// In is A where B is.
	In
	// Out is A where B is not.
	Out
	// Atop is A where B is, and B.
	Atop
	// Xor is A where B is not, and B where A is not.
	Xor
)

// Composite puts src on dst by op, pixel by pixel, in place into dst,
// both premultiplied: each channel A times Fa plus B times Fb, Fa and Fb
// the operator's fractions of the two alphas (Compositing 1, 9.1). The
// surfaces share format and size; on coverage surfaces the one channel
// is the alpha.
func Composite(ctx context.Context, dst, src *Surface, op Operator) error {
	if err := same("composite", dst, src); err != nil {
		return err
	}
	ch := int(dst.F)
	for i := 0; i < len(dst.Pix); i += ch {
		if i%(1<<22) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		a, b := src.Pix[i:i+ch:i+ch], dst.Pix[i:i+ch:i+ch]
		aa, ab := uint32(a[ch-1]), uint32(b[ch-1])
		var fa, fb uint32
		switch op {
		case Over:
			fa, fb = 255, 255-aa
		case In:
			fa, fb = ab, 0
		case Out:
			fa, fb = 255-ab, 0
		case Atop:
			fa, fb = ab, 255-aa
		case Xor:
			fa, fb = 255-ab, 255-aa
		}
		for c := range ch {
			b[c] = uint8(min(div255(uint32(a[c])*fa+uint32(b[c])*fb), 255)) //nolint:gosec // clamped to a byte
		}
	}
	return nil
}

// arithmeticLimit bounds an arithmetic coefficient. Anti-DoS guard, not
// spec-mandated: past 2^15 every result saturates already, and the bound
// keeps the fixed-point sums within 64 bits.
const arithmeticLimit = 1 << 15

// Arithmetic puts src (i1) on dst (i2) as k1 i1 i2 + k2 i1 + k3 i2 + k4,
// channel by channel on premultiplied values in [0, 1], clamped to [0,
// 1] and each colour to the alpha (Filter Effects 1, 9.8 feComposite,
// operator arithmetic), in place into dst. The coefficients are rounded
// to 2^-16; one that is not finite reads as 0, one past 2^15 is clamped.
func Arithmetic(ctx context.Context, dst, src *Surface, k1, k2, k3, k4 float64) error {
	if err := same("arithmetic", dst, src); err != nil {
		return err
	}
	q := func(k float64) int64 {
		if !numeric.Finite(k) {
			return 0
		}
		return int64(math.Round(min(max(k, -arithmeticLimit), arithmeticLimit) * 65536)) // fits int64: within 2^31
	}
	q1, q2, q3, q4 := q(k1), q(k2), q(k3), q(k4)
	ch := int(dst.F)
	const d = 255 * 65536
	for i := 0; i < len(dst.Pix); i += ch {
		if i%(1<<22) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		a, b := src.Pix[i:i+ch:i+ch], dst.Pix[i:i+ch:i+ch]
		for c := range ch {
			x, y := int64(a[c]), int64(b[c])
			// In units of 1/(255 65,536): k1 x y / 255 + k2 x + k3 y + k4 255,
			// all over 255 once more.
			v := q1*x*y + 255*(q2*x+q3*y) + 255*255*q4
			b[c] = uint8(min(max(roundDivFloor(v, d), 0), 255)) //nolint:gosec // clamped to a byte
		}
		if ch == 4 {
			for c := range 3 {
				b[c] = min(b[c], b[3])
			}
		}
	}
	return nil
}

// roundDivFloor returns v/d to the nearest integer, halves up, d > 0.
func roundDivFloor(v, d int64) int64 {
	return floorDiv(v+d/2, d)
}

// Merge puts each of srcs over dst in order, the last on top (Filter
// Effects 1, 9.16 feMerge).
func Merge(ctx context.Context, dst *Surface, srcs ...*Surface) error {
	for _, s := range srcs {
		if err := Composite(ctx, dst, s, Over); err != nil {
			return err
		}
	}
	return nil
}

// BlendMode is a blend mode of Compositing 1, 5.
type BlendMode uint8

// The blend modes of Compositing 1, 5.3 (separable) and 5.4
// (non-separable), the sixteen of ISO 32000-1, 11.3.5.
const (
	Normal BlendMode = iota
	Multiply
	Screen
	Overlay
	Darken
	Lighten
	ColorDodge
	ColorBurn
	HardLight
	SoftLight
	Difference
	Exclusion
	Hue
	Saturation
	ColorMode
	Luminosity
)

// unitQ is 1 in the fixed point the blend functions work in: 2^16.
const unitQ = 1 << 16

// Blend blends src (the source) onto dst (the backdrop) by mode, in place
// into dst, both premultiplied RGBA (Compositing 1, 5.1, with source-over
// compositing, 9.1.4): co = cs (1 - ab) + cb (1 - as) + as ab B(Cb, Cs),
// the blend function B on colour that is not premultiplied, in fixed
// point of 2^-16, rounded once per channel.
func Blend(ctx context.Context, dst, src *Surface, mode BlendMode) error {
	if err := same("blend", dst, src); err != nil {
		return err
	}
	if err := want("blend", dst, RGBA); err != nil {
		return err
	}
	const d = 255 * unitQ
	for i := 0; i < len(dst.Pix); i += 4 {
		if i%(1<<22) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		s, b := src.Pix[i:i+4:i+4], dst.Pix[i:i+4:i+4]
		as, ab := int64(s[3]), int64(b[3])
		if as == 0 {
			continue
		}
		var cs, cb [3]int64
		for c := range 3 {
			cs[c] = min(int64(s[c])*unitQ/as, unitQ)
			if ab > 0 {
				cb[c] = min(int64(b[c])*unitQ/ab, unitQ)
			}
		}
		mixed := blend(mode, cb, cs)
		for c := range 3 {
			v := int64(s[c])*(255-ab)*unitQ + int64(b[c])*(255-as)*unitQ + as*ab*mixed[c]
			b[c] = uint8(min(max((v+d/2)/d, 0), 255)) //nolint:gosec // clamped to a byte
		}
		b[3] = uint8(as + ab - int64(div255(uint32(as*ab)))) //nolint:gosec // a union of two bytes
	}
	return nil
}

// blend returns B(cb, cs) for each channel, channels in [0, unitQ].
func blend(mode BlendMode, cb, cs [3]int64) [3]int64 {
	switch mode {
	case Hue:
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case Saturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case ColorMode:
		return setLum(cs, lum(cb))
	case Luminosity:
		return setLum(cb, lum(cs))
	case Normal, Multiply, Screen, Overlay, Darken, Lighten, ColorDodge, ColorBurn,
		HardLight, SoftLight, Difference, Exclusion:
	}
	var out [3]int64
	for c := range 3 {
		out[c] = separable(mode, cb[c], cs[c])
	}
	return out
}

// separable returns the separable blend function of Compositing 1, 5.3
// for one channel, b the backdrop and s the source, in [0, unitQ].
func separable(mode BlendMode, b, s int64) int64 {
	const u = unitQ
	switch mode {
	case Multiply:
		return b * s / u
	case Screen:
		return b + s - b*s/u
	case Overlay:
		return separable(HardLight, s, b)
	case Darken:
		return min(b, s)
	case Lighten:
		return max(b, s)
	case ColorDodge:
		switch {
		case b == 0:
			return 0
		case s >= u:
			return u
		}
		return min(u, b*u/(u-s))
	case ColorBurn:
		switch {
		case b >= u:
			return u
		case s == 0:
			return 0
		}
		return u - min(u, (u-b)*u/s)
	case HardLight:
		if s <= u/2 {
			return b * (2 * s) / u
		}
		return separable(Screen, b, 2*s-u)
	case SoftLight:
		if s <= u/2 {
			return b - (u-2*s)*b/u*(u-b)/u
		}
		var dd int64
		if b <= u/4 {
			dd = ((16*b-12*u)*b/u + 4*u) * b / u
		} else {
			dd = isqrt(b * u)
		}
		return b + (2*s-u)*(dd-b)/u
	case Difference:
		return abs64(b - s)
	case Exclusion:
		return b + s - 2*b*s/u
	case Normal, Hue, Saturation, ColorMode, Luminosity:
	}
	return s
}

// lum returns the luminosity of c, 0.3 R + 0.59 G + 0.11 B (Compositing
// 1, 5.4).
func lum(c [3]int64) int64 { return (30*c[0] + 59*c[1] + 11*c[2]) / 100 }

// setLum returns c moved to luminosity l and clipped into gamut
// (Compositing 1, 5.4, SetLum and ClipColor).
func setLum(c [3]int64, l int64) [3]int64 {
	d := l - lum(c)
	for i := range c {
		c[i] += d
	}
	l = lum(c)
	n, x := min(c[0], c[1], c[2]), max(c[0], c[1], c[2])
	if n < 0 && l != n {
		for i := range c {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
	}
	if x > unitQ && x != l {
		for i := range c {
			c[i] = l + (c[i]-l)*(unitQ-l)/(x-l)
		}
	}
	return c
}

// sat returns the saturation of c, its largest channel less its smallest.
func sat(c [3]int64) int64 { return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2]) }

// setSat returns c with saturation s, its hue kept (Compositing 1, 5.4,
// SetSat).
func setSat(c [3]int64, s int64) [3]int64 {
	lo, mid, hi := 0, 1, 2
	if c[lo] > c[mid] {
		lo, mid = mid, lo
	}
	if c[mid] > c[hi] {
		mid, hi = hi, mid
	}
	if c[lo] > c[mid] {
		lo, mid = mid, lo
	}
	var out [3]int64
	if c[hi] > c[lo] {
		out[mid] = (c[mid] - c[lo]) * s / (c[hi] - c[lo])
		out[hi] = s
	}
	return out
}

// isqrt returns the integer square root of n >= 0.
func isqrt(n int64) int64 {
	r := int64(math.Sqrt(float64(n)))
	for r > 0 && r*r > n {
		r--
	}
	for (r+1)*(r+1) <= n {
		r++
	}
	return r
}
