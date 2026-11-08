package css

import (
	"math"
	"net/url"
	"strings"
)

// length is a specified <length-percentage>: a linear combination of units
// that calc() sums (CSS Values 4, 10.7). A plain "12px" sets pt alone. ex
// and ch resolve against the font's x-height and "0" advance when
// [ComputeOptions.FontUnits] gives them, else at half an em, the fallback
// CSS Values 4, 6.1.1 allows when no font metrics are at hand.
type length struct {
	pt, em, ex, ch, rem, vw, vh, vmin, vmax, pct float64
	// lh and rlh are line heights of the element and of the root (CSS
	// Values 4, 6.1.1).
	lh, rlh float64
	// hasLen and hasPct record the types the value holds, whatever the
	// coefficients: calc(10px + 0%) is still a mix (CSS Values 4, 10.8).
	hasLen, hasPct bool
	// cmp, when set, is a comparison function the value adds cmpK times:
	// a min(), max() or clamp() whose arguments only resolve at computed
	// value time (CSS Values 4, 10.2). It is shared and never mutated.
	cmp  *calcCmp
	cmpK float64
	// bad marks a sum of two comparison functions, which a length cannot
	// hold: the value is invalid.
	bad bool
}

// calcCmp is a min() or max() of lengths (CSS Values 4, 10.2); clamp(a, b,
// c) is max(a, min(b, c)).
type calcCmp struct {
	max  bool
	args []length
}

// eval resolves the comparison against c.
func (m *calcCmp) eval(c lenCtx) float64 {
	v := 0.0
	for i, a := range m.args {
		x := a.points(c)
		if i == 0 || m.max && x > v || !m.max && x < v {
			v = x
		}
	}
	return v
}

func (l length) mixed() bool { return l.hasLen && l.hasPct }

// zeroLen reports a length part that is zero in every unit, as in
// calc(50% - 0px): with a percentage the value is that percentage alone.
func (l length) zeroLen() bool {
	return l.cmp == nil && l.pt == 0 && l.em == 0 && l.ex == 0 && l.ch == 0 && l.rem == 0 &&
		l.vw == 0 && l.vh == 0 && l.vmin == 0 && l.vmax == 0 && l.lh == 0 && l.rlh == 0
}

func (l length) scale(k float64) length {
	l.pt *= k
	l.em *= k
	l.ex *= k
	l.ch *= k
	l.rem *= k
	l.vw *= k
	l.vh *= k
	l.vmin *= k
	l.vmax *= k
	l.lh *= k
	l.rlh *= k
	l.pct *= k
	l.cmpK *= k
	return l
}

func (l length) add(o length) length {
	cmp, k := l.cmp, l.cmpK
	if o.cmp != nil {
		cmp, k = o.cmp, o.cmpK
	}
	return length{
		cmp: cmp, cmpK: k, bad: l.bad || o.bad || l.cmp != nil && o.cmp != nil,
		pt: l.pt + o.pt, em: l.em + o.em, ex: l.ex + o.ex, ch: l.ch + o.ch, rem: l.rem + o.rem,
		vw: l.vw + o.vw, vh: l.vh + o.vh, vmin: l.vmin + o.vmin, vmax: l.vmax + o.vmax,
		lh: l.lh + o.lh, rlh: l.rlh + o.rlh,
		pct: l.pct + o.pct, hasLen: l.hasLen || o.hasLen, hasPct: l.hasPct || o.hasPct,
	}
}

// lenCtx is what resolves a length to points: the element's font size,
// the root's, and the page box size for the viewport units.
type lenCtx struct {
	fontSize, rootFontSize float64
	// ex and ch are the font's x-height and "0" advance as fractions of
	// its size, 0 for the half-em fallback.
	ex, ch        float64
	width, height float64
	// lineHeight and rootLineHeight are the used line heights the lh and
	// rlh units resolve against, in points: the element's, its parent's
	// while line-height itself computes, and the root's.
	lineHeight, rootLineHeight float64
}

