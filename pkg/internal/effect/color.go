package effect

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Color is a colour of 8-bit channels, not premultiplied.
type Color struct {
	R, G, B, A uint8
}

// ColorOf returns c's channels, each clamped to [0, 1] and rounded to a
// byte; NaN reads as 0.
func ColorOf(c typeset.Color) Color {
	return Color{byteOf(c.R), byteOf(c.G), byteOf(c.B), byteOf(c.A)}
}

// byteOf returns v in [0, 1] as a byte, rounded; NaN reads as 0.
func byteOf(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	return uint8(math.Round(min(v, 1) * 255)) //nolint:gosec // within [0, 255]
}

// div255 returns v/255 rounded, for v in [0, 255 * 255].
func div255(v uint32) uint32 { return (v + 127) / 255 }

// premultiply returns c times a over 255, rounded.
func premultiply(c, a uint8) uint8 { return uint8(div255(uint32(c) * uint32(a))) } //nolint:gosec // at most 255

// unpremultiply returns c over a as a byte, rounded, 0 when a is 0.
func unpremultiply(c, a uint8) uint8 {
	if a == 0 {
		return 0
	}
	return uint8(min((uint32(c)*255+uint32(a)/2)/uint32(a), 255)) //nolint:gosec // clamped to a byte
}

// Flood fills s with c, in place (Filter Effects 1, 9.13 feFlood): an
// RGBA surface with c premultiplied, a coverage surface with c's alpha.
func Flood(ctx context.Context, s *Surface, c Color) error {
	px := []uint8{c.A}
	if s.F == RGBA {
		px = []uint8{premultiply(c.R, c.A), premultiply(c.G, c.A), premultiply(c.B, c.A), c.A}
	}
	for i := 0; i < len(s.Pix); i += len(px) {
		if i%(1<<20) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		copy(s.Pix[i:], px)
	}
	return nil
}

// Tint returns an RGBA surface of cov's size and grid in the colour c,
// its alpha cov's coverage times c's alpha: a shadow or a glow drawn from
// its silhouette. The new surface is charged to the budget ctx carries.
func Tint(ctx context.Context, cov *Surface, c Color) (*Surface, error) {
	if err := want("tint", cov, Coverage); err != nil {
		return nil, err
	}
	out, err := New(ctx, RGBA, cov.W, cov.H, cov.Grid)
	if err != nil {
		return nil, err
	}
	for i, v := range cov.Pix {
		if i%(1<<20) == 0 {
			if err := ctx.Err(); err != nil {
				out.Release()
				return nil, err
			}
		}
		a := premultiply(v, c.A)
		p := out.Pix[4*i : 4*i+4 : 4*i+4]
		p[0], p[1], p[2], p[3] = premultiply(c.R, a), premultiply(c.G, a), premultiply(c.B, a), a
	}
	return out, nil
}

// AlphaOf returns a coverage surface of s's alpha, s's size and grid:
// the SourceAlpha of Filter Effects 1, 9.2. It is charged to the budget
// ctx carries.
func AlphaOf(ctx context.Context, s *Surface) (*Surface, error) {
	out, err := New(ctx, Coverage, s.W, s.H, s.Grid)
	if err != nil {
		return nil, err
	}
	if s.F == Coverage {
		copy(out.Pix, s.Pix)
		return out, nil
	}
	for i := range out.Pix {
		out.Pix[i] = s.Pix[4*i+3]
	}
	return out, nil
}

// Threshold sets every pixel of the coverage surface s at or above level
// to fully covered and every other to uncovered, in place: with level 1,
// the silhouette of an antialiased coverage, as an Office glow and soft
// edge begin.
func Threshold(s *Surface, level uint8) error {
	if err := want("threshold", s, Coverage); err != nil {
		return err
	}
	for i, v := range s.Pix {
		if v >= level {
			s.Pix[i] = 255
		} else {
			s.Pix[i] = 0
		}
	}
	return nil
}

// Matrix is a colour matrix of Filter Effects 1, 9.6 feColorMatrix: four
// rows, red, green, blue and alpha, of five columns, the weights of red,
// green, blue and alpha and a constant, applied to colour that is not
// premultiplied, every channel in [0, 1].
type Matrix [20]float64

// matrixLimit bounds a matrix coefficient. Anti-DoS guard, not
// spec-mandated: a coefficient past 2^15 saturates every channel already,
// and the bound keeps the fixed-point sums within 64 bits.
const matrixLimit = 1 << 15

// IdentityMatrix changes no colour.
var IdentityMatrix = Matrix{1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0}

