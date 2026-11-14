package css

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// This file holds the math functions of CSS Values 4, 10: a calculation
// tree parsed from calc(), min(), max(), clamp(), round(), mod(), rem(),
// the trigonometric, exponential and sign functions, type-checked as
// 10.9 asks, folded to a linear combination of units where it is one, and
// evaluated otherwise once the units and the percentage basis are known.

// calcOp is the operation of a calculation tree node.
type calcOp uint8

// The calculation tree operations: leaves, the sum and product nodes of
// 10.10, and the functions of 10.2 to 10.6.
const (
	opNum calcOp = iota
	opLen
	opPct
	opVar
	opSum
	opNeg
	opProduct
	opInvert
	opMin
	opMax
	opClamp
	opRound
	opMod
	opRem
	opSin
	opCos
	opTan
	opAsin
	opAcos
	opAtan
	opAtan2
	opPow
	opSqrt
	opHypot
	opLog
	opExp
	opAbs
	opSign
	// opSize is the size keyword of calc-size(), the used size of its
	// basis (CSS Sizing 4, 7.1).
	opSize
)

// roundStrategy is a <rounding-strategy> of round() (CSS Values 4, 10.3).
type roundStrategy uint8

// The rounding strategies litho evaluates.
const (
	roundNearest roundStrategy = iota
	roundUp
	roundDown
	roundToZero
)

// calcNode is a node of a calculation tree. A leaf holds a number, a
// length in a unit (an absolute length already in points, an angle in
// degrees, a time in seconds, a frequency in hertz and a resolution in
// dppx, each a number of its canonical unit), a percentage or a channel
// keyword of a relative colour. A node is never mutated once parsed, so
// a tree is shared freely.
type calcNode struct {
	op       calcOp
	unit     lenUnit
	strategy roundStrategy
	v        float64
	kids     []*calcNode
}

// calcType is the type of a calculation (CSS Values 4, 10.9): the power
// of each base type, all zero for a <number>.
type calcType struct {
	length, angle, time, freq, res, pct int8
}

// The base types of the leaves.
var (
	typeNumber = calcType{}
	typeLength = calcType{length: 1}
	typeAngle  = calcType{angle: 1}
	typeTime   = calcType{time: 1}
	typeFreq   = calcType{freq: 1}
	typeRes    = calcType{res: 1}
	typePct    = calcType{pct: 1}
)

func (t calcType) add(o calcType, k int8) calcType {
	return calcType{
		length: t.length + k*o.length, angle: t.angle + k*o.angle, time: t.time + k*o.time,
		freq: t.freq + k*o.freq, res: t.res + k*o.res, pct: t.pct + k*o.pct,
	}
}

// lenUnit is a length unit whose size depends on the element, the root or
// the page, or uPt for every absolute unit, already converted to points.
type lenUnit uint8

// The length units (CSS Values 4, 6).
const (
	uPt lenUnit = iota
	uEm
	uEx
	uCh
	uRem
	uVw
	uVh
	uVmin
	uVmax
	uLh
	uRlh
	uCap
	uRcap
	uRex
	uRch
	uIc
	uRic
	uVi
	uVb
	numLenUnits
)

// lengthUnits maps the relative length units to their [lenUnit]. The
// small, large and dynamic viewport units are the viewport units: on
// paper the viewport is the page area and never changes size, so the
// three viewport sizes of CSS Values 4, 6.1.2.1 are the one page area.
var lengthUnits = map[string]lenUnit{
	"em": uEm, "ex": uEx, "ch": uCh, "rem": uRem, "lh": uLh, "rlh": uRlh,
	"cap": uCap, "rcap": uRcap, "rex": uRex, "rch": uRch, "ic": uIc, "ric": uRic,
	"vw": uVw, "svw": uVw, "lvw": uVw, "dvw": uVw,
	"vh": uVh, "svh": uVh, "lvh": uVh, "dvh": uVh,
	"vi": uVi, "svi": uVi, "lvi": uVi, "dvi": uVi,
	"vb": uVb, "svb": uVb, "lvb": uVb, "dvb": uVb,
	"vmin": uVmin, "svmin": uVmin, "lvmin": uVmin, "dvmin": uVmin,
	"vmax": uVmax, "svmax": uVmax, "lvmax": uVmax, "dvmax": uVmax,
}

