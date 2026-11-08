package shape

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// emuPerPoint is the number of EMUs in a point (ECMA-376 Part 1, 20.1.2.1:
// 12,700 EMUs to the point), the unit of DrawingML's guide space.
const emuPerPoint = 12700

// hundredthMMPerPoint is the number of 1/100 mm in a point, the unit of
// ODF's logwidth and logheight.
const hundredthMMPerPoint = 2540.0 / 72

// The evaluation states of a guide.
const (
	guidePending uint8 = iota
	guideBusy
	guideDone
)

// evaluator computes one geometry's guides for one box and maps its
// coordinates onto the box.
type evaluator struct {
	def      *Custom
	vals     []float64
	state    []uint8
	builtins [builtinCount]float64
	depth    int
	// problem records a cycle, a chain past MaxGuideDepth or a reference
	// out of range.
	problem string
	// gx, gy, gsx and gsy map guide space onto the box: x = gx + v*gsx.
	gx, gy, gsx, gsy float64
	box              compose.Rect
	mirrorH, mirrorV bool
}

// newEvaluator prepares def's guides for the box r, the adjustments of g
// overriding the defaults by name.
func newEvaluator(def *Custom, g *Geometry, r compose.Rect) *evaluator {
	nAdj := min(len(def.Adjust), MaxGuides)
	nGd := min(len(def.Guides), MaxGuides-nAdj)
	ev := &evaluator{
		def:   def,
		vals:  make([]float64, nAdj+nGd),
		state: make([]uint8, nAdj+nGd),
		box:   r,
	}
	if g != nil {
		ev.mirrorH, ev.mirrorV = g.MirrorH, g.MirrorV
	}
	var l, t, w, h float64
	if v := def.View; v != nil {
		l, t, w, h = v.X, v.Y, v.Width, v.Height
		ev.gx, ev.gy = r.X-l*ratio(r.Width, w), r.Y-t*ratio(r.Height, h)
		ev.gsx, ev.gsy = ratio(r.Width, w), ratio(r.Height, h)
	} else {
		w, h = r.Width*emuPerPoint, r.Height*emuPerPoint
		ev.gx, ev.gy = r.X, r.Y
		ev.gsx, ev.gsy = 1.0/emuPerPoint, 1.0/emuPerPoint
	}
	ss, ls := math.Min(w, h), math.Max(w, h)
	b := &ev.builtins
	b[BuiltinL], b[BuiltinT], b[BuiltinR], b[BuiltinB] = l, t, l+w, t+h
	b[BuiltinW], b[BuiltinH] = w, h
	b[BuiltinHC], b[BuiltinVC] = l+w/2, t+h/2
	b[BuiltinSS], b[BuiltinLS] = ss, ls
	b[BuiltinCD2], b[BuiltinCD4], b[BuiltinCD8] = 10800000, 5400000, 2700000
	b[Builtin3CD4], b[Builtin3CD8], b[Builtin5CD8], b[Builtin7CD8] = 16200000, 8100000, 13500000, 18900000
	b[BuiltinWD2], b[BuiltinWD3], b[BuiltinWD4], b[BuiltinWD5] = w/2, w/3, w/4, w/5
	b[BuiltinWD6], b[BuiltinWD8], b[BuiltinWD10], b[BuiltinWD12], b[BuiltinWD32] = w/6, w/8, w/10, w/12, w/32
	b[BuiltinHD2], b[BuiltinHD3], b[BuiltinHD4], b[BuiltinHD5], b[BuiltinHD6], b[BuiltinHD8] = h/2, h/3, h/4, h/5, h/6, h/8
	b[BuiltinSSD2], b[BuiltinSSD4], b[BuiltinSSD6], b[BuiltinSSD8] = ss/2, ss/4, ss/6, ss/8
	b[BuiltinSSD16], b[BuiltinSSD32] = ss/16, ss/32
	b[BuiltinLogWidth], b[BuiltinLogHeight] = r.Width*hundredthMMPerPoint, r.Height*hundredthMMPerPoint
	b[BuiltinHasFill], b[BuiltinHasStroke] = 1, 1
	for i := range nAdj {
		if g == nil {
			break
		}
		for _, a := range g.Adjust {
			if a.Name != "" && a.Name == def.Adjust[i].Name && finite(a.Value) {
				ev.vals[i], ev.state[i] = a.Value, guideDone
			}
		}
	}
	for i := range ev.vals {
		ev.guide(i)
	}
	return ev
}

