package page

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/glyph"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/shade"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
)

// renderer carries one render.
type renderer struct {
	ctx  context.Context //nolint:containedctx // one render's context, never kept past Render
	doc  *document.Document
	page *document.Page
	// width and height are the device raster's size: the page's, or a
	// pattern tile's in the renderer its cells run in.
	width, height int
	device        geom.Matrix // the page's default user space to device pixels
	budget        int64       // render pixel budget
	clipLimit     int64       // pixels the live clip masks may hold
	report        *Report

	m *graphics.Machine
	// clips is the clip of each graphics state of m.Stack, index for index.
	clips []*clip
	// ops counts the operators run so far (maxRenderOperators).
	ops int64
	// chain holds the object numbers of the forms being painted, outermost
	// first, so a form that paints itself is caught.
	chain []int
	// depth is how many forms enclose the stream being run.
	depth int

	// groups is the stack of transparency groups being painted, the page
	// group at the bottom; every painting goes into the top one.
	groups []frame
	// edition is the edition of ISO 32000 the blend modes follow.
	edition blend.Edition
	// offscreen counts the pixels of the rasters besides the page, shared
	// with the renderers of pattern cells and soft masks.
	offscreen *offscreen
	// masks keeps the soft masks rendered in this renderer's device space.
	masks *maskCache
	// normalForms keeps, per form, whether its content blends with Normal
	// only ([renderer.normalOnly]).
	normalForms map[int]bool
	// mergedResources keeps, per form, its /Resources
	// with the page's names it lacks ([renderer.withPageNames]).
	mergedResources map[ast.PdfIndirectRef]ast.PdfDict

	shadings map[ast.PdfIndirectRef]*shade.Shading
	patterns map[ast.PdfIndirectRef]*shade.Pattern

	// colorCache keeps the colour converters of the render's page. It is a
	// pointer so the renderers a tiling pattern's cells run in share it, and
	// copying a renderer copies no lock.
	colorCache *colorconv.Cache
	// transfers keeps the transfer functions compiled so far, by the value
	// /TR or /TR2 states (maxTransferCache).
	transfers map[string]*transfer
	// fonts loads the fonts text is shown in; glyphs and type3 keep the
	// glyphs resolved and the Type 3 glyph descriptions parsed so far, and
	// type3Depth counts the descriptions being run.
	fonts      *glyph.Cache
	glyphs     map[glyphKey]glyph.Glyph
	type3      map[glyphKey][]content.Operation
	type3Depth int
	// tiling holds the render's tiling patterns and tiles, made on first use.
	tiling *tilingState
	// omit is the content left out ([Options.Omit]). It is cleared where a
	// kind's own content runs: a Type 3 glyph description, a pattern cell, a
	// soft mask's group.
	omit Content
}

// omits reports whether the render leaves out content of kind k.
func (r *renderer) omits(k Content) bool {
	return r.omit&k != 0
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

	// text is the text object being shown, which a Type 3 glyph
	// description, a stream of its own, does not share.
	text textObject
}

// run executes ops, the content of one stream.
func (r *renderer) run(ops []content.Operation, resources ast.PdfDict, base geom.Matrix, depth int) error {
	prevRes, prevOC := r.m.Resources, r.m.ActiveOCs
	r.m.Resources = resources
	r.m.ActiveOCs = activeOCs(r.ctx, r.doc, resources)
	defer func() { r.m.Resources, r.m.ActiveOCs = prevRes, prevOC }()

	prevDepth := r.depth
	r.depth = depth
	defer func() { r.depth = prevDepth }()

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
	// A text object the stream leaves open paints its knocked-out glyphs.
	return r.flushText(s)
}

// exec runs one operator. It returns only an error that ends the render.
func (r *renderer) exec(s *scope, op content.Operation) error {
	if len(s.text.knock) > 0 {
		switch op.Operator {
		case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "Do", "BI", "sh", "BT":
			// What a text object holds back for its knockout paints before
			// anything painted after it (9.3.8).
			if err := r.flushText(s); err != nil {
				return err
			}
		}
	}
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
	case "Tj", "TJ", "'", "\"":
		return r.showText(s, op)
	case "BT":
		s.beginText()
	case "ET":
		if err := r.endText(s); err != nil {
			return err
		}
	case "Q":
		if len(r.m.Stack) <= s.floor {
			return nil
		}
	case "cs", "CS":
		return r.selectSpace(op)
	case "sc", "SC", "scn", "SCN":
		if !r.fitsSpace(op) {
			return nil
		}
	}
	if err := r.m.ExecuteOne(r.ctx, op); err != nil {
		return fmt.Errorf("executing %s: %w", op.Operator, err)
	}
	r.syncClips()
	return nil
}

