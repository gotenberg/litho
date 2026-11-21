package css

// GlyphPaint holds how the glyphs of an element's text are painted:
// -webkit-text-stroke-width, -webkit-text-stroke-color and
// -webkit-text-fill-color (Compatibility Standard, 3.4.6) and paint-order (SVG
// 2, 13.8, which HTML text follows as CSS Text Decoration 4 states it).
// All four are inherited. Read it with [Style.TextStroke] and
// [Style.TextFill]: a colour that is currentcolor computes to the keyword
// (CSS Color 4, 4.4) and resolves against the color of the element that
// paints the text.
type GlyphPaint struct {
	// StrokeWidth is the width of the line drawn along the glyph outlines,
	// centred on them, in points; 0 draws none.
	StrokeWidth float64
	// StrokeFirst paints the stroke before the fill: a paint-order that
	// names stroke before fill, or stroke alone.
	StrokeFirst bool
	stroke      paintColor
	fill        paintColor
}

// paintColor is a computed colour of [GlyphPaint]: a colour, or a value
// that depends on currentcolor, kept as the keyword.
type paintColor struct {
	c Color
	// v, when set, is a value that resolves against the element's color.
	v *colorValue
	// set marks a value other than the initial currentcolor.
	set bool
}

// resolve returns the colour against st's color.
func (p paintColor) resolve(st *Style) Color {
	switch {
	case !p.set:
		return st.Color
	case p.v != nil:
		return p.v.resolve(st.Color, st.DarkScheme)
	}
	return p.c
}

// NewGlyphPaint returns the glyph paint of text stroked width points wide
// in stroke and filled with fill, the stroke painted first when first is
// set: the computed values of a style that states each colour.
func NewGlyphPaint(width float64, stroke, fill Color, first bool) *GlyphPaint {
	return &GlyphPaint{
		StrokeWidth: width, StrokeFirst: first,
		stroke: paintColor{c: stroke, set: true}, fill: paintColor{c: fill, set: true},
	}
}

// initial reports a GlyphPaint every longhand of which is initial.
func (t *GlyphPaint) initial() bool {
	return t.StrokeWidth == 0 && !t.StrokeFirst && !t.stroke.set && !t.fill.set
}

// TextStroke returns the width in points and the colour of the stroke the
// element's glyphs take, and whether it is painted before the fill; a
// width of 0 for none.
func (s *Style) TextStroke() (width float64, c Color, first bool) {
	if s.GlyphPaint == nil {
		return 0, s.Color, false
	}
	return s.GlyphPaint.StrokeWidth, s.GlyphPaint.stroke.resolve(s), s.GlyphPaint.StrokeFirst
}

// TextFill returns the colour that fills the element's glyphs: its color
// unless -webkit-text-fill-color says otherwise.
func (s *Style) TextFill() Color {
	if s.GlyphPaint == nil {
		return s.Color
	}
	return s.GlyphPaint.fill.resolve(s)
}

// glyphPaintProps returns -webkit-text-stroke-width, -webkit-text-stroke-color,
// -webkit-text-fill-color and paint-order.
func glyphPaintProps() map[propID]propDef {
	colour := func(name string, f func(*GlyphPaint) *paintColor) propDef {
		return glyphPaintLonghand(name, func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			return parseColor(t)
		}, func(_ *styler, st *Style, tp *GlyphPaint, v any) {
			c, ok := v.(colorValue)
			if !ok {
				return
			}
			switch {
			case c.current && c.expr == nil:
				*f(tp) = paintColor{}
			case c.dependsOnCurrent():
				*f(tp) = paintColor{v: &c, set: true}
			default:
				*f(tp) = paintColor{c: c.resolve(st.Color, st.DarkScheme), set: true}
			}
		}, func(dst, src *GlyphPaint) { *f(dst) = *f(src) })
	}
	return map[propID]propDef{
		// -webkit-text-stroke-width: <line-width>, computed to an absolute
		// length (Compatibility Standard, 3.4.6.3).
		pTextStrokeWidth: glyphPaintLonghand("-webkit-text-stroke-width", parseBorderWidth, func(s *styler, _ *Style, tp *GlyphPaint, v any) {
			if l, ok := v.(length); ok {
				tp.StrokeWidth = max(clampLen(l.points(s.lc)), 0)
			}
		}, func(dst, src *GlyphPaint) { dst.StrokeWidth = src.StrokeWidth }),
		pTextStrokeColor: colour("-webkit-text-stroke-color", func(t *GlyphPaint) *paintColor { return &t.stroke }),
		pTextFillColor:   colour("-webkit-text-fill-color", func(t *GlyphPaint) *paintColor { return &t.fill }),
		pPaintOrder: glyphPaintLonghand("paint-order", parsePaintOrder, func(_ *styler, _ *Style, tp *GlyphPaint, v any) {
			if first, ok := v.(bool); ok {
				tp.StrokeFirst = first
			}
		}, func(dst, src *GlyphPaint) { dst.StrokeFirst = src.StrokeFirst }),
	}
}

