package imgpaint

import "math"

// maxBoxTaps caps the samples the box filter reads per axis for one device
// pixel. Not spec-mandated: it bounds the work of drawing a huge image into a
// few pixels, which a file controls, to maxBoxTaps² reads per pixel. Eight
// taps reproduce an exact box average for every reduction up to 8:1 and keep
// the residual aliasing of larger reductions below what one 8-bit level shows
// on photographic content.
const maxBoxTaps = 8

// footprintSlack is how far past a whole number of samples a pixel's
// footprint may run before it counts one more. Page matrices rarely land on
// exact ratios: a 150 dpi page shows a 150 ppi image at a footprint of
// 1.0000001, and an 8-sample image placed at 7.99 points at 72 dpi at 1.0013.
// Counting the sliver as a whole extra tap would weigh a neighbour as much as
// the sample itself and blur an image drawn at its own resolution. One
// percent is far below a visible change of scale and far above float error.
const footprintSlack = 0.01

// tap is one sample index on an axis and its weight.
type tap struct {
	i int
	w float32
}

// axisTaps holds the taps of one axis for one device pixel.
type axisTaps struct {
	n int
	t [maxBoxTaps]tap
}

// grid is a sample grid laid over the unit square: size samples per axis and
// the extent, in samples, one device pixel's footprint covers on each axis.
type grid struct {
	w, h        int
	ex, ey      float64
	interpolate bool
}

// newGrid lays a w × h grid over the unit square whose device-to-unit
// Jacobian has extents eux and euy (unit-square units per device pixel).
func newGrid(w, h int, eux, euy float64, interpolate bool) grid {
	return grid{w: w, h: h, ex: eux * float64(w), ey: euy * float64(h), interpolate: interpolate}
}

// taps fills xt and yt with the taps of the device pixel whose centre maps to
// (u, v) in the unit square. Image space puts sample row 0 at the top of the
// square (ISO 32000-1, 8.9.4), hence the flip on v.
func (g *grid) taps(u, v float64, xt, yt *axisTaps) {
	axis(u*float64(g.w), g.ex, g.w, g.interpolate, xt)
	axis((1-v)*float64(g.h), g.ey, g.h, g.interpolate, yt)
}

// axis fills out with the taps at sample coordinate s for a footprint of
// extent samples on an axis of size samples. See the package documentation
// for the choice of filter.
func axis(s, extent float64, size int, interpolate bool, out *axisTaps) {
	switch {
	case extent > 1+footprintSlack:
		n := min(int(math.Ceil(extent-footprintSlack)), maxBoxTaps)
		w := 1 / float32(n)
		step := extent / float64(n)
		start := s - extent/2 + step/2
		for k := range n {
			out.t[k] = tap{i: clampIndex(math.Floor(start+float64(k)*step), size), w: w}
		}
		out.n = n
	case interpolate:
		f := s - 0.5
		i0 := math.Floor(f)
		t := float32(f - i0)
		out.t[0] = tap{i: clampIndex(i0, size), w: 1 - t}
		out.t[1] = tap{i: clampIndex(i0+1, size), w: t}
		out.n = 2
	default:
		out.t[0] = tap{i: clampIndex(math.Floor(s), size), w: 1}
		out.n = 1
	}
}

// clampIndex returns f as a sample index inside [0, size), so a tap past an
// edge reads the edge sample.
func clampIndex(f float64, size int) int {
	switch {
	case !(f >= 0):
		return 0
	case f >= float64(size-1):
		return size - 1
	}
	return int(f)
}
