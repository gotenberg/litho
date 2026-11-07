package odt

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// decoLine is one text decoration line of style:text-properties (ODF
// 1.3, Part 3, 20.379 to 20.384 for underlines, 20.364 to 20.369 for
// line-through, 20.372 to 20.376 for overlines).
type decoLine struct {
	on    bool
	style css.DecorationStyle
	// color is nil for the font colour.
	color *css.Color
	bold  bool
}

// textState is the text properties in force on a run, folded layer by
// layer: a family's default style, the style chain from the farthest,
// then the span styles from the outermost (ODF 1.3, Part 3, 16.2 and
// 6.1.7, a span's text properties inherit from the paragraph's).
type textState struct {
	// family holds the western, asian and complex family names and
	// generic the western family's style:font-family-generic.
	family  [3]string
	generic string
	size    float64
	weight  int
	italic  bool
	// color is nil for the automatic colour, black on white paper.
	color      *css.Color
	background css.Color
	under      decoLine
	through    decoLine
	over       decoLine
	// pos and rel are style:text-position: the raise as a fraction of the
	// font size and the scale of the raised text, rel 0 for none.
	pos, rel float64
	// auto is +1 for "super" and -1 for "sub", Writer's automatic
	// escapement, which [builder.raise] measures on the face; pos then
	// holds the default raise for a face it cannot measure.
	auto      int
	transform css.TextTransform
	smallCaps bool
	letter    float64
	letterSet bool
	// kern is style:letter-kerning, false when no style sets it.
	kern    bool
	scale   float64
	shadow  *css.Shadow
	relief  string
	outline bool
	hidden  bool
	lang    string
	country string
}

// defaultFontSize is the font size when no style sets one: LibreOffice's
// 12pt, the size of its default paragraph style.
const defaultFontSize = 12

// genericFamilies maps style:font-family-generic (ODF 1.3, Part 3,
// 19.512) to the CSS generic family the text stack resolves.
var genericFamilies = map[string]string{
	"roman": "serif", "swiss": "sans-serif", "modern": "monospace",
	"decorative": "fantasy", "script": "cursive",
}

// fold returns t with the text properties layer laid over it.
func (b *builder) foldText(t textState, layer odf.Props) textState {
	if v, ok := layer.Lookup("fo:font-size"); ok {
		if pt, ok := odf.Length(v); ok && pt > 0 {
			t.size = pt
		} else if pc, ok := odf.Percent(v); ok && pc > 0 {
			t.size *= pc
		}
	}
	for i, suffix := range []string{"", "-asian", "-complex"} {
		if name, ok := layer.Lookup("style:font-name" + suffix); ok {
			if ff, ok := b.styles.Fonts[name]; ok {
				t.family[i] = ff.Family
				if i == 0 {
					t.generic = ff.Generic
				}
			} else {
				t.family[i] = name
			}
		} else if fam, ok := layer.Lookup(fontFamilyKey(suffix)); ok {
			t.family[i] = odf.Unquote(fam)
			if i == 0 {
				t.generic = layer.Get("style:font-family-generic")
			}
		}
	}
	if v, ok := layer.Lookup("fo:font-weight"); ok {
		t.weight = fontWeight(v, t.weight)
	}
	if v, ok := layer.Lookup("fo:font-style"); ok {
		t.italic = v == "italic" || v == "oblique"
	}
	if v, ok := layer.Lookup("fo:color"); ok {
		if c, ok := odf.ParseColor(v); ok {
			cc := cssColor(c)
			t.color = &cc
		}
	}
	if layer.Get("style:use-window-font-color") == "true" {
		t.color = nil
	}
	if v, ok := layer.Lookup("fo:background-color"); ok {
		t.background = css.Transparent
		if c, ok := odf.ParseColor(v); ok {
			t.background = cssColor(c)
		}
	}
	t.under = foldDeco(t.under, layer, "underline")
	t.through = foldDeco(t.through, layer, "line-through")
	t.over = foldDeco(t.over, layer, "overline")
	if v, ok := layer.Lookup("style:text-position"); ok {
		t.pos, t.rel = textPosition(v)
		t.auto = 0
		if f := strings.Fields(v); len(f) > 0 && t.rel > 0 {
			t.auto = map[string]int{"super": 1, "sub": -1}[f[0]]
		}
	}
	if v, ok := layer.Lookup("fo:text-transform"); ok {
		t.transform = map[string]css.TextTransform{"uppercase": css.Uppercase, "lowercase": css.Lowercase, "capitalize": css.Capitalize}[v]
	}
	if v, ok := layer.Lookup("fo:font-variant"); ok {
		t.smallCaps = v == "small-caps"
	}
	if v, ok := layer.Lookup("fo:letter-spacing"); ok {
		t.letter, t.letterSet = odf.Length(v)
	}
	if v, ok := layer.Lookup("style:letter-kerning"); ok {
		t.kern = v == "true"
	}
	if v, ok := layer.Lookup("style:text-scale"); ok {
		if pc, ok := odf.Percent(v); ok && pc > 0 {
			t.scale = pc
		}
	}
	if v, ok := layer.Lookup("fo:text-shadow"); ok {
		t.shadow = textShadow(v)
	}
	if v, ok := layer.Lookup("style:font-relief"); ok {
		t.relief = v
		if v == "none" {
			t.relief = ""
		}
	}
	if v, ok := layer.Lookup("style:text-outline"); ok {
		t.outline = v == "true"
	}
	if v, ok := layer.Lookup("text:display"); ok {
		t.hidden = v == "none"
	}
	if v, ok := layer.Lookup("fo:language"); ok {
		t.lang = v
	}
	if v, ok := layer.Lookup("fo:country"); ok {
		t.country = v
	}
	return t
}

