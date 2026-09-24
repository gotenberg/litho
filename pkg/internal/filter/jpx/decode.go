package jpx

import (
	"context"
	"fmt"
	"math"
)

// maxSamples bounds the samples an image holds, summed over its components,
// and the values one Render call returns, one per channel per grid point. It
// is an anti-DoS guard, not spec-mandated: a codestream of a few hundred bytes
// can describe a huge flat image, or components so unevenly subsampled that
// the default grid dwarfs them, and every sample costs decoding time. 2^27
// admits the largest image real documents carry and a 400 dpi A3 page in
// colour. It is below maxImageBytes in graphics/image, 400 million one-byte
// samples, because Decode holds each sample in four bytes.
//
// # Corpus evidence
//
//   - All corpora: the largest image decoded, 6642x5338 in three components,
//     holds 106 million samples.
const maxSamples = 1 << 27

// maxSampleBits is the deepest component this package returns: samples are
// int32. It is an implementation limit, not spec-mandated; T.800, Table A.11
// allows 38 bits.
//
// # Corpus evidence
//
//   - All corpora: no component is deeper than 16 bits.
const maxSampleBits = 31

// Image is a decoded JPEG 2000 image: one sample plane per codestream
// component, with what its JP2 header says of them.
type Image struct {
	Components []Component
	// Colours holds the colour specifications of the JP2 header in the order
	// a reader tries them, most preferred first, and none for a bare
	// codestream or a header whose methods are all unknown.
	Colours []Colour
	header  *fileHeader
	cs      *codestream
	// x0, y0, x1 and y1 bound the image area on the reference grid (B.2).
	x0, y0, x1, y1 int
}

// Component is one decoded component (T.800, B.2): Width x Height samples in
// row-major order, each Precision bits deep and signed when Signed is set.
// DX and DY are its sample separation on the reference grid.
type Component struct {
	Width, Height int
	Precision     int
	Signed        bool
	DX, DY        int
	Samples       []int32
	// x0 and y0 place the component on its sample grid (B-1).
	x0, y0 int
}

