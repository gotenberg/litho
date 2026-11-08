package ods

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// The colours of LibreOffice's drawing item pool, which a shape takes when
// no style, not even the document's default graphic style, sets one.
var (
	poolFill   = odf.Color{R: 0x72, G: 0x9f, B: 0xcf}
	poolStroke = odf.Color{R: 0x34, G: 0x65, B: 0xa4}
)

// The dimension line defaults of LibreOffice's drawing item pool, in
// points: the dimension line 0.8 cm from the measured points, the guides
// starting 1 mm from them and passing the dimension line by 2 mm, the
// arrowheads 2 mm wide (ODF 1.3, Part 3, 20.98 draw:line-distance, 20.90
// draw:guide-overhang, 20.89 draw:guide-distance).
const (
	measureLineDistance  = 0.8 / 2.54 * 72
	measureGuideOverhang = 0.2 / 2.54 * 72
	measureGuideDistance = 0.1 / 2.54 * 72
	measureArrowWidth    = 0.2 / 2.54 * 72
)

// shapeElement reports a drawing shape of ODF 1.3, Part 3, 10.3 and 10.6
// that [reader.shape] draws.
func shapeElement(el *odf.Element) bool {
	if el.Name.Space != odf.NSDraw {
		return false
	}
	switch el.Name.Local {
	case "custom-shape", "rect", "ellipse", "circle", "line", "polyline", "polygon", "path", "regular-polygon", "measure", "connector":
		return true
	}
	return false
}

// shapeArt is what a shape paints, in sheet coordinates: draw returns
// its fragments, and bounds the box they cover.
type shapeArt struct {
	draw   func(ctx context.Context) []compose.Fragment
	bounds compose.Rect
	// logic, when set, is the shape's unturned box, which the anchors of
	// a shape resized with its cells place (ODF 1.3, Part 3, 19.661
	// table:end-cell-address); nil for a line or a dimension line, whose
	// ends place it.
	logic *logicBox
}

// logicBox is a shape's unturned box: its corner (x, y) on the sheet, its
// size w by h, the linear part lin of its transform about that corner,
// and what local draws in the box (0, 0, w, h).
type logicBox struct {
	x, y, w, h float64
	lin        compose.Matrix
	local      func(ctx context.Context, box compose.Rect) []compose.Fragment
}

// shape reads a drawing shape (ODF 1.3, Part 3, 10.3 and 10.6) into a
// drawing whose picture paints its geometry with its fill and stroke, under
// its draw:transform, as Calc's drawing layer prints it; at is its anchor
// cell, nil for a shape of the sheet. The text of a shape is not drawn.
func (r *reader) shape(ts *tableState, el *odf.Element, at *sheet.Anchor) {
	if at != nil && (ts.rowHidden || ts.colHidden(at.Column)) {
		// Calc hides a shape whose anchor cell is in a hidden row or
		// column (sc/legacycellanchoredrotatedhiddenshape.ods).
		return
	}
	p := r.styles.Resolve(odf.ScopeContent, odf.FamilyGraphic, el.A(odf.NSDraw, "style-name"), odf.GraphicProps)
	paint := r.shapePaint(p)
	line := r.shapeLine(p)
	// Calc writes the position of a shape anchored to a cell from the
	// cell's corner (measured on the pinned LibreOffice:
	// sc/LargeAnchorOffset.ods prints its dimension line from the left
	// edge of its anchor column E, sc/hideColsShow.ods its shape 15 mm
	// right of its anchor column C).
	dx, dy := 0.0, 0.0
	if at != nil {
		dx, dy = ts.colLeft(at.Column), ts.rowY
	}
	var art shapeArt
	var ok bool
	switch el.Name.Local {
	case "line", "connector":
		art, ok = lineArt(el, line, dx, dy)
	case "measure":
		art, ok = measureArt(el, line, p, dx, dy)
	default:
		art, ok = r.geometryArt(el, paint, line, dx, dy)
	}
	if !ok {
		return
	}
	if hasText(el) && el.Name.Local != "measure" {
		r.warn("shape-text", "the text of drawing shapes is not drawn")
	}
	r.place(ts, el, at, art)
}