// fontFamilyKey returns the fo:font-family attribute key of a script
// suffix: fo:font-family for the western text, style:font-family-asian
// and style:font-family-complex for the others (ODF 1.3, Part 3, 20.192
// and 20.270).
func fontFamilyKey(suffix string) string {
	if suffix == "" {
		return "fo:font-family"
	}
	return "style:font-family" + suffix
}

// fontWeight parses fo:font-weight (XSL 1.0, 7.8.9): normal, bold or a
// multiple of 100; anything else keeps cur.
func fontWeight(v string, cur int) int {
	switch v {
	case "normal":
		return 400
	case "bold":
		return 700
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 100 && n <= 900 {
		return n
	}
	return cur
}

// foldDeco folds the decoration line kind ("underline", "line-through" or
// "overline") of layer over d (ODF 1.3, Part 3, 20.379 to 20.384 and
// their line-through and overline counterparts): the style, the type
// (single or double), the width and the colour.
func foldDeco(d decoLine, layer odf.Props, kind string) decoLine {
	key := "style:text-" + kind + "-"
	if v, ok := layer.Lookup(key + "style"); ok {
		d.on = v != "none" && v != ""
		switch v {
		case "dotted":
			d.style = css.DecorationDotted
		case "dash", "long-dash", "dot-dash", "dot-dot-dash":
			d.style = css.DecorationDashed
		case "wave":
			d.style = css.DecorationWavy
		default:
			d.style = css.DecorationSolid
		}
	}
	if v, ok := layer.Lookup(key + "type"); ok {
		switch v {
		case "none":
			d.on = false
		case "double":
			if d.style == css.DecorationSolid {
				d.style = css.DecorationDouble
			}
			d.on = d.on || !layer.Has(key+"style")
		default:
			d.on = d.on || !layer.Has(key+"style")
		}
	}
	if v, ok := layer.Lookup(key + "color"); ok {
		d.color = nil
		if c, ok := odf.ParseColor(v); ok {
			cc := cssColor(c)
			d.color = &cc
		}
	}
	if v, ok := layer.Lookup(key + "width"); ok {
		d.bold = v == "bold" || v == "thick"
	}
	return d
}

// superRaise is the raise of "super" and the lowering of "sub" in
// style:text-position, a third of the font size, and superScale the
// scale LibreOffice gives text whose position names no size (ODF 1.3,
// Part 3, 20.374).
const (
	superRaise = 0.33
	superScale = 1.0
)

// textPosition parses style:text-position (ODF 1.3, Part 3, 20.374):
// "super", "sub" or a percentage of the font size, then an optional scale
// percentage. "0% 100%" is the baseline at full size: rel 0.
func textPosition(v string) (pos, rel float64) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return 0, 0
	}
	switch f[0] {
	case "super":
		pos = superRaise
	case "sub":
		pos = -superRaise
	default:
		p, ok := odf.Percent(f[0])
		if !ok {
			return 0, 0
		}
		pos = p
	}
	rel = superScale
	if len(f) > 1 {
		if r, ok := odf.Percent(f[1]); ok && r > 0 {
			rel = r
		}
	}
	if pos == 0 && rel == 1 {
		return 0, 0
	}
	return pos, rel
}

// textShadowColor is the colour LibreOffice draws a text shadow in: a
// mid grey.
var textShadowColor = css.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}

