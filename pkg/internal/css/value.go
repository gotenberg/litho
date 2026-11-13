package css

import (
	"math"
	"net/url"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// length is a specified <length-percentage>: a linear combination of units
// and of a percentage (CSS Values 4, 10.10), or a calculation tree that is
// not one, such as min(100%, 30em). A plain "12px" sets the points alone.
type length struct {
	// u holds the coefficient of each unit, num a number, which only a
	// calculation folding a <number> holds.
	u   [numLenUnits]float64
	pct float64
	num float64
	// hasLen and hasPct record the types the value holds, whatever the
	// coefficients: calc(10px + 0%) is still a mix (CSS Values 4, 10.8).
	// A tree with a percentage is a mix whatever its leaves.
	hasLen, hasPct bool
	// expr, when set, is a calculation that is not linear, evaluated when
	// its units are known; the coefficients are then zero. It is shared
	// and never mutated.
	expr *calcNode
}

// constant reports a value of no unit and no percentage: a number.
func (l length) constant() bool {
	return !l.hasLen && !l.hasPct && l.expr == nil
}

func (l length) mixed() bool { return l.hasLen && l.hasPct }

// zeroLen reports a length part that is zero in every unit, as in
// calc(50% - 0px): with a percentage the value is that percentage alone.
func (l length) zeroLen() bool {
	if l.expr != nil {
		return false
	}
	for _, v := range l.u {
		if v != 0 {
			return false
		}
	}
	return true
}

// viewportRelative reports a length that depends on the page size, which
// a media query, a page size and a bleed cannot.
func (l length) viewportRelative() bool {
	if l.expr != nil {
		return l.expr.usesUnit(uVw, uVh, uVmin, uVmax, uVi, uVb)
	}
	return l.u[uVw] != 0 || l.u[uVh] != 0 || l.u[uVmin] != 0 || l.u[uVmax] != 0 || l.u[uVi] != 0 || l.u[uVb] != 0
}

func (l length) scale(k float64) length {
	for i := range l.u {
		l.u[i] *= k
	}
	l.pct *= k
	l.num *= k
	return l
}

func (l length) add(o length) length {
	for i := range l.u {
		l.u[i] += o.u[i]
	}
	l.pct += o.pct
	l.num += o.num
	l.hasLen = l.hasLen || o.hasLen
	l.hasPct = l.hasPct || o.hasPct
	return l
}

// lenCtx is what resolves a length to points: the element's font size,
// the root's, and the page box size for the viewport units.
type lenCtx struct {
	fontSize, rootFontSize float64
	// font and rootFont are the measures of the element's first
	// available font and of the root's, as fractions of their sizes.
	font, rootFont FontUnits
	width, height  float64
	// lineHeight and rootLineHeight are the used line heights the lh and
	// rlh units resolve against, in points: the element's, its parent's
	// while line-height itself computes, and the root's.
	lineHeight, rootLineHeight float64
	// verticalRoot swaps the vi and vb units, which follow the root's
	// inline and block axes (CSS Values 4, 6.1.2.2).
	verticalRoot bool
}

// unitSizes returns the size of each unit in points.
func (c *lenCtx) unitSizes() [numLenUnits]float64 {
	var s [numLenUnits]float64
	s[uPt] = 1
	s[uEm] = c.fontSize
	s[uRem] = c.rootFontSize
	s[uEx], s[uCh], s[uCap], s[uIc] = c.font.sizes(c.fontSize)
	s[uRex], s[uRch], s[uRcap], s[uRic] = c.rootFont.sizes(c.rootFontSize)
	s[uVw], s[uVh] = c.width/100, c.height/100
	s[uVmin], s[uVmax] = math.Min(s[uVw], s[uVh]), math.Max(s[uVw], s[uVh])
	s[uVi], s[uVb] = s[uVw], s[uVh]
	if c.verticalRoot {
		s[uVi], s[uVb] = s[uVb], s[uVi]
	}
	s[uLh], s[uRlh] = c.lineHeight, c.rootLineHeight
	return s
}

// FontUnits are the measures of a font the ex, ch, cap and ic units and
// their root forms resolve against (CSS Values 4, 6.1.1), as fractions of
// the font size. A measure that is not positive takes the fallback CSS
// Values 4, 6.1.1 gives when the font has none: half an em for ex and ch,
// one em for ic, and for cap the ascent, or 0.7 em without one.
type FontUnits struct {
	// XHeight is the x-height, Zero the advance of "0", CapHeight the
	// height of a capital letter, Ascent the font's ascent, and
	// Ideograph the advance of "水".
	XHeight, Zero, CapHeight, Ascent, Ideograph float64
}

// fallbackCap is the cap height, in ems, of a font that records neither a
// cap height nor an ascent: the cap height of the common Latin faces.
const fallbackCap = 0.7

// sizes returns the ex, ch, cap and ic of the font at size, in points.
func (f FontUnits) sizes(size float64) (ex, ch, capH, ic float64) {
	pick := func(v, fallback float64) float64 {
		if v > 0 && numeric.Finite(v) {
			return v * size
		}
		return fallback * size
	}
	asc := pick(f.Ascent, fallbackCap) / size
	if size == 0 {
		asc = fallbackCap
	}
	return pick(f.XHeight, 0.5), pick(f.Zero, 0.5), pick(f.CapHeight, asc), pick(f.Ideograph, 1)
}

// points resolves the length part to points, clamped to maxLength. A
// percentage in a calculation tree counts as zero.
func (l length) points(c lenCtx) float64 {
	return l.resolve(c, 0)
}

// resolve resolves the length to points against basis, the size its
// percentage is of, clamped to maxLength.
func (l length) resolve(c lenCtx, basis float64) float64 {
	sizes := c.unitSizes()
	if l.expr != nil {
		return clampLen(censor(l.expr.eval(&calcScope{units: &sizes, basis: basis})))
	}
	v := l.pct / 100 * basis
	for i, k := range l.u {
		if k != 0 {
			v += k * sizes[i]
		}
	}
	return clampLen(censor(v))
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

// dim computes the length: a pure percentage stays Percent, a mix of a
// length and a percentage becomes Calc, anything else Points. nonNeg
// clamps a negative calc() result to zero as CSS Values 4, 10.12 asks; a
// Calc only knows its sign once resolved, so the layout engine clamps it
// at used-value time.
func (l length) dim(c lenCtx, nonNeg bool) Dimension {
	if l.expr != nil && l.hasPct {
		return l.exprDim(c, nonNeg)
	}
	if l.hasPct && !l.hasLen {
		v := l.pct / 100
		if nonNeg && v < 0 {
			v = 0
		}
		return Dimension{Kind: Percent, Value: clampLen(v)}
	}
	if l.mixed() {
		return Dimension{Kind: Calc, Value: l.points(c), Fraction: clampLen(l.pct / 100)}
	}
	v := l.points(c)
	if nonNeg && v < 0 {
		v = 0
	}
	return Pts(v)
}

// exprDim computes a calculation tree that holds a percentage: a linear
// one becomes a Calc of its length and percentage parts, any other keeps
// its tree, its units resolved, for the layout engine to evaluate against
// the percentage basis (CSS Values 4, 10.11).
func (l length) exprDim(c lenCtx, nonNeg bool) Dimension {
	sizes := c.unitSizes()
	n := l.expr.resolveUnits(&sizes)
	if lin, ok := n.linear(nil); ok {
		v := lin.u[uPt]
		if !lin.hasLen {
			pct := clampLen(censor(lin.pct / 100))
			if nonNeg && pct < 0 {
				pct = 0
			}
			return Dimension{Kind: Percent, Value: pct}
		}
		return Dimension{Kind: Calc, Value: clampLen(censor(v)), Fraction: clampLen(censor(lin.pct / 100))}
	}
	return Dimension{Kind: Calc, expr: n}
}

// ptLength returns the length of v points.
func ptLength(v float64) length {
	var l length
	l.u[uPt] = v
	l.hasLen = true
	return l
}

// absUnits maps the absolute length units to points (CSS Values 4, 6.2).
var absUnits = map[string]float64{
	"px": Pt, "pt": 1, "pc": 12, "in": 72, "cm": 72 / 2.54, "mm": 72 / 25.4, "q": 72 / 101.6,
}

// unitLength returns the length of n units, false for a unit litho does
// not support.
func unitLength(n float64, unit string) (length, bool) {
	unit = lower(unit)
	var l length
	l.hasLen = true
	if k, ok := absUnits[unit]; ok {
		l.u[uPt] = n * k
		return l, true
	}
	u, ok := lengthUnits[unit]
	if !ok {
		return length{}, false
	}
	l.u[u] = n
	return l, true
}

// lengthFlags say what a property's <length> accepts.
type lengthFlags uint8

const (
	// allowPct accepts percentages.
	allowPct lengthFlags = 1 << iota
	// nonNeg rejects negative values (a negative calc() clamps instead).
	nonNeg
	// allowMixed accepts a calc() mixing lengths and percentages: the
	// properties whose percentages resolve at computed-value time
	// (font-size, the decoration thickness and offset), and those the
	// layout engine resolves as a [Calc] (the box sizes, margins,
	// paddings, insets, gaps and flex-basis).
	allowMixed
)

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
		if !numeric.Finite(t.num) || (f&nonNeg != 0 && t.num < 0) {
			return length{}, false
		}
		return unitLength(t.num, t.value)
	case tPercentage:
		if f&allowPct == 0 || !numeric.Finite(t.num) || (f&nonNeg != 0 && t.num < 0) {
			return length{}, false
		}
		return length{pct: t.num, hasPct: true}, true
	case tFunction:
		if !isCalc(&t) {
			return length{}, false
		}
		l, ok := calcLength(&t)
		if !ok {
			return length{}, false
		}
		if l.hasPct && f&allowPct == 0 {
			return length{}, false
		}
		if l.hasPct && l.zeroLen() {
			// A length part that cancels out leaves a percentage, which
			// resolves as one (CSS Values 4, 10.9: the used value is the
			// same).
			l.hasLen = false
		}
		if l.mixed() && f&allowMixed == 0 {
			return length{}, false
		}
		return l, true
	}
	return length{}, false
}