// hasText reports a shape with text in its paragraphs.
func hasText(el *odf.Element) bool {
	for _, c := range el.Children {
		if c.Is(odf.NSText, "p") && strings.TrimSpace(c.TextContent()) != "" {
			return true
		}
	}
	return false
}

// place adds the drawing of art to the sheet: anchored to the cell at,
// the offset from its corner kept, and its bottom-right corner to the
// cell table:end-cell-address names when the shape gives one, as Calc
// resizes a shape with its cells; from the sheet's origin when at is nil.
func (r *reader) place(ts *tableState, el *odf.Element, at *sheet.Anchor, art shapeArt) {
	if r.drawings >= maxDrawings {
		r.warn("too-many-drawings", fmt.Sprintf("pictures and charts past %d are not drawn", maxDrawings))
		return
	}
	b := art.bounds
	if b.X+b.Width <= 0 || b.Y+b.Height <= 0 {
		// A shape wholly left of or above the sheet's origin prints
		// nowhere (sc/formula-across-sheets.ods).
		return
	}
	if !(b.Width > 0) {
		b.Width = 1
	}
	if !(b.Height > 0) {
		b.Height = 1
	}
	draw := art.draw
	pic := &layout.Picture{Width: b.Width, Height: b.Height}
	pic.Vector = func(ctx context.Context, rect compose.Rect) []compose.Fragment {
		sx, sy := rect.Width/b.Width, rect.Height/b.Height
		m := compose.Matrix{A: sx, D: sy, E: rect.X - b.X*sx, F: rect.Y - b.Y*sy}
		return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: draw(ctx)}}
	}
	d := sheet.Drawing{Name: el.A(odf.NSDraw, "name"), Picture: pic, Width: b.Width, Height: b.Height, Unbounded: true}
	end, resize := cellRef(el.A(odf.NSTable, "end-cell-address"))
	switch {
	case at == nil:
		d.Absolute, d.X, d.Y = true, max(b.X, 0), max(b.Y, 0)
	case resize && art.logic != nil && art.logic.w > 0 && art.logic.h > 0:
		// Calc sets the unturned box of a shape resized with its cells
		// from its anchors, then turns it about its corner
		// (ScDrawLayer::InitializeCellAnchoredObj, lcl_SetLogicRectFromAnchor).
		lb := art.logic
		ex, _ := odf.Length(el.A(odf.NSTable, "end-x"))
		ey, _ := odf.Length(el.A(odf.NSTable, "end-y"))
		d.From = sheet.Anchor{Row: at.Row, Column: at.Column, DX: lb.x - ts.colLeft(at.Column), DY: lb.y - ts.rowY}
		d.To = &sheet.Anchor{Row: end.row, Column: end.col, DX: max(ex, 0), DY: max(ey, 0)}
		d.Width, d.Height = lb.w, lb.h
		pic.Width, pic.Height = lb.w, lb.h
		pic.Vector = func(ctx context.Context, rect compose.Rect) []compose.Fragment {
			m := mul(translation(rect.X, rect.Y), mul(lb.lin, compose.Matrix{A: rect.Width / lb.w, D: rect.Height / lb.h}))
			return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: lb.local(ctx, compose.Rect{Width: lb.w, Height: lb.h})}}
		}
	default:
		d.From = sheet.Anchor{Row: at.Row, Column: at.Column, DX: b.X - ts.colLeft(at.Column), DY: b.Y - ts.rowY}
	}
	r.drawings++
	ts.sh.Drawings = append(ts.sh.Drawings, d)
}

