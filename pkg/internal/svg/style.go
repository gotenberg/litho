package svg

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// paintKind says what a fill or stroke paints with (SVG 1.1, 11.2).
type paintKind uint8

const (
	paintNone paintKind = iota
	paintColor
	paintCurrent
	paintURL
)

// paint is a computed fill or stroke.
type paint struct {
	kind  paintKind
	color compose.Color
	// url is the fragment a paint server reference names, without "#".
	url string
	// fallback is what a url paint falls back to when the reference does
	// not resolve: none, a colour or currentColor (SVG 1.1, 11.2). Its
	// kind is paintNone when none is given, which then paints nothing.
	fallback *paint
}

// anchor is the text-anchor property (SVG 1.1, 10.9.1).
type anchor uint8

const (
	anchorStart anchor = iota
	anchorMiddle
	anchorEnd
)

// style is the computed style of an element: the properties of SVG 1.1,
// Appendix N the renderer draws with.
type style struct {
	fill          paint
	fillOpacity   float64
	fillRule      compose.FillRule
	stroke        paint
	strokeWidth   length
	strokeOpacity float64
	lineCap       compose.LineCap
	lineJoin      compose.LineJoin
	miterLimit    float64
	dashArray     []length
	dashOffset    length

	opacity    float64
	visible    bool
	displayed  bool
	color      compose.Color
	clipPath   string
	clipRule   compose.FillRule
	stopColor  paint
	stopAlpha  float64
	overflowOK bool

	families []string
	fontSize float64
	weight   int
	italic   bool
	anchor   anchor
	lang     string

	// background is the CSS background-color of the root element, which
	// paints its viewport (CSS Backgrounds 3, 3.2); it is not inherited.
	background compose.Color

	// mask, filter and marker record the properties the renderer does not
	// draw, for a warning.
	mask, filter, marker bool
}

// initialStyle returns the initial values (SVG 1.1, Appendix N), with
// color and the font properties from the embedding context.
func initialStyle(ctx Context) *style {
	st := &style{
		fill:          paint{kind: paintColor, color: compose.Black},
		fillOpacity:   1,
		stroke:        paint{kind: paintNone},
		strokeWidth:   length{v: 1},
		strokeOpacity: 1,
		miterLimit:    4,
		opacity:       1,
		visible:       true,
		displayed:     true,
		color:         compose.Black,
		stopColor:     paint{kind: paintColor, color: compose.Black},
		stopAlpha:     1,
		families:      ctx.Families,
		lang:          ctx.Lang,
		fontSize:      16,
		weight:        400,
	}
	if ctx.Color != (compose.Color{}) {
		st.color = ctx.Color
	}
	if ctx.FontSize > 0 {
		st.fontSize = ctx.FontSize
	}
	if ctx.Weight > 0 {
		st.weight = ctx.Weight
	}
	st.italic = ctx.Italic
	return st
}

// inherit returns the style an element starts from: its parent's
// inherited properties, the initial values of the others (SVG 1.1,
// Appendix N lists which inherit).
func (p *style) inherit() *style {
	st := *p
	st.opacity = 1
	st.displayed = true
	st.clipPath = ""
	st.stopColor = paint{kind: paintColor, color: compose.Black}
	st.stopAlpha = 1
	st.overflowOK = false
	st.mask, st.filter = false, false
	st.background = compose.Color{}
	return &st
}