// textShadow parses fo:text-shadow (XSL 1.0, 7.17.5, the CSS 2 syntax):
// none, or offsets with an optional colour; nil for none.
func textShadow(v string) *css.Shadow {
	if v == "none" || strings.TrimSpace(v) == "" {
		return nil
	}
	sh := &css.Shadow{Color: textShadowColor}
	var lens []float64
	for _, f := range strings.Fields(v) {
		if pt, ok := odf.Length(f); ok {
			lens = append(lens, pt)
		} else if c, ok := odf.ParseColor(f); ok {
			sh.Color = cssColor(c)
		}
	}
	if len(lens) < 2 {
		return nil
	}
	sh.X, sh.Y = lens[0], lens[1]
	if len(lens) > 2 {
		sh.Blur = max(lens[2], 0)
	}
	return sh
}

// cssColor converts an ODF colour to an opaque css.Color.
func cssColor(c odf.Color) css.Color {
	return css.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: 1}
}

// lineMode is how a paragraph's line height is given (ODF 1.3, Part 3,
// 20.317, 20.335 and 20.336).
type lineMode uint8

// The line height modes.
const (
	lineNormal lineMode = iota
	// lineProportional is fo:line-height in percent of the font's line
	// height.
	lineProportional
	// lineFixed is fo:line-height as a length.
	lineFixed
	// lineAtLeast is style:line-height-at-least.
	lineAtLeast
	// lineSpacing is style:line-spacing, leading added to the font's.
	lineSpacing
)

// paraState is the paragraph properties of a paragraph, folded layer by
// layer as [textState] is (ODF 1.3, Part 3, 17.6).
type paraState struct {
	margin                  [4]float64
	indent                  float64
	align                   string
	alignLast               string
	line                    lineMode
	lineVal                 float64
	keepNext                bool
	keepTogether            bool
	widows, orphans         int
	breakBefore, breakAfter string
	background              css.Color
	bgImage                 *odf.Element
	border                  [4]css.Border
	padding                 [4]float64
	joinBorder              bool
	shadow                  []css.Shadow
	tabs                    *odf.Element
	tabDist                 float64
	writing                 string
	vAlign                  string
	dropCap                 *odf.Element
	contextual              bool
}

// sideNames are the side suffixes of the fo:margin, fo:padding and
// fo:border longhands, indexed by css.Side.
var sideNames = [4]string{"top", "right", "bottom", "left"}

// foldPara returns p with the paragraph properties layer laid over it. A
// margin or indent in percent is relative to the value below it (ODF
// 1.3, Part 3, 20.200 to 20.205: relative to the parent style's).
func foldPara(p paraState, layer odf.Props) paraState {
	rel := func(v string, cur float64) (float64, bool) {
		if pt, ok := odf.Length(v); ok {
			return pt, true
		}
		if pc, ok := odf.Percent(v); ok {
			return cur * pc, true
		}
		return 0, false
	}
	if v, ok := layer.Lookup("fo:margin"); ok {
		for i := range p.margin {
			if m, ok := rel(v, p.margin[i]); ok {
				p.margin[i] = m
			}
		}
	}
	for i, s := range sideNames {
		if v, ok := layer.Lookup("fo:margin-" + s); ok {
			if m, ok := rel(v, p.margin[i]); ok {
				p.margin[i] = m
			}
		}
	}
	if v, ok := layer.Lookup("fo:text-indent"); ok {
		if m, ok := rel(v, p.indent); ok {
			p.indent = m
		}
	}
	if v, ok := layer.Lookup("fo:text-align"); ok {
		p.align = v
	}
	if v, ok := layer.Lookup("fo:text-align-last"); ok {
		p.alignLast = v
	}
	if v, ok := layer.Lookup("fo:line-height"); ok {
		if v == "normal" {
			p.line, p.lineVal = lineNormal, 0
		} else if pc, ok := odf.Percent(v); ok && pc > 0 {
			p.line, p.lineVal = lineProportional, pc
		} else if pt, ok := odf.Length(v); ok && pt > 0 {
			p.line, p.lineVal = lineFixed, pt
		}
	}
	if v, ok := layer.Lookup("style:line-height-at-least"); ok {
		if pt, ok := odf.Length(v); ok && pt > 0 {
			p.line, p.lineVal = lineAtLeast, pt
		}
	}
	if v, ok := layer.Lookup("style:line-spacing"); ok {
		if pt, ok := odf.Length(v); ok {
			p.line, p.lineVal = lineSpacing, pt
		}
	}
	if v, ok := layer.Lookup("fo:keep-with-next"); ok {
		p.keepNext = v == "always"
	}
	if v, ok := layer.Lookup("fo:keep-together"); ok {
		p.keepTogether = v == "always"
	}
	if v, ok := layer.Lookup("fo:widows"); ok {
		p.widows, _ = strconv.Atoi(v)
	}
	if v, ok := layer.Lookup("fo:orphans"); ok {
		p.orphans, _ = strconv.Atoi(v)
	}
	if v, ok := layer.Lookup("fo:break-before"); ok {
		p.breakBefore = v
	}
	if v, ok := layer.Lookup("fo:break-after"); ok {
		p.breakAfter = v
	}
	if v, ok := layer.Lookup("fo:background-color"); ok {
		p.background = css.Transparent
		if c, ok := odf.ParseColor(v); ok {
			p.background = cssColor(c)
		}
	}
	if bi := layer.Child("style:background-image"); bi != nil {
		p.bgImage = bi
	}
	p.border, p.padding = foldBox(p.border, p.padding, layer)
	if v, ok := layer.Lookup("style:join-border"); ok {
		p.joinBorder = v != "false"
	}
	if v, ok := layer.Lookup("style:shadow"); ok {
		p.shadow = boxShadow(v)
	}
	if ts := layer.Child("style:tab-stops"); ts != nil {
		p.tabs = ts
	}
	if v, ok := layer.Lookup("style:tab-stop-distance"); ok {
		if pt, ok := odf.Length(v); ok && pt >= 0 {
			p.tabDist = pt
		}
	}
	if v, ok := layer.Lookup("style:writing-mode"); ok {
		p.writing = v
	}
	if v, ok := layer.Lookup("style:vertical-align"); ok {
		p.vAlign = v
	}
	if dc := layer.Child("style:drop-cap"); dc != nil {
		p.dropCap = dc
	}
	if v, ok := layer.Lookup("style:contextual-spacing"); ok {
		p.contextual = v == "true"
	}
	return p
}

