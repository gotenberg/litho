package css

import (
	"math"
	"slices"
)

// maxShadows caps the shadows of one box-shadow value: 32. Real designs
// stack two or three; each shadow is a path or a blurred raster the layout
// engine paints. Anti-DoS guard, not spec-mandated.
const maxShadows = 32

// snapBorderWidth snaps a width in points as a border width (CSS Values 4,
// 6.1.1, "snap as a border width"), one CSS pixel standing for a device
// pixel as browsers do at their default zoom: a width above 0 and under 1px
// becomes 1px, a wider one rounds down to a whole number of pixels.
func snapBorderWidth(pt float64) float64 {
	if !(pt > 0) {
		return 0
	}
	px := pt / Pt
	if px < 1 {
		return Pt
	}
	// The epsilon keeps a width that is a whole pixel up to the float
	// error of a unit conversion (0.75pt is 1px exactly, 2em may not be).
	return math.Floor(px+1e-6) * Pt
}

// computeLineWidth computes a border or outline width: the specified
// length snapped as a border width, kept whatever the style (CSS
// Backgrounds 3, 4.3 as Blink, Gecko and WebKit implement it).
func computeLineWidth(s *styler, v any) (float64, bool) {
	l, ok := v.(length)
	if !ok {
		return 0, false
	}
	return snapBorderWidth(l.points(s.lc)), true
}

// outlineStyles are the outline-style values (CSS UI 3, 5.3): the border
// styles but hidden, and auto, which draws as solid.
var outlineStyles = map[string]BorderStyle{
	"none": BorderNone, "auto": Solid, "dotted": Dotted, "dashed": Dashed,
	"solid": Solid, "double": Double, "groove": Groove, "ridge": Ridge,
	"inset": Inset, "outset": Outset,
}

// outlineProps defines outline-style, outline-width, outline-color and
// outline-offset (CSS UI 3, 5).
func outlineProps() (style, width, colour, offset propDef) {
	style = enumProp("outline-style", false, func(s *Style) *BorderStyle { return &s.Outline.Style }, outlineStyles)
	width = propDef{
		name:  "outline-width",
		parse: parseBorderWidth,
		compute: func(s *styler, st *Style, v any) {
			if w, ok := computeLineWidth(s, v); ok {
				st.Outline.Width = w
			}
		},
		copyFrom: field(func(s *Style) *float64 { return &s.Outline.Width }),
		initial:  length{pt: 3 * Pt, hasLen: true},
	}
	// CSS UI 3, 5.4: the initial colour is currentColor where invert is
	// not supported.
	colour = colorProp("outline-color", func(s *Style) *Color { return &s.Outline.Color }, slotOutline, colorValue{current: true})
	offset = propDef{
		name: "outline-offset",
		parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			l, ok := parseLength(t, 0)
			return l, ok
		},
		compute: func(s *styler, st *Style, v any) {
			if l, ok := v.(length); ok {
				st.OutlineOffset = l.points(s.lc)
			}
		},
		copyFrom: field(func(s *Style) *float64 { return &s.OutlineOffset }),
	}
	return style, width, colour, offset
}

// expandOutline expands the outline shorthand: <outline-color> ||
// <outline-style> || <outline-width> (CSS UI 3, 5.1).
func expandOutline(v []token, c *declCtx) ([]entry, bool) {
	if len(v) < 1 || len(v) > 3 {
		return nil, false
	}
	width, style, colour := any(wideInitial), any(wideInitial), any(wideInitial)
	var w, s, col bool
	for _, t := range v {
		one := []token{t}
		if !s {
			if pv, ok := props[pOutlineStyle].parse(one, c); ok {
				style, s = pv, true
				continue
			}
		}
		if !w {
			if pv, ok := parseBorderWidth(one, c); ok {
				width, w = pv, true
				continue
			}
		}
		if !col {
			if pv, ok := parseColor(t); ok {
				colour, col = pv, true
				continue
			}
		}
		return nil, false
	}
	return []entry{{pOutlineStyle, style}, {pOutlineWidth, width}, {pOutlineColor, colour}}, true
}

// shadowSpec is one specified shadow.
type shadowSpec struct {
	lens  [4]length
	n     int
	color colorValue
	inset bool
}