// ColorMatrix applies m to every pixel of the RGBA surface s, in place:
// unpremultiplied, each channel the row's weighted sum, clamped to [0,
// 1], then premultiplied again. The coefficients are rounded to 2^-16
// first; one that is not finite reads as 0, one past 2^15 is clamped.
func ColorMatrix(ctx context.Context, s *Surface, m Matrix) error {
	if err := want("color matrix", s, RGBA); err != nil {
		return err
	}
	var q [20]int64
	for i, v := range m {
		if !numeric.Finite(v) {
			continue
		}
		q[i] = int64(math.Round(min(max(v, -matrixLimit), matrixLimit) * 65536)) // fits int64: within 2^31
	}
	for i := 0; i < len(s.Pix); i += 4 {
		if i%(1<<22) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		p := s.Pix[i : i+4 : i+4]
		a := p[3]
		c := [4]int64{int64(unpremultiply(p[0], a)), int64(unpremultiply(p[1], a)), int64(unpremultiply(p[2], a)), int64(a)}
		var out [4]uint8
		for r := range 4 {
			row := q[5*r : 5*r+5]
			v := row[0]*c[0] + row[1]*c[1] + row[2]*c[2] + row[3]*c[3] + row[4]*255
			out[r] = uint8(min(max((v+32768)>>16, 0), 255)) //nolint:gosec // clamped to a byte
		}
		p[0], p[1], p[2], p[3] = premultiply(out[0], out[3]), premultiply(out[1], out[3]), premultiply(out[2], out[3]), out[3]
	}
	return nil
}

// mul returns a times b rounded on its own: a product the compiler may
// not fuse with an addition, so a matrix is the same on every machine.
func mul(a, b float64) float64 { return float64(a * b) }

// SaturateMatrix returns the matrix of Filter Effects 1, 9.6 type
// saturate, the CSS saturate(s) of Filter Effects 1, 13.1.
func SaturateMatrix(s float64) Matrix {
	return Matrix{
		0.213 + mul(0.787, s), 0.715 - mul(0.715, s), 0.072 - mul(0.072, s), 0, 0,
		0.213 - mul(0.213, s), 0.715 + mul(0.285, s), 0.072 - mul(0.072, s), 0, 0,
		0.213 - mul(0.213, s), 0.715 - mul(0.715, s), 0.072 + mul(0.928, s), 0, 0,
		0, 0, 0, 1, 0,
	}
}

// HueRotateMatrix returns the matrix of Filter Effects 1, 9.6 type
// hueRotate, the CSS hue-rotate(deg) of Filter Effects 1, 13.1, its sine
// and cosine from [detSincos].
func HueRotateMatrix(deg float64) Matrix {
	s, c := detSincos(deg)
	if math.IsNaN(s) {
		return IdentityMatrix
	}
	e := func(k, kc, ks float64) float64 { return k + mul(c, kc) + mul(s, ks) }
	return Matrix{
		e(0.213, 0.787, -0.213), e(0.715, -0.715, -0.715), e(0.072, -0.072, 0.928), 0, 0,
		e(0.213, -0.213, 0.143), e(0.715, 0.285, 0.140), e(0.072, -0.072, -0.283), 0, 0,
		e(0.213, -0.213, -0.787), e(0.715, -0.715, 0.715), e(0.072, 0.928, 0.072), 0, 0,
		0, 0, 0, 1, 0,
	}
}

// LuminanceToAlphaMatrix returns the matrix of Filter Effects 1, 9.6
// type luminanceToAlpha: no colour, the alpha the luminance.
func LuminanceToAlphaMatrix() Matrix {
	return Matrix{15: 0.2125, 16: 0.7154, 17: 0.0721}
}

// GrayscaleMatrix returns the matrix of the CSS grayscale(amount) of
// Filter Effects 1, 13.1, amount clamped to [0, 1].
func GrayscaleMatrix(amount float64) Matrix {
	k := 1 - unit(amount)
	return Matrix{
		0.2126 + mul(0.7874, k), 0.7152 - mul(0.7152, k), 0.0722 - mul(0.0722, k), 0, 0,
		0.2126 - mul(0.2126, k), 0.7152 + mul(0.2848, k), 0.0722 - mul(0.0722, k), 0, 0,
		0.2126 - mul(0.2126, k), 0.7152 - mul(0.7152, k), 0.0722 + mul(0.9278, k), 0, 0,
		0, 0, 0, 1, 0,
	}
}

// SepiaMatrix returns the matrix of the CSS sepia(amount) of Filter
// Effects 1, 13.1, amount clamped to [0, 1].
func SepiaMatrix(amount float64) Matrix {
	k := 1 - unit(amount)
	return Matrix{
		0.393 + mul(0.607, k), 0.769 - mul(0.769, k), 0.189 - mul(0.189, k), 0, 0,
		0.349 - mul(0.349, k), 0.686 + mul(0.314, k), 0.168 - mul(0.168, k), 0, 0,
		0.272 - mul(0.272, k), 0.534 - mul(0.534, k), 0.131 + mul(0.869, k), 0, 0,
		0, 0, 0, 1, 0,
	}
}