// ratio returns a / b, 0 when b is 0.
func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// finite reports whether v is neither NaN nor infinite.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// guide returns the value of guide i, evaluating it on first use.
func (ev *evaluator) guide(i int) float64 {
	if i < 0 || i >= len(ev.vals) {
		ev.fail(fmt.Sprintf("guide %d out of range", i))
		return 0
	}
	switch ev.state[i] {
	case guideDone:
		return ev.vals[i]
	case guideBusy:
		ev.fail("a guide reference cycle")
		return 0
	}
	if ev.depth >= MaxGuideDepth {
		ev.fail(fmt.Sprintf("guide references deeper than %d", MaxGuideDepth))
		return 0
	}
	ev.state[i] = guideBusy
	ev.depth++
	var gd *Guide
	if i < len(ev.def.Adjust) {
		gd = &ev.def.Adjust[i]
	} else {
		gd = &ev.def.Guides[i-len(ev.def.Adjust)]
	}
	v := apply(gd.Op, ev.arg(gd.Args[0]), ev.arg(gd.Args[1]), ev.arg(gd.Args[2]))
	ev.depth--
	if !finite(v) {
		v = 0
	}
	ev.vals[i], ev.state[i] = v, guideDone
	return v
}

// fail records the first evaluation problem.
func (ev *evaluator) fail(msg string) {
	if ev.problem == "" {
		ev.problem = msg
	}
}

// arg returns the value of the operand a.
func (ev *evaluator) arg(a Arg) float64 {
	switch a.Kind {
	case ArgConst:
		return a.Value
	case ArgGuide:
		return ev.guide(a.Ref)
	case ArgBuiltin:
		if a.Ref >= 0 && a.Ref < int(builtinCount) {
			return ev.builtins[a.Ref]
		}
	}
	return 0
}

// guidePoint maps the guide-space point (x, y) onto the box.
func (ev *evaluator) guidePoint(x, y Arg) (float64, float64) {
	return ev.mirror(ev.gx+ev.arg(x)*ev.gsx, ev.gy+ev.arg(y)*ev.gsy)
}

// mirror applies the geometry's mirroring to a box point.
func (ev *evaluator) mirror(x, y float64) (float64, float64) {
	if ev.mirrorH {
		x = 2*ev.box.X + ev.box.Width - x
	}
	if ev.mirrorV {
		y = 2*ev.box.Y + ev.box.Height - y
	}
	return x, y
}

// pathBuilder accumulates one path's operations in path space and maps
// them onto the box.
type pathBuilder struct {
	ev                  *evaluator
	ops                 []compose.PathOp
	ox, oy, sx, sy      float64
	cx, cy, startX, sy0 float64
	open                bool
}

// path evaluates the commands cmds of pd into box operations.
func (ev *evaluator) path(pd *PathDef, cmds []Cmd) []compose.PathOp {
	pb := &pathBuilder{ev: ev, ox: ev.gx, oy: ev.gy, sx: ev.gsx, sy: ev.gsy}
	if ev.def.View == nil && pd.W > 0 && pd.H > 0 {
		pb.ox, pb.oy = ev.box.X, ev.box.Y
		pb.sx, pb.sy = ev.box.Width/pd.W, ev.box.Height/pd.H
	} else if ev.def.View == nil && (pd.W > 0 || pd.H > 0) {
		// One of w and h alone: the other is the guide space's
		// (20.1.9.15 path: w and h default to 0, the shape's own).
		pb.ox, pb.oy = ev.box.X, ev.box.Y
		if pd.W > 0 {
			pb.sx = ev.box.Width / pd.W
		}
		if pd.H > 0 {
			pb.sy = ev.box.Height / pd.H
		}
	}
	for i := range cmds {
		pb.cmd(&cmds[i])
	}
	return pb.ops
}

// at returns operand i of args, 0 past the end.
func (pb *pathBuilder) at(args []Arg, i int) float64 {
	if i < len(args) {
		return pb.ev.arg(args[i])
	}
	return 0
}

// emit appends an operation of kind k over path-space points.
func (pb *pathBuilder) emit(k compose.OpKind, pts ...float64) {
	var op compose.PathOp
	op.Kind = k
	for i := 0; i+1 < len(pts) && i/2 < 3; i += 2 {
		x, y := pb.ev.mirror(pb.ox+pts[i]*pb.sx, pb.oy+pts[i+1]*pb.sy)
		op.P[i/2] = compose.Point{X: x, Y: y}
	}
	pb.ops = append(pb.ops, op)
}

