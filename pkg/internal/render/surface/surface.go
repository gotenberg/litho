package surface

import (
	"errors"
	"fmt"
	"image"
	"image/color"
)

// ErrTooLarge reports a surface whose pixel count is not positive or exceeds
// the budget the caller passed.
var ErrTooLarge = errors.New("surface size out of bounds")

// checkSize validates width × height against maxPixels before anything is
// allocated. The dimensions come from the page box and the requested
// resolution, which a file controls, so an unchecked product is a denial of
// service.
func checkSize(width, height int, maxPixels int64) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%w: %dx%d", ErrTooLarge, width, height)
	}
	if int64(width) > maxPixels/int64(height) {
		return fmt.Errorf("%w: %dx%d exceeds %d pixels", ErrTooLarge, width, height, maxPixels)
	}
	return nil
}

// Canvas is a premultiplied RGBA raster, rows top to bottom, channels in
// [0, 1]. Premultiplied values make source-over compositing one multiply-add
// per channel (ISO 32000-1, 11.3.3, with the colour and shape terms folded).
type Canvas struct {
	Width, Height int
	// Pix holds 4 values per pixel: R, G, B, A, premultiplied by A.
	Pix []float32
}

// NewCanvas returns a transparent canvas of width × height pixels, refusing
// one larger than maxPixels.
func NewCanvas(width, height int, maxPixels int64) (*Canvas, error) {
	if err := checkSize(width, height, maxPixels); err != nil {
		return nil, err
	}
	return &Canvas{Width: width, Height: height, Pix: make([]float32, 4*width*height)}, nil
}

// Fill sets every pixel to the unpremultiplied colour r, g, b at alpha a.
func (c *Canvas) Fill(r, g, b, a float32) {
	for i := 0; i < len(c.Pix); i += 4 {
		c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = r*a, g*a, b*a, a
	}
}

// At returns the premultiplied value of pixel (x, y), and zero outside the
// canvas.
func (c *Canvas) At(x, y int) (r, g, b, a float32) {
	if x < 0 || y < 0 || x >= c.Width || y >= c.Height {
		return 0, 0, 0, 0
	}
	i := 4 * (y*c.Width + x)
	return c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3]
}

// BlendOver composites the unpremultiplied colour r, g, b at alpha a over
// pixel (x, y) with the Normal blend mode. Pixels outside the canvas are
// ignored.
func (c *Canvas) BlendOver(x, y int, r, g, b, a float32) {
	if x < 0 || y < 0 || x >= c.Width || y >= c.Height || a <= 0 {
		return
	}
	a = min(a, 1)
	i := 4 * (y*c.Width + x)
	keep := 1 - a
	c.Pix[i] = r*a + c.Pix[i]*keep
	c.Pix[i+1] = g*a + c.Pix[i+1]*keep
	c.Pix[i+2] = b*a + c.Pix[i+2]*keep
	c.Pix[i+3] = a + c.Pix[i+3]*keep
}

// Image returns the canvas as a non-premultiplied 8-bit image, each channel
// rounded to the nearest value.
func (c *Canvas) Image() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, c.Width, c.Height))
	for y := range c.Height {
		for x := range c.Width {
			r, g, b, a := c.At(x, y)
			var px color.NRGBA
			if a > 0 {
				px = color.NRGBA{R: to8(r / a), G: to8(g / a), B: to8(b / a), A: to8(a)}
			}
			img.SetNRGBA(x, y, px)
		}
	}
	return img
}

func to8(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	}
	return uint8(v*255 + 0.5)
}

// Mask is an alpha raster, rows top to bottom, values in [0, 1]: the
// coverage of a filled shape, or a clip.
type Mask struct {
	Width, Height int
	A             []float32
}

// NewMask returns an all-zero mask of width × height pixels, refusing one
// larger than maxPixels.
func NewMask(width, height int, maxPixels int64) (*Mask, error) {
	if err := checkSize(width, height, maxPixels); err != nil {
		return nil, err
	}
	return &Mask{Width: width, Height: height, A: make([]float32, width*height)}, nil
}

// At returns the value of (x, y), and zero outside the mask.
func (m *Mask) At(x, y int) float32 {
	if x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return 0
	}
	return m.A[y*m.Width+x]
}

// Set stores v at (x, y), clamped to [0, 1]. Pixels outside the mask are
// ignored.
func (m *Mask) Set(x, y int, v float32) {
	if x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return
	}
	m.A[y*m.Width+x] = min(max(v, 0), 1)
}

// Intersect multiplies m by other pixel by pixel, the coverage of both. The
// masks must share a size.
func (m *Mask) Intersect(other *Mask) error {
	if other.Width != m.Width || other.Height != m.Height {
		return fmt.Errorf("intersecting a %dx%d mask with a %dx%d one: sizes differ", m.Width, m.Height, other.Width, other.Height)
	}
	for i := range m.A {
		m.A[i] *= other.A[i]
	}
	return nil
}
