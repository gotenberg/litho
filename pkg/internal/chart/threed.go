package chart

import (
	"math"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// View3D is how a three-dimensional plot is seen (ECMA-376 Part 1,
// 21.2.2.228 view3D). With right-angled axes, or under LibreOffice's
// rules, the renderer draws an oblique parallel projection: the front plane
// of the plot keeps its right angles and its scale, and the depth axis runs
// up and to the side. Under [OfficeRules] without right-angled axes it
// turns the plot's box by RotY, tilts it by RotX and draws it in
// perspective, as Office does.
type View3D struct {
	// RotX tilts the plot towards the viewer, in degrees from -90 to 90:
	// a positive angle looks from above (21.2.2.157 rotX).
	RotX float64
	// RotY turns the plot about its vertical axis, in degrees from 0 to
	// 360: a positive sine looks from the right (21.2.2.158 rotY). A pie
	// turns its first slice clockwise by it.
	RotY float64
	// Depth is the depth of a bar, or of a pie's slab, as a percentage of
	// its width, 20 to 2000, 100 by default (21.2.2.41 depthPercent).
	Depth float64
	// Perspective is twice the camera's field of view in degrees, 0 to
	// 240 (21.2.2.136 perspective, as [MS-OI29500] reads it). A pie is
	// drawn a little flatter with it.
	Perspective float64
	// Height is the height of a turned plot's box as a percentage of its
	// width, 5 to 500 (21.2.2.83 hPercent); 0 scales it to the plot area,
	// as Office does when the file leaves it out.
	Height float64
	// RightAngle draws the axes at right angles, without perspective
	// (21.2.2.155 rAngAx).
	RightAngle bool
}

// BarShape is the solid a three-dimensional bar is drawn as (ECMA-376 Part
// 1, 21.2.3.37 ST_Shape).
type BarShape uint8

// The bar shapes.
const (
	ShapeBox BarShape = iota
	ShapeCylinder
	// ShapeCone and ShapePyramid narrow to a point at the bar's end;
	// ShapeConeToMax and ShapePyramidToMax narrow to a point at the value
	// axis's maximum, so a bar ends in a flat top.
	ShapeCone
	ShapeConeToMax
	ShapePyramid
	ShapePyramidToMax
)

// The shading of the faces of a solid, as a factor on the red, green and
// blue of its fill: the front keeps the fill, a face turned up or down
// takes faceUp, a face turned to the side faceSide.
//
// # Corpus evidence
//
//   - litho/pptx: chart-3d's clustered column, PowerPoint's 100 ppi plot
//     bitmap: the fronts #4472C5, #ED7E30 and #A5A5A5 have tops (52, 86,
//     149), (181, 94, 36) and (126, 126, 126), 0.746 to 0.765 of the front,
//     and right sides (42, 72, 126), (151, 80, 30) and (106, 106, 106), 0.618
//     to 0.642.
const (
	faceUp   = 0.76
	faceSide = 0.64
)

// pieRim is the shading of the rim of a three-dimensional pie, and
// pieHeight its thickness as a fraction of the radius at a depth of 100
// percent, before the tilt shortens it.
//
// # Corpus evidence
//
//   - litho/pptx: chart-pie's 3-D pie (rotX 30, perspective 30): radius
//     159 pixels, top 149 pixels high, rim 37 pixels; orange rim 0.40 to
//     0.57 of the top's colour.
//   - openxmlsdk/pptx: O14ISOStrict/Graphics/3D_Pie-O12-PPT-Charts.pptx
//     (rotX 30, perspective 30): radius 250, top 239, rim 55; blue rim 0.46
//     to 0.55 of the top's.
//   - poi/xlsx: spreadsheet/45544.xlsx (no rotX, perspective 0): radius
//     290.5, top 150, rim 69.
const (
	pieRim    = 0.5
	pieHeight = 0.25
)

// clampFinite returns v within [lo, hi], lo when v is not a number.
func clampFinite(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Max(lo, math.Min(v, hi))
}

// shade returns c with its red, green and blue scaled by f.
func shade(c typeset.Color, f float64) typeset.Color {
	return typeset.Color{R: c.R * f, G: c.G * f, B: c.B * f, A: c.A}
}

// shaded returns the paint of a face shaded by f: the fill's colour, a
// gradient's or a pattern's approximation, scaled, or the fill itself for
// the front.
func shaded(p *Paint, f float64) *Paint {
	if p == nil || p.None || f == 1 {
		return p
	}
	return &Paint{Color: shade(p.Color, f)}
}

// projection maps a point of a three-dimensional plot to the page. A point
// is given as a point of the plot's front plane, x to the right and y down
// as on the page, and its depth z behind that plane: the axes, series and
// labels are laid out on the front plane as on a flat plot, and the
// projection carries them into the view.
//
// The oblique view keeps the front plane on the page and moves a point by
// its depth along a fixed direction. The turned view is a box in space:
// turned about its vertical axis, tilted, and seen from a camera at a
// finite distance, so the front plane is a rectangle of the box's own size
// centred on the plot area, which the camera maps onto the page.
type projection struct {
	// dx and dy are the page offsets of one point of depth in the oblique
	// view: right by the sine of rotY, up by the sine of rotX.
	dx, dy float64
	// depth is the plot's depth, rowDepth a row's, and solid the depth of
	// a bar, a ribbon or a slab, centred in its row.
	depth, rowDepth, solid float64
	// horizontal lays the categories down the page.
	horizontal bool

	// turned selects the turned view; the fields below are its own.
	turned bool
	// sinY, cosY turn the box about its vertical axis by rotY, the right
	// side coming forward for a positive sine; sinX, cosX tilt it by rotX,
	// the camera looking from above for a positive sine.
	sinY, cosY, sinX, cosX float64
	// cam is the camera's distance from the plane through the box's
	// centre that faces it.
	cam float64
	// cx and cy are the centre of the front plane; ox and oy move the box
	// across the camera's view, right and up.
	cx, cy, ox, oy float64
	// origin is the page point the camera looks at: the plot area's centre.
	origin typeset.Point
	// eyeAt is the camera in front-plane coordinates.
	eyeAt [3]float64
}

// view returns front-plane point (x, y, z) in the camera's frame: right,
// up and away from the camera, from the point it looks at.
func (p *projection) view(x, y, z float64) [3]float64 {
	X, Y, Z := x-p.cx, p.cy-y, z-float64(p.depth/2)
	x1 := float64(p.cosY*X) + float64(p.sinY*Z)
	z1 := float64(p.cosY*Z) - float64(p.sinY*X)
	y2 := float64(p.cosX*Y) + float64(p.sinX*z1)
	z2 := float64(p.cosX*z1) - float64(p.sinX*Y)
	return [3]float64{x1 + p.ox, y2 + p.oy, z2}
}

// nearestCamera is the least share of the camera's distance a point of
// the turned box keeps in front of the camera, so a wide field of view
// cannot put a corner behind it. Anti-degeneracy guard, not
// spec-mandated: the box is sized to keep its corners at least this far,
// and a projected point never divides by less.
const nearestCamera = 0.25

// at returns the page point of front-plane point (x, y) at depth z.
func (p *projection) at(x, y, z float64) typeset.Point {
	if !p.turned {
		return typeset.Point{X: x + float64(z*p.dx), Y: y - float64(z*p.dy)}
	}
	v := p.view(x, y, z)
	w := p.cam / math.Max(p.cam+v[2], float64(nearestCamera*p.cam))
	return typeset.Point{X: p.origin.X + float64(v[0]*w), Y: p.origin.Y - float64(v[1]*w)}
}

// eye returns the direction from front-plane point (x, y, z) towards the
// viewer: a face whose outward normal has a positive product with it is
// seen. The oblique view looks along one direction, the turned view from
// the camera.
func (p *projection) eye(x, y, z float64) [3]float64 {
	if !p.turned {
		return [3]float64{p.dx, -p.dy, -1}
	}
	return [3]float64{p.eyeAt[0] - x, p.eyeAt[1] - y, p.eyeAt[2] - z}
}

// sees reports a face at (x, y, z) whose outward normal is n seen by the
// viewer, a face seen edge on included.
func (p *projection) sees(n [3]float64, x, y, z float64) bool {
	e := p.eye(x, y, z)
	return float64(n[0]*e[0])+float64(n[1]*e[1])+float64(n[2]*e[2]) >= 0
}

// farther reports a solid at coordinate a along front-plane axis k (0 for
// x, 1 for y, 2 for depth) farther from the viewer than one at b, which it
// is painted before. Two solids of a grid are parted by a plane across
// that axis, and the one on the viewer's side may hide the other, never
// the reverse.
func (p *projection) farther(a, b float64, k int) bool {
	if !p.turned {
		// A view straight along an axis orders it as one seen from the
		// right and from above.
		s := p.eye(0, 0, 0)[k]
		if s == 0 {
			s = 1
			if k == 1 {
				s = -1
			}
		}
		return (a-b)*math.Copysign(1, s) < 0
	}
	e := p.eyeAt[k]
	return math.Abs(a-e) > math.Abs(b-e)
}

// row returns the front and back depths of the solids of row i.
func (p *projection) row(i int) (float64, float64) {
	z0 := float64(float64(i)*p.rowDepth) + float64((p.rowDepth-p.solid)/2)
	return z0, z0 + p.solid
}

// maxDepthShare caps the share of the plot area's width and height the
// depth axis may take, so a deep view keeps a front plane to draw on.
const maxDepthShare = 0.5

// threeD returns the projection of g0's plot area when the chart draws it
// in three dimensions, nil when it draws it flat: the turned view under
// [OfficeRules] for a view without right-angled axes, as Office gives
// rAngAx precedence over the perspective ([MS-OI29500], ECMA-376 Part 1,
// 21.2.2.136 perspective), the oblique view otherwise.
func (rd *renderer) threeD(g0 *Group) *projection {
	v := rd.c.View3D
	if v == nil || !g0.ThreeD || (g0.Kind != Bar && g0.Kind != Line && g0.Kind != Area) {
		return nil
	}
	rx, ry := clampFinite(v.RotX, -90, 90), v.RotY
	if !numeric.Finite(ry) {
		ry = 0
	}
	p := &projection{horizontal: g0.Kind == Bar && g0.Horizontal}
	if rd.office() && !v.RightAngle {
		p.turned = true
		p.sinY, p.cosY = detmath.Sincos(ry * math.Pi / 180)
		p.sinX, p.cosX = detmath.Sincos(rx * math.Pi / 180)
		return p
	}
	p.dx = detmath.Sin(ry * math.Pi / 180)
	p.dy = detmath.Sin(rx * math.Pi / 180)
	return p
}

// depthPercent returns the view's depth percentage, within its range.
func (v *View3D) depthPercent() float64 {
	if v == nil || !numeric.Positive(v.Depth) {
		return 100
	}
	return clampFinite(v.Depth, 20, 2000)
}

// layout sizes the projection for g0 against the category dimension x and
// returns the front plane: inside inner, the rectangle the projected plot
// fills, for the oblique view; centred on it for the turned view. A solid
// is as deep as a bar is wide, scaled by the view's depth percentage; a
// row adds the group's gap depth; the plot is as deep as its rows.
//
// # Corpus evidence
//
//   - litho/pptx: chart-3d's clustered column (rotX 15, rotY 20, depth 100,
//     gap width and gap depth 150): bars 28 pixels wide, the floor 70
//     deep, projected 23.9 right and 18.1 up (sin 20 and sin 15), the
//     bars' fronts 21 pixels behind the floor's front edge; the front
//     plane's gridlines run 172 pixels apart from bottom to top, as the
//     back wall's.
//   - openxmlsdk/xlsx: BusinessPerformancetest21_with_content (rotX 23,
//     rotY 45, one series of cylinders, 5 categories 132.8 pixels apart):
//     the side wall's gridlines rise 0.55 for 1 across, the ratio of the
//     sines (0.553), and the floor is 52 pixels deep upward, 133 times sin
//     23.
//   - openxmlsdk/xlsx: O14ISOStrict/Graphics/Axis_Title-O12-XL-ChartProperties.xlsx
//     (one series over three categories, turned view) and openxmlsdk/pptx
//     v2FxTestFiles/presentation/Chart_3D_Column_.pptx (three rows over
//     four): a box fitted to Office's tick marks is 0.333 and 0.751 of its
//     width deep.
func (p *projection) layout(rd *renderer, g0 *Group, x *dim, inner typeset.Rect) typeset.Rect {
	k := 1
	rows := 1
	if g0.Rows {
		for _, g := range rd.c.Groups {
			if g.ThreeD {
				rows = max(rows, len(g.Series))
			}
		}
	} else if g0.Kind == Bar && g0.Grouping == Clustered {
		k = max(len(g0.Series), 1)
	}
	gap := math.Max(0, math.Min(g0.GapWidth, 500)) / 100
	ov := math.Max(-1, math.Min(g0.Overlap, 100)/100)
	if k == 1 {
		ov = 0
	}
	units := float64(max(x.n, 1))
	if !x.between && x.n > 1 {
		units = float64(x.n - 1)
	}
	gd := math.Max(0, math.Min(g0.GapDepth, 500)) / 100
	dp := rd.c.View3D.depthPercent() / 100
	// The depth per point of the category axis's length.
	ratio := float64(rows) * dp * (1 + gd) / (units * (float64(k) - float64(float64(k-1)*ov) + gap))
	// The names of the rows stand beside the floor, inside the plot area.
	if w := rd.rowLabelsWidth(p, g0); w > 0 && w < inner.Width/2 {
		inner.Width -= w
		if p.dx < 0 {
			inner.X += w
		}
	}
	if p.turned {
		f := p.turn(rd.c.View3D, inner, ratio)
		p.rowDepth = p.depth / float64(rows)
		p.solid = p.rowDepth / (1 + gd)
		return f
	}
	ax, ay := math.Abs(p.dx), math.Abs(p.dy)
	var front typeset.Rect
	if p.horizontal {
		front.Height = inner.Height / (1 + float64(ratio*ay))
		p.depth = ratio * front.Height
	} else {
		front.Width = inner.Width / (1 + float64(ratio*ax))
		p.depth = ratio * front.Width
	}
	if ax > 0 {
		p.depth = math.Min(p.depth, maxDepthShare*inner.Width/ax)
	}
	if ay > 0 {
		p.depth = math.Min(p.depth, maxDepthShare*inner.Height/ay)
	}
	front.Width = inner.Width - float64(p.depth*ax)
	front.Height = inner.Height - float64(p.depth*ay)
	p.rowDepth = p.depth / float64(rows)
	p.solid = p.rowDepth / (1 + gd)
	front.X = inner.X
	if p.dx < 0 {
		front.X += float64(p.depth * ax)
	}
	front.Y = inner.Y
	if p.dy > 0 {
		front.Y += float64(p.depth * ay)
	}
	return front
}

// The box of Office's turned view.
//
// The camera looks at the centre of the plot area from the distance at
// which its field of view spans the plot area's height: the field of view
// is half the perspective in degrees, and 0.1 degrees for a perspective of
// 0 ([MS-OI29500] on ECMA-376 Part 1, 21.2.2.136 perspective). Without
// hPercent Office scales the box's height to the plot area's height over
// its width (officeAutoHeight); with it, the height is that share of the
// box's width ([MS-OI29500] on 21.2.2.83 hPercent). The box is as deep as the rows make it
// (layout). Its projection is then scaled to fill officeBoxFill of the
// plot area's width or height, whichever binds, and centred on it.
//
// # Corpus evidence
//
//   - openxmlsdk/xlsx: O14ISOStrict/Graphics/Axis_Title-O12-XL-ChartProperties.xlsx,
//     eight charts at rotX 15, rotY 20, perspective 30, plot areas 120 to
//     222 pt wide and 132 or 152 pt high: a box turned by exactly 20 and
//     15 degrees and fitted to Office's tick marks matches each within
//     0.06 pt; the camera stands 3.80 plot heights from the box's centre,
//     1/(2 tan 7.5 degrees) = 3.798; the projection fills 0.972 of the
//     plot area's width; the box's height is 0.599 of the plot area's
//     height times the box's width plus its depth times sin rotY cos rotX,
//     over the plot area's width.
//   - litho/docx: charts-line-area's 3-D line chart, Word: 3.81 plot
//     heights, the projection 0.973 of the plot area's height, the
//     height rule at 0.602.
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/Chart_3D_Column_.pptx,
//     PowerPoint: 3.75 plot heights, 0.971 of the width, the height rule
//     at 0.600.
const (
	officeBoxFill    = 0.972
	officeAutoHeight = 0.6
	// officeNoFieldOfView is the field of view of a perspective of 0, in
	// degrees.
	officeNoFieldOfView = 0.1
	// turnPasses is how often the box is scaled and moved to fit the plot
	// area: under perspective the projection's size is not proportional
	// to the box's, so each pass scales by what the last one missed.
	turnPasses = 12
)

// turn sizes and places the box of the turned view in inner, ratio being
// its depth per point of its category length, and returns its front
// plane.
func (p *projection) turn(v *View3D, inner typeset.Rect, ratio float64) typeset.Rect {
	p.origin = typeset.Point{X: inner.X + float64(inner.Width/2), Y: inner.Y + float64(inner.Height/2)}
	p.cx, p.cy = p.origin.X, p.origin.Y
	fov := clampFinite(v.Perspective, 0, 240) / 2
	if fov <= 0 {
		fov = officeNoFieldOfView
	}
	p.cam = inner.Height / 2 / detmath.Tan(fov/2*math.Pi/180)
	if !above(inner.Width, 0) || !above(inner.Height, 0) || !numeric.Finite(p.cam) {
		p.cam, p.depth = 1, 0
		return inner
	}
	// The box's height over its width, and its depth over its width.
	slant := float64(math.Abs(p.sinY) * p.cosX)
	aspect := inner.Height / inner.Width
	var hw, dw float64
	if h := v.Height; numeric.Positive(h) {
		hw = clampFinite(h, 5, 500) / 100
	} else {
		hw = float64(officeAutoHeight * aspect)
		if p.horizontal {
			// The categories run up the box: its depth follows its
			// height, and the rule solves for it.
			den := 1 - float64(float64(hw*ratio)*slant)
			hw /= math.Max(den, 0.5)
		} else {
			hw = float64(hw * (1 + float64(ratio*slant)))
		}
	}
	dw = ratio
	if p.horizontal {
		dw = float64(ratio * hw)
	}
	w := math.Min(inner.Width, inner.Height)
	p.ox, p.oy = 0, 0
	var f typeset.Rect
	for range turnPasses {
		f = typeset.Rect{X: p.cx - float64(w/2), Y: p.cy - float64(float64(w*hw)/2), Width: w, Height: float64(w * hw)}
		p.depth = float64(w * dw)
		x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		near := 0.0
		for i := range 8 {
			x, y, z := f.X, f.Y, 0.0
			if i&1 != 0 {
				x += f.Width
			}
			if i&2 != 0 {
				y += f.Height
			}
			if i&4 != 0 {
				z = p.depth
			}
			pt := p.at(x, y, z)
			x0, y0, x1, y1 = math.Min(x0, pt.X), math.Min(y0, pt.Y), math.Max(x1, pt.X), math.Max(y1, pt.Y)
			near = math.Min(near, p.view(x, y, z)[2])
		}
		if !above(x1-x0, 0) || !above(y1-y0, 0) {
			break
		}
		s := math.Min(float64(officeBoxFill*inner.Width)/(x1-x0), float64(officeBoxFill*inner.Height)/(y1-y0))
		if near < 0 {
			// Keep the nearest corner nearestCamera of the way out.
			s = math.Min(s, float64((1-nearestCamera)*p.cam)/-near)
		}
		p.ox -= float64((x0+x1)/2) - p.origin.X
		p.oy += float64((y0+y1)/2) - p.origin.Y
		w *= s
	}
	f = typeset.Rect{X: p.cx - float64(w/2), Y: p.cy - float64(float64(w*hw)/2), Width: w, Height: float64(w * hw)}
	p.depth = float64(w * dw)
	p.eyeAt = p.camera()
	return f
}

// camera returns the camera's place in front-plane coordinates: the
// inverse of view at the camera, which stands cam before the point it
// looks at.
func (p *projection) camera() [3]float64 {
	x1, y2, z2 := -p.ox, -p.oy, -p.cam
	Y := float64(p.cosX*y2) - float64(p.sinX*z2)
	z1 := float64(p.sinX*y2) + float64(p.cosX*z2)
	X := float64(p.cosY*x1) - float64(p.sinY*z1)
	Z := float64(p.sinY*x1) + float64(p.cosY*z1)
	return [3]float64{X + p.cx, p.cy - Y, Z + float64(p.depth/2)}
}

// walls draws the back wall, the side wall and the floor of the front
// plane f, then the gridlines of dims on the back wall and on the side
// wall or the floor. The side wall is the one the viewer sees from inside
// the box.
func (rd *renderer) walls(p *projection, f typeset.Rect, dims []*dim) {
	x0, y0, x1, y1 := f.X, f.Y, f.X+f.Width, f.Y+f.Height
	d := p.depth
	side := x0
	if !p.sees([3]float64{1, 0, 0}, x0, f.Y+float64(f.Height/2), float64(d/2)) {
		side = x1
	}
	quad := func(a, b, c, e typeset.Point) []typeset.PathOp { return polygon(a, b, c, e) }
	rd.paint(quad(p.at(x0, y0, d), p.at(x1, y0, d), p.at(x1, y1, d), p.at(x0, y1, d)), rd.c.BackWall.Fill, rd.c.BackWall.Border)
	rd.paint(quad(p.at(side, y0, 0), p.at(side, y0, d), p.at(side, y1, d), p.at(side, y1, 0)), rd.c.SideWall.Fill, rd.c.SideWall.Border)
	rd.paint(quad(p.at(x0, y1, 0), p.at(x1, y1, 0), p.at(x1, y1, d), p.at(x0, y1, d)), rd.c.Floor.Fill, rd.c.Floor.Border)
	for _, dm := range dims {
		if dm.ax == nil {
			continue
		}
		draw := func(fs []float64, s *Stroke) {
			for _, fr := range fs {
				v := dm.pixel(fr)
				if dm.vertical {
					a, b, c := p.at(side, v, 0), p.at(side, v, d), p.at(x0, v, d)
					e := p.at(x1, v, d)
					if side == x1 {
						c, e = e, c
					}
					rd.add(line(a.X, a.Y, b.X, b.Y, s), line(c.X, c.Y, e.X, e.Y, s))
				} else {
					a, b, c, e := p.at(v, y1, 0), p.at(v, y1, d), p.at(v, y0, d), p.at(v, y1, d)
					rd.add(line(a.X, a.Y, b.X, b.Y, s), line(c.X, c.Y, e.X, e.Y, s))
				}
			}
		}
		if dm.ax.MinorGrid != nil && !dm.category {
			var fs []float64
			for _, v := range dm.sc.minorTicks() {
				fs = append(fs, dm.sc.frac(v))
			}
			draw(fs, dm.ax.MinorGrid)
		}
		if dm.ax.MajorGrid != nil {
			draw(dm.ticks, dm.ax.MajorGrid)
		}
	}
}

// solid is one convex piece of a three-dimensional plot, with the keys
// that order it back to front.
type solid struct {
	// z is its depth's middle; cat and val its middle along the category
	// and value directions on the front plane.
	z, cat, val float64
	draw        func()
}

// paintSolids draws ss in the painter's order of p: the farthest row
// first; within a row, the farthest along the category direction; then
// the farthest along the value direction. Bars, ribbons and slabs stand on
// a grid and do not pierce one another, so a plane across one of those
// directions parts any two of them, and a solid drawn later is never
// behind one drawn before.
func paintSolids(p *projection, ss []solid) {
	catK, valK := 0, 1
	if p.horizontal {
		catK, valK = 1, 0
	}
	sort.SliceStable(ss, func(i, j int) bool {
		a, b := ss[i], ss[j]
		if a.z != b.z {
			return p.farther(a.z, b.z, 2)
		}
		if a.cat != b.cat {
			return p.farther(a.cat, b.cat, catK)
		}
		return p.farther(a.val, b.val, valK)
	})
	for _, s := range ss {
		s.draw()
	}
}

// series3D draws the three-dimensional groups of the plot against x and
// their value dimensions, and returns their data labels.
func (rd *renderer) series3D(p *projection, x *dim, yOf map[*Group]*dim) []func() {
	var ss []solid
	var labels []func()
	for _, g := range rd.c.Groups {
		y := yOf[g]
		if y == nil {
			continue
		}
		if !g.ThreeD {
			labels = append(labels, rd.series(g, x, y)...)
			continue
		}
		var s []solid
		var l []func()
		switch g.Kind {
		case Bar:
			s, l = rd.bars3D(p, g, x, y)
		case Area:
			s = rd.areas3D(p, g, x, y)
		case Line:
			s = rd.lines3D(p, g, x, y)
		case Pie, Doughnut, Scatter, Bubble, Radar, Unsupported, Waterfall, BoxWhisker, Funnel, Treemap, Sunburst:
		}
		ss = append(ss, s...)
		labels = append(labels, l...)
	}
	paintSolids(p, ss)
	return labels
}

// bars3D lays out a bar group's solids: per category, the series side by
// side, stacked, or one behind the other in their rows.
func (rd *renderer) bars3D(p *projection, g *Group, x, y *dim) ([]solid, []func()) {
	sv := rd.stack(g)
	k := len(g.Series)
	if g.Grouping != Clustered || g.Rows {
		k = 1
	}
	if k == 0 {
		return nil, nil
	}
	gap := math.Max(0, math.Min(g.GapWidth, 500)) / 100
	ov := math.Max(-1, math.Min(g.Overlap, 100)/100)
	if k == 1 {
		ov = 1
	}
	catW := x.length() / float64(max(x.n, 1))
	if !x.between && x.n > 1 {
		catW = x.length() / float64(x.n-1)
	}
	barW := catW / (float64(k) - float64(float64(k-1)*ov) + gap)
	step := barW * (1 - ov)
	total := barW + float64(float64(k-1)*step)
	base := y.sc.clamp(x.crossValue())
	if y.sc.log > 0 {
		base = y.sc.min
	}
	top := y.valuePixel(y.sc.max)
	var ss []solid
	var labels []func()
	for i, s := range g.Series {
		slot, row := i, 0
		if g.Grouping != Clustered || g.Rows {
			slot = 0
		}
		if g.Rows {
			row = i
		}
		z0, z1 := p.row(row)
		for j := 0; j < sv.n && j < x.n; j++ {
			v := sv.hi[i][j]
			if math.IsNaN(v) {
				continue
			}
			b := sv.lo[i][j]
			if math.IsNaN(b) {
				b = base
			}
			c := x.valuePixel(float64(j))
			sign := 1.0
			if x.ax != nil && x.ax.Reverse {
				sign = -1
			}
			if x.vertical {
				sign = -sign
			}
			p0 := c + float64(sign*(float64(-total/2)+float64(float64(slot)*step)))
			p1 := p0 + float64(sign*barW)
			v0, v1 := y.valuePixel(y.sc.clamp(b)), y.valuePixel(y.sc.clamp(v))
			var r typeset.Rect
			if x.vertical {
				r = typeset.Rect{X: math.Min(v0, v1), Y: math.Min(p0, p1), Width: math.Abs(v1 - v0), Height: math.Abs(p1 - p0)}
			} else {
				r = typeset.Rect{X: math.Min(p0, p1), Y: math.Min(v0, v1), Width: math.Abs(p1 - p0), Height: math.Abs(v1 - v0)}
			}
			fill, border := rd.fillOf(g, s, j), rd.borderOf(g, s, j)
			// A cone or pyramid to the maximum keeps the share of its
			// width that the axis's maximum leaves above its end.
			taper := 1.0
			switch g.Shape {
			case ShapeCone, ShapePyramid:
				taper = 0
			case ShapeConeToMax, ShapePyramidToMax:
				if l := math.Abs(top - v0); l > 0 {
					taper = math.Max(0, 1-math.Abs(v1-v0)/l)
				}
			case ShapeBox, ShapeCylinder:
			}
			round := g.Shape == ShapeCylinder || g.Shape == ShapeCone || g.Shape == ShapeConeToMax
			bar := bar3D{p: p, r: r, v0: v0, v1: v1, z0: z0, z1: z1, taper: taper, round: round}
			sl := solid{z: (z0 + z1) / 2, cat: (p0 + p1) / 2, val: (v0 + v1) / 2}
			sl.draw = func() { rd.drawBar3D(bar, fill, border) }
			ss = append(ss, sl)
			lb := rd.labelsAt(g, s, j)
			if lb.Any() {
				raw, neg := sv.raw[i][j], v < b
				stacked := g.Grouping != Clustered
				lr := p.face(r, z0)
				labels = append(labels, func() { rd.barLabel(g, s, j, raw, lb, lr, x.vertical, neg, stacked) })
			}
		}
	}
	return ss, labels
}

// face returns the page rectangle that holds front-plane rectangle r at
// depth z: r itself on the front plane of the oblique view.
func (p *projection) face(r typeset.Rect, z float64) typeset.Rect {
	if !p.turned {
		return r
	}
	a, b := p.at(r.X, r.Y, z), p.at(r.X+r.Width, r.Y+r.Height, z)
	c, e := p.at(r.X+r.Width, r.Y, z), p.at(r.X, r.Y+r.Height, z)
	x0, x1 := math.Min(math.Min(a.X, b.X), math.Min(c.X, e.X)), math.Max(math.Max(a.X, b.X), math.Max(c.X, e.X))
	y0, y1 := math.Min(math.Min(a.Y, b.Y), math.Min(c.Y, e.Y)), math.Max(math.Max(a.Y, b.Y), math.Max(c.Y, e.Y))
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// bar3D is one bar of a three-dimensional bar group: its front rectangle
// r on the front plane, the front-plane coordinates v0 of its base and v1
// of its end along the value direction, its depths, and the share taper
// of its width its end keeps.
type bar3D struct {
	p      *projection
	r      typeset.Rect
	v0, v1 float64
	z0, z1 float64
	taper  float64
	// round draws circular sections: a cylinder or a cone.
	round bool
}

// roundSteps is the number of sides of the polygon that draws a circular
// section.
const roundSteps = 32

// across returns the bar's middle along the category direction and its
// half width there, scaled by s.
func (b bar3D) across(s float64) (float64, float64) {
	if b.p.horizontal {
		return b.r.Y + float64(b.r.Height/2), b.r.Height / 2 * s
	}
	return b.r.X + float64(b.r.Width/2), b.r.Width / 2 * s
}

// point returns the front-plane point at value coordinate v, u along the
// category direction, depth z.
func (b bar3D) point(v, u, z float64) [3]float64 {
	if b.p.horizontal {
		return [3]float64{v, u, z}
	}
	return [3]float64{u, v, z}
}

// section returns the outline of the bar's section at value coordinate v,
// scaled by s about its axis.
func (b bar3D) section(v, s float64) []typeset.Point {
	p := b.p
	zc, hz := float64((b.z0+b.z1)/2), float64((b.z1-b.z0)/2*s)
	c, hw := b.across(s)
	at := func(u, z float64) typeset.Point {
		q := b.point(v, c+u, z)
		return p.at(q[0], q[1], q[2])
	}
	if !b.round {
		return []typeset.Point{at(-hw, zc-hz), at(hw, zc-hz), at(hw, zc+hz), at(-hw, zc+hz)}
	}
	pts := make([]typeset.Point, roundSteps)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / roundSteps
		pts[i] = at(float64(hw*detmath.Cos(a)), zc+float64(hz*detmath.Sin(a)))
	}
	return pts
}

// side returns the indices of the corners of a box section that bound the
// face along the bar's length the viewer sees from the side, and its
// shading: the right or left face of a column, the lower or upper face of
// a horizontal bar. A section's corners run front, then back, from the low
// side of the category direction to its high side: left to right for a
// column, top to bottom for a horizontal bar.
func (b bar3D) side() (int, int, float64) {
	c, _ := b.across(1)
	n := b.point(0, 1, 0)
	at := b.point(float64((b.v0+b.v1)/2), c, float64((b.z0+b.z1)/2))
	f := faceSide
	if b.p.horizontal {
		f = faceUp
	}
	if b.p.sees(n, at[0], at[1], at[2]) {
		return 1, 2, f
	}
	return 0, 3, f
}

// drawBar3D draws one bar: its silhouette or its front face, the face
// along its length that the viewer sees from the side, and the end that
// faces the viewer.
func (rd *renderer) drawBar3D(b bar3D, fill *Paint, border *Stroke) {
	p := b.p
	base, end := b.section(b.v0, 1), b.section(b.v1, b.taper)
	// The end the viewer sees: the upper one of a column seen from above,
	// the right one of a bar seen from the right.
	capSec, capScale, capShade := end, b.taper, faceUp
	if p.horizontal {
		capShade = faceSide
	}
	c, _ := b.across(1)
	n := b.point(b.v0-b.v1, 0, 0)
	at := b.point(b.v0, c, float64((b.z0+b.z1)/2))
	if p.sees(n, at[0], at[1], at[2]) {
		capSec, capScale = base, 1
	}
	if b.round || b.taper < 1 {
		rd.paint(polygon(hull(append(slices.Clone(base), end...))...), fill, border)
		if !b.round && b.taper < 1 {
			// The sloping face that the viewer sees from the side.
			i, j, f := b.side()
			rd.paint(polygon(base[i], base[j], end[j], end[i]), shaded(fill, f), border)
		}
		if capScale > 0 {
			rd.paint(polygon(capSec...), shaded(fill, capShade), border)
		}
		return
	}
	// A box: its front, its long side and its end.
	rd.paint(polygon(base[0], base[1], end[1], end[0]), fill, border)
	i, j, f := b.side()
	rd.paint(polygon(base[i], base[j], end[j], end[i]), shaded(fill, f), border)
	rd.paint(polygon(capSec...), shaded(fill, capShade), border)
}

// hull returns the convex hull of pts in order around it (Andrew's
// monotone chain), pts reordered.
func hull(pts []typeset.Point) []typeset.Point {
	if len(pts) < 3 {
		return pts
	}
	slices.SortFunc(pts, func(a, b typeset.Point) int {
		if a.X != b.X {
			if a.X < b.X {
				return -1
			}
			return 1
		}
		switch {
		case a.Y < b.Y:
			return -1
		case a.Y > b.Y:
			return 1
		}
		return 0
	})
	cross := func(o, a, b typeset.Point) float64 {
		return float64((a.X-o.X)*(b.Y-o.Y)) - float64((a.Y-o.Y)*(b.X-o.X))
	}
	out := make([]typeset.Point, 0, 2*len(pts))
	for _, pt := range pts {
		for len(out) >= 2 && cross(out[len(out)-2], out[len(out)-1], pt) <= 0 {
			out = out[:len(out)-1]
		}
		out = append(out, pt)
	}
	lower := len(out) + 1
	for i := len(pts) - 2; i >= 0; i-- {
		pt := pts[i]
		for len(out) >= lower && cross(out[len(out)-2], out[len(out)-1], pt) <= 0 {
			out = out[:len(out)-1]
		}
		out = append(out, pt)
	}
	return out[:len(out)-1]
}

// strip returns the face swept by the segment from a to b on the front
// plane between depths z0 and z1, and its shading: turned up when the
// viewer sees its upper side.
func (p *projection) strip(a, b typeset.Point, z0, z1 float64) ([]typeset.PathOp, float64) {
	ops := polygon(p.at(a.X, a.Y, z0), p.at(b.X, b.Y, z0), p.at(b.X, b.Y, z1), p.at(a.X, a.Y, z1))
	// The normal of the face on the left of the segment, upward for a
	// segment drawn to the right.
	n := [3]float64{b.Y - a.Y, a.X - b.X, 0}
	if p.sees(n, float64((a.X+b.X)/2), float64((a.Y+b.Y)/2), float64((z0+z1)/2)) {
		return ops, faceUp
	}
	return ops, faceSide
}

// lines3D lays out a line group as ribbons: each series a band as deep as
// a bar along its line, in its row.
func (rd *renderer) lines3D(p *projection, g *Group, x, y *dim) []solid {
	sv := rd.stack(g)
	var ss []solid
	for i, s := range g.Series {
		row := 0
		if g.Rows {
			row = i
		}
		z0, z1 := p.row(row)
		paint := rd.fillOf(g, s, -1)
		if st := rd.borderOf(g, s, -1); st != nil && !st.None {
			paint = &Paint{Color: st.Color}
		}
		var prev *typeset.Point
		for j := 0; j < sv.n && j < x.n; j++ {
			v := sv.hi[i][j]
			if math.IsNaN(v) {
				prev = nil
				continue
			}
			pt := point(x, y, float64(j), y.sc.clamp(v))
			if prev != nil {
				a, b := *prev, pt
				ops, f := p.strip(a, b, z0, z1)
				ss = append(ss, solid{z: (z0 + z1) / 2, cat: (a.X + b.X) / 2, draw: func() {
					rd.paint(ops, shaded(paint, f), nil)
				}})
			}
			prev = &pt
		}
	}
	return ss
}

// areas3D lays out an area group as slabs: each series its area, as deep
// as a bar, in its row: the strips along its upper edge, the end the
// viewer sees, then its front.
func (rd *renderer) areas3D(p *projection, g *Group, x, y *dim) []solid {
	sv := rd.stack(g)
	base := y.sc.clamp(x.crossValue())
	var ss []solid
	for i, s := range g.Series {
		row := 0
		if g.Rows {
			row = i
		}
		z0, z1 := p.row(row)
		var top, bottom []typeset.Point
		for j := 0; j < sv.n && j < x.n; j++ {
			v, b := sv.hi[i][j], sv.lo[i][j]
			if math.IsNaN(v) {
				v = 0
				if g.Grouping != Clustered && i > 0 {
					if v = sv.hi[i-1][j]; math.IsNaN(v) {
						v = 0
					}
				}
			}
			if math.IsNaN(b) {
				b = base
			}
			top = append(top, point(x, y, float64(j), y.sc.clamp(v)))
			bottom = append(bottom, point(x, y, float64(j), y.sc.clamp(b)))
		}
		if len(top) < 2 {
			continue
		}
		fill, border := rd.fillOf(g, s, -1), rd.borderOf(g, s, -1)
		var front []typeset.Point
		front = append(front, top...)
		for _, b := range slices.Backward(bottom) {
			front = append(front, b)
		}
		ss = append(ss, solid{z: (z0 + z1) / 2, draw: func() {
			for j := 1; j < len(top); j++ {
				ops, f := p.strip(top[j-1], top[j], z0, z1)
				rd.paint(ops, shaded(fill, f), border)
			}
			e := len(top) - 1
			if !p.sees([3]float64{1, 0, 0}, top[e].X, top[e].Y, float64((z0+z1)/2)) {
				e = 0
			}
			ops := polygon(p.at(bottom[e].X, bottom[e].Y, z0), p.at(top[e].X, top[e].Y, z0), p.at(top[e].X, top[e].Y, z1), p.at(bottom[e].X, bottom[e].Y, z1))
			rd.paint(ops, shaded(fill, faceSide), border)
			fr := make([]typeset.Point, len(front))
			for k, q := range front {
				fr[k] = p.at(q.X, q.Y, z0)
			}
			rd.paint(polygon(fr...), fill, border)
		}})
	}
	return ss
}

// moveLabels carries the tick labels of d from the front plane into the
// view: each keeps its offset from the point of the axis line it labels,
// which the projection moves. Office sets the labels of a turned plot
// upright beside the projected edges.
func (p *projection) moveLabels(d *dim) {
	for i := range d.labels {
		l := &d.labels[i]
		ax, ay := l.pos, d.lineAt
		if d.vertical {
			ax, ay = d.lineAt, l.pos
		}
		q := p.at(ax, ay, 0)
		l.cx += q.X - ax
		l.cy += q.Y - ay
	}
}

// rowAxis returns the series axis that names the rows of g, nil when it
// draws no labels.
func (rd *renderer) rowAxis(p *projection, g *Group) *Axis {
	a := rd.c.axisAt(g.ZAxis)
	if !g.Rows || a == nil || a.Kind != SeriesAxis || a.Deleted || a.Labels == LabelNone || p.horizontal {
		return nil
	}
	return a
}

// rowTicks reports a series axis that draws its tick marks outside.
func rowTicks(a *Axis) bool {
	return a.Line != nil && (a.MajorTicks == TickOut || a.MajorTicks == TickCross)
}

// rowLabelsWidth returns the width the names of g's rows take beside the
// floor, their gap and tick marks included, 0 when it draws none.
//
// # Corpus evidence
//
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/Chart_3D_Column_.pptx,
//     turned view: PowerPoint keeps 65.7 pt between the plot area and the
//     room of its legend for names 56.7 pt wide.
func (rd *renderer) rowLabelsWidth(p *projection, g *Group) float64 {
	a := rd.rowAxis(p, g)
	if a == nil {
		return 0
	}
	w := 0.0
	for _, s := range g.Series {
		if b := rd.measure(s.Name, a.Style, 0); b != nil {
			bw, _ := b.rotated()
			w = math.Max(w, bw)
		}
	}
	if w == 0 {
		return 0
	}
	if p.turned && rowTicks(a) {
		w += tickLength
	}
	return w + labelSpacing
}

// rowLabels draws the names of the rows of g along the floor's side edge,
// as the series axis labels them: the edge across from the side wall. The
// turned view marks the rows' bounds and parts the names from the edge as
// Office parts the labels of an axis beside the plot area.
//
// # Corpus evidence
//
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/Chart_3D_Column_.pptx:
//     PowerPoint draws tick marks at the rows' bounds and sets each name
//     16.9 pt right of the edge, centred on its row, as it sets the value
//     labels 16.9 pt left of theirs (officeSideLabelGap of 18 pt).
func (rd *renderer) rowLabels(p *projection, g *Group, f typeset.Rect) {
	a := rd.rowAxis(p, g)
	if a == nil {
		return
	}
	edge := f.X + f.Width
	al, dir := alignLeft, 1.0
	if !p.sees([3]float64{1, 0, 0}, f.X, f.Y+float64(f.Height/2), float64(p.depth/2)) {
		// The side wall stands on the right.
		edge, al, dir = f.X, alignRight, -1
	}
	gap := labelSpacing
	if p.turned {
		gap = float64(officeSideLabelGap * fontSize(a.Style.Size))
		if rowTicks(a) {
			for i := 0; i <= len(g.Series); i++ {
				q := p.at(edge, f.Y+f.Height, float64(float64(i)*p.rowDepth))
				rd.add(line(q.X, q.Y, q.X+float64(dir*tickLength), q.Y, a.Line))
			}
		}
	}
	for i, s := range g.Series {
		z0, z1 := p.row(i)
		b := rd.measure(s.Name, a.Style, 0)
		if b == nil {
			continue
		}
		w, _ := b.rotated()
		pt := p.at(edge, f.Y+f.Height, (z0+z1)/2)
		rd.out = append(rd.out, b.draw(pt.X+float64(dir*(gap+float64(w/2))), pt.Y, al)...)
	}
}

// pie3D is the tilt of a three-dimensional pie: its height over its width
// and its rim's thickness over its radius.
type pie3D struct {
	tilt, rim float64
}

// pieView returns the tilt of a three-dimensional pie group, nil for a
// flat one. A pie seen at rotX degrees is an ellipse whose height is the
// width times the sine of rotX, and its rim pieHeight of the radius times
// the cosine; a perspective flattens the ellipse by a twentieth at 30.
//
// # Corpus evidence
//
//   - poi/xlsx: spreadsheet/45544.xlsx (rotX 15, perspective 0): height over
//     width 0.258, sin 15 = 0.259; rim 0.2375 of the radius.
//   - litho/pptx: chart-pie (rotX 30, perspective 30): 0.469; rim 0.233.
//   - openxmlsdk/pptx: 3D_Pie-O12-PPT-Charts.pptx (rotX 30, perspective 30):
//     0.478; rim 0.22.
func (rd *renderer) pieView(g *Group) *pie3D {
	v := rd.c.View3D
	if v == nil || !g.ThreeD || g.Kind != Pie {
		return nil
	}
	rx := clampFinite(v.RotX, 0, 90)
	persp := 0.0
	if !v.RightAngle {
		persp = clampFinite(v.Perspective, 0, 240)
	}
	a := rx * math.Pi / 180
	return &pie3D{
		tilt: detmath.Sin(a) * (1 - persp/600),
		rim:  pieHeight * detmath.Cos(a) * v.depthPercent() / 100,
	}
}

// tiltOps returns ops squeezed vertically by k about the line y = cy.
func tiltOps(ops []typeset.PathOp, cy, k float64) []typeset.PathOp {
	out := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		out[i] = op
		for j := range op.P {
			out[i].P[j].Y = cy + float64((op.P[j].Y-cy)*k)
		}
	}
	return out
}

