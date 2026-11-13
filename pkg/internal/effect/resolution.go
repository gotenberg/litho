package effect

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// pixelsPerSigma is the pixels a blur's standard deviation spans at the
// resolution [Plan] chooses: 5, where the error stops falling. See the
// package documentation for the measurement.
const pixelsPerSigma = 5

// MinScale is the fewest pixels a point an effect is drawn at: 1, 72
// pixels an inch. A page shown at its size on a screen of 96 pixels an
// inch then spreads a pixel of the effect over at most 1.33 pixels of
// the screen, so the result does not depend on how the viewer
// interpolates an enlarged picture.
const MinScale = 1.0

// MaxScale is the most pixels a point an effect is drawn at: 300 pixels
// an inch, the resolution of print and the one Chromium draws its
// filters at. Content that is not blurred, a reflection or a filtered
// picture, takes it; a blur of deviation under 1.2 points reaches it
// first and is drawn at fewer than 5 pixels a deviation.
const MaxScale = 300.0 / 72

// MaxPagePixels caps the pixels of every surface of one page together.
// Anti-DoS guard, not spec-mandated: 67,108,864 pixels hold eight pages
// of effects at 300 pixels an inch, more than a page holds; past it an
// effect falls back to its vector approximation, with a warning.
const MaxPagePixels = 1 << 26

// Resolution is the pixel grid [Plan] chooses for one effect: Scale
// pixels a point, W by H pixels. Reduced reports that the surface cap
// lowered the scale under the rule's.
type Resolution struct {
	Scale   float64
	W, H    int
	Reduced bool
}

// Plan returns the grid of an effect over w by h points whose blur has
// standard deviation sigma points, the smaller of two when it has two;
// 0 for content not blurred. The scale is 5 pixels a deviation, within
// [MinScale, MaxScale]; each side is rounded up to a whole pixel. When
// the grid would pass [MaxPixels], or a side 65,536 pixels, the scale is
// lowered until it holds, and Reduced is set: the caller warns that the
// effect is approximated.
// It returns false for a side that is not positive and finite.
//
// The arithmetic rounds every product on its own, so the grid is the
// same on every machine.
func Plan(sigma, w, h float64) (Resolution, bool) {
	if !numeric.PositiveFinite(w) || !numeric.PositiveFinite(h) || w > maxPixelCoordinate || h > maxPixelCoordinate {
		return Resolution{}, false
	}
	k := MaxScale
	if numeric.PositiveFinite(sigma) {
		k = min(max(pixelsPerSigma/sigma, MinScale), MaxScale)
	}
	r := Resolution{Scale: k}
	r.W, r.H = pixelSide(float64(w*k)), pixelSide(float64(h*k))
	if !fits(r.W, r.H) {
		f := min(math.Sqrt(MaxPixels/float64(float64(r.W)*float64(r.H))), maxSide/float64(r.W), maxSide/float64(r.H))
		r.Scale = float64(k * f)
		r.Reduced = true
		r.W, r.H = pixelSide(float64(w*r.Scale)), pixelSide(float64(h*r.Scale))
		for !fits(r.W, r.H) {
			r.Scale = float64(r.Scale * 0.99)
			r.W, r.H = pixelSide(float64(w*r.Scale)), pixelSide(float64(h*r.Scale))
		}
	}
	return r, true
}

// fits reports whether a grid of w by h pixels holds within [MaxPixels]
// and within the longest side [Fill] covers.
func fits(w, h int) bool {
	return w <= maxSide && h <= maxSide && int64(w)*int64(h) <= MaxPixels
}

// pixelSide returns v rounded up to a whole pixel, at least 1.
func pixelSide(v float64) int {
	return max(int(math.Ceil(v)), 1) // fits int: at most 2^20 times MaxScale
}

// Page counts the pixels of the surfaces one page's effects take, against
// [MaxPagePixels]. Its zero value is an empty page. A Page is not safe
// for concurrent use.
type Page struct {
	used int64
}

// Take accounts the pixels of r and reports true, or reports false
// without accounting them when they would pass [MaxPagePixels]: the
// caller then draws the effect's vector approximation and warns.
func (p *Page) Take(r Resolution) bool {
	n := int64(r.W) * int64(r.H)
	if n <= 0 || p.used+n > MaxPagePixels {
		return false
	}
	p.used += n
	return true
}
