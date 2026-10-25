package truetype

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// Simple glyph flags (OpenType specification, glyf table, simple glyph
// description). Bit 6, OVERLAP_SIMPLE, matters to a rasteriser only through
// the nonzero fill rule every glyph is filled with, and bit 7 is reserved.
const (
	simpleOnCurve      = 0x01
	simpleXShort       = 0x02
	simpleYShort       = 0x04
	simpleRepeat       = 0x08
	simpleXSameOrPlus  = 0x10
	simpleYSameOrPlus  = 0x20
	compositeArgsAreXY = 0x0002
	// compositeScaledOffset and compositeUnscaledOffset state whether a
	// component's offset goes through its transform (Apple) or not
	// (Microsoft, and the default when neither is set).
	compositeScaledOffset   = 0x0800
	compositeUnscaledOffset = 0x1000
)

// glyphHeaderBytes is the size of a glyph description's header:
// numberOfContours and the bounding box (OpenType specification, glyf table).
const glyphHeaderBytes = 10

// f2dot14Scale is the denominator of an F2DOT14 fixed-point number, the
// format of a component's scale and transform (OpenType specification, data
// types).
const f2dot14Scale = 1 << 14

// maxComponentDepth bounds the nesting of composite glyphs. Anti-DoS guard,
// not spec-mandated: the maxp table states a font's own maxComponentDepth, and
// real fonts nest a few levels, so 16 leaves room for every one while a
// crafted chain cannot run the walk deep.
const maxComponentDepth = 16

// maxGlyphComponents bounds the components one glyph's walk reads, at every
// level. Anti-DoS guard, not spec-mandated: a depth bound alone lets each
// level multiply the components of the next, so a crafted font fans out
// exponentially; maxp states maxComponentElements as a uint16 per level, and
// no real glyph comes near this total.
const maxGlyphComponents = 1 << 12

// maxGlyphPoints bounds the points of one glyph, components included. A
// conforming font cannot exceed it: maxp states maxCompositePoints as a
// uint16, and a point-matching component indexes points with a uint16.
const maxGlyphPoints = 1 << 16

// Font is a TrueType program read for its glyph outlines: the glyf and loca
// tables, the horizontal metrics and the units per em. It reads the
// quadratic outlines of the glyf table only; the outlines of an OpenType
// program with CFF data come from that data ([CFFTable]).
//
// A Font is immutable once [ParseFont] returns it and is safe for concurrent
// use.
type Font struct {
	glyf        []byte
	loca        []byte
	longLoca    bool
	numGlyphs   int
	hmtx        []byte
	numHMetrics int
	unitsPerEm  int
}

// ParseFont reads the head, maxp, loca and glyf tables of a TrueType program,
// and its hhea and hmtx tables when it carries them (OpenType specification).
// It fails on data that is not an sfnt with glyf outlines and on a program
// missing one of the four tables or whose head or maxp is cut short.
func ParseFont(data []byte) (*Font, error) {
	if !IsSfnt(data) || string(data[:4]) == "OTTO" {
		return nil, fmt.Errorf("%w: not a TrueType program with glyf outlines", ErrInvalidFont)
	}
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}
	table := func(tag string) ([]byte, error) {
		t, err := TableData(data, records, tag)
		if err != nil {
			return nil, fmt.Errorf("reading the %s table: %w", tag, err)
		}
		return t, nil
	}
	head, err := table("head")
	if err != nil {
		return nil, err
	}
	if len(head) < 54 { //nolint:mnd // indexToLocFormat ends the 54-byte head table.
		return nil, fmt.Errorf("%w: head table too short", ErrInvalidFont)
	}
	maxp, err := table("maxp")
	if err != nil {
		return nil, err
	}
	if len(maxp) < 6 { //nolint:mnd // maxp numGlyphs is at offset 4..6.
		return nil, fmt.Errorf("%w: maxp table too short", ErrInvalidFont)
	}
	f := &Font{
		longLoca:   binary.BigEndian.Uint16(head[50:52]) != 0,
		numGlyphs:  int(binary.BigEndian.Uint16(maxp[4:6])),
		unitsPerEm: int(binary.BigEndian.Uint16(head[18:20])),
	}
	if f.unitsPerEm == 0 {
		// The head table states 16 to 16384 units per em (OpenType
		// specification, head table). A program stating 0 would map every
		// glyph to infinity; it is read at the common 1000, as PDF.js does.
		f.unitsPerEm = 1000
	}
	if f.loca, err = table("loca"); err != nil {
		return nil, err
	}
	if f.glyf, err = table("glyf"); err != nil {
		return nil, err
	}
	// loca holds numGlyphs+1 offsets: a shorter one holds fewer glyphs.
	entry := 2
	if f.longLoca {
		entry = 4
	}
	f.numGlyphs = max(min(f.numGlyphs, len(f.loca)/entry-1), 0)
	if hhea, err := TableData(data, records, "hhea"); err == nil && len(hhea) >= 36 {
		if hmtx, err := TableData(data, records, "hmtx"); err == nil {
			f.hmtx = hmtx
			f.numHMetrics = min(int(binary.BigEndian.Uint16(hhea[34:36])), len(hmtx)/4)
		}
	}
	return f, nil
}

