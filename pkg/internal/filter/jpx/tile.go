package jpx

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// tileDecoder decodes one tile: its coding parameters after the precedence
// rules of T.800, A.6, its components laid out into code-blocks, and the
// packets that fill them.
type tileDecoder struct {
	t *tile
	// x0, y0, x1 and y1 are tx0, ty0, tx1 and ty1 (B-7 to B-10).
	x0, y0, x1, y1 int
	sop, eph       bool
	layers         int
	transformed    bool
	volumes        []progressionChange
	comps          []tileComponent
	// pos is the offset in t.data of the next packet, and headerPos that in
	// t.packetHeaders when the headers are carried apart from the data.
	pos, headerPos int
	// truncated records that t.data ended inside a packet (B.11).
	truncated bool
}

// newTileDecoder resolves tile t's coding parameters and lays out its
// components, counting code-blocks against budget.
func (cs *codestream) newTileDecoder(t *tile, budget *blockBudget) (*tileDecoder, error) {
	// T.800, A.6.1: a tile-part COD overrides the main one for the whole
	// tile, and its per-component part overrides the main COCs too.
	cod := cs.main.cod
	if t.header.cod != nil {
		cod = t.header.cod
	}
	d := &tileDecoder{t: t, sop: cod.sop, eph: cod.eph, layers: cod.layers, transformed: cod.transformed}
	// B-6 to B-10.
	p, q := t.index%cs.tilesX, t.index/cs.tilesX
	d.x0 = max(cs.tileX0+p*cs.tileW, cs.x0)
	d.y0 = max(cs.tileY0+q*cs.tileH, cs.y0)
	d.x1 = min(cs.tileX0+(p+1)*cs.tileW, cs.x1)
	d.y1 = min(cs.tileY0+(q+1)*cs.tileH, cs.y1)
	d.comps = make([]tileComponent, len(cs.components))
	levels := 0
	for c := range d.comps {
		tc := &d.comps[c]
		comp := cs.components[c]
		tc.x0, tc.y0 = ceilDiv(d.x0, comp.dx), ceilDiv(d.y0, comp.dy)
		tc.x1, tc.y1 = ceilDiv(d.x1, comp.dx), ceilDiv(d.y1, comp.dy)
		// A.6.1 and A.6.2: tile-part COC > tile-part COD > main COC > main
		// COD. A.6.4 and A.6.5 rank quantization the same way, and A.6.3
		// lets a tile-part RGN override the main one.
		switch {
		case t.header.coc[c] != nil:
			tc.style = t.header.coc[c]
		case t.header.cod != nil:
			tc.style = &t.header.cod.style
		case cs.main.coc[c] != nil:
			tc.style = cs.main.coc[c]
		default:
			tc.style = &cs.main.cod.style
		}
		switch {
		case t.header.qcc[c] != nil:
			tc.quant = t.header.qcc[c]
		case t.header.qcd != nil:
			tc.quant = t.header.qcd
		case cs.main.qcc[c] != nil:
			tc.quant = cs.main.qcc[c]
		default:
			tc.quant = cs.main.qcd
		}
		tc.roi = max(cs.main.roi[c], 0)
		if t.header.roi[c] >= 0 {
			tc.roi = t.header.roi[c]
		}
		// A.6.4: without derivation there is one step size per sub-band.
		if need := 3*tc.style.levels + 1; tc.quant.style != quantDerived && len(tc.quant.steps) < need {
			return nil, fmt.Errorf("%w: component %d of tile %d has %d quantization step sizes for %d sub-bands", ErrMalformed, c, t.index, len(tc.quant.steps), need)
		}
		if err := layoutComponent(tc, budget); err != nil {
			return nil, err
		}
		levels = max(levels, tc.style.levels+1)
	}
	// A.6.6: tile-part POC > main POC > tile-part COD > main COD. Volume
	// bounds past the tile's layers, levels or components are clamped: the
	// loops of B.12 have nothing to visit there.
	volumes := t.header.poc
	if len(volumes) == 0 {
		volumes = cs.main.poc
	}
	if len(volumes) == 0 {
		volumes = []progressionChange{{layerEnd: d.layers, resEnd: levels, compEnd: len(d.comps), order: cod.order}}
	}
	for _, v := range volumes {
		v.layerEnd = min(v.layerEnd, d.layers)
		v.resEnd = min(v.resEnd, levels)
		v.compEnd = min(v.compEnd, len(d.comps))
		d.volumes = append(d.volumes, v)
	}
	return d, nil
}

