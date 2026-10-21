package surface

import "fmt"

// MaxPlaneChannels bounds the colour channels of a [Plane]: four, the CMYK of
// the widest blending colour space a transparency group composites in (ISO
// 32000-1, 11.3.4). Not a resource limit: it sizes the fixed arrays the
// compositor keeps one pixel in.
const MaxPlaneChannels = 4

// Plane is a premultiplied raster of Channels colour channels and an alpha,
// rows top to bottom, values in [0, 1]: a [Canvas] generalised to the grey
// and CMYK blending colour spaces. Each pixel holds Channels+1 values, the
// colour premultiplied by the alpha, then the alpha.
type Plane struct {
	Width, Height, Channels int
	Pix                     []float32
}

// NewPlane returns a transparent plane of width × height pixels of channels
// colour channels, refusing one larger than maxPixels or a channel count
// outside 1 to [MaxPlaneChannels].
func NewPlane(width, height, channels int, maxPixels int64) (*Plane, error) {
	if channels < 1 || channels > MaxPlaneChannels {
		return nil, fmt.Errorf("%w: %d colour channels", ErrTooLarge, channels)
	}
	if err := checkSize(width, height, maxPixels); err != nil {
		return nil, err
	}
	return &Plane{Width: width, Height: height, Channels: channels, Pix: make([]float32, (channels+1)*width*height)}, nil
}

// Canvas returns a plane of three channels as a [Canvas] sharing its pixels,
// and nil for any other channel count.
func (p *Plane) Canvas() *Canvas {
	if p.Channels != 3 {
		return nil
	}
	return &Canvas{Width: p.Width, Height: p.Height, Pix: p.Pix}
}
