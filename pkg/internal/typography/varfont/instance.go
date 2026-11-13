package varfont

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// maxInstanceWork caps the deltas one instance decodes, X and Y counted
// apart: 2^27. Anti-DoS guard, not spec-mandated: a tuple of a few bytes
// can name every point of a glyph through runs of zeros, so the data's
// size does not bound the work. A large CJK font, tens of thousands of
// glyphs with a few regions of a few dozen points each, decodes some
// 2^25.
const maxInstanceWork = 1 << 27

// instanceCtxCheckEvery sets how many glyphs an instance processes between
// two checks of its context.
const instanceCtxCheckEvery = 256

// Table offsets the instancer reads and writes (OpenType specification,
// 'head', 'hhea', 'vhea', 'maxp', 'OS/2' and 'post' tables).
const (
	headSize              = 54
	headBoxAt             = 36
	headIndexToLocAt      = 50
	hheaSize              = 36
	hheaAscenderAt        = 4
	hheaDescenderAt       = 6
	hheaLineGapAt         = 8
	hheaAdvanceMaxAt      = 10
	hheaMinLSBAt          = 12
	hheaMinRSBAt          = 14
	hheaXMaxExtentAt      = 16
	hheaCaretRiseAt       = 18
	hheaCaretRunAt        = 20
	hheaCaretOffsetAt     = 22
	hheaNumberOfMetricsAt = 34
	maxpNumGlyphsAt       = 4
	maxpMinSize           = 6
	os2WeightClassAt      = 4
	os2WidthClassAt       = 6
	postItalicAngleAt     = 4
	postUnderlineAt       = 8
	postMinSize           = 12
)

// droppedTables are left out of an instance: the variation tables, which
// describe a design space the instance no longer has, and the hinting and
// device tables, whose values belong to the default instance's outlines.
var droppedTables = []string{
	"HVAR", "LTSH", "MVAR", "STAT", "VDMX", "VVAR", "avar", "cvar", "cvt ", "fpgm", "fvar", "gvar", "hdmx", "prep",
}

// mvarTargets maps each 'MVAR' value tag to the table and byte offset of
// the value it varies (OpenType specification, 'MVAR' table, Value tags),
// and whether the value is unsigned. The gasp tags are left out: an
// instance keeps the default gasp ranges.
var mvarTargets = map[string]struct {
	table    string
	at       int
	unsigned bool
}{
	"hasc": {"OS/2", 68, false}, "hdsc": {"OS/2", 70, false}, "hlgp": {"OS/2", 72, false},
	"hcla": {"OS/2", 74, true}, "hcld": {"OS/2", 76, true},
	"vasc": {"vhea", 4, false}, "vdsc": {"vhea", 6, false}, "vlgp": {"vhea", 8, false},
	"hcrs": {"hhea", hheaCaretRiseAt, false}, "hcrn": {"hhea", hheaCaretRunAt, false}, "hcof": {"hhea", hheaCaretOffsetAt, false},
	"vcrs": {"vhea", 18, false}, "vcrn": {"vhea", 20, false}, "vcof": {"vhea", 22, false},
	"xhgt": {"OS/2", 86, false}, "cpht": {"OS/2", 88, false},
	"sbxs": {"OS/2", 10, false}, "sbys": {"OS/2", 12, false}, "sbxo": {"OS/2", 14, false}, "sbyo": {"OS/2", 16, false},
	"spxs": {"OS/2", 18, false}, "spys": {"OS/2", 20, false}, "spxo": {"OS/2", 22, false}, "spyo": {"OS/2", 24, false},
	"strs": {"OS/2", 26, false}, "stro": {"OS/2", 28, false},
	"unds": {"post", 10, false}, "undo": {"post", postUnderlineAt, false},
}

// Font is a variable font program ready to be instanced.
type Font struct {
	program []byte
	records []truetype.TableRecord
	axes    *Axes
	avar    *Avar
}

