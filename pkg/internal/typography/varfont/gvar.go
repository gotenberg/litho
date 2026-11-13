package varfont

import (
	"encoding/binary"
	"fmt"
)

// gvar layout (OpenType specification, 'gvar' table, and OpenType Font
// Variations Common Table Formats, Tuple variation store).
const (
	gvarHeaderSize = 20
	// gvarLongOffsets is bit 0 of the header's flags: the glyph variation
	// data offsets are Offset32 rather than Offset16 halved.
	gvarLongOffsets = 1

	sharedPointNumbers  = 0x8000
	tupleCountMask      = 0x0FFF
	embeddedPeakTuple   = 0x8000
	intermediateRegion  = 0x4000
	privatePointNumbers = 0x2000
	tupleIndexMask      = 0x0FFF

	pointsAreWords    = 0x80
	pointRunCountMask = 0x7F

	deltasAreZero     = 0x80
	deltasAreWords    = 0x40
	deltaRunCountMask = 0x3F
)

// phantomPoints is the number of phantom points that follow a glyph's
// outline points in its variation data: left and right side bearing, top
// and bottom (OpenType specification, 'gvar' table, Point numbers and
// processing for composite glyphs).
const phantomPoints = 4

// gvarTable is a parsed 'gvar' header.
type gvarTable struct {
	axisCount  int
	shared     []byte
	sharedN    int
	glyphCount int
	long       bool
	offsets    []byte
	array      []byte
	// tx, ty and touched are scratch buffers of [gvarTable.glyphDeltas].
	tx, ty  []float64
	touched []bool
}

// parseGvar reads the header of a 'gvar' table for a font of axisCount
// axes and numGlyphs glyphs.
func parseGvar(data []byte, axisCount, numGlyphs int) (*gvarTable, error) {
	if len(data) < gvarHeaderSize || binary.BigEndian.Uint16(data) != 1 {
		return nil, fmt.Errorf("%w: gvar header", ErrInvalid)
	}
	g := &gvarTable{
		axisCount:  int(binary.BigEndian.Uint16(data[4:])),
		sharedN:    int(binary.BigEndian.Uint16(data[6:])),
		glyphCount: int(binary.BigEndian.Uint16(data[12:])),
		long:       binary.BigEndian.Uint16(data[14:])&gvarLongOffsets != 0,
	}
	if g.axisCount != axisCount {
		return nil, fmt.Errorf("%w: gvar has %d axes, fvar %d", ErrInvalid, g.axisCount, axisCount)
	}
	if g.glyphCount != numGlyphs {
		return nil, fmt.Errorf("%w: gvar has %d glyphs, maxp %d", ErrInvalid, g.glyphCount, numGlyphs)
	}
	sharedAt := uint64(binary.BigEndian.Uint32(data[8:]))
	arrayAt := uint64(binary.BigEndian.Uint32(data[16:]))
	width := 2
	if g.long {
		width = 4
	}
	offsetsEnd := gvarHeaderSize + (g.glyphCount+1)*width
	sharedEnd := sharedAt + uint64(g.sharedN*axisCount*2) //nolint:gosec // a product of uint16 counts and maxAxes, not negative
	if offsetsEnd > len(data) || sharedEnd > uint64(len(data)) || arrayAt > uint64(len(data)) {
		return nil, fmt.Errorf("%w: gvar arrays past the table", ErrInvalid)
	}
	g.offsets = data[gvarHeaderSize:offsetsEnd]
	g.shared = data[sharedAt:sharedEnd]
	g.array = data[arrayAt:]
	return g, nil
}

// scratch returns three buffers of n entries for one tuple's deltas and
// the points it names, reused from glyph to glyph.
func (g *gvarTable) scratch(n int) (tx, ty []float64, touched []bool) {
	if cap(g.tx) < n {
		g.tx, g.ty, g.touched = make([]float64, n), make([]float64, n), make([]bool, n)
	}
	return g.tx[:n], g.ty[:n], g.touched[:n]
}