// CFFTable returns the CFF data of an OpenType program whose outlines are
// CFF, its "CFF " table (OpenType specification, CFF table), for the cff
// package to read. It fails on a program that carries no such table.
func CFFTable(data []byte) ([]byte, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}
	return TableData(data, records, "CFF ")
}

// CFF2Table returns the 'CFF2' table of an OpenType program whose outlines
// are CFF2 (OpenType specification, CFF2 table), for cff.ParseCFF2 to read. It
// fails on a program that carries no such table.
func CFF2Table(data []byte) ([]byte, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}
	return TableData(data, records, "CFF2")
}

// NumGlyphs returns the number of glyphs the program holds: the maxp count,
// lowered to the offsets loca holds.
func (f *Font) NumGlyphs() int {
	return f.numGlyphs
}

// UnitsPerEm returns the head table's units per em.
func (f *Font) UnitsPerEm() int {
	return f.unitsPerEm
}

// FontMatrix returns the transform from glyph space, in font units, to text
// space, where one unit is the em: a scale of 1/unitsPerEm (ISO 32000-1,
// 9.2.4, gives every font but Type 3 this unit em).
func (f *Font) FontMatrix() geom.Matrix {
	s := 1 / float64(f.unitsPerEm)
	return geom.Matrix{s, 0, 0, s, 0, 0}
}

// Advance returns the advance width of glyph gid in font units from the hmtx
// table: a glyph past numberOfHMetrics takes the last long metric's advance
// (OpenType specification, hmtx table). A program without horizontal metrics
// gives 0.
func (f *Font) Advance(gid int) float64 {
	if f.numHMetrics == 0 || gid < 0 {
		return 0
	}
	i := min(gid, f.numHMetrics-1)
	return float64(binary.BigEndian.Uint16(f.hmtx[4*i:]))
}

// ttPoint is a point of a glyph's outline as the glyf table states it, on or
// off the curve.
type ttPoint struct {
	geom.Point
	on bool
}

// glyphPoints is a glyph's outline as points: pts, and the index of each
// contour's last point.
type glyphPoints struct {
	pts  []ttPoint
	ends []int
}

// GlyphOutline returns the outline of glyph gid in font units, each contour a
// closed subpath whose quadratic segments are written as the cubics that draw
// them exactly. A composite glyph draws its components through their
// transforms, offsets and matched points; ROUND_XY_TO_GRID is a hinting
// instruction and an unhinted outline ignores it. An empty glyph gives an
// empty path.
//
// It fails on a glyph ID the program does not hold, a component's included,
// on a glyph description cut short, on composite glyphs that nest past
// [maxComponentDepth], that refer back to a glyph they are part of, or that
// read more than [maxGlyphComponents] components or [maxGlyphPoints] points,
// and on a point-matching component naming a point that does not exist.
//
// A negative numberOfContours other than -1 still marks a composite glyph:
// the glyf table makes any negative count composite and only recommends -1.
//
// # Corpus evidence
//
//   - All corpora: over the first 400 glyphs of each embedded TrueType
//     program fontTools 4.60 reads, every outline matches its points and the
//     path drawn from them.
//   - pdfjs: PDFJS-7562-reduced.pdf holds a glyph with numberOfContours -70,
//     which fontTools reads as simple and fails on, and this reader as the
//     composite the table makes it.
//   - All corpora: of 3973 distinct embedded TrueType programs, 756 hold
//     composite glyphs, 128119 in all, nested at most 4 deep; one glyph
//     contains itself, in pdfjs recursiveCompositGlyf.pdf.
//   - All corpora: over every glyph of the 3956 programs both read, this
//     reader and FreeType 2.13.2 agree on which glyphs fail and which are
//     empty but for 2 glyphs of pdfjs issue15813.pdf, failing here and empty
//     to FreeType, which draws nothing for either.
//   - pdfjs: issue14618.pdf holds 14 composites naming a component past the
//     glyphs its loca holds; FreeType refuses each whole, as this reader
//     does, and none is drawn on the page.
func (f *Font) GlyphOutline(ctx context.Context, gid int) (geom.Path, error) {
	w := &glyphWalk{font: f, components: maxGlyphComponents}
	g, err := w.points(ctx, gid, 0)
	if err != nil {
		return geom.Path{}, fmt.Errorf("reading the outline of glyph %d: %w", gid, err)
	}
	return g.path(), nil
}