// points resolves the length part to points, clamped to maxLength.
func (l length) points(c lenCtx) float64 {
	vmin, vmax := math.Min(c.width, c.height), math.Max(c.width, c.height)
	ex, ch := c.ex, c.ch
	if ex <= 0 {
		ex = 0.5
	}
	if ch <= 0 {
		ch = 0.5
	}
	v := l.pt + (l.em+l.ex*ex+l.ch*ch)*c.fontSize + l.rem*c.rootFontSize +
		(l.vw*c.width+l.vh*c.height+l.vmin*vmin+l.vmax*vmax)/100 +
		l.lh*c.lineHeight + l.rlh*c.rootLineHeight
	if l.cmp != nil {
		v += l.cmpK * l.cmp.eval(c)
	}
	return clampLen(v)
}

func clampLen(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v > maxLength:
		return maxLength
	case v < -maxLength:
		return -maxLength
	}
	return v
}

// dim computes the length: a pure percentage stays Percent, anything
// else becomes Points. nonNeg clamps a negative calc() result to zero as
// CSS Values 4, 10.12 asks.
func (l length) dim(c lenCtx, nonNeg bool) Dimension {
	if l.hasPct && !l.hasLen {
		v := l.pct / 100
		if nonNeg && v < 0 {
			v = 0
		}
		return Dimension{Kind: Percent, Value: clampLen(v)}
	}
	v := l.points(c)
	if nonNeg && v < 0 {
		v = 0
	}
	return Pts(v)
}

// absUnits maps the absolute length units to points (CSS Values 4, 6.2).
var absUnits = map[string]float64{
	"px": Pt, "pt": 1, "pc": 12, "in": 72, "cm": 72 / 2.54, "mm": 72 / 25.4, "q": 72 / 101.6,
}

