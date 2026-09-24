package jpx

import (
	"context"
	"fmt"
	"slices"
)

// Enumerated colour spaces, T.800 Table I.10. ISO/IEC 15444-2 adds CMYK (12)
// and CIEJab (19), which ISO 32000-1, 7.4.9 admits in a PDF.
const (
	ColourCMYK      = 12
	ColourSRGB      = 16
	ColourGreyscale = 17
	ColourSYCC      = 18
	ColourCIEJab    = 19
)

// Colour is one colour specification of the image (T.800, I.5.3.3).
type Colour struct {
	// Enumerated is the EnumCS of an enumerated specification, and 0
	// otherwise.
	Enumerated int
	// ICC is the profile of an ICC specification, and nil otherwise.
	ICC []byte
}

// rankColours returns the colour specifications in the order ISO 32000-1,
// 7.4.9 has a PDF reader try them: highest precedence first, then best
// approximation, 1 being accurate and 4 poor (ISO/IEC 15444-2, M.11.7.2), and
// file order among equals. A JP2 file sets both fields to 0 (T.800, I.5.3.3),
// which keeps its boxes in file order, the first being the one a JP2 reader
// uses (I.3.3).
func rankColours(specs []colourSpec) []Colour {
	ranked := slices.Clone(specs)
	slices.SortStableFunc(ranked, func(a, b colourSpec) int {
		if a.precedence != b.precedence {
			return b.precedence - a.precedence
		}
		return approximationRank(a.approximation) - approximationRank(b.approximation)
	})
	colours := make([]Colour, len(ranked))
	for i, c := range ranked {
		switch c.method {
		case methodEnumerated:
			colours[i].Enumerated = int(c.enumerated)
		case methodICC, methodAnyICC:
			colours[i].ICC = c.profile
		}
	}
	return colours
}