// glyphData returns the glyph description of gid, empty for a glyph without
// an outline.
//
// The loca table gives a glyph's data as the bytes from its offset to the
// next glyph's, so its offsets ascend and stay within the glyf table
// (OpenType specification, loca table). Broken entries are read as FreeType
// reads them (tt_face_get_location), the reading poppler, PDFium and MuPDF
// draw: a glyph starting past the glyf table, or ending past it when it is not
// the last glyph, is empty; the last glyph ending past the table ends with
// it; a glyph whose next offset is lower runs to the end of the table, and
// its description ends where its own data says. Refusing such a glyph would
// drop a glyph all three viewers draw, a whole composite with it.
//
// # Corpus evidence
//
//   - pdfjs: bug1650302_reduced.pdf (a subset whose unused loca entries are
//     0, so used glyphs end lower than they start), issue4398.pdf (the last
//     glyph ends 2 bytes past the glyf table) and five other files hold 109
//     glyphs FreeType draws and a refusal dropped, among them the "ř" and "í"
//     of bug1650302_reduced and the "8" of issue4398, which poppler, PDFium
//     and MuPDF all draw.
//   - pdfjs: issue15813.pdf holds 19880 glyphs whose entries point past the
//     glyf table, empty to FreeType.
func (f *Font) glyphData(gid int) ([]byte, error) {
	if gid < 0 || gid >= f.numGlyphs {
		return nil, fmt.Errorf("%w: glyph %d of %d", ErrInvalidFont, gid, f.numGlyphs)
	}
	var start, end, entries int
	if f.longLoca {
		start = int(binary.BigEndian.Uint32(f.loca[4*gid:]))
		end = int(binary.BigEndian.Uint32(f.loca[4*gid+4:]))
		entries = len(f.loca) / 4
	} else {
		start = 2 * int(binary.BigEndian.Uint16(f.loca[2*gid:]))
		end = 2 * int(binary.BigEndian.Uint16(f.loca[2*gid+2:]))
		entries = len(f.loca) / 2
	}
	switch {
	case start > len(f.glyf):
		return nil, nil
	case end > len(f.glyf):
		if gid != entries-2 {
			return nil, nil
		}
		end = len(f.glyf)
	case end < start:
		end = len(f.glyf)
	}
	return f.glyf[start:end], nil
}

// glyphWalk reads one glyph's points, its components and theirs, within the
// budgets that bound the walk.
type glyphWalk struct {
	font       *Font
	components int
	// stack holds the glyphs whose components are being read, to detect a
	// component that refers back to one of them.
	stack []int
}

// errComponentCycle reports a composite glyph that contains itself.
var errComponentCycle = errors.New("a composite glyph refers back to itself")

// points returns the outline points of glyph gid. depth counts the composite
// glyphs gid is a component of.
func (w *glyphWalk) points(ctx context.Context, gid, depth int) (glyphPoints, error) {
	g, err := w.font.glyphData(gid)
	if err != nil || len(g) == 0 {
		return glyphPoints{}, err
	}
	if len(g) < glyphHeaderBytes {
		return glyphPoints{}, fmt.Errorf("%w: glyph header cut short", ErrInvalidFont)
	}
	contours := int(int16(binary.BigEndian.Uint16(g))) //nolint:gosec // G115: numberOfContours is an int16.
	if contours >= 0 {
		return simplePoints(g, contours)
	}
	if depth >= maxComponentDepth {
		return glyphPoints{}, fmt.Errorf("%w: composite glyphs nested past %d", ErrInvalidFont, maxComponentDepth)
	}
	// The glyf table builds a composite from other glyphs' descriptions
	// (OpenType specification, glyf table, composite glyph description): one
	// that contains itself describes no finite outline, so the whole glyph is
	// refused, as FreeType refuses it (Invalid_Composite) for MuPDF and PDFium,
	// rather than drawn from the components read before the cycle, as PDF.js
	// draws it (font_renderer.js, compileGlyf). The cycle check, with
	// [maxComponentDepth] and [maxGlyphComponents], keeps the walk bounded.
	for _, ancestor := range w.stack {
		if ancestor == gid {
			return glyphPoints{}, fmt.Errorf("%w: %w (glyph %d)", ErrInvalidFont, errComponentCycle, gid)
		}
	}
	w.stack = append(w.stack, gid)
	defer func() { w.stack = w.stack[:len(w.stack)-1] }()
	return w.compositePoints(ctx, g[glyphHeaderBytes:], depth)
}