// glyphData returns the GlyphVariationData of glyph gid, nil for a glyph
// without variations.
func (g *gvarTable) glyphData(gid int) ([]byte, error) {
	var start, end uint64
	if g.long {
		start = uint64(binary.BigEndian.Uint32(g.offsets[4*gid:]))
		end = uint64(binary.BigEndian.Uint32(g.offsets[4*gid+4:]))
	} else {
		start = 2 * uint64(binary.BigEndian.Uint16(g.offsets[2*gid:]))
		end = 2 * uint64(binary.BigEndian.Uint16(g.offsets[2*gid+2:]))
	}
	if start == end {
		return nil, nil
	}
	if start > end || end > uint64(len(g.array)) {
		return nil, fmt.Errorf("%w: variation data of glyph %d past the table", ErrInvalid, gid)
	}
	return g.array[start:end], nil
}

// glyphDeltas returns the net X and Y adjustments of the points of glyph
// gid at the normalized coordinates coords: the sum over its tuple
// variation tables of each delta times the table's scalar (OpenType Font
// Variations Overview, Algorithm for interpolation of instance values),
// unrounded. orig holds the default positions of the glyph's points, its
// phantom points last; for a simple glyph, ends holds the last point of
// each contour and a table that names some points of a contour infers the
// others ('gvar' table, Inferred deltas for un-referenced point numbers).
// A composite glyph (ends nil) infers nothing. work counts the deltas
// decoded, and the call fails with [ErrTooComplex] past [maxInstanceWork].
func (g *gvarTable) glyphDeltas(gid int, coords []int16, orig []point, ends []int, work *int) (dx, dy []float64, err error) {
	data, err := g.glyphData(gid)
	if err != nil || data == nil {
		return nil, nil, err
	}
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("%w: variation data of glyph %d", ErrInvalid, gid)
	}
	n := len(orig)
	head := binary.BigEndian.Uint16(data)
	count := int(head & tupleCountMask)
	serial := int(binary.BigEndian.Uint16(data[2:]))
	if serial > len(data) {
		return nil, nil, fmt.Errorf("%w: serialized data of glyph %d past its table", ErrInvalid, gid)
	}
	pos := serial
	var shared []int
	sharedAll := false
	if head&sharedPointNumbers != 0 {
		if shared, pos, err = unpackPoints(data, pos); err != nil {
			return nil, nil, err
		}
		sharedAll = shared == nil
	}
	dx, dy = make([]float64, n), make([]float64, n)
	hdr := 4
	tx, ty, touched := g.scratch(n)
	for range count {
		if hdr+4 > serial {
			return nil, nil, fmt.Errorf("%w: tuple headers of glyph %d past their data", ErrInvalid, gid)
		}
		size := int(binary.BigEndian.Uint16(data[hdr:]))
		index := binary.BigEndian.Uint16(data[hdr+2:])
		hdr += 4
		var peak []byte
		if index&embeddedPeakTuple != 0 {
			if hdr+2*g.axisCount > serial {
				return nil, nil, fmt.Errorf("%w: peak tuple of glyph %d past its header", ErrInvalid, gid)
			}
			peak = data[hdr : hdr+2*g.axisCount]
			hdr += 2 * g.axisCount
		} else {
			i := int(index & tupleIndexMask)
			if i >= g.sharedN {
				return nil, nil, fmt.Errorf("%w: shared tuple %d of %d", ErrInvalid, i, g.sharedN)
			}
			peak = g.shared[2*g.axisCount*i : 2*g.axisCount*(i+1)]
		}
		var startT, endT []byte
		if index&intermediateRegion != 0 {
			if hdr+4*g.axisCount > serial {
				return nil, nil, fmt.Errorf("%w: intermediate tuples of glyph %d past their header", ErrInvalid, gid)
			}
			startT = data[hdr : hdr+2*g.axisCount]
			endT = data[hdr+2*g.axisCount : hdr+4*g.axisCount]
			hdr += 4 * g.axisCount
		}
		chunkEnd := pos + size
		if chunkEnd > len(data) {
			return nil, nil, fmt.Errorf("%w: tuple data of glyph %d past its table", ErrInvalid, gid)
		}
		scalar := tupleScalar(coords, peak, startT, endT)
		chunk := pos
		pos = chunkEnd
		if scalar == 0 {
			continue
		}
		points, all := shared, sharedAll
		if index&privatePointNumbers != 0 {
			if points, chunk, err = unpackPoints(data[:chunkEnd], chunk); err != nil {
				return nil, nil, err
			}
			all = points == nil
		} else if head&sharedPointNumbers == 0 {
			all = true
		}
		m := len(points)
		if all {
			m = n
		}
		if *work += 2 * m; *work > maxInstanceWork {
			return nil, nil, ErrTooComplex
		}
		xs, chunk, err := unpackDeltas(data[:chunkEnd], chunk, m)
		if err != nil {
			return nil, nil, err
		}
		ys, _, err := unpackDeltas(data[:chunkEnd], chunk, m)
		if err != nil {
			return nil, nil, err
		}
		if all {
			for i := range n {
				dx[i] += float64(scalar * float64(xs[i]))
				dy[i] += float64(scalar * float64(ys[i]))
			}
			continue
		}
		clear(tx)
		clear(ty)
		clear(touched)
		for i, p := range points {
			if p >= n {
				continue
			}
			tx[p], ty[p], touched[p] = float64(xs[i]), float64(ys[i]), true
		}
		if ends != nil {
			inferDeltas(orig, ends, tx, ty, touched)
		}
		for i := range n {
			dx[i] += float64(scalar * tx[i])
			dy[i] += float64(scalar * ty[i])
		}
	}
	return dx, dy, nil
}