// apply sets the property of d on st, parent being the parent's computed
// style for inherit. An invalid value leaves st as it was (CSS Syntax 3,
// 9), and a property the renderer does not know is ignored. It reports
// whether the property was known and valid.
func (st *style) apply(d declaration, parent *style) bool {
	v := d.value
	if strings.EqualFold(v, "inherit") {
		st.copyFrom(d.prop, parent)
		return true
	}
	switch d.prop {
	case "fill":
		p, ok := parsePaint(v)
		if ok {
			st.fill = p
		}
		return ok
	case "stroke":
		p, ok := parsePaint(v)
		if ok {
			st.stroke = p
		}
		return ok
	case "fill-opacity", "stroke-opacity", "opacity", "stop-opacity":
		a, ok := parseAlpha(v)
		if !ok {
			return false
		}
		switch d.prop {
		case "fill-opacity":
			st.fillOpacity = a
		case "stroke-opacity":
			st.strokeOpacity = a
		case "opacity":
			st.opacity = a
		default:
			st.stopAlpha = a
		}
		return true
	case "fill-rule", "clip-rule":
		var r compose.FillRule
		switch strings.ToLower(v) {
		case "nonzero":
			r = compose.NonZero
		case "evenodd":
			r = compose.EvenOdd
		default:
			return false
		}
		if d.prop == "fill-rule" {
			st.fillRule = r
		} else {
			st.clipRule = r
		}
		return true
	case "stroke-width":
		l, ok := parseLength(v)
		if !ok || l.v < 0 {
			return false
		}
		st.strokeWidth = l
		return true
	case "stroke-linecap":
		c, ok := map[string]compose.LineCap{"butt": compose.ButtCap, "round": compose.RoundCap, "square": compose.SquareCap}[strings.ToLower(v)]
		if ok {
			st.lineCap = c
		}
		return ok
	case "stroke-linejoin":
		// SVG 2's miter-clip and arcs fall back to miter (SVG 2, 13.5.4).
		j, ok := map[string]compose.LineJoin{"miter": compose.MiterJoin, "miter-clip": compose.MiterJoin, "arcs": compose.MiterJoin, "round": compose.RoundJoin, "bevel": compose.BevelJoin}[strings.ToLower(v)]
		if ok {
			st.lineJoin = j
		}
		return ok
	case "stroke-miterlimit":
		m, ok := parseNumber(v)
		if !ok || m < 1 {
			return false
		}
		st.miterLimit = m
		return true
	case "stroke-dasharray":
		if strings.EqualFold(v, "none") {
			st.dashArray = nil
			return true
		}
		l, ok := parseLengthList(v, maxDashes)
		if !ok || len(l) == 0 {
			return false
		}
		for _, x := range l {
			if x.v < 0 {
				return false
			}
		}
		st.dashArray = l
		return true
	case "stroke-dashoffset":
		l, ok := parseLength(v)
		if ok {
			st.dashOffset = l
		}
		return ok
	case "visibility":
		switch strings.ToLower(v) {
		case "visible":
			st.visible = true
		case "hidden", "collapse":
			st.visible = false
		default:
			return false
		}
		return true
	case "display":
		st.displayed = !strings.EqualFold(v, "none")
		return true
	case "color":
		c, current, ok := css.ParseColor(v)
		if !ok {
			return false
		}
		if !current {
			st.color = composeColor(c)
		} else if parent != nil {
			st.color = parent.color
		}
		return true
	case "background-color":
		c, current, ok := css.ParseColor(v)
		if !ok {
			return false
		}
		st.background = composeColor(c)
		if current {
			st.background = st.color
		}
		return true
	case "stop-color":
		p, ok := parsePaint(v)
		if !ok || p.kind == paintURL || p.kind == paintNone {
			return false
		}
		st.stopColor = p
		return true
	case "clip-path":
		if strings.EqualFold(v, "none") {
			st.clipPath = ""
			return true
		}
		id, ok := parseURLRef(v)
		if ok {
			st.clipPath = id
		}
		return ok
	case "mask":
		st.mask = !strings.EqualFold(v, "none")
		return true
	case "filter":
		st.filter = !strings.EqualFold(v, "none")
		return true
	case "marker", "marker-start", "marker-mid", "marker-end":
		if !strings.EqualFold(v, "none") {
			st.marker = true
		}
		return true
	case "overflow":
		switch strings.ToLower(v) {
		case "visible", "auto":
			st.overflowOK = true
		case "hidden", "scroll", "clip":
			st.overflowOK = false
		default:
			return false
		}
		return true
	case "font-family":
		f := parseFamilies(v)
		if len(f) == 0 {
			return false
		}
		st.families = f
		return true
	case "font-size":
		sz, ok := parseFontSize(v, parent)
		if ok {
			st.fontSize = sz
		}
		return ok
	case "font-weight":
		w, ok := parseWeight(v, parent)
		if ok {
			st.weight = w
		}
		return ok
	case "font-style":
		switch strings.ToLower(v) {
		case "normal":
			st.italic = false
		case "italic", "oblique":
			st.italic = true
		default:
			return false
		}
		return true
	case "text-anchor":
		a, ok := map[string]anchor{"start": anchorStart, "middle": anchorMiddle, "end": anchorEnd}[strings.ToLower(v)]
		if ok {
			st.anchor = a
		}
		return ok
	}
	return false
}

