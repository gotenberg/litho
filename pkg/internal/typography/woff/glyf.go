package woff

import (
	"encoding/binary"
	"fmt"
)

// glyfResult is a rebuilt glyf table with its loca table.
type glyfResult struct {
	glyf, loca []byte
	// xMin is every glyph's bounding box left edge, 0 for an empty glyph:
	// a transformed hmtx table takes its side bearings from it.
	xMin        []int16
	indexFormat uint16
	numGlyphs   int
}

// Simple glyph flags (ISO/IEC 14496-22, glyf table, Simple Glyph Flags).
const (
	flagOnCurve      = 0x01
	flagXShort       = 0x02
	flagYShort       = 0x04
	flagRepeat       = 0x08
	flagXSame        = 0x10
	flagYSame        = 0x20
	flagOverlapSimpl = 0x40
)

// Composite glyph flags (ISO/IEC 14496-22, glyf table, Component Glyph
// Flags).
const (
	compArgsAreWords    = 0x0001
	compHaveScale       = 0x0008
	compMoreComponents  = 0x0020
	compHaveXYScale     = 0x0040
	compHaveTwoByTwo    = 0x0080
	compHaveInstruction = 0x0100
)

// glyfStreams are the seven streams of a transformed glyf table (WOFF 2.0,
// 5.1, Table 3) and its two bitmaps.
type glyfStreams struct {
	nContour, nPoints, flags, glyph, composite, bbox, instruction reader
	bboxBitmap, overlapBitmap                                     []byte
}

// bitSet reports bit i of bitmap, most significant bit first.
func bitSet(bitmap []byte, i int) bool {
	return bitmap != nil && bitmap[i>>3]&(0x80>>(i&7)) != 0
}

// reconstructGlyf rebuilds the glyf and loca tables of a transformed glyf
// table (WOFF 2.0, 5.1 and 5.3): each glyph's contour count, point counts,
// flags, coordinate triplets, instructions, composite records and explicit
// bounding box come from their own streams, and simple glyphs without an
// explicit box get the box of their points. Every glyph is padded to four
// bytes. The glyph count is the table's 16-bit numGlyphs, so at most 65535
// glyphs are rebuilt, each of at most [maxGlyphPoints] points.
func reconstructGlyf(data []byte) (*glyfResult, error) {
	r := &reader{b: data}
	r.u16() // reserved
	options := r.u16()
	numGlyphs := int(r.u16())
	indexFormat := r.u16()
	var sizes [7]uint32
	for i := range sizes {
		sizes[i] = r.u32()
	}
	if r.err != nil {
		return nil, r.err
	}
	if indexFormat > 1 {
		return nil, invalid("glyf index format %d", indexFormat)
	}
	var s glyfStreams
	streams := []*reader{&s.nContour, &s.nPoints, &s.flags, &s.glyph, &s.composite, &s.bbox, &s.instruction}
	for i, st := range streams {
		if int64(sizes[i]) > int64(len(data)) {
			return nil, invalid("glyf stream %d of %d bytes past the end of the table", i, sizes[i])
		}
		b := r.bytes(int(sizes[i])) // fits int: at most len(data), checked above
		if r.err != nil {
			return nil, invalid("glyf stream %d of %d bytes past the end of the table", i, sizes[i])
		}
		st.b = b
	}
	bitmapLen := 4 * ((numGlyphs + 31) / 32)
	s.bboxBitmap = s.bbox.bytes(bitmapLen)
	if s.bbox.err != nil {
		return nil, invalid("a bounding box bitmap of %d bytes past its stream", bitmapLen)
	}
	if options&1 != 0 {
		s.overlapBitmap = r.bytes(bitmapLen)
		if r.err != nil {
			return nil, invalid("an overlap bitmap of %d bytes past the end of the table", bitmapLen)
		}
	}

	res := &glyfResult{indexFormat: indexFormat, numGlyphs: numGlyphs, xMin: make([]int16, numGlyphs)}
	offsets := make([]uint32, numGlyphs+1)
	var glyf []byte
	for g := range numGlyphs {
		nContours := int16(s.nContour.u16()) //nolint:gosec // reinterprets the stream's int16 bits
		if s.nContour.err != nil {
			return nil, invalid("glyph %d: contour stream ends early", g)
		}
		var err error
		start := len(glyf)
		switch {
		case nContours == 0:
			if bitSet(s.bboxBitmap, g) {
				return nil, invalid("empty glyph %d with an explicit bounding box", g)
			}
		case nContours < 0:
			if nContours != -1 {
				return nil, invalid("glyph %d: contour count %d", g, nContours)
			}
			glyf, err = s.compositeGlyph(glyf, g)
		default:
			glyf, err = s.simpleGlyph(glyf, g, int(nContours))
		}
		if err != nil {
			return nil, err
		}
		if len(glyf) > start {
			res.xMin[g] = int16(binary.BigEndian.Uint16(glyf[start+2:])) //nolint:gosec // reinterprets the glyph's int16 xMin bits
		}
		for len(glyf)%4 != 0 {
			glyf = append(glyf, 0)
		}
		if int64(len(glyf)) > MaxSfntBytes {
			return nil, fmt.Errorf("%w: a glyf table of more than %d bytes", ErrTooLarge, MaxSfntBytes)
		}
		offsets[g+1] = uint32(len(glyf)) //nolint:gosec // at most MaxSfntBytes, checked above
	}
	res.glyf = glyf
	if indexFormat == 0 {
		if offsets[numGlyphs] > 2*0xFFFF {
			return nil, invalid("a glyf table of %d bytes for short loca offsets", offsets[numGlyphs])
		}
		res.loca = make([]byte, 2*(numGlyphs+1))
		for i, o := range offsets {
			binary.BigEndian.PutUint16(res.loca[2*i:], uint16(o/2)) //nolint:gosec // at most 0xFFFF, checked above
		}
	} else {
		res.loca = make([]byte, 4*(numGlyphs+1))
		for i, o := range offsets {
			binary.BigEndian.PutUint32(res.loca[4*i:], o)
		}
	}
	return res, nil
}