// tupleScalar returns the scalar of a tuple variation table at coords: the
// product of each axis's scalar over the region peak names, bounded by
// start and end when the table holds an intermediate region and by 0 and
// the peak otherwise (OpenType Font Variations Common Table Formats,
// TupleVariationHeader, and Overview, Algorithm for interpolation of
// instance values).
func tupleScalar(coords []int16, peak, start, end []byte) float64 {
	s := 1.0
	for a, v := range coords {
		p := f2dot14At(peak, 2*a)
		var lo, hi int32
		if start != nil {
			lo, hi = f2dot14At(start, 2*a), f2dot14At(end, 2*a)
		} else {
			lo, hi = min(p, 0), max(p, 0)
		}
		if s *= axisScalar(int32(v), lo, p, hi); s == 0 {
			return 0
		}
	}
	return s
}

// unpackPoints reads packed point numbers at data[pos:] (OpenType Font
// Variations Common Table Formats, Packed "point" numbers) and returns
// them with the position after them. A count of 0 means every point and
// returns nil. A point number may repeat; one past the glyph's points is
// kept for the caller to skip.
func unpackPoints(data []byte, pos int) ([]int, int, error) {
	if pos >= len(data) {
		return nil, 0, fmt.Errorf("%w: point numbers past their data", ErrInvalid)
	}
	count := int(data[pos])
	pos++
	if count == 0 {
		return nil, pos, nil
	}
	if count&0x80 != 0 {
		if pos >= len(data) {
			return nil, 0, fmt.Errorf("%w: point count past its data", ErrInvalid)
		}
		count = (count&0x7F)<<8 | int(data[pos])
		pos++
	}
	// A run element costs at least one byte, so a count past the data
	// left is a lie: refuse it before allocating.
	if count > len(data)-pos {
		return nil, 0, fmt.Errorf("%w: %d point numbers", ErrInvalid, count)
	}
	points := make([]int, 0, count)
	last := 0
	for len(points) < count {
		if pos >= len(data) {
			return nil, 0, fmt.Errorf("%w: point run past its data", ErrInvalid)
		}
		ctl := data[pos]
		pos++
		run := int(ctl&pointRunCountMask) + 1
		words := ctl&pointsAreWords != 0
		for range run {
			if len(points) == count {
				break
			}
			var d int
			if words {
				if pos+2 > len(data) {
					return nil, 0, fmt.Errorf("%w: point run past its data", ErrInvalid)
				}
				d = int(binary.BigEndian.Uint16(data[pos:]))
				pos += 2
			} else {
				if pos >= len(data) {
					return nil, 0, fmt.Errorf("%w: point run past its data", ErrInvalid)
				}
				d = int(data[pos])
				pos++
			}
			last += d
			points = append(points, last)
		}
	}
	return points, pos, nil
}

