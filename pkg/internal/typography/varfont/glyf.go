package varfont

import (
	"encoding/binary"
	"fmt"
	"math"
)

// glyf layout (OpenType specification, 'glyf' table): the glyph header,
// the simple glyph flags and the composite glyph flags.
const (
	glyphHeaderSize = 10

	flagOnCurve      = 0x01
	flagXShort       = 0x02
	flagYShort       = 0x04
	flagRepeat       = 0x08
	flagXSame        = 0x10
	flagYSame        = 0x20
	flagOverlapSimpl = 0x40

	argsAreWords        = 0x0001
	argsAreXY           = 0x0002
	haveScale           = 0x0008
	moreComponents      = 0x0020
	haveXYScale         = 0x0040
	haveTwoByTwo        = 0x0080
	haveInstructions    = 0x0100
	scaledOffset        = 0x0800
	unscaledOffset      = 0x1000
	componentFlagsKept  = 0xFFFF &^ (argsAreWords | haveInstructions)
	maxComponentsPerRef = 1 << 12
)

// maxGlyphPoints caps the points of one glyph, outline points and phantom
// points together: 65,540. Anti-DoS guard, not spec-mandated: a simple
// glyph's contour ends are uint16 point indices, so no valid glyph holds
// more than 65,536 points, and the four phantom points follow them.
const maxGlyphPoints = 1<<16 + phantomPoints

// maxComponentDepth caps how deeply composite glyphs nest when an
// instance's bounding boxes are computed: 16. Anti-DoS guard, not
// spec-mandated: 'maxp' records a font's depth, a handful in real fonts,
// and the bound stops a cycle.
const maxComponentDepth = 16

// point is a glyph point in font units.
type point struct {
	x, y float64
}

// simpleGlyph is a simple glyph of a 'glyf' table.
type simpleGlyph struct {
	ends    []int
	onCurve []bool
	overlap bool
	pts     []point
}

// component is one component of a composite glyph.
type component struct {
	flags uint16
	gid   uint16
	// dx and dy are the offset when the arguments are XY values; else
	// the parent and child point numbers.
	dx, dy int32
	// matrix holds the transform's bytes as the font stores them, and
	// m its value: xx, yx, xy, yy.
	matrix []byte
	m      [4]float64
}

// parseSimple reads a simple glyph of numberOfContours contours.
func parseSimple(g []byte, contours int) (*simpleGlyph, error) {
	at := glyphHeaderSize
	if at+2*contours+2 > len(g) {
		return nil, fmt.Errorf("%w: simple glyph header past its data", ErrInvalid)
	}
	s := &simpleGlyph{ends: make([]int, contours)}
	prev := -1
	for i := range s.ends {
		s.ends[i] = int(binary.BigEndian.Uint16(g[at+2*i:]))
		if s.ends[i] <= prev {
			return nil, fmt.Errorf("%w: contour ends out of order", ErrInvalid)
		}
		prev = s.ends[i]
	}
	at += 2 * contours
	n := prev + 1
	insLen := int(binary.BigEndian.Uint16(g[at:]))
	at += 2 + insLen
	if at > len(g) || n > len(g)-at {
		// Every point costs at least one flag byte.
		return nil, fmt.Errorf("%w: simple glyph of %d points past its data", ErrInvalid, n)
	}
	flags := make([]byte, n)
	for i := 0; i < n; {
		if at >= len(g) {
			return nil, fmt.Errorf("%w: glyph flags past their data", ErrInvalid)
		}
		f := g[at]
		at++
		flags[i] = f
		i++
		if f&flagRepeat != 0 {
			if at >= len(g) {
				return nil, fmt.Errorf("%w: glyph flags past their data", ErrInvalid)
			}
			r := int(g[at])
			at++
			for ; r > 0 && i < n; r-- {
				flags[i] = f
				i++
			}
		}
	}
	s.pts = make([]point, n)
	s.onCurve = make([]bool, n)
	if n > 0 {
		s.overlap = flags[0]&flagOverlapSimpl != 0
	}
	var err error
	if at, err = readCoords(g, at, flags, s.pts, true); err != nil {
		return nil, err
	}
	if _, err = readCoords(g, at, flags, s.pts, false); err != nil {
		return nil, err
	}
	for i, f := range flags {
		s.onCurve[i] = f&flagOnCurve != 0
	}
	return s, nil
}