// unitLength returns the length of n units, false for a unit litho does
// not support.
func unitLength(n float64, unit string) (length, bool) {
	unit = lower(unit)
	if k, ok := absUnits[unit]; ok {
		return length{pt: n * k, hasLen: true}, true
	}
	l := length{hasLen: true}
	switch unit {
	case "em":
		l.em = n
	case "ex":
		l.ex = n
	case "ch":
		l.ch = n
	case "rem":
		l.rem = n
	case "vw":
		l.vw = n
	case "vh":
		l.vh = n
	case "vmin":
		l.vmin = n
	case "vmax":
		l.vmax = n
	case "lh":
		l.lh = n
	case "rlh":
		l.rlh = n
	default:
		return length{}, false
	}
	return l, true
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// lengthFlags say what a property's <length> accepts.
type lengthFlags uint8

const (
	// allowPct accepts percentages.
	allowPct lengthFlags = 1 << iota
	// nonNeg rejects negative values (a negative calc() clamps instead).
	nonNeg
	// allowMixed accepts a calc() mixing lengths and percentages, for the
	// properties that resolve percentages at computed-value time.
	allowMixed
)

// isCalc reports whether t is a math function: calc(), prefixed or not,
// or one of the comparison functions min(), max() and clamp() (CSS Values
// 4, 10.1 and 10.2).
func isCalc(t *token) bool {
	if t.typ != tFunction {
		return false
	}
	switch lower(t.value) {
	case "calc", "-webkit-calc", "-moz-calc", "min", "max", "clamp":
		return true
	}
	return false
}

// maxCmpArgs caps the arguments of one min() or max(): 32. Real styles
// compare two or three values. Anti-DoS guard, not spec-mandated.
const maxCmpArgs = 32

// calcOf evaluates the math function t, which [isCalc] accepts.
func calcOf(t *token, depth int) (calcValue, bool) {
	name := lower(t.value)
	if name != "min" && name != "max" && name != "clamp" {
		return parseCalc(t.children, depth)
	}
	if depth > maxCalcDepth {
		return calcValue{}, false
	}
	parts := splitCommas(t.children)
	if len(parts) == 0 || len(parts) > maxCmpArgs || name == "clamp" && len(parts) != 3 {
		return calcValue{}, false
	}
	vals := make([]calcValue, len(parts))
	for i, p := range parts {
		v, ok := parseCalc(p, depth+1)
		if !ok || v.isNum != vals[0].isNum && i > 0 || v.l.hasPct || v.l.bad {
			// A percentage inside a comparison resolves only at used
			// value time, which a computed length cannot defer.
			return calcValue{}, false
		}
		vals[i] = v
	}
	if name == "clamp" {
		inner, ok := compare(vals[1:], false)
		if !ok {
			return calcValue{}, false
		}
		return compare([]calcValue{vals[0], inner}, true)
	}
	return compare(vals, name == "max")
}

// compare returns the minimum of vals, or their maximum when isMax: a
// number or a plain length at once, a deferred [calcCmp] otherwise.
func compare(vals []calcValue, isMax bool) (calcValue, bool) {
	if vals[0].isNum {
		v := vals[0].num
		for _, x := range vals[1:] {
			if isMax && x.num > v || !isMax && x.num < v {
				v = x.num
			}
		}
		return calcValue{isNum: true, num: v}, finite(v)
	}
	plain := true
	for _, x := range vals {
		if x.l.cmp != nil || x.l.lh != 0 || x.l.rlh != 0 || x.l.em != 0 || x.l.ex != 0 || x.l.ch != 0 || x.l.rem != 0 || x.l.vw != 0 || x.l.vh != 0 || x.l.vmin != 0 || x.l.vmax != 0 {
			plain = false
		}
	}
	if plain {
		v := vals[0].l.pt
		for _, x := range vals[1:] {
			if isMax && x.l.pt > v || !isMax && x.l.pt < v {
				v = x.l.pt
			}
		}
		return calcValue{l: length{pt: v, hasLen: true}}, true
	}
	m := &calcCmp{max: isMax, args: make([]length, len(vals))}
	for i, x := range vals {
		m.args[i] = x.l
	}
	return calcValue{l: length{hasLen: true, cmp: m, cmpK: 1}}, true
}

// parseLength parses a <length> or <length-percentage> component value
// (CSS Values 4, 6 and 5.6). A unitless zero is a length (CSS Values 4,
// 6.1).
func parseLength(t token, f lengthFlags) (length, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		if t.num == 0 {
			return length{hasLen: true}, true
		}
	case tDimension:
		if !finite(t.num) || (f&nonNeg != 0 && t.num < 0) {
			return length{}, false
		}
		return unitLength(t.num, t.value)
	case tPercentage:
		if f&allowPct == 0 || !finite(t.num) || (f&nonNeg != 0 && t.num < 0) {
			return length{}, false
		}
		return length{pct: t.num, hasPct: true}, true
	case tFunction:
		if !isCalc(&t) {
			return length{}, false
		}
		v, ok := calcOf(&t, 0)
		if !ok || v.isNum || v.l.bad {
			return length{}, false
		}
		if v.l.hasPct && f&allowPct == 0 {
			return length{}, false
		}
		if v.l.hasPct && v.l.zeroLen() {
			// A length part that cancels out leaves a percentage, which
			// resolves as one (CSS Values 4, 10.9: the used value is the
			// same).
			v.l.hasLen = false
		}
		if v.l.mixed() && f&allowMixed == 0 {
			return length{}, false
		}
		return v.l, true
	}
	return length{}, false
}

// calcValue is the value of a calc() sub-expression: a number, or a
// length-percentage.
type calcValue struct {
	isNum bool
	num   float64
	l     length
}

// calcCursor walks the component values inside calc().
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

