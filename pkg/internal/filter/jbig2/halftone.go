package jbig2

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/image/ccitt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// maxPatterns bounds the patterns one pattern dictionary may define. It is an
// anti-DoS guard, not spec-mandated: GRAYMAX is a 32-bit field, and a
// dictionary of zero-height patterns would otherwise demand billions of them
// at no pixel cost. The largest of the 21 pattern dictionaries in the test
// corpora holds 625 patterns (10-bit gray-scale values); 2^16 allows a full
// 16-bit gray scale.
const maxPatterns = 1 << 16

// maxGrayBits bounds GSBPP, the bits per gray-scale value. It follows from
// maxPatterns: a halftone needs no more bits than it takes to index the
// patterns of its dictionary.
const maxGrayBits = 16

// patternDict holds the patterns of a pattern dictionary segment.
type patternDict struct {
	patterns []*Bitmap
}

// decodePatternDict decodes a pattern dictionary segment, ITU-T T.88 7.4.4
// and the procedure of 6.7.5.
func decodePatternDict(ctx context.Context, seg segment, b *budget) (*patternDict, error) {
	c := &cursor{b: seg.data}
	flags := c.u8()
	width := int64(c.u8())
	height := int64(c.u8())
	grayMax := int64(c.u32())
	if c.err != nil {
		return nil, c.err
	}
	mmr := flags&1 != 0
	template := int(flags>>1) & 3
	count := grayMax + 1
	if count > maxPatterns {
		return nil, fmt.Errorf("%w: pattern dictionary of %d patterns exceeds %d", ErrLimit, count, maxPatterns)
	}
	// Step 1: all the patterns side by side in one collective bitmap.
	total := count * width
	if err := b.charge(total, height); err != nil {
		return nil, err
	}
	data := seg.data[c.off:]
	var collective *Bitmap
	var err error
	if mmr {
		// The data length is known, so EOFB may be absent (6.2.6).
		collective, err = decodeMMR(int(total), int(height), data)
	} else {
		// Table 27: AT pixel A1 sits one pattern to the left, the others at
		// their nominal places.
		p := genericParams{width: int(total), height: int(height), template: template}
		p.at = [4][2]int{{-int(width), 0}, {-3, -1}, {2, -2}, {-2, -2}}
		collective, err = decodeGeneric(ctx, p, mq.NewDecoder(data), make([]mq.Context, genericContexts(template)))
	}
	if err != nil {
		return nil, err
	}
	dict := &patternDict{patterns: make([]*Bitmap, count)}
	for gray := range count {
		if dict.patterns[gray], err = collective.columns(gray*width, width); err != nil {
			return nil, err
		}
	}
	return dict, nil
}

// halftoneHeader is the halftone region segment data header, ITU-T T.88
// 7.4.5.1.
type halftoneHeader struct {
	info       regionInfo
	mmr        bool
	template   int
	enableSkip bool
	combineOp  int
	defPixel   uint32
	gridW      int64
	gridH      int64
	gridX      int64
	gridY      int64
	vectorX    int64
	vectorY    int64
	dataStart  int
}

func parseHalftoneHeader(data []byte) (halftoneHeader, error) {
	info, err := parseRegionInfo(data)
	if err != nil {
		return halftoneHeader{}, err
	}
	c := &cursor{b: data, off: regionInfoSize}
	flags := c.u8()
	h := halftoneHeader{
		info:       info,
		mmr:        flags&1 != 0,
		template:   int(flags>>1) & 3,
		enableSkip: flags&8 != 0,
		combineOp:  int(flags>>4) & 7,
		defPixel:   uint32(flags>>7) & 1,
	}
	h.gridW = int64(c.u32())
	h.gridH = int64(c.u32())
	h.gridX = c.s32()
	h.gridY = c.s32()
	h.vectorX = int64(c.u16())
	h.vectorY = int64(c.u16())
	if c.err != nil {
		return halftoneHeader{}, c.err
	}
	if h.combineOp > combineReplace {
		return halftoneHeader{}, fmt.Errorf("%w: halftone combination operator %d", ErrMalformed, h.combineOp)
	}
	h.dataStart = c.off
	return h, nil
}

