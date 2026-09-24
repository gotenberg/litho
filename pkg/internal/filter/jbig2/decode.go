package jbig2

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// maxDecodedAreas bounds the total area one Decode call decodes or draws,
// counted in bitmap caps: every region, every symbol bitmap, and every symbol
// instance a text region draws. It is an anti-DoS guard, not spec-mandated:
// each bitmap is capped by the cap Decode is given, but a short stream can
// declare many of them, and arithmetically coded data costs time in proportion
// to the area it covers however few bytes back it, since the decoder reads
// 1-bits past the end of its data. Four page-sized areas leave room for a page
// drawn in overlapping regions and intermediate buffers, while refusing
// thousands of empty full-size ones.
const maxDecodedAreas = 4

// Decode decodes one page of JBIG2 data in the embedded organisation (ITU-T
// T.88, D.3; ISO 32000-1, 7.4.7) and returns its bitmap. globals holds the
// global segments of a JBIG2Globals stream and may be empty. maxPixels caps
// the area of every bitmap the call decodes, and maxDecodedAreas times it
// their total; [DefaultMaxBitmapPixels] is the cap of the default image
// budget.
//
// The page decoded is the one named by the first page information segment in
// data. Global segments, and segments of that page, are processed in order;
// segments associated with any other page are skipped.
//
// Decode is safe for concurrent use: it keeps no state between calls.
func Decode(ctx context.Context, data, globals []byte, maxPixels int64) (*Bitmap, error) {
	globalSegs, err := parseSegments(globals)
	if err != nil {
		return nil, fmt.Errorf("JBIG2Globals: %w", err)
	}
	pageSegs, err := parseSegments(data)
	if err != nil {
		return nil, err
	}
	d := &decoder{budget: budget{area: maxPixels}}
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
	budget    budget
	// dicts holds the decoded symbol dictionaries, by segment number, for the
	// segments that refer to them.
	dicts map[uint32]*symbolDict
	// aux holds the auxiliary buffers of intermediate region segments (8.2 step
	// 5 b), by the number of the segment they are currently associated with.
	aux map[uint32]*auxRegion
	// seen records every segment processed so far, so that a reference to one
	// the data never held can be told apart from one to a tables segment.
	seen map[uint32]bool
	// tables holds the Huffman tables of code table segments (7.4.13).
	tables map[uint32]*huffTable
	// patterns holds the pattern dictionaries, for halftone regions.
	patterns map[uint32]*patternDict
}

// auxRegion is an auxiliary buffer: an intermediate region's bitmap and the
// region information giving where it will eventually be drawn.
type auxRegion struct {
	bm   *Bitmap
	info regionInfo
}

// budget counts the area of the bitmaps one Decode call decodes or draws
// against maxDecodedAreas bitmap caps.
type budget struct {
	used int64
	// area is the cap on one bitmap Decode was given, or 0 for
	// DefaultMaxBitmapPixels.
	area int64
}

// areaCap returns the cap on the area of one bitmap. A nil budget has the
// default cap.
func (b *budget) areaCap() int64 {
	if b == nil || b.area <= 0 {
		return DefaultMaxBitmapPixels
	}
	return b.area
}

// charge counts width x height pixels, refusing any single area above
// areaCap and any total above maxDecodedAreas times it.
func (b *budget) charge(width, height int64) error {
	limit := b.areaCap()
	if areaExceeds(width, height, limit) {
		return fmt.Errorf("%w: region of %dx%d pixels exceeds %d", ErrLimit, width, height, limit)
	}
	b.used += width * height
	if total := maxDecodedAreas * limit; b.used > total {
		return fmt.Errorf("%w: regions total more than %d pixels", ErrLimit, total)
	}
	return nil
}