// simplePoints decodes a simple glyph description (OpenType specification,
// glyf table): the contour end points, the instructions it skips, the flags
// and the delta-encoded coordinates.
func simplePoints(g []byte, contours int) (glyphPoints, error) {
	pos := glyphHeaderBytes
	if contours == 0 {
		return glyphPoints{}, nil
	}
	if len(g) < pos+2*contours+2 {
		return glyphPoints{}, fmt.Errorf("%w: contour end points cut short", ErrInvalidFont)
	}
	ends := make([]int, contours)
	for i := range ends {
		ends[i] = int(binary.BigEndian.Uint16(g[pos:]))
		pos += 2
		if i > 0 && ends[i] <= ends[i-1] {
			return glyphPoints{}, fmt.Errorf("%w: contour end points out of order", ErrInvalidFont)
		}
	}
	n := ends[contours-1] + 1
	pos += 2 + int(binary.BigEndian.Uint16(g[pos:]))
	if pos > len(g) {
		return glyphPoints{}, fmt.Errorf("%w: instructions cut short", ErrInvalidFont)
	}
	// n is a uint16 plus one, so the point array stays below 65537 entries.
	flags := make([]byte, 0, n)
	for len(flags) < n {
		if pos >= len(g) {
			return glyphPoints{}, fmt.Errorf("%w: flags cut short", ErrInvalidFont)
		}
		flag := g[pos]
		pos++
		flags = append(flags, flag)
		if flag&simpleRepeat != 0 {
			if pos >= len(g) {
				return glyphPoints{}, fmt.Errorf("%w: flags cut short", ErrInvalidFont)
			}
			for range min(int(g[pos]), n-len(flags)) {
				flags = append(flags, flag)
			}
			pos++
		}
	}
	pts := make([]ttPoint, n)
	var err error
	if pos, err = decodeCoordinates(g, pos, flags, pts, simpleXShort, simpleXSameOrPlus, func(p *ttPoint, v float64) { p.X = v }); err != nil {
		return glyphPoints{}, err
	}
	if _, err = decodeCoordinates(g, pos, flags, pts, simpleYShort, simpleYSameOrPlus, func(p *ttPoint, v float64) { p.Y = v }); err != nil {
		return glyphPoints{}, err
	}
	for i, flag := range flags {
		pts[i].on = flag&simpleOnCurve != 0
	}
	return glyphPoints{pts: pts, ends: ends}, nil
}

// decodeCoordinates reads one axis of a simple glyph's coordinates, each a
// delta from the point before: a byte whose sign the same-or-positive flag
// gives when the short flag is set, else a repeat of the previous coordinate
// when the same-or-positive flag is set, else an int16. It returns the
// position after the axis.
func decodeCoordinates(g []byte, pos int, flags []byte, pts []ttPoint, short, sameOrPlus byte, set func(*ttPoint, float64)) (int, error) {
	v := 0
	for i, flag := range flags {
		switch {
		case flag&short != 0:
			if pos >= len(g) {
				return 0, fmt.Errorf("%w: coordinates cut short", ErrInvalidFont)
			}
			d := int(g[pos])
			pos++
			if flag&sameOrPlus == 0 {
				d = -d
			}
			v += d
		case flag&sameOrPlus == 0:
			if pos+2 > len(g) {
				return 0, fmt.Errorf("%w: coordinates cut short", ErrInvalidFont)
			}
			v += int(int16(binary.BigEndian.Uint16(g[pos:]))) //nolint:gosec // G115: a coordinate delta is an int16.
			pos += 2
		}
		set(&pts[i], float64(v))
	}
	return pos, nil
}

