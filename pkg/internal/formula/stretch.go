package formula

import (
	"fmt"
	"math"

	gofont "github.com/go-text/typesetting/font"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// glyphs is a glyph, or the glyphs of an assembly, of one face at one
// size, placed from the left baseline point of their box, y downward,
// with the box's metrics: its ink is its box.
type glyphs struct {
	face                 *face
	size                 float64
	parts                []placed
	w, asc, desc, italic float64
	accent               float64
	hasAccent            bool
}

// placed is one glyph of [glyphs].
type placed struct {
	gid  uint16
	x, y float64
}

// singleGlyph returns glyph gid of f at size with its normal bounding
// box: its advance, its ink above and below the baseline, its italics
// correction and top accent attachment from the MATH table.
func (l *layouter) singleGlyph(f *face, gid uint16, size float64) *glyphs {
	g := &glyphs{face: f, size: size, parts: []placed{{gid: gid}}}
	g.w = f.units(float64(f.glyphs.HorizontalAdvance(gofont.GID(gid))), size)
	if ext, ok := f.glyphs.GlyphExtents(gofont.GID(gid)); ok {
		g.asc = f.units(float64(ext.YBearing), size)
		g.desc = -f.units(float64(ext.YBearing+ext.Height), size)
	}
	if v, ok := f.math.ItalicsCorrection(gid); ok {
		g.italic = f.units(float64(v), size)
	}
	if v, ok := f.math.TopAccentAttachment(gid); ok {
		g.accent, g.hasAccent = f.units(float64(v), size), true
	}
	return g
}

// stretchGlyph runs the algorithm to shape a stretchy glyph to inline or
// block dimension t of MathML Core, 5.3.2: the base glyph when it is
// large enough, else the first variant that is, else the glyph assembly,
// else the last one tried. nil when the font gives gid no construction
// along the axis.
func (l *layouter) stretchGlyph(f *face, gid uint16, vertical bool, t, size float64) *glyphs {
	variants, asm, ok := f.math.Construction(gid, vertical)
	if !ok {
		return nil
	}
	g := l.singleGlyph(f, gid, size)
	measure := func(g *glyphs) float64 {
		if vertical {
			return g.asc + g.desc
		}
		return g.w
	}
	if measure(g) >= t {
		return g
	}
	for _, v := range variants {
		g = l.singleGlyph(f, v.GID, size)
		if f.units(float64(v.Advance), size) >= t {
			return g
		}
	}
	if a := l.assemble(f, asm, vertical, t, size); a != nil {
		return a
	}
	return g
}

// assemble builds the glyph assembly asm of f to cover t (MathML Core,
// 5.3.1), nil when the assembly is not valid. Each extender repeats the
// least number of times that reaches t, at most [MaxAssemblyGlyphs]
// glyphs in all, and every connection overlaps by the same amount, the
// most that the connectors allow and that still reaches t.
//
// The connectors that bound the overlap are those that connect: the
// start connector of every part but a first part that does not repeat,
// and the end connector of every part but a last one that does not.
// MathML Core, 5.3.1 states the two exclusions the other way round,
// which would read the outer connectors of the end pieces, of length 0
// in the math fonts in use, and make every assembly invalid.
func (l *layouter) assemble(f *face, asm *mt.Assembly, vertical bool, t, size float64) *glyphs {
	if asm == nil || len(asm.Parts) == 0 {
		return nil
	}
	omin := f.units(float64(f.math.MinConnectorOverlap()), size)
	var nExt, nNonExt int
	var sExt, sNonExt float64
	maxOverlap := math.Inf(1)
	last := len(asm.Parts) - 1
	for i, p := range asm.Parts {
		full := f.units(float64(p.FullAdvance), size)
		start := f.units(float64(p.StartConnector), size)
		end := f.units(float64(p.EndConnector), size)
		if p.Extender {
			nExt++
			sExt += full
		} else {
			nNonExt++
			sNonExt += full
		}
		if i > 0 || p.Extender {
			if start < omin {
				return nil
			}
			maxOverlap = min(maxOverlap, start)
		}
		if i < last || p.Extender {
			if end < omin {
				return nil
			}
			maxOverlap = min(maxOverlap, end)
		}
	}
	sExtNonOverlapping := sExt - float64(omin*float64(nExt))
	if nExt == 0 || sExtNonOverlapping <= 0 {
		return nil
	}
	count := func(r int) int { return nNonExt + r*nExt }
	size1 := func(o float64, r int) float64 {
		return sNonExt + float64(float64(r)*sExt) - float64(o*float64(count(r)-1))
	}
	rmin := 0
	if need := (t - sNonExt + float64(omin*float64(nNonExt-1))) / sExtNonOverlapping; need > 0 {
		rmin = int(math.Ceil(min(need, MaxAssemblyGlyphs))) // fits int: at most MaxAssemblyGlyphs
	}
	if count(rmin) > MaxAssemblyGlyphs {
		rmin = max(0, (MaxAssemblyGlyphs-nNonExt)/nExt)
		l.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("a stretched operator was cut to %d glyphs", MaxAssemblyGlyphs))
	}
	o := 0.0
	if n := count(rmin); n > 1 {
		o = max(0, min((size1(0, rmin)-t)/float64(n-1), maxOverlap))
	}
	if !l.mem.Take(int64(count(rmin)), limits.GlyphCost, "formula glyphs") {
		l.fail(l.mem.Err())
		return nil
	}
	g := &glyphs{face: f, size: size, italic: f.units(float64(asm.ItalicsCorrection), size)}
	pos := 0.0
	for _, p := range asm.Parts {
		reps := 1
		if p.Extender {
			reps = rmin
		}
		full := f.units(float64(p.FullAdvance), size)
		for range reps {
			if vertical {
				// The part's ink bottom sits pos above the assembly's
				// bottom, which is its baseline.
				bottom := 0.0
				if ext, ok := f.glyphs.GlyphExtents(gofont.GID(p.GID)); ok {
					bottom = f.units(float64(ext.YBearing+ext.Height), size)
				}
				g.parts = append(g.parts, placed{gid: p.GID, y: -(pos - bottom)})
				g.w = max(g.w, f.units(float64(f.glyphs.HorizontalAdvance(gofont.GID(p.GID))), size))
			} else {
				g.parts = append(g.parts, placed{gid: p.GID, x: pos})
				if ext, ok := f.glyphs.GlyphExtents(gofont.GID(p.GID)); ok {
					g.asc = max(g.asc, f.units(float64(ext.YBearing), size))
					g.desc = max(g.desc, -f.units(float64(ext.YBearing+ext.Height), size))
				}
			}
			pos += full - o
		}
	}
	if vertical {
		g.asc = size1(o, rmin)
	} else {
		g.w = size1(o, rmin)
	}
	return g
}

// glyphBox returns the box of g, its glyphs moved down by delta, drawn
// in st's colour unless st hides them. The first glyph stands for the
// character r, the others for nothing: the text copied from the page
// reads the operator once.
func (l *layouter) glyphBox(g *glyphs, r rune, st *Style, delta float64) *box {
	b := &box{
		w: g.w, asc: g.asc - delta, desc: g.desc + delta, italic: g.italic,
		accent: g.accent, hasAccent: g.hasAccent,
	}
	b.inkAsc, b.inkDesc = b.asc, b.desc
	if st.Hidden || len(g.parts) == 0 {
		return b
	}
	run := &typeset.Text{Face: g.face.ts, Size: g.size, Color: st.Color, Glyphs: make([]typeset.Glyph, len(g.parts))}
	for i, p := range g.parts {
		run.Glyphs[i] = typeset.Glyph{GID: p.gid, X: p.x, Y: float64(p.y + delta)}
	}
	run.Glyphs[0].Text = string(r)
	b.items = append(b.items, item{run: run})
	return b
}