// shapePaint reads the fill of graphic properties p (ODF 1.3, Part 3,
// 20.118 draw:fill and the properties it selects), nil for none: a fill
// no style sets is solid, as LibreOffice's item pool gives it. A gradient
// is drawn linear or axial between its colours, the other kinds radial; a
// hatch and a picture fill are drawn as the fill colour, with a warning.
func (r *reader) shapePaint(p odf.Props) *shape.Paint {
	kind, ok := p.Lookup("draw:fill")
	if !ok {
		kind = "solid"
	}
	alpha := 1.0
	if v, ok := odf.Percent(p.Get("draw:opacity")); ok {
		alpha = math.Max(0, math.Min(1, v))
	} else if v, ok := odf.Percent(p.Get("draw:transparency")); ok {
		alpha = math.Max(0, math.Min(1, 1-v))
	}
	col := func(c odf.Color) compose.Color {
		return compose.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: alpha}
	}
	solid := func() *shape.Paint {
		c, ok := odf.ParseColor(p.Get("draw:fill-color"))
		if !ok {
			c = poolFill
		}
		return &shape.Paint{Kind: shape.PaintSolid, Color: col(c)}
	}
	switch kind {
	case "none":
		return nil
	case "gradient":
		g := r.drawStyles().Gradients[p.Get("draw:fill-gradient-name")]
		if g == nil {
			return solid()
		}
		// ODF's draw:angle turns counterclockwise from a start colour at
		// the top (19.112); the paint's angle clockwise from the x axis.
		pt := &shape.Paint{Kind: shape.PaintGradient, Angle: math.Mod(90-g.Angle+720, 360)}
		s, e := col(g.Start), col(g.End)
		switch g.Style {
		case "", "linear":
			pt.Stops = []compose.ColorStop{{Offset: 0, Color: s}, {Offset: 1, Color: e}}
		case "axial":
			pt.Stops = []compose.ColorStop{{Offset: 0, Color: e}, {Offset: 0.5, Color: s}, {Offset: 1, Color: e}}
		default:
			pt.Radial = true
			pt.Stops = []compose.ColorStop{{Offset: 0, Color: e}, {Offset: 1, Color: s}}
		}
		return pt
	case "hatch", "bitmap":
		r.warn("shape-fill", "hatched and picture fills of drawing shapes are drawn as their fill colour")
		return solid()
	}
	return solid()
}

// shapeStroke is a shape's line: a shape.Line, drawn as the thinnest
// line the device draws when hairline is set.
type shapeStroke struct {
	line     shape.Line
	hairline bool
	// startArrow and endArrow are the widths of the arrowheads at the
	// line's ends, 0 for none.
	startArrow, endArrow float64
}

// hairlineWidth is the width a hairline's arrowheads and dashes are sized
// for: half a point.
const hairlineWidth = 0.5

// shapeLine reads the stroke of graphic properties p (ODF 1.3, Part 3,
// 20.167 draw:stroke, 20.410 svg:stroke-width, 20.408 svg:stroke-color,
// 20.168 draw:stroke-dash, 20.146 and 20.143 the markers), nil for none:
// a stroke no style sets is a solid hairline, as LibreOffice's item pool
// gives it.
func (r *reader) shapeLine(p odf.Props) *shapeStroke {
	stroke, ok := p.Lookup("draw:stroke")
	if !ok {
		stroke = "solid"
	}
	if stroke == "none" {
		return nil
	}
	w, _ := odf.Length(p.Get("svg:stroke-width"))
	w = max(w, 0)
	c, ok := odf.ParseColor(p.Get("svg:stroke-color"))
	if !ok {
		c = poolStroke
	}
	s := &shapeStroke{line: shape.Line{Width: w, Color: compose.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: 1}, Join: compose.RoundJoin}}
	if v, ok := odf.Percent(p.Get("svg:stroke-opacity")); ok {
		s.line.Color.A = math.Max(0, math.Min(1, v))
	}
	if w == 0 {
		s.line.Width, s.hairline = hairlineWidth, true
	}
	if stroke == "dash" {
		if d := r.drawStyles().Dashes[p.Get("draw:stroke-dash")]; d != nil {
			s.line.Dash = d.DashArray(s.line.Width)
		}
	}
	switch p.Get("svg:stroke-linecap") {
	case "round":
		s.line.Cap = compose.RoundCap
	case "square":
		s.line.Cap = compose.SquareCap
	}
	arrow := func(end string) float64 {
		if p.Get("draw:marker-"+end) == "" {
			return 0
		}
		if mw, ok := odf.Length(p.Get("draw:marker-" + end + "-width")); ok && mw > 0 {
			return mw
		}
		return measureArrowWidth
	}
	s.startArrow, s.endArrow = arrow("start"), arrow("end")
	return s
}