// otherUnits maps the angle, time, frequency and resolution units to their
// type and their size in its canonical unit (CSS Values 4, 7).
var otherUnits = map[string]struct {
	t calcType
	k float64
}{
	"deg": {typeAngle, 1}, "grad": {typeAngle, 0.9}, "rad": {typeAngle, 180 / math.Pi}, "turn": {typeAngle, 360},
	"s": {typeTime, 1}, "ms": {typeTime, 0.001},
	"hz": {typeFreq, 1}, "khz": {typeFreq, 1000},
	"dppx": {typeRes, 1}, "x": {typeRes, 1}, "dpi": {typeRes, 1.0 / 96}, "dpcm": {typeRes, 2.54 / 96},
}

// channelVars are the channel keywords of a relative colour (CSS Color 5,
// 4.1), each a number in a calculation.
type channelVars struct {
	names [4]string
	vals  [4]float64
}

// maxCalcNodes caps the nodes of one calculation tree: 1024. Real values
// hold a dozen. Anti-DoS guard, not spec-mandated: it bounds the work of
// evaluating a tree once per element.
const maxCalcNodes = 1024

// calcParser parses one math function.
type calcParser struct {
	// vars are the channel keywords in scope, nil outside a relative
	// colour.
	vars *channelVars
	// pctLength types a percentage as a length, in a context whose
	// percentages resolve against a length (CSS Values 4, 10.9, the
	// percent hint).
	pctLength bool
	// pctAngle types a percentage as an angle, in a context whose
	// percentages resolve against a full turn: a conic gradient's stops
	// (CSS Images 4, 3.3.2).
	pctAngle bool
	// size accepts the size keyword, in the calculation of a
	// calc-size() whose basis is not any (CSS Sizing 4, 7).
	size  bool
	nodes int
}

// isCalc reports whether t is a math function (CSS Values 4, 10): calc(),
// prefixed or not, a comparison, stepped-value, trigonometric,
// exponential or sign-related function.
func isCalc(t *token) bool {
	if t.typ != tFunction {
		return false
	}
	_, ok := calcFunctions[lower(t.value)]
	return ok
}

// calcFunctions maps the name of each math function to its operation and
// its argument count, at least and at most; calc() is a sum.
var calcFunctions = map[string]struct {
	op       calcOp
	min, max int
}{
	"calc": {opSum, 1, 1}, "-webkit-calc": {opSum, 1, 1}, "-moz-calc": {opSum, 1, 1},
	"min": {opMin, 1, maxCmpArgs}, "max": {opMax, 1, maxCmpArgs}, "clamp": {opClamp, 3, 3},
	"round": {opRound, 1, 3}, "mod": {opMod, 2, 2}, "rem": {opRem, 2, 2},
	"sin": {opSin, 1, 1}, "cos": {opCos, 1, 1}, "tan": {opTan, 1, 1},
	"asin": {opAsin, 1, 1}, "acos": {opAcos, 1, 1}, "atan": {opAtan, 1, 1}, "atan2": {opAtan2, 2, 2},
	"pow": {opPow, 2, 2}, "sqrt": {opSqrt, 1, 1}, "hypot": {opHypot, 1, maxCmpArgs},
	"log": {opLog, 1, 2}, "exp": {opExp, 1, 1},
	"abs": {opAbs, 1, 1}, "sign": {opSign, 1, 1},
}

// maxCmpArgs caps the arguments of one min(), max() or hypot(): 32. Real
// styles compare two or three values. Anti-DoS guard, not spec-mandated.
const maxCmpArgs = 32