// moveTo starts a subpath at (x, y).
func (pb *pathBuilder) moveTo(x, y float64) {
	pb.emit(compose.MoveTo, x, y)
	pb.cx, pb.cy, pb.startX, pb.sy0 = x, y, x, y
	pb.open = true
}

// ensure opens a subpath at the current point when none is open, so a
// drawing command never comes first.
func (pb *pathBuilder) ensure() {
	if !pb.open {
		pb.moveTo(pb.cx, pb.cy)
	}
}

// lineTo draws a line to (x, y).
func (pb *pathBuilder) lineTo(x, y float64) {
	pb.ensure()
	pb.emit(compose.LineTo, x, y)
	pb.cx, pb.cy = x, y
}

// curveTo draws a cubic Bézier.
func (pb *pathBuilder) curveTo(x1, y1, x2, y2, x, y float64) {
	pb.ensure()
	pb.emit(compose.CurveTo, x1, y1, x2, y2, x, y)
	pb.cx, pb.cy = x, y
}

// cmd evaluates one command.
func (pb *pathBuilder) cmd(c *Cmd) {
	a := func(i int) float64 { return pb.at(c.Args, i) }
	switch c.Kind {
	case CmdMove:
		pb.moveTo(a(0), a(1))
	case CmdLine:
		pb.lineTo(a(0), a(1))
	case CmdCubic:
		pb.curveTo(a(0), a(1), a(2), a(3), a(4), a(5))
	case CmdQuad:
		x0, y0, qx, qy, x, y := pb.cx, pb.cy, a(0), a(1), a(2), a(3)
		pb.curveTo(x0+2*(qx-x0)/3, y0+2*(qy-y0)/3, x+2*(qx-x)/3, y+2*(qy-y)/3, x, y)
	case CmdClose:
		if pb.open {
			pb.ops = append(pb.ops, compose.PathOp{Kind: compose.ClosePath})
			pb.cx, pb.cy = pb.startX, pb.sy0
			pb.open = false
		}
	case CmdArc:
		pb.arcTo(a(0), a(1), angle60k(a(2)), angle60k(a(3)))
	case CmdAngleEllipseTo, CmdAngleEllipse:
		pb.angleEllipse(a(0), a(1), a(2), a(3), a(4), a(5), c.Kind == CmdAngleEllipse)
	case CmdArcTo, CmdArcMove, CmdClockwiseArcTo, CmdClockwiseArcMove:
		pb.arcRect(a, c.Kind == CmdArcMove || c.Kind == CmdClockwiseArcMove,
			c.Kind == CmdClockwiseArcTo || c.Kind == CmdClockwiseArcMove)
	case CmdQuadrantX, CmdQuadrantY:
		x0, y0, x, y := pb.cx, pb.cy, a(0), a(1)
		if c.Kind == CmdQuadrantX {
			pb.curveTo(x0+kappa*(x-x0), y0, x, y+kappa*(y0-y), x, y)
		} else {
			pb.curveTo(x0, y0+kappa*(y-y0), x+kappa*(x0-x), y, x, y)
		}
	}
}

// kappa places the control points of a cubic Bézier quarter ellipse.
const kappa = 0.5522847498

// arcTo draws the DrawingML arc of radii wR and hR from the current point,
// which sits at angle st on the ellipse, sweeping sw (ECMA-376 Part 1,
// 20.1.9.3 arcTo). The angles are those of the visible ellipse, clockwise
// from the positive x axis in the y-down space; they are turned into the
// ellipse's parametric angles as LibreOffice's custom shape engine does,
// t = atan2(wR·sin a, hR·cos a), the sweep keeping its sign and a sweep
// of a full turn or more drawing the whole ellipse once.
func (pb *pathBuilder) arcTo(wR, hR, st, sw float64) {
	pb.ensure()
	if wR == 0 || hR == 0 || sw == 0 {
		return
	}
	t0 := math.Atan2(wR*math.Sin(st), hR*math.Cos(st))
	var dt float64
	if math.Abs(sw) >= 2*math.Pi {
		dt = math.Copysign(2*math.Pi, sw)
	} else {
		t1 := math.Atan2(wR*math.Sin(st+sw), hR*math.Cos(st+sw))
		dt = t1 - t0
		if sw > 0 && dt <= 0 {
			dt += 2 * math.Pi
		} else if sw < 0 && dt >= 0 {
			dt -= 2 * math.Pi
		}
	}
	cx, cy := pb.cx-wR*math.Cos(t0), pb.cy-hR*math.Sin(t0)
	pb.ellipse(cx, cy, wR, hR, t0, dt)
}