// parseCalc parses the contents of calc() or of a parenthesised
// sub-expression (CSS Values 4, 10.1): <calc-sum>. Multiplication needs a
// number on one side and division a non-zero number on the right, so every
// result stays a linear combination. min(), max() and clamp() are not
// supported.
func parseCalc(list []token, depth int) (calcValue, bool) {
	if depth > maxCalcDepth {
		return calcValue{}, false
	}
	c := &calcCursor{l: list}
	c.ws()
	sum, ok := calcProduct(c, depth)
	if !ok {
		return calcValue{}, false
	}
	for {
		hadWS := c.ws()
		t := c.peek()
		if t == nil {
			break
		}
		if !hadWS || !t.isDelim('+') && !t.isDelim('-') {
			return calcValue{}, false
		}
		neg := t.isDelim('-')
		c.i++
		if !c.ws() {
			return calcValue{}, false
		}
		rhs, ok := calcProduct(c, depth)
		if !ok || rhs.isNum != sum.isNum {
			return calcValue{}, false
		}
		if neg {
			rhs.num, rhs.l = -rhs.num, rhs.l.scale(-1)
		}
		sum.num += rhs.num
		sum.l = sum.l.add(rhs.l)
	}
	if !finite(sum.num) {
		return calcValue{}, false
	}
	return sum, true
}

// calcProduct parses a <calc-product>.
func calcProduct(c *calcCursor, depth int) (calcValue, bool) {
	v, ok := calcLeaf(c, depth)
	if !ok {
		return calcValue{}, false
	}
	for {
		save := c.i
		c.ws()
		t := c.peek()
		if t == nil || !t.isDelim('*') && !t.isDelim('/') {
			c.i = save
			return v, true
		}
		div := t.isDelim('/')
		c.i++
		c.ws()
		rhs, ok := calcLeaf(c, depth)
		if !ok {
			return calcValue{}, false
		}
		switch {
		case div:
			if !rhs.isNum || rhs.num == 0 {
				return calcValue{}, false
			}
			v.num /= rhs.num
			v.l = v.l.scale(1 / rhs.num)
		case rhs.isNum:
			v.num *= rhs.num
			v.l = v.l.scale(rhs.num)
		case v.isNum:
			rhs.l = rhs.l.scale(v.num)
			v = rhs
		default:
			return calcValue{}, false
		}
	}
}

// calcLeaf parses a <calc-value>.
func calcLeaf(c *calcCursor, depth int) (calcValue, bool) {
	t := c.peek()
	if t == nil {
		return calcValue{}, false
	}
	c.i++
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return calcValue{isNum: true, num: t.num}, finite(t.num)
	case tDimension:
		if !finite(t.num) {
			return calcValue{}, false
		}
		l, ok := unitLength(t.num, t.value)
		return calcValue{l: l}, ok
	case tPercentage:
		return calcValue{l: length{pct: t.num, hasPct: true}}, finite(t.num)
	case tLParen:
		return parseCalc(t.children, depth+1)
	case tFunction:
		if isCalc(t) {
			return calcOf(t, depth+1)
		}
	case tIdent:
		// CSS Values 4, 10.7.1: the numeric constants. An infinite value
		// stands for the largest one representable (10.9), here a huge
		// finite number every computed length clamps.
		switch lower(t.value) {
		case "e":
			return calcValue{isNum: true, num: math.E}, true
		case "pi":
			return calcValue{isNum: true, num: math.Pi}, true
		case "infinity":
			return calcValue{isNum: true, num: calcInfinity}, true
		case "-infinity":
			return calcValue{isNum: true, num: -calcInfinity}, true
		}
	}
	return calcValue{}, false
}

// calcInfinity is the value of the infinity constant: finite, so sums and
// products stay finite, and far past maxLength, so a length using it
// clamps there.
const calcInfinity = 1e100

// parseNumber parses a <number>, calc() included (CSS Values 4, 5.4).
func parseNumber(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return t.num, finite(t.num)
	case tFunction:
		if isCalc(&t) {
			v, ok := calcOf(&t, 0)
			if ok && v.isNum {
				return v.num, true
			}
		}
	}
	return 0, false
}

// parseInteger parses an <integer> (CSS Values 4, 5.3).
func parseInteger(t token) (int, bool) {
	if t.typ == tNumber && t.integer && finite(t.num) && math.Abs(t.num) < 1<<30 {
		return int(t.num), true
	}
	return 0, false
}