// calcLength parses the math function t as a <length-percentage>: a
// linear combination when the tree folds to one, else the tree, which a
// percentage makes a mix of a length and a percentage.
func calcLength(t *token) (length, bool) {
	p := &calcParser{pctLength: true}
	n, ty, ok := p.fn(t, 0)
	if !ok || ty != typeLength {
		return length{}, false
	}
	if !n.relative() {
		var l length
		l.u[uPt] = n.eval(&calcScope{})
		l.hasLen = true
		return l, true
	}
	if l, ok := n.linear(nil); ok {
		l.num = 0
		return l, true
	}
	pct := n.hasPct()
	return length{expr: n, hasLen: true, hasPct: pct}, true
}

// usesUnit reports whether the tree holds one of units.
func (n *calcNode) usesUnit(units ...lenUnit) bool {
	if n.op == opLen {
		for _, u := range units {
			if n.unit == u {
				return true
			}
		}
		return false
	}
	for _, k := range n.kids {
		if k.usesUnit(units...) {
			return true
		}
	}
	return false
}

// parseNumber parses a <number>, a math function included (CSS Values 4,
// 5.4).
func parseNumber(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return t.num, numeric.Finite(t.num)
	case tFunction:
		if isCalc(&t) {
			return calcNumber(&t, nil)
		}
	}
	return 0, false
}

