package layout

import (
	"bytes"
	"fmt"
	"log/slog"

	gofont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxClipGlyphs bounds the glyphs whose outlines one background-clip:
// text clip gathers: past it the rest are left out of the clip, with a
// warning. Anti-DoS guard, not spec-mandated: a heading clips a few dozen
// glyphs, a page of text a few thousand.
const maxClipGlyphs = 20_000

// maxClipDepth bounds the nesting of the boxes [painter.textClip] walks for
// the glyphs of one clip. Recursion guard, not spec-mandated: the text a
// box clips its background to sits a few boxes below it.
const maxClipDepth = maxGroupDepth * 8

// textClip returns the clip of background-clip: text for the box b (CSS
// Backgrounds 4, 2.4): the outlines of the glyphs b and its in-flow
// descendants draw, inline blocks and relatively positioned boxes
// included, in the painter's coordinates; nil when b draws none.
func (p *painter) textClip(b *box) *typeset.Path {
	var ops []typeset.PathOp
	glyphs := 0
	cut := false
	addRun := func(t *typeset.Text, x, y float64) {
		if t == nil || t.Face == nil || cut {
			return
		}
		f := p.outlineFace(t.Face)
		if f == nil {
			return
		}
		upem := float64(f.Upem())
		if upem == 0 {
			return
		}
		k := t.Size / upem
		for _, g := range t.Glyphs {
			if glyphs == maxClipGlyphs {
				cut = true
				p.e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("a background clipped to more than %d glyphs was clipped to the first", maxClipGlyphs), y)
				return
			}
			glyphs++
			hs := hscale(t)
			ox, oy := x+g.X*hs, y+g.Y
			ops = appendGlyph(ops, f, g.GID, ox, oy, k, hs, t.Skew)
		}
	}
	var walk func(c *box, dx, dy float64, depth int)
	walk = func(c *box, dx, dy float64, depth int) {
		if depth > maxClipDepth || c.hidden && c.kind != kindBlock {
			return
		}
		for _, l := range c.lines {
			for _, it := range l.items {
				if it.box == nil {
					continue
				}
				ox, oy := inlineOffset(it.box.parent)
				if it.atomic != nil {
					ax, ay := offset(it.atomic)
					walk(it.atomic, dx+ox+ax, dy+oy+ay, depth+1)
					continue
				}
				if it.text == nil || b.kind == kindInline && !descends(it.box, b) || it.box.hidden {
					continue
				}
				addRun(it.text, l.x+it.text.Origin.X+dx+ox, l.y+it.text.Origin.Y+dy+oy)
				// The background clips to the emphasis marks too, as
				// Chromium clips it: they are glyphs of the text's
				// geometry (CSS Backgrounds 4, 2.4).
				for _, m := range p.e.markRuns(it) {
					addRun(&m, l.x+m.Origin.X+dx+ox, l.y+m.Origin.Y+dy+oy)
				}
			}
		}
		for _, k := range c.kids {
			if k.abs || k.kind == kindInline || k.kind == kindText || k.kind == kindBreak {
				continue
			}
			kx, ky := offset(k)
			walk(k, dx+kx, dy+ky, depth+1)
		}
	}
	root := b
	if b.kind == kindInline {
		// An inline box clips to its own text: the lines of its block
		// container that hold it.
		root = b.parent
		for root != nil && root.kind == kindInline {
			root = root.parent
		}
		if root == nil {
			return nil
		}
	}
	walk(root, p.dx, p.dy, 0)
	if len(ops) == 0 {
		return nil
	}
	return &typeset.Path{Ops: ops, Fill: &typeset.Fill{Rule: typeset.NonZero}}
}

// descends reports whether a is d or lies inside it.
func descends(a, d *box) bool {
	for ; a != nil; a = a.parent {
		if a == d {
			return true
		}
	}
	return false
}

// outlineFace returns the parsed program of the typeset face cf, nil when
// it does not parse; each face is parsed once per layout.
func (p *painter) outlineFace(cf *typeset.Face) *gofont.Face { return p.e.outlineFace(cf) }

// outlineFace returns the parsed program of the typeset face cf, nil when
// it does not parse; each face is parsed once per layout.
func (e *engine) outlineFace(cf *typeset.Face) *gofont.Face {
	if f, ok := e.outlines[cf]; ok {
		return f
	}
	if e.outlines == nil {
		e.outlines = map[*typeset.Face]*gofont.Face{}
	}
	f, err := gofont.ParseTTF(bytes.NewReader(cf.Program))
	if err != nil {
		// The text stack shaped with this program, so it parses; one that
		// does not adds no glyph outline, as a face without outlines.
		if lg := e.opts.Logger; lg != nil {
			lg.Debug("layout: face without outlines", slog.Any("error", err))
		}
		f = nil
	}
	e.outlines[cf] = f
	return f
}

// appendGlyph appends the outline of glyph gid of f, drawn at size k
// points per font unit with its origin at x, y and slanted by skew, to
// ops. Quadratic segments become the cubic Béziers they equal.
func appendGlyph(ops []typeset.PathOp, f *gofont.Face, gid uint16, x, y, k, hs, skew float64) []typeset.PathOp {
	data, ok := f.GlyphData(gofont.GID(gid)).(gofont.GlyphOutline)
	if !ok {
		return ops
	}
	pt := func(s opentype.SegmentPoint) typeset.Point {
		up := float64(s.Y) * k
		return typeset.Point{X: x + hs*float64(s.X)*k + skew*up, Y: y - up}
	}
	var last typeset.Point
	open := false
	for _, s := range data.Segments {
		switch s.Op {
		case opentype.SegmentOpMoveTo:
			if open {
				ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
			}
			last = pt(s.Args[0])
			ops = append(ops, typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{last}})
			open = true
		case opentype.SegmentOpLineTo:
			last = pt(s.Args[0])
			ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{last}})
		case opentype.SegmentOpQuadTo:
			c, to := pt(s.Args[0]), pt(s.Args[1])
			c1 := typeset.Point{X: last.X + 2*(c.X-last.X)/3, Y: last.Y + 2*(c.Y-last.Y)/3}
			c2 := typeset.Point{X: to.X + 2*(c.X-to.X)/3, Y: to.Y + 2*(c.Y-to.Y)/3}
			ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{c1, c2, to}})
			last = to
		case opentype.SegmentOpCubeTo:
			last = pt(s.Args[2])
			ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(s.Args[0]), pt(s.Args[1]), last}})
		}
	}
	if open {
		ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	}
	return ops
}

// clipToText reports whether st clips its background to text.
func clipToText(st *css.Style) bool { return st.BackgroundClip == css.TextEdge }