// parseAngle parses an <angle> or a bare number, in degrees (CSS Values 4,
// 7.1).
func parseAngle(t token) (float64, bool) {
	if !finite(t.num) {
		return 0, false
	}
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return t.num, true
	case tDimension:
		switch lower(t.value) {
		case "deg":
			return t.num, true
		case "grad":
			return t.num * 0.9, true
		case "rad":
			return t.num * 180 / math.Pi, true
		case "turn":
			return t.num * 360, true
		}
	}
	return 0, false
}

// parseURL parses a <url> (CSS Values 4, 4.5): a url token, or url() or
// src() with a string, resolved against base.
func parseURL(t token, base string) (string, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tURL:
		return resolveURL(base, t.value), true
	case tFunction:
		n := lower(t.value)
		if n != "url" && n != "src" {
			return "", false
		}
		args := significant(t.children)
		if len(args) != 1 || args[0].typ != tString {
			return "", false
		}
		return resolveURL(base, args[0].value), true
	}
	return "", false
}

// resolveURL resolves ref against base, returning ref unchanged when
// either does not parse or base is empty. A fragment-only URL stays as
// written: it names something in the document.
func resolveURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if base == "" || ref == "" || strings.HasPrefix(ref, "#") {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// colorValue is a specified <color>: a colour or currentColor.
type colorValue struct {
	c       Color
	current bool
}

// parseColor parses a <color> (CSS Color 4, 4.1): a named colour,
// transparent, currentColor, a hex colour, or rgb(), rgba(), hsl() and
// hsla() in their legacy and modern syntaxes.
func parseColor(t token) (colorValue, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		n := lower(t.value)
		switch n {
		case "transparent":
			return colorValue{}, true
		case "currentcolor":
			return colorValue{current: true}, true
		}
		if rgb, ok := namedColors[n]; ok {
			return colorValue{c: rgb8(rgb[0], rgb[1], rgb[2])}, true
		}
	case tHash:
		return parseHex(t.value)
	case tFunction:
		switch lower(t.value) {
		case "rgb", "rgba":
			return parseRGB(t.children)
		case "hsl", "hsla":
			return parseHSL(t.children)
		}
	}
	return colorValue{}, false
}

func rgb8(r, g, b uint8) Color {
	return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}
}

// parseHex parses the digits of a hex colour (CSS Color 4, 5.2).
func parseHex(s string) (colorValue, bool) {
	var d [8]float64
	n := len(s)
	if n != 3 && n != 4 && n != 6 && n != 8 {
		return colorValue{}, false
	}
	for i := 0; i < n; i++ {
		c := rune(s[i])
		if !isHexDigit(c) {
			return colorValue{}, false
		}
		switch {
		case c >= 'a':
			d[i] = float64(c - 'a' + 10)
		case c >= 'A':
			d[i] = float64(c - 'A' + 10)
		default:
			d[i] = float64(c - '0')
		}
	}
	ch := func(i int) float64 {
		if n <= 4 {
			return d[i] * 17 / 255
		}
		return (d[2*i]*16 + d[2*i+1]) / 255
	}
	c := Color{R: ch(0), G: ch(1), B: ch(2), A: 1}
	if n == 4 || n == 8 {
		c.A = ch(3)
	}
	return colorValue{c: c}, true
}

// colorArgs splits a colour function's arguments into its channels and
// alpha, in the legacy comma syntax or the modern space syntax (CSS Color
// 4, 5.1). legacy reports which.
func colorArgs(list []token) (ch []token, alpha *token, legacy bool, ok bool) {
	args := significant(list)
	for _, a := range args {
		if a.typ == tComma {
			legacy = true
		}
	}
	if legacy {
		if len(args) != 5 && len(args) != 7 {
			return nil, nil, false, false
		}
		for i := 1; i < len(args); i += 2 {
			if args[i].typ != tComma {
				return nil, nil, false, false
			}
		}
		ch = []token{args[0], args[2], args[4]}
		for _, c := range ch {
			if c.isIdent("none") {
				return nil, nil, false, false
			}
		}
		if len(args) == 7 {
			alpha = &args[6]
		}
		return ch, alpha, true, true
	}
	switch {
	case len(args) == 3:
		return args, nil, false, true
	case len(args) == 5 && args[3].isDelim('/'):
		return args[:3], &args[4], false, true
	}
	return nil, nil, false, false
}

