package jpx

import "fmt"

// maxCodeBlocks bounds the code-blocks and precincts one Decode call lays
// out. It is an anti-DoS guard, not spec-mandated: T.800, B.7 lets a
// precinct exponent of 1 shrink code-blocks to a single coefficient, so a
// short codestream can describe one code-block per sample of a huge image.
// The largest image in the corpora, 5100x6600 in three components with
// 64x64 code-blocks, needs about 25 000; four million leaves room for small
// code-blocks on large images while refusing per-sample layouts of gigapixel
// ones.
const maxCodeBlocks = 1 << 22

// Sub-band orientations, T.800 Table B.1.
const (
	bandLL = iota
	bandHL
	bandLH
	bandHH
)

// tileComponent is one component of one tile, split into resolution levels,
// sub-bands, precincts and code-blocks (T.800, B.3 to B.7).
type tileComponent struct {
	// x0, y0, x1 and y1 are tcx0, tcy0, tcx1 and tcy1 (B-12).
	x0, y0, x1, y1 int
	style          *codingStyle
	quant          *quantization
	// coeffs holds the tile-component's coefficients, first as sub-bands,
	// then as samples once inverseTransform has run.
	coeffs plane
	// roi is the RGN shift, 0 without an RGN marker segment.
	roi         int
	resolutions []resolution
}

// resolution is one resolution level of a tile-component (T.800, B.5 and
// B.6).
type resolution struct {
	// x0, y0, x1 and y1 are trx0, try0, trx1 and try1 (B-14).
	x0, y0, x1, y1 int
	// ppx and ppy are the precinct size exponents, pw and ph the number of
	// precincts across and down (B-16).
	ppx, ppy  int
	pw, ph    int
	bands     []band
	precincts []precinct
}

// band is one sub-band of a resolution level (T.800, B.5 and B.7).
type band struct {
	orient int
	// level is nb, the decomposition level the sub-band comes from.
	level int
	// x0, y0, x1 and y1 are tbx0, tby0, tbx1 and tby1 (B-15).
	x0, y0, x1, y1 int
	// cbw and cbh are the code-block exponents xcb' and ycb' (B-17, B-18).
	cbw, cbh int
	// step is the index of the sub-band's quantization step size in the order
	// of T.800, F.3.1.
	step int
	// offX and offY place the sub-band in the tile-component's plane: beside,
	// below or diagonal to the resolution level below its own.
	offX, offY int
}

// precinct is one precinct of a resolution level: its code-blocks in each
// sub-band and how many of its packets have been read.
type precinct struct {
	bands []precinctBand
	// layers counts the packets of this precinct read so far, which T.800,
	// B.12.2 needs to skip packets a progression volume repeats.
	layers int
}

// precinctBand holds the code-blocks of one sub-band inside one precinct, in
// raster order, with the tag trees that code their inclusion and zero
// bit-planes (T.800, B.10.2, B.10.4 and B.10.5).
type precinctBand struct {
	w, h       int
	blocks     []codeBlock
	inclusion  *tagTree
	zeroPlanes *tagTree
}

// codeBlock is one code-block: its rectangle in sub-band coordinates and the
// coded data its packets have delivered.
type codeBlock struct {
	x0, y0, x1, y1 int
	included       bool
	// lblock is the Lblock state of T.800, B.10.7.1.
	lblock int
	// zeroPlanes is P, the missing most significant bit-planes (B.10.5).
	zeroPlanes int
	passes     int
	segments   []codewordSegment
}

// codewordSegment is a run of coding passes the encoder terminated as a unit
// (T.800, D.4 and D.6) and the bytes that code them.
type codewordSegment struct {
	first, passes int
	data          []byte
	// closed marks a segment whose last pass was terminated, so later passes
	// start a new one.
	closed bool
}

// ceilShift returns ceil(v / 2^s). It holds for negative v too, since >>
// rounds toward minus infinity.
func ceilShift(v, s int) int {
	return (v + 1<<s - 1) >> s
}

// blockBudget counts the precincts and code-blocks one Decode call lays out
// against maxCodeBlocks.
type blockBudget struct {
	used int
}

// charge counts a w x h grid, refusing it before w*h can overflow.
func (b *blockBudget) charge(w, h int) error {
	if w > maxCodeBlocks || h > maxCodeBlocks || b.used+w*h > maxCodeBlocks {
		return fmt.Errorf("%w: more than %d precincts and code-blocks", ErrLimit, maxCodeBlocks)
	}
	b.used += w * h
	return nil
}