// process decodes one segment and applies it to the page (T.88, 8.2).
func (d *decoder) process(ctx context.Context, seg segment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.seen == nil {
		d.seen = make(map[uint32]bool)
	}
	d.seen[seg.number] = true
	var err error
	switch seg.kind {
	case typePageInformation:
		err = d.pageInformation(seg)
	case typeEndOfStripe:
		err = d.endOfStripe(seg)
	case typeEndOfPage, typeEndOfFile:
		d.ended = true
	case typeSymbolDictionary:
		err = d.symbolDictionary(ctx, seg)
	case typeIntermediateGeneric, typeImmediateGeneric, typeImmediateLosslessGeneric,
		typeIntermediateText, typeImmediateText, typeImmediateLosslessText,
		typeIntermediateHalftone, typeImmediateHalftone, typeImmediateLosslessHalf:
		err = d.directRegion(ctx, seg)
	case typePatternDictionary:
		err = d.patternDictionary(ctx, seg)
	case typeIntermediateRefinement, typeImmediateRefinement, typeImmediateLosslessRefine:
		err = d.refinementRegion(ctx, seg)
	case typeTables:
		err = d.codeTable(seg)
	case typeProfiles:
		// A profiles segment lists what the stream conforms to (7.4.12); it
		// changes nothing a decoder produces.
	case typeExtension:
		err = checkExtension(seg)
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
	if limit := d.budget.areaCap(); areaExceeds(int64(width), h, limit) {
		return fmt.Errorf("%w: bitmap of %dx%d pixels exceeds %d", ErrLimit, width, h, limit)
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
	d.stripeEnd = int(min(endRow+1, d.budget.areaCap()))
	return nil
}

// growPage extends a page of unknown height to at least height rows, filling
// the new rows with the default pixel value.
func (d *decoder) growPage(height int64) error {
	if height <= int64(d.page.Height) {
		return nil
	}
	limit := d.budget.areaCap()
	height = min(height, limit+1)
	if areaExceeds(int64(d.page.Width), height, limit) {
		return fmt.Errorf("%w: bitmap of %dx%d pixels exceeds %d", ErrLimit, d.page.Width, height, limit)
	}
	grown, err := newBitmap(d.page.Width, int(height))
	if err != nil {
		return err
	}
	grown.fill(d.defaultPixel)
	copy(grown.Data, d.page.Data)
	d.page = grown
	return nil
}

// referredSymbols concatenates the symbols the dictionaries seg refers to
// export, in referral order (T.88, Tables 28 and 31).
//
// Referred-to segments that are not symbol dictionaries are skipped: a tables
// segment matters only to the Huffman variants. A referred-to segment the data
// never held is malformed (7.2.5 allows references only to earlier segments),
// and is reported as such rather than as the out-of-range symbol IDs that
// decoding without its symbols would run into.
func (d *decoder) referredSymbols(seg segment) ([]*symbolDict, []*Bitmap, error) {
	var dicts []*symbolDict
	for _, n := range seg.referred {
		if dict, ok := d.dicts[n]; ok {
			dicts = append(dicts, dict)
			continue
		}
		if !d.seen[n] {
			return nil, nil, fmt.Errorf("%w: refers to segment %d, which the data does not contain", ErrMalformed, n)
		}
	}
	var n int64
	for _, dict := range dicts {
		n += int64(len(dict.exported))
	}
	if n > maxSymbols {
		return nil, nil, fmt.Errorf("%w: referred-to dictionaries export %d symbols, more than %d", ErrLimit, n, maxSymbols)
	}
	syms := make([]*Bitmap, 0, n)
	for _, dict := range dicts {
		syms = append(syms, dict.exported...)
	}
	return dicts, syms, nil
}

// symbolDictionary decodes a symbol dictionary segment (T.88, 7.4.2) and keeps
// its exported symbols for the segments that refer to it.
func (d *decoder) symbolDictionary(ctx context.Context, seg segment) error {
	dicts, syms, err := d.referredSymbols(seg)
	if err != nil {
		return err
	}
	var last *symbolDict
	if len(dicts) > 0 {
		last = dicts[len(dicts)-1]
	}
	dict, err := decodeSymbolDict(ctx, seg, syms, last, d.referredTables(seg), &d.budget)
	if err != nil {
		return err
	}
	if d.dicts == nil {
		d.dicts = make(map[uint32]*symbolDict)
	}
	d.dicts[seg.number] = dict
	return nil
}

// directRegion decodes a generic or text region segment. An intermediate one
// becomes an auxiliary buffer (T.88, 8.2 step 5 b); an immediate one is
// combined with the page (step 5 a).
func (d *decoder) directRegion(ctx context.Context, seg segment) error {
	if d.page == nil {
		return fmt.Errorf("%w: region before page information", ErrMalformed)
	}
	var bm *Bitmap
	var info regionInfo
	var err error
	switch seg.kind {
	case typeIntermediateText, typeImmediateText, typeImmediateLosslessText:
		bm, info, err = d.textRegion(ctx, seg)
	case typeIntermediateHalftone, typeImmediateHalftone, typeImmediateLosslessHalf:
		var dict *patternDict
		if dict, err = referredPatternDict(seg, d.patterns); err == nil {
			bm, info, err = decodeHalftone(ctx, seg, dict, &d.budget)
		}
	default:
		bm, info, err = d.genericRegion(ctx, seg)
	}
	if err != nil {
		return err
	}
	if seg.kind == typeIntermediateText || seg.kind == typeIntermediateGeneric || seg.kind == typeIntermediateHalftone {
		d.keepAux(seg.number, &auxRegion{bm: bm, info: info})
		return nil
	}
	return d.drawRegion(bm, info.x, info.y, info.combineOp)
}

// genericRegion decodes a generic region segment (T.88, 7.4.6).
func (d *decoder) genericRegion(ctx context.Context, seg segment) (*Bitmap, regionInfo, error) {
	info, err := parseRegionInfo(seg.data)
	if err != nil {
		return nil, regionInfo{}, err
	}
	if seg.rows >= 0 {
		// 7.4.6.4: a region of unknown length may hold fewer rows than its
		// header states, and its trailing row count says how many.
		if seg.rows > info.height {
			return nil, regionInfo{}, fmt.Errorf("%w: row count %d exceeds the region height %d", ErrMalformed, seg.rows, info.height)
		}
		info.height = seg.rows
	}
	if err := d.budget.charge(info.width, info.height); err != nil {
		return nil, regionInfo{}, err
	}
	bm, err := decodeGenericSegment(ctx, seg.data, int(info.width), int(info.height))
	return bm, info, err
}

// textRegion decodes a text region segment (T.88, 7.4.3.2), which starts from
// reset statistics (step 3).
func (d *decoder) textRegion(ctx context.Context, seg segment) (*Bitmap, regionInfo, error) {
	ts, err := parseTextSegment(seg.data)
	if err != nil {
		return nil, regionInfo{}, err
	}
	_, syms, err := d.referredSymbols(seg)
	if err != nil {
		return nil, regionInfo{}, err
	}
	if limit := d.budget.areaCap(); areaExceeds(ts.info.width, ts.info.height, limit) {
		return nil, regionInfo{}, fmt.Errorf("%w: text region of %dx%d pixels exceeds %d", ErrLimit, ts.info.width, ts.info.height, limit)
	}
	p := ts.params
	p.width, p.height = int(ts.info.width), int(ts.info.height)
	p.syms = syms
	gr := make([]mq.Context, refineContexts(p.refTemplate))
	if ts.huffman {
		src, err := newHuffText(ts, seg.data[ts.dataStart:], len(syms), d.referredTables(seg), gr, &d.budget)
		if err != nil {
			return nil, regionInfo{}, err
		}
		bm, err := decodeText(ctx, p, src)
		return bm, ts.info, err
	}
	st := &arithState{
		dec:    mq.NewDecoder(seg.data[ts.dataStart:]),
		iaid:   newIAID(symbolCodeLen(len(syms))),
		gr:     gr,
		budget: &d.budget,
	}
	bm, err := decodeText(ctx, p, st)
	return bm, ts.info, err
}

// refinementRegion decodes a generic refinement region segment (T.88, 7.4.7)
// and applies it as 8.2 steps 5 c to 5 e describe.
func (d *decoder) refinementRegion(ctx context.Context, seg segment) error {
	if d.page == nil {
		return fmt.Errorf("%w: region before page information", ErrMalformed)
	}
	info, err := parseRegionInfo(seg.data)
	if err != nil {
		return err
	}
	if limit := d.budget.areaCap(); areaExceeds(info.width, info.height, limit) {
		return fmt.Errorf("%w: refinement region of %dx%d pixels exceeds %d", ErrLimit, info.width, info.height, limit)
	}
	c := &cursor{b: seg.data, off: regionInfoSize}
	flags := c.u8()
	p := refineParams{
		width:    int(info.width),
		height:   int(info.height),
		template: int(flags & 1),
		tpgron:   flags&2 != 0,
	}
	if p.template == 0 {
		for i := range 2 {
			p.at[i][0] = c.s8()
			p.at[i][1] = c.s8()
		}
	}
	if c.err != nil {
		return c.err
	}

	// 7.4.7.4: the reference is the referred-to intermediate region's buffer,
	// or, with no referred-to segment, the page area the region covers.
	var ref *auxRegion
	switch len(seg.referred) {
	case 0:
		if seg.kind == typeIntermediateRefinement {
			return fmt.Errorf("%w: intermediate refinement region refers to no region", ErrMalformed)
		}
		if d.heightUnknown {
			if err := d.growPage(info.y + info.height); err != nil {
				return err
			}
		}
		p.reference = d.page.area(info.x, info.y, p.width, p.height)
	case 1:
		ref = d.aux[seg.referred[0]]
		if ref == nil {
			return fmt.Errorf("%w: refinement region refers to segment %d, which holds no intermediate region", ErrMalformed, seg.referred[0])
		}
		p.reference = ref.bm
	default:
		return fmt.Errorf("%w: refinement region refers to %d segments", ErrMalformed, len(seg.referred))
	}

	// 7.4.7.5 step 2: the statistics start reset. Table 35 aligns the
	// reference with the region, GRREFERENCEDX and GRREFERENCEDY 0.
	st := &arithState{
		dec:    mq.NewDecoder(seg.data[c.off:]),
		gr:     make([]mq.Context, refineContexts(p.template)),
		budget: &d.budget,
	}
	bm, err := decodeRefinement(ctx, p, st)
	if err != nil {
		return err
	}
	switch {
	case seg.kind == typeIntermediateRefinement:
		// Step 5 e: the refined buffer replaces the one it refines and moves to
		// this segment.
		delete(d.aux, seg.referred[0])
		d.keepAux(seg.number, &auxRegion{bm: bm, info: ref.info})
		return nil
	case ref != nil:
		// Step 5 d: draw the refined buffer where the buffer it refines was
		// to go, with this segment's operator, then discard it.
		delete(d.aux, seg.referred[0])
		return d.drawRegion(bm, ref.info.x, ref.info.y, info.combineOp)
	default:
		// Step 5 c: the refined area replaces that part of the page.
		return d.drawRegion(bm, info.x, info.y, combineReplace)
	}
}

// patternDictionary decodes a pattern dictionary segment (T.88, 7.4.4) and
// keeps its patterns for the halftone regions that refer to it.
func (d *decoder) patternDictionary(ctx context.Context, seg segment) error {
	dict, err := decodePatternDict(ctx, seg, &d.budget)
	if err != nil {
		return err
	}
	if d.patterns == nil {
		d.patterns = make(map[uint32]*patternDict)
	}
	d.patterns[seg.number] = dict
	return nil
}

// codeTable decodes a code table segment (T.88, 7.4.13 and B.2) and keeps its
// table for the segments that refer to it.
func (d *decoder) codeTable(seg segment) error {
	t, err := parseCodeTable(seg.data)
	if err != nil {
		return err
	}
	if d.tables == nil {
		d.tables = make(map[uint32]*huffTable)
	}
	d.tables[seg.number] = t
	return nil
}

// referredTables returns the tables of the code table segments seg refers to,
// in referral order, the order 7.4.2.1.6 and 7.4.3.1.6 assign them in.
func (d *decoder) referredTables(seg segment) []*huffTable {
	var tables []*huffTable
	for _, n := range seg.referred {
		if t, ok := d.tables[n]; ok {
			tables = append(tables, t)
		}
	}
	return tables
}

// keepAux associates an auxiliary buffer with segment n.
func (d *decoder) keepAux(n uint32, a *auxRegion) {
	if d.aux == nil {
		d.aux = make(map[uint32]*auxRegion)
	}
	d.aux[n] = a
}

// drawRegion combines a region bitmap with the page at (x, y), first growing
// a page of unknown height to hold it.
func (d *decoder) drawRegion(bm *Bitmap, x, y int64, op int) error {
	if d.heightUnknown {
		if err := d.growPage(y + int64(bm.Height)); err != nil {
			return err
		}
	}
	return d.page.combine(bm, x, y, op)
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
		return decodeMMR(ctx, width, height, data[c.off:])
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
