package text

import (
	"bytes"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Outlines reads the glyph outlines of laid-out runs, for a converter
// that draws text as paths: a text path, a warped text. It parses the
// program of each [typeset.Face] once. The zero value is ready to use; an
// Outlines is not safe for concurrent use.
type Outlines struct {
	faces map[*typeset.Face]*font.Face
}

// Face returns the parsed program of cf, nil when it does not parse or
// has no units per em, so draws no outline.
func (o *Outlines) Face(cf *typeset.Face) *font.Face {
	if cf == nil {
		return nil
	}
	if f, ok := o.faces[cf]; ok {
		return f
	}
	if o.faces == nil {
		o.faces = map[*typeset.Face]*font.Face{}
	}
	// The text stack shaped the run with this program, so it parses; one
	// that does not draws no outline, as a face without outlines.
	f, err := font.ParseTTF(bytes.NewReader(cf.Program))
	if err != nil || f.Upem() == 0 {
		f = nil
	}
	o.faces[cf] = f
	return f
}

// Glyph appends to ops the outline of glyph i of the run t, in the run's
// coordinates (its Origin, y downward), drawn at its size, horizontal
// scale and skew; nothing for a glyph of a face without outlines or a
// glyph out of range. Quadratic segments become the cubic Béziers they
// equal. A run turned Upright or Mirrored is drawn as an unturned one.
func (o *Outlines) Glyph(ops []typeset.PathOp, t *typeset.Text, i int) []typeset.PathOp {
	f := o.Face(t.Face)
	if f == nil || i < 0 || i >= len(t.Glyphs) {
		return ops
	}
	g := t.Glyphs[i]
	hs := hScale(t)
	k := t.Size / float64(f.Upem())
	return AppendGlyphOutline(ops, f, g.GID, t.Origin.X+float64(g.X*hs), t.Origin.Y+g.Y, k, hs, t.Skew)
}

// Run appends to ops the outlines of every glyph of t, as [Outlines.Glyph]
// draws each.
func (o *Outlines) Run(ops []typeset.PathOp, t *typeset.Text) []typeset.PathOp {
	for i := range t.Glyphs {
		ops = o.Glyph(ops, t, i)
	}
	return ops
}

// Advance returns the advance of glyph i of the run t in points, scaled
// as the run draws it, 0 for a face without outlines.
func (o *Outlines) Advance(t *typeset.Text, i int) float64 {
	f := o.Face(t.Face)
	if f == nil || i < 0 || i >= len(t.Glyphs) {
		return 0
	}
	adv := float64(f.HorizontalAdvance(font.GID(t.Glyphs[i].GID)))
	return float64(adv*t.Size) / float64(f.Upem()) * hScale(t)
}

// hScale returns the horizontal scale t draws at, 1 for none.
func hScale(t *typeset.Text) float64 {
	if t.HScale <= 0 {
		return 1
	}
	return t.HScale
}

// AppendGlyphOutline appends to ops the outline of glyph gid of f, drawn
// at k points per font unit with its origin at x, y (y downward), scaled
// horizontally by hs and slanted by skew points right per point up.
// Quadratic segments become the cubic Béziers they equal; each contour is
// closed.
func AppendGlyphOutline(ops []typeset.PathOp, f *font.Face, gid uint16, x, y, k, hs, skew float64) []typeset.PathOp {
	data, ok := f.GlyphData(font.GID(gid)).(font.GlyphOutline)
	if !ok {
		return ops
	}
	pt := func(s opentype.SegmentPoint) typeset.Point {
		up := float64(float64(s.Y) * k)
		return typeset.Point{X: x + float64(hs*float64(s.X)*k) + float64(skew*up), Y: y - up}
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