// fn parses the math function t, which [isCalc] accepts.
func (p *calcParser) fn(t *token, depth int) (*calcNode, calcType, bool) {
	if depth > maxCalcDepth {
		return nil, calcType{}, false
	}
	def := calcFunctions[lower(t.value)]
	if def.op == opSum {
		return p.sum(t.children, depth+1)
	}
	parts := splitCommas(t.children)
	var strategy roundStrategy
	if def.op == opRound && len(parts) > 0 {
		if s, ok := roundStrategyOf(parts[0]); ok {
			strategy = s
			parts = parts[1:]
			if len(parts) == 0 {
				return nil, calcType{}, false
			}
		}
	}
	if len(parts) < def.min || len(parts) > def.max {
		return nil, calcType{}, false
	}
	kids := make([]*calcNode, len(parts))
	types := make([]calcType, len(parts))
	for i, a := range parts {
		n, ty, ok := p.sum(a, depth+1)
		if !ok {
			return nil, calcType{}, false
		}
		kids[i], types[i] = n, ty
	}
	same := func() bool {
		for _, ty := range types[1:] {
			if ty != types[0] {
				return false
			}
		}
		return true
	}
	res := types[0]
	switch def.op { //nolint:exhaustive // the leaves and arithmetic nodes are not functions
	case opMin, opMax, opClamp, opHypot, opMod, opRem:
		// CSS Values 4, 10.2, 10.3 and 10.5: arguments of one type.
		if !same() {
			return nil, calcType{}, false
		}
	case opRound:
		// CSS Values 4, 10.3: B defaults to 1 for a number and is
		// required otherwise.
		if !same() || len(kids) == 1 && res != typeNumber {
			return nil, calcType{}, false
		}
		if len(kids) == 1 {
			kids = append(kids, &calcNode{op: opNum, v: 1})
		}
	case opSin, opCos, opTan:
		// CSS Values 4, 10.4: a number of radians or an angle.
		if res != typeNumber && res != typeAngle {
			return nil, calcType{}, false
		}
		if res == typeNumber {
			kids[0] = &calcNode{op: opProduct, kids: []*calcNode{kids[0], {op: opNum, v: 180 / math.Pi}}}
		}
		res = typeNumber
	case opAsin, opAcos, opAtan:
		if res != typeNumber {
			return nil, calcType{}, false
		}
		res = typeAngle
	case opAtan2:
		if !same() {
			return nil, calcType{}, false
		}
		res = typeAngle
	case opPow, opSqrt, opLog, opExp:
		// CSS Values 4, 10.5: numbers only.
		for _, ty := range types {
			if ty != typeNumber {
				return nil, calcType{}, false
			}
		}
	case opSign:
		res = typeNumber
	}
	if !p.count() {
		return nil, calcType{}, false
	}
	return &calcNode{op: def.op, strategy: strategy, kids: kids}, res, true
}

// count charges one node to the tree, false past maxCalcNodes.
func (p *calcParser) count() bool {
	p.nodes++
	return p.nodes <= maxCalcNodes
}

// roundStrategyOf reads a <rounding-strategy> argument of round().
func roundStrategyOf(l []token) (roundStrategy, bool) {
	l = significant(l)
	if len(l) != 1 || l[0].typ != tIdent {
		return 0, false
	}
	switch lower(l[0].value) {
	case "nearest":
		return roundNearest, true
	case "up":
		return roundUp, true
	case "down":
		return roundDown, true
	case "to-zero":
		return roundToZero, true
	}
	return 0, false
}

// sum parses a <calc-sum> (CSS Values 4, 10.8): products joined by + and
// - with white space around them, of one type.
func (p *calcParser) sum(list []token, depth int) (*calcNode, calcType, bool) {
	if depth > maxCalcDepth {
		return nil, calcType{}, false
	}
	c := &calcCursor{l: list}
	c.ws()
	first, ty, ok := p.product(c, depth)
	if !ok {
		return nil, calcType{}, false
	}
	terms := []*calcNode{first}
	for {
		hadWS := c.ws()
		t := c.peek()
		if t == nil {
			break
		}
		if !hadWS || !t.isDelim('+') && !t.isDelim('-') {
			return nil, calcType{}, false
		}
		neg := t.isDelim('-')
		c.i++
		if !c.ws() {
			return nil, calcType{}, false
		}
		rhs, rty, ok := p.product(c, depth)
		if !ok || rty != ty {
			return nil, calcType{}, false
		}
		if neg {
			rhs = &calcNode{op: opNeg, kids: []*calcNode{rhs}}
			if !p.count() {
				return nil, calcType{}, false
			}
		}
		terms = append(terms, rhs)
	}
	if len(terms) == 1 {
		return first, ty, true
	}
	if !p.count() {
		return nil, calcType{}, false
	}
	return &calcNode{op: opSum, kids: terms}, ty, true
}

