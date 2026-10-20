package page

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/shade"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// renderer carries one render.
type renderer struct {
	ctx       context.Context //nolint:containedctx // one render's context, never kept past Render
	doc       *document.Document
	page      *document.Page
	canvas    *surface.Canvas
	device    geom.Matrix // the page's default user space to device pixels
	budget    int64       // render pixel budget
	clipLimit int64       // pixels the live clip masks may hold
	colors    ColorProvider
	report    *Report

	m *graphics.Machine
	// clips is the clip of each graphics state of m.Stack, index for index.
	clips []*clip
	// ops counts the operators run so far (maxRenderOperators).
	ops int64
	// chain holds the object numbers of the forms being painted, outermost
	// first, so a form that paints itself is caught.
	chain []int

	shadings map[ast.PdfIndirectRef]*shade.Shading
	patterns map[ast.PdfIndirectRef]*shade.Pattern
}

// scope is one content stream being run: the page's or a form's.
type scope struct {
	resources ast.PdfDict
	// base maps the stream's default coordinate space to device space, the
	// space a pattern's matrix maps into (ISO 32000-1, 8.7.3.1).
	base geom.Matrix
	// depth is how many forms enclose the stream.
	depth int
	// floor is the depth of m.Stack when the stream began, below which a Q
	// does not restore: a form's Q cannot undo the state of its parent.
	floor int

	path     geom.Path // the current path, in device space
	start    geom.Point
	cur      geom.Point
	hasCur   bool
	clipRule raster.FillRule
	clipSet  bool
}

// run executes ops, the content of one stream.
func (r *renderer) run(ops []content.Operation, resources ast.PdfDict, base geom.Matrix, depth int) error {
	prevRes, prevOC := r.m.Resources, r.m.ActiveOCs
	r.m.Resources = resources
	r.m.ActiveOCs = activeOCs(r.ctx, r.doc, resources)
	defer func() { r.m.Resources, r.m.ActiveOCs = prevRes, prevOC }()

	s := &scope{resources: resources, base: base, depth: depth, floor: len(r.m.Stack)}
	for _, op := range ops {
		r.ops++
		if r.ops > maxRenderOperators {
			return fmt.Errorf("%w: more than %d operators", ErrTooLarge, maxRenderOperators)
		}
		if r.ops%opsPerContextCheck == 0 {
			if err := r.ctx.Err(); err != nil {
				return fmt.Errorf("rendering: %w", err)
			}
		}
		if err := r.exec(s, op); err != nil {
			return err
		}
	}
	return nil
}

// exec runs one operator. It returns only an error that ends the render.
func (r *renderer) exec(s *scope, op content.Operation) error {
	switch op.Operator {
	case "m", "l", "c", "v", "y", "h", "re":
		r.construct(s, op)
		return nil
	case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "n":
		return r.paintPath(s, op.Operator)
	case "W":
		s.clipRule, s.clipSet = raster.NonZero, true
		return nil
	case "W*":
		s.clipRule, s.clipSet = raster.EvenOdd, true
		return nil
	case "Do":
		return r.do(s, op)
	case "BI":
		return r.inlineImage(s, op)
	case "sh":
		return r.shading(s, op)
	case "Tj", "TJ":
		r.report.count(UnsupportedText)
		return nil
	case "'", "\"":
		r.report.count(UnsupportedText)
	case "Q":
		if len(r.m.Stack) <= s.floor {
			return nil
		}
	}
	if err := r.m.ExecuteOne(r.ctx, op); err != nil {
		return fmt.Errorf("executing %s: %w", op.Operator, err)
	}
	r.syncClips()
	return nil
}

// syncClips keeps r.clips as long as the machine's stack: a q copies the
// clip of the state it saves, a Q drops the clip of the state it discards
// (ISO 32000-1, 8.4.2).
func (r *renderer) syncClips() {
	n := len(r.m.Stack)
	for len(r.clips) < n {
		r.clips = append(r.clips, r.clips[len(r.clips)-1])
	}
	if len(r.clips) > n {
		clear(r.clips[n:])
		r.clips = r.clips[:n]
	}
}

// clip returns the clip of the current graphics state.
func (r *renderer) clip() *clip {
	return r.clips[len(r.clips)-1]
}