// decodePackets reads the tile's packets in progression order and hands each
// code-block the data they carry (T.800, B.9 to B.12).
func (d *tileDecoder) decodePackets(ctx context.Context, cs *codestream) error {
	w := &progression{
		ctx: ctx, comps: d.comps, tx0: d.x0, ty0: d.y0,
		dx: make([]int, len(d.comps)), dy: make([]int, len(d.comps)),
		visit: d.readPacket,
	}
	for c, comp := range cs.components {
		w.dx[c], w.dy[c] = comp.dx, comp.dy
	}
	for _, v := range d.volumes {
		if err := w.run(v); err != nil {
			if errors.Is(err, errTruncated) {
				d.truncated = true
				return nil
			}
			return err
		}
	}
	return nil
}

// readPacket reads one packet: an optional SOP marker segment (A.8.1), the
// header from the data or from the PPM or PPT packet headers, an EPH marker
// when COD requires one (A.8.2), then the body.
func (d *tileDecoder) readPacket(s packetSlot) error {
	data := d.t.data
	if d.sop && len(data)-d.pos >= 2 && data[d.pos] == 0xFF && data[d.pos+1] == byte(markerSOP&0xFF) {
		if len(data)-d.pos < 6 {
			return errTruncated
		}
		d.pos += 6
	}
	hr := &headerReader{data: data, pos: d.pos}
	if d.t.packetHeaders != nil {
		hr = &headerReader{data: d.t.packetHeaders, pos: d.headerPos}
	}
	cbStyle := d.comps[s.c].style.cbStyle
	contributions, err := readPacketHeader(hr, s.p, s.layer, cbStyle)
	if err != nil {
		return err
	}
	if err := hr.align(); err != nil {
		return err
	}
	if d.eph {
		if len(hr.data)-hr.pos < 2 {
			return errTruncated
		}
		if hr.data[hr.pos] != 0xFF || hr.data[hr.pos+1] != byte(markerEPH&0xFF) {
			return fmt.Errorf("%w: packet header of tile %d lacks the EPH marker its COD requires", ErrMalformed, d.t.index)
		}
		hr.pos += 2
	}
	if d.t.packetHeaders != nil {
		d.headerPos = hr.pos
	} else {
		d.pos = hr.pos
	}
	for _, c := range contributions {
		for _, piece := range c.pieces {
			n := piece[0]
			// B.11: the last packet of a tile may be cut short. Its code-blocks
			// keep the bytes that did arrive.
			if n > len(data)-d.pos {
				addPiece(c.block, data[d.pos:], piece[1], cbStyle)
				d.pos = len(data)
				return errTruncated
			}
			addPiece(c.block, data[d.pos:d.pos+n], piece[1], cbStyle)
			d.pos += n
		}
	}
	return nil
}

// stepSize returns the quantization exponent and mantissa of sub-band b of tc
// (T.800, A.6.4). Derived quantization scales the NLLL exponent by the
// sub-band's decomposition level (E-5).
func (tc *tileComponent) stepSize(b *band) step {
	q := tc.quant
	if q.style == quantDerived {
		s := q.steps[0]
		s.exponent += b.level - tc.style.levels
		return s
	}
	return q.steps[b.step]
}

// magnitudeBits returns Mb for sub-band b of tc (T.800, E-2): the guard bits
// plus the sub-band's exponent, less one.
func (tc *tileComponent) magnitudeBits(b *band) int {
	return tc.quant.guards + tc.stepSize(b).exponent - 1
}

// maxReversibleBits is the most magnitude bit-planes a sub-band of the 5-3
// reversible path may have: its coefficients are held in int32. It is an
// implementation limit, not spec-mandated; a 16-bit image needs about 19.
const maxReversibleBits = 31

// Base 2 logarithms of the sub-band gains of T.800, Table E.1, by
// orientation.
var logGain = [4]int{bandLL: 0, bandHL: 1, bandLH: 1, bandHH: 2}