// stroke returns the fragments that stroke ops with s.
func (s *shapeStroke) stroke(ops []compose.PathOp) []compose.Fragment {
	if s == nil || s.line.Color.A <= 0 || len(ops) < 2 {
		return nil
	}
	out := shape.Stroke(ops, s.line)
	if s.hairline {
		for _, f := range out {
			if p, ok := f.(*compose.Path); ok && p.Stroke != nil {
				p.Stroke.Width = 0
			}
		}
	}
	return out
}

// arrowhead returns a filled triangle of width w and a length of 1.5
// widths whose tip is (x, y), pointing away from (fx, fy), in colour c:
// the "Arrow" marker of LibreOffice's line ends (its svg:viewBox is 20 by
// 30).
func arrowhead(x, y, fx, fy, w float64, c compose.Color) compose.Fragment {
	dx, dy := x-fx, y-fy
	n := math.Hypot(dx, dy)
	if n == 0 {
		return nil
	}
	ux, uy := dx/n, dy/n
	l := 1.5 * w
	bx, by := x-ux*l, y-uy*l
	pt := func(px, py float64) [3]compose.Point { return [3]compose.Point{{X: px, Y: py}} }
	return &compose.Path{Ops: []compose.PathOp{
		{Kind: compose.MoveTo, P: pt(x, y)},
		{Kind: compose.LineTo, P: pt(bx-uy*w/2, by+ux*w/2)},
		{Kind: compose.LineTo, P: pt(bx+uy*w/2, by-ux*w/2)},
		{Kind: compose.ClosePath},
	}, Fill: &compose.Fill{Color: c}}
}

// segment returns the path of the line from (x1, y1) to (x2, y2).
func segment(x1, y1, x2, y2 float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x1, Y: y1}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x2, Y: y2}}},
	}
}

// bounds returns the bounding box of the points pts, grown by pad.
func bounds(pts [][2]float64, pad float64) compose.Rect {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		x0, y0 = math.Min(x0, p[0]), math.Min(y0, p[1])
		x1, y1 = math.Max(x1, p[0]), math.Max(y1, p[1])
	}
	if math.IsInf(x0, 0) {
		return compose.Rect{}
	}
	return compose.Rect{X: x0 - pad, Y: y0 - pad, Width: x1 - x0 + 2*pad, Height: y1 - y0 + 2*pad}
}

// lineEnds reads a line's or a connector's svg:x1, svg:y1, svg:x2 and
// svg:y2 (ODF 1.3, Part 3, 19.568 to 19.573), moved by (dx, dy).
func lineEnds(el *odf.Element, dx, dy float64) (x1, y1, x2, y2 float64, ok bool) {
	x1, ok1 := odf.Length(el.A(odf.NSSVG, "x1"))
	y1, ok2 := odf.Length(el.A(odf.NSSVG, "y1"))
	x2, ok3 := odf.Length(el.A(odf.NSSVG, "x2"))
	y2, ok4 := odf.Length(el.A(odf.NSSVG, "y2"))
	return x1 + dx, y1 + dy, x2 + dx, y2 + dy, ok1 && ok2 && ok3 && ok4
}