// foldBox folds the fo:border and fo:padding shorthands and longhands of
// layer over border and padding, the shorthand first (ODF 1.3, Part 3,
// 20.183 and 20.240).
func foldBox(border [4]css.Border, padding [4]float64, layer odf.Props) ([4]css.Border, [4]float64) {
	if v, ok := layer.Lookup("fo:border"); ok {
		bd := parseBorder(v)
		border = [4]css.Border{bd, bd, bd, bd}
	}
	for i, s := range sideNames {
		if v, ok := layer.Lookup("fo:border-" + s); ok {
			border[i] = parseBorder(v)
		}
	}
	if v, ok := layer.Lookup("fo:padding"); ok {
		if pt, ok := odf.Length(v); ok && pt >= 0 {
			padding = [4]float64{pt, pt, pt, pt}
		}
	}
	for i, s := range sideNames {
		if v, ok := layer.Lookup("fo:padding-" + s); ok {
			if pt, ok := odf.Length(v); ok && pt >= 0 {
				padding[i] = pt
			}
		}
	}
	return border, padding
}

// borderStyles maps the border styles of fo:border (XSL 1.0, 7.8.20, and
// LibreOffice's dash-dot extensions, drawn dashed) to css.
var borderStyles = map[string]css.BorderStyle{
	"none": css.BorderNone, "hidden": css.BorderHidden, "dotted": css.Dotted,
	"dashed": css.Dashed, "solid": css.Solid, "double": css.Double,
	"groove": css.Groove, "ridge": css.Ridge, "inset": css.Inset, "outset": css.Outset,
	"dash-dot": css.Dashed, "dash-dot-dot": css.Dashed, "fine-dashed": css.Dashed,
	"double-thin": css.Double,
}

// borderWidths are the width keywords of fo:border (XSL 1.0, 7.8.20):
// thin, medium and thick as CSS draws them, in points.
var borderWidths = map[string]float64{"thin": 0.75, "medium": 2.25, "thick": 3.75}

// parseBorder parses an fo:border value, "0.5pt solid #000000" (ODF 1.3,
// Part 3, 20.183): a width, a style and a colour in any order. "none"
// draws nothing.
func parseBorder(v string) css.Border {
	bd := css.Border{Color: css.Color{A: 1}}
	widthSet := false
	for _, f := range strings.Fields(v) {
		if pt, ok := odf.Length(f); ok {
			bd.Width, widthSet = max(pt, 0), true
		} else if w, ok := borderWidths[f]; ok {
			bd.Width, widthSet = w, true
		} else if s, ok := borderStyles[f]; ok {
			bd.Style = s
		} else if c, ok := odf.ParseColor(f); ok {
			bd.Color = cssColor(c)
		}
	}
	if !widthSet && bd.Style != css.BorderNone {
		bd.Width = borderWidths["medium"]
	}
	return bd
}

// boxShadow parses style:shadow (ODF 1.3, Part 3, 20.349): none, or a
// colour and two offsets.
func boxShadow(v string) []css.Shadow {
	sh := textShadow(v)
	if sh == nil {
		return nil
	}
	return []css.Shadow{*sh}
}
