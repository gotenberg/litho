package docx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// w is the WordprocessingML namespace, which every element and attribute
// the converter reads is in.
const w = ooxml.NSW

// colorSpec is a w:color value: a colour, or auto (ECMA-376 Part 1,
// 17.3.2.6).
type colorSpec struct {
	auto bool
	c    css.Color
}

// underlineSpec is a w:u value (ECMA-376 Part 1, 17.3.2.40): its style
// (none, single, double, thick, dotted, dash, wave...) and its colour.
type underlineSpec struct {
	style string
	color *colorSpec
}

// fontSpec is a w:rFonts value (ECMA-376 Part 1, 17.3.2.26), each slot
// set or empty: the typefaces of the four script classes, each possibly
// named through the theme.
type fontSpec struct {
	ascii, hAnsi, eastAsia, cs string
}

// runProps are the run properties the converter reads (ECMA-376 Part 1,
// 17.3.2), each nil when not specified at the level that holds them, so
// that levels merge: a later level overrides the properties it sets.
type runProps struct {
	style                                          string
	fonts                                          fontSpec
	bold, italic, caps, smallCaps, strike, dstrike *bool
	vanish, outline, shadow, emboss, imprint, rtl  *bool
	size                                           *float64
	color                                          *colorSpec
	underline                                      *underlineSpec
	highlight, shading                             *css.Color
	noHighlight, noShading                         bool
	spacing, scale, position                       *float64
	// kern is the least font size in points that kerns (17.3.2.19), 0
	// for none.
	kern                                    *float64
	vertAlign, lang, langEastAsia, langBidi *string
	specVanish                              *bool
}

// merge returns r with the properties o sets laid over it.
func (r runProps) merge(o *runProps) runProps {
	if o == nil {
		return r
	}
	if o.style != "" {
		r.style = o.style
	}
	setStr := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	setStr(&r.fonts.ascii, o.fonts.ascii)
	setStr(&r.fonts.hAnsi, o.fonts.hAnsi)
	setStr(&r.fonts.eastAsia, o.fonts.eastAsia)
	setStr(&r.fonts.cs, o.fonts.cs)
	for _, p := range []struct{ dst, src **bool }{
		{&r.bold, &o.bold},
		{&r.italic, &o.italic},
		{&r.caps, &o.caps},
		{&r.smallCaps, &o.smallCaps},
		{&r.strike, &o.strike},
		{&r.dstrike, &o.dstrike},
		{&r.vanish, &o.vanish},
		{&r.outline, &o.outline},
		{&r.shadow, &o.shadow},
		{&r.emboss, &o.emboss},
		{&r.imprint, &o.imprint},
		{&r.rtl, &o.rtl},
		{&r.specVanish, &o.specVanish},
	} {
		if *p.src != nil {
			*p.dst = *p.src
		}
	}
	for _, p := range []struct{ dst, src **float64 }{
		{&r.size, &o.size}, {&r.spacing, &o.spacing}, {&r.scale, &o.scale}, {&r.position, &o.position}, {&r.kern, &o.kern},
	} {
		if *p.src != nil {
			*p.dst = *p.src
		}
	}
	for _, p := range []struct{ dst, src **string }{
		{&r.vertAlign, &o.vertAlign}, {&r.lang, &o.lang}, {&r.langEastAsia, &o.langEastAsia}, {&r.langBidi, &o.langBidi},
	} {
		if *p.src != nil {
			*p.dst = *p.src
		}
	}
	if o.color != nil {
		r.color = o.color
	}
	if o.underline != nil {
		r.underline = o.underline
	}
	if o.highlight != nil || o.noHighlight {
		r.highlight, r.noHighlight = o.highlight, o.noHighlight
	}
	if o.shading != nil || o.noShading {
		r.shading, r.noShading = o.shading, o.noShading
	}
	return r
}