// unit clamps v to [0, 1], NaN to 0.
func unit(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return min(v, 1)
}

// TransferKind is the type of a transfer function of Filter Effects 1,
// 9.7 feComponentTransfer.
type TransferKind uint8

const (
	// TransferIdentity changes nothing.
	TransferIdentity TransferKind = iota
	// TransferTable interpolates linearly between Values spread evenly
	// over [0, 1].
	TransferTable
	// TransferDiscrete steps through Values spread evenly over [0, 1].
	TransferDiscrete
	// TransferLinear is Slope times the channel plus Intercept.
	TransferLinear
	// TransferGamma is Amplitude times the channel to the Exponent, plus
	// Offset.
	TransferGamma
)

// TransferFunc is one channel's transfer function.
type TransferFunc struct {
	Kind                        TransferKind
	Values                      []float64
	Slope, Intercept            float64
	Amplitude, Exponent, Offset float64
}

// LUT is a channel's lookup table: entry v is what the byte v becomes.
type LUT [256]uint8

// Table returns f's lookup table: for every byte v, f(v/255) clamped to
// [0, 1] and rounded to a byte. A table or discrete function with no
// value, and a function whose result is not finite, is the identity
// there; the gamma's power comes from [detPow].
func (f TransferFunc) Table() LUT {
	var t LUT
	n := len(f.Values)
	for v := range t {
		c := float64(v) / 255
		r := c
		switch f.Kind {
		case TransferTable:
			if n == 1 {
				r = f.Values[0]
			} else if n > 1 {
				k := min(int(float64(c*float64(n-1))), n-2) // fits int: within [0, n-2]
				lo, hi := f.Values[k], f.Values[k+1]
				r = lo + mul(float64(c-float64(k)/float64(n-1))*float64(n-1), hi-lo)
			}
		case TransferDiscrete:
			if n > 0 {
				r = f.Values[min(int(float64(c*float64(n))), n-1)] // fits int: within [0, n-1]
			}
		case TransferLinear:
			r = mul(f.Slope, c) + f.Intercept
		case TransferGamma:
			r = mul(f.Amplitude, detPow(c, f.Exponent)) + f.Offset
		case TransferIdentity:
		}
		if !numeric.Finite(r) {
			r = c
		}
		t[v] = byteOf(r)
	}
	return t
}

// IdentityLUT changes no byte.
var IdentityLUT = TransferFunc{}.Table()

// Transfer applies a lookup table to each channel of the RGBA surface
// s, in place, on colour that is not premultiplied, then premultiplies
// again (Filter Effects 1, 9.7 feComponentTransfer).
func Transfer(ctx context.Context, s *Surface, r, g, b, a *LUT) error {
	if err := want("transfer", s, RGBA); err != nil {
		return err
	}
	for i := 0; i < len(s.Pix); i += 4 {
		if i%(1<<22) == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		p := s.Pix[i : i+4 : i+4]
		al := a[p[3]]
		cr, cg, cb := r[unpremultiply(p[0], p[3])], g[unpremultiply(p[1], p[3])], b[unpremultiply(p[2], p[3])]
		p[0], p[1], p[2], p[3] = premultiply(cr, al), premultiply(cg, al), premultiply(cb, al), al
	}
	return nil
}

// BrightnessTransfer returns the CSS brightness(k) of Filter Effects 1,
// 13: a linear function of slope k on the colour channels.
func BrightnessTransfer(k float64) TransferFunc {
	return TransferFunc{Kind: TransferLinear, Slope: k}
}

// ContrastTransfer returns the CSS contrast(k) of Filter Effects 1, 13.1:
// slope k about the middle grey.
func ContrastTransfer(k float64) TransferFunc {
	return TransferFunc{Kind: TransferLinear, Slope: k, Intercept: 0.5 - mul(0.5, k)}
}

// InvertTransfer returns the CSS invert(amount) of Filter Effects 1, 13.1,
// amount clamped to [0, 1].
func InvertTransfer(amount float64) TransferFunc {
	k := unit(amount)
	return TransferFunc{Kind: TransferTable, Values: []float64{k, 1 - k}}
}

// OpacityTransfer returns the CSS opacity(amount) of Filter Effects 1,
// 13 for the alpha channel, amount clamped to [0, 1].
func OpacityTransfer(amount float64) TransferFunc {
	return TransferFunc{Kind: TransferTable, Values: []float64{0, unit(amount)}}
}
