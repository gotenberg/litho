package compose

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// realLimit is the largest magnitude of a real number a PDF 1.4 reader
// holds (PDF Reference 1.4, Appendix C, Table C.1), which PDF/A-1 makes a
// rule (ISO 19005-1, 6.1.12). Later versions raise it to the range of a
// single-precision float (ISO 32000-1, Annex C.2, Table C.1), which no
// coordinate of a page reaches.
const realLimit = 32767

// maxFitShift bounds the power of two [fitScale] divides by: 2^30 leaves
// every coordinate under about 3.5e13 points, 12 thousand kilometres,
// within [realLimit], and writes the scale as an integer PDF 1.4 holds
// (Table C.1 bounds integers at 2^31 - 1). A larger coordinate is written
// as it is.
const maxFitShift = 30

// limited reports whether the file's version bounds real numbers at
// [realLimit].
func (p *pageWriter) limited() bool {
	return p.w.version == Version14
}

// fitScale returns the power of two k that brings a magnitude of extent
// within [realLimit] once divided by it: 1 when it already fits or the
// version sets no such limit, and 1 too when no k up to 2^[maxFitShift]
// does, the numbers then written as they are.
//
// Dividing by a power of two and multiplying the CTM back by it is exact
// in binary floating point, so what a scaled fragment paints is where its
// unscaled numbers would put it, to the precision every number is written
// at.
func (p *pageWriter) fitScale(extent float64) float64 {
	if !p.limited() || round(extent) <= realLimit {
		return 1
	}
	for shift := 1; shift <= maxFitShift; shift++ {
		k := math.Ldexp(1, shift)
		if round(extent/k) <= realLimit {
			return k
		}
	}
	p.w.logger.Debug("compose: coordinate past the PDF 1.4 real range written as it is", "page", p.index+1, "extent", extent)
	return 1
}

// fitted writes the fragment draw paints under q, a uniform scale by k as
// cm, and Q, opening the fragment's marked-content sequence for parent
// before q so that no sequence straddles the pair. draw writes the copy
// of the fragment whose numbers are divided by k, the folded translation
// included, so it runs with no translation folded.
func (p *pageWriter) fitted(k float64, parent *StructElem, draw func() error) error {
	p.mark(parent)
	fmt.Fprintf(&p.buf, "q %s 0 0 %s 0 0 cm\n", writer.FormatReal(k), writer.FormatReal(k))
	p.alpha = append(p.alpha, p.currentAlpha())
	saved := p.off
	p.off = Point{}
	err := draw()
	p.off = saved
	p.buf.WriteString("Q\n")
	p.alpha = p.alpha[:len(p.alpha)-1]
	return err
}

// textExtent returns the largest magnitude of the numbers the text object
// of t writes, the folded translation off included.
func textExtent(t *Text, off Point) float64 {
	hs := 1.0
	if t.HScale > 0 {
		hs = t.HScale
	}
	m := max(math.Abs(t.Size), math.Abs(t.Stroke))
	for _, g := range t.Glyphs {
		m = max(m, math.Abs(t.Origin.X+g.X*hs+off.X), math.Abs(t.Origin.Y+g.Y+off.Y))
	}
	return m
}

// scaledText returns a copy of t moved by off and scaled by 1/k about the
// origin: its origin, glyph offsets, size and stroke width.
func scaledText(t *Text, off Point, k float64) *Text {
	c := *t
	c.Origin = Point{X: (t.Origin.X + off.X) / k, Y: (t.Origin.Y + off.Y) / k}
	c.Size = t.Size / k
	c.Stroke = t.Stroke / k
	c.Glyphs = make([]Glyph, len(t.Glyphs))
	for i, g := range t.Glyphs {
		g.X /= k
		g.Y /= k
		c.Glyphs[i] = g
	}
	return &c
}

// imageExtent returns the largest magnitude of the numbers the matrix of
// img writes, the folded translation off included.
func imageExtent(img *Image, off Point) float64 {
	r := img.Rect
	return max(math.Abs(r.Width), math.Abs(r.Height), math.Abs(r.X+off.X), math.Abs(r.Y+r.Height+off.Y))
}

// scaledImage returns a copy of img moved by off and scaled by 1/k.
func scaledImage(img *Image, off Point, k float64) *Image {
	c := *img
	r := img.Rect
	c.Rect = Rect{X: (r.X + off.X) / k, Y: (r.Y + off.Y) / k, Width: r.Width / k, Height: r.Height / k}
	return &c
}

// opsExtent returns the largest magnitude of the points of ops moved by
// off.
func opsExtent(ops []PathOp, off Point) float64 {
	var m float64
	for _, op := range ops {
		for _, pt := range op.P[:opPoints(op.Kind)] {
			m = max(m, math.Abs(pt.X+off.X), math.Abs(pt.Y+off.Y))
		}
	}
	return m
}

