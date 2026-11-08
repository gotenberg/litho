package odf

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// maxPathDataLen caps the length of one svg:d or draw:points attribute
// read. Anti-DoS guard, not spec-mandated: a freehand line of thousands
// of points stays under a megabyte.
const maxPathDataLen = 1 << 20

// maxPolygonCorners caps draw:corners. Anti-DoS guard, not
// spec-mandated: Impress's dialog stops at 100 corners.
const maxPolygonCorners = 1024

// PathData reads svg:d path data (ODF 1.3 Part 3, 19.530 svg:d, whose
// syntax is SVG 1.1, 8.3) as a custom geometry in the coordinate space
// view: every command, absolute and relative, H and V as lines, the
// smooth curves S and T with their reflected control points (8.3.6,
// 8.3.7), and the elliptical arcs A as cubic Béziers of at most a quarter
// turn (SVG 1.1, F.6). The geometry has one path, filled and stroked,
// whose open subpaths are stroked only, as LibreOffice draws a path
// shape. On an error it keeps the path up to the last complete command,
// as SVG 1.1, F.2 asks. It returns nil for data that draws nothing.
func PathData(d string, view compose.Rect) *shape.Custom {
	if len(d) > maxPathDataLen {
		return nil
	}
	cmds := parsePathData(d)
	if len(cmds) == 0 {
		return nil
	}
	return &shape.Custom{View: &view, OpenUnfilled: true, Paths: []shape.PathDef{{Stroke: true, Cmds: cmds}}}
}

// Points reads draw:points (ODF 1.3 Part 3, 19.206), x,y pairs separated
// by white space, as a custom geometry in the coordinate space view: a
// closed polygon (10.3.5 draw:polygon) or an open polyline (10.3.4
// draw:polyline), which is stroked only. A pair that does not parse ends
// the list. It returns nil for fewer than two points.
func Points(points string, view compose.Rect, closed bool) *shape.Custom {
	if len(points) > maxPathDataLen {
		return nil
	}
	sc := pathScanner{s: points}
	var cmds []shape.Cmd
	for len(cmds) < shape.MaxCommands {
		x, okX := sc.number()
		y, okY := sc.number()
		if !okX || !okY {
			break
		}
		kind := shape.CmdLine
		if len(cmds) == 0 {
			kind = shape.CmdMove
		}
		cmds = append(cmds, cmdOf(kind, x, y))
	}
	if len(cmds) < 2 {
		return nil
	}
	pd := shape.PathDef{Stroke: true, Cmds: cmds}
	if closed {
		pd.Cmds = append(pd.Cmds, shape.Cmd{Kind: shape.CmdClose})
	} else {
		pd.Fill = shape.FillNone
	}
	return &shape.Custom{View: &view, Paths: []shape.PathDef{pd}}
}

// RegularPolygon returns the outline of a draw:regular-polygon (ODF 1.3
// Part 3, 10.3.6) of corners corners: a regular convex polygon, or with
// concave a star of twice as many corners whose inner corners lie at
// sharpness percent of the way from the outer circle to the centre
// (19.211 draw:sharpness). The first corner of a convex polygon or the
// first outer corner of a star with an odd count is at the top, of a star
// with an even count the first inner corner is (19.211); the model is
// scaled so its bounding box fills the shape's (19.123 draw:concave). It
// returns nil for fewer than 3 corners.
func RegularPolygon(corners int, concave bool, sharpness float64) *shape.Custom {
	if corners < 3 || corners > maxPolygonCorners {
		return nil
	}
	step := 2 * math.Pi / float64(corners)
	start := -math.Pi / 2
	n := corners
	inner := 1.0
	if concave {
		n *= 2
		// T lies on MR, R the midpoint of the arc PQ at radius 1, at
		// (1 - s%) of its length.
		inner = 1 - clamp01(sharpness/100)
		if corners%2 == 0 {
			start -= step / 2
		}
	}
	pts := make([][2]float64, n)
	for i := range n {
		a, r := start+float64(i)*step, 1.0
		if concave {
			a = start + float64(i)*step/2
			if i%2 == 1 {
				r = inner
			}
		}
		pts[i] = [2]float64{r * math.Cos(a), r * math.Sin(a)}
	}
	cmds := make([]shape.Cmd, 0, n+1)
	for i, p := range pts {
		kind := shape.CmdLine
		if i == 0 {
			kind = shape.CmdMove
		}
		cmds = append(cmds, cmdOf(kind, p[0], p[1]))
	}
	cmds = append(cmds, shape.Cmd{Kind: shape.CmdClose})
	view, _ := Bounds(cmds)
	return &shape.Custom{View: &view, Paths: []shape.PathDef{{Stroke: true, Cmds: cmds}}}
}