// lineArt draws a draw:line or draw:connector (ODF 1.3, Part 3, 10.3.3 and
// 10.3.10) as the straight line between its ends, with its arrowheads.
func lineArt(el *odf.Element, s *shapeStroke, dx, dy float64) (shapeArt, bool) {
	x1, y1, x2, y2, ok := lineEnds(el, dx, dy)
	if !ok || s == nil {
		return shapeArt{}, false
	}
	pad := math.Max(s.line.Width, math.Max(s.startArrow, s.endArrow))
	return shapeArt{
		bounds: bounds([][2]float64{{x1, y1}, {x2, y2}}, pad),
		draw: func(context.Context) []compose.Fragment {
			out := s.stroke(segment(x1, y1, x2, y2))
			if s.startArrow > 0 {
				out = appendFrag(out, arrowhead(x1, y1, x2, y2, s.startArrow, s.line.Color))
			}
			if s.endArrow > 0 {
				out = appendFrag(out, arrowhead(x2, y2, x1, y1, s.endArrow, s.line.Color))
			}
			return out
		},
	}, true
}

// appendFrag appends f to out when it is not nil.
func appendFrag(out []compose.Fragment, f compose.Fragment) []compose.Fragment {
	if f == nil {
		return out
	}
	return append(out, f)
}

// measureArt draws a draw:measure (ODF 1.3, Part 3, 10.3.12), its points
// moved by (dx, dy), as LibreOffice draws a dimension line: the
// dimension line draw:line-distance to the left of the way from its
// first point to its second, with its arrowheads, and a guide from each
// point, starting draw:guide-distance from it and passing the dimension
// line by draw:guide-overhang. The measured value is not drawn.
func measureArt(el *odf.Element, s *shapeStroke, p odf.Props, dx, dy float64) (shapeArt, bool) {
	x1, y1, x2, y2, ok := lineEnds(el, dx, dy)
	n := math.Hypot(x2-x1, y2-y1)
	if !ok || s == nil || n == 0 {
		return shapeArt{}, false
	}
	length := func(key string, def float64) float64 {
		if v, ok := odf.Length(p.Get(key)); ok {
			return v
		}
		return def
	}
	dist := length("draw:line-distance", measureLineDistance)
	over := length("draw:guide-overhang", measureGuideOverhang)
	gap := length("draw:guide-distance", measureGuideDistance)
	// The normal turns the way from the first point to the second a
	// quarter counterclockwise on the page (measured on the pinned
	// LibreOffice: sc/tdf137576_Measureline.ods, measured downwards,
	// prints its dimension line right of its points).
	nx, ny := (y2-y1)/n, -(x2-x1)/n
	ax, ay := x1+nx*dist, y1+ny*dist
	bx, by := x2+nx*dist, y2+ny*dist
	arrow := s.startArrow
	if arrow == 0 {
		arrow = s.endArrow
	}
	guide := func(x, y float64) []compose.PathOp {
		return segment(x+nx*gap, y+ny*gap, x+nx*(dist+over), y+ny*(dist+over))
	}
	pts := [][2]float64{{x1, y1}, {x2, y2}, {x1 + nx*(dist+over), y1 + ny*(dist+over)}, {x2 + nx*(dist+over), y2 + ny*(dist+over)}}
	return shapeArt{
		bounds: bounds(pts, math.Max(arrow, s.line.Width)),
		draw: func(context.Context) []compose.Fragment {
			out := s.stroke(segment(ax, ay, bx, by))
			out = append(out, s.stroke(guide(x1, y1))...)
			out = append(out, s.stroke(guide(x2, y2))...)
			if s.startArrow > 0 {
				out = appendFrag(out, arrowhead(ax, ay, bx, by, s.startArrow, s.line.Color))
			}
			if s.endArrow > 0 {
				out = appendFrag(out, arrowhead(bx, by, ax, ay, s.endArrow, s.line.Color))
			}
			return out
		},
	}, true
}

