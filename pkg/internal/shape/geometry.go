package shape

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// Geometry is the outline a shape is drawn with: a preset by name with its
// adjustments, or a custom geometry. A reader fills Preset and Adjust from
// the file and leaves Custom nil; a custom geometry's parsing lands with
// the geometry engine, which declares Custom's fields.
type Geometry struct {
	// Preset is the DrawingML preset name (ECMA-376 Part 1, 20.1.10.56
	// ST_ShapeType: "rect", "roundRect", "ellipse", "triangle",
	// "rightArrow", ...), or, for an OpenDocument shape, the draw:type the
	// engine maps onto one ("round-rectangle" to "roundRect"). "" with a
	// nil Custom draws a rectangle.
	Preset string
	// Adjust are the adjustment values the file sets, by the preset's
	// guide name ("adj", "adj1", "adj2"), in the preset's units: a
	// DrawingML value is 1/100,000 of the shorter side for most presets
	// and 1/60,000 of a degree for angles (20.1.9.5 gd); an OpenDocument
	// draw:modifiers value is read in the same scale by the engine.
	Adjust []Adjust
	// Custom, when not nil, is a custom geometry, which overrides Preset.
	Custom *Custom
}

// Adjust is one adjustment value of a preset geometry.
type Adjust struct {
	Name  string
	Value float64
}

// Custom is a custom geometry: guide formulas and paths in the shape's
// own coordinate space. Its fields are declared with the geometry engine.
type Custom struct{}

// Path is one outline of a geometry fitted to a box, with how it is
// painted: the shape's fill when Fill is set, its line when Stroke is.
type Path struct {
	Ops    []compose.PathOp
	Fill   bool
	Stroke bool
}

// kappa places the control points of a cubic Bézier quarter circle.
const kappa = 0.5522847498

// adjustUnit is the scale of a DrawingML adjustment value: 100,000 is the
// shorter side (ECMA-376 Part 1, 20.1.9.5 gd, the presets of Annex D).
const adjustUnit = 100000

// Paths returns the outlines of g fitted to r, in paint order, filled and
// stroked. The presets drawn so far are rect, roundRect (its adj, a sixth
// of the shorter side by default), ellipse, line and the connectors drawn
// as lines; every other geometry is its bounding rectangle until the
// geometry engine lands. A nil g is a rectangle.
func Paths(g *Geometry, r compose.Rect) []Path {
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	pt := func(x, y float64) [3]compose.Point { return [3]compose.Point{{X: x, Y: y}} }
	name := ""
	if g != nil {
		name = g.Preset
	}
	switch name {
	case "line", "straightConnector1", "bentConnector2", "bentConnector3", "curvedConnector3":
		return []Path{{Ops: []compose.PathOp{{Kind: compose.MoveTo, P: pt(x0, y0)}, {Kind: compose.LineTo, P: pt(x1, y1)}}, Stroke: true}}
	case "ellipse", "flowChartConnector":
		cx, cy, rx, ry := (x0+x1)/2, (y0+y1)/2, r.Width/2, r.Height/2
		kx, ky := rx*kappa, ry*kappa
		return []Path{{Fill: true, Stroke: true, Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: pt(cx+rx, cy)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx + rx, Y: cy + ky}, {X: cx + kx, Y: cy + ry}, {X: cx, Y: cy + ry}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx - kx, Y: cy + ry}, {X: cx - rx, Y: cy + ky}, {X: cx - rx, Y: cy}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx - rx, Y: cy - ky}, {X: cx - kx, Y: cy - ry}, {X: cx, Y: cy - ry}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx + kx, Y: cy - ry}, {X: cx + rx, Y: cy - ky}, {X: cx + rx, Y: cy}}},
			{Kind: compose.ClosePath},
		}}}
	case "roundRect", "flowChartAlternateProcess":
		adj := adjustUnit / 6.0
		for _, a := range g.Adjust {
			if a.Name == "adj" && a.Value >= 0 && a.Value <= adjustUnit/2 {
				adj = a.Value
			}
		}
		rad := min(r.Width, r.Height) * adj / adjustUnit
		k := rad * (1 - kappa)
		return []Path{{Fill: true, Stroke: true, Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: pt(x0+rad, y0)},
			{Kind: compose.LineTo, P: pt(x1-rad, y0)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x1 - k, Y: y0}, {X: x1, Y: y0 + k}, {X: x1, Y: y0 + rad}}},
			{Kind: compose.LineTo, P: pt(x1, y1-rad)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x1, Y: y1 - k}, {X: x1 - k, Y: y1}, {X: x1 - rad, Y: y1}}},
			{Kind: compose.LineTo, P: pt(x0+rad, y1)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x0 + k, Y: y1}, {X: x0, Y: y1 - k}, {X: x0, Y: y1 - rad}}},
			{Kind: compose.LineTo, P: pt(x0, y0+rad)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x0, Y: y0 + k}, {X: x0 + k, Y: y0}, {X: x0 + rad, Y: y0}}},
			{Kind: compose.ClosePath},
		}}}
	}
	return []Path{{Ops: RectOps(r), Fill: true, Stroke: true}}
}

// Known reports whether Paths draws the geometry g as its preset asks,
// rather than as its bounding rectangle.
func Known(g *Geometry) bool {
	if g == nil {
		return true
	}
	switch g.Preset {
	case "", "rect", "flowChartProcess", "frame", "line", "straightConnector1", "bentConnector2", "bentConnector3", "curvedConnector3",
		"ellipse", "flowChartConnector", "roundRect", "flowChartAlternateProcess":
		return true
	}
	return false
}

// TextRect returns the box the text of a shape of geometry g fitted to r
// is laid out in (ECMA-376 Part 1, 20.1.9.22 rect, the text rectangle of
// the presets of Annex D; ODF draw:text-areas): the ellipse's inscribed
// rectangle, r for every other geometry until the engine lands.
func TextRect(g *Geometry, r compose.Rect) compose.Rect {
	if g != nil && (g.Preset == "ellipse" || g.Preset == "flowChartConnector") {
		// Annex D: il = w * 29289 / 200000, the inset of the inscribed
		// square's corner, (1 - cos 45°) / 2 of the side.
		ix, iy := r.Width*(1-math.Sqrt2/2)/2, r.Height*(1-math.Sqrt2/2)/2
		return compose.Rect{X: r.X + ix, Y: r.Y + iy, Width: r.Width - 2*ix, Height: r.Height - 2*iy}
	}
	return r
}

// RectOps returns the path of the rectangle r.
func RectOps(r compose.Rect) []compose.PathOp {
	pt := func(x, y float64) [3]compose.Point { return [3]compose.Point{{X: x, Y: y}} }
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: pt(r.X, r.Y)},
		{Kind: compose.LineTo, P: pt(r.X+r.Width, r.Y)},
		{Kind: compose.LineTo, P: pt(r.X+r.Width, r.Y+r.Height)},
		{Kind: compose.LineTo, P: pt(r.X, r.Y+r.Height)},
		{Kind: compose.ClosePath},
	}
}