// copyFrom sets the property prop of st to parent's value, for inherit.
func (st *style) copyFrom(prop string, parent *style) {
	if parent == nil {
		return
	}
	switch prop {
	case "fill":
		st.fill = parent.fill
	case "stroke":
		st.stroke = parent.stroke
	case "fill-opacity":
		st.fillOpacity = parent.fillOpacity
	case "stroke-opacity":
		st.strokeOpacity = parent.strokeOpacity
	case "opacity":
		st.opacity = parent.opacity
	case "stop-opacity":
		st.stopAlpha = parent.stopAlpha
	case "stop-color":
		st.stopColor = parent.stopColor
	case "fill-rule":
		st.fillRule = parent.fillRule
	case "clip-rule":
		st.clipRule = parent.clipRule
	case "stroke-width":
		st.strokeWidth = parent.strokeWidth
	case "stroke-linecap":
		st.lineCap = parent.lineCap
	case "stroke-linejoin":
		st.lineJoin = parent.lineJoin
	case "stroke-miterlimit":
		st.miterLimit = parent.miterLimit
	case "stroke-dasharray":
		st.dashArray = parent.dashArray
	case "stroke-dashoffset":
		st.dashOffset = parent.dashOffset
	case "visibility":
		st.visible = parent.visible
	case "color":
		st.color = parent.color
	case "clip-path":
		st.clipPath = parent.clipPath
	case "font-family":
		st.families = parent.families
	case "font-size":
		st.fontSize = parent.fontSize
	case "font-weight":
		st.weight = parent.weight
	case "font-style":
		st.italic = parent.italic
	case "text-anchor":
		st.anchor = parent.anchor
	case "overflow":
		st.overflowOK = parent.overflowOK
	}
}

// presentationAttrs are the presentation attributes (SVG 1.1, 6.4) the
// renderer reads, which map to the property of the same name.
var presentationAttrs = map[string]bool{
	"fill": true, "fill-opacity": true, "fill-rule": true, "stroke": true, "stroke-width": true,
	"stroke-opacity": true, "stroke-linecap": true, "stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true, "opacity": true, "visibility": true,
	"display": true, "color": true, "clip-path": true, "clip-rule": true, "stop-color": true,
	"stop-opacity": true, "mask": true, "filter": true, "marker-start": true, "marker-mid": true,
	"marker-end": true, "overflow": true, "font-family": true, "font-size": true,
	"font-weight": true, "font-style": true, "text-anchor": true,
}