// ellipse draws the arc of the ellipse of centre (cx, cy) and radii rx,
// ry from parametric angle t0 sweeping dt, y down, as cubic Béziers of at
// most a quarter turn each, from the current point.
func (pb *pathBuilder) ellipse(cx, cy, rx, ry, t0, dt float64) {
	n := int(math.Ceil(math.Abs(dt)/(math.Pi/2) - 1e-9))
	n = max(1, min(n, 4))
	step := dt / float64(n)
	alpha := 4.0 / 3 * math.Tan(step/4)
	a := t0
	for range n {
		b := a + step
		ca, sa, cb, sb := math.Cos(a), math.Sin(a), math.Cos(b), math.Sin(b)
		pb.curveTo(
			cx+rx*ca-alpha*rx*sa, cy+ry*sa+alpha*ry*ca,
			cx+rx*cb+alpha*rx*sb, cy+ry*sb-alpha*ry*cb,
			cx+rx*cb, cy+ry*sb)
		a = b
	}
}

// angleEllipse draws the ODF angle-ellipse of centre (x, y) and radii (w,
// h) from t0 to t1 degrees, counter-clockwise on the page (ODF 1.3 Part 3,
// 19.145, T and U), from a line to its start or a new subpath.
func (pb *pathBuilder) angleEllipse(x, y, w, h, t0, t1 float64, move bool) {
	sweep := math.Mod(t1-t0, 360)
	if sweep <= 0 && t1 != t0 {
		sweep += 360
	}
	if t1 == t0 {
		return
	}
	// Counter-clockwise on the y-down page is a falling parametric angle.
	a0 := -t0 * math.Pi / 180
	sx, sy := x+w*math.Cos(a0), y+h*math.Sin(a0)
	if move {
		pb.moveTo(sx, sy)
	} else {
		pb.lineTo(sx, sy)
	}
	pb.ellipse(x, y, w, h, a0, -sweep*math.Pi/180)
}

// arcRect draws the ODF arc of the ellipse inscribed in a box, between
// the rays through two points (ODF 1.3 Part 3, 19.145, A, B, W and V).
func (pb *pathBuilder) arcRect(a func(int) float64, move, clockwise bool) {
	x1, y1, x2, y2 := a(0), a(1), a(2), a(3)
	cx, cy, rx, ry := (x1+x2)/2, (y1+y2)/2, math.Abs(x2-x1)/2, math.Abs(y2-y1)/2
	t0 := math.Atan2((a(5)-cy)*rx, (a(4)-cx)*ry)
	t1 := math.Atan2((a(7)-cy)*rx, (a(6)-cx)*ry)
	sx, sy := cx+rx*math.Cos(t0), cy+ry*math.Sin(t0)
	if move {
		pb.moveTo(sx, sy)
	} else {
		pb.lineTo(sx, sy)
	}
	if rx == 0 || ry == 0 {
		return
	}
	dt := t1 - t0
	if clockwise {
		if dt <= 0 {
			dt += 2 * math.Pi
		}
	} else if dt >= 0 {
		dt -= 2 * math.Pi
	}
	pb.ellipse(cx, cy, rx, ry, t0, dt)
}

// ErrUnresolved reports a geometry whose guides do not all resolve: a
// name the reader did not know, a reference cycle, or a chain of
// references deeper than [MaxGuideDepth]. Each such value evaluates to 0.
var ErrUnresolved = errors.New("shape: unresolved geometry guides")

// Check evaluates g's guides in a unit box and returns nil when every
// one resolves, else an error wrapping [ErrUnresolved] that names the
// problem, for the caller's warnings. A preset of the table always
// resolves; an unknown preset is not an error, Paths draws its box.
func Check(g *Geometry) error {
	def := definition(g)
	if def == nil {
		return nil
	}
	ev := newEvaluator(def, g, compose.Rect{Width: 100, Height: 100})
	var probs []string
	if len(def.Unresolved) > 0 {
		probs = append(probs, "unknown names "+strings.Join(def.Unresolved, ", "))
	}
	if ev.problem != "" {
		probs = append(probs, ev.problem)
	}
	if len(probs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrUnresolved, strings.Join(probs, "; "))
}
