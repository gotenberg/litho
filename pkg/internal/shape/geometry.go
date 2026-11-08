package shape

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
)

// Geometry is the outline a shape is drawn with: a preset by name with its
// adjustments, or a custom geometry. A reader fills Preset and Adjust from
// the file, or Custom for a geometry the file spells out (ECMA-376 Part
// 1, 20.1.9.8 custGeom; ODF 1.3 Part 3, 19.145 draw:enhanced-path).
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
	// MirrorH and MirrorV mirror the outline and the text rectangle
	// about the box's vertical and horizontal centre lines (ODF 1.3 Part
	// 3, 19.137 draw:mirror-horizontal and 19.138 draw:mirror-vertical).
	// A DrawingML shape's flips belong to its transform, not here.
	MirrorH, MirrorV bool
}

// Adjust is one adjustment value of a preset geometry.
type Adjust struct {
	Name  string
	Value float64
}

// Custom is a geometry spelled out as guides and paths: a DrawingML
// custom geometry, a preset of the generated table, or an OpenDocument
// enhanced geometry compiled into the same form.
//
// Guides are evaluated in order over the adjustments, then the guides: an
// [ArgGuide] operand's Ref indexes Adjust followed by Guides. A guide may
// name a later one; a reference cycle, a chain deeper than [MaxGuideDepth]
// or an index out of range evaluates to 0, and [Check] reports it.
type Custom struct {
	// Adjust are the adjustment guides with their defaults (20.1.9.5
	// avLst; ODF draw:modifiers), which a [Geometry]'s Adjust overrides by
	// name.
	Adjust []Guide
	// Guides are the formulas (20.1.9.12 gdLst; ODF draw:equation).
	Guides []Guide
	// Text is the text rectangle (20.1.9.22 rect; the first of ODF's
	// draw:text-areas), nil for the whole box.
	Text *TextBox
	// Paths are the outlines, in paint order (20.1.9.16 pathLst; ODF
	// draw:enhanced-path, cut at its N commands).
	Paths []PathDef
	// View, when not nil, is an OpenDocument svg:viewBox: the guide and
	// path coordinate space, fitted to the box, and the left, top, right,
	// bottom, width and height of the formulas. Nil is DrawingML's guide
	// space: the shape's size in EMUs, with the origin at its corner.
	View *compose.Rect
	// Unresolved lists the names the reader could not resolve, which it
	// replaced with 0 (20.1.9.11: a guide names an earlier guide, an
	// adjustment or a built-in).
	Unresolved []string
}

// TextBox is a text rectangle by its left, top, right and bottom edges in
// guide space.
type TextBox struct {
	L, T, R, B Arg
}

// FillMode is how a path's interior is painted (ECMA-376 Part 1,
// 20.1.10.37 ST_PathFillMode).
type FillMode uint8

// The fill modes.
const (
	// FillNorm fills with the shape's fill.
	FillNorm FillMode = iota
	// FillNone does not fill.
	FillNone
	// FillLighten, FillLightenLess, FillDarken and FillDarkenLess fill
	// with the shape's fill made lighter or darker, as the outline of a
	// 3D-looking preset asks.
	FillLighten
	FillLightenLess
	FillDarken
	FillDarkenLess
)

// PathDef is one outline of a [Custom] geometry in its own coordinate
// space (ECMA-376 Part 1, 20.1.9.15 path).
type PathDef struct {
	// W and H are the path's coordinate space, fitted to the box; 0 is
	// the guide space. An OpenDocument path ignores them for the View.
	W, H float64
	// Fill is how the path fills; Stroke whether it is stroked.
	Fill   FillMode
	Stroke bool
	// Cmds are the path's commands.
	Cmds []Cmd
}

// CmdKind is the kind of a path command.
type CmdKind uint8

// The path commands, with their operands in Args. Points are (x, y)
// pairs; DrawingML angles are in 60,000ths of a degree, OpenDocument ones
// in degrees.
const (
	// CmdMove starts a subpath at (x, y) (20.1.9.14 moveTo; ODF M).
	CmdMove CmdKind = iota
	// CmdLine draws a line to (x, y) (20.1.9.13 lnTo; ODF L).
	CmdLine
	// CmdArc draws the arc of the ellipse of radii wR and hR that starts
	// at the current point at angle stAng and sweeps swAng (20.1.9.3
	// arcTo: wR, hR, stAng, swAng).
	CmdArc
	// CmdQuad draws a quadratic Bézier through (x1, y1) to (x, y)
	// (20.1.9.21 quadBezTo; ODF Q).
	CmdQuad
	// CmdCubic draws a cubic Bézier through (x1, y1) and (x2, y2) to (x,
	// y) (20.1.9.7 cubicBezTo; ODF C).
	CmdCubic
	// CmdClose closes the subpath (20.1.9.4 close; ODF Z).
	CmdClose
	// CmdAngleEllipseTo draws a line to, then the arc of, the ellipse of
	// centre (x, y) and radii (w, h) from angle t0 to t1 degrees,
	// counter-clockwise (ODF T: x y w h t0 t1).
	CmdAngleEllipseTo
	// CmdAngleEllipse is CmdAngleEllipseTo starting a new subpath (ODF
	// U).
	CmdAngleEllipse
	// CmdArcTo draws a line to, then the arc counter-clockwise of, the
	// ellipse inscribed in the box (x1, y1)-(x2, y2), from where the ray
	// from its centre through (x3, y3) meets it to where the ray through
	// (x4, y4) does (ODF A).
	CmdArcTo
	// CmdArcMove is CmdArcTo starting a new subpath (ODF B).
	CmdArcMove
	// CmdClockwiseArcTo is CmdArcTo drawn clockwise (ODF W).
	CmdClockwiseArcTo
	// CmdClockwiseArcMove is CmdArcMove drawn clockwise (ODF V).
	CmdClockwiseArcMove
	// CmdQuadrantX draws a quarter ellipse from the current point to (x,
	// y) leaving it horizontally (ODF X).
	CmdQuadrantX
	// CmdQuadrantY draws a quarter ellipse leaving the current point
	// vertically (ODF Y).
	CmdQuadrantY
)