// Parse reads the design space of program, an sfnt program: its 'fvar'
// table and, when present, its 'avar' table. It fails with
// [truetype.ErrTableNotFound] for a program without 'fvar', with
// [ErrUnsupported] for an 'avar' of version 2, and with [ErrInvalid] for
// variation tables it cannot read. A program with CFF2 outlines parses:
// [Font.Instance] refuses it.
func Parse(program []byte) (*Font, error) {
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		return nil, err
	}
	fvar, err := truetype.TableData(program, records, "fvar")
	if err != nil {
		return nil, err
	}
	axes, err := ParseFvar(fvar)
	if err != nil {
		return nil, err
	}
	f := &Font{program: program, records: records, axes: axes}
	if data, err := truetype.TableData(program, records, "avar"); err == nil {
		if f.avar, err = ParseAvar(data, len(axes.Axes)); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// HasCFF2 reports a program with CFF2 outlines and no 'glyf' table, whose
// default instance a PDF cannot embed as it stands: its instance at the
// default coordinates is the same font with CFF outlines.
func (f *Font) HasCFF2() bool {
	has := func(tag string) bool {
		return slices.ContainsFunc(f.records, func(r truetype.TableRecord) bool { return r.Tag == tag })
	}
	return has("CFF2") && !has("glyf")
}

// Axes returns the font's design space. The caller must not modify it.
func (f *Font) Axes() *Axes { return f.axes }

// Normalize returns the normalized coordinates of user, one user
// coordinate per axis (see [Normalize]).
func (f *Font) Normalize(user []float64) []int16 { return Normalize(f.axes, f.avar, user) }

// glyphMetrics are a glyph's advance and side bearing.
type glyphMetrics struct {
	advance uint16
	bearing int16
}

// instancer holds the state of one [Font.Instance] call.
type instancer struct {
	f      *Font
	coords []int16
	glyf   []byte
	loca   []int
	n      int
	h, v   []glyphMetrics
	gvar   *gvarTable
	hvar   *advanceVariations
	vvar   *advanceVariations
	work   int
	// out holds each instance glyph; comps and offsets the components of
	// a composite one and their instance offsets.
	out     [][]byte
	boxes   [][4]int16
	comps   [][]component
	offsets [][2][]int32
	// phantom holds each glyph's instance phantom points, unrounded.
	phantom [][phantomPoints]point
	// used marks the glyphs a composite uses; memo holds their instance
	// points.
	used map[int]bool
	memo map[int][]point
}

// Instance returns the static instance of the font at the user
// coordinates user, one per axis, each clamped to its axis (OpenType Font
// Variations Overview): a TrueType program whose outlines carry the
// 'gvar' deltas, whose advances carry the 'HVAR' and 'VVAR' deltas, or the
// phantom points' when the font has neither, whose font-wide metrics carry
// the 'MVAR' deltas, and whose PostScript name names the instance (see
// [PostScriptName]). Coordinates and metrics are rounded half up once
// every delta is summed. The variation, hinting and device tables are
// dropped; every other table is kept as it is.
//
// A program with CFF2 outlines becomes an OpenType program with CFF
// outlines at the instance, its blends resolved and its 16.16 values kept
// (see [cff.InstanceCFF2]); its vertical metrics stay the default
// instance's.
//
// It fails with [ErrUnsupported] for a program with neither 'glyf' nor
// 'CFF2' outlines,
// with [ErrInvalid] for tables it cannot read, with [ErrTooComplex] past
// the work bound, and with ctx's error when ctx ends.
func (f *Font) Instance(ctx context.Context, user []float64) ([]byte, error) {
	in := &instancer{f: f, coords: f.Normalize(user), memo: map[int][]point{}, used: map[int]bool{}}
	tables, err := in.tables()
	if err != nil {
		return nil, err
	}
	if tables["glyf"] == nil && tables["CFF2"] != nil {
		return in.instanceCFF2(ctx, tables, user)
	}
	if err := in.read(tables); err != nil {
		return nil, err
	}
	if err := in.markComponents(); err != nil {
		return nil, err
	}
	for gid := range in.n {
		if gid%instanceCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if err := in.glyph(gid); err != nil {
			return nil, fmt.Errorf("glyph %d: %w", gid, err)
		}
	}
	for gid := range in.n {
		if in.comps[gid] == nil {
			continue
		}
		if gid%instanceCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if err := in.composite(gid); err != nil {
			return nil, fmt.Errorf("glyph %d: %w", gid, err)
		}
	}
	return in.assemble(tables, user)
}

// tables returns every table of the program by tag.
func (in *instancer) tables() (map[string][]byte, error) {
	tables := make(map[string][]byte, len(in.f.records))
	for _, r := range in.f.records {
		data, err := truetype.TableData(in.f.program, in.f.records, r.Tag)
		if err != nil {
			return nil, err
		}
		tables[r.Tag] = data
	}
	return tables, nil
}

// read checks and reads the 'glyf' tables the instance is built from, of
// tables, every table of the program by tag.
func (in *instancer) read(tables map[string][]byte) error {
	if tables["glyf"] == nil || tables["loca"] == nil {
		return fmt.Errorf("%w: no glyf outlines", ErrUnsupported)
	}
	head, hhea, maxp, hmtx := tables["head"], tables["hhea"], tables["maxp"], tables["hmtx"]
	if len(head) < headSize || len(hhea) < hheaSize || len(maxp) < maxpMinSize || hmtx == nil {
		return fmt.Errorf("%w: head, hhea, maxp or hmtx missing or short", ErrInvalid)
	}
	in.n = int(binary.BigEndian.Uint16(maxp[maxpNumGlyphsAt:]))
	in.glyf = tables["glyf"]
	var err error
	if in.loca, err = readLoca(tables["loca"], len(in.glyf), in.n, binary.BigEndian.Uint16(head[headIndexToLocAt:]) != 0); err != nil {
		return err
	}
	if in.h, err = readMetrics(hmtx, int(binary.BigEndian.Uint16(hhea[hheaNumberOfMetricsAt:])), in.n); err != nil {
		return fmt.Errorf("hmtx: %w", err)
	}
	if vhea, vmtx := tables["vhea"], tables["vmtx"]; len(vhea) >= hheaSize && vmtx != nil {
		if in.v, err = readMetrics(vmtx, int(binary.BigEndian.Uint16(vhea[hheaNumberOfMetricsAt:])), in.n); err != nil {
			return fmt.Errorf("vmtx: %w", err)
		}
	}
	axisCount := len(in.f.axes.Axes)
	if data := tables["gvar"]; data != nil {
		if in.gvar, err = parseGvar(data, axisCount, in.n); err != nil {
			return err
		}
	}
	if data := tables["HVAR"]; data != nil {
		if in.hvar, err = parseAdvanceVariations(data, in.coords, false); err != nil {
			return fmt.Errorf("HVAR: %w", err)
		}
	}
	if data := tables["VVAR"]; data != nil && in.v != nil {
		if in.vvar, err = parseAdvanceVariations(data, in.coords, true); err != nil {
			return fmt.Errorf("VVAR: %w", err)
		}
	}
	in.out = make([][]byte, in.n)
	in.boxes = make([][4]int16, in.n)
	in.comps = make([][]component, in.n)
	in.offsets = make([][2][]int32, in.n)
	in.phantom = make([][phantomPoints]point, in.n)
	return nil
}

// readLoca reads the n+1 glyph offsets of a 'loca' table into a glyf table
// of glyfLen bytes, long or short (OpenType specification, 'loca' table).
func readLoca(loca []byte, glyfLen, n int, long bool) ([]int, error) {
	width := 2
	if long {
		width = 4
	}
	if (n+1)*width > len(loca) {
		return nil, fmt.Errorf("%w: loca shorter than %d glyphs", ErrInvalid, n)
	}
	out := make([]int, n+1)
	for i := range out {
		var o uint64
		if long {
			o = uint64(binary.BigEndian.Uint32(loca[4*i:]))
		} else {
			o = 2 * uint64(binary.BigEndian.Uint16(loca[2*i:]))
		}
		if o > uint64(glyfLen) || (i > 0 && o < uint64(out[i-1])) { //nolint:gosec // lengths and earlier offsets are not negative
			return nil, fmt.Errorf("%w: loca offset %d of glyph %d", ErrInvalid, o, i)
		}
		out[i] = int(o) //nolint:gosec // fits int: at most glyfLen
	}
	return out, nil
}

// readMetrics reads the advances and side bearings of n glyphs from an
// 'hmtx' or 'vmtx' table of long metrics numbers of long metrics: a glyph
// past them takes the last advance and its own bearing (OpenType
// specification, 'hmtx' table).
func readMetrics(mtx []byte, long, n int) ([]glyphMetrics, error) {
	long = min(long, n)
	if long == 0 || 4*long > len(mtx) {
		return nil, fmt.Errorf("%w: %d long metrics in %d bytes", ErrInvalid, long, len(mtx))
	}
	out := make([]glyphMetrics, n)
	for i := range out {
		if i < long {
			out[i] = glyphMetrics{advance: binary.BigEndian.Uint16(mtx[4*i:]), bearing: int16(binary.BigEndian.Uint16(mtx[4*i+2:]))} //nolint:gosec // reinterprets a signed side bearing
			continue
		}
		out[i].advance = out[long-1].advance
		if at := 4*long + 2*(i-long); at+2 <= len(mtx) {
			out[i].bearing = int16(binary.BigEndian.Uint16(mtx[at:])) //nolint:gosec // as above
		}
	}
	return out, nil
}

// glyph instances the outline points, or the component offsets, and the
// phantom points of glyph gid.
func (in *instancer) glyph(gid int) error {
	g := in.glyf[in.loca[gid]:in.loca[gid+1]]
	var contours int
	var box [4]float64
	if len(g) > 0 {
		if len(g) < glyphHeaderSize {
			return fmt.Errorf("%w: glyph header past its data", ErrInvalid)
		}
		contours = int(int16(binary.BigEndian.Uint16(g))) //nolint:gosec // reinterprets the signed numberOfContours
		for i := range box {
			box[i] = float64(int16(binary.BigEndian.Uint16(g[2+2*i:]))) //nolint:gosec // reinterprets a signed FWORD
		}
	}
	var s *simpleGlyph
	var orig []point
	switch {
	case len(g) == 0:
	case contours >= 0:
		var err error
		if s, err = parseSimple(g, contours); err != nil {
			return err
		}
		if len(s.pts)+phantomPoints > maxGlyphPoints {
			return fmt.Errorf("%w: %d points", ErrInvalid, len(s.pts))
		}
		orig = append(make([]point, 0, len(s.pts)+phantomPoints), s.pts...)
	default:
		comps, err := parseComposite(g)
		if err != nil {
			return err
		}
		in.comps[gid] = comps
		orig = make([]point, 0, len(comps)+phantomPoints)
		for _, c := range comps {
			orig = append(orig, point{float64(c.dx), float64(c.dy)})
		}
	}
	orig = append(orig, in.phantoms(gid, box)...)
	var dx, dy []float64
	if in.gvar != nil {
		var ends []int
		if s != nil {
			ends = s.ends
		}
		var err error
		if dx, dy, err = in.gvar.glyphDeltas(gid, in.coords, orig, ends, &in.work); err != nil {
			return err
		}
	}
	if dx == nil {
		dx, dy = make([]float64, len(orig)), make([]float64, len(orig))
	}
	m := len(orig) - phantomPoints
	for k := range phantomPoints {
		in.phantom[gid][k] = point{orig[m+k].x + dx[m+k], orig[m+k].y + dy[m+k]}
	}
	switch {
	case s != nil:
		xs, ys := make([]int16, m), make([]int16, m)
		for i := range m {
			xs[i] = clampInt16(otRound(orig[i].x + dx[i]))
			ys[i] = clampInt16(otRound(orig[i].y + dy[i]))
		}
		in.out[gid], in.boxes[gid] = encodeSimple(s, xs, ys)
		if in.used[gid] {
			pts := make([]point, m)
			for i := range m {
				pts[i] = point{float64(xs[i]), float64(ys[i])}
			}
			in.memo[gid] = pts
		}
	case in.comps[gid] != nil:
		ox, oy := make([]int32, m), make([]int32, m)
		for i, c := range in.comps[gid] {
			ox[i], oy[i] = c.dx, c.dy
			// Deltas move a component placed by offsets; one placed by
			// matching points keeps its point numbers ('gvar' table,
			// Point numbers and processing for composite glyphs).
			if c.flags&argsAreXY != 0 {
				ox[i] = otRound(float64(c.dx) + dx[i])
				oy[i] = otRound(float64(c.dy) + dy[i])
			}
		}
		in.offsets[gid] = [2][]int32{ox, oy}
	}
	return nil
}

// markComponents records the glyphs composites use.
func (in *instancer) markComponents() error {
	for gid := range in.n {
		g := in.glyf[in.loca[gid]:in.loca[gid+1]]
		if len(g) < glyphHeaderSize || int16(binary.BigEndian.Uint16(g)) >= 0 { //nolint:gosec // reinterprets the signed numberOfContours
			continue
		}
		comps, err := parseComposite(g)
		if err != nil {
			return fmt.Errorf("glyph %d: %w", gid, err)
		}
		for _, c := range comps {
			in.used[int(c.gid)] = true
		}
	}
	return nil
}

// phantoms returns the default phantom points of glyph gid, whose
// bounding box is box: left and right from 'hmtx', top and bottom from
// 'vmtx' or 0 without it (OpenType specification, Instructing TrueType
// glyphs, Phantom points).
func (in *instancer) phantoms(gid int, box [4]float64) []point {
	h := in.h[gid]
	left := box[0] - float64(h.bearing)
	out := []point{{left, 0}, {left + float64(h.advance), 0}, {0, 0}, {0, 0}}
	if in.v != nil {
		v := in.v[gid]
		top := box[3] + float64(v.bearing)
		out[2], out[3] = point{0, top}, point{0, top - float64(v.advance)}
	}
	return out
}

// composite computes the bounding box of composite glyph gid at the
// instance and encodes it.
func (in *instancer) composite(gid int) error {
	pts, err := in.points(gid, 0)
	if err != nil {
		return err
	}
	box := [4]int16{}
	if len(pts) > 0 {
		lo, hi := pts[0], pts[0]
		for _, p := range pts {
			lo.x, lo.y = min(lo.x, p.x), min(lo.y, p.y)
			hi.x, hi.y = max(hi.x, p.x), max(hi.y, p.y)
		}
		box = [4]int16{clampInt16(otRound(lo.x)), clampInt16(otRound(lo.y)), clampInt16(otRound(hi.x)), clampInt16(otRound(hi.y))}
	}
	in.boxes[gid] = box
	in.out[gid] = encodeComposite(in.comps[gid], in.offsets[gid][0], in.offsets[gid][1], box)
	return nil
}

// points returns the outline points of glyph gid at the instance, its
// components resolved and transformed (OpenType specification, 'glyf'
// table, Composite glyph description), at most [maxGlyphPoints] of them.
func (in *instancer) points(gid, depth int) ([]point, error) {
	if depth > maxComponentDepth {
		return nil, fmt.Errorf("%w: components nest deeper than %d", ErrInvalid, maxComponentDepth)
	}
	if p, ok := in.memo[gid]; ok {
		return p, nil
	}
	if gid >= in.n {
		return nil, fmt.Errorf("%w: component glyph %d of %d", ErrInvalid, gid, in.n)
	}
	comps := in.comps[gid]
	if comps == nil {
		// A simple glyph's points are recorded when it is instanced, if a
		// composite uses it; an empty glyph has none.
		return nil, nil
	}
	children := make([][]point, len(comps))
	total := 0
	for i, c := range comps {
		child, err := in.points(int(c.gid), depth+1)
		if err != nil {
			return nil, err
		}
		if total += len(child); total > maxGlyphPoints {
			return nil, fmt.Errorf("%w: composite of more than %d points", ErrInvalid, maxGlyphPoints)
		}
		children[i] = child
	}
	pts := make([]point, 0, total)
	for i, c := range comps {
		move := func(p point) point {
			return point{float64(c.m[0]*p.x) + float64(c.m[2]*p.y), float64(c.m[1]*p.x) + float64(c.m[3]*p.y)}
		}
		var ox, oy float64
		if c.flags&argsAreXY != 0 {
			ox, oy = float64(in.offsets[gid][0][i]), float64(in.offsets[gid][1][i])
			if c.flags&scaledOffset != 0 && c.flags&unscaledOffset == 0 {
				ox, oy = float64(c.m[0]*ox)+float64(c.m[2]*oy), float64(c.m[1]*ox)+float64(c.m[3]*oy)
			}
		} else {
			parent, own := int(c.dx), int(c.dy)
			if parent >= len(pts) || own >= len(children[i]) {
				return nil, fmt.Errorf("%w: component anchored at point %d of %d", ErrInvalid, parent, len(pts))
			}
			anchor := move(children[i][own])
			ox, oy = pts[parent].x-anchor.x, pts[parent].y-anchor.y
		}
		for _, p := range children[i] {
			p = move(p)
			pts = append(pts, point{p.x + ox, p.y + oy})
		}
	}
	in.memo[gid] = pts
	return pts, nil
}

// assemble writes the instance program from the instanced glyphs and the
// program's other tables.
func (in *instancer) assemble(tables map[string][]byte, user []float64) ([]byte, error) {
	glyf, loca := make([]byte, 0, len(in.glyf)), make([]byte, 0, 4*(in.n+1))
	for _, g := range in.out {
		loca = binary.BigEndian.AppendUint32(loca, uint32(len(glyf))) //nolint:gosec // the glyf table is at most a few times its source, itself bounded by the sfnt's uint32 offsets
		glyf = append(glyf, g...)
		for len(glyf)%4 != 0 {
			glyf = append(glyf, 0)
		}
	}
	loca = binary.BigEndian.AppendUint32(loca, uint32(len(glyf))) //nolint:gosec // as above
	if len(glyf) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: instance glyf table of %d bytes", ErrInvalid, len(glyf))
	}

	hmtx := make([]byte, 0, 4*in.n)
	hhea := slices.Clone(tables["hhea"])
	var advMax uint16
	minLSB, minRSB, maxExtent := int16(math.MaxInt16), int16(math.MaxInt16), int16(math.MinInt16)
	box := [4]int16{math.MaxInt16, math.MaxInt16, math.MinInt16, math.MinInt16}
	for gid := range in.n {
		ph := in.phantom[gid]
		adv := otRound(ph[1].x - ph[0].x)
		if in.hvar != nil {
			adv = int32(in.h[gid].advance) + otRound(in.hvar.delta(gid))
		}
		advance := uint16(min(max(adv, 0), math.MaxUint16)) //nolint:gosec // clamped to the uint16 range first
		b := in.boxes[gid]
		lsb := clampInt16(int32(b[0]) - otRound(ph[0].x))
		hmtx = binary.BigEndian.AppendUint16(hmtx, advance)
		hmtx = binary.BigEndian.AppendUint16(hmtx, uint16(lsb)) //nolint:gosec // writes the 16 bits of a signed FWORD
		advMax = max(advMax, advance)
		if len(in.out[gid]) == 0 {
			continue
		}
		minLSB = min(minLSB, lsb)
		minRSB = min(minRSB, clampInt16(int32(advance)-int32(lsb)-(int32(b[2])-int32(b[0]))))
		maxExtent = max(maxExtent, clampInt16(int32(lsb)+int32(b[2])-int32(b[0])))
		box = [4]int16{min(box[0], b[0]), min(box[1], b[1]), max(box[2], b[2]), max(box[3], b[3])}
	}
	if box[0] > box[2] {
		box = [4]int16{}
		minLSB, minRSB, maxExtent = 0, 0, 0
	}
	putU16(hhea, hheaAdvanceMaxAt, advMax)
	putI16(hhea, hheaMinLSBAt, minLSB)
	putI16(hhea, hheaMinRSBAt, minRSB)
	putI16(hhea, hheaXMaxExtentAt, maxExtent)
	putU16(hhea, hheaNumberOfMetricsAt, uint16(in.n)) //nolint:gosec // n came from maxp's uint16 numGlyphs

	head := slices.Clone(tables["head"])
	for i, v := range box {
		putI16(head, headBoxAt+2*i, v)
	}
	putU16(head, headIndexToLocAt, 1)

	out := map[string][]byte{"glyf": glyf, "loca": loca, "hmtx": hmtx, "hhea": hhea, "head": head}
	if in.v != nil {
		vhea, vmtx := slices.Clone(tables["vhea"]), make([]byte, 0, 4*in.n)
		for gid := range in.n {
			ph := in.phantom[gid]
			adv := otRound(ph[2].y - ph[3].y)
			if in.vvar != nil {
				adv = int32(in.v[gid].advance) + otRound(in.vvar.delta(gid))
			}
			tsb := clampInt16(otRound(ph[2].y) - int32(in.boxes[gid][3]))
			vmtx = binary.BigEndian.AppendUint16(vmtx, uint16(min(max(adv, 0), math.MaxUint16))) //nolint:gosec // clamped to the uint16 range first
			vmtx = binary.BigEndian.AppendUint16(vmtx, uint16(tsb))                              //nolint:gosec // writes the 16 bits of a signed FWORD
		}
		putU16(vhea, hheaNumberOfMetricsAt, uint16(in.n)) //nolint:gosec // as for hhea
		out["vhea"], out["vmtx"] = vhea, vmtx
	}
	for _, tag := range []string{"OS/2", "post"} {
		if t := tables[tag]; t != nil {
			out[tag] = slices.Clone(t)
		}
	}
	if err := in.applyMVAR(tables, out); err != nil {
		return nil, err
	}
	in.applyStyle(out, user)
	if name := tables["name"]; name != nil {
		renamed, err := renamePostScript(name, PostScriptName(in.f.axes, name, user))
		if err != nil {
			return nil, err
		}
		out["name"] = renamed
	}
	tags := make([]string, 0, len(tables))
	for tag, data := range tables {
		if slices.Contains(droppedTables, tag) {
			continue
		}
		if _, ok := out[tag]; !ok {
			out[tag] = data
		}
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	return truetype.AssembleTables(0x00010000, tags, out), nil
}

// applyMVAR adds the 'MVAR' deltas to the values they vary, each rounded
// half up and clamped to its field. The hhea ascender, descender and line
// gap follow the OS/2 typographic ones when they equalled them, as
// fontTools' instancer keeps them in sync: 'MVAR' has no tag of its own for
// them.
func (in *instancer) applyMVAR(tables, out map[string][]byte) error {
	data := tables["MVAR"]
	if data == nil {
		return nil
	}
	deltas, err := parseMVAR(data, in.coords)
	if err != nil {
		return fmt.Errorf("MVAR: %w", err)
	}
	os2, hhea := out["OS/2"], out["hhea"]
	synced := len(os2) >= 74 && i16(os2, 68) == i16(hhea, hheaAscenderAt) && i16(os2, 70) == i16(hhea, hheaDescenderAt) && i16(os2, 72) == i16(hhea, hheaLineGapAt)
	tags := make([]string, 0, len(deltas))
	for tag := range deltas {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	for _, tag := range tags {
		t, ok := mvarTargets[tag]
		d := otRound(deltas[tag])
		if !ok || d == 0 {
			continue
		}
		dst := out[t.table]
		if len(dst) < t.at+2 {
			continue
		}
		if t.unsigned {
			v := int32(binary.BigEndian.Uint16(dst[t.at:])) + d
			putU16(dst, t.at, uint16(min(max(v, 0), math.MaxUint16))) //nolint:gosec // clamped to the uint16 range first
			continue
		}
		putI16(dst, t.at, clampInt16(int32(i16(dst, t.at))+d))
	}
	if synced {
		putI16(hhea, hheaAscenderAt, i16(os2, 68))
		putI16(hhea, hheaDescenderAt, i16(os2, 70))
		putI16(hhea, hheaLineGapAt, i16(os2, 72))
	}
	return nil
}

// widthClasses are the widths, in percent of normal, of OS/2
// usWidthClass 1 to 9 (OpenType specification, 'OS/2' table).
var widthClasses = [...]float64{50, 62.5, 75, 87.5, 100, 112.5, 125, 150, 200}

// applyStyle sets the instance's OS/2 weight and width classes and its
// post italic angle from its 'wght', 'wdth' and 'slnt' coordinates, the
// values the 'MVAR' table cannot vary (OpenType specification, 'MVAR'
// table, Value tags): usWeightClass is the weight, usWidthClass the class
// whose width is nearest, italicAngle the slant, both counter-clockwise
// degrees.
func (in *instancer) applyStyle(out map[string][]byte, user []float64) {
	value := func(tag string) (float64, bool) {
		i := in.f.axes.Index(tag)
		if i < 0 {
			return 0, false
		}
		v := in.f.axes.Axes[i].Default
		if i < len(user) {
			v = in.f.axes.Clamp(i, user[i])
		}
		return v, true
	}
	if os2 := out["OS/2"]; len(os2) >= os2WidthClassAt+2 {
		if w, ok := value("wght"); ok {
			putU16(os2, os2WeightClassAt, uint16(min(max(math.Floor(w+0.5), 1), 1000)))
		}
		if w, ok := value("wdth"); ok {
			class, best := 1, math.Inf(1)
			for i, c := range widthClasses {
				if d := math.Abs(c - w); d < best {
					class, best = i+1, d
				}
			}
			putU16(os2, os2WidthClassAt, uint16(class)) //nolint:gosec // 1 to 9
		}
	}
	if post := out["post"]; len(post) >= postMinSize {
		if s, ok := value("slnt"); ok {
			binary.BigEndian.PutUint32(post[postItalicAngleAt:], uint32(floatToFixed(s))) //nolint:gosec // writes the 32 bits of a signed Fixed
		}
	}
}

// i16 reads the int16 at b[at:].
func i16(b []byte, at int) int16 { return int16(binary.BigEndian.Uint16(b[at:])) } //nolint:gosec // reinterprets a signed field

// putI16 writes v at b[at:].
func putI16(b []byte, at int, v int16) { binary.BigEndian.PutUint16(b[at:], uint16(v)) } //nolint:gosec // writes the 16 bits of a signed field

// putU16 writes v at b[at:].
func putU16(b []byte, at int, v uint16) { binary.BigEndian.PutUint16(b[at:], v) }