// opPoints returns how many points an operator of kind k carries.
func opPoints(k OpKind) int {
	switch k {
	case MoveTo, LineTo:
		return 1
	case CurveTo:
		return 3
	case ClosePath:
	}
	return 0
}

// scaledOps returns ops moved by off and scaled by 1/k.
func scaledOps(ops []PathOp, off Point, k float64) []PathOp {
	out := make([]PathOp, len(ops))
	for i, op := range ops {
		for j := range opPoints(op.Kind) {
			op.P[j] = Point{X: (op.P[j].X + off.X) / k, Y: (op.P[j].Y + off.Y) / k}
		}
		out[i] = op
	}
	return out
}

// pathExtent returns the largest magnitude of the numbers the path object
// of pa writes, the folded translation off included: its points, its line
// parameters, and the matrix that places its shading.
func pathExtent(pa *Path, off Point) float64 {
	m := opsExtent(pa.Ops, off)
	if s := pa.Stroke; s != nil {
		m = max(m, math.Abs(s.Width), math.Abs(s.DashPhase))
		for _, d := range s.Dash {
			m = max(m, math.Abs(d))
		}
	}
	if pa.Fill != nil && pa.Fill.Shading != nil {
		sh, sm := placedShading(pa.Fill.Shading, off)
		m = max(m, math.Abs(sm.A), math.Abs(sm.B), math.Abs(sm.C), math.Abs(sm.D), math.Abs(sm.E), math.Abs(sm.F))
		if sm == Identity {
			m = max(m, math.Abs(sh.From.X), math.Abs(sh.From.Y), math.Abs(sh.To.X), math.Abs(sh.To.Y), math.Abs(sh.FromRadius), math.Abs(sh.ToRadius))
		}
	}
	return m
}

// scaledPath returns a copy of pa moved by off and scaled by 1/k: its
// points, its line width and dash pattern, and its shading: the matrix of
// one with a transform, the coordinates of one without.
func scaledPath(pa *Path, off Point, k float64) *Path {
	c := *pa
	c.Ops = scaledOps(pa.Ops, off, k)
	if pa.Stroke != nil {
		s := *pa.Stroke
		s.Width /= k
		s.DashPhase /= k
		s.Dash = make([]float64, len(pa.Stroke.Dash))
		for i, d := range pa.Stroke.Dash {
			s.Dash[i] = d / k
		}
		c.Stroke = &s
	}
	if pa.Fill != nil && pa.Fill.Shading != nil {
		f := *pa.Fill
		placed, m := placedShading(pa.Fill.Shading, off)
		sh := *placed
		if m == Identity {
			// The coordinates are the page's: they scale with the path.
			sh.From = Point{X: sh.From.X / k, Y: sh.From.Y / k}
			sh.To = Point{X: sh.To.X / k, Y: sh.To.Y / k}
			sh.FromRadius /= k
			sh.ToRadius /= k
		} else {
			sh.Transform = Matrix{A: m.A / k, B: m.B / k, C: m.C / k, D: m.D / k, E: m.E / k, F: m.F / k}
		}
		f.Shading = &sh
		c.Fill = &f
	}
	return &c
}

// clip writes the clipping path of a group, W n or W* n (ISO 32000-1,
// 8.5.4), its points moved by the folded translation. A clip whose numbers
// pass [realLimit] at PDF 1.4 is written under a scale by k, which the
// CTM drops right after the clip with a scale by 1/k: both are powers of
// two, so the CTM the content paints under is the group's, exactly.
func (p *pageWriter) clip(ctx context.Context, c *Path) error {
	op := "W n\n"
	if c.Fill != nil && c.Fill.Rule == EvenOdd {
		op = "W* n\n"
	}
	k := 1.0
	if p.limited() {
		k = p.fitScale(opsExtent(c.Ops, p.off))
	}
	if k == 1 {
		if err := p.ops(ctx, c.Ops); err != nil {
			return err
		}
		p.buf.WriteString(op)
		return nil
	}
	fmt.Fprintf(&p.buf, "%s 0 0 %s 0 0 cm\n", writer.FormatReal(k), writer.FormatReal(k))
	saved := p.off
	p.off = Point{}
	err := p.ops(ctx, scaledOps(c.Ops, saved, k))
	p.off = saved
	if err != nil {
		return err
	}
	p.buf.WriteString(op)
	fmt.Fprintf(&p.buf, "%s 0 0 %s 0 0 cm\n", writer.FormatReal(1/k), writer.FormatReal(1/k))
	return nil
}