// boolOf returns the value of an on/off property, false when unset.
func boolOf(b *bool) bool { return b != nil && *b }

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// parseRunProps reads a w:rPr element; nil yields an empty set.
func (d *document) parseRunProps(e *ooxml.Element) *runProps {
	r := &runProps{}
	if e == nil {
		return r
	}
	for _, c := range e.Kids() {
		if c.Space != w {
			continue
		}
		val, hasVal := c.Attr("val")
		switch c.Local {
		case "rStyle":
			r.style = val
		case "rFonts":
			r.fonts = d.parseFonts(c)
		case "b":
			r.bold = ptr(ooxml.Toggle(c))
		case "i":
			r.italic = ptr(ooxml.Toggle(c))
		case "caps":
			r.caps = ptr(ooxml.Toggle(c))
		case "smallCaps":
			r.smallCaps = ptr(ooxml.Toggle(c))
		case "strike":
			r.strike = ptr(ooxml.Toggle(c))
		case "dstrike":
			r.dstrike = ptr(ooxml.Toggle(c))
		case "vanish":
			r.vanish = ptr(ooxml.Toggle(c))
		case "specVanish":
			r.specVanish = ptr(ooxml.Toggle(c))
		case "outline":
			r.outline = ptr(ooxml.Toggle(c))
		case "shadow":
			r.shadow = ptr(ooxml.Toggle(c))
		case "emboss":
			r.emboss = ptr(ooxml.Toggle(c))
		case "imprint":
			r.imprint = ptr(ooxml.Toggle(c))
		case "rtl":
			r.rtl = ptr(ooxml.Toggle(c))
		case "sz":
			// ST_HpsMeasure, half-points (17.3.2.38).
			if v, ok := ooxml.Measure(val, 0.5); ok && v > 0 {
				r.size = ptr(min(v, maxFontSize))
			}
		case "color":
			r.color = d.parseColor(c, "val")
		case "u":
			u := &underlineSpec{style: "single"}
			if hasVal {
				u.style = val
			}
			if _, ok := c.Attr("color"); ok {
				u.color = d.parseColor(c, "color")
			}
			r.underline = u
		case "highlight":
			if col, ok := highlightColor(val); ok {
				r.highlight = &col
			} else {
				r.noHighlight = true
			}
		case "shd":
			if col, ok := shadingColor(c); ok {
				r.shading = &col
			} else {
				r.noShading = true
			}
		case "spacing":
			if v, ok := ooxml.Twips(val); ok {
				r.spacing = ptr(max(-maxFontSize, min(maxFontSize, v)))
			}
		case "w":
			// ST_TextScale, a percentage, 1 to 600 (17.3.2.43).
			if v, ok := ooxml.Int(strings.TrimSuffix(val, "%")); ok && v > 0 {
				r.scale = ptr(float64(min(v, 600)) / 100)
			}
		case "kern":
			// ST_HpsMeasure, half-points (17.3.2.19).
			if v, ok := ooxml.Measure(val, 0.5); ok {
				r.kern = ptr(max(0, min(maxFontSize, v)))
			}
		case "position":
			if v, ok := ooxml.Measure(val, 0.5); ok {
				r.position = ptr(max(-maxFontSize, min(maxFontSize, v)))
			}
		case "vertAlign":
			r.vertAlign = ptr(val)
		case "lang":
			if hasVal {
				r.lang = ptr(val)
			}
			if v, ok := c.Attr("eastAsia"); ok {
				r.langEastAsia = ptr(v)
			}
			if v, ok := c.Attr("bidi"); ok {
				r.langBidi = ptr(v)
			}
		}
	}
	return r
}

// parseFonts reads a w:rFonts element, resolving the theme font attributes
// (asciiTheme, hAnsiTheme, eastAsiaTheme, cstheme), which take precedence
// over the plain ones (ECMA-376 Part 1, 17.3.2.26).
func (d *document) parseFonts(e *ooxml.Element) fontSpec {
	pick := func(plain, theme string) string {
		if t, ok := e.Attr(theme); ok {
			if f := d.theme.Font(t); f != "" {
				return f
			}
		}
		v, _ := e.Attr(plain)
		return v
	}
	return fontSpec{
		ascii:    pick("ascii", "asciiTheme"),
		hAnsi:    pick("hAnsi", "hAnsiTheme"),
		eastAsia: pick("eastAsia", "eastAsiaTheme"),
		cs:       pick("cs", "cstheme"),
	}
}

// parseColor reads the colour of a w:color element (attr val), or of a
// w:u or border element (attr color): the attribute as
// written (what Word last computed from the theme colour), else the theme
// colour with its tint or shade, else auto (ECMA-376 Part 1, 17.3.2.6).
func (d *document) parseColor(e *ooxml.Element, attr string) *colorSpec {
	if v, ok := e.Attr(attr); ok {
		if c, ok := ooxml.HexColor(v); ok {
			return &colorSpec{c: c}
		}
	}
	if t, ok := e.Attr("themeColor"); ok {
		if c, ok := d.theme.Color(t); ok {
			if v, ok := ooxml.HexByte(attrOr(e, "themeTint")); ok {
				c = ooxml.Tint(c, v)
			}
			if v, ok := ooxml.HexByte(attrOr(e, "themeShade")); ok {
				c = ooxml.Shade(c, v)
			}
			return &colorSpec{c: c}
		}
	}
	return &colorSpec{auto: true}
}

