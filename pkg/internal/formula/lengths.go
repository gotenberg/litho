package formula

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// length is a <length-percentage> attribute value (MathML Core, 2.1.2):
// a length in points, or a percentage as a fraction.
type length struct {
	v       float64
	percent bool
}

// absoluteUnits are the CSS absolute length units in points (CSS Values
// 4, 6.2).
var absoluteUnits = map[string]float64{
	"px": pxPt, "pt": 1, "pc": 12, "in": 72, "cm": 72 / 2.54, "mm": 72 / 25.4, "q": 72 / 101.6,
}

// parseLength reads v, a CSS <length-percentage> as MathML Core, 2.1.2
// takes it: a number with a unit, a percentage, or 0. em and ex resolve
// against st's font; the other relative units are not read.
func (l *layouter) parseLength(v string, st *Style) (length, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return length{}, false
	}
	i := 0
	for i < len(v) && (v[i] >= '0' && v[i] <= '9' || v[i] == '.' || v[i] == '-' || v[i] == '+' || v[i] == 'e' && i > 0 && i+1 < len(v) && (v[i+1] >= '0' && v[i+1] <= '9' || v[i+1] == '-' || v[i+1] == '+')) {
		i++
	}
	num, unit := v[:i], v[i:]
	n, err := strconv.ParseFloat(num, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || math.Abs(n) > maxLength {
		return length{}, false
	}
	switch unit {
	case "%":
		return length{v: n / 100, percent: true}, true
	case "":
		if n == 0 {
			return length{}, true
		}
		return length{}, false
	case "em":
		return length{v: float64(n * st.Size)}, true
	case "ex":
		f := l.faceOf(st)
		ex := st.Size / 2
		if f != nil {
			ex = f.units(f.xHeight, st.Size)
		}
		return length{v: float64(n * ex)}, true
	}
	if k, ok := absoluteUnits[unit]; ok {
		return length{v: float64(n * k)}, true
	}
	return length{}, false
}

// maxLength bounds the number of a length attribute. Not spec-mandated: a
// length is laid out in points, and nothing a formula holds is a thousand
// kilometres wide; the bound keeps every sum of lengths finite.
const maxLength = 1e7

// sizeAttr returns the font size a mathsize attribute v sets on an element
// whose parent is styled pst, a percentage of pst's font size, 0 when v
// is not valid (MathML Core, 2.1.5).
func (l *layouter) sizeAttr(v string, pst *Style) float64 {
	n, ok := l.parseLength(v, pst)
	if !ok {
		return 0
	}
	if n.percent {
		return min(float64(n.v*pst.Size), maxSize)
	}
	return min(n.v, maxSize)
}

// attrColor reads the colour attribute name of e, a CSS <color> (MathML
// Core, 2.1.5); currentColor keeps the inherited colour.
func attrColor(e *Element, name string) (typeset.Color, bool) {
	v, ok := e.Attr(name)
	if !ok {
		return typeset.Color{}, false
	}
	c, current, ok := css.ParseColor(v)
	if !ok || current {
		return typeset.Color{}, false
	}
	return typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}, true
}
