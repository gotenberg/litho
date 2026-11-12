package effect

import (
	"context"
	"math"
)

// Kernel is a symmetric convolution kernel of integer weights: W[0] for
// the pixel itself, W[i] for the pixels i away on either side. A pixel
// becomes the weighted sum of its neighbours over the sum of the
// weights, rounded once at the end of each pass. The zero Kernel is the
// identity.
type Kernel struct {
	W []uint32
}

// MaxKernelRadius caps a kernel's radius in pixels. Anti-DoS guard, not
// spec-mandated: [Plan] keeps a blur's deviation within 32 pixels, three
// deviations a side, and 1,024 taps a side bound a blur's work for any
// caller that asks for more.
const MaxKernelRadius = 1024

// kernelUnit is what the weights of [GaussianKernel] and [TentKernel]
// sum to, near enough: 2^16, so a weight carries five decimal digits.
const kernelUnit = 1 << 16

// sigmaUnit is the step a deviation or a radius is rounded to, in
// pixels: 1/256, so a kernel depends on a fixed-point value, the same on
// every machine whatever float the caller computed it from.
const sigmaUnit = 256

// GaussianKernel returns the Gaussian of standard deviation sigma pixels
// (Filter Effects 1, 9.14 feGaussianBlur), cut at three deviations, its
// weights e^(-i^2 / 2 sigma^2) times 2^16, rounded. Sigma is first rounded
// to 1/256 pixel; one below 1/256 returns the identity, one whose radius
// passes [MaxKernelRadius] is cut there.
//
// The kernel is the Gaussian itself, not the three box blurs Filter
// Effects 1 allows in its place: Microsoft Office's shadows, glows and
// soft edges fit a Gaussian within half a level, three boxes within two
// to three.
func GaussianKernel(sigma float64) Kernel {
	q := quantize(sigma)
	if q == 0 {
		return Kernel{}
	}
	n := min((3*q+sigmaUnit-1)/sigmaUnit, MaxKernelRadius)
	w := make([]uint32, n+1)
	for i := range w {
		// i^2 / (2 sigma^2) with sigma = q/256: i^2 * 32768 / q^2.
		x := float64(int64(i)*int64(i)*32768) / float64(q*q)
		w[i] = uint32(math.Round(float64(detExp(-x) * kernelUnit))) // fits uint32: at most 2^16
	}
	return trim(w)
}

// TentKernel returns the triangle of radius r pixels, the weight of the
// pixel i away r+1-i: a box blur run twice, the falloff LibreOffice draws
// its blurred shadows, glows and soft edges with. A fractional radius
// weighs its outermost pixels by the fraction. The radius is rounded to
// 1/256 pixel; one below 1/256 returns the identity, one past
// [MaxKernelRadius] is cut there.
func TentKernel(r float64) Kernel {
	q := quantize(r)
	if q == 0 {
		return Kernel{}
	}
	q = min(q, MaxKernelRadius*sigmaUnit)
	n := (q + sigmaUnit - 1) / sigmaUnit
	w := make([]uint32, n+1)
	for i := range w {
		w[i] = uint32(max(q+sigmaUnit-int64(i)*sigmaUnit, 0)) // fits uint32: at most 2^19
	}
	return trim(w)
}

// BoxKernel returns the kernel of passes box blurs of width pixels each,
// convolved into one: three passes are the approximation of a Gaussian
// Filter Effects 1, 9.14 allows and Skia, so Chromium, draws CSS and SVG
// blurs with (width d = floor(sigma 3 sqrt(2 pi) / 4 + 0.5)), two the
// tent of [TentKernel] at a whole radius. An odd width centres every box
// on its pixel. An even width, as the specification has it for three
// passes, centres the boxes of a pair half a pixel left and half a pixel
// right, and an odd last pass on its pixel at width+1, so the whole stays
// centred. A width below 2 or no pass returns the identity; the width is
// cut at [MaxKernelRadius], the passes at 3.
func BoxKernel(width, passes int) Kernel {
	if width < 2 || passes < 1 {
		return Kernel{}
	}
	width = min(width, MaxKernelRadius)
	passes = min(passes, 3)
	full := []uint64{1}
	conv := func(box int) {
		next := make([]uint64, len(full)+box-1)
		for i, v := range full {
			for j := range box {
				next[i+j] += v
			}
		}
		full = next
	}
	for p := range passes {
		box := width
		if width%2 == 0 && p == passes-1 && passes%2 == 1 {
			box++
		}
		conv(box)
	}
	// An even box shifts the centre by half a pixel; a pair of them,
	// one shifted each way, by none: the convolution is then centred on
	// its middle element.
	c := len(full) / 2
	w := make([]uint32, c+1)
	for i := range w {
		w[i] = uint32(full[c+i]) //nolint:gosec // at most 1025^3, under 2^31
	}
	return trim(w)
}

// Edge says what a blur reads past the surface's edge (Filter Effects 1,
// 9.14, edgeMode).
type Edge uint8