// Decode decodes a JP2 file, or a bare codestream, and returns its image with
// every component's samples.
//
// Decode is safe for concurrent use: it keeps no state between calls.
func Decode(ctx context.Context, data []byte) (*Image, error) {
	img, err := Describe(data)
	if err != nil {
		return nil, err
	}
	for i := range img.Components {
		c := &img.Components[i]
		c.Samples = make([]int32, c.Width*c.Height)
	}
	err = img.decodeTiles(ctx, func(c int, tc *tileComponent) error {
		img.Components[c].place(tc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

// Describe reads a JP2 file, or a bare codestream, without decoding its
// samples. The image it returns has components whose Samples are nil, and the
// colour specification and channels [Decode] would give; [Image.Render]
// decodes it.
func Describe(data []byte) (*Image, error) {
	stream, hdr, err := readFile(data)
	if err != nil {
		return nil, err
	}
	cs, err := parseCodestream(stream)
	if err != nil {
		return nil, err
	}
	img, err := newImage(cs)
	if err != nil {
		return nil, err
	}
	img.header, img.cs = hdr, cs
	if hdr != nil {
		img.Colours = rankColours(hdr.colours)
	}
	return img, nil
}

// newImage lays out the components of the image area (T.800, B-1 and B-2),
// checking their samples against maxSamples before any are allocated.
func newImage(cs *codestream) (*Image, error) {
	img := &Image{Components: make([]Component, len(cs.components)), x0: cs.x0, y0: cs.y0, x1: cs.x1, y1: cs.y1}
	total := 0
	for i, comp := range cs.components {
		if comp.precision > maxSampleBits {
			return nil, fmt.Errorf("%w: component %d is %d bits deep", ErrUnsupported, i, comp.precision)
		}
		c := &img.Components[i]
		c.Precision, c.Signed, c.DX, c.DY = comp.precision, comp.signed, comp.dx, comp.dy
		c.x0, c.y0 = ceilDiv(cs.x0, comp.dx), ceilDiv(cs.y0, comp.dy)
		c.Width, c.Height = ceilDiv(cs.x1, comp.dx)-c.x0, ceilDiv(cs.y1, comp.dy)-c.y0
		if c.Width > maxSamples || c.Height > maxSamples || total+c.Width*c.Height > maxSamples {
			return nil, fmt.Errorf("%w: image of more than %d samples", ErrLimit, maxSamples)
		}
		total += c.Width * c.Height
	}
	return img, nil
}

// decodeTiles decodes the image's tiles in turn and hands each reconstructed
// tile-component to emit, with the index of its component. A tile's
// coefficients are released before the next tile allocates its own, so only
// one tile is held at a time.
func (img *Image) decodeTiles(ctx context.Context, emit func(c int, tc *tileComponent) error) error {
	budget := &blockBudget{}
	for _, t := range img.cs.tiles {
		if t == nil {
			continue
		}
		if err := decodeTile(ctx, img.cs, t, budget, emit); err != nil {
			return fmt.Errorf("tile %d: %w", t.index, err)
		}
	}
	return nil
}

// decodeTile decodes one tile and hands its components to emit.
func decodeTile(ctx context.Context, cs *codestream, t *tile, budget *blockBudget, emit func(c int, tc *tileComponent) error) error {
	d, err := cs.newTileDecoder(t, budget)
	if err != nil {
		return err
	}
	if err := d.decodePackets(ctx, cs); err != nil {
		return err
	}
	if err := d.decodeBlocks(ctx, cs); err != nil {
		return err
	}
	for c := range d.comps {
		d.comps[c].inverseTransform()
	}
	if d.transformed {
		if err := inverseMCT(cs, d); err != nil {
			return err
		}
	}
	for c := range d.comps {
		if err := emit(c, &d.comps[c]); err != nil {
			return err
		}
		d.comps[c].coeffs = plane{}
	}
	return nil
}

// inverseMCT undoes the multiple component transformation on components 0, 1
// and 2: the RCT on the reversible path (T.800, G.2.2), the ICT on the
// irreversible one (G.3.2).
func inverseMCT(cs *codestream, d *tileDecoder) error {
	// G.2 and G.3: three components of one separation, bit depth and filter.
	if len(d.comps) < 3 {
		return fmt.Errorf("%w: multiple component transformation over %d components", ErrMalformed, len(d.comps))
	}
	c0, c1, c2 := cs.components[0], cs.components[1], cs.components[2]
	rev := d.comps[0].style.reversible
	if c1 != c0 || c2 != c0 || d.comps[1].style.reversible != rev || d.comps[2].style.reversible != rev {
		return fmt.Errorf("%w: multiple component transformation over unlike components", ErrMalformed)
	}
	p0, p1, p2 := &d.comps[0].coeffs, &d.comps[1].coeffs, &d.comps[2].coeffs
	if rev {
		for i := range p0.ints {
			y0, y1, y2 := p0.ints[i], p1.ints[i], p2.ints[i]
			i1 := y0 - (y2+y1)>>2
			p0.ints[i], p1.ints[i], p2.ints[i] = y2+i1, i1, y1+i1
		}
		return nil
	}
	for i := range p0.floats {
		y0, y1, y2 := p0.floats[i], p1.floats[i], p2.floats[i]
		p0.floats[i] = y0 + 1.402*y2
		p1.floats[i] = y0 - 0.34413*y1 - 0.71414*y2
		p2.floats[i] = y0 + 1.772*y1
	}
	return nil
}

// place writes one tile-component into the component's samples.
func (c *Component) place(tc *tileComponent) {
	s := c.sampler()
	for y := tc.y0; y < tc.y1; y++ {
		row := (y - c.y0) * c.Width
		for x := tc.x0; x < tc.x1; x++ {
			c.Samples[row+x-c.x0] = s.sample(&tc.coeffs, (y-tc.y0)*tc.coeffs.stride+x-tc.x0)
		}
	}
}

// sampler turns reconstructed coefficients into samples of one component.
type sampler struct {
	lo, hi, shift int64
}

// sampler returns the sampler of the component's depth and signedness.
func (c *Component) sampler() sampler {
	shift := int64(1) << (c.Precision - 1)
	if c.Signed {
		return sampler{lo: -shift, hi: shift - 1}
	}
	return sampler{lo: 0, hi: int64(1)<<c.Precision - 1, shift: shift}
}

// sample returns coefficient j of p as a sample: rounded, level shifted back
// if unsigned (T.800, G.1.2) and clipped to the component's range, the
// typical answer G.1.2 NOTE gives to values quantization pushed outside it.
func (s sampler) sample(p *plane, j int) int32 {
	var v int64
	if p.ints != nil {
		v = int64(p.ints[j])
	} else {
		v = int64(math.Round(float64(p.floats[j])))
	}
	return int32(min(max(v+s.shift, s.lo), s.hi)) //nolint:gosec // the value lies in [lo, hi], within maxSampleBits.
}