// approximationRank orders APPROX values best first. The JP2 value 0 and the
// reserved values state no approximation and rank after every stated one.
func approximationRank(approx int) int {
	if approx >= 1 && approx <= 4 {
		return approx
	}
	return 5
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

// Depth returns the bit depth of the values channel ch carries: its palette
// column's when it looks one up, else its component's, and 0 for a channel
// the image does not hold.
func (img *Image) Depth(ch Channel) int {
	if ch.Component < 0 || ch.Component >= len(img.Components) {
		return 0
	}
	if ch.PaletteColumn < 0 {
		return img.Components[ch.Component].Precision
	}
	if img.header == nil || img.header.palette == nil || ch.PaletteColumn >= len(img.header.palette.columns) {
		return 0
	}
	return img.header.palette.columns[ch.PaletteColumn].precision
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

// Render decodes the image's samples and returns the channels chs on the
// default image grid (T.800, I.5.3.1.1), pixel by pixel in rows, one byte per
// channel per pixel in the order of chs. Each grid point takes the sample of
// its channel's component whose separation cell covers it, and conv turns that
// sample, of the given depth and signedness, into the byte; a channel that
// looks up a palette column hands conv the entry the sample indexes, at the
// column's depth (I.5.3.4).
//
// Render holds one tile's samples at a time, where [Decode] holds every
// component's.
func (img *Image) Render(ctx context.Context, chs []Channel, conv func(v int32, precision int, signed bool) byte) ([]byte, error) {
	w, h := img.DefaultSize()
	n := len(chs)
	if n == 0 {
		return nil, fmt.Errorf("%w: no channel to render", ErrMalformed)
	}
	if limit := img.maxSamples; int64(w) > limit || int64(h) > limit || int64(w)*int64(h) > limit/int64(n) {
		return nil, fmt.Errorf("%w: %dx%d grid points of %d channels", ErrLimit, w, h, n)
	}
	for i, ch := range chs {
		if ch.Component < 0 || ch.Component >= len(img.Components) {
			return nil, fmt.Errorf("%w: channel %d maps component %d of %d", ErrMalformed, i, ch.Component, len(img.Components))
		}
		if ch.PaletteColumn >= 0 {
			if img.header == nil || img.header.palette == nil || ch.PaletteColumn >= len(img.header.palette.columns) {
				return nil, fmt.Errorf("%w: channel %d looks up a palette column the file lacks", ErrMalformed, i)
			}
			if col := img.header.palette.columns[ch.PaletteColumn]; col.precision > maxSampleBits {
				return nil, fmt.Errorf("%w: palette column of %d bits", ErrUnsupported, col.precision)
			}
		}
	}
	out := make([]byte, w*h*n)
	grids := make([]*componentGrid, len(img.Components))
	err := img.decodeTiles(ctx, func(c int, tc *tileComponent) error {
		var mine []int
		for i, ch := range chs {
			if ch.Component == c {
				mine = append(mine, i)
			}
		}
		if len(mine) == 0 {
			return nil
		}
		if grids[c] == nil {
			grids[c] = img.grid(c)
		}
		return img.renderTile(out, chs, mine, grids[c], c, tc, conv)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// componentGrid maps the default grid onto one component: cols[u] and rows[v]
// are the column and row of the component sample that covers grid point
// (u, v). Both are non-decreasing.
type componentGrid struct {
	cols, rows []int
}

// grid returns the componentGrid of component c.
func (img *Image) grid(c int) *componentGrid {
	comp := img.Components[c]
	m := img.gridStep()
	w, h := img.DefaultSize()
	gx0, gy0 := ceilDiv(img.x0, m), ceilDiv(img.y0, m)
	g := &componentGrid{cols: make([]int, w), rows: make([]int, h)}
	for u := range g.cols {
		g.cols[u] = min(max((gx0+u)*m/comp.DX-comp.x0, 0), comp.Width-1)
	}
	for v := range g.rows {
		g.rows[v] = min(max((gy0+v)*m/comp.DY-comp.y0, 0), comp.Height-1)
	}
	return g
}

// renderTile writes the channels mine, all of component c, for the grid
// points whose samples tile-component tc holds.
func (img *Image) renderTile(out []byte, chs []Channel, mine []int, g *componentGrid, c int, tc *tileComponent, conv func(int32, int, bool) byte) error {
	comp := &img.Components[c]
	s := comp.sampler()
	// The grid points tc covers form a rectangle: the maps are monotonic.
	ua := firstAtLeast(g.cols, tc.x0-comp.x0)
	ub := firstAtLeast(g.cols, tc.x1-comp.x0)
	va := firstAtLeast(g.rows, tc.y0-comp.y0)
	vb := firstAtLeast(g.rows, tc.y1-comp.y0)
	w, n := len(g.cols), len(chs)
	for v := va; v < vb; v++ {
		row := (g.rows[v] + comp.y0 - tc.y0) * tc.coeffs.stride
		for u := ua; u < ub; u++ {
			sample := s.sample(&tc.coeffs, row+g.cols[u]+comp.x0-tc.x0)
			for _, i := range mine {
				ch := chs[i]
				if ch.PaletteColumn < 0 {
					out[(v*w+u)*n+i] = conv(sample, comp.Precision, comp.Signed)
					continue
				}
				p := img.header.palette
				if sample < 0 || int(sample) >= p.entries {
					return fmt.Errorf("%w: palette index %d of %d entries", ErrMalformed, sample, p.entries)
				}
				col := p.columns[ch.PaletteColumn]
				out[(v*w+u)*n+i] = conv(int32(col.values[sample]), col.precision, col.signed) //nolint:gosec // col.precision <= maxSampleBits = 31.
			}
		}
	}
	return nil
}

// firstAtLeast returns the first index of the non-decreasing xs whose value is
// at least x, or len(xs).
func firstAtLeast(xs []int, x int) int {
	i, _ := slices.BinarySearch(xs, x)
	return i
}
