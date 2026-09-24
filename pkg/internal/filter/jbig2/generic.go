package jbig2

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/ccitt"
	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// regionInfoSize is the length of the region segment information field that
// begins every region segment's data part (ITU-T T.88, 7.4.1).
const regionInfoSize = 17

// regionInfo is the region segment information field of T.88, 7.4.1.
type regionInfo struct {
	width, height int64
	x, y          int64
	combineOp     int
}

// parseRegionInfo reads the region segment information field at the start of
// a region segment's data part.
func parseRegionInfo(data []byte) (regionInfo, error) {
	c := &cursor{b: data}
	info := regionInfo{
		width:  int64(c.u32()),
		height: int64(c.u32()),
		x:      int64(c.u32()),
		y:      int64(c.u32()),
	}
	flags := c.u8()
	if c.err != nil {
		return regionInfo{}, c.err
	}
	// 7.4.1.5: bits 0-2 carry the external combination operator, and values
	// above REPLACE are undefined.
	info.combineOp = int(flags & 7)
	if info.combineOp > combineReplace {
		return regionInfo{}, fmt.Errorf("%w: region combination operator %d", ErrMalformed, info.combineOp)
	}
	return info, nil
}

// genericATBytes is the length of the generic region segment AT flags field
// for a template: four AT pixels of two bytes for template 0, one otherwise
// (T.88, 7.4.6.3).
func genericATBytes(template int) int {
	if template == 0 {
		return 8
	}
	return 2
}

// genericParams holds the parameters of the generic region decoding procedure
// that the arithmetic path uses (T.88, Table 2). USESKIP is expressed by a
// non-nil skip bitmap; only the gray-scale planes of a halftone region set it
// (Table C.4), since generic region, symbol and pattern bitmaps fix it to 0.
type genericParams struct {
	width, height int
	template      int
	tpgdon        bool
	skip          *Bitmap
	// at holds the (x, y) offsets of the adaptive template pixels A1 to A4;
	// only A1 is used unless template is 0.
	at [4][2]int
}

// templateRun is one row of fixed template pixels: those at horizontal offsets
// x0 to x1 inclusive, dy rows above the pixel being decoded.
type templateRun struct{ dy, x0, x1 int }

// genericTemplates lists the fixed pixels of each generic region template,
// ITU-T T.88 Figures 3 to 6, top row first. The adaptive pixels are not part
// of these runs; their nominal positions are in Table 5.
//
//nolint:gochecknoglobals // static spec table, ITU-T T.88 Figures 3 to 6
var genericTemplates = [4][]templateRun{
	{{-2, -1, 1}, {-1, -2, 2}, {0, -4, -1}}, // Figure 3: 12 fixed pixels, A1-A4
	{{-2, -1, 2}, {-1, -2, 2}, {0, -3, -1}}, // Figure 4: 12 fixed pixels, A1
	{{-2, -1, 1}, {-1, -2, 1}, {0, -2, -1}}, // Figure 5: 9 fixed pixels, A1
	{{-1, -3, 1}, {0, -4, -1}},              // Figure 6: 9 fixed pixels, A1
}

// sltpPixels gives, per template, the pixel values of the context T.88 uses to
// decode SLTP (Figures 8 to 11): the fixed pixels row by row, left to right,
// followed by A1 to A4. The AT pixels take these values wherever they have
// been moved (6.2.5.7 step 3 b, NOTE).
//
//nolint:gochecknoglobals // static spec table, ITU-T T.88 Figures 8 to 11
var sltpPixels = [4]struct{ fixed, at []uint32 }{
	{fixed: []uint32{0, 0, 1, 1, 1, 0, 0, 1, 0, 1, 0, 1}, at: []uint32{0, 0, 1, 1}}, // Figure 8
	{fixed: []uint32{0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 1}, at: []uint32{0}},          // Figure 9
	{fixed: []uint32{0, 0, 1, 1, 1, 0, 0, 0, 1}, at: []uint32{1}},                   // Figure 10
	{fixed: []uint32{0, 1, 1, 0, 0, 0, 1, 0, 1}, at: []uint32{1}},                   // Figure 11
}

// atCount is the number of adaptive template pixels a template uses.
func atCount(template int) int {
	if template == 0 {
		return 4
	}
	return 1
}

// sltpContext returns the context value used to decode SLTP for a template, in
// the same bit order decodeGeneric gathers pixels in: fixed pixels first, row
// by row from the top, left to right, then A1 to A4. T.88, 6.2.5.7 step 3 ii
// leaves the gathering order to the decoder provided it is consistent, and
// this is the one place outside the pixel loop that depends on it.
func sltpContext(template int) uint32 {
	var cx uint32
	for _, v := range sltpPixels[template].fixed {
		cx = cx<<1 | v
	}
	for _, v := range sltpPixels[template].at {
		cx = cx<<1 | v
	}
	return cx
}