// layoutComponent splits one tile-component into resolution levels, sub-bands,
// precincts and code-blocks, counting code-blocks and precincts against
// budget.
func layoutComponent(tc *tileComponent, budget *blockBudget) error {
	s := tc.style
	nl := s.levels
	tc.resolutions = make([]resolution, nl+1)
	for r := range tc.resolutions {
		res := &tc.resolutions[r]
		// B-14.
		res.x0, res.y0 = ceilShift(tc.x0, nl-r), ceilShift(tc.y0, nl-r)
		res.x1, res.y1 = ceilShift(tc.x1, nl-r), ceilShift(tc.y1, nl-r)
		res.ppx, res.ppy = s.ppx[r], s.ppy[r]
		// B-16.
		if res.x1 > res.x0 {
			res.pw = ceilShift(res.x1, res.ppx) - res.x0>>res.ppx
		}
		if res.y1 > res.y0 {
			res.ph = ceilShift(res.y1, res.ppy) - res.y0>>res.ppy
		}
		// B-17 and B-18: code-blocks never exceed a precinct, whose extent in
		// a sub-band above level 0 is half that in the resolution level.
		cbw, cbh := min(s.cbw, res.ppx), min(s.cbh, res.ppy)
		if r == 0 {
			res.bands = []band{{orient: bandLL, level: nl}}
		} else {
			cbw, cbh = min(s.cbw, res.ppx-1), min(s.cbh, res.ppy-1)
			res.bands = []band{{orient: bandHL}, {orient: bandLH}, {orient: bandHH}}
		}
		for i := range res.bands {
			b := &res.bands[i]
			b.cbw, b.cbh = cbw, cbh
			if r == 0 {
				b.x0, b.y0, b.x1, b.y1 = res.x0, res.y0, res.x1, res.y1
				continue
			}
			b.level = nl - r + 1
			b.step = 1 + 3*(r-1) + i
			// The level below this one fills lw x lh at the top left.
			below := &tc.resolutions[r-1]
			lw, lh := below.x1-below.x0, below.y1-below.y0
			if b.orient != bandLH {
				b.offX = lw
			}
			if b.orient != bandHL {
				b.offY = lh
			}
			// B-15 with the offsets of Table B.1: HL is high-pass across,
			// LH down, HH both.
			xo, yo := 0, 0
			if b.orient == bandHL || b.orient == bandHH {
				xo = 1 << (b.level - 1)
			}
			if b.orient == bandLH || b.orient == bandHH {
				yo = 1 << (b.level - 1)
			}
			b.x0, b.x1 = ceilShift(tc.x0-xo, b.level), ceilShift(tc.x1-xo, b.level)
			b.y0, b.y1 = ceilShift(tc.y0-yo, b.level), ceilShift(tc.y1-yo, b.level)
		}
		if err := budget.charge(res.pw, res.ph); err != nil {
			return err
		}
		res.precincts = make([]precinct, res.pw*res.ph)
		for k := range res.precincts {
			if err := layoutPrecinct(res, r, k, budget); err != nil {
				return err
			}
		}
	}
	return nil
}

// layoutPrecinct lays out the code-blocks of precinct k of resolution level r
// in each sub-band (T.800, B.6, B.7 and B.9).
func layoutPrecinct(res *resolution, r, k int, budget *blockBudget) error {
	p := &res.precincts[k]
	px, py := k%res.pw, k/res.pw
	// The precinct in resolution-level coordinates, anchored at (0, 0).
	x0 := (res.x0>>res.ppx + px) << res.ppx
	y0 := (res.y0>>res.ppy + py) << res.ppy
	x1, y1 := x0+1<<res.ppx, y0+1<<res.ppy
	if r > 0 {
		// Every sub-band of a level above 0 has half its resolution.
		x0, y0, x1, y1 = x0>>1, y0>>1, x1>>1, y1>>1
	}
	p.bands = make([]precinctBand, len(res.bands))
	for i := range res.bands {
		b := &res.bands[i]
		pb := &p.bands[i]
		bx0, by0 := max(x0, b.x0), max(y0, b.y0)
		bx1, by1 := min(x1, b.x1), min(y1, b.y1)
		if bx0 >= bx1 || by0 >= by1 {
			continue
		}
		cx0, cy0 := bx0>>b.cbw, by0>>b.cbh
		pb.w = ceilShift(bx1, b.cbw) - cx0
		pb.h = ceilShift(by1, b.cbh) - cy0
		if err := budget.charge(pb.w, pb.h); err != nil {
			return err
		}
		pb.blocks = make([]codeBlock, pb.w*pb.h)
		for j := range pb.blocks {
			cx, cy := cx0+j%pb.w, cy0+j/pb.w
			pb.blocks[j] = codeBlock{
				x0: max(cx<<b.cbw, bx0), y0: max(cy<<b.cbh, by0),
				x1: min((cx+1)<<b.cbw, bx1), y1: min((cy+1)<<b.cbh, by1),
				lblock: 3,
			}
		}
		pb.inclusion = newTagTree(pb.w, pb.h)
		pb.zeroPlanes = newTagTree(pb.w, pb.h)
	}
	return nil
}