// geometryArt draws a shape of a box: a custom shape's enhanced geometry,
// a rectangle, an ellipse, a polygon, polyline or path, in the box svg:x,
// svg:y, svg:width and svg:height give, under its draw:transform, moved
// by (dx, dy).
func (r *reader) geometryArt(el *odf.Element, paint *shape.Paint, s *shapeStroke, dx, dy float64) (shapeArt, bool) {
	x, _ := odf.Length(el.A(odf.NSSVG, "x"))
	y, _ := odf.Length(el.A(odf.NSSVG, "y"))
	w, okW := odf.Length(el.A(odf.NSSVG, "width"))
	h, okH := odf.Length(el.A(odf.NSSVG, "height"))
	if !okW || !okH || w < 0 || h < 0 || (w == 0 && h == 0) {
		return shapeArt{}, false
	}
	var g *shape.Geometry
	switch el.Name.Local {
	case "custom-shape":
		eg := el.Child(odf.NSDraw, "enhanced-geometry")
		if eg != nil {
			g = odf.ParseGeometry(eg)
		}
		if g != nil && !shape.Known(g) {
			r.warn("custom-geometry", "the shape type "+strconv.Quote(g.Preset)+" is drawn as its bounding box")
		}
	case "rect":
		if rad, ok := odf.Length(el.A(odf.NSDraw, "corner-radius")); ok && rad > 0 {
			if side := math.Min(w, h); side > 0 {
				g = &shape.Geometry{Preset: "roundRect", Adjust: []shape.Adjust{{Name: "adj", Value: math.Min(rad/side, 0.5) * 100000}}}
			}
		}
	case "ellipse", "circle":
		if k := el.A(odf.NSDraw, "kind"); k != "" && k != "full" {
			r.warn("approximated", "ellipse sections, cuts and arcs are drawn as whole ellipses")
		}
		g = &shape.Geometry{Preset: "ellipse"}
	case "polyline", "polygon", "path", "regular-polygon":
		g = outline(el)
		if g == nil {
			r.warn("custom-geometry", fmt.Sprintf("the outline of draw:%s is drawn as its bounding box", el.Name.Local))
		}
		if el.Name.Local == "polyline" {
			paint = nil
		}
	}
	m := translation(x, y)
	if tr := el.A(odf.NSDraw, "transform"); tr != "" {
		t, ok := parseTransform(tr)
		if !ok {
			r.warn("transform", "a draw:transform that does not parse is not applied")
		} else {
			m = mul(t, m)
		}
	}
	m = mul(translation(dx, dy), m)
	if g != nil && g.MirrorH != g.MirrorV && (math.Abs(m.B) > 1e-9 || math.Abs(m.C) > 1e-9) {
		// LibreOffice mirrors a turned custom shape about the page's axis
		// through its centre, after the turn: the geometry's own mirror,
		// undone, then mirrored on the page.
		cx, cy := apply(m, w/2, h/2)
		local, page := compose.Matrix{A: -1, D: 1, E: w}, compose.Matrix{A: -1, D: 1, E: 2 * cx}
		if g.MirrorV {
			local, page = compose.Matrix{A: 1, D: -1, F: h}, compose.Matrix{A: 1, D: -1, F: 2 * cy}
		}
		m = mul(page, mul(m, local))
	}
	pts := make([][2]float64, 0, 4)
	for _, c := range [][2]float64{{0, 0}, {w, 0}, {0, h}, {w, h}} {
		px, py := apply(m, c[0], c[1])
		pts = append(pts, [2]float64{px, py})
	}
	pad := 0.0
	if s != nil {
		pad = s.line.Width / 2
	}
	local := func(_ context.Context, box compose.Rect) []compose.Fragment {
		var out []compose.Fragment
		for _, path := range shape.Paths(g, box) {
			if path.Fill && paint != nil {
				pt := *paint
				if path.Mode != shape.FillNorm && pt.Kind == shape.PaintSolid {
					pt.Color = shape.Shade(pt.Color, path.Mode)
				}
				out = append(out, shape.Fill(path.Ops, box, pt)...)
			}
			if path.Stroke {
				out = append(out, s.stroke(path.Ops)...)
			}
		}
		return out
	}
	lin := m
	lin.E, lin.F = 0, 0
	return shapeArt{
		bounds: bounds(pts, pad),
		draw: func(ctx context.Context) []compose.Fragment {
			return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: local(ctx, compose.Rect{Width: w, Height: h})}}
		},
		logic: &logicBox{x: m.E, y: m.F, w: w, h: h, lin: lin, local: local},
	}, true
}