// readCoords reads the X coordinates, or the Y ones, of a simple glyph's
// points at g[at:] and returns the position after them.
func readCoords(g []byte, at int, flags []byte, pts []point, x bool) (int, error) {
	short, same := byte(flagYShort), byte(flagYSame)
	if x {
		short, same = flagXShort, flagXSame
	}
	var v float64
	for i, f := range flags {
		switch {
		case f&short != 0:
			if at >= len(g) {
				return 0, fmt.Errorf("%w: glyph coordinates past their data", ErrInvalid)
			}
			d := float64(g[at])
			at++
			if f&same == 0 {
				d = -d
			}
			v += d
		case f&same == 0:
			if at+2 > len(g) {
				return 0, fmt.Errorf("%w: glyph coordinates past their data", ErrInvalid)
			}
			v += float64(int16(binary.BigEndian.Uint16(g[at:]))) //nolint:gosec // reinterprets a signed int16 coordinate delta
			at += 2
		}
		if x {
			pts[i].x = v
		} else {
			pts[i].y = v
		}
	}
	return at, nil
}

// parseComposite reads the components of a composite glyph.
func parseComposite(g []byte) ([]component, error) {
	at := glyphHeaderSize
	var comps []component
	for {
		if len(comps) >= maxComponentsPerRef {
			return nil, fmt.Errorf("%w: more than %d components", ErrInvalid, maxComponentsPerRef)
		}
		if at+4 > len(g) {
			return nil, fmt.Errorf("%w: component past its glyph", ErrInvalid)
		}
		c := component{flags: binary.BigEndian.Uint16(g[at:]), gid: binary.BigEndian.Uint16(g[at+2:]), m: [4]float64{1, 0, 0, 1}}
		at += 4
		if c.flags&argsAreWords != 0 {
			if at+4 > len(g) {
				return nil, fmt.Errorf("%w: component past its glyph", ErrInvalid)
			}
			a, b := binary.BigEndian.Uint16(g[at:]), binary.BigEndian.Uint16(g[at+2:])
			if c.flags&argsAreXY != 0 {
				c.dx, c.dy = int32(int16(a)), int32(int16(b)) //nolint:gosec // reinterprets signed int16 offsets
			} else {
				c.dx, c.dy = int32(a), int32(b)
			}
			at += 4
		} else {
			if at+2 > len(g) {
				return nil, fmt.Errorf("%w: component past its glyph", ErrInvalid)
			}
			if c.flags&argsAreXY != 0 {
				c.dx, c.dy = int32(int8(g[at])), int32(int8(g[at+1])) //nolint:gosec // reinterprets signed int8 offsets
			} else {
				c.dx, c.dy = int32(g[at]), int32(g[at+1])
			}
			at += 2
		}
		var size int
		switch {
		case c.flags&haveScale != 0:
			size = 2
		case c.flags&haveXYScale != 0:
			size = 4
		case c.flags&haveTwoByTwo != 0:
			size = 8
		}
		if at+size > len(g) {
			return nil, fmt.Errorf("%w: component transform past its glyph", ErrInvalid)
		}
		c.matrix = g[at : at+size]
		switch size {
		case 2:
			s := f2dot14Float(c.matrix, 0)
			c.m = [4]float64{s, 0, 0, s}
		case 4:
			c.m = [4]float64{f2dot14Float(c.matrix, 0), 0, 0, f2dot14Float(c.matrix, 2)}
		case 8:
			c.m = [4]float64{f2dot14Float(c.matrix, 0), f2dot14Float(c.matrix, 2), f2dot14Float(c.matrix, 4), f2dot14Float(c.matrix, 6)}
		}
		at += size
		comps = append(comps, c)
		if c.flags&moreComponents == 0 {
			return comps, nil
		}
	}
}

// f2dot14Float reads the F2DOT14 at b[at:] as a float.
func f2dot14Float(b []byte, at int) float64 { return float64(f2dot14At(b, at)) / 16384 }

// otRound rounds v half up, as the OpenType instancers round coordinates
// and metrics once every delta is summed.
func otRound(v float64) int32 {
	return int32(min(max(math.Floor(v+0.5), math.MinInt32), math.MaxInt32))
}

// clampInt16 clamps v to the int16 range a glyf coordinate or an FWORD
// holds.
func clampInt16(v int32) int16 {
	return int16(min(max(v, math.MinInt16), math.MaxInt16)) //nolint:gosec // clamped to the int16 range first
}