// composeColor converts a CSS colour.
func composeColor(c css.Color) compose.Color {
	return compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// parsePaint parses a <paint> (SVG 2, 13.2): none, currentColor, a
// colour, or a url() reference with an optional fallback. SVG 1.1's ICC
// colour makes the value invalid, as SVG 2 and browsers have it.
func parsePaint(v string) (paint, bool) {
	v = strings.TrimSpace(v)
	switch {
	case strings.EqualFold(v, "none"):
		return paint{kind: paintNone}, true
	case len(v) >= 4 && strings.EqualFold(v[:4], "url("):
		end := strings.IndexByte(v, ')')
		if end < 0 {
			return paint{}, false
		}
		id, ok := parseURLRef(v[:end+1])
		if !ok {
			return paint{}, false
		}
		p := paint{kind: paintURL, url: id}
		if rest := strings.TrimSpace(v[end+1:]); rest != "" {
			fb, ok := parsePaint(rest)
			if !ok || fb.kind == paintURL {
				return paint{}, false
			}
			p.fallback = &fb
		}
		return p, true
	}
	c, current, ok := css.ParseColor(v)
	if !ok {
		return paint{}, false
	}
	if current {
		return paint{kind: paintCurrent}, true
	}
	return paint{kind: paintColor, color: composeColor(c)}, true
}

// parseURLRef parses url(#id), quoted or not, into the id. Only a
// same-document reference resolves: litho reads no external resource
// from an SVG paint or clip.
func parseURLRef(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) < 5 || !strings.EqualFold(v[:4], "url(") || v[len(v)-1] != ')' {
		return "", false
	}
	inner := strings.TrimSpace(v[4 : len(v)-1])
	if len(inner) >= 2 && (inner[0] == '"' || inner[0] == '\'') && inner[len(inner)-1] == inner[0] {
		inner = inner[1 : len(inner)-1]
	}
	id, ok := strings.CutPrefix(strings.TrimSpace(inner), "#")
	return id, ok && id != ""
}

// parseAlpha parses an <alpha-value>: a number or a percentage, clamped
// to [0, 1] (SVG 1.1, 11.3 and CSS Color 4, 4.2).
func parseAlpha(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	pct := strings.HasSuffix(v, "%")
	n, ok := parseNumber(strings.TrimSuffix(v, "%"))
	if !ok {
		return 0, false
	}
	if pct {
		n /= 100
	}
	return min(max(n, 0), 1), true
}

// parseFamilies parses a font-family list (CSS Fonts 4, 3.1): names
// separated by commas, quoted or as identifiers.
func parseFamilies(v string) []string {
	var out []string
	for f := range strings.SplitSeq(v, ",") {
		f = strings.TrimSpace(f)
		if len(f) >= 2 && (f[0] == '"' || f[0] == '\'') && f[len(f)-1] == f[0] {
			f = f[1 : len(f)-1]
		}
		f = strings.Join(strings.Fields(f), " ")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// absoluteSizes maps the absolute-size keywords to pixels (CSS Fonts 4,
// 2.5, with the medium of 16 pixels).
var absoluteSizes = map[string]float64{
	"xx-small": 9, "x-small": 10, "small": 13, "medium": 16, "large": 18, "x-large": 24,
	"xx-large": 32, "xxx-large": 48,
}

// parseFontSize parses a font-size (CSS Fonts 4, 2.5): an absolute or
// relative keyword, a length, or a percentage of the parent's size, in
// user units.
func parseFontSize(v string, parent *style) (float64, bool) {
	ref := 16.0
	if parent != nil {
		ref = parent.fontSize
	}
	lv := strings.ToLower(strings.TrimSpace(v))
	if px, ok := absoluteSizes[lv]; ok {
		return px, true
	}
	switch lv {
	case "larger":
		return ref * 1.2, true
	case "smaller":
		return ref / 1.2, true
	}
	l, ok := parseLength(lv)
	if !ok || l.v < 0 {
		return 0, false
	}
	if l.u == unitPercent {
		return clampCoord(l.v / 100 * ref), true
	}
	return l.resolve(lengthCtx{fontSize: ref}, axisX), true
}

// parseWeight parses a font-weight (CSS Fonts 4, 2.2).
func parseWeight(v string, parent *style) (int, bool) {
	ref := 400
	if parent != nil {
		ref = parent.weight
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "normal":
		return 400, true
	case "bold":
		return 700, true
	case "bolder":
		// CSS Fonts 4, 2.2.1, Table 1.
		switch {
		case ref < 350:
			return 400, true
		case ref < 550:
			return 700, true
		}
		return max(ref, 900), true
	case "lighter":
		switch {
		case ref < 550:
			return 100, true
		case ref < 750:
			return 400, true
		}
		return 700, true
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 1000 {
		return 0, false
	}
	return n, true
}
