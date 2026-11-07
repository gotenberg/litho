package layout

import (
	"bytes"
	"fmt"

	gofont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// maxClipGlyphs bounds the glyphs whose outlines one background-clip:
// text clip gathers: past it the rest are left out of the clip, with a
// warning. Anti-DoS guard, not spec-mandated: a heading clips a few dozen
// glyphs, a page of text a few thousand.
const maxClipGlyphs = 20_000

// codeClipText is the warning of a text clip cut short.
const codeClipText = "background-clip-text"

// textClip returns the clip of background-clip: text for the box b (CSS
// Backgrounds 4, 2.4): the outlines of the glyphs b and its in-flow
// descendants draw, inline blocks and relatively positioned boxes
// included, in the painter's coordinates; nil when b draws none.
func (p *painter) textClip(b *box) *compose.Path {
	var ops []compose.PathOp
	glyphs := 0
	cut := false
	addRun := func(t *compose.Text, x, y float64) {
		if t == nil || t.Face == nil || cut {
			return
		}
		f := p.outlineFace(t.Face)
		if f == nil {
			return
		}
		upem := float64(f.Upem())
		if !(upem > 0) {
			return
		}
		k := t.Size / upem
		for _, g := range t.Glyphs {
			if glyphs == maxClipGlyphs {
				cut = true
				p.e.warnOnce(codeClipText, fmt.Sprintf("a background clipped to more than %d glyphs was clipped to the first", maxClipGlyphs), y)
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
		if depth > maxGroupDepth*8 || c.hidden && c.kind != kindBlock {
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
		for root = b.parent; root != nil && root.kind == kindInline; root = root.parent {
		}
		if root == nil {
			return nil
		}
	}
	walk(root, p.dx, p.dy, 0)
	if len(ops) == 0 {
		return nil
	}
	return &compose.Path{Ops: ops, Fill: &compose.Fill{Rule: compose.NonZero}}
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

// outlineFace returns the parsed program of the composed face cf, nil when
// it does not parse; each face is parsed once per layout.
func (p *painter) outlineFace(cf *compose.Face) *gofont.Face {
	if f, ok := p.outlines[cf]; ok {
		return f
	}
	if p.outlines == nil {
		p.outlines = map[*compose.Face]*gofont.Face{}
	}
	f, err := gofont.ParseTTF(bytes.NewReader(cf.Program))
	if err != nil {
		f = nil
	}
	p.outlines[cf] = f
	return f
}

// appendGlyph appends the outline of glyph gid of f, drawn at size k
// points per font unit with its origin at x, y and slanted by skew, to
// ops. Quadratic segments become the cubic Béziers they equal.
func appendGlyph(ops []compose.PathOp, f *gofont.Face, gid uint16, x, y, k, hs, skew float64) []compose.PathOp {
	data, ok := f.GlyphData(gofont.GID(gid)).(gofont.GlyphOutline)
	if !ok {
		return ops
	}
	pt := func(s opentype.SegmentPoint) compose.Point {
		up := float64(s.Y) * k
		return compose.Point{X: x + hs*float64(s.X)*k + skew*up, Y: y - up}
	}
	var last compose.Point
	open := false
	for _, s := range data.Segments {
		switch s.Op {
		case opentype.SegmentOpMoveTo:
			if open {
				ops = append(ops, compose.PathOp{Kind: compose.ClosePath})
			}
			last = pt(s.Args[0])
			ops = append(ops, compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{last}})
			open = true
		case opentype.SegmentOpLineTo:
			last = pt(s.Args[0])
			ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{last}})
		case opentype.SegmentOpQuadTo:
			c, to := pt(s.Args[0]), pt(s.Args[1])
			c1 := compose.Point{X: last.X + 2*(c.X-last.X)/3, Y: last.Y + 2*(c.Y-last.Y)/3}
			c2 := compose.Point{X: to.X + 2*(c.X-to.X)/3, Y: to.Y + 2*(c.Y-to.Y)/3}
			ops = append(ops, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{c1, c2, to}})
			last = to
		case opentype.SegmentOpCubeTo:
			last = pt(s.Args[2])
			ops = append(ops, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{pt(s.Args[0]), pt(s.Args[1]), last}})
		}
	}
	if open {
		ops = append(ops, compose.PathOp{Kind: compose.ClosePath})
	}
	return ops
}

// clipToText reports whether st clips its background to text.
func clipToText(st *css.Style) bool { return st.BackgroundClip == css.TextEdge }