// Cmd is one path command with its operands.
type Cmd struct {
	Kind CmdKind
	Args []Arg
}

// The caps of a geometry. A reader stops adding past them, and the
// engine evaluates no more.
const (
	// MaxGuides caps a geometry's adjustments and guides together. Anti-DoS
	// guard, not spec-mandated: the largest preset of Annex D has 244.
	MaxGuides = 4096
	// MaxCommands caps a geometry's path commands, over all its paths.
	// Anti-DoS guard, not spec-mandated: the largest preset has 70, a
	// freeform traced from a drawing a few thousand.
	MaxCommands = 1 << 16
	// MaxPaths caps a geometry's paths. Anti-DoS guard, not
	// spec-mandated: the presets have at most 6.
	MaxPaths = 1024
	// MaxGuideDepth caps the chain of forward guide references evaluated
	// recursively, which protects the stack. Anti-DoS guard, not
	// spec-mandated: DrawingML guides name earlier guides only, so a
	// preset never recurses.
	MaxGuideDepth = 64
)

// Path is one outline of a geometry fitted to a box, with how it is
// painted: the shape's fill when Fill is set, its line when Stroke is.
type Path struct {
	Ops    []compose.PathOp
	Fill   bool
	Stroke bool
	// Mode is the fill mode of a filled path: [FillNorm], or one of the
	// lighten and darken modes the caller may tint the fill by.
	Mode FillMode
}

// definition returns the custom geometry g draws, nil for a rectangle.
func definition(g *Geometry) *Custom {
	if g == nil {
		return nil
	}
	if g.Custom != nil {
		return g.Custom
	}
	return presetTable[g.Preset]
}

// Paths returns the outlines of g fitted to r, in paint order, with each
// path's fill and stroke as the definition says (ECMA-376 Part 1, 20.1.9.15
// path fill and stroke). A nil g, an empty Preset and a preset the table
// lacks are the rectangle r; a custom geometry with no path draws nothing.
// Arcs are turned into cubic Béziers of at most a quarter turn each.
func Paths(g *Geometry, r compose.Rect) []Path {
	def := definition(g)
	if def == nil {
		return []Path{{Ops: RectOps(r), Fill: true, Stroke: true}}
	}
	ev := newEvaluator(def, g, r)
	out := make([]Path, 0, len(def.Paths))
	budget := MaxCommands
	for i := range def.Paths {
		if i >= MaxPaths || budget <= 0 {
			break
		}
		pd := &def.Paths[i]
		n := min(len(pd.Cmds), budget)
		budget -= n
		ops := ev.path(pd, pd.Cmds[:n])
		if len(ops) == 0 {
			continue
		}
		out = append(out, Path{Ops: ops, Fill: pd.Fill != FillNone, Stroke: pd.Stroke, Mode: pd.Fill})
	}
	return out
}

// Known reports whether Paths draws the geometry g as its preset or custom
// definition asks, rather than as its bounding rectangle: true for a nil
// g, the rectangle, every preset of ECMA-376 Part 1, Annex D and every
// custom geometry.
func Known(g *Geometry) bool {
	return g == nil || (g.Custom == nil && g.Preset == "") || definition(g) != nil
}

// TextRect returns the box the text of a shape of geometry g fitted to r
// is laid out in (ECMA-376 Part 1, 20.1.9.22 rect, the text rectangle of
// the presets of Annex D; ODF draw:text-areas), r when the geometry
// defines none. An inverted rectangle is normalised.
func TextRect(g *Geometry, r compose.Rect) compose.Rect {
	def := definition(g)
	if def == nil || def.Text == nil {
		return r
	}
	ev := newEvaluator(def, g, r)
	x0, y0 := ev.guidePoint(def.Text.L, def.Text.T)
	x1, y1 := ev.guidePoint(def.Text.R, def.Text.B)
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
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