// product parses a <calc-product>: values joined by * and /, whose types
// multiply (CSS Values 4, 10.9).
func (p *calcParser) product(c *calcCursor, depth int) (*calcNode, calcType, bool) {
	first, ty, ok := p.value(c, depth)
	if !ok {
		return nil, calcType{}, false
	}
	factors := []*calcNode{first}
	for {
		save := c.i
		c.ws()
		t := c.peek()
		if t == nil || !t.isDelim('*') && !t.isDelim('/') {
			c.i = save
			break
		}
		div := t.isDelim('/')
		c.i++
		c.ws()
		rhs, rty, ok := p.value(c, depth)
		if !ok {
			return nil, calcType{}, false
		}
		if div {
			rhs = &calcNode{op: opInvert, kids: []*calcNode{rhs}}
			if !p.count() {
				return nil, calcType{}, false
			}
			ty = ty.add(rty, -1)
		} else {
			ty = ty.add(rty, 1)
		}
		factors = append(factors, rhs)
	}
	if len(factors) == 1 {
		return first, ty, true
	}
	if !p.count() {
		return nil, calcType{}, false
	}
	return &calcNode{op: opProduct, kids: factors}, ty, true
}

// value parses a <calc-value>: a number, a dimension, a percentage, a
// constant, a channel keyword, a nested math function or a parenthesised
// sum.
func (p *calcParser) value(c *calcCursor, depth int) (*calcNode, calcType, bool) {
	t := c.peek()
	if t == nil || !p.count() {
		return nil, calcType{}, false
	}
	c.i++
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return &calcNode{op: opNum, v: t.num}, typeNumber, numeric.Finite(t.num)
	case tDimension:
		if !numeric.Finite(t.num) {
			return nil, calcType{}, false
		}
		unit := lower(t.value)
		if k, ok := absUnits[unit]; ok {
			return &calcNode{op: opLen, unit: uPt, v: t.num * k}, typeLength, true
		}
		if u, ok := lengthUnits[unit]; ok {
			return &calcNode{op: opLen, unit: u, v: t.num}, typeLength, true
		}
		if o, ok := otherUnits[unit]; ok {
			return &calcNode{op: opNum, v: t.num * o.k}, o.t, true
		}
	case tPercentage:
		if !numeric.Finite(t.num) {
			return nil, calcType{}, false
		}
		if p.pctLength {
			return &calcNode{op: opPct, v: t.num}, typeLength, true
		}
		if p.pctAngle {
			return &calcNode{op: opPct, v: t.num}, typeAngle, true
		}
		return &calcNode{op: opNum, v: t.num}, typePct, true
	case tLParen:
		return p.sum(t.children, depth+1)
	case tFunction:
		if isCalc(t) {
			return p.fn(t, depth+1)
		}
	case tIdent:
		// CSS Values 4, 10.7: the numeric constants, then the channel
		// keywords of a relative colour (CSS Color 5, 4.1).
		switch lower(t.value) {
		case "e":
			return &calcNode{op: opNum, v: math.E}, typeNumber, true
		case "pi":
			return &calcNode{op: opNum, v: math.Pi}, typeNumber, true
		case "infinity":
			return &calcNode{op: opNum, v: math.Inf(1)}, typeNumber, true
		case "-infinity":
			return &calcNode{op: opNum, v: math.Inf(-1)}, typeNumber, true
		case "nan":
			return &calcNode{op: opNum, v: math.NaN()}, typeNumber, true
		}
		if p.size && eqFold(t.value, "size") {
			return &calcNode{op: opSize}, typeLength, true
		}
		if p.vars != nil {
			for i, n := range p.vars.names {
				if n != "" && eqFold(t.value, n) {
					return &calcNode{op: opVar, unit: lenUnit(i)}, typeNumber, true
				}
			}
		}
	}
	return nil, calcType{}, false
}

// calcCursor walks the component values inside a math function.
type calcCursor struct {
	l []token
	i int
}