// unpackDeltas reads n packed deltas at data[pos:] (OpenType Font
// Variations Common Table Formats, Packed deltas) and returns them with
// the position after them.
func unpackDeltas(data []byte, pos, n int) ([]int16, int, error) {
	out := make([]int16, 0, n)
	for len(out) < n {
		if pos >= len(data) {
			return nil, 0, fmt.Errorf("%w: delta run past its data", ErrInvalid)
		}
		ctl := data[pos]
		pos++
		run := min(int(ctl&deltaRunCountMask)+1, n-len(out))
		switch {
		case ctl&deltasAreZero != 0:
			for range run {
				out = append(out, 0)
			}
		case ctl&deltasAreWords != 0:
			if pos+2*run > len(data) {
				return nil, 0, fmt.Errorf("%w: delta run past its data", ErrInvalid)
			}
			for i := range run {
				out = append(out, int16(binary.BigEndian.Uint16(data[pos+2*i:]))) //nolint:gosec // reinterprets a signed int16 delta
			}
			pos += 2 * run
		default:
			if pos+run > len(data) {
				return nil, 0, fmt.Errorf("%w: delta run past its data", ErrInvalid)
			}
			for i := range run {
				out = append(out, int16(int8(data[pos+i]))) //nolint:gosec // reinterprets a signed int8 delta
			}
			pos += run
		}
	}
	return out, pos, nil
}

// inferDeltas infers the deltas of the points of each contour that
// touched leaves out from the deltas of the nearest touched points before
// and after them in the contour ('gvar' table, Inferred deltas for
// un-referenced point numbers). A contour no point of which is touched
// keeps deltas of 0. Phantom points, past the last contour, are never
// inferred.
func inferDeltas(orig []point, ends []int, dx, dy []float64, touched []bool) {
	start := 0
	for _, end := range ends {
		if end < start || end >= len(orig) {
			return
		}
		first := -1
		for i := start; i <= end; i++ {
			if touched[i] {
				first = i
				break
			}
		}
		if first >= 0 {
			// Walk the touched points in contour order, filling the gap
			// after each up to the next, the last one wrapping to the
			// first.
			prev := first
			for i := first + 1; i <= end+(first-start)+1; i++ {
				j := i
				if j > end {
					j = start + (j - end - 1)
				}
				if !touched[j] {
					continue
				}
				fillGap(orig, start, end, prev, j, dx, dy)
				prev = j
				if j == first {
					break
				}
			}
		}
		start = end + 1
	}
}

// fillGap infers the deltas of the points strictly between the touched
// points a and b of the contour [start, end], walking forward from a and
// wrapping past end.
func fillGap(orig []point, start, end, a, b int, dx, dy []float64) {
	for i := next(a, start, end); i != b; i = next(i, start, end) {
		dx[i] = inferred(orig[i].x, orig[a].x, orig[b].x, dx[a], dx[b])
		dy[i] = inferred(orig[i].y, orig[a].y, orig[b].y, dy[a], dy[b])
	}
}

// next returns the point after i in the contour [start, end].
func next(i, start, end int) int {
	if i == end {
		return start
	}
	return i + 1
}

// inferred returns the delta of a point at coordinate t between touched
// points at c1 and c2 with deltas d1 and d2, in one direction ('gvar'
// table, Inferred deltas for un-referenced point numbers).
func inferred(t, c1, c2, d1, d2 float64) float64 {
	if c1 == c2 {
		if d1 == d2 {
			return d1
		}
		return 0
	}
	if c1 > c2 {
		c1, c2, d1, d2 = c2, c1, d2, d1
	}
	switch {
	case t <= c1:
		return d1
	case t >= c2:
		return d2
	}
	return d1 + (t-c1)*(d2-d1)/(c2-c1)
}