// glyphPaintLonghand defines an inherited longhand held in
// [Style.GlyphPaint]: set stores a parsed value into a copy, cp copies the
// field.
func glyphPaintLonghand(name string, parse parseFn, set func(*styler, *Style, *GlyphPaint, any), cp func(dst, src *GlyphPaint)) propDef {
	return propDef{
		name: name, inherited: true,
		parse: parse,
		compute: func(s *styler, st *Style, v any) {
			setGlyphPaint(st, func(tp *GlyphPaint) { set(s, st, tp, v) })
		},
		copyFrom: func(dst, src *Style) {
			if dst.GlyphPaint == src.GlyphPaint {
				// The common case, both initial or both shared, allocates
				// nothing.
				return
			}
			from := src.GlyphPaint
			if from == nil {
				from = &initialGlyphPaint
			}
			setGlyphPaint(dst, func(tp *GlyphPaint) { cp(tp, from) })
		},
	}
}

var initialGlyphPaint GlyphPaint

// setGlyphPaint applies f to a copy of st's text paint, storing nil when
// every longhand ends initial. The shared value is never mutated.
func setGlyphPaint(st *Style, f func(*GlyphPaint)) {
	n := GlyphPaint{}
	if st.GlyphPaint != nil {
		n = *st.GlyphPaint
	}
	f(&n)
	switch {
	case n.initial():
		st.GlyphPaint = nil
	case st.GlyphPaint != nil && *st.GlyphPaint == n:
	default:
		st.GlyphPaint = &n
	}
}

// expandTextStroke expands -webkit-text-stroke: <line-width> || <color>
// (Compatibility Standard, 3.4.6.4), either left out and reset to its
// initial value.
func expandTextStroke(v []token, c *declCtx) ([]entry, bool) {
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	out := []entry{{pTextStrokeWidth, wideInitial}, {pTextStrokeColor, wideInitial}}
	var haveW, haveC bool
	for _, t := range v {
		one := []token{t}
		if w, ok := parseBorderWidth(one, c); ok && !haveW {
			out[0].v, haveW = w, true
			continue
		}
		if col, ok := parseColor(t); ok && !haveC {
			out[1].v, haveC = col, true
			continue
		}
		return nil, false
	}
	return out, true
}

// parsePaintOrder parses paint-order: normal, or fill, stroke and markers
// each at most once in any order, the ones left out following in that
// order (SVG 2, 13.8). It returns whether the stroke is painted before
// the fill: markers paint nothing on text.
func parsePaintOrder(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return false, true
	}
	if len(v) == 0 || len(v) > 3 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		k := lower(t.value)
		if k != "fill" && k != "stroke" && k != "markers" || seen[k] {
			return nil, false
		}
		seen[k] = true
	}
	for _, t := range v {
		switch lower(t.value) {
		case "stroke":
			return true, true
		case "fill":
			return false, true
		}
	}
	// markers alone: fill, then stroke, follow it.
	return false, true
}

// DecorationColor returns the colour the element's decoration lines are
// painted in: its text-decoration-color, where the currentcolor keyword
// takes the stroke colour of text stroked in a colour of its own that is
// not transparent, else the fill colour of its text. CSS Text Decoration
// 3, 2.3 resolves currentcolor to color; Chromium paints the lines so on
// screen and in print alike, and litho follows it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 underlines bold
//     green text in its red stroke colour, in its orange
//     -webkit-text-fill-color without a stroke, in red with both, and in
//     green under a stroke of currentcolor; a text-decoration-color of
//     blue stays blue under a stroke; screenshot and PDF agree.
func (s *Style) DecorationColor() Color {
	if s.currentColor&slotTextDecoration == 0 || s.slotValue(slotTextDecoration).expr != nil {
		return s.TextDecorationColor
	}
	if g := s.GlyphPaint; g != nil && g.StrokeWidth > 0 && g.stroke.set {
		if c := g.stroke.resolve(s); c.A > 0 {
			return c
		}
	}
	return s.TextFill()
}
