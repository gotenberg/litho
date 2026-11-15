package formula

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// Enclosure holds the measures menclose is drawn by (MathML 4, 3.3.9),
// in ems of the element's font size: [Style.Enclose] sets it for the
// formulas of a source whose producer draws the notations. MathML Core
// lays menclose out as an mrow, and Chromium draws none.
type Enclosure struct {
	// InlineGap and BlockGap are the space a line of a box, a rounded box,
	// a circle or a side keeps from the content: from its advance on the
	// inline sides, from its ink on the block sides.
	InlineGap, BlockGap float64
	// Rule is the thickness of every line; 0 takes the
	// FractionRuleThickness of the first available font.
	Rule float64
	// StrikeRaise, when not 0, is the height of a horizontal strike's
	// middle above the baseline; 0 strikes through the middle of what the
	// strike crosses.
	StrikeRaise float64
	// Notations, when not empty, are the notations drawn; the others draw
	// nothing. Empty draws every notation of 3.3.9.
	Notations []string
}

// maxNotations bounds the notations of one menclose read. Anti-DoS guard,
// not spec-mandated: 3.3.9 lists sixteen, a repeated one draws once, and
// the words past the bound are not read.
const maxNotations = 32

// enclosed is what the notations of one menclose draw.
type enclosed struct {
	sides                                  [4]bool
	rounded, circle, longdiv, phasor       bool
	horizontal, vertical, updiag, downdiag bool
}

// notations reads the notation attribute of e, the notations of 3.3.9
// that rules draws; ASCII case-insensitive, separated by white space. The
// deprecated radical draws nothing: MathML 4 points to msqrt.
func notations(e *Element, rules *Enclosure) enclosed {
	var n enclosed
	v, _ := e.Attr("notation")
	i := 0
	for w := range strings.FieldsSeq(v) {
		if i++; i > maxNotations {
			break
		}
		w = strings.ToLower(w)
		if len(rules.Notations) > 0 && !containsFold(rules.Notations, w) {
			continue
		}
		switch w {
		case "box":
			n.sides = [4]bool{true, true, true, true}
		case "roundedbox":
			n.rounded = true
		case "circle":
			n.circle = true
		case "top":
			n.sides[top] = true
		case "right":
			n.sides[right] = true
		case "bottom":
			n.sides[bottom] = true
		case "left":
			n.sides[left] = true
		case "actuarial":
			n.sides[top], n.sides[right] = true, true
		case "madruwb":
			n.sides[right], n.sides[bottom] = true, true
		case "longdiv":
			n.longdiv, n.sides[top] = true, true
		case "phasorangle":
			n.phasor, n.sides[bottom] = true, true
		case "horizontalstrike":
			n.horizontal = true
		case "verticalstrike":
			n.vertical = true
		case "updiagonalstrike":
			n.updiag = true
		case "downdiagonalstrike":
			n.downdiag = true
		}
	}
	return n
}

// containsFold reports whether list holds w, ASCII case-insensitively.
func containsFold(list []string, w string) bool {
	for _, s := range list {
		if strings.EqualFold(s, w) {
			return true
		}
	}
	return false
}

