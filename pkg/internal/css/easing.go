package css

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// EasingKind says what an [Easing] is (CSS Easing 2, 2).
type EasingKind uint8

// The kinds of easing function.
const (
	// EasingCubic is a cubic Bézier easing function: ease, ease-in,
	// ease-out, ease-in-out and cubic-bezier() (CSS Easing 2, 2.2).
	EasingCubic EasingKind = iota
	// EasingLinear is linear or linear() (CSS Easing 2, 2.1).
	EasingLinear
	// EasingSteps is step-start, step-end or steps() (CSS Easing 2, 2.3).
	EasingSteps
)

// StepPosition is the step position of steps() (CSS Easing 2, 2.3):
// start and end compute to jump-start and jump-end.
type StepPosition uint8

// The step positions.
const (
	JumpEnd StepPosition = iota
	JumpStart
	JumpNone
	JumpBoth
)

// LinearStop is one control point of linear(): an output progress at an
// input progress, both canonicalized (CSS Easing 2, 2.1).
type LinearStop struct {
	Output, Input float64
}

// Easing is a computed easing function (CSS Easing 2, 2). The zero value
// is cubic-bezier(0, 0, 0, 0), which outputs its input: linear.
type Easing struct {
	Kind EasingKind
	// X1, Y1, X2 and Y2 are the control points of a cubic Bézier.
	X1, Y1, X2, Y2 float64
	// Steps and Position are the intervals and step position of steps().
	Steps    int
	Position StepPosition
	// Stops are the control points of linear(), nil for linear.
	Stops []LinearStop
}

// The keyword easing functions (CSS Easing 2, 2.2 and 2.3).
var (
	easeEasing   = Easing{Kind: EasingCubic, X1: 0.25, Y1: 0.1, X2: 0.25, Y2: 1}
	linearEasing = Easing{Kind: EasingLinear}
)

// maxLinearStops caps the control points of one linear(): 1000. Anti-DoS
// guard, not spec-mandated: a stop costs a comparison each time a value
// eases, and hand-written or generated curves use a few dozen.
const maxLinearStops = 1000

// parseEasing parses an <easing-function> (CSS Easing 2, 2): linear,
// linear(), the cubic Bézier keywords, cubic-bezier(), step-start,
// step-end and steps().
func parseEasing(t token) (Easing, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		switch lower(t.value) {
		case "linear":
			return linearEasing, true
		case "ease":
			return easeEasing, true
		case "ease-in":
			return Easing{Kind: EasingCubic, X1: 0.42, X2: 1, Y2: 1}, true
		case "ease-out":
			return Easing{Kind: EasingCubic, X2: 0.58, Y2: 1}, true
		case "ease-in-out":
			return Easing{Kind: EasingCubic, X1: 0.42, X2: 0.58, Y2: 1}, true
		case "step-start":
			return Easing{Kind: EasingSteps, Steps: 1, Position: JumpStart}, true
		case "step-end":
			return Easing{Kind: EasingSteps, Steps: 1, Position: JumpEnd}, true
		}
	case tFunction:
		args := splitCommas(significant(t.children))
		switch lower(t.value) {
		case "cubic-bezier":
			return parseCubicBezier(args)
		case "steps":
			return parseSteps(args)
		case "linear":
			return parseLinear(args)
		}
	}
	return Easing{}, false
}

// parseCubicBezier parses the arguments of cubic-bezier(): four numbers,
// both x values in [0, 1] (CSS Easing 2, 2.2).
func parseCubicBezier(args [][]token) (Easing, bool) {
	if len(args) != 4 {
		return Easing{}, false
	}
	var v [4]float64
	for i, a := range args {
		t, ok := single(a)
		if !ok {
			return Easing{}, false
		}
		if v[i], ok = parseNumber(t); !ok {
			return Easing{}, false
		}
	}
	if v[0] < 0 || v[0] > 1 || v[2] < 0 || v[2] > 1 {
		return Easing{}, false
	}
	return Easing{Kind: EasingCubic, X1: v[0], Y1: v[1], X2: v[2], Y2: v[3]}, true
}

// maxSteps caps the intervals of steps(): 2^30, the integer range litho
// parses. Not spec-mandated: CSS Easing 2, 2.3 sets no upper bound.
const maxSteps = maxInteger

// parseSteps parses the arguments of steps(): an integer and an optional
// step position; jump-none needs two steps or more, the others one (CSS
// Easing 2, 2.3).
func parseSteps(args [][]token) (Easing, bool) {
	if len(args) < 1 || len(args) > 2 {
		return Easing{}, false
	}
	t, ok := single(args[0])
	if !ok {
		return Easing{}, false
	}
	n, ok := parseInteger(t)
	if !ok || n > maxSteps {
		return Easing{}, false
	}
	e := Easing{Kind: EasingSteps, Steps: n, Position: JumpEnd}
	if len(args) == 2 {
		k, ok := keyword(args[1])
		if !ok {
			return Easing{}, false
		}
		switch k {
		case "jump-start", "start":
			e.Position = JumpStart
		case "jump-end", "end":
			e.Position = JumpEnd
		case "jump-none":
			e.Position = JumpNone
		case "jump-both":
			e.Position = JumpBoth
		default:
			return Easing{}, false
		}
	}
	if n < 1 || e.Position == JumpNone && n < 2 {
		return Easing{}, false
	}
	return e, true
}