// outline reads the outline of a draw:path, draw:polygon, draw:polyline
// or draw:regular-polygon (ODF 1.3, Part 3, 10.3.7, 10.3.5, 10.3.4,
// 10.3.6) as a custom geometry: svg:d or draw:points in the coordinate
// space svg:viewBox fitted to the shape's box, or the regular polygon
// draw:corners, draw:concave and draw:sharpness describe; nil when it
// does not parse.
func outline(el *odf.Element) *shape.Geometry {
	var c *shape.Custom
	view := compose.Rect{}
	if v, ok := odf.ParseViewBox(el.A(odf.NSSVG, "viewBox")); ok && v[2] > 0 && v[3] > 0 {
		view = compose.Rect{X: v[0], Y: v[1], Width: v[2], Height: v[3]}
	}
	switch el.Name.Local {
	case "path":
		c = odf.PathData(el.A(odf.NSSVG, "d"), view)
	case "polygon", "polyline":
		c = odf.Points(el.A(odf.NSDraw, "points"), view, el.Name.Local == "polygon")
	case "regular-polygon":
		n, _ := strconv.Atoi(el.A(odf.NSDraw, "corners"))
		sharp, _ := strconv.ParseFloat(strings.TrimSuffix(el.A(odf.NSDraw, "sharpness"), "%"), 64)
		c = odf.RegularPolygon(n, el.A(odf.NSDraw, "concave") == "true", sharp)
	}
	if c == nil {
		return nil
	}
	return &shape.Geometry{Custom: c}
}

// drawStyles returns the document's gradients, dashes and markers, read
// once.
func (r *reader) drawStyles() *odf.DrawStyles {
	if r.draw == nil {
		r.draw = odf.NewDrawStyles(r.pkg)
	}
	return r.draw
}

// colHidden reports a hidden column c of the table read.
func (ts *tableState) colHidden(c int) bool {
	for _, col := range ts.sh.Columns {
		if col.First <= c && c <= col.Last {
			return col.Hidden
		}
	}
	return false
}

// transformedPicture adds the picture pic of the frame el, of size w by h,
// drawn under the draw:transform tr (ODF 1.3, Part 3, 19.228) from the
// corner of its anchor cell at, or the sheet's origin when at is nil.
func (r *reader) transformedPicture(ts *tableState, el *odf.Element, at *sheet.Anchor, pic *layout.Picture, tr string, w, h float64) {
	m, ok := parseTransform(tr)
	if !ok {
		r.warn("transform", "a draw:transform that does not parse is not applied")
		m = compose.Identity
	}
	x, _ := odf.Length(el.A(odf.NSSVG, "x"))
	y, _ := odf.Length(el.A(odf.NSSVG, "y"))
	m = mul(m, translation(x, y))
	if at != nil {
		m = mul(translation(ts.colLeft(at.Column), ts.rowY), m)
	}
	pts := make([][2]float64, 0, 4)
	for _, c := range [][2]float64{{0, 0}, {w, 0}, {0, h}, {w, h}} {
		px, py := apply(m, c[0], c[1])
		pts = append(pts, [2]float64{px, py})
	}
	local := func(ctx context.Context, box compose.Rect) []compose.Fragment {
		switch {
		case pic.Vector != nil:
			return pic.Vector(ctx, box)
		case pic.Source != nil:
			return []compose.Fragment{&compose.Image{Rect: box, Source: pic.Source}}
		}
		return nil
	}
	lin := m
	lin.E, lin.F = 0, 0
	r.place(ts, el, at, shapeArt{
		bounds: bounds(pts, 0),
		draw: func(ctx context.Context) []compose.Fragment {
			return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: local(ctx, compose.Rect{Width: w, Height: h})}}
		},
		logic: &logicBox{x: m.E, y: m.F, w: w, h: h, lin: lin, local: local},
	})
}