// setClip installs c as the clip of the current graphics state, refusing it
// when the masks the render keeps alive pass their budget.
func (r *renderer) setClip(c *clip) error {
	r.clips[len(r.clips)-1] = c
	live := 0
	for i, cl := range r.clips {
		var parent *clip
		if i > 0 {
			parent = r.clips[i-1]
		}
		live += cl.owns(parent)
	}
	if int64(live)*int64(r.canvas.Width)*int64(r.canvas.Height) > r.clipLimit {
		return fmt.Errorf("%w: %d live clip masks exceed %d times the render pixel budget", ErrTooLarge, live, clipBudgetShare)
	}
	return nil
}

// toDevice returns the matrix from the current user space to device space.
func (r *renderer) toDevice() geom.Matrix {
	return geom.Matrix(r.m.Current().CTM).Mul(r.device)
}

// numbers reads n numeric operands, false when op holds another count or a
// value that is no number. A viewer skips such an operator (poppler's Gfx
// reports "wrong number of args" and goes on).
func numbers(op content.Operation, n int) ([]float64, bool) {
	if len(op.Operands) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, v := range op.Operands {
		f, ok := document.Number(v)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// construct runs a path construction operator (ISO 32000-1, 8.5.2.1, Table
// 59), building the path in device space through the CTM, which cannot change
// inside a path object (8.2, Figure 9).
func (r *renderer) construct(s *scope, op content.Operation) {
	m := r.toDevice()
	pt := func(v []float64, i int) geom.Point { return m.Apply(geom.Point{X: v[i], Y: v[i+1]}) }
	switch op.Operator {
	case "m":
		if v, ok := numbers(op, 2); ok {
			p := pt(v, 0)
			s.path.MoveTo(p)
			s.start, s.cur, s.hasCur = p, p, true
		}
	case "l":
		if v, ok := numbers(op, 2); ok && s.hasCur {
			s.cur = pt(v, 0)
			s.path.LineTo(s.cur)
		}
	case "c":
		if v, ok := numbers(op, 6); ok && s.hasCur {
			s.cur = pt(v, 4)
			s.path.CubicTo(pt(v, 0), pt(v, 2), s.cur)
		}
	case "v":
		// The first control point is the current point.
		if v, ok := numbers(op, 4); ok && s.hasCur {
			c0 := s.cur
			s.cur = pt(v, 2)
			s.path.CubicTo(c0, pt(v, 0), s.cur)
		}
	case "y":
		// The second control point is the end point.
		if v, ok := numbers(op, 4); ok && s.hasCur {
			s.cur = pt(v, 2)
			s.path.CubicTo(pt(v, 0), s.cur, s.cur)
		}
	case "h":
		if s.hasCur {
			s.path.Close()
			s.cur = s.start
		}
	case "re":
		// Table 59: x y m, x+w y l, x+w y+h l, x y+h l, h.
		if v, ok := numbers(op, 4); ok {
			x, y, w, h := v[0], v[1], v[2], v[3]
			p := m.Apply(geom.Point{X: x, Y: y})
			s.path.MoveTo(p)
			s.path.LineTo(m.Apply(geom.Point{X: x + w, Y: y}))
			s.path.LineTo(m.Apply(geom.Point{X: x + w, Y: y + h}))
			s.path.LineTo(m.Apply(geom.Point{X: x, Y: y + h}))
			s.path.Close()
			s.start, s.cur, s.hasCur = p, p, true
		}
	}
}

// paintPath runs a path-painting operator (ISO 32000-1, 8.5.3.1, Table 60),
// then applies a pending W or W*, which takes effect after the painting
// (8.5.4), and ends the path object.
func (r *renderer) paintPath(s *scope, operator string) error {
	defer func() {
		s.path = geom.Path{}
		s.hasCur, s.clipSet = false, false
	}()
	switch operator {
	case "s", "b", "b*":
		if s.hasCur {
			s.path.Close()
		}
	}
	st := r.m.Current()
	if !st.SkipExtraction && !s.path.Empty() {
		var err error
		switch operator {
		case "f", "F", "B", "b":
			err = r.fill(s, raster.NonZero, st)
		case "f*", "B*", "b*":
			err = r.fill(s, raster.EvenOdd, st)
		}
		if err != nil {
			return err
		}
		switch operator {
		case "S", "s", "B", "B*", "b", "b*":
			if err := r.stroke(s, st); err != nil {
				return err
			}
		}
	}
	if !s.clipSet {
		return nil
	}
	c, err := r.clip().withPath(s.path, s.clipRule, r.canvas.Width, r.canvas.Height, r.budget)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("clipping path", err)
		return nil
	}
	return r.setClip(c)
}

// noteEffects counts the transparency parameters of st the renderer does not
// apply yet.
func (r *renderer) noteEffects(st *graphics.State) {
	if st.SoftMask != nil {
		r.report.count(UnsupportedSoftMask)
	}
	if st.BlendMode != "" && st.BlendMode != "Normal" && st.BlendMode != "Compatible" {
		r.report.count(UnsupportedBlendMode)
	}
}

// fill fills the current path with rule in the fill colour (ISO 32000-1,
// 8.5.3.3).
func (r *renderer) fill(s *scope, rule raster.FillRule, st *graphics.State) error {
	r.noteEffects(st)
	cov, err := r.coverage(s.path, rule)
	if err != nil {
		r.skip("fill", err)
		return nil
	}
	return r.paintCoverage(s, cov, st.Fill, st.FillAlpha)
}

// coverage rasterises the device path p under the current clip.
func (r *renderer) coverage(p geom.Path, rule raster.FillRule) (*raster.Coverage, error) {
	c := r.clip()
	if c.empty() {
		return &raster.Coverage{}, nil
	}
	if b, ok := p.Bounds(); !ok || b.Max.X <= c.rect.Min.X || b.Min.X >= c.rect.Max.X || b.Max.Y <= c.rect.Min.Y || b.Min.Y >= c.rect.Max.Y {
		return &raster.Coverage{}, nil
	}
	cov, err := raster.Fill(p, rule, r.canvas.Width, r.canvas.Height, c.mask)
	if err != nil {
		return nil, fmt.Errorf("rasterising: %w", err)
	}
	c.applyRect(cov)
	return cov, nil
}

// stroke strokes the current path in the stroke colour (ISO 32000-1,
// 8.5.3.2). The path is mapped back to user space so the pen is the line
// width in user space; a CTM that cannot be inverted strokes nothing.
func (r *renderer) stroke(s *scope, st *graphics.State) error {
	r.noteEffects(st)
	m := r.toDevice()
	inv, ok := m.Invert()
	if !ok {
		return nil
	}
	style := stroke.Style{
		Width:      st.LineWidth,
		Cap:        stroke.Cap(st.LineCap),
		Join:       stroke.Join(st.LineJoin),
		MiterLimit: st.MiterLimit,
		Dash:       st.Dash.Array,
		DashPhase:  st.Dash.Phase,
	}
	outline, err := stroke.Outline(s.path.Transform(inv), style, m, 0)
	if err != nil {
		r.skip("stroke", err)
		return nil
	}
	cov, err := r.coverage(outline, raster.NonZero)
	if err != nil {
		r.skip("stroke", err)
		return nil
	}
	return r.paintCoverage(s, cov, st.Stroke, st.StrokeAlpha)
}

// alpha32 clamps a constant alpha to [0, 1].
func alpha32(a float64) float32 {
	switch {
	case !(a > 0):
		return 0
	case a >= 1:
		return 1
	}
	return float32(a)
}

// paintCoverage paints cov in colour c at constant alpha a: a solid colour,
// or a shading pattern (ISO 32000-1, 8.7.3.1).
func (r *renderer) paintCoverage(s *scope, cov *raster.Coverage, c graphics.Color, a float64) error {
	if cov == nil || cov.Empty() {
		return nil
	}
	if c.Space.Family == graphics.FamilyPattern {
		return r.patternFill(s, cov, c, a)
	}
	conv := r.converter(s, c.Space)
	cr, cg, cb := conv.RGB(c.Components)
	raster.PaintSolid(r.canvas, cov, cr, cg, cb, alpha32(a))
	return nil
}

// converter returns the converter for space, the stand-in grey when the
// provider fails.
func (r *renderer) converter(s *scope, space graphics.ColorSpace) Converter {
	conv, err := r.colors.For(r.ctx, r.doc, space, s.resources)
	if err != nil {
		r.skip("colour space", err)
		return greyConverter{}
	}
	return conv
}

// components resolves the component count of a shading's colour space.
func (r *renderer) components(ctx context.Context, cs ast.PdfObject) (int, error) {
	space, err := graphics.DescribeColorSpace(ctx, r.doc, cs)
	if err != nil {
		return 0, err
	}
	if space.Components < 1 {
		return 0, fmt.Errorf("colour space %s has no components", space.Family)
	}
	return space.Components, nil
}