func (c *calcCursor) ws() bool {
	seen := false
	for c.i < len(c.l) && c.l[c.i].typ == tWhitespace {
		c.i++
		seen = true
	}
	return seen
}

func (c *calcCursor) peek() *token {
	if c.i < len(c.l) {
		return &c.l[c.i]
	}
	return nil
}

// calcScope is what a calculation tree evaluates against: the sizes of
// the units, the percentage basis and the channel keywords. A nil unit
// table leaves the relative units unresolved.
type calcScope struct {
	units *[numLenUnits]float64
	basis float64
	vars  *channelVars
	// size is the value of the size keyword of a calc-size().
	size float64
}

// eval evaluates the tree in IEEE-754 arithmetic, as CSS Values 4, 10.9.2
// asks inside a calculation: infinities and NaN are censored by the
// caller, at the top level.
func (n *calcNode) eval(s *calcScope) float64 {
	switch n.op { //nolint:exhaustive // the functions are applied below, by applyCalc
	case opNum:
		return n.v
	case opLen:
		if n.unit == uPt || s.units == nil {
			return n.v
		}
		return n.v * s.units[n.unit]
	case opPct:
		return n.v / 100 * s.basis
	case opSize:
		return s.size
	case opVar:
		if s.vars == nil {
			return 0
		}
		return s.vars.vals[n.unit]
	case opSum:
		v := 0.0
		for _, k := range n.kids {
			v += k.eval(s)
		}
		return v
	case opNeg:
		return -n.kids[0].eval(s)
	case opProduct:
		v := 1.0
		for _, k := range n.kids {
			v *= k.eval(s)
		}
		return v
	case opInvert:
		return 1 / n.kids[0].eval(s)
	}
	args := make([]float64, len(n.kids))
	for i, k := range n.kids {
		args[i] = k.eval(s)
	}
	return applyCalc(n.op, n.strategy, args)
}

// applyCalc applies the function op to its evaluated arguments (CSS Values
// 4, 10.2 to 10.6). Angles are in degrees.
func applyCalc(op calcOp, strategy roundStrategy, a []float64) float64 {
	const deg = math.Pi / 180
	switch op { //nolint:exhaustive // the leaves and arithmetic nodes are evaluated by eval
	case opMin, opMax:
		v := a[0]
		for _, x := range a[1:] {
			if math.IsNaN(x) {
				return x
			}
			if op == opMin && x < v || op == opMax && x > v {
				v = x
			}
		}
		return v
	case opClamp:
		// clamp(MIN, VAL, MAX) is max(MIN, min(VAL, MAX)) (CSS Values 4,
		// 10.2).
		return math.Max(a[0], math.Min(a[1], a[2]))
	case opRound:
		return roundStep(strategy, a[0], a[1])
	case opMod, opRem:
		return modStep(op == opMod, a[0], a[1])
	case opSin:
		return detmath.Sin(a[0] * deg)
	case opCos:
		return detmath.Cos(a[0] * deg)
	case opTan:
		return detmath.Tan(a[0] * deg)
	case opAsin:
		return detmath.Asin(a[0]) / deg
	case opAcos:
		return detmath.Acos(a[0]) / deg
	case opAtan:
		return detmath.Atan(a[0]) / deg
	case opAtan2:
		return detmath.Atan2(a[0], a[1]) / deg
	case opPow:
		return detmath.Pow(a[0], a[1])
	case opSqrt:
		return math.Sqrt(a[0])
	case opHypot:
		v := 0.0
		for _, x := range a {
			v = detmath.Hypot(v, x)
		}
		return v
	case opLog:
		if len(a) == 2 {
			return detmath.Log(a[0]) / detmath.Log(a[1])
		}
		return detmath.Log(a[0])
	case opExp:
		return detmath.Exp(a[0])
	case opAbs:
		return math.Abs(a[0])
	case opSign:
		switch {
		case a[0] > 0:
			return 1
		case a[0] < 0:
			return -1
		}
		return a[0]
	}
	return math.NaN()
}