// parseLinear parses the arguments of linear() (CSS Easing 2, 2.1): each a
// number and up to two percentages, in any order, and canonicalizes the
// control points.
func parseLinear(args [][]token) (Easing, bool) {
	if len(args) == 0 || len(args) > maxLinearStops {
		return Easing{}, false
	}
	type point struct {
		out, in float64
		set     bool
	}
	var pts []point
	for _, a := range args {
		var out float64
		var ins []float64
		hasOut := false
		for _, t := range a {
			if t.typ == tPercentage {
				if !numeric.Finite(t.num) || len(ins) == 2 {
					return Easing{}, false
				}
				ins = append(ins, t.num/100)
				continue
			}
			if hasOut {
				return Easing{}, false
			}
			v, ok := parseNumber(t)
			if !ok {
				return Easing{}, false
			}
			out, hasOut = v, true
		}
		if !hasOut {
			return Easing{}, false
		}
		if len(ins) == 0 {
			pts = append(pts, point{out: out})
		}
		for _, in := range ins {
			pts = append(pts, point{out: out, in: in, set: true})
		}
	}
	if len(pts) > maxLinearStops {
		return Easing{}, false
	}
	if !pts[0].set {
		pts[0].in, pts[0].set = 0, true
	}
	if last := &pts[len(pts)-1]; !last.set {
		last.in, last.set = 1, true
	}
	largest := math.Inf(-1)
	for i := range pts {
		if pts[i].set {
			largest = max(largest, pts[i].in)
			pts[i].in = largest
		}
	}
	for i := 0; i < len(pts); {
		if pts[i].set {
			i++
			continue
		}
		j := i
		for !pts[j].set {
			j++
		}
		from, to := pts[i-1].in, pts[j].in
		for k := i; k < j; k++ {
			pts[k].in = from + (to-from)*float64(k-i+1)/float64(j-i+1)
			pts[k].set = true
		}
		i = j
	}
	e := Easing{Kind: EasingLinear, Stops: make([]LinearStop, len(pts))}
	for i, p := range pts {
		e.Stops[i] = LinearStop{Output: p.out, Input: p.in}
	}
	return e, true
}

// at returns the output progress of e for the input progress x, with the
// before flag of Web Animations 1, 4.7.7 (CSS Easing 2, 2.1.2, 2.2.2 and
// 2.3.2).
func (e *Easing) at(x float64, before bool) float64 {
	switch e.Kind {
	case EasingLinear:
		return e.linearAt(x, before)
	case EasingSteps:
		return e.stepsAt(x, before)
	case EasingCubic:
	}
	return e.cubicAt(x)
}

// linearAt evaluates linear() (CSS Easing 2, 2.1.2).
func (e *Easing) linearAt(x float64, before bool) float64 {
	pts := e.Stops
	switch {
	case len(pts) == 0:
		return x
	case len(pts) == 1:
		return pts[0].Output
	case before && x == pts[0].Input:
		return pts[0].Output
	}
	for i := len(pts) - 1; i >= 0; i-- {
		if pts[i].Input == x {
			return pts[i].Output
		}
	}
	var a, b LinearStop
	switch {
	case x < pts[0].Input:
		a, b = pts[0], pts[1]
		if a.Input == b.Input {
			return a.Output
		}
	case x > pts[len(pts)-1].Input:
		a, b = pts[len(pts)-2], pts[len(pts)-1]
		if a.Input == b.Input {
			return b.Output
		}
	default:
		j := slices.IndexFunc(pts, func(p LinearStop) bool { return p.Input > x })
		a, b = pts[j-1], pts[j]
	}
	return a.Output + (b.Output-a.Output)*(x-a.Input)/(b.Input-a.Input)
}

// stepsAt evaluates steps() (CSS Easing 2, 2.3.2): the value of the
// interval the input falls in, the higher value at a boundary, 0 with the
// before flag set.
func (e *Easing) stepsAt(x float64, before bool) float64 {
	if before || x < 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	n := float64(e.Steps)
	k := math.Floor(x * n)
	switch e.Position {
	case JumpEnd:
	case JumpStart:
		return (k + 1) / n
	case JumpNone:
		return k / (n - 1)
	case JumpBoth:
		return (k + 1) / (n + 1)
	}
	return k / n
}

// cubicTolerance is how close the solved x of a cubic Bézier comes to the
// input progress: a thousandth of a millionth of the iteration, far below
// what a page draws.
const cubicTolerance = 1e-9

// cubicAt evaluates a cubic Bézier easing function (CSS Easing 2, 2.2.2):
// the y of the curve where its x is the input, the tangent at an end
// extended past [0, 1].
func (e *Easing) cubicAt(x float64) float64 {
	switch {
	case x < 0:
		switch {
		case e.X1 > 0:
			return e.Y1 / e.X1 * x
		case e.X2 > 0:
			return e.Y2 / e.X2 * x
		}
		return 0
	case x > 1:
		switch {
		case e.X2 < 1:
			return 1 + float64((1-e.Y2)/(1-e.X2)*(x-1))
		case e.X1 < 1:
			return 1 + float64((1-e.Y1)/(1-e.X1)*(x-1))
		}
		return 1
	}
	bez := func(t, p1, p2 float64) float64 {
		u := 1 - t
		return float64(3*u*u*t*p1) + float64(3*u*t*t*p2) + float64(t*t*t)
	}
	// The x of the curve rises with t, the x values of the control points
	// lying in [0, 1]: bisection finds the t of x.
	lo, hi := 0.0, 1.0
	t := x
	for range 64 {
		v := bez(t, e.X1, e.X2)
		if math.Abs(v-x) < cubicTolerance {
			break
		}
		if v < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(t, e.Y1, e.Y2)
}
