package surface

import (
	"math"
	"testing"
)

// fuzzMaxPixels caps the budget [FuzzSurfaces] grants, so an accepted size
// allocates at most a few megabytes.
const fuzzMaxPixels = 1 << 18

// FuzzSurfaces sizes a canvas, a mask and a plane from fuzzed dimensions,
// channel count and budget, as a page box and a resolution a file controls
// size them. Each is refused or allocated at the size asked, never past the
// budget, and drawing at fuzzed coordinates stays inside it.
func FuzzSurfaces(f *testing.F) {
	f.Add(100, 50, 4, int64(10000), -1, 3)
	f.Add(1<<20, 1<<20, 0, int64(math.MaxInt64), math.MaxInt32, math.MinInt32)
	f.Fuzz(func(t *testing.T, width, height, channels int, maxPixels int64, x, y int) {
		maxPixels = min(maxPixels, fuzzMaxPixels)
		if c, err := NewCanvas(width, height, maxPixels); err == nil {
			if len(c.Pix) != 4*width*height || int64(width)*int64(height) > maxPixels {
				t.Fatalf("canvas %dx%d holds %d values under budget %d", width, height, len(c.Pix), maxPixels)
			}
			c.BlendOver(x, y, 1, 0, 0, 0.5)
			c.At(x, y)
		}
		if m, err := NewMask(width, height, maxPixels); err == nil {
			if len(m.A) != width*height {
				t.Fatalf("mask %dx%d holds %d values", width, height, len(m.A))
			}
			m.Set(x, y, 2)
			if v := m.At(x, y); v < 0 || v > 1 {
				t.Fatalf("mask value %v", v)
			}
		}
		if p, err := NewPlane(width, height, channels, maxPixels); err == nil {
			if len(p.Pix) != (channels+1)*width*height {
				t.Fatalf("plane %dx%dx%d holds %d values", width, height, channels, len(p.Pix))
			}
		}
	})
}