// selectSpace runs cs or CS, leaving the current colour space and colour as
// they were when the operand names a colour space the resources do not
// define.
//
// ISO 32000-1, 8.6.8, Table 74 requires the operand to name a colour space
// family or an entry of the /ColorSpace subdictionary of the resources, and
// says nothing of one that names neither. The graphics machine selects an
// unresolved space, which compliance judges as such, and the renderer painted
// it black. Deliberate leniency toward that construct, as the renderers draw
// it: poppler 26.09.0 (Gfx::opSetFillColorSpace reports "Bad color space" and
// changes nothing) and PDFium (pypdfium2 4.30.0) keep the space and colour in
// force, so a later sc or scn reads its components in it. PDF.js
// (ColorSpaceUtils.parse, "Unrecognized ColorSpace") selects DeviceGray
// instead, which agrees with them while DeviceGray is in force.
//
// # Corpus evidence
//
//   - litho resinherit_2b.pdf page 2: /CS4 cs, which the page's own
//     /Resources do not define (7.7.3.4), then 1 scn. poppler fills it white
//     in the initial DeviceGray. Read without the resources of an ancestor,
//     litho filled it black.
//   - A crafted page (TestUndefinedColourSpaceKeepsTheCurrentOne): red in
//     DeviceRGB, an undefined cs, then 0 0 1 sc fills blue in poppler and
//     PDFium, black in PDF.js.
func (r *renderer) selectSpace(op content.Operation) error {
	st := r.m.Current()
	fill, stroke := st.Fill, st.Stroke
	if err := r.m.ExecuteOne(r.ctx, op); err != nil {
		return fmt.Errorf("executing %s: %w", op.Operator, err)
	}
	st = r.m.Current()
	switch {
	case op.Operator == "cs" && st.Fill.Space.Family == "":
		r.skip("colour space", fmt.Errorf("cs names %q, which no resource defines", st.Fill.Space.Resource))
		st.Fill = fill
	case op.Operator == "CS" && st.Stroke.Space.Family == "":
		r.skip("colour space", fmt.Errorf("CS names %q, which no resource defines", st.Stroke.Space.Resource))
		st.Stroke = stroke
	}
	return nil
}

// fitsSpace reports whether the operands of sc, SC, scn or SCN are as many
// numbers as the colour space in force has components, or the space is a
// Pattern space or one the machine could not resolve, which the paint itself
// judges.
//
// ISO 32000-1, 8.6.8, Table 74 makes the number of operands depend on the
// current colour space, and says nothing of a colour with another count. The
// renderer leaves the colour in force then, as poppler 26.09.0 draws it
// (Gfx::opSetFillColor and its siblings report "Incorrect number of
// arguments" and change nothing). PDFium (pypdfium2 4.30.0) agrees on too few
// operands and reads the leading ones of too many.
//
// # Corpus evidence
//
//   - litho csnames_2b.pdf page 3: /CS2 names /CS0, which no reader takes as
//     a colour space, so DeviceGray stays in force, and 1 0 0 sc then fills
//     black in poppler.
func (r *renderer) fitsSpace(op content.Operation) bool {
	st := r.m.Current()
	space := st.Fill.Space
	if op.Operator == "SC" || op.Operator == "SCN" {
		space = st.Stroke.Space
	}
	if space.Family == "" || space.Family == graphics.FamilyPattern || space.Components == 0 {
		return true
	}
	n := 0
	for _, v := range op.Operands {
		if _, ok := document.Number(v); ok {
			n++
		}
	}
	if n == space.Components {
		return true
	}
	r.doc.Logger.Debug("render: colour operands do not fit the colour space; keeping the colour",
		slog.String("operator", op.Operator), slog.Int("operands", n), slog.Int("components", space.Components))
	return false
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
	if int64(live)*int64(r.width)*int64(r.height) > r.clipLimit {
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
	if !st.SkipExtraction && !s.path.Empty() && !r.omits(Paths) {
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
	c, err := r.clip().withPath(s.path, s.clipRule, r.width, r.height, r.budget)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("clipping path", err)
		return nil
	}
	return r.setClip(c)
}

// fill fills the current path with rule in the fill colour (ISO 32000-1,
// 8.5.3.3).
func (r *renderer) fill(s *scope, rule raster.FillRule, st *graphics.State) error {
	path := s.path
	if adj, ok := adjustedRect(path); ok {
		path = adj
	}
	cov, err := r.coverage(path, rule)
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
	cov, err := raster.Fill(p, rule, r.width, r.height, c.mask)
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
	cov, err := r.coverage(adjustOutline(outline, s.path, style, m), raster.NonZero)
	if err != nil {
		r.skip("stroke", err)
		return nil
	}
	return r.paintCoverage(s, cov, st.Stroke, st.StrokeAlpha)
}

// paintCoverage paints cov in colour c at constant alpha a: a solid colour,
// or a pattern (ISO 32000-1, 8.7.3.1), through the current group with the
// effects of the graphics state.
func (r *renderer) paintCoverage(s *scope, cov *raster.Coverage, c graphics.Color, a float64) error {
	if cov == nil || cov.Empty() {
		return nil
	}
	if c.Space.Family == graphics.FamilyPattern {
		return r.patternFill(s, cov, c, a)
	}
	conv, paints := r.colorFor(s, c.Space)
	if !paints {
		return nil
	}
	cr, cg, cb := conv.RGB(c.Components)
	return r.paintSolid(cov, [3]float32{cr, cg, cb}, r.effectsOf(r.m.Current(), a))
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