// compositePoints reads the components of a composite glyph description (the
// bytes after its header) and returns their points, each component placed by
// its transform and offset or its matched points (OpenType specification,
// glyf table, composite glyph description).
func (w *glyphWalk) compositePoints(ctx context.Context, g []byte, depth int) (glyphPoints, error) {
	var out glyphPoints
	pos := 0
	for {
		if err := ctx.Err(); err != nil {
			return glyphPoints{}, err
		}
		if w.components--; w.components < 0 {
			return glyphPoints{}, fmt.Errorf("%w: more than %d components", ErrInvalidFont, maxGlyphComponents)
		}
		c, next, err := readComponent(g, pos)
		if err != nil {
			return glyphPoints{}, err
		}
		pos = next
		// A component names a glyph of the same font (OpenType specification,
		// glyf table, composite glyph description), so one past numGlyphs
		// leaves the composite undefined: the whole glyph is refused, as
		// FreeType refuses it (Invalid_Composite) for poppler, PDFium and
		// MuPDF, rather than drawn without that component, as PDF.js draws it.
		child, err := w.points(ctx, c.gid, depth+1)
		if err != nil {
			return glyphPoints{}, fmt.Errorf("component glyph %d: %w", c.gid, err)
		}
		if len(out.pts)+len(child.pts) > maxGlyphPoints {
			return glyphPoints{}, fmt.Errorf("%w: more than %d points", ErrInvalidFont, maxGlyphPoints)
		}
		if err := c.place(child.pts, out.pts); err != nil {
			return glyphPoints{}, err
		}
		base := len(out.pts)
		out.pts = append(out.pts, child.pts...)
		for _, e := range child.ends {
			out.ends = append(out.ends, base+e)
		}
		if c.flags&compositeMoreComponent == 0 {
			return out, nil
		}
	}
}

// component is one component record of a composite glyph.
type component struct {
	flags uint16
	gid   int
	// arg1 and arg2 are the x and y offsets when the flags say so, else the
	// parent's and the child's matched point numbers.
	arg1, arg2 int
	// m is the component's linear transform, with no translation.
	m geom.Matrix
}

// readComponent reads the component record at pos and returns the position
// after it.
func readComponent(g []byte, pos int) (component, int, error) {
	if pos+4 > len(g) {
		return component{}, 0, fmt.Errorf("%w: composite glyph cut short", ErrInvalidFont)
	}
	c := component{
		flags: binary.BigEndian.Uint16(g[pos:]),
		gid:   int(binary.BigEndian.Uint16(g[pos+2:])),
		m:     geom.Identity,
	}
	pos += 4
	xy := c.flags&compositeArgsAreXY != 0
	if c.flags&compositeArgsAreWords != 0 {
		if pos+4 > len(g) {
			return component{}, 0, fmt.Errorf("%w: composite glyph cut short", ErrInvalidFont)
		}
		a1, a2 := binary.BigEndian.Uint16(g[pos:]), binary.BigEndian.Uint16(g[pos+2:])
		if xy {
			c.arg1, c.arg2 = int(int16(a1)), int(int16(a2)) //nolint:gosec // G115: offsets are int16.
		} else {
			c.arg1, c.arg2 = int(a1), int(a2)
		}
		pos += 4
	} else {
		if pos+2 > len(g) {
			return component{}, 0, fmt.Errorf("%w: composite glyph cut short", ErrInvalidFont)
		}
		if xy {
			c.arg1, c.arg2 = int(int8(g[pos])), int(int8(g[pos+1])) //nolint:gosec // G115: offsets are int8.
		} else {
			c.arg1, c.arg2 = int(g[pos]), int(g[pos+1])
		}
		pos += 2
	}
	f2dot14 := func(at int) float64 {
		return float64(int16(binary.BigEndian.Uint16(g[at:]))) / f2dot14Scale //nolint:gosec // G115: an F2DOT14 is an int16.
	}
	var n int
	switch {
	case c.flags&compositeHaveScale != 0:
		n = 2
	case c.flags&compositeHaveXYScale != 0:
		n = 4
	case c.flags&compositeHaveTwoByTwo != 0:
		n = 8
	}
	if pos+n > len(g) {
		return component{}, 0, fmt.Errorf("%w: composite glyph cut short", ErrInvalidFont)
	}
	switch n {
	case 2:
		s := f2dot14(pos)
		c.m[0], c.m[3] = s, s
	case 4:
		c.m[0], c.m[3] = f2dot14(pos), f2dot14(pos+2)
	case 8:
		// xscale, scale01, scale10, yscale: x' = xscale·x + scale10·y and
		// y' = scale01·x + yscale·y, the order of a PDF matrix's a, b, c, d.
		c.m[0], c.m[1], c.m[2], c.m[3] = f2dot14(pos), f2dot14(pos+2), f2dot14(pos+4), f2dot14(pos+6)
	}
	return c, pos + n, nil
}

