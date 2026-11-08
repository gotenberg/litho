package slides

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// effects returns what paints under a KindGeometry shape for its effects:
// its shadow, as a copy of the filled paths of paths in the shadow's
// colour and opacity, scaled about box's centre and moved by the offsets
// (ECMA-376 Part 1, 20.1.8.49 outerShdw; ODF 1.3 Part 3, 20.158
// draw:shadow). A blurred shadow prints hard, a glow and a soft edge
// print nothing, each with a warning: the composition model draws no
// blur.
func (b *builder) effects(s *Shape, paths []shape.Path, box compose.Rect) []compose.Fragment {
	if s.Glow != nil && s.Glow.Radius > 0 && s.Glow.Opacity > 0 {
		b.warn(CodeEffect, "the glow of "+quote(s.Name)+" is not drawn")
	}
	if s.SoftEdge > 0 {
		b.warn(CodeEffect, "the soft edge of "+quote(s.Name)+" is not drawn")
	}
	sh := s.Shadow
	if sh == nil || sh.Opacity <= 0 || !finite(box) {
		return nil
	}
	if sh.Blur > 0 {
		b.warn(CodeEffect, "the shadow of "+quote(s.Name)+" is drawn without its blur")
	}
	c := colorOf(sh.Color)
	c.A *= sh.Opacity
	paint := shape.Paint{Kind: shape.PaintSolid, Color: c}
	var frags []compose.Fragment
	for _, p := range paths {
		if p.Fill {
			frags = append(frags, shape.Fill(p.Ops, box, paint)...)
		}
	}
	if len(frags) == 0 {
		return nil
	}
	sx, sy := sh.ScaleX, sh.ScaleY
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	cx, cy := box.X+box.Width/2, box.Y+box.Height/2
	m := then(translate(-cx, -cy), compose.Matrix{A: sx, D: sy})
	m = then(m, translate(cx+sh.OffsetX, cy+sh.OffsetY))
	return group(m, frags)
}