// attrOr returns e's attribute local, "" when absent.
func attrOr(e *ooxml.Element, local string) string {
	v, _ := e.Attr(local)
	return v
}

// shadingColor returns the colour a w:shd paints (ECMA-376 Part 1,
// 17.3.5): its fill, the pattern colour for a solid pattern, and the
// colour pct patterns mix of the two; false for none or auto.
func shadingColor(e *ooxml.Element) (css.Color, bool) {
	val := attrOr(e, "val")
	fill, okFill := ooxml.HexColor(attrOr(e, "fill"))
	pat, okPat := ooxml.HexColor(attrOr(e, "color"))
	if !okPat {
		pat = css.Color{A: 1}
	}
	switch {
	case val == "solid":
		return pat, true
	case strings.HasPrefix(val, "pct"):
		p, ok := ooxml.Int(strings.TrimPrefix(val, "pct"))
		if !ok {
			break
		}
		if !okFill {
			fill = css.Color{R: 1, G: 1, B: 1, A: 1}
		}
		f := float64(p) / 100
		return css.Color{R: fill.R*(1-f) + pat.R*f, G: fill.G*(1-f) + pat.G*f, B: fill.B*(1-f) + pat.B*f, A: 1}, true
	}
	return fill, okFill
}

// highlightColor returns the colour of an ST_HighlightColor (ECMA-376
// Part 1, 17.18.40), false for none.
func highlightColor(name string) (css.Color, bool) {
	hex := map[string]string{
		"black": "000000", "blue": "0000FF", "cyan": "00FFFF", "green": "00FF00",
		"magenta": "FF00FF", "red": "FF0000", "yellow": "FFFF00", "white": "FFFFFF",
		"darkBlue": "000080", "darkCyan": "008080", "darkGreen": "008000",
		"darkMagenta": "800080", "darkRed": "800000", "darkYellow": "808000",
		"darkGray": "808080", "lightGray": "C0C0C0",
	}[name]
	return ooxml.HexColor(hex)
}

// maxFontSize bounds font sizes and letter spacing, in points: ECMA-376
// Part 1, 17.3.2.38 allows sizes to 1638 points; the text stack takes at
// most 1000.
const maxFontSize = 1000

// borderSpec is one border of a paragraph, a table or a cell (ECMA-376
// Part 1, 17.3.4).
type borderSpec struct {
	css.Border
	// space is the distance from the text to the border in points.
	space float64
}

// tabSpec is one custom tab stop of a paragraph (ECMA-376 Part 1,
// 17.3.1.37), or a clear of an inherited one.
type tabSpec struct {
	stop  css.TabStop
	clear bool
}

// paraProps are the paragraph properties the converter reads (ECMA-376
// Part 1, 17.3.1), each nil when not specified at their level.
type paraProps struct {
	style                                     string
	jc                                        *string
	indLeft, indRight, firstLine              *float64
	before, after, line                       *float64
	beforeAuto, afterAuto                     *bool
	lineRule                                  *string
	contextual, keepNext, keepLines, widow    *bool
	pageBreakBefore, bidi, suppressLineNumber *bool
	borders                                   map[string]*borderSpec
	shading                                   *css.Color
	noShading                                 bool
	tabs                                      []tabSpec
	outline                                   *int
	numID, ilvl                               *int
	framePr                                   *ooxml.Element
	// mark holds the run properties of the paragraph mark (17.3.1.29),
	// which also style a list label.
	mark *runProps
}