// genericContexts returns the number of contexts a template addresses: one
// per value of its 10 to 16 template pixels.
func genericContexts(template int) int {
	n := atCount(template)
	for _, run := range genericTemplates[template] {
		n += run.x1 - run.x0 + 1
	}
	return 1 << n
}

// decodeGeneric runs the generic region decoding procedure with template-based
// arithmetic coding, ITU-T T.88, 6.2.5.7, reading decisions from dec and
// adapting cx, which must hold genericContexts(p.template) contexts.
//
// The fixed template pixels are kept in one shift register per template row,
// so each pixel costs one read per row above it instead of one per template
// pixel. Only the adaptive pixels, which may sit anywhere in the field of
// Figure 7, are read individually.
func decodeGeneric(ctx context.Context, p genericParams, dec *mq.Decoder, cx []mq.Context) (*Bitmap, error) {
	if p.template < 0 || p.template > 3 {
		return nil, fmt.Errorf("%w: generic region template %d", ErrMalformed, p.template)
	}
	if len(cx) != genericContexts(p.template) {
		return nil, fmt.Errorf("generic region template %d needs %d contexts, got %d", p.template, genericContexts(p.template), len(cx))
	}
	bm, err := newBitmap(p.width, p.height)
	if err != nil {
		return nil, err
	}
	runs := genericTemplates[p.template]
	nAT := atCount(p.template)
	sltp := sltpContext(p.template)

	regs := make([]uint32, len(runs))
	widths := make([]uint, len(runs))
	for i, run := range runs {
		widths[i] = uint(run.x1 - run.x0 + 1)
	}

	ltp := 0
	for y := range p.height {
		if y%rowsPerCancelCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if p.tpgdon {
			ltp ^= dec.Decode(&cx[sltp])
			if ltp == 1 {
				// 6.2.5.7 step 3 c: a typical row repeats the row above; the
				// first row has none, and a pixel outside the bitmap is 0.
				if y > 0 {
					copy(bm.Data[y*bm.Stride:(y+1)*bm.Stride], bm.Data[(y-1)*bm.Stride:y*bm.Stride])
				}
				continue
			}
		}

		for i, run := range runs {
			regs[i] = 0
			for dx := run.x0; dx <= run.x1; dx++ {
				regs[i] = regs[i]<<1 | bm.pixel(dx, y+run.dy)
			}
		}
		for x := range p.width {
			var context uint32
			for i := range runs {
				context = context<<widths[i] | regs[i]
			}
			for a := range nAT {
				context = context<<1 | bm.pixel(x+p.at[a][0], y+p.at[a][1])
			}

			var bit uint32
			// 6.2.5.7 step 3 d i: a skipped pixel is 0 and is not decoded.
			if (p.skip == nil || p.skip.pixel(x, y) == 0) && dec.Decode(&cx[context]) != 0 {
				bit = 1
				bm.setPixel(x, y, 1)
			}

			// Slide every register one pixel right. The current row's run
			// ends at x-1, so its new rightmost pixel is the one just decoded.
			for i, run := range runs {
				mask := uint32(1)<<widths[i] - 1
				next := bit
				if run.dy != 0 {
					next = bm.pixel(x+1+run.x1, y+run.dy)
				}
				regs[i] = (regs[i]<<1 | next) & mask
			}
		}
	}
	return bm, nil
}

// rowsPerCancelCheck is how many bitmap rows a decoding loop decodes between
// checks of its context for cancellation. A row costs at most a few hundred
// microseconds at the widths maxBitmapPixels allows, so 16 rows keep the
// latency of a cancellation well under a tenth of a second.
const rowsPerCancelCheck = 16

// decodeMMR runs the generic region decoding procedure with MMR coding, ITU-T
// T.88, 6.2.6: T.6 two-dimensional coding in which black decodes to 1. The
// data length is known wherever this is called, so a missing EOFB is not an
// error (6.2.6).
func decodeMMR(ctx context.Context, width, height int, data []byte) (*Bitmap, error) {
	bm, _, err := decodeMMRBlock(ctx, width, height, data)
	return bm, err
}

// decodeMMRBlock decodes an MMR-coded bitmap and the EOFB that may end it,
// and reports how many whole bytes they occupied, so a following block can
// start after it (T.88 6.2.6: MMR data consumes an integral number of bytes).
func decodeMMRBlock(ctx context.Context, width, height int, data []byte) (*Bitmap, int, error) {
	bm, err := newBitmap(width, height)
	if err != nil {
		return nil, 0, err
	}
	if width == 0 || height == 0 {
		return bm, 0, nil
	}
	rows, used, err := ccitt.DecodeBlock(ctx, data, ccitt.Params{K: -1, Columns: width, Rows: height, BlackIs1: true})
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, err
		}
		return nil, 0, fmt.Errorf("%w: MMR data of %d rows of %d pixels: %w", ErrMalformed, height, width, err)
	}
	copy(bm.Data, rows)
	return bm, used, nil
}
