package css

import (
	"math"
	"strings"

	"golang.org/x/net/html"
)

// MathStyle is the math-style property (MathML Core, 4.3): whether math
// layout keeps the logical height of formulas low.
type MathStyle uint8

// The math-style values.
const (
	// MathNormal is the initial value: display style.
	MathNormal MathStyle = iota
	// MathCompact minimises the height: text style, scripts smaller.
	MathCompact
)

// MathShift is the math-shift property (MathML Core, 4.4): whether a
// superscript sits at SuperscriptShiftUp or, cramped, at
// SuperscriptShiftUpCramped.
type MathShift uint8

// The math-shift values.
const (
	// MathShiftNormal is the initial value.
	MathShiftNormal MathShift = iota
	// MathShiftCompact cramps superscripts.
	MathShiftCompact
)

// mathDepthSpec is a specified math-depth (MathML Core, 4.5): auto-add,
// add(<integer>) or an <integer>.
type mathDepthSpec struct {
	autoAdd bool
	add     bool
	n       int
}

// mathProps returns the properties of MathML Core, 4: math-style,
// math-shift and math-depth, all inherited.
func mathProps() map[propID]propDef {
	return map[propID]propDef{
		pMathStyle: enumProp("math-style", true, func(s *Style) *MathStyle { return &s.MathStyle }, map[string]MathStyle{
			"normal": MathNormal, "compact": MathCompact,
		}),
		pMathShift: enumProp("math-shift", true, func(s *Style) *MathShift { return &s.MathShift }, map[string]MathShift{
			"normal": MathShiftNormal, "compact": MathShiftCompact,
		}),
		pMathDepth: {
			name: "math-depth", inherited: true, parse: parseMathDepth, compute: computeMathDepth,
			copyFrom: field(func(s *Style) *int32 { return &s.MathDepth }),
		},
	}
}

// parseMathDepth parses math-depth (MathML Core, 4.5).
func parseMathDepth(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent && lower(t.value) == "auto-add" {
		return mathDepthSpec{autoAdd: true}, true
	}
	if t.typ == tFunction && lower(t.value) == "add" {
		args := significant(t.children)
		if len(args) != 1 {
			return nil, false
		}
		n, ok := parseInteger(args[0])
		return mathDepthSpec{add: true, n: n}, ok
	}
	n, ok := parseInteger(t)
	return mathDepthSpec{n: n}, ok
}

// computeMathDepth computes math-depth (MathML Core, 4.5): auto-add adds
// one to the inherited value when the inherited math-style is compact,
// add(n) adds n, an integer replaces it. The result stays within the
// int32 range ([AddIntegers]).
func computeMathDepth(s *styler, st *Style, v any) {
	d, ok := v.(mathDepthSpec)
	if !ok {
		return
	}
	inherited := int(s.parent.MathDepth)
	switch {
	case d.autoAdd:
		st.MathDepth = s.parent.MathDepth
		if s.parent.MathStyle == MathCompact {
			st.MathDepth = int32(AddIntegers(inherited, 1)) //nolint:gosec // AddIntegers keeps the int32 range
		}
	case d.add:
		st.MathDepth = int32(AddIntegers(inherited, d.n)) //nolint:gosec // AddIntegers keeps the int32 range
	default:
		st.MathDepth = int32(ClampInteger(int64(d.n))) //nolint:gosec // ClampInteger keeps the int32 range
	}
}

// mathFontScale returns the factor font-size: math scales the inherited
// font size by when math-depth goes from a, the inherited value, to b,
// the computed one (MathML Core, 4.5): the scale-downs of the inherited
// first available font's MATH table, u, for the first two levels, and
// 0.71 a level past them or without that table.
func mathFontScale(a, b int, u FontUnits) float64 {
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
	if u.Math {
		script, scriptScript := u.ScriptPercentScaleDown, u.ScriptScriptPercentScaleDown
		if script <= 0 {
			script = 0.71
		}
		if scriptScript <= 0 {
			scriptScript = 0.5041
		}
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
	// 0.71 to a power past maxMathDepthSteps is below any size a font is
	// laid out at: the steps stop there.
	for range min(e, maxMathDepthSteps) {
		s *= 0.71
	}
	if invert {
		return 1 / s
	}
	return math.Max(s, 0)
}

// maxMathDepthSteps bounds the levels font-size: math scales by. Not
// spec-mandated: math-depth is an integer of the int32 range, and 0.71 to
// the 64th is 3e-10, a size nothing draws at.
const maxMathDepthSteps = 64

// mathHints returns the presentational hints of the MathML element n
// (MathML Core, 2.1.4 to 2.1.6 and 3.2.2): dir sets direction, mathcolor
// color, mathbackground background-color, mathsize font-size,
// displaystyle math-style, scriptlevel math-depth, and mathvariant="normal"
// on mi sets text-transform to none.
func mathHints(n *html.Node) []Declaration {
	var h hints
	if v, ok := attr(n, "dir"); ok {
		switch lower(strings.TrimSpace(v)) {
		case "ltr", "rtl":
			h.add("direction", lower(strings.TrimSpace(v)))
		}
	}
	if v, ok := attr(n, "mathcolor"); ok {
		h.add("color", v)
	}
	if v, ok := attr(n, "mathbackground"); ok {
		h.add("background-color", v)
	}
	if v, ok := attr(n, "mathsize"); ok {
		h.add("font-size", v)
	}
	if v, ok := attr(n, "displaystyle"); ok {
		switch lower(strings.TrimSpace(v)) {
		case "true":
			h.add("math-style", "normal")
		case "false":
			h.add("math-style", "compact")
		}
	}
	if v, ok := attr(n, "scriptlevel"); ok {
		if d, ok := scriptLevel(strings.TrimSpace(v)); ok {
			h.add("math-depth", d)
		}
	}
	if n.Data == "mi" {
		if v, ok := attr(n, "mathvariant"); ok && lower(strings.TrimSpace(v)) == "normal" {
			h.add("text-transform", "none")
		}
	}
	return h.list
}

// scriptLevel returns the math-depth a scriptlevel attribute v maps to
// (MathML Core, 2.1.6): +U to add(U), -U to add(-U), U to U, U an
// unsigned integer.
func scriptLevel(v string) (string, bool) {
	sign := ""
	switch {
	case strings.HasPrefix(v, "+"):
		v = v[1:]
	case strings.HasPrefix(v, "-"):
		sign, v = "-", v[1:]
	default:
		if v == "" || strings.Trim(v, "0123456789") != "" {
			return "", false
		}
		return v, true
	}
	if v == "" || strings.Trim(v, "0123456789") != "" {
		return "", false
	}
	return "add(" + sign + v + ")", true
}