// place maps a component's points into the composite: through its transform,
// then by its offset, or by the offset that brings the child's matched point
// onto the parent's, which the transform does not move. With
// SCALED_COMPONENT_OFFSET (and not UNSCALED_COMPONENT_OFFSET) the offset goes
// through the transform too, the Apple reading; otherwise it is added as is,
// the OpenType default.
func (c component) place(child, parent []ttPoint) error {
	for i := range child {
		child[i].Point = c.m.Apply(child[i].Point)
	}
	var off geom.Point
	switch {
	case c.flags&compositeArgsAreXY == 0:
		if c.arg1 >= len(parent) || c.arg2 >= len(child) {
			return fmt.Errorf("%w: component matches point %d of %d to point %d of %d", ErrInvalidFont, c.arg2, len(child), c.arg1, len(parent))
		}
		off = geom.Point{X: parent[c.arg1].X - child[c.arg2].X, Y: parent[c.arg1].Y - child[c.arg2].Y}
	case c.flags&compositeScaledOffset != 0 && c.flags&compositeUnscaledOffset == 0:
		off = c.m.ApplyVector(geom.Point{X: float64(c.arg1), Y: float64(c.arg2)})
	default:
		off = geom.Point{X: float64(c.arg1), Y: float64(c.arg2)}
	}
	for i := range child {
		child[i].X += off.X
		child[i].Y += off.Y
	}
	return nil
}

// path writes the points as closed subpaths. Two consecutive off-curve points
// imply the on-curve point midway between them; a contour starts at its first
// on-curve point, or at the midpoint of its last and first points when it has
// none. Each quadratic segment from p0 with control q to p2 becomes the cubic
// with controls p0 + 2/3(q - p0) and p2 + 2/3(q - p2), which draws the same
// curve.
func (g glyphPoints) path() geom.Path {
	var p geom.Path
	start := 0
	for _, end := range g.ends {
		contour := g.pts[start : end+1]
		start = end + 1
		contourPath(&p, contour)
	}
	return p
}

// contourPath writes one contour.
func contourPath(p *geom.Path, c []ttPoint) {
	n := len(c)
	first := -1
	for i, pt := range c {
		if pt.on {
			first = i
			break
		}
	}
	var origin geom.Point
	hasOn := first >= 0
	if !hasOn {
		origin = midpoint(c[n-1].Point, c[0].Point)
		first = 0
	} else {
		origin = c[first].Point
		first++
	}
	p.MoveTo(origin)
	cur := origin
	var ctrl geom.Point
	pending := false
	for k := range n {
		pt := c[(first+k)%n]
		if hasOn && k == n-1 {
			// The last point visited is the origin, which the closing
			// segment reaches.
			break
		}
		switch {
		case pt.on && pending:
			quadTo(p, cur, ctrl, pt.Point)
			cur, pending = pt.Point, false
		case pt.on:
			p.LineTo(pt.Point)
			cur = pt.Point
		case pending:
			mid := midpoint(ctrl, pt.Point)
			quadTo(p, cur, ctrl, mid)
			cur, ctrl = mid, pt.Point
		default:
			ctrl, pending = pt.Point, true
		}
	}
	if pending {
		quadTo(p, cur, ctrl, origin)
	}
	p.Close()
}

// midpoint returns the point halfway between a and b.
func midpoint(a, b geom.Point) geom.Point {
	return geom.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
}

// quadTo appends the cubic that draws the quadratic Bézier from p0 with
// control q to p2.
func quadTo(p *geom.Path, p0, q, p2 geom.Point) {
	const twoThirds = 2.0 / 3.0
	c1 := geom.Point{X: p0.X + twoThirds*(q.X-p0.X), Y: p0.Y + twoThirds*(q.Y-p0.Y)}
	c2 := geom.Point{X: p2.X + twoThirds*(q.X-p2.X), Y: p2.Y + twoThirds*(q.Y-p2.Y)}
	p.CubicTo(c1, c2, p2)
}