// rim returns the outline of the rim of a tilted pie's sector of centre
// (cx, cy) and radius r between angles a0 and a1 (counter-clockwise, y
// up), the part of it in front, below the centre: nil when none is.
func (t *pie3D) rimOps(cx, cy, r, a0, a1 float64) [][]typeset.PathOp {
	lo, hi := math.Min(a0, a1), math.Max(a0, a1)
	h := float64(t.rim * r)
	var out [][]typeset.PathOp
	// The front half turns from pi to 2 pi, once a turn.
	for m := math.Floor(lo/(2*math.Pi)) - 1; m <= math.Ceil(hi/(2*math.Pi)); m++ {
		s := math.Max(lo, math.Pi+float64(2*math.Pi*m))
		e := math.Min(hi, 2*math.Pi+float64(2*math.Pi*m))
		if e-s < 1e-9 {
			continue
		}
		start := typeset.Point{X: cx + float64(r*detmath.Cos(s)), Y: cy - float64(r*detmath.Sin(s))}
		up := arc([]typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{start}}}, cx, cy, r, s, e)
		up = tiltOps(up, cy, t.tilt)
		end := typeset.Point{X: cx + float64(r*detmath.Cos(e)), Y: cy + h - float64(r*detmath.Sin(e)*t.tilt)}
		down := tiltOps(arc(nil, cx, cy+h, r, e, s), cy+h, t.tilt)
		up = append(up, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{end}})
		up = append(up, down...)
		out = append(out, append(up, typeset.PathOp{Kind: typeset.ClosePath}))
	}
	return out
}