// numberOrPct parses a <number>, a <percentage> or none (modern syntax
// only), returning the number, the fraction for a percentage, and which.
func numberOrPct(t token, allowNone bool) (v float64, pct bool, ok bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tPercentage:
		return t.num / 100, true, finite(t.num)
	case tIdent:
		if allowNone && t.isIdent("none") {
			return 0, false, true
		}
	case tFunction:
		if isCalc(&t) {
			c, ok := calcOf(&t, 0)
			if !ok {
				return 0, false, false
			}
			if c.isNum {
				return c.num, false, true
			}
			if c.l.hasPct && !c.l.hasLen {
				return c.l.pct / 100, true, true
			}
			return 0, false, false
		}
	}
	n, ok := parseNumber(t)
	return n, false, ok
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// parseAlpha parses an <alpha-value> (CSS Color 4, 4.2).
func parseAlpha(t *token, legacy bool) (float64, bool) {
	if t == nil {
		return 1, true
	}
	v, _, ok := numberOrPct(*t, !legacy)
	return clamp01(v), ok
}

// parseRGB parses the arguments of rgb() and rgba() (CSS Color 4, 5.1).
// The legacy syntax takes three numbers or three percentages.
func parseRGB(list []token) (colorValue, bool) {
	ch, alpha, legacy, ok := colorArgs(list)
	if !ok {
		return colorValue{}, false
	}
	var rgb [3]float64
	var pcts [3]bool
	for i, t := range ch {
		v, pct, ok := numberOrPct(t, !legacy)
		if !ok {
			return colorValue{}, false
		}
		pcts[i] = pct
		if !pct {
			v /= 255
		}
		rgb[i] = clamp01(v)
	}
	if legacy && (pcts[0] != pcts[1] || pcts[1] != pcts[2]) {
		return colorValue{}, false
	}
	a, ok := parseAlpha(alpha, legacy)
	if !ok {
		return colorValue{}, false
	}
	return colorValue{c: Color{R: rgb[0], G: rgb[1], B: rgb[2], A: a}}, true
}

// parseHSL parses the arguments of hsl() and hsla() (CSS Color 4, 7.1).
// The legacy syntax takes percentages for saturation and lightness.
func parseHSL(list []token) (colorValue, bool) {
	ch, alpha, legacy, ok := colorArgs(list)
	if !ok {
		return colorValue{}, false
	}
	var h float64
	if !legacy && ch[0].isIdent("none") {
		h = 0
	} else if h, ok = parseAngle(ch[0]); !ok {
		if ch[0].typ != tFunction {
			return colorValue{}, false
		}
		if h, ok = parseNumber(ch[0]); !ok {
			return colorValue{}, false
		}
	}
	var sl [2]float64
	for i, t := range ch[1:] {
		v, pct, ok := numberOrPct(t, !legacy)
		if !ok || (legacy && !pct) {
			return colorValue{}, false
		}
		if !pct {
			v /= 100
		}
		sl[i] = clamp01(v)
	}
	a, ok := parseAlpha(alpha, legacy)
	if !ok {
		return colorValue{}, false
	}
	r, g, b := hslToRGB(h, sl[0], sl[1])
	return colorValue{c: Color{R: r, G: g, B: b, A: a}}, true
}

// hslToRGB converts HSL to sRGB (CSS Color 4, 7.1, the sample code).
func hslToRGB(h, s, l float64) (r, g, b float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))
	}
	return clamp01(f(0)), clamp01(f(8)), clamp01(f(4))
}