// compositeGlyph appends composite glyph g: its explicit bounding box, its
// component records copied from the composite stream, and its
// instructions when a component asks for them.
func (s *glyfStreams) compositeGlyph(out []byte, g int) ([]byte, error) {
	if !bitSet(s.bboxBitmap, g) {
		return nil, invalid("composite glyph %d without a bounding box", g)
	}
	box := s.bbox.bytes(8)
	if s.bbox.err != nil {
		return nil, invalid("glyph %d: bounding box stream ends early", g)
	}
	start := s.composite.pos
	instructions := false
	for {
		flags := s.composite.u16()
		s.composite.u16() // glyphIndex
		n := 2
		if flags&compArgsAreWords != 0 {
			n = 4
		}
		switch {
		case flags&compHaveScale != 0:
			n += 2
		case flags&compHaveXYScale != 0:
			n += 4
		case flags&compHaveTwoByTwo != 0:
			n += 8
		}
		s.composite.bytes(n)
		if s.composite.err != nil {
			return nil, invalid("glyph %d: composite stream ends early", g)
		}
		if flags&compHaveInstruction != 0 {
			instructions = true
		}
		if flags&compMoreComponents == 0 {
			break
		}
	}
	out = append(out, 0xFF, 0xFF)
	out = append(out, box...)
	out = append(out, s.composite.b[start:s.composite.pos]...)
	if instructions {
		var err error
		if out, err = s.instructions(out, g); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// instructions appends the instruction length the glyph stream gives and
// as many bytes of the instruction stream.
func (s *glyfStreams) instructions(out []byte, g int) ([]byte, error) {
	n := s.glyph.u255()
	code := s.instruction.bytes(int(n))
	if s.glyph.err != nil || s.instruction.err != nil {
		return nil, invalid("glyph %d: instructions past their stream", g)
	}
	out = binary.BigEndian.AppendUint16(out, n)
	return append(out, code...), nil
}

// point is one decoded point of a simple glyph.
type point struct {
	x, y    int
	onCurve bool
}

// withSign returns base, negated unless bit 0 of flag is set (WOFF 2.0,
// 5.2, Triplet Encoding).
func withSign(flag, base int) int {
	if flag&1 != 0 {
		return base
	}
	return -base
}

// triplet decodes one point delta from flag, the low seven bits of its
// flag byte, and the bytes it reads from the glyph stream (WOFF 2.0, 5.2,
// Table 4).
func (s *glyfStreams) triplet(flag int) (dx, dy int, ok bool) {
	var n int
	switch {
	case flag < 84:
		n = 1
	case flag < 120:
		n = 2
	case flag < 124:
		n = 3
	default:
		n = 4
	}
	b := s.glyph.bytes(n)
	if s.glyph.err != nil {
		return 0, 0, false
	}
	switch {
	case flag < 10:
		dy = withSign(flag, (flag&14)<<7+int(b[0]))
	case flag < 20:
		dx = withSign(flag, ((flag-10)&14)<<7+int(b[0]))
	case flag < 84:
		b0, b1 := flag-20, int(b[0])
		dx = withSign(flag, 1+(b0&0x30)+(b1>>4))
		dy = withSign(flag>>1, 1+((b0&0x0C)<<2)+(b1&0x0F))
	case flag < 120:
		b0 := flag - 84
		dx = withSign(flag, 1+((b0/12)<<8)+int(b[0]))
		dy = withSign(flag>>1, 1+(((b0%12)>>2)<<8)+int(b[1]))
	case flag < 124:
		b2 := int(b[1])
		dx = withSign(flag, int(b[0])<<4+b2>>4)
		dy = withSign(flag>>1, (b2&0x0F)<<8+int(b[2]))
	default:
		dx = withSign(flag, int(b[0])<<8+int(b[1]))
		dy = withSign(flag>>1, int(b[2])<<8+int(b[3]))
	}
	return dx, dy, true
}

// simpleGlyph appends simple glyph g of nContours contours in the glyf table's
// own encoding.
func (s *glyfStreams) simpleGlyph(out []byte, g, nContours int) ([]byte, error) {
	ends := make([]uint16, nContours)
	total := 0
	for c := range ends {
		n := int(s.nPoints.u255())
		if s.nPoints.err != nil {
			return nil, invalid("glyph %d: point count stream ends early", g)
		}
		total += n
		if total > maxGlyphPoints || total == 0 {
			return nil, invalid("glyph %d: %d points", g, total)
		}
		ends[c] = uint16(total - 1) //nolint:gosec // total is at most maxGlyphPoints, checked above
	}
	flags := s.flags.bytes(total)
	if s.flags.err != nil {
		return nil, invalid("glyph %d: flag stream ends early", g)
	}
	pts := make([]point, total)
	x, y := 0, 0
	for i, f := range flags {
		dx, dy, ok := s.triplet(int(f & 0x7F))
		if !ok {
			return nil, invalid("glyph %d: glyph stream ends early", g)
		}
		x, y = x+dx, y+dy
		pts[i] = point{x: x, y: y, onCurve: f&0x80 == 0}
	}
	var box [4]int
	if bitSet(s.bboxBitmap, g) {
		b := s.bbox.bytes(8)
		if s.bbox.err != nil {
			return nil, invalid("glyph %d: bounding box stream ends early", g)
		}
		for k := range box {
			box[k] = int(int16(binary.BigEndian.Uint16(b[2*k:]))) //nolint:gosec // reinterprets the stream's int16 bits
		}
	} else {
		box = [4]int{pts[0].x, pts[0].y, pts[0].x, pts[0].y}
		for _, p := range pts[1:] {
			box[0], box[1] = min(box[0], p.x), min(box[1], p.y)
			box[2], box[3] = max(box[2], p.x), max(box[3], p.y)
		}
	}
	out = binary.BigEndian.AppendUint16(out, uint16(nContours)) //nolint:gosec // a positive int16 read above
	for _, v := range box {
		out = binary.BigEndian.AppendUint16(out, uint16(int16(v))) //nolint:gosec // wraps to int16 as the format stores coordinates
	}
	for _, e := range ends {
		out = binary.BigEndian.AppendUint16(out, e)
	}
	var err error
	if out, err = s.instructions(out, g); err != nil {
		return nil, err
	}
	return encodePoints(out, pts, bitSet(s.overlapBitmap, g)), nil
}

// encodePoints appends the flags and coordinates of pts in the glyf
// encoding: one-byte deltas where they fit, repeated flags run-length
// coded, and OVERLAP_SIMPLE on the first flag when overlap is set.
func encodePoints(out []byte, pts []point, overlap bool) []byte {
	flags := make([]byte, len(pts))
	var xs, ys []byte
	px, py := 0, 0
	for i, p := range pts {
		var f byte
		if p.onCurve {
			f |= flagOnCurve
		}
		dx, dy := p.x-px, p.y-py
		px, py = p.x, p.y
		switch {
		case dx == 0:
			f |= flagXSame
		case dx > -256 && dx < 256:
			f |= flagXShort
			if dx > 0 {
				f |= flagXSame
				xs = append(xs, byte(dx))
			} else {
				xs = append(xs, byte(-dx))
			}
		default:
			xs = binary.BigEndian.AppendUint16(xs, uint16(int16(dx))) //nolint:gosec // wraps to int16 as the format stores deltas
		}
		switch {
		case dy == 0:
			f |= flagYSame
		case dy > -256 && dy < 256:
			f |= flagYShort
			if dy > 0 {
				f |= flagYSame
				ys = append(ys, byte(dy))
			} else {
				ys = append(ys, byte(-dy))
			}
		default:
			ys = binary.BigEndian.AppendUint16(ys, uint16(int16(dy))) //nolint:gosec // wraps to int16 as the format stores deltas
		}
		flags[i] = f
	}
	if overlap && len(flags) > 0 {
		flags[0] |= flagOverlapSimpl
	}
	for i := 0; i < len(flags); {
		run := 1
		for i+run < len(flags) && flags[i+run] == flags[i] && run < 256 {
			run++
		}
		if run > 1 {
			out = append(out, flags[i]|flagRepeat, byte(run-1))
		} else {
			out = append(out, flags[i])
		}
		i += run
	}
	out = append(out, xs...)
	return append(out, ys...)
}

// reconstructHmtx rebuilds a transformed hmtx table (WOFF 2.0, 5.4): the
// advances are stored, and the side bearings flags leaves out are the
// glyphs' xMin.
func reconstructHmtx(data []byte, numHMetrics int, xMin []int16) ([]byte, error) {
	r := &reader{b: data}
	flags := r.u8()
	if flags&0xFC != 0 || flags&0x03 == 0 {
		return nil, invalid("hmtx transform flags %#x", flags)
	}
	numGlyphs := len(xMin)
	if numHMetrics < 1 || numHMetrics > numGlyphs {
		return nil, invalid("%d horizontal metrics for %d glyphs", numHMetrics, numGlyphs)
	}
	adv := make([]uint16, numHMetrics)
	for i := range adv {
		adv[i] = r.u16()
	}
	lsb := make([]int16, numGlyphs)
	copy(lsb, xMin)
	if flags&1 == 0 {
		for i := range numHMetrics {
			lsb[i] = int16(r.u16()) //nolint:gosec // reinterprets the table's int16 bits
		}
	}
	if flags&2 == 0 {
		for i := numHMetrics; i < numGlyphs; i++ {
			lsb[i] = int16(r.u16()) //nolint:gosec // reinterprets the table's int16 bits
		}
	}
	if r.err != nil {
		return nil, invalid("hmtx: %v", r.err)
	}
	out := make([]byte, 0, 4*numHMetrics+2*(numGlyphs-numHMetrics))
	for i := range numGlyphs {
		if i < numHMetrics {
			out = binary.BigEndian.AppendUint16(out, adv[i])
		}
		out = binary.BigEndian.AppendUint16(out, uint16(lsb[i])) //nolint:gosec // writes the int16 bits back
	}
	return out, nil
}
