package formula

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// pxPt is one CSS pixel in points (CSS Values 4, 6.2).
const pxPt = 0.75

// childStyle returns the style of e, the child at index i of p styled
// pst: e's own when the converter gave one, else the one the user agent
// stylesheet of MathML Core (Appendix A) and e's attributes, as
// presentational hints, derive from pst.
func (l *layouter) childStyle(e *Element, p *Element, i int, pst *Style) *Style {
	if e.Style != nil {
		return e.Style
	}
	return l.derive(e, p, i, pst)
}

// derive applies the rules of MathML Core, Appendix A that hold for e, the
// child at index i of p, to the style it inherits from pst, then e's
// presentational hints (2.1.5, 2.1.6, 3.2.2), which outrank the user agent
// rules as author rules do (CSS Cascading 4, 6.2).
func (l *layouter) derive(e *Element, p *Element, i int, pst *Style) *Style {
	st := &Style{
		Families: pst.Families, Size: pst.Size, Weight: pst.Weight, Italic: pst.Italic,
		Lang: pst.Lang, Fallback: pst.Fallback, Color: pst.Color,
		Display: pst.Display, Cramped: pst.Cramped, Depth: pst.Depth,
		MathAuto: pst.MathAuto, Hidden: pst.Hidden,
	}
	nth := i + 1
	inheritSize := false
	switch p.Name {
	case "mfrac":
		// mfrac > * { math-depth: auto-add; math-style: compact }: auto-add
		// adds one when the inherited math-style is compact (4.5).
		if !pst.Display {
			st.Depth++
		}
		st.Display = false
		if nth == 2 {
			st.Cramped = true
		}
	case "mroot":
		if nth > 1 {
			st.Depth += 2
			st.Display = false
		}
	case "msub", "msup", "msubsup", "mmultiscripts", "munder", "mover", "munderover":
		if nth > 1 {
			st.Depth++
			st.Display = false
		}
		switch {
		case p.Name == "munder" && nth == 2 && p.attrIs("accentunder", "true"),
			p.Name == "mover" && nth == 2 && p.attrIs("accent", "true"),
			p.Name == "munderover" && nth == 2 && p.attrIs("accentunder", "true"),
			p.Name == "munderover" && nth == 3 && p.attrIs("accent", "true"):
			inheritSize = true
		}
		switch {
		case (p.Name == "msub" || p.Name == "msubsup") && nth == 2,
			(p.Name == "mover" || p.Name == "munderover") && nth == 1 && p.attrIs("accent", "true"):
			st.Cramped = true
		case p.Name == "mmultiscripts":
			st.Cramped = multiscriptCramped(p, i, pst.Cramped)
		}
	}
	switch e.Name {
	case "mtable":
		st.Display = false
	case "mroot", "msqrt":
		st.Cramped = true
	case "mi":
		st.MathAuto = !e.attrIs("mathvariant", "normal")
	case "mphantom":
		st.Hidden = true
	case "merror":
		red := typeset.Color{R: 1, A: 1}
		st.Border = [4]float64{pxPt, pxPt, pxPt, pxPt}
		st.BorderColor = [4]typeset.Color{red, red, red, red}
		// lightYellow, #FFFFE0 (CSS Color 4, 6.1).
		st.Background = typeset.Color{R: 1, G: 1, B: float64(0xE0) / 0xFF, A: 1}
	case "mfrac":
		st.Padding[left], st.Padding[right] = pxPt, pxPt
	}
	// Presentational hints.
	if v, ok := e.Attr("displaystyle"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			st.Display = true
		case "false":
			st.Display = false
		}
	}
	if v, ok := e.Attr("scriptlevel"); ok {
		if n, rel, ok := parseScriptLevel(v); ok {
			if rel {
				st.Depth = pst.Depth + n
			} else {
				st.Depth = n
			}
		}
	}
	if c, ok := attrColor(e, "mathcolor"); ok {
		st.Color = c
	}
	if c, ok := attrColor(e, "mathbackground"); ok {
		st.Background = c
	}
	// font-size: math, unless mathsize or an accent's font-size: inherit
	// sets it (4.5).
	switch v, ok := e.Attr("mathsize"); {
	case ok && l.sizeAttr(v, pst) > 0:
		st.Size = l.sizeAttr(v, pst)
	case !inheritSize:
		st.Size = float64(pst.Size * l.depthScale(pst, pst.Depth, st.Depth))
	}
	if e.Name == "mtd" {
		// mtd { padding: 0.5ex 0.4em }, in the cell's own font.
		ex := st.Size / 2
		if f := l.faceOf(st); f != nil {
			ex = f.units(f.xHeight, st.Size)
		}
		st.Padding = [4]float64{ex / 2, float64(0.4 * st.Size), ex / 2, float64(0.4 * st.Size)}
	}
	return st
}

// multiscriptCramped returns the math-shift of the child at index i of
// mmultiscripts p (MathML Core, 3.4.4): compact at an even position before
// mprescripts and at an odd one after it, inherited at an even one after
// it.
func multiscriptCramped(p *Element, i int, inherited bool) bool {
	nth := i + 1
	after := false
	for _, c := range p.Children[:i] {
		if c.Name == "mprescripts" {
			after = true
			break
		}
	}
	switch {
	case after && nth%2 == 0:
		return inherited
	case after:
		return true
	case nth%2 == 0:
		return true
	}
	return inherited
}

// depthScale returns the factor font-size: math scales the inherited font
// size by from math-depth a to b (MathML Core, 4.5), the scale-downs read
// from the inherited first available font, that of pst.
func (l *layouter) depthScale(pst *Style, a, b int) float64 {
	if a == b {
		return 1
	}
	invert := false
	if b < a {
		a, b = b, a
		invert = true
	}
	e := b - a
	s := 1.0
	f := l.faceOf(pst)
	if f != nil && f.math != nil {
		script := f.constant(mt.ScriptPercentScaleDown, pst.Size)
		scriptScript := f.constant(mt.ScriptScriptPercentScaleDown, pst.Size)
		switch {
		case a <= 0 && b >= 2:
			s *= scriptScript
			e -= 2
		case a == 1:
			s *= scriptScript / script
			e--
		case b == 1:
			s *= script
			e--
		}
	}
	s *= detmath.Pow(0.71, float64(min(e, maxDepthSteps)))
	if invert {
		return 1 / s
	}
	return s
}

// maxDepthSteps bounds the math-depth steps one font-size: math scales
// by. Not spec-mandated: 0.71 to that power is below the smallest font
// size anything prints at, and the exponent stays a small integer.
const maxDepthSteps = 64

// parseScriptLevel reads a scriptlevel attribute: +U, -U or U, U an
// unsigned integer (MathML Core, 2.1.6). rel reports +U and -U, which add
// to the inherited math-depth.
func parseScriptLevel(v string) (n int, rel, ok bool) {
	v = strings.TrimSpace(v)
	sign := 1
	switch {
	case strings.HasPrefix(v, "+"):
		rel, v = true, v[1:]
	case strings.HasPrefix(v, "-"):
		rel, sign, v = true, -1, v[1:]
	}
	if v == "" || v[0] == '+' || v[0] == '-' {
		return 0, false, false
	}
	u, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, false, false
	}
	return sign * int(min(u, maxDepthSteps)), rel, true // fits int: at most maxDepthSteps
}
