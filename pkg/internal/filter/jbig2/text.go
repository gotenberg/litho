package jbig2

import (
	"context"
	"fmt"
)

// maxTextInstances bounds the symbol instances one text region may declare.
// It is an anti-DoS guard, not spec-mandated: SBNUMINSTANCES is a 32-bit
// field, and past the end of its data the arithmetic decoder keeps producing
// instances, so four bytes could demand four billion iterations. A 300 dpi
// letter page of dense text holds about ten thousand glyphs, so this leaves a
// hundredfold margin.
const maxTextInstances = 1 << 20

// Reference corners of a symbol instance, ITU-T T.88, 7.4.3.1.1.
const (
	cornerBottomLeft = iota
	cornerTopLeft
	cornerBottomRight
	cornerTopRight
)

// textParams holds the parameters of the text region decoding procedure that
// the arithmetic path uses, ITU-T T.88 Table 9.
type textParams struct {
	width, height int
	numInstances  int64
	strips        int64
	syms          []*Bitmap
	defPixel      uint32
	combineOp     int
	transposed    bool
	refCorner     int
	dsOffset      int64
	refine        bool
	refTemplate   int
	refAT         [2][2]int
}

// decodeText runs the text region decoding procedure of ITU-T T.88, 6.4.5,
// with arithmetic coding, reading every field from st. st.iaid must decode IDs
// of symbolCodeLen(len(p.syms)) bits.
func decodeText(ctx context.Context, p textParams, st *arithState) (*Bitmap, error) {
	if p.numInstances > maxTextInstances {
		return nil, fmt.Errorf("%w: text region of %d symbol instances exceeds %d", ErrLimit, p.numInstances, maxTextInstances)
	}
	if err := st.budget.charge(int64(p.width), int64(p.height)); err != nil {
		return nil, err
	}
	region, err := newBitmap(p.width, p.height)
	if err != nil {
		return nil, err
	}
	region.fill(p.defPixel)

	// Step 2: the strip base starts at minus the first delta T.
	dt, err := st.iadt.value(st.dec, "initial STRIPT")
	if err != nil {
		return nil, err
	}
	stripT := -dt * p.strips
	var firstS int64
	var instances int64
	for instances < p.numInstances {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dt, err := st.iadt.value(st.dec, "strip delta T")
		if err != nil {
			return nil, err
		}
		stripT += dt * p.strips

		var curS int64
		for first := true; ; first = false {
			if first {
				dfs, err := st.iafs.value(st.dec, "first symbol instance S")
				if err != nil {
					return nil, err
				}
				firstS += dfs
				curS = firstS
			} else {
				// 6.4.8: OOB ends the strip.
				ids, ok := st.iads.decode(st.dec)
				if !ok {
					break
				}
				curS += ids + p.dsOffset
			}
			if instances >= maxTextInstances {
				return nil, fmt.Errorf("%w: text region strips hold more than %d symbol instances", ErrLimit, maxTextInstances)
			}

			var curT int64
			if p.strips > 1 {
				if curT, err = st.iait.value(st.dec, "symbol instance T"); err != nil {
					return nil, err
				}
			}
			t := stripT + curT
			id := st.iaid.decode(st.dec)
			if id >= int64(len(p.syms)) {
				return nil, fmt.Errorf("%w: symbol ID %d of %d symbols", ErrMalformed, id, len(p.syms))
			}
			symbol := p.syms[id]

			if p.refine {
				ri, err := st.iari.value(st.dec, "RI")
				if err != nil {
					return nil, err
				}
				if ri != 0 {
					if symbol, err = refineInstance(ctx, p, st, symbol); err != nil {
						return nil, err
					}
				}
			}

			w, h := int64(symbol.Width), int64(symbol.Height)
			// Step 3 c vi: advance to the reference corner's side first when the
			// corner is on the far side of the strip direction.
			switch {
			case !p.transposed && (p.refCorner == cornerTopRight || p.refCorner == cornerBottomRight):
				curS += w - 1
			case p.transposed && (p.refCorner == cornerBottomLeft || p.refCorner == cornerBottomRight):
				curS += h - 1
			}
			s := curS

			// Step 3 c viii: place the corner at (S, T), or at (T, S) when
			// transposed.
			x, y := s, t
			if p.transposed {
				x, y = t, s
			}
			if p.refCorner == cornerTopRight || p.refCorner == cornerBottomRight {
				x -= w - 1
			}
			if p.refCorner == cornerBottomLeft || p.refCorner == cornerBottomRight {
				y -= h - 1
			}
			// Drawing costs time in proportion to the symbol's area, so every
			// instance is charged, or a million instances of one large symbol
			// would escape the budget that bounds each region.
			if err := st.budget.charge(w, h); err != nil {
				return nil, err
			}
			if err := region.combine(symbol, x, y, p.combineOp); err != nil {
				return nil, err
			}

			// Step 3 c x.
			switch {
			case !p.transposed && (p.refCorner == cornerTopLeft || p.refCorner == cornerBottomLeft):
				curS += w - 1
			case p.transposed && (p.refCorner == cornerTopLeft || p.refCorner == cornerTopRight):
				curS += h - 1
			}
			instances++
		}
	}
	return region, nil
}

