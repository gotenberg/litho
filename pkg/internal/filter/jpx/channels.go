package jpx

import "fmt"

// Enumerated colour spaces, T.800 Table I.10. ISO/IEC 15444-2 adds CMYK (12)
// and CIEJab (19), which ISO 32000-1, 7.4.9 admits in a PDF.
const (
	ColourCMYK      = 12
	ColourSRGB      = 16
	ColourGreyscale = 17
	ColourSYCC      = 18
	ColourCIEJab    = 19
)

// Colour is the colour specification a JP2 reader uses (T.800, I.3.3): the
// first Colour Specification box whose method it knows.
type Colour struct {
	// Enumerated is the EnumCS of an enumerated specification, and 0
	// otherwise.
	Enumerated int
	// ICC is the profile of an ICC specification, and nil otherwise.
	ICC []byte
}

// Channel types, T.800 Table I.16.
const (
	ChannelColour        = 0
	ChannelOpacity       = 1
	ChannelPremultiplied = 2
	// ChannelUnspecified is the type, and the association, of a channel
	// whose Channel Definition box says nothing of it.
	ChannelUnspecified = 0xFFFF
)

// Channel is one channel of the image (T.800, I.5.3.5 and I.5.3.6): samples
// taken from a codestream component, directly or through a palette column,
// and what they mean.
type Channel struct {
	Component int
	// PaletteColumn is the palette column the component's values index, or
	// -1 for a channel that uses the component's samples directly.
	PaletteColumn int
	// Kind is a channel type, and Association the colour the channel
	// carries, counted from 1 in the order of Table I.18, 0 for the image as
	// a whole.
	Kind, Association int
}

// Channels returns the channels of the image: those the Component Mapping box
// defines, or one per component without it (T.800, I.5.3.5). The Channel
// Definition box types them; without one, every channel is a colour channel
// and channel i carries colour i + 1 (I.5.3.6). A channel described twice
// keeps its first description.
func (img *Image) Channels() ([]Channel, error) {
	h := img.header
	var chs []Channel
	if h == nil || h.mapping == nil {
		for c := range img.Components {
			chs = append(chs, Channel{Component: c, PaletteColumn: -1})
		}
	} else {
		for i, m := range h.mapping {
			if m.component >= len(img.Components) {
				return nil, fmt.Errorf("%w: channel %d maps component %d of %d", ErrMalformed, i, m.component, len(img.Components))
			}
			ch := Channel{Component: m.component, PaletteColumn: -1}
			if m.viaPalette {
				if h.palette == nil || m.column >= len(h.palette.columns) {
					return nil, fmt.Errorf("%w: channel %d looks up a palette column the file lacks", ErrMalformed, i)
				}
				ch.PaletteColumn = m.column
			}
			chs = append(chs, ch)
		}
	}
	if h == nil || h.channels == nil {
		for i := range chs {
			chs[i].Kind, chs[i].Association = ChannelColour, i+1
		}
		return chs, nil
	}
	for i := range chs {
		chs[i].Kind, chs[i].Association = ChannelUnspecified, ChannelUnspecified
	}
	described := make([]bool, len(chs))
	for _, d := range h.channels {
		if d.channel >= len(chs) {
			return nil, fmt.Errorf("%w: definition of channel %d of %d", ErrMalformed, d.channel, len(chs))
		}
		if described[d.channel] {
			continue
		}
		described[d.channel] = true
		chs[d.channel].Kind, chs[d.channel].Association = d.kind, d.association
	}
	return chs, nil
}

// ChannelSamples returns the samples of ch at its component's resolution, with
// their depth and signedness: the component's own, or the entries its values
// index in the palette column (T.800, I.5.3.4).
func (img *Image) ChannelSamples(ch Channel) ([]int32, int, bool, error) {
	comp := img.Components[ch.Component]
	if ch.PaletteColumn < 0 {
		return comp.Samples, comp.Precision, comp.Signed, nil
	}
	p := img.header.palette
	col := p.columns[ch.PaletteColumn]
	if col.precision > maxSampleBits {
		return nil, 0, false, fmt.Errorf("%w: palette column of %d bits", ErrUnsupported, col.precision)
	}
	out := make([]int32, len(comp.Samples))
	for i, v := range comp.Samples {
		if v < 0 || int(v) >= p.entries {
			return nil, 0, false, fmt.Errorf("%w: palette index %d of %d entries", ErrMalformed, v, p.entries)
		}
		out[i] = int32(col.values[v]) //nolint:gosec // col.precision <= maxSampleBits = 31.
	}
	return out, col.precision, col.signed, nil
}

// DefaultSize returns the default image dimensions (T.800, I.5.3.1.1): the
// image area divided by M, the greatest common divisor of every component's
// sample separation.
func (img *Image) DefaultSize() (int, int) {
	m := img.gridStep()
	return ceilDiv(img.x1, m) - ceilDiv(img.x0, m), ceilDiv(img.y1, m) - ceilDiv(img.y0, m)
}

// gridStep returns M of T.800, Equation (I-1).
func (img *Image) gridStep() int {
	m := 0
	for _, c := range img.Components {
		m = gcd(gcd(m, c.DX), c.DY)
	}
	return m
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// Resample places values, samples of component c, on the default image grid:
// each grid point takes the sample of c whose separation cell covers it, the
// sample itself when c is sampled at every grid point.
func (img *Image) Resample(values []int32, c int) []int32 {
	comp := img.Components[c]
	m := img.gridStep()
	w, h := img.DefaultSize()
	if comp.DX == m && comp.DY == m && comp.Width == w && comp.Height == h {
		return values
	}
	gx0, gy0 := ceilDiv(img.x0, m), ceilDiv(img.y0, m)
	cols := make([]int, w)
	for u := range cols {
		cols[u] = min(max((gx0+u)*m/comp.DX-comp.x0, 0), comp.Width-1)
	}
	out := make([]int32, w*h)
	for v := range h {
		row := min(max((gy0+v)*m/comp.DY-comp.y0, 0), comp.Height-1) * comp.Width
		for u, col := range cols {
			out[v*w+u] = values[row+col]
		}
	}
	return out
}