// menclose lays e out, styled st (MathML 4, 3.3.9): its in-flow children
// as an anonymous mrow, the content, within the lines its notations
// draw. Without [Style.Enclose] it is an mrow (MathML Core, 4.1, an
// element Core does not define). The notations draw over one another,
// none inside another: a box and a circle enclose the same content. A
// line keeps the rules' gap from the content and pads its side by the
// gap and its thickness; a strike crosses the padded content and pads
// nothing; a circle passes through the corners of the content and its
// gaps. The baseline is the content's.
func (l *layouter) menclose(e *Element, st *Style, c lctx, depth int) *box {
	inner := l.row(e, st, c, depth)
	rules := st.Enclose
	if rules == nil {
		return inner
	}
	n := notations(e, rules)
	em := st.Size
	rule := float64(rules.Rule * em)
	if rules.Rule <= 0 {
		rule = l.faceOf(st).constant(mt.FractionRuleThickness, em)
	}
	ig, bg := float64(rules.InlineGap*em), float64(rules.BlockGap*em)
	// The area the lines keep their gaps from: the content's advance
	// and ink, its baseline at 0.
	cw, ca, cd := inner.w, inner.inkAsc, inner.inkDesc
	boxed := n.rounded || n.circle
	var pad [4]float64
	for s := range 4 {
		if n.sides[s] || boxed || n.longdiv && s == left || n.phasor && s == left {
			gap := ig
			if s == top || s == bottom {
				gap = bg
			}
			pad[s] = gap + rule
		}
	}
	// The gapped area.
	gx0, gx1 := -max(pad[left]-rule, 0), cw+max(pad[right]-rule, 0)
	gy0, gy1 := -(ca + max(pad[top]-rule, 0)), cd+max(pad[bottom]-rule, 0)
	if n.circle {
		// The ellipse through the corners of the gapped area, of the same
		// proportions: its semi-axes are those of the area times √2.
		hw, hh := float64((gx1-gx0)/2), float64((gy1-gy0)/2)
		ex, ey := float64(hw*math.Sqrt2)-hw, float64(hh*math.Sqrt2)-hh
		pad[left], pad[right] = pad[left]+ex, pad[right]+ex
		pad[top], pad[bottom] = pad[top]+ey, pad[bottom]+ey
	}
	b := &box{
		w:   pad[left] + cw + pad[right],
		asc: max(inner.asc, ca+pad[top]), desc: max(inner.desc, cd+pad[bottom]),
		italic: inner.italic, op: inner.op,
	}
	b.inkAsc, b.inkDesc = max(inner.inkAsc, ca+pad[top]), max(inner.inkDesc, cd+pad[bottom])
	ox := pad[left]
	b.add(inner, ox, 0)
	if st.Hidden {
		return b
	}
	// The line rectangle, the middle of the lines of a box.
	half := float64(rule / 2)
	x0, x1 := ox+gx0-half, ox+gx1+half
	y0, y1 := gy0-half, gy1+half
	col := st.Color
	if n.sides[top] {
		b.fill(x0-half, y0-half, float64(x1-x0)+rule, rule, col)
	}
	if n.sides[bottom] {
		b.fill(x0-half, y1-half, float64(x1-x0)+rule, rule, col)
	}
	if n.sides[left] {
		b.fill(x0-half, y0-half, rule, float64(y1-y0)+rule, col)
	}
	if n.sides[right] {
		b.fill(x1-half, y0-half, rule, float64(y1-y0)+rule, col)
	}
	if n.rounded {
		r := min(float64((x1-x0)/4), float64((y1-y0)/4), float64(em/4))
		b.stroke(roundedOps(x0, y0, x1, y1, r), rule, col)
	}
	if n.circle {
		hw, hh := float64(float64((gx1-gx0)/2)*math.Sqrt2), float64(float64((gy1-gy0)/2)*math.Sqrt2)
		cx, cy := ox+float64((gx0+gx1)/2), float64((gy0+gy1)/2)
		b.stroke(ellipseOps(cx, cy, hw+half, hh+half), rule, col)
	}
	if n.longdiv {
		// The left side of a long division is a bracket bowed to the
		// right, from the top line down past the content.
		k := float64((x1 - x0) / 8)
		k = min(k, float64(em/4))
		b.stroke([]typeset.PathOp{
			moveTo(x0, y0),
			{Kind: typeset.CurveTo, P: [3]typeset.Point{{X: x0 + k, Y: y0 + float64((y1-y0)/3)}, {X: x0 + k, Y: y1 - float64((y1-y0)/3)}, {X: x0, Y: y1}}},
		}, rule, col)
	}
	if n.phasor {
		// The angle of a phasor rises from the bottom line's start to the
		// top of the gapped area, at the slope of its height over its
		// width at most.
		run := min(float64(x1-x0), float64(y1-y0))
		b.stroke([]typeset.PathOp{moveTo(x0, y1), lineTo(x0+run, y0)}, rule, col)
	}
	// Strikes cross the gapped area, the content alone when nothing pads
	// it.
	sx0, sx1, sy0, sy1 := ox+gx0, ox+gx1, gy0, gy1
	if n.horizontal {
		mid := float64((sy0 + sy1) / 2)
		if rules.StrikeRaise != 0 {
			mid = -float64(rules.StrikeRaise * em)
		}
		b.fill(sx0, mid-half, sx1-sx0, rule, col)
	}
	if n.vertical {
		mid := float64((sx0 + sx1) / 2)
		b.fill(mid-half, sy0, rule, sy1-sy0, col)
	}
	if n.updiag {
		b.stroke([]typeset.PathOp{moveTo(sx0, sy1), lineTo(sx1, sy0)}, rule, col)
	}
	if n.downdiag {
		b.stroke([]typeset.PathOp{moveTo(sx0, sy0), lineTo(sx1, sy1)}, rule, col)
	}
	return b
}

// moveTo and lineTo return the path operators to x, y.
func moveTo(x, y float64) typeset.PathOp {
	return typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}}
}

func lineTo(x, y float64) typeset.PathOp {
	return typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x, Y: y}}}
}

// kappa is the distance of the control points of a cubic Bézier quarter
// circle from its ends, in radii: 4(√2−1)/3.
const kappa = 0.5522847498307936

// ellipseOps returns the closed path of the ellipse of centre cx, cy and
// semi-axes a and b, four cubic Béziers.
func ellipseOps(cx, cy, a, b float64) []typeset.PathOp {
	ka, kb := float64(kappa*a), float64(kappa*b)
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	return []typeset.PathOp{
		moveTo(cx+a, cy),
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx+a, cy+kb), pt(cx+ka, cy+b), pt(cx, cy+b)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx-ka, cy+b), pt(cx-a, cy+kb), pt(cx-a, cy)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx-a, cy-kb), pt(cx-ka, cy-b), pt(cx, cy-b)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx+ka, cy-b), pt(cx+a, cy-kb), pt(cx+a, cy)}},
		{Kind: typeset.ClosePath},
	}
}

// roundedOps returns the closed path of the rectangle x0, y0 to x1, y1
// with corners of radius r.
func roundedOps(x0, y0, x1, y1, r float64) []typeset.PathOp {
	k := float64(kappa * r)
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	curve := func(c1, c2, to typeset.Point) typeset.PathOp {
		return typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{c1, c2, to}}
	}
	return []typeset.PathOp{
		moveTo(x0+r, y0),
		lineTo(x1-r, y0),
		curve(pt(x1-r+k, y0), pt(x1, y0+r-k), pt(x1, y0+r)),
		lineTo(x1, y1-r),
		curve(pt(x1, y1-r+k), pt(x1-r+k, y1), pt(x1-r, y1)),
		lineTo(x0+r, y1),
		curve(pt(x0+r-k, y1), pt(x0, y1-r+k), pt(x0, y1-r)),
		lineTo(x0, y0+r),
		curve(pt(x0, y0+r-k), pt(x0+r-k, y0), pt(x0+r, y0)),
		{Kind: typeset.ClosePath},
	}
}