// refineInstance decodes a symbol instance's refined bitmap, ITU-T T.88,
// 6.4.11 steps 1 to 6 and Table 12.
func refineInstance(ctx context.Context, p textParams, st *arithState, symbol *Bitmap) (*Bitmap, error) {
	rdw, err := st.iardw.value(st.dec, "refinement delta width")
	if err != nil {
		return nil, err
	}
	rdh, err := st.iardh.value(st.dec, "refinement delta height")
	if err != nil {
		return nil, err
	}
	rdx, err := st.iardx.value(st.dec, "refinement X offset")
	if err != nil {
		return nil, err
	}
	rdy, err := st.iardy.value(st.dec, "refinement Y offset")
	if err != nil {
		return nil, err
	}
	w, h := int64(symbol.Width)+rdw, int64(symbol.Height)+rdh
	if w < 0 || h < 0 || areaExceeds(w, h, maxBitmapPixels) {
		return nil, fmt.Errorf("%w: refined symbol of %dx%d pixels", ErrMalformed, w, h)
	}
	// Table 12 gives GRREFERENCEDX as RDWI/2 + RDXI. T.88 (2000) does not
	// say how the halving rounds, and the two readings differ only for an odd
	// negative delta. Rounding toward minus infinity is the one the standard's
	// own example needs: page 3 of the Annex H.1 datastream refines a symbol
	// by such a delta, and truncating toward zero shifts it by a pixel.
	return decodeRefinement(ctx, refineParams{
		width:     int(w),
		height:    int(h),
		template:  p.refTemplate,
		reference: symbol,
		dx:        rdw>>1 + rdx,
		dy:        rdh>>1 + rdy,
		at:        p.refAT,
	}, st)
}

// textSegment holds the parsed header of a text region segment, ITU-T T.88,
// 7.4.3.1, and the offset its coded data starts at.
type textSegment struct {
	info      regionInfo
	params    textParams
	huffman   bool
	dataStart int
}

// parseTextSegment reads a text region segment data header.
func parseTextSegment(data []byte) (textSegment, error) {
	info, err := parseRegionInfo(data)
	if err != nil {
		return textSegment{}, err
	}
	c := &cursor{b: data, off: regionInfoSize}
	flags := c.u16()
	ts := textSegment{info: info, huffman: flags&1 != 0}
	p := &ts.params
	p.refine = flags&2 != 0
	p.strips = 1 << (flags >> 2 & 3)
	p.refCorner = int(flags >> 4 & 3)
	p.transposed = flags&0x40 != 0
	p.combineOp = int(flags >> 7 & 3)
	p.defPixel = uint32(flags>>9) & 1
	// SBDSOFFSET is a signed five-bit field (7.4.3.1.1, bits 10-14).
	p.dsOffset = int64(flags >> 10 & 0x1F)
	if p.dsOffset >= 16 {
		p.dsOffset -= 32
	}
	p.refTemplate = int(flags >> 15)
	if ts.huffman {
		// The Huffman flags field and the symbol ID table belong to the
		// Huffman variant.
		return ts, nil
	}
	if p.refine && p.refTemplate == 0 {
		for i := range 2 {
			p.refAT[i][0] = c.s8()
			p.refAT[i][1] = c.s8()
		}
	}
	p.numInstances = int64(c.u32())
	if c.err != nil {
		return textSegment{}, c.err
	}
	ts.dataStart = c.off
	return ts, nil
}