// decodeBlocks decodes every code-block of the tile (T.800, Annex D) and
// reconstructs its coefficients into the tile-components' planes (E.1).
func (d *tileDecoder) decodeBlocks(ctx context.Context, cs *codestream) error {
	t := &t1{}
	n := 0
	for c := range d.comps {
		tc := &d.comps[c]
		w, h := tc.x1-tc.x0, tc.y1-tc.y0
		tc.coeffs = plane{stride: w}
		if tc.style.reversible {
			tc.coeffs.ints = make([]int32, w*h)
		} else {
			tc.coeffs.floats = make([]float32, w*h)
		}
		for r := range tc.resolutions {
			res := &tc.resolutions[r]
			if tc.style.reversible {
				for i := range res.bands {
					if mb := tc.magnitudeBits(&res.bands[i]); mb > maxReversibleBits {
						return fmt.Errorf("%w: reversible sub-band of %d magnitude bit-planes", ErrUnsupported, mb)
					}
				}
			}
			for k := range res.precincts {
				for i := range res.precincts[k].bands {
					b := &res.bands[i]
					for j := range res.precincts[k].bands[i].blocks {
						if n++; n%256 == 0 {
							if err := ctx.Err(); err != nil {
								return err
							}
						}
						cb := &res.precincts[k].bands[i].blocks[j]
						if err := tc.decodeBlock(t, b, cb, cs.components[c].precision); err != nil {
							return fmt.Errorf("code-block of component %d, level %d: %w", c, r, err)
						}
					}
				}
			}
		}
	}
	return nil
}

// decodeBlock decodes code-block cb of sub-band b and writes its reconstructed
// coefficients into the sub-band's plane. With r = 1/2 as the reconstruction
// parameter, a coefficient decoded to fewer than Mb bit-planes is placed at
// the middle of the interval its bits leave open (T.800, E-6 and E-8).
func (tc *tileComponent) decodeBlock(t *t1, b *band, cb *codeBlock, precision int) error {
	mb := tc.magnitudeBits(b)
	if err := t.decode(cb, b.orient, mb+tc.roi, tc.style.cbStyle); err != nil {
		return err
	}
	if cb.passes == 0 {
		return nil
	}
	// E-3 and E-4: the step size of an irreversible sub-band.
	st := tc.stepSize(b)
	delta := math.Ldexp(1+float64(st.mantissa)/2048, precision+logGain[b.orient]-st.exponent)
	stride := tc.coeffs.stride
	for j := range t.w * t.h {
		v, nb := t.coefficient(j, mb, tc.roi, cb.zeroPlanes)
		if v == 0 {
			continue
		}
		at := (b.offY+cb.y0-b.y0+j/t.w)*stride + b.offX + cb.x0 - b.x0 + j%t.w
		if tc.style.reversible {
			// E-7 and E-8. The half step is below every decoded bit, so the
			// value stays within Mb bits.
			if nb < mb {
				half := int64(1) << (mb - nb - 1)
				if v < 0 {
					half = -half
				}
				v += half
			}
			if v > math.MaxInt32 || v < math.MinInt32 {
				return fmt.Errorf("%w: coefficient %d beyond %d bits", ErrMalformed, v, maxReversibleBits)
			}
			tc.coeffs.ints[at] = int32(v)
			continue
		}
		// E-6.
		mag := math.Abs(float64(v)) + math.Ldexp(0.5, mb-nb)
		if v < 0 {
			mag = -mag
		}
		tc.coeffs.floats[at] = float32(mag * delta)
	}
	return nil
}

// inverseTransform is the IDWT procedure of T.800, F.3.1: starting from the
// NLLL sub-band, each level reconstructs, in place, the level above from the
// one below and its three high sub-bands, until the tile-component itself.
func (tc *tileComponent) inverseTransform() {
	for r := 1; r < len(tc.resolutions); r++ {
		res, below := &tc.resolutions[r], &tc.resolutions[r-1]
		reconstructLevel(&tc.coeffs, res.x0, res.y0, res.x1, res.y1, below.x1-below.x0, below.y1-below.y0)
	}
}