// parseBoxShadow parses box-shadow (CSS Backgrounds 3, 7.1): none, or a
// comma-separated list of <shadow>, each two to four lengths, a colour
// and the inset keyword in any order around the lengths.
func parseBoxShadow(v []token, c *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return []shadowSpec(nil), true
	}
	layers := splitCommas(v)
	if len(layers) > maxShadows {
		c.warn("unsupported-css-value", "box-shadow: only the first %d shadows are drawn", maxShadows)
		layers = layers[:maxShadows]
	}
	out := make([]shadowSpec, 0, len(layers))
	for _, l := range layers {
		s, ok := parseShadow(l)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// parseShadow parses one <shadow>.
func parseShadow(v []token) (shadowSpec, bool) {
	s := shadowSpec{color: colorValue{current: true}}
	var hasColor, lensDone bool
	for i := 0; i < len(v); i++ {
		t := v[i]
		if t.isIdent("inset") && !s.inset {
			s.inset = true
			if s.n > 0 {
				lensDone = true
			}
			continue
		}
		if l, ok := parseLength(t, 0); ok && !l.hasPct && !lensDone {
			if s.n == 4 {
				return s, false
			}
			if s.n == 2 {
				// The blur radius is not negative.
				if l, ok = parseLength(t, nonNeg); !ok {
					return s, false
				}
			}
			s.lens[s.n] = l
			s.n++
			continue
		}
		if c, ok := parseColor(t); ok && !hasColor {
			s.color, hasColor = c, true
			if s.n > 0 {
				lensDone = true
			}
			continue
		}
		return s, false
	}
	return s, s.n >= 2
}

// computeShadows computes the specified shadows: lengths in points, a
// negative blur clamped to 0, currentColor resolved.
func computeShadows(s *styler, st *Style, v any) {
	specs, ok := v.([]shadowSpec)
	if !ok {
		return
	}
	if len(specs) == 0 {
		st.BoxShadow = nil
		return
	}
	out := make([]Shadow, len(specs))
	for i, sp := range specs {
		var d [4]float64
		for k := range sp.n {
			d[k] = sp.lens[k].points(s.lc)
		}
		sh := Shadow{X: d[0], Y: d[1], Blur: math.Max(d[2], 0), Spread: d[3], Color: sp.color.c, Inset: sp.inset}
		if sp.color.current {
			sh.Color, sh.current = st.Color, true
		}
		out[i] = sh
	}
	st.BoxShadow = out
}

// copyShadows copies box-shadow, resolving again a currentColor shadow
// against dst's color.
func copyShadows(dst, src *Style) {
	dst.BoxShadow = src.BoxShadow
	copied := false
	for i, sh := range src.BoxShadow {
		if !sh.current || sh.Color == dst.Color {
			continue
		}
		if !copied {
			dst.BoxShadow = slices.Clone(src.BoxShadow)
			copied = true
		}
		dst.BoxShadow[i].Color = dst.Color
	}
}

// objectFits are the object-fit values (CSS Images 3, 5.5).
var objectFits = map[string]ObjectFit{
	"fill": FitFill, "contain": FitContain, "cover": FitCover, "none": FitNone, "scale-down": FitScaleDown,
}

// parseObjectPosition parses object-position, a <position> (CSS Images 3,
// 5.6).
func parseObjectPosition(v []token, _ *declCtx) (any, bool) {
	p, ok := parsePosition(v)
	return p, ok
}

// containKeywords maps the contain keywords to their bits (CSS
// Containment 2, 2).
var containKeywords = map[string]Containment{
	"size": ContainSize, "inline-size": ContainInlineSize, "layout": ContainLayout,
	"style": ContainStyle, "paint": ContainPaint,
	"strict":  ContainSize | ContainLayout | ContainStyle | ContainPaint,
	"content": ContainLayout | ContainStyle | ContainPaint,
}

// parseContain parses contain: none, strict, content, or a set of size or
// inline-size, layout, style and paint.
func parseContain(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		if k == "none" {
			return Containment(0), true
		}
		b, ok := containKeywords[k]
		return b, ok
	}
	var out Containment
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		k := lower(t.value)
		b, ok := containKeywords[k]
		if !ok || k == "strict" || k == "content" || out&b != 0 {
			return nil, false
		}
		out |= b
	}
	if out&ContainSize != 0 && out&ContainInlineSize != 0 {
		return nil, false
	}
	return out, len(v) > 0
}