// maxInteger bounds the magnitude of an <integer>: 2^30. Anti-DoS guard,
// not spec-mandated (CSS Values 4, 5.3 sets no range): it keeps the value
// inside a 32-bit int, and counters, z-indexes and column counts stay far
// below it.
const maxInteger = 1 << 30

// parseInteger parses an <integer> (CSS Values 4, 5.3). A math function
// resolving to a number rounds to the nearest integer (CSS Values 4,
// 10.12).
func parseInteger(t token) (int, bool) {
	if t.typ == tNumber && t.integer && numeric.Finite(t.num) && math.Abs(t.num) < maxInteger {
		return int(t.num), true
	}
	if t.typ == tFunction && isCalc(&t) {
		if v, ok := calcNumber(&t, nil); ok {
			v = math.Max(-maxInteger+1, math.Min(maxInteger-1, math.Floor(v+0.5)))
			return int(v), true // fits int: clamped to ±maxInteger above
		}
	}
	return 0, false
}

// parseAngle parses an <angle> or a bare number, in degrees (CSS Values 4,
// 7.1), a math function of type <angle> included.
func parseAngle(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return t.num, numeric.Finite(t.num)
	case tDimension:
		if !numeric.Finite(t.num) {
			return 0, false
		}
		if o, ok := otherUnits[lower(t.value)]; ok && o.t == typeAngle {
			return t.num * o.k, true
		}
	case tFunction:
		if isCalc(&t) {
			if v, ty, ok := calcConst(&t, nil); ok && ty == typeAngle {
				return v, true
			}
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
	for i := range n {
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
		return t.num / 100, true, numeric.Finite(t.num)
	case tIdent:
		if allowNone && t.isIdent("none") {
			return 0, false, true
		}
	case tFunction:
		if isCalc(&t) {
			v, ty, ok := calcConst(&t, nil)
			switch {
			case !ok:
				return 0, false, false
			case ty == typeNumber:
				return v, false, true
			case ty == typePct:
				return v / 100, true, true
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
