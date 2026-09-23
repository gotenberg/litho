package jbig2

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// maxDecodedPixels bounds the total area of all the regions one Decode call
// decodes. It is an anti-DoS guard, not spec-mandated: each region is capped
// by maxBitmapPixels, but a short stream can declare many regions, and an
// arithmetically coded region costs time in proportion to its area however
// few bytes back it, since the decoder reads 1-bits past the end of its data.
// Four page-sized areas leave room for a page drawn in overlapping regions and
// intermediate buffers, while refusing thousands of empty full-size regions.
const maxDecodedPixels = 4 * maxBitmapPixels

// Decode decodes one page of JBIG2 data in the embedded organisation (ITU-T
// T.88, D.3; ISO 32000-1, 7.4.7) and returns its bitmap. globals holds the
// global segments of a JBIG2Globals stream and may be empty.
//
// The page decoded is the one named by the first page information segment in
// data. Global segments, and segments of that page, are processed in order;
// segments associated with any other page are skipped.
//
// Decode is safe for concurrent use: it keeps no state between calls.
func Decode(ctx context.Context, data, globals []byte) (*Bitmap, error) {
	globalSegs, err := parseSegments(globals)
	if err != nil {
		return nil, fmt.Errorf("JBIG2Globals: %w", err)
	}
	pageSegs, err := parseSegments(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{}
	for _, seg := range globalSegs {
		if seg.page != 0 {
			return nil, fmt.Errorf("%w: JBIG2Globals segment %d is associated with page %d", ErrMalformed, seg.number, seg.page)
		}
		if err := d.process(ctx, seg); err != nil {
			return nil, err
		}
	}
	pageNumber := uint32(0)
	for _, seg := range pageSegs {
		if seg.kind == typePageInformation {
			pageNumber = seg.page
			break
		}
	}
	if pageNumber == 0 {
		return nil, fmt.Errorf("%w: no page information segment", ErrMalformed)
	}
	for _, seg := range pageSegs {
		if seg.page != 0 && seg.page != pageNumber {
			continue
		}
		if err := d.process(ctx, seg); err != nil {
			return nil, err
		}
		if d.ended {
			break
		}
	}
	return d.finish()
}

// decoder holds the state of decoding one page: the page buffer and what
// earlier segments left for later ones to use.
type decoder struct {
	page *Bitmap
	// heightUnknown records a page information segment whose height was
	// 0xFFFFFFFF (T.88, 7.4.8.2); the height then follows from the end of
	// stripe segments.
	heightUnknown bool
	defaultPixel  uint32
	// stripeEnd is one past the end row of the last end of stripe segment,
	// or 0 before the first.
	stripeEnd int
	ended     bool
	decoded   int64
}

// process decodes one segment and applies it to the page (T.88, 8.2).
func (d *decoder) process(ctx context.Context, seg segment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	switch seg.kind {
	case typePageInformation:
		err = d.pageInformation(seg)
	case typeEndOfStripe:
		err = d.endOfStripe(seg)
	case typeEndOfPage, typeEndOfFile:
		d.ended = true
	case typeImmediateGeneric, typeImmediateLosslessGeneric:
		err = d.immediateGeneric(ctx, seg)
	case typeProfiles:
		// A profiles segment lists what the stream conforms to (7.4.12); it
		// changes nothing a decoder produces.
	case typeExtension:
		err = checkExtension(seg)
	case typeSymbolDictionary, typeIntermediateText, typeImmediateText, typeImmediateLosslessText,
		typePatternDictionary, typeIntermediateHalftone, typeImmediateHalftone, typeImmediateLosslessHalf,
		typeIntermediateGeneric, typeIntermediateRefinement, typeImmediateRefinement, typeImmediateLosslessRefine,
		typeTables:
		err = fmt.Errorf("%w: segment %d of type %d", ErrUnsupported, seg.number, seg.kind)
	default:
		// 7.3: every other value is reserved and must not be used.
		err = fmt.Errorf("%w: segment %d has the reserved type %d", ErrMalformed, seg.number, seg.kind)
	}
	if err != nil {
		return fmt.Errorf("segment %d: %w", seg.number, err)
	}
	return nil
}

// checkExtension accepts an extension segment unless it is flagged necessary.
// T.88, 7.4.14: a necessary extension carries data without which the page
// cannot be decoded correctly, and no extension type defined in 7.4.15 is one.
func checkExtension(seg segment) error {
	c := &cursor{b: seg.data}
	extType := c.u32()
	if c.err != nil {
		return c.err
	}
	if extType&0x80000000 != 0 {
		return fmt.Errorf("%w: necessary extension of type 0x%08X", ErrUnsupported, extType)
	}
	return nil
}

// pageInformation creates the page buffer from a page information segment
// (T.88, 7.4.8) and fills it with the page's default pixel value (8.2 step 3).
func (d *decoder) pageInformation(seg segment) error {
	if d.page != nil {
		return fmt.Errorf("%w: second page information segment", ErrMalformed)
	}
	c := &cursor{b: seg.data}
	width := c.u32()
	height := c.u32()
	c.u32() // X resolution
	c.u32() // Y resolution
	flags := c.u8()
	c.u16() // striping information
	if c.err != nil {
		return c.err
	}
	d.defaultPixel = uint32(flags>>2) & 1
	h := int64(height)
	if height == 0xFFFFFFFF {
		// 7.4.8.2: the height arrives later, through end of stripe
		// segments. The buffer starts empty and grows with them.
		d.heightUnknown = true
		h = 0
	}
	page, err := newBitmap(int(width), int(h))
	if err != nil {
		return err
	}
	page.fill(d.defaultPixel)
	d.page = page
	return nil
}

// endOfStripe records the end row of a stripe (T.88, 7.4.10). On a page of
// unknown height the page is at least that tall.
func (d *decoder) endOfStripe(seg segment) error {
	if d.page == nil {
		return fmt.Errorf("%w: end of stripe before page information", ErrMalformed)
	}
	c := &cursor{b: seg.data}
	endRow := int64(c.u32())
	if c.err != nil {
		return c.err
	}
	if d.heightUnknown {
		if err := d.growPage(endRow + 1); err != nil {
			return err
		}
	}
	d.stripeEnd = int(min(endRow+1, int64(maxBitmapPixels)))
	return nil
}

// growPage extends a page of unknown height to at least height rows, filling
// the new rows with the default pixel value.
func (d *decoder) growPage(height int64) error {
	if height <= int64(d.page.Height) {
		return nil
	}
	grown, err := newBitmap(d.page.Width, int(min(height, int64(maxBitmapPixels)+1)))
	if err != nil {
		return err
	}
	grown.fill(d.defaultPixel)
	copy(grown.Data, d.page.Data)
	d.page = grown
	return nil
}

// immediateGeneric decodes an immediate generic region segment (T.88, 7.4.6)
// and combines it with the page (8.2 step 5 a).
func (d *decoder) immediateGeneric(ctx context.Context, seg segment) error {
	if d.page == nil {
		return fmt.Errorf("%w: region before page information", ErrMalformed)
	}
	info, err := parseRegionInfo(seg.data)
	if err != nil {
		return err
	}
	height := info.height
	if seg.rows >= 0 {
		// 7.4.6.4: a region of unknown length may hold fewer rows than its
		// header states, and its trailing row count says how many.
		if seg.rows > info.height {
			return fmt.Errorf("%w: row count %d exceeds the region height %d", ErrMalformed, seg.rows, info.height)
		}
		height = seg.rows
	}
	if err := d.charge(info.width, height); err != nil {
		return err
	}
	bm, err := decodeGenericSegment(ctx, seg.data, int(info.width), int(height))
	if err != nil {
		return err
	}
	if d.heightUnknown {
		if err := d.growPage(info.y + height); err != nil {
			return err
		}
	}
	return d.page.combine(bm, info.x, info.y, info.combineOp)
}

// charge counts a region's area against maxDecodedPixels before it is
// decoded.
func (d *decoder) charge(width, height int64) error {
	if areaExceeds(width, height, maxBitmapPixels) {
		return fmt.Errorf("%w: region of %dx%d pixels exceeds %d", ErrLimit, width, height, maxBitmapPixels)
	}
	d.decoded += width * height
	if d.decoded > maxDecodedPixels {
		return fmt.Errorf("%w: regions total more than %d pixels", ErrLimit, int64(maxDecodedPixels))
	}
	return nil
}

// decodeGenericSegment decodes the data part of a generic region segment
// (T.88, 7.4.6.4) into a width x height bitmap.
func decodeGenericSegment(ctx context.Context, data []byte, width, height int) (*Bitmap, error) {
	c := &cursor{b: data, off: regionInfoSize}
	flags := c.u8()
	if c.err != nil {
		return nil, c.err
	}
	// 7.4.6.2: bit 0 is MMR, bits 1-2 GBTEMPLATE, bit 3 TPGDON. Table 2
	// leaves GBTEMPLATE and TPGDON unused when MMR is 1.
	if flags&1 != 0 {
		return decodeMMR(width, height, data[c.off:])
	}
	p := genericParams{
		width:    width,
		height:   height,
		template: int(flags>>1) & 3,
		tpgdon:   flags&8 != 0,
	}
	for a := range atCount(p.template) {
		p.at[a][0] = c.s8()
		p.at[a][1] = c.s8()
	}
	if c.err != nil {
		return nil, c.err
	}
	// 7.4.6.4 step 2: every generic region starts from reset statistics.
	cx := make([]mq.Context, genericContexts(p.template))
	return decodeGeneric(ctx, p, mq.NewDecoder(data[c.off:]), cx)
}

// finish returns the page bitmap once every segment has been processed. A page
// of unknown height ends at the last end of stripe row (T.88, 7.4.9).
func (d *decoder) finish() (*Bitmap, error) {
	if d.page == nil {
		return nil, fmt.Errorf("%w: no page information segment", ErrMalformed)
	}
	if d.heightUnknown && d.stripeEnd > 0 && d.stripeEnd < d.page.Height {
		d.page.Height = d.stripeEnd
		d.page.Data = d.page.Data[:d.stripeEnd*d.page.Stride]
	}
	return d.page, nil
}