// merge returns p with the properties o sets laid over it; the tab stops
// accumulate, a clear removing an inherited stop at its position.
func (p paraProps) merge(o *paraProps) paraProps {
	if o == nil {
		return p
	}
	if o.style != "" {
		p.style = o.style
	}
	for _, f := range []struct{ dst, src **float64 }{
		{&p.indLeft, &o.indLeft},
		{&p.indRight, &o.indRight},
		{&p.firstLine, &o.firstLine},
		{&p.before, &o.before},
		{&p.after, &o.after},
		{&p.line, &o.line},
	} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
	for _, f := range []struct{ dst, src **bool }{
		{&p.beforeAuto, &o.beforeAuto},
		{&p.afterAuto, &o.afterAuto},
		{&p.contextual, &o.contextual},
		{&p.keepNext, &o.keepNext},
		{&p.keepLines, &o.keepLines},
		{&p.widow, &o.widow},
		{&p.pageBreakBefore, &o.pageBreakBefore},
		{&p.bidi, &o.bidi},
		{&p.suppressLineNumber, &o.suppressLineNumber},
	} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
	for _, f := range []struct{ dst, src **string }{{&p.jc, &o.jc}, {&p.lineRule, &o.lineRule}} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
	for _, f := range []struct{ dst, src **int }{{&p.outline, &o.outline}, {&p.numID, &o.numID}, {&p.ilvl, &o.ilvl}} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
	if len(o.borders) > 0 {
		merged := make(map[string]*borderSpec, len(p.borders)+len(o.borders))
		for k, v := range p.borders {
			merged[k] = v
		}
		for k, v := range o.borders {
			merged[k] = v
		}
		p.borders = merged
	}
	if o.shading != nil || o.noShading {
		p.shading, p.noShading = o.shading, o.noShading
	}
	if len(o.tabs) > 0 {
		var tabs []tabSpec
		for _, t := range p.tabs {
			cleared := false
			for _, n := range o.tabs {
				if n.clear && abs(n.stop.Pos-t.stop.Pos) < 0.5 {
					cleared = true
				}
			}
			if !cleared {
				tabs = append(tabs, t)
			}
		}
		for _, n := range o.tabs {
			if !n.clear {
				tabs = append(tabs, n)
			}
		}
		p.tabs = tabs
	}
	if o.framePr != nil {
		p.framePr = o.framePr
	}
	if o.mark != nil {
		m := runProps{}
		if p.mark != nil {
			m = *p.mark
		}
		m = m.merge(o.mark)
		p.mark = &m
	}
	return p
}

// abs returns the absolute value of v.
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// parseParaProps reads a w:pPr element; nil yields an empty set.
func (d *document) parseParaProps(e *ooxml.Element) *paraProps {
	p := &paraProps{}
	if e == nil {
		return p
	}
	for _, c := range e.Kids() {
		if c.Space != w {
			continue
		}
		val := attrOr(c, "val")
		switch c.Local {
		case "pStyle":
			p.style = val
		case "jc":
			p.jc = ptr(val)
		case "ind":
			d.parseInd(c, p)
		case "spacing":
			for _, a := range []struct {
				name string
				dst  **float64
			}{{"before", &p.before}, {"after", &p.after}} {
				if v, ok := ooxml.Twips(attrOr(c, a.name)); ok {
					*a.dst = ptr(max(0, v))
				}
			}
			// beforeLines and afterLines are hundredths of a line, which
			// Word sizes as 12 points (17.3.1.33).
			if v, ok := ooxml.Int(attrOr(c, "beforeLines")); ok {
				p.before = ptr(max(0, float64(v)*0.12))
			}
			if v, ok := ooxml.Int(attrOr(c, "afterLines")); ok {
				p.after = ptr(max(0, float64(v)*0.12))
			}
			if v, ok := c.Attr("beforeAutospacing"); ok {
				p.beforeAuto = ptr(ooxml.OnOff(v, true))
			}
			if v, ok := c.Attr("afterAutospacing"); ok {
				p.afterAuto = ptr(ooxml.OnOff(v, true))
			}
			if v, ok := ooxml.Int(attrOr(c, "line")); ok {
				p.line = ptr(float64(v))
				if p.lineRule == nil {
					p.lineRule = ptr("auto")
				}
			}
			if v, ok := c.Attr("lineRule"); ok {
				p.lineRule = ptr(v)
			}
		case "contextualSpacing":
			p.contextual = ptr(ooxml.Toggle(c))
		case "keepNext":
			p.keepNext = ptr(ooxml.Toggle(c))
		case "keepLines":
			p.keepLines = ptr(ooxml.Toggle(c))
		case "widowControl":
			p.widow = ptr(ooxml.Toggle(c))
		case "pageBreakBefore":
			p.pageBreakBefore = ptr(ooxml.Toggle(c))
		case "bidi":
			p.bidi = ptr(ooxml.Toggle(c))
		case "suppressLineNumbers":
			p.suppressLineNumber = ptr(ooxml.Toggle(c))
		case "pBdr":
			p.borders = map[string]*borderSpec{}
			for _, b := range c.Kids() {
				name := b.Local
				switch name {
				case "start":
					name = "left"
				case "end":
					name = "right"
				}
				p.borders[name] = d.parseBorder(b)
			}
		case "shd":
			if col, ok := shadingColor(c); ok {
				p.shading = &col
			} else {
				p.noShading = true
			}
		case "tabs":
			for _, t := range c.Kids() {
				if !t.Is(w, "tab") {
					continue
				}
				if ts, ok := parseTab(t); ok {
					p.tabs = append(p.tabs, ts)
				}
			}
		case "outlineLvl":
			if v, ok := ooxml.Int(val); ok && v >= 0 && v <= 9 {
				p.outline = ptr(v)
			}
		case "numPr":
			if v, ok := ooxml.Int(attrOr(c.Child(w, "numId"), "val")); ok {
				p.numID = ptr(v)
			}
			if v, ok := ooxml.Int(attrOr(c.Child(w, "ilvl"), "val")); ok {
				p.ilvl = ptr(max(0, min(8, v)))
			}
		case "framePr":
			p.framePr = c
		case "rPr":
			p.mark = d.parseRunProps(c)
		}
	}
	return p
}