// encodeSimple writes a simple glyph whose points are xs and ys, without
// instructions (OpenType specification, 'glyf' table, Simple glyph
// description), with the bounding box it computes, and returns it with that
// box.
func encodeSimple(s *simpleGlyph, xs, ys []int16) ([]byte, [4]int16) {
	box := boundsOf(xs, ys)
	out := make([]byte, 0, glyphHeaderSize+2*len(s.ends)+2+5*len(xs))
	out = binary.BigEndian.AppendUint16(out, uint16(len(s.ends))) //nolint:gosec // the contour count came from an int16 field
	for _, v := range box {
		out = binary.BigEndian.AppendUint16(out, uint16(v)) //nolint:gosec // writes the 16 bits of a signed FWORD
	}
	for _, e := range s.ends {
		out = binary.BigEndian.AppendUint16(out, uint16(e)) //nolint:gosec // the contour end came from a uint16 field
	}
	out = binary.BigEndian.AppendUint16(out, 0)
	flags := make([]byte, len(xs))
	var px, py int16
	var xb, yb []byte
	for i := range xs {
		var f byte
		if s.onCurve[i] {
			f = flagOnCurve
		}
		if i == 0 && s.overlap {
			f |= flagOverlapSimpl
		}
		f, xb = appendCoord(f, xb, int32(xs[i])-int32(px), flagXShort, flagXSame)
		f, yb = appendCoord(f, yb, int32(ys[i])-int32(py), flagYShort, flagYSame)
		flags[i] = f
		px, py = xs[i], ys[i]
	}
	for i := 0; i < len(flags); {
		r := 0
		for i+r+1 < len(flags) && flags[i+r+1] == flags[i] && r < 255 {
			r++
		}
		if r > 1 {
			out = append(out, flags[i]|flagRepeat, byte(r))
		} else {
			out = append(out, flags[i])
			r = 0
		}
		i += r + 1
	}
	out = append(out, xb...)
	out = append(out, yb...)
	return out, box
}

// appendCoord encodes the coordinate delta d in the flag f and the bytes b
// with the short and same flag bits of its direction.
func appendCoord(f byte, b []byte, d int32, short, same byte) (byte, []byte) {
	switch {
	case d == 0:
		return f | same, b
	case d > -256 && d < 256:
		if d > 0 {
			return f | short | same, append(b, byte(d))
		}
		return f | short, append(b, byte(-d))
	}
	return f, binary.BigEndian.AppendUint16(b, uint16(int16(d))) //nolint:gosec // both coordinates are int16, so their difference fits after wrapping as TrueType reads it
}

// boundsOf returns the box of the points: xMin, yMin, xMax, yMax, all 0
// for no point.
func boundsOf(xs, ys []int16) [4]int16 {
	if len(xs) == 0 {
		return [4]int16{}
	}
	b := [4]int16{xs[0], ys[0], xs[0], ys[0]}
	for i := range xs {
		b[0], b[1] = min(b[0], xs[i]), min(b[1], ys[i])
		b[2], b[3] = max(b[2], xs[i]), max(b[3], ys[i])
	}
	return b
}

// encodeComposite writes a composite glyph with the components comps,
// their offsets dx and dy, without instructions, under the bounding box
// bounds
// (OpenType specification, 'glyf' table, Composite glyph description).
func encodeComposite(comps []component, dx, dy []int32, bounds [4]int16) []byte {
	out := make([]byte, 0, glyphHeaderSize+16*len(comps))
	out = binary.BigEndian.AppendUint16(out, 0xFFFF)
	for _, v := range bounds {
		out = binary.BigEndian.AppendUint16(out, uint16(v)) //nolint:gosec // writes the 16 bits of a signed FWORD
	}
	for i, c := range comps {
		flags := c.flags & componentFlagsKept
		if i < len(comps)-1 {
			flags |= moreComponents
		} else {
			flags &^= moreComponents
		}
		a, b := dx[i], dy[i]
		var words bool
		if c.flags&argsAreXY != 0 {
			words = a < math.MinInt8 || a > math.MaxInt8 || b < math.MinInt8 || b > math.MaxInt8
		} else {
			words = a > math.MaxUint8 || b > math.MaxUint8
		}
		if words {
			flags |= argsAreWords
		}
		out = binary.BigEndian.AppendUint16(out, flags)
		out = binary.BigEndian.AppendUint16(out, c.gid)
		switch {
		case words && c.flags&argsAreXY != 0:
			out = binary.BigEndian.AppendUint16(out, uint16(clampInt16(a))) //nolint:gosec // writes the 16 bits of a signed offset
			out = binary.BigEndian.AppendUint16(out, uint16(clampInt16(b))) //nolint:gosec // as above
		case words:
			// Point numbers came from uint16 fields and are written back
			// unchanged.
			out = binary.BigEndian.AppendUint16(out, uint16(a)) //nolint:gosec // a uint16 point number
			out = binary.BigEndian.AppendUint16(out, uint16(b)) //nolint:gosec // as above
		default:
			out = append(out, byte(a), byte(b)) //nolint:gosec // words is false: both fit a byte, signed or not
		}
		out = append(out, c.matrix...)
	}
	return out
}