// Bounds returns the bounding box of the points and control points of
// cmds, which PathData, Points and RegularPolygon write with literal
// operands, and whether there was a point.
func Bounds(cmds []shape.Cmd) (compose.Rect, bool) {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range cmds {
		for i := 0; i+1 < len(c.Args); i += 2 {
			x, y := c.Args[i].Value, c.Args[i+1].Value
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	if x0 > x1 {
		return compose.Rect{}, false
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

// cmdOf returns a command of literal operands.
func cmdOf(kind shape.CmdKind, v ...float64) shape.Cmd {
	args := make([]shape.Arg, len(v))
	for i, x := range v {
		args[i] = shape.Arg{Value: x}
	}
	return shape.Cmd{Kind: kind, Args: args}
}

// pathScanner reads the numbers and flags of path data (SVG 1.1, 8.3.9).
type pathScanner struct {
	s   string
	pos int
}

// skip skips white space and one comma.
func (sc *pathScanner) skip() {
	comma := false
	for sc.pos < len(sc.s) {
		switch c := sc.s[sc.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
		case c == ',' && !comma:
			comma = true
		default:
			return
		}
		sc.pos++
	}
}

// number reads a number: a sign, digits with a decimal point, an
// exponent; the next one may start at a sign or a second point.
func (sc *pathScanner) number() (float64, bool) {
	sc.skip()
	start := sc.pos
	i := sc.pos
	if i < len(sc.s) && (sc.s[i] == '+' || sc.s[i] == '-') {
		i++
	}
	digits, dot := 0, false
scan:
	for ; i < len(sc.s); i++ {
		switch c := sc.s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot:
			dot = true
		default:
			break scan
		}
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(sc.s) && (sc.s[i] == 'e' || sc.s[i] == 'E') {
		j := i + 1
		if j < len(sc.s) && (sc.s[j] == '+' || sc.s[j] == '-') {
			j++
		}
		k := j
		for k < len(sc.s) && sc.s[k] >= '0' && sc.s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	v, err := strconv.ParseFloat(sc.s[start:i], 64)
	if err != nil || !finite(v) {
		return 0, false
	}
	sc.pos = i
	return v, true
}

// flag reads an arc flag, 0 or 1, which needs no separator (8.3.9).
func (sc *pathScanner) flag() (bool, bool) {
	sc.skip()
	if sc.pos < len(sc.s) && (sc.s[sc.pos] == '0' || sc.s[sc.pos] == '1') {
		sc.pos++
		return sc.s[sc.pos-1] == '1', true
	}
	return false, false
}

// numbers reads n numbers into v.
func (sc *pathScanner) numbers(v []float64) bool {
	for i := range v {
		x, ok := sc.number()
		if !ok {
			return false
		}
		v[i] = x
	}
	return true
}

// atNumber reports whether a number follows, which repeats the command.
func (sc *pathScanner) atNumber() bool {
	sc.skip()
	return sc.pos < len(sc.s) && strings.IndexByte("+-.0123456789", sc.s[sc.pos]) >= 0
}

// parsePathData parses svg:d into commands of absolute literal operands.
func parsePathData(d string) []shape.Cmd {
	sc := pathScanner{s: d}
	var out []shape.Cmd
	var cx, cy, sx, sy float64 // current point, subpath start
	var lastCtl [2]float64     // last control point, for S and T
	var prev byte
	add := func(c shape.Cmd) bool {
		if len(out) >= shape.MaxCommands {
			return false
		}
		out = append(out, c)
		return true
	}
	var cmd byte
	for {
		sc.skip()
		if sc.pos >= len(sc.s) {
			return out
		}
		if c := sc.s[sc.pos]; len(out) == 0 && c != 'M' && c != 'm' {
			// Path data starts with a move (SVG 1.1, 8.3.2).
			return nil
		} else if strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0 {
			cmd = c
			sc.pos++
		} else if cmd == 0 || cmd == 'Z' || cmd == 'z' || !sc.atNumber() {
			return out
		}
		rel := cmd >= 'a'
		ox, oy := 0.0, 0.0
		if rel {
			ox, oy = cx, cy
		}
		var v [7]float64
		switch cmd {
		case 'M', 'm':
			if !sc.numbers(v[:2]) || !add(cmdOf(shape.CmdMove, ox+v[0], oy+v[1])) {
				return out
			}
			cx, cy = ox+v[0], oy+v[1]
			sx, sy = cx, cy
			// Further pairs are lines (8.3.2).
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
			prev = 'M'
			continue
		case 'L', 'l':
			if !sc.numbers(v[:2]) {
				return out
			}
			cx, cy = ox+v[0], oy+v[1]
		case 'H', 'h':
			if !sc.numbers(v[:1]) {
				return out
			}
			cx = ox + v[0]
		case 'V', 'v':
			if !sc.numbers(v[:1]) {
				return out
			}
			cy = oy + v[0]
		case 'C', 'c', 'S', 's':
			n := 6
			if cmd == 'S' || cmd == 's' {
				n = 4
			}
			if !sc.numbers(v[:n]) {
				return out
			}
			var x1, y1 float64
			if n == 4 {
				x1, y1 = cx, cy
				if prev == 'C' || prev == 'S' {
					x1, y1 = 2*cx-lastCtl[0], 2*cy-lastCtl[1]
				}
				copy(v[2:6], v[0:4])
			} else {
				x1, y1 = ox+v[0], oy+v[1]
			}
			x2, y2, x, y := ox+v[2], oy+v[3], ox+v[4], oy+v[5]
			if !add(cmdOf(shape.CmdCubic, x1, y1, x2, y2, x, y)) {
				return out
			}
			lastCtl = [2]float64{x2, y2}
			cx, cy = x, y
			prev = 'C'
			if n == 4 {
				prev = 'S'
			}
			continue
		case 'Q', 'q', 'T', 't':
			n := 4
			if cmd == 'T' || cmd == 't' {
				n = 2
			}
			if !sc.numbers(v[:n]) {
				return out
			}
			var qx, qy float64
			if n == 2 {
				qx, qy = cx, cy
				if prev == 'Q' || prev == 'T' {
					qx, qy = 2*cx-lastCtl[0], 2*cy-lastCtl[1]
				}
				copy(v[2:4], v[0:2])
			} else {
				qx, qy = ox+v[0], oy+v[1]
			}
			x, y := ox+v[2], oy+v[3]
			if !add(cmdOf(shape.CmdQuad, qx, qy, x, y)) {
				return out
			}
			lastCtl = [2]float64{qx, qy}
			cx, cy = x, y
			prev = 'Q'
			if n == 2 {
				prev = 'T'
			}
			continue
		case 'A', 'a':
			if !sc.numbers(v[:3]) {
				return out
			}
			large, ok1 := sc.flag()
			sweep, ok2 := sc.flag()
			if !ok1 || !ok2 || !sc.numbers(v[3:5]) {
				return out
			}
			x, y := ox+v[3], oy+v[4]
			for _, c := range arcCubics(cx, cy, v[0], v[1], v[2], large, sweep, x, y) {
				if !add(c) {
					return out
				}
			}
			cx, cy = x, y
			prev = 'A'
			continue
		case 'Z', 'z':
			if !add(shape.Cmd{Kind: shape.CmdClose}) {
				return out
			}
			cx, cy = sx, sy
			prev = 'Z'
			continue
		}
		if !add(cmdOf(shape.CmdLine, cx, cy)) {
			return out
		}
		prev = 'L'
	}
}

// arcCubics converts the elliptical arc from (x0, y0) to (x, y) (SVG 1.1,
// 8.3.8) into cubic Béziers of at most a quarter turn: the endpoint to
// centre parameterization of F.6.5 with the radii corrected as F.6.6
// says. A zero radius draws a line, coincident ends nothing (F.6.2).
func arcCubics(x0, y0, rx, ry, rotDeg float64, large, sweep bool, x, y float64) []shape.Cmd {
	if x0 == x && y0 == y {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []shape.Cmd{cmdOf(shape.CmdLine, x, y)}
	}
	sin, cos := math.Sincos(rotDeg * math.Pi / 180)
	dx, dy := (x0-x)/2, (y0-y)/2
	x1p, y1p := cos*dx+sin*dy, -sin*dx+cos*dy
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if den > 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1p/ry, -co*ry*x1p/rx
	ccx, ccy := cos*cxp-sin*cyp+(x0+x)/2, sin*cxp+cos*cyp+(y0+y)/2
	angle := func(ux, uy, vx, vy float64) float64 { return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy) }
	t1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dt := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	if !finite(dt) || !finite(ccx) || !finite(ccy) {
		return []shape.Cmd{cmdOf(shape.CmdLine, x, y)}
	}
	n := int(math.Ceil(math.Abs(dt)/(math.Pi/2) - 1e-9))
	n = max(n, 1)
	step := dt / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64, float64, float64) {
		st, ct := math.Sincos(t)
		px, py := rx*ct, ry*st
		tx, ty := -rx*st, ry*ct
		return cos*px - sin*py + ccx, sin*px + cos*py + ccy, cos*tx - sin*ty, sin*tx + cos*ty
	}
	out := make([]shape.Cmd, 0, n)
	t := t1
	ax, ay, adx, ady := pt(t)
	for i := range n {
		t2 := t + step
		bx, by, bdx, bdy := pt(t2)
		if i == n-1 {
			bx, by = x, y
		}
		out = append(out, cmdOf(shape.CmdCubic, ax+k*adx, ay+k*ady, bx-k*bdx, by-k*bdy, bx, by))
		ax, ay, adx, ady, t = bx, by, bdx, bdy, t2
	}
	return out
}
