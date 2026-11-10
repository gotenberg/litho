package raster

import (
	"image"
	"math"
)

// Kernel is a symmetric convolution kernel: an odd number of weights
// summing to 1, the middle one applying to the pixel itself.
type Kernel []float64

// MaxKernelRadius caps a kernel's radius in pixels. Anti-DoS guard, not
// spec-mandated: a blur reaching 1024 pixels on each side is wider than
// half of the largest mask a caller makes, so a wider one changes nothing
// a page shows, and the cap bounds a blur's work to 2049 taps a pixel.
const MaxKernelRadius = 1024

// Tent returns the triangular kernel of radius r pixels, the weight of
// the pixel at distance i proportional to r+1-|i|: a box blur run twice,
// the kernel of a stack blur. A blurred edge then ramps from transparent
// to opaque over 2r pixels about the edge, quadratically at both ends. A
// fractional radius weighs its outermost pixels by the fraction; a radius
// below a pixel's fraction returns the identity, one above
// [MaxKernelRadius] is clipped to it.
func Tent(r float64) Kernel {
	if !(r > 0) {
		return Kernel{1}
	}
	r = min(r, MaxKernelRadius)
	n := int(math.Ceil(r))
	k := make(Kernel, 2*n+1)
	sum := 0.0
	for i := range k {
		w := max(r+1-math.Abs(float64(i-n)), 0)
		k[i] = w
		sum += w
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

// Gaussian returns the Gaussian kernel of standard deviation sigma pixels,
// cut at three deviations (CSS Filter Effects 1, 13.7.3 feGaussianBlur:
// a blur radius r is a deviation of r/2 for box-shadow and text-shadow,
// CSS Backgrounds 3, 7.1). A deviation below a hundredth of a pixel
// returns the identity; the radius is clipped to [MaxKernelRadius].
func Gaussian(sigma float64) Kernel {
	if !(sigma >= 0.01) {
		return Kernel{1}
	}
	n := int(min(math.Ceil(3*sigma), MaxKernelRadius))
	k := make(Kernel, 2*n+1)
	sum := 0.0
	for i := range k {
		d := float64(i - n)
		k[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

// Blur returns m convolved with k horizontally, then vertically. Pixels
// past the mask's edge repeat the edge's, so a coverage that reaches the
// edge stays at its value there: a mask clipped by its caller keeps its
// coverage at the cut. The result has m's size, its origin at (0, 0); a
// kernel of even length or a single weight returns a copy of m.
func Blur(m *image.Alpha, k Kernel) *image.Alpha {
	out := clone(m)
	if len(k) < 3 || len(k)%2 == 0 {
		return out
	}
	w, h := out.Bounds().Dx(), out.Bounds().Dy()
	line := make([]float64, max(w, h))
	res := make([]float64, max(w, h))
	for y := range h {
		row := out.Pix[y*out.Stride : y*out.Stride+w]
		for x, v := range row {
			line[x] = float64(v)
		}
		convolve(line[:w], res[:w], k)
		for x := range row {
			row[x] = byteOf(res[x])
		}
	}
	for x := range w {
		for y := range h {
			line[y] = float64(out.Pix[y*out.Stride+x])
		}
		convolve(line[:h], res[:h], k)
		for y := range h {
			out.Pix[y*out.Stride+x] = byteOf(res[y])
		}
	}
	return out
}

// convolve writes into dst the convolution of src with k, the edges
// repeated.
func convolve(src, dst []float64, k Kernel) {
	n := len(k) / 2
	last := len(src) - 1
	for i := range src {
		s := 0.0
		for j, w := range k {
			p := min(max(i+j-n, 0), last)
			s += w * src[p]
		}
		dst[i] = s
	}
}

// byteOf rounds v to the nearest byte value.
func byteOf(v float64) uint8 {
	return uint8(min(max(math.Round(v), 0), 255)) //nolint:gosec // clamped to [0, 255] on the same line
}
