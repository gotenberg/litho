package raster

import (
	"fmt"
	"image"
)

// MaxPixels caps the pixels of one mask or image. Anti-DoS guard, not
// spec-mandated: 4,194,304 pixels (2048 by 2048, 16 MiB as NRGBA) holds
// an effect over a whole slide or an A4 page at 2.5 pixels a point, more
// than the resolution any caller asks for, and keeps a blur's work and
// memory bounded on hostile sizes.
const MaxPixels = 1 << 22

// NewMask returns an uncovered mask of w by h pixels, or [ErrTooLarge].
func NewMask(w, h int) (*image.Alpha, error) {
	if w <= 0 || h <= 0 || w > MaxPixels || h > MaxPixels || w > MaxPixels/h {
		return nil, fmt.Errorf("%w: %d by %d", ErrTooLarge, w, h)
	}
	return image.NewAlpha(image.Rect(0, 0, w, h)), nil
}

// clone returns a copy of m with its origin at (0, 0).
func clone(m *image.Alpha) *image.Alpha {
	b := m.Bounds()
	out := image.NewAlpha(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		copy(out.Pix[y*out.Stride:y*out.Stride+b.Dx()], m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):])
	}
	return out
}