// parseInd reads a w:ind element (ECMA-376 Part 1, 17.3.1.12): left or
// start, right or end, and firstLine or hanging, a hanging indent stored
// as a negative first line.
func (d *document) parseInd(c *ooxml.Element, p *paraProps) {
	for _, a := range []struct {
		names []string
		dst   **float64
	}{{[]string{"start", "left"}, &p.indLeft}, {[]string{"end", "right"}, &p.indRight}} {
		for _, n := range a.names {
			if v, ok := ooxml.Twips(attrOr(c, n)); ok {
				*a.dst = ptr(v)
				break
			}
		}
	}
	// startChars and the other character units count hundredths of a
	// character, sized here at the 10.5-point default of East Asian text.
	if v, ok := ooxml.Int(attrOr(c, "startChars")); ok && v != 0 {
		p.indLeft = ptr(float64(v) * 0.105)
	}
	if v, ok := ooxml.Twips(attrOr(c, "firstLine")); ok {
		p.firstLine = ptr(v)
	}
	if v, ok := ooxml.Twips(attrOr(c, "hanging")); ok {
		p.firstLine = ptr(-v)
	}
}

// parseTab reads a w:tab stop (ECMA-376 Part 1, 17.3.1.37).
func parseTab(t *ooxml.Element) (tabSpec, bool) {
	pos, ok := ooxml.Twips(attrOr(t, "pos"))
	if !ok {
		return tabSpec{}, false
	}
	ts := tabSpec{stop: css.TabStop{Pos: max(0, pos)}}
	switch attrOr(t, "val") {
	case "clear":
		ts.clear = true
	case "center":
		ts.stop.Align = css.TabCenter
	case "right", "end":
		ts.stop.Align = css.TabRight
	case "decimal":
		ts.stop.Align = css.TabDecimal
	case "bar":
		// LibreOffice draws no rule for a bar tab and stops at it as at a
		// left one.
		ts.stop.Align = css.TabLeft
	}
	switch attrOr(t, "leader") {
	case "dot":
		ts.stop.Leader = css.TabLeaderDots
	case "hyphen":
		ts.stop.Leader = css.TabLeaderHyphens
	case "underscore", "heavy":
		ts.stop.Leader = css.TabLeaderUnderscore
	case "middleDot":
		ts.stop.Leader = css.TabLeaderMiddleDots
	}
	return ts, true
}

// parseBorder reads one border element (ECMA-376 Part 1, 17.3.4: its val,
// its sz in eighths of a point, its space in points and its colour).
func (d *document) parseBorder(b *ooxml.Element) *borderSpec {
	bs := &borderSpec{}
	val := attrOr(b, "val")
	sz := 4.0
	if v, ok := ooxml.Int(attrOr(b, "sz")); ok {
		sz = float64(max(2, min(96, v)))
	}
	bs.Width = sz / 8
	switch val {
	case "nil", "none", "":
		bs.Style = css.BorderNone
		bs.Width = 0
	case "double", "triple", "thinThickSmallGap", "thickThinSmallGap", "thinThickThinSmallGap",
		"thinThickMediumGap", "thickThinMediumGap", "thinThickThinMediumGap",
		"thinThickLargeGap", "thickThinLargeGap", "thinThickThinLargeGap", "doubleWave":
		bs.Style = css.Double
		// A double line draws its two lines and the gap between them.
		bs.Width = max(bs.Width*3, 1.5)
	case "dotted":
		bs.Style = css.Dotted
	case "dashed", "dashSmallGap", "dotDash", "dotDotDash", "dashDotStroked":
		bs.Style = css.Dashed
	case "threeDEmboss":
		bs.Style = css.Ridge
	case "threeDEngrave":
		bs.Style = css.Groove
	case "outset":
		bs.Style = css.Outset
	case "inset":
		bs.Style = css.Inset
	default:
		bs.Style = css.Solid
	}
	if v, ok := ooxml.Int(attrOr(b, "space")); ok {
		bs.space = float64(max(0, min(31, v)))
	}
	bs.Color = css.Color{A: 1}
	if c := d.parseColor(b, "color"); !c.auto {
		bs.Color = c.c
	}
	return bs
}