// decodeHalftone decodes a halftone region segment, ITU-T T.88 7.4.5.2 and
// the procedure of 6.6.5, drawing the patterns of dict.
func decodeHalftone(ctx context.Context, seg segment, dict *patternDict, b *budget) (*Bitmap, regionInfo, error) {
	h, err := parseHalftoneHeader(seg.data)
	if err != nil {
		return nil, regionInfo{}, err
	}
	if err := b.charge(h.info.width, h.info.height); err != nil {
		return nil, regionInfo{}, err
	}
	region, err := newBitmap(int(h.info.width), int(h.info.height))
	if err != nil {
		return nil, regionInfo{}, err
	}
	region.fill(h.defPixel)
	if len(dict.patterns) == 0 {
		return nil, regionInfo{}, fmt.Errorf("%w: halftone region over an empty pattern dictionary", ErrMalformed)
	}
	if areaExceeds(h.gridW, h.gridH, maxBitmapPixels) {
		return nil, regionInfo{}, fmt.Errorf("%w: halftone grid of %dx%d cells exceeds %d", ErrLimit, h.gridW, h.gridH, maxBitmapPixels)
	}
	pw, ph := int64(dict.patterns[0].Width), int64(dict.patterns[0].Height)

	// cell returns the top left corner of grid cell (ng, mg), 6.6.5.1 step 1
	// a i and 6.6.5.2: the grid origin plus the grid vectors, in 1/256
	// pixel units, shifted arithmetically.
	cell := func(ng, mg int64) (int64, int64) {
		return (h.gridX + mg*h.vectorY + ng*h.vectorX) >> 8, (h.gridY + mg*h.vectorX - ng*h.vectorY) >> 8
	}

	var skip *Bitmap
	if h.enableSkip {
		// 6.6.5.1: skip every cell whose pattern would fall wholly outside.
		if skip, err = newBitmap(int(h.gridW), int(h.gridH)); err != nil {
			return nil, regionInfo{}, err
		}
		for mg := range h.gridH {
			for ng := range h.gridW {
				x, y := cell(ng, mg)
				if x+pw <= 0 || x >= h.info.width || y+ph <= 0 || y >= h.info.height {
					skip.setPixel(int(ng), int(mg), 1)
				}
			}
		}
	}

	bits := symbolCodeLen(len(dict.patterns))
	gray, err := decodeGrayScale(ctx, grayParams{
		width: int(h.gridW), height: int(h.gridH), bits: bits,
		mmr: h.mmr, template: h.template, skip: skip,
	}, seg.data[h.dataStart:], b)
	if err != nil {
		return nil, regionInfo{}, err
	}

	// 6.6.5.2: draw each cell's pattern at its grid position.
	for mg := range h.gridH {
		if err := ctx.Err(); err != nil {
			return nil, regionInfo{}, err
		}
		for ng := range h.gridW {
			v := gray[mg*h.gridW+ng]
			if v >= len(dict.patterns) {
				return nil, regionInfo{}, fmt.Errorf("%w: gray-scale value %d of %d patterns", ErrMalformed, v, len(dict.patterns))
			}
			x, y := cell(ng, mg)
			if err := b.charge(pw, ph); err != nil {
				return nil, regionInfo{}, err
			}
			if err := region.combine(dict.patterns[v], x, y, h.combineOp); err != nil {
				return nil, regionInfo{}, err
			}
		}
	}
	return region, h.info, nil
}

// grayParams holds the parameters of the gray-scale image decoding procedure,
// ITU-T T.88 Table C.1.
type grayParams struct {
	width, height int
	bits          int
	mmr           bool
	template      int
	skip          *Bitmap
}

