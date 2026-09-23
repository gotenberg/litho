package jbig2

import (
	"context"
	"fmt"
)

// refineParams holds the parameters of the generic refinement region decoding
// procedure, ITU-T T.88 Table 6.
type refineParams struct {
	width, height int
	template      int
	reference     *Bitmap
	// dx and dy are GRREFERENCEDX and GRREFERENCEDY: the reference pixel
	// aligned with (x, y) of the refined bitmap is (x-dx, y-dy).
	dx, dy int64
	tpgron bool
	// at holds RA1, a pixel of the refined bitmap, and RA2, a pixel of the
	// reference; both are used only by template 0.
	at [2][2]int
}

// refineTemplates lists the fixed pixels of each refinement template, ITU-T
// T.88 Figures 12 and 13, as (x, y) offsets: first those of the bitmap being
// decoded, then those of the reference. The adaptive pixels RA1 and RA2 of
// template 0 are not listed.
//
//nolint:gochecknoglobals // static spec table, ITU-T T.88 Figures 12 and 13
var refineTemplates = [2]struct{ refined, reference [][2]int }{
	{ // Figure 12: 3 + RA1 refined pixels, 8 + RA2 reference pixels
		refined:   [][2]int{{0, -1}, {1, -1}, {-1, 0}},
		reference: [][2]int{{0, -1}, {1, -1}, {-1, 0}, {0, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}},
	},
	{ // Figure 13: 4 refined pixels, 6 reference pixels
		refined:   [][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}},
		reference: [][2]int{{0, -1}, {-1, 0}, {0, 0}, {1, 0}, {0, 1}, {1, 1}},
	},
}

// refineContexts returns the number of contexts a refinement template
// addresses: 8192 for the 13 pixels of template 0, 1024 for the 10 of
// template 1.
func refineContexts(template int) int {
	if template == 0 {
		return 1 << 13
	}
	return 1 << 10
}

// refineContext gathers the template around (x, y): the refined bitmap's
// pixels, RA1 for template 0, the reference's pixels, and RA2 for template 0,
// in that order from the most significant bit. T.88, 6.3.5.6 step 3 c ii
// leaves the order to the decoder provided it is consistent.
func refineContext(p *refineParams, bm *Bitmap, x, y int) uint32 {
	t := &refineTemplates[p.template]
	var cx uint32
	for _, o := range t.refined {
		cx = cx<<1 | bm.pixel(x+o[0], y+o[1])
	}
	if p.template == 0 {
		cx = cx<<1 | bm.pixel(x+p.at[0][0], y+p.at[0][1])
	}
	rx, ry := int64(x)-p.dx, int64(y)-p.dy
	for _, o := range t.reference {
		cx = cx<<1 | refPixel(p.reference, rx+int64(o[0]), ry+int64(o[1]))
	}
	if p.template == 0 {
		cx = cx<<1 | refPixel(p.reference, rx+int64(p.at[1][0]), ry+int64(p.at[1][1]))
	}
	return cx
}

// refPixel reads the reference at 64-bit coordinates, which the reference
// offsets of a text region can push past the range of int on any platform
// before clipping; every such pixel is outside the bitmap and so 0 (6.3.5.2).
func refPixel(b *Bitmap, x, y int64) uint32 {
	if x < 0 || y < 0 || x >= int64(b.Width) || y >= int64(b.Height) {
		return 0
	}
	return b.pixel(int(x), int(y))
}

// refineSLTPContext is the context of Figures 14 and 15: every template pixel
// 0 except the reference pixel aligned with the pixel being decoded. It is
// found by gathering from a reference whose only set pixel sits there, so it
// follows refineContext's bit order by construction.
func refineSLTPContext(template int) uint32 {
	one := &Bitmap{Width: 1, Height: 1, Stride: 1, Data: []byte{0x80}}
	empty := &Bitmap{}
	p := refineParams{template: template, reference: one}
	// The AT pixels are placed off the one-pixel reference so that, as the
	// figures show, they read 0.
	p.at = [2][2]int{{-1, -1}, {-1, -1}}
	return refineContext(&p, empty, 0, 0)
}

// decodeRefinement runs the generic refinement region decoding procedure of
// ITU-T T.88, 6.3.5.6, reading decisions from st.dec and adapting st.gr.
func decodeRefinement(ctx context.Context, p refineParams, st *arithState) (*Bitmap, error) {
	if p.template != 0 && p.template != 1 {
		return nil, fmt.Errorf("%w: refinement template %d", ErrMalformed, p.template)
	}
	if len(st.gr) != refineContexts(p.template) {
		return nil, fmt.Errorf("refinement template %d needs %d contexts, got %d", p.template, refineContexts(p.template), len(st.gr))
	}
	if err := st.budget.charge(int64(p.width), int64(p.height)); err != nil {
		return nil, err
	}
	bm, err := newBitmap(p.width, p.height)
	if err != nil {
		return nil, err
	}
	sltp := refineSLTPContext(p.template)
	ltp := 0
	for y := range p.height {
		if y%rowsPerCancelCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if p.tpgron {
			ltp ^= st.dec.Decode(&st.gr[sltp])
		}
		for x := range p.width {
			if ltp == 1 {
				// 6.3.5.6 step 3 d: a pixel whose 3x3 reference neighbourhood
				// (Figure 16) is uniform takes that value undecoded.
				if v, uniform := uniformNeighbourhood(p.reference, int64(x)-p.dx, int64(y)-p.dy); uniform {
					if v != 0 {
						bm.setPixel(x, y, 1)
					}
					continue
				}
			}
			if st.dec.Decode(&st.gr[refineContext(&p, bm, x, y)]) != 0 {
				bm.setPixel(x, y, 1)
			}
		}
	}
	return bm, nil
}

// uniformNeighbourhood reports whether the 3x3 block of b centred on (x, y) is
// a single value, and which.
func uniformNeighbourhood(b *Bitmap, x, y int64) (uint32, bool) {
	v := refPixel(b, x, y)
	for dy := int64(-1); dy <= 1; dy++ {
		for dx := int64(-1); dx <= 1; dx++ {
			if refPixel(b, x+dx, y+dy) != v {
				return 0, false
			}
		}
	}
	return v, true
}