// roundStep rounds a to a multiple of b by strategy (CSS Values 4, 10.3
// and 10.3.1).
func roundStep(strategy roundStrategy, a, b float64) float64 {
	switch {
	case math.IsNaN(a) || math.IsNaN(b) || b == 0:
		return math.NaN()
	case math.IsInf(a, 0):
		if math.IsInf(b, 0) {
			return math.NaN()
		}
		return a
	case math.IsInf(b, 0):
		switch strategy {
		case roundUp:
			if a > 0 {
				return math.Inf(1)
			}
		case roundDown:
			if a < 0 {
				return math.Inf(-1)
			}
		case roundNearest, roundToZero:
		}
		return math.Copysign(0, a)
	}
	b = math.Abs(b)
	q := a / b
	lo, hi := float64(math.Floor(q)*b), float64(math.Ceil(q)*b)
	if lo == hi {
		return a
	}
	switch strategy {
	case roundUp:
		return hi
	case roundDown:
		return lo
	case roundToZero:
		if math.Abs(lo) < math.Abs(hi) {
			return lo
		}
		return hi
	case roundNearest:
	}
	if a-lo < hi-a {
		return lo
	}
	return hi
}

// modStep is mod(a, b), the result taking b's sign, or rem(a, b), the
// result taking a's (CSS Values 4, 10.3 and 10.3.1).
func modStep(isMod bool, a, b float64) float64 {
	switch {
	case math.IsNaN(a) || math.IsNaN(b) || b == 0 || math.IsInf(a, 0):
		return math.NaN()
	case math.IsInf(b, 0):
		if isMod && a != 0 && math.Signbit(a) != math.Signbit(b) {
			return math.NaN()
		}
		return a
	}
	r := math.Mod(a, b)
	if isMod && r != 0 && math.Signbit(r) != math.Signbit(b) {
		r += b
	}
	return r
}

