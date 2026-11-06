package svg

import (
	"math"
	"strconv"
	"strings"
)

// scanner reads the numbers, flags and separators of an attribute value:
// path data (SVG 1.1, 8.3.9), point lists (9.7.1), transform lists (7.6)
// and number lists.
type scanner struct {
	s string
	i int
}

// isWSP reports an SVG white space character (SVG 1.1, 8.3.9 wsp).
func isWSP(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// skipWSP skips white space.
func (sc *scanner) skipWSP() {
	for sc.i < len(sc.s) && isWSP(sc.s[sc.i]) {
		sc.i++
	}
}

// skipSep skips white space and at most one comma (comma-wsp).
func (sc *scanner) skipSep() {
	sc.skipWSP()
	if sc.i < len(sc.s) && sc.s[sc.i] == ',' {
		sc.i++
		sc.skipWSP()
	}
}

// done reports whether only white space is left.
func (sc *scanner) done() bool {
	sc.skipWSP()
	return sc.i >= len(sc.s)
}

// peek returns the next byte, 0 at the end.
func (sc *scanner) peek() byte {
	if sc.i < len(sc.s) {
		return sc.s[sc.i]
	}
	return 0
}

// number reads one number (SVG 1.1, 8.3.9 number; CSS Syntax 3, 4.3.12
// for the exponent): a sign, digits with an optional fraction, an
// optional exponent. "1.5.5" reads as 1.5 then .5, and "1-2" as 1 then -2.
// It reports false, leaving the position unchanged, when no number starts
// here.
func (sc *scanner) number() (float64, bool) {
	start := sc.i
	j := sc.i
	if j < len(sc.s) && (sc.s[j] == '+' || sc.s[j] == '-') {
		j++
	}
	digits := 0
	for j < len(sc.s) && sc.s[j] >= '0' && sc.s[j] <= '9' {
		j++
		digits++
	}
	if j < len(sc.s) && sc.s[j] == '.' {
		k := j + 1
		frac := 0
		for k < len(sc.s) && sc.s[k] >= '0' && sc.s[k] <= '9' {
			k++
			frac++
		}
		if frac > 0 || digits > 0 {
			j = k
			digits += frac
		}
	}
	if digits == 0 {
		return 0, false
	}
	if j < len(sc.s) && (sc.s[j] == 'e' || sc.s[j] == 'E') {
		k := j + 1
		if k < len(sc.s) && (sc.s[k] == '+' || sc.s[k] == '-') {
			k++
		}
		exp := 0
		for k < len(sc.s) && sc.s[k] >= '0' && sc.s[k] <= '9' {
			k++
			exp++
		}
		if exp > 0 {
			j = k
		}
	}
	v, err := strconv.ParseFloat(sc.s[start:j], 64)
	if err != nil && !math.IsInf(v, 0) {
		// The text is a valid float, so only a range error is possible,
		// and it leaves ±Inf, which clampCoord bounds below.
		v = 0
	}
	sc.i = j
	return clampCoord(v), true
}

// flag reads an arc flag, a single 0 or 1 (SVG 1.1, 8.3.9 flag).
func (sc *scanner) flag() (bool, bool) {
	switch sc.peek() {
	case '0':
		sc.i++
		return false, true
	case '1':
		sc.i++
		return true, true
	}
	return false, false
}

// parseNumber parses a whole attribute value as one number.
func parseNumber(s string) (float64, bool) {
	sc := scanner{s: s}
	sc.skipWSP()
	v, ok := sc.number()
	if !ok || !sc.done() {
		return 0, false
	}
	return v, true
}

// parseNumberList parses a list of numbers separated by white space or
// commas (SVG 1.1, 4.1), at most limit of them. It reports false when the
// list holds anything else.
func parseNumberList(s string, limit int) ([]float64, bool) {
	sc := scanner{s: s}
	var out []float64
	sc.skipWSP()
	for !sc.done() {
		v, ok := sc.number()
		if !ok || len(out) >= limit {
			return out, false
		}
		out = append(out, v)
		sc.skipSep()
	}
	return out, true
}

// unit is the unit of a [length].
type unit uint8

// The length units (SVG 1.1, 7.10; CSS Values 3, 5 and 6).
const (
	unitNone unit = iota
	unitPx
	unitPt
	unitPc
	unitMm
	unitCm
	unitIn
	unitQ
	unitEm
	unitEx
	unitRem
	unitPercent
)

// pxPer maps an absolute unit to its size in user units, CSS pixels at 96
// per inch (CSS Values 3, 6.2).
var pxPer = map[unit]float64{
	unitNone: 1, unitPx: 1, unitPt: 96.0 / 72, unitPc: 16, unitMm: 96 / 25.4,
	unitCm: 96 / 2.54, unitIn: 96, unitQ: 96 / 101.6,
}

// units maps a unit's spelling to it.
var units = map[string]unit{
	"": unitNone, "px": unitPx, "pt": unitPt, "pc": unitPc, "mm": unitMm, "cm": unitCm,
	"in": unitIn, "q": unitQ, "em": unitEm, "ex": unitEx, "rem": unitRem, "%": unitPercent,
}

// length is a length as written: a number and its unit.
type length struct {
	v float64
	u unit
}

// axis says what a percentage length refers to (SVG 1.1, 7.10).
type axis uint8

const (
	axisX axis = iota
	axisY
	axisDiag
)

// lengthCtx is what resolving a length needs: the font size for em and ex,
// and the viewport for percentages.
type lengthCtx struct {
	fontSize float64
	vw, vh   float64
}

// resolve returns l in user units.
func (l length) resolve(c lengthCtx, a axis) float64 {
	switch l.u {
	case unitEm, unitRem:
		return clampCoord(l.v * c.fontSize)
	case unitEx:
		// The x-height is taken as half the font size, the fallback CSS
		// Values 3, 5.1.1 allows when the font's is not at hand.
		return clampCoord(l.v * c.fontSize / 2)
	case unitPercent:
		ref := c.vw
		switch a {
		case axisY:
			ref = c.vh
		case axisDiag:
			ref = math.Sqrt(c.vw*c.vw+c.vh*c.vh) / math.Sqrt2
		case axisX:
		}
		return clampCoord(l.v / 100 * ref)
	case unitNone, unitPx, unitPt, unitPc, unitMm, unitCm, unitIn, unitQ:
	}
	return clampCoord(l.v * pxPer[l.u])
}

// parseLength parses a length (SVG 1.1, 4.2): a number and an optional
// unit, case-insensitively.
func parseLength(s string) (length, bool) {
	sc := scanner{s: strings.TrimSpace(s)}
	v, ok := sc.number()
	if !ok {
		return length{}, false
	}
	u, ok := units[strings.ToLower(sc.s[sc.i:])]
	if !ok {
		return length{}, false
	}
	return length{v: v, u: u}, true
}

// parseLengthList parses a list of lengths separated by white space or
// commas, at most limit of them.
func parseLengthList(s string, limit int) ([]length, bool) {
	var out []length
	for f := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }) {
		if len(out) >= limit {
			return out, false
		}
		l, ok := parseLength(f)
		if !ok {
			return out, false
		}
		out = append(out, l)
	}
	return out, true
}