// decodeGrayScale decodes a gray-scale image, ITU-T T.88 C.5: its bitplanes,
// most significant first, each Gray-coded against the one above, and returns
// the values row by row.
//
// With arithmetic coding every plane continues the same coded data and the
// same generic statistics. With MMR each plane is its own block ending in
// EOFB (6.2.6: the byte count is not known here), and the next starts on the
// following byte.
func decodeGrayScale(ctx context.Context, p grayParams, data []byte, b *budget) ([]int, error) {
	if p.bits < 0 || p.bits > maxGrayBits {
		return nil, fmt.Errorf("%w: %d bits per gray-scale value", ErrLimit, p.bits)
	}
	// A dictionary of one pattern needs zero bits: every value is 0 and no
	// plane is coded (6.6.5 step 3, HBPP = log2 of 1).
	values := make([]int, p.width*p.height)
	if p.bits == 0 {
		return values, nil
	}
	// Table C.4: AT pixel A1 at (3, -1) for templates 0 and 1, (2, -1) for 2
	// and 3; the others at their nominal places.
	gp := genericParams{width: p.width, height: p.height, template: p.template, skip: p.skip}
	gp.at = [4][2]int{{3, -1}, {-3, -1}, {2, -2}, {-2, -2}}
	if p.template >= 2 {
		gp.at[0] = [2]int{2, -1}
	}
	var dec *mq.Decoder
	var cx []mq.Context
	if !p.mmr {
		dec = mq.NewDecoder(data)
		cx = make([]mq.Context, genericContexts(p.template))
	}
	var above *Bitmap
	offset := 0
	for j := p.bits - 1; j >= 0; j-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := b.charge(int64(p.width), int64(p.height)); err != nil {
			return nil, err
		}
		var plane *Bitmap
		var err error
		if p.mmr {
			var used int
			plane, used, err = decodeMMRBlock(p.width, p.height, data[offset:])
			offset += used
		} else {
			plane, err = decodeGeneric(ctx, gp, dec, cx)
		}
		if err != nil {
			return nil, fmt.Errorf("gray-scale bitplane %d: %w", j, err)
		}
		for y := range p.height {
			for x := range p.width {
				v := plane.pixel(x, y)
				if above != nil {
					v ^= above.pixel(x, y)
					if v != 0 {
						plane.setPixel(x, y, 1)
					} else {
						plane.setPixel(x, y, 0)
					}
				}
				if v != 0 {
					values[y*p.width+x] |= 1 << j
				}
			}
		}
		above = plane
	}
	return values, nil
}

// oneByteReader serves its data one byte per Read and counts the bytes
// served.
type oneByteReader struct {
	data []byte
	n    int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.n >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.n]
	r.n++
	return 1, nil
}

// decodeMMRBlock decodes an MMR-coded bitmap that ends with EOFB and reports
// how many whole bytes it occupied, so a following block can start after it
// (T.88 6.2.6: MMR data consumes an integral number of bytes).
//
// The count relies on how the CCITT reader consumes its input: it asks for
// more bytes only when it has run out of bits, so a source that yields one
// byte per Read has served exactly the bytes the block used, the last one
// padded. The EOFB is checked by the Read that follows the final row.
func decodeMMRBlock(width, height int, data []byte) (*Bitmap, int, error) {
	bm, err := newBitmap(width, height)
	if err != nil {
		return nil, 0, err
	}
	if width == 0 || height == 0 {
		return bm, 0, nil
	}
	src := &oneByteReader{data: data}
	r := ccitt.NewReader(src, ccitt.MSB, ccitt.Group4, width, height, &ccitt.Options{Invert: true})
	if _, err := io.ReadFull(r, bm.Data); err != nil {
		return nil, 0, fmt.Errorf("%w: MMR bitplane: %w", ErrMalformed, err)
	}
	var tail [1]byte
	if _, err := r.Read(tail[:]); err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, fmt.Errorf("%w: MMR bitplane end: %w", ErrMalformed, err)
	}
	return bm, src.n, nil
}

// referredPatternDict returns the pattern dictionary a halftone region refers
// to, which 7.3.1 requires to be its only referred-to segment.
func referredPatternDict(seg segment, dicts map[uint32]*patternDict) (*patternDict, error) {
	if len(seg.referred) != 1 {
		return nil, fmt.Errorf("%w: halftone region refers to %d segments, not one pattern dictionary", ErrMalformed, len(seg.referred))
	}
	dict, ok := dicts[seg.referred[0]]
	if !ok {
		return nil, fmt.Errorf("%w: halftone region refers to segment %d, which is no pattern dictionary", ErrMalformed, seg.referred[0])
	}
	return dict, nil
}