// censor turns the result of a top-level calculation into a value a
// property can hold (CSS Values 4, 10.9.2): NaN becomes zero, and the
// caller clamps an infinity to its range.
func censor(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

// relative reports whether the tree holds a unit whose size depends on
// the element, the root or the page, a percentage or a channel keyword:
// a value only known at computed or used-value time.
func (n *calcNode) relative() bool {
	switch n.op { //nolint:exhaustive // the other operations hold children
	case opNum:
		return false
	case opLen:
		return n.unit != uPt
	case opPct, opVar, opSize:
		return true
	}
	for _, k := range n.kids {
		if k.relative() {
			return true
		}
	}
	return false
}

// hasPct reports whether the tree holds a percentage of a length basis.
func (n *calcNode) hasPct() bool {
	if n.op == opPct {
		return true
	}
	for _, k := range n.kids {
		if k.hasPct() {
			return true
		}
	}
	return false
}

// pctFunction reports a math function other than a sum, a product or a
// negation that holds a percentage: its value depends on the sign of the
// percentage basis, not on the percentages alone.
func (n *calcNode) pctFunction() bool {
	switch n.op { //nolint:exhaustive // the other operators are functions
	case opNum, opVar, opLen, opPct, opSize:
		return false
	case opSum, opNeg, opProduct, opInvert:
		for _, k := range n.kids {
			if k.pctFunction() {
				return true
			}
		}
		return false
	}
	return n.hasPct()
}

// linear folds the tree into a linear combination of the length units
// and of the percentage, a [length], when it is one: sums and products
// by a constant of such combinations, and functions of constants. ok is
// false for a tree that is not linear, such as min() of two units.
func (n *calcNode) linear(vars *channelVars) (l length, ok bool) {
	switch n.op { //nolint:exhaustive // the functions are folded below
	case opNum:
		return length{num: n.v}, true
	case opVar:
		if vars == nil {
			return length{}, false
		}
		return length{num: vars.vals[n.unit]}, true
	case opLen:
		l.u[n.unit] = n.v
		l.hasLen = true
		return l, true
	case opPct:
		return length{pct: n.v, hasPct: true}, true
	case opSize:
		return length{}, false
	case opSum:
		for _, k := range n.kids {
			x, ok := k.linear(vars)
			if !ok {
				return length{}, false
			}
			l = l.add(x)
		}
		return l, true
	case opNeg:
		x, ok := n.kids[0].linear(vars)
		return x.scale(-1), ok
	case opProduct:
		l = length{num: 1}
		for _, k := range n.kids {
			x, ok := k.linear(vars)
			switch {
			case !ok:
				return length{}, false
			case x.constant():
				l = l.scale(x.num)
			case l.constant():
				x = x.scale(l.num)
				x.num = 0
				l = x
			default:
				return length{}, false
			}
		}
		return l, true
	case opInvert:
		x, ok := n.kids[0].linear(vars)
		if !ok || !x.constant() {
			return length{}, false
		}
		return length{num: 1 / x.num}, true
	}
	// A function of constants is a constant. A function of percentages
	// alone is a percentage: its type is that of a length, so it scales
	// with the basis, and its value is that of its percentages (sign()
	// and atan2() of them a number). That holds for a basis that is never
	// negative; a property whose basis can be negative keeps the tree
	// (signedBasis).
	args := make([]float64, len(n.kids))
	consts, pcts := true, true
	for i, k := range n.kids {
		x, ok := k.linear(vars)
		if !ok {
			return length{}, false
		}
		switch {
		case x.constant():
			args[i], pcts = x.num, false
		case x.hasPct && !x.hasLen && x.num == 0:
			args[i], consts = x.pct, false
		default:
			return length{}, false
		}
	}
	v := applyCalc(n.op, n.strategy, args)
	if consts || n.op == opSign || n.op == opAtan2 {
		return length{num: v}, consts || pcts
	}
	if !pcts {
		return length{}, false
	}
	return length{pct: v, hasPct: true}, true
}

// resolveUnits returns a copy of the tree whose relative lengths are
// points, by the unit sizes of s: the tree a [Dimension] keeps for its
// percentage basis, known only at used-value time. A tree without a
// relative length is returned as it is.
func (n *calcNode) resolveUnits(units *[numLenUnits]float64) *calcNode {
	if !n.relativeLength() {
		return n
	}
	if n.op == opLen {
		return &calcNode{op: opLen, unit: uPt, v: n.v * units[n.unit]}
	}
	c := *n
	c.kids = make([]*calcNode, len(n.kids))
	for i, k := range n.kids {
		c.kids[i] = k.resolveUnits(units)
	}
	return &c
}

// relativeLength reports whether the tree holds a relative length unit.
func (n *calcNode) relativeLength() bool {
	if n.op == opLen {
		return n.unit != uPt
	}
	for _, k := range n.kids {
		if k.relativeLength() {
			return true
		}
	}
	return false
}

// calcNumber parses a math function of type <number>, with the channel
// keywords vars in scope, nil for none.
func calcNumber(t *token, vars *channelVars) (float64, bool) {
	v, ty, ok := calcConst(t, vars)
	return v, ok && ty == typeNumber
}

// calcConst parses a math function whose value is known once vars are:
// one without a relative length or a percentage of a length basis. It
// returns the value in the canonical unit of its type, a percentage as
// its number of percent.
func calcConst(t *token, vars *channelVars) (float64, calcType, bool) {
	v, ty, ok := calcInfinite(t, vars)
	if math.IsInf(v, 0) {
		v = math.Copysign(maxLength, v)
	}
	return v, ty, ok
}

// calcInfinite is [calcConst] with an infinite value kept, for a caller
// that clamps to a range of its own: CSS Values 4, 10.9 clamps infinity
// to the largest value the type supports, which for an <integer> is not
// the bound of a length. NaN censors to 0.
func calcInfinite(t *token, vars *channelVars) (float64, calcType, bool) {
	p := &calcParser{vars: vars}
	n, ty, ok := p.fn(t, 0)
	if !ok {
		return 0, calcType{}, false
	}
	l, ok := n.linear(vars)
	if !ok || !l.constant() {
		return 0, calcType{}, false
	}
	return censor(l.num), ty, true
}

// calcAnglePercent parses a math function of type <angle-percentage>, a
// percentage being of a full turn, into degrees.
func calcAnglePercent(t *token) (float64, bool) {
	p := &calcParser{pctAngle: true}
	n, ty, ok := p.fn(t, 0)
	if !ok || ty != typeAngle {
		return 0, false
	}
	v := censor(n.eval(&calcScope{basis: 360}))
	if math.IsInf(v, 0) {
		v = math.Copysign(maxLength, v)
	}
	return v, true
}