const (
	// EdgeNone reads transparent black.
	EdgeNone Edge = iota
	// EdgeDuplicate reads the nearest edge pixel.
	EdgeDuplicate
	// EdgeWrap reads the opposite edge, the surface repeated.
	EdgeWrap
)

// Blur convolves s with kx along rows, then with ky along columns, every
// channel alone, in place. Each pass sums integer weights times the
// pixels and divides once, rounded; the result of the row pass keeps
// eight more bits for the column pass. A Kernel's zero value skips its
// pass. Premultiplied colour blurs as Filter Effects 1, 9.14 requires.
// The work memory, two bytes a channel of every pixel, is charged to the
// budget ctx carries for the length of the call.
func Blur(ctx context.Context, s *Surface, kx, ky Kernel, edge Edge) error {
	if len(kx.W) == 0 && len(ky.W) == 0 {
		return nil
	}
	ch := int(s.F)
	w, h := s.W, s.H
	n := w * ch
	nx, ny := max(len(kx.W)-1, 0), max(len(ky.W)-1, 0)
	var sc scratch
	defer sc.release()
	if err := sc.take(ctx, int64(n)*int64(h)*2+int64(w+2*nx)*4+int64(n)*8); err != nil {
		return err
	}
	plane := make([]uint16, n*h)
	line := make([]uint32, w+2*nx)
	acc := make([]uint64, n)
	sx := kx.sum()
	for y := range h {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		row := s.Pix[y*n : (y+1)*n]
		out := plane[y*n : (y+1)*n]
		if nx == 0 {
			for i, v := range row {
				out[i] = uint16(v) << 8
			}
			continue
		}
		for c := range ch {
			pad(line, row, c, ch, w, nx, edge)
			convolve(line, out, c, ch, w, nx, kx.W, sx)
		}
	}
	sy := ky.sum()
	for y := range h {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		dst := s.Pix[y*n : (y+1)*n]
		if ny == 0 {
			for i, v := range plane[y*n : (y+1)*n] {
				dst[i] = uint8((uint32(v) + 128) >> 8) //nolint:gosec // at most 255 by 256, shifted back
			}
			continue
		}
		clear(acc)
		for i := -ny; i <= ny; i++ {
			r, ok := edgeIndex(y+i, h, edge)
			if !ok {
				continue
			}
			k := uint64(ky.W[abs(i)])
			for x, v := range plane[r*n : (r+1)*n] {
				acc[x] += k * uint64(v)
			}
		}
		d := 256 * sy
		for x, v := range acc {
			dst[x] = uint8(min((v+d/2)/d, 255)) //nolint:gosec // clamped to a byte
		}
	}
	return nil
}

// sum returns the sum of the kernel's weights over both sides.
func (k Kernel) sum() uint64 {
	if len(k.W) == 0 {
		return 1
	}
	s := uint64(k.W[0])
	for _, v := range k.W[1:] {
		s += 2 * uint64(v)
	}
	return s
}

// pad fills line with channel c of the w pixels of row, ch bytes a
// pixel, and r pixels past each end read by edge.
func pad(line []uint32, row []uint8, c, ch, w, r int, edge Edge) {
	for i := range w + 2*r {
		x, ok := edgeIndex(i-r, w, edge)
		if !ok {
			line[i] = 0
			continue
		}
		line[i] = uint32(row[x*ch+c])
	}
}

// edgeIndex returns the index i reads in a run of n by edge; false for
// transparent.
func edgeIndex(i, n int, edge Edge) (int, bool) {
	if i >= 0 && i < n {
		return i, true
	}
	switch edge {
	case EdgeDuplicate:
		return min(max(i, 0), n-1), true
	case EdgeWrap:
		return ((i % n) + n) % n, true
	case EdgeNone:
	}
	return 0, false
}

// convolve writes into channel c of out, ch values a pixel, the w
// values of the padded line convolved with the half-kernel k over its
// sum s, in 1/256 of a level.
func convolve(line []uint32, out []uint16, c, ch, w, r int, k []uint32, s uint64) {
	k0 := uint64(k[0])
	for x := range w {
		m := x + r
		v := k0 * uint64(line[m])
		for i := 1; i <= r; i++ {
			v += uint64(k[i]) * uint64(line[m-i]+line[m+i])
		}
		out[x*ch+c] = uint16(min((v*256+s/2)/s, 65535)) //nolint:gosec // clamped to 16 bits
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// quantize returns v in 1/256 pixel, rounded, 0 for NaN or below 1/256.
func quantize(v float64) int64 {
	if !(v >= 1.0/sigmaUnit) {
		return 0
	}
	return int64(math.Round(min(v, MaxKernelRadius) * sigmaUnit)) // fits int64: at most 2^18
}

// trim drops the zero weights past the last one that is not, the
// identity when only the centre is left.
func trim(w []uint32) Kernel {
	n := len(w)
	for n > 1 && w[n-1] == 0 {
		n--
	}
	if n <= 1 {
		return Kernel{}
	}
	return Kernel{W: w[:n]}
}
