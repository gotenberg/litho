package jbig2

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
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

// textSource supplies the coded fields of a text region, ITU-T T.88 6.4.6 to
// 6.4.11, from one coding variant: arithmetic (SBHUFF 0) or Huffman
// (SBHUFF 1). decodeText holds the procedure both share.
type textSource interface {
	// deltaT returns a strip's delta T before the SBSTRIPS multiplier (6.4.6).
	deltaT() (int64, error)
	firstS() (int64, error)
	// deltaS returns ok false at the OOB that ends a strip (6.4.8).
	deltaS() (value int64, ok bool, err error)
	curT() (int64, error)
	symbolID() (int64, error)
	refines() (bool, error)
	// refine decodes a symbol instance's refined bitmap (6.4.11 steps 1-7).
	refine(ctx context.Context, p textParams, symbol *Bitmap) (*Bitmap, error)
	// chargeBuffer charges the region buffer and each instance drawn into it
	// (budget.chargeBuffer).
	chargeBuffer(width, height int64) error
}

// decodeText runs the text region decoding procedure of ITU-T T.88, 6.4.5,
// reading every coded field from src.
func decodeText(ctx context.Context, p textParams, src textSource) (*Bitmap, error) {
	if p.numInstances > maxTextInstances {
		return nil, fmt.Errorf("%w: text region of %d symbol instances exceeds %d", ErrLimit, p.numInstances, maxTextInstances)
	}
	if err := src.chargeBuffer(int64(p.width), int64(p.height)); err != nil {
		return nil, err
	}
	region, err := newBitmap(p.width, p.height)
	if err != nil {
		return nil, err
	}
	region.fill(p.defPixel)

	// Step 2: the strip base starts at minus the first delta T.
	dt, err := src.deltaT()
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
		dt, err := src.deltaT()
		if err != nil {
			return nil, err
		}
		stripT += dt * p.strips

		var curS int64
		for first := true; ; first = false {
			if first {
				dfs, err := src.firstS()
				if err != nil {
					return nil, err
				}
				firstS += dfs
				curS = firstS
			} else {
				// 6.4.8: OOB ends the strip.
				ids, ok, err := src.deltaS()
				if err != nil {
					return nil, err
				}
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
				if curT, err = src.curT(); err != nil {
					return nil, err
				}
			}
			t := stripT + curT
			id, err := src.symbolID()
			if err != nil {
				return nil, err
			}
			if id < 0 || id >= int64(len(p.syms)) {
				return nil, fmt.Errorf("%w: symbol ID %d of %d symbols", ErrMalformed, id, len(p.syms))
			}
			symbol := p.syms[id]

			if p.refine {
				ri, err := src.refines()
				if err != nil {
					return nil, err
				}
				if ri {
					if symbol, err = src.refine(ctx, p, symbol); err != nil {
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
			// Drawing costs time in proportion to the symbol's bytes, so every
			// instance is charged, or a million instances of one large symbol
			// would escape the budget that bounds each region.
			if err := src.chargeBuffer(w, h); err != nil {
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

// refineSymbol decodes a symbol instance's refined bitmap from its refinement
// deltas, ITU-T T.88 6.4.11 step 6 and Table 12, reading the bitmap from st.
func refineSymbol(ctx context.Context, p textParams, symbol *Bitmap, rdw, rdh, rdx, rdy int64, st *arithState) (*Bitmap, error) {
	w, h := int64(symbol.Width)+rdw, int64(symbol.Height)+rdh
	if w < 0 || h < 0 || bitmapExceeds(w, h, st.budget.byteCap()) {
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

// The arithmetic variant: every field through its integer procedure (Annex A)
// and every refinement through the same decoder.

func (st *arithState) deltaT() (int64, error) { return st.iadt.value(st.dec, "strip delta T") }

func (st *arithState) firstS() (int64, error) {
	return st.iafs.value(st.dec, "first symbol instance S")
}
func (st *arithState) curT() (int64, error) { return st.iait.value(st.dec, "symbol instance T") }
func (st *arithState) symbolID() (int64, error) {
	return st.iaid.decode(st.dec), nil
}

func (st *arithState) deltaS() (int64, bool, error) {
	v, ok := st.iads.decode(st.dec)
	return v, ok, nil
}

func (st *arithState) refines() (bool, error) {
	ri, err := st.iari.value(st.dec, "RI")
	return ri != 0, err
}

func (st *arithState) chargeBuffer(width, height int64) error {
	return st.budget.chargeBuffer(width, height)
}

func (st *arithState) refine(ctx context.Context, p textParams, symbol *Bitmap) (*Bitmap, error) {
	var deltas [4]int64
	for i, d := range []*intDecoder{&st.iardw, &st.iardh, &st.iardx, &st.iardy} {
		v, err := d.value(st.dec, "refinement delta")
		if err != nil {
			return nil, err
		}
		deltas[i] = v
	}
	return refineSymbol(ctx, p, symbol, deltas[0], deltas[1], deltas[2], deltas[3], st)
}

// huffText is the Huffman variant: fields through the tables 7.4.3.1.6 selects,
// symbol IDs through the table 7.4.3.1.7 sends or, inside a symbol
// dictionary, through fixed-length codes (6.5.8.2.3), and each refinement
// arithmetically coded in a byte-aligned block of its own (6.4.11 steps 5-7).
type huffText struct {
	r                         *bitReader
	fs, ds, dt                *huffTable
	rdw, rdh, rdx, rdy, rsize *huffTable
	ids                       *huffTable
	idLen                     int
	logStrips                 int
	gr                        []mq.Context
	budget                    *budget
}

func (h *huffText) deltaT() (int64, error) { return h.dt.value(h.r, "strip delta T") }
func (h *huffText) firstS() (int64, error) { return h.fs.value(h.r, "first symbol instance S") }
func (h *huffText) deltaS() (int64, bool, error) {
	return h.ds.decode(h.r)
}

// curT reads log2(SBSTRIPS) bits directly (6.4.9).
func (h *huffText) curT() (int64, error) {
	v, err := h.r.readBits(h.logStrips)
	return int64(v), err
}

func (h *huffText) symbolID() (int64, error) {
	if h.ids == nil {
		v, err := h.r.readBits(h.idLen)
		return int64(v), err
	}
	return h.ids.value(h.r, "symbol ID")
}

func (h *huffText) refines() (bool, error) {
	b, err := h.r.readBit()
	return b != 0, err
}

func (h *huffText) chargeBuffer(width, height int64) error {
	return h.budget.chargeBuffer(width, height)
}

func (h *huffText) refine(ctx context.Context, p textParams, symbol *Bitmap) (*Bitmap, error) {
	var deltas [4]int64
	for i, t := range []*huffTable{h.rdw, h.rdh, h.rdx, h.rdy} {
		v, err := t.value(h.r, "refinement delta")
		if err != nil {
			return nil, err
		}
		deltas[i] = v
	}
	block, err := h.refinementBlock()
	if err != nil {
		return nil, err
	}
	st := &arithState{dec: mq.NewDecoder(block), gr: h.gr, budget: h.budget}
	return refineSymbol(ctx, p, symbol, deltas[0], deltas[1], deltas[2], deltas[3], st)
}

// refinementBlock reads a refinement's data size with rsize, aligns, and
// returns that many bytes, leaving the reader after them (6.4.11 steps 5 and
// 7: the refinement consumes exactly the bytes its size names).
func (h *huffText) refinementBlock() ([]byte, error) {
	size, err := h.rsize.value(h.r, "refinement data size")
	if err != nil {
		return nil, err
	}
	h.r.align()
	if size < 0 || size > int64(len(h.r.data)-h.r.pos) {
		return nil, fmt.Errorf("%w: refinement data of %d bytes, %d remain", ErrMalformed, size, len(h.r.data)-h.r.pos)
	}
	block := h.r.data[h.r.pos : h.r.pos+int(size)]
	h.r.pos += int(size)
	return block, nil
}

// textSegment holds the parsed header of a text region segment, ITU-T T.88,
// 7.4.3.1, and the offset its coded data starts at.
type textSegment struct {
	info      regionInfo
	params    textParams
	huffman   bool
	huffFlags uint16
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
	// Figure 35: the Huffman flags come only with SBHUFF 1, and the symbol ID
	// table they are followed by starts at dataStart, bit-packed.
	if ts.huffman {
		ts.huffFlags = c.u16()
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

// newHuffText prepares the Huffman variant of a text region segment: the
// tables its Huffman flags select (7.4.3.1.6) and the symbol ID table that
// starts its data (7.4.3.1.7).
func newHuffText(ts textSegment, data []byte, numSyms int, user []*huffTable, gr []mq.Context, b *budget) (*huffText, error) {
	h := &huffText{r: &bitReader{data: data}, gr: gr, budget: b}
	f := ts.huffFlags
	next := 0
	picks := []struct {
		dst     **huffTable
		sel     int
		choices map[int]int
		userSel int
	}{
		{&h.fs, int(f & 3), map[int]int{0: 6, 1: 7}, 3},
		{&h.ds, int(f >> 2 & 3), map[int]int{0: 8, 1: 9, 2: 10}, 3},
		{&h.dt, int(f >> 4 & 3), map[int]int{0: 11, 1: 12, 2: 13}, 3},
		{&h.rdw, int(f >> 6 & 3), map[int]int{0: 14, 1: 15}, 3},
		{&h.rdh, int(f >> 8 & 3), map[int]int{0: 14, 1: 15}, 3},
		{&h.rdx, int(f >> 10 & 3), map[int]int{0: 14, 1: 15}, 3},
		{&h.rdy, int(f >> 12 & 3), map[int]int{0: 14, 1: 15}, 3},
		{&h.rsize, int(f >> 14 & 1), map[int]int{0: 1}, 1},
	}
	for _, pick := range picks {
		t, err := pickTable(pick.sel, pick.choices, pick.userSel, user, &next)
		if err != nil {
			return nil, err
		}
		*pick.dst = t
	}
	for strips := ts.params.strips; strips > 1; strips >>= 1 {
		h.logStrips++
	}
	ids, err := decodeSymbolIDTable(h.r, numSyms)
	if err != nil {
		return nil, err
	}
	h.ids = ids
	return h, nil
}
