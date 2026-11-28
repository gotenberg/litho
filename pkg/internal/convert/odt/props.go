package odt

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
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
	// family holds the western, asian and complex family names, generic
	// the western family's style:font-family-generic and fixed whether
	// its style:font-pitch is fixed.
	family  [3]string
	generic string
	fixed   bool
	// lead is the family listed before the others: the family of the
	// script a part of a run is drawn in ([textState.forScript]), "" for
	// Western text.
	lead   string
	size   float64
	weight int
	italic bool
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
	kern   bool
	scale  float64
	shadow *css.Shadow
	// border, padding and boxShadow are a character border, its spacing
	// and its shadow (ODF 1.3, Part 3, 20.183, 20.240 and 20.349 on text
	// properties).
	border    [4]css.Border
	padding   [4]float64
	boxShadow []css.Shadow
	relief    string
	outline   bool
	hidden    bool
	lang      string
	country   string
	// scripts are the size, weight, posture and language of Asian and
	// complex text, which a style sets apart from the Western text's.
	scripts [2]scriptText
	// base holds the Western, Asian and complex sizes of the default
	// paragraph style, where the size of a text style's chain starts, and
	// chain the sizes that chain has reached ([builder.spanText]).
	base, chain [3]float64
}

// percentRule says what a font size in percent is a percentage of, by the
// layer that sets it: fo:font-size (ODF 1.3, Part 3, 20.190) and its
// -asian and -complex counterparts (20.270 to 20.281). 20.190 allows a
// percentage in a common style alone and bases it on the parent style's
// size.
type percentRule uint8

const (
	// percentOfBelow multiplies the size the layers below set: a common
	// paragraph style's parent styles, the default style at the root
	// (20.190), and a list level's text properties.
	percentOfBelow percentRule = iota
	// percentOfTextChain multiplies the size the chain of a common text
	// style has reached, from the default paragraph style's: the text
	// style's parent styles, not the paragraph it sits in.
	percentOfTextChain
	// percentIgnored leaves the size below as it is: an automatic style,
	// where 20.190 allows no percentage, and the default style, which has
	// no parent.
	percentIgnored
)

// stylePercent returns the [percentRule] of the style st's text
// properties, as Writer reads them.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, "abc 漢字 عربي" under
//     fo:font-size, style:font-size-asian and style:font-size-complex all
//     50% prints in all three scripts at 10 points (Tf) in a common
//     paragraph style whose parent sets 20, at 20 under 200% below that
//     style, at 6 under Standard; at 6 in a common text style without a
//     parent inside a 20-point paragraph, and at 10 there when the
//     default style sets 20 points and the paragraph 30; 50% and 150% in
//     an automatic paragraph or text style print the size below; 50% in
//     the default style prints 12.
//   - probe, odt: a styles.xml without office:master-styles makes Writer
//     read a common paragraph style's percentage against the default
//     style's size (6 points at 50% under a 20-point parent), the import
//     alone; a document Writer saves always holds master pages.
//   - libreoffice/odt odfexport/tdf107696.odt: Heading 1 at 130% under the
//     14-point Heading prints at 18.2 points.
func stylePercent(st *odf.Style) percentRule {
	switch {
	case st.Auto:
		return percentIgnored
	case st.Family == odf.FamilyText:
		return percentOfTextChain
	}
	return percentOfBelow
}

// foldSize lays the font size v of a layer over size, the size below, as
// pct says: a length sets it, a percentage multiplies what pct names.
// chain is the size a text style's chain has reached, which a length sets
// too under percentOfTextChain.
func foldSize(size, chain *float64, v string, pct percentRule) {
	if pt, ok := odf.Length(v); ok && pt > 0 {
		*size = pt
		if pct == percentOfTextChain {
			*chain = pt
		}
		return
	}
	pc, ok := odf.Percent(v)
	if !ok || pc <= 0 {
		return
	}
	switch pct {
	case percentOfBelow:
		*size *= pc
	case percentOfTextChain:
		*chain *= pc
		*size = *chain
	case percentIgnored:
	}
}

// scriptText is the text properties an OpenDocument style gives Asian or
// complex text apart from Western text: the -asian and -complex
// counterparts of fo:font-size, fo:font-weight, fo:font-style,
// fo:language and fo:country (ODF 1.3, Part 3, 20.270 to 20.281).
type scriptText struct {
	size    float64
	weight  int
	italic  bool
	lang    string
	country string
}

// Writer's font sizes for Asian and complex text when no style sets one,
// as fo:font-size sets the Western size alone: 12pt each in a package,
// and 10.5pt for Asian text in a flat document (ODF 1.3, Part 1, 2.2),
// which Writer reads with the defaults of a new document. ODF 1.3, Part
// 3, 20.190 gives no initial value; the sizes are Writer's.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a package without a
//     default style, with an empty one, with one setting fo:font-size
//     12pt alone or an Asian language (zh-CN, zh-TW, ja-JP, ko-KR), with
//     or without office:master-styles, an automatic style setting the
//     Asian language, and a meta:generator of LibreOffice or another,
//     prints "abc 漢字 عربي" at 12, 12 and 12 points; under a style setting
//     fo:font-size 20pt alone at 20, 12 and 12.
//   - probe, odt: the same text in a flat document without a default
//     style, with a Standard style, or with a default style setting
//     fo:font-size 12pt alone prints at 12, 10.5 and 12 points, the same
//     bytes under a .odt name too; a default style setting
//     style:font-size-asian 12pt prints the Han text at 12;
//     style:font-size-asian 200% and style:font-size-complex 150% print
//     21 and 18 points.
const (
	defaultAsianSize   = 12
	flatAsianSize      = 10.5
	defaultComplexSize = 12
)

// newTextState returns the text properties no style has set: Writer's
// default sizes, those of a flat document when flat, and the normal
// weight.
func newTextState(flat bool) textState {
	asian := float64(defaultAsianSize)
	if flat {
		asian = flatAsianSize
	}
	return textState{
		size: defaultFontSize, weight: 400,
		scripts: [2]scriptText{{size: asian, weight: 400}, {size: defaultComplexSize, weight: 400}},
	}
}

// forScript returns t as text of the script sc draws: its size, weight,
// posture and language, its family listed first. The parts of a run
// split by script each take theirs (ODF 1.3, Part 3, 20.270 to 20.281).
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a paragraph whose
//     style sets fo:font-weight bold and fo:font-style italic alone
//     prints its Han and Arabic text in Noto Sans CJK JP Regular and
//     DejaVu Sans; style:font-weight-asian and -complex bold print Noto
//     Sans CJK JP Bold and DejaVu Sans Bold.
func (t textState) forScript(sc odf.Script) textState {
	if sc == odf.Western {
		return t
	}
	s := t.scripts[sc-1]
	if s.size > 0 {
		t.size = s.size
	}
	if s.weight > 0 {
		t.weight = s.weight
	}
	t.italic = s.italic
	t.lang, t.country = s.lang, s.country
	t.lead = t.family[sc]
	return t
}

// defaultFontSize is the font size when no style sets one: LibreOffice's
// 12pt, the size of its default paragraph style.
const defaultFontSize = 12

// escapements maps the keyword of style:text-position (ODF 1.3, Part 3,
// 20.374) to [textState]'s auto: +1 for super, -1 for sub, 0 for a
// percentage.
var escapements = map[string]int{"super": 1, "sub": -1}

// textTransforms maps fo:text-transform to css, none
// for any other value.
var textTransforms = map[string]css.TextTransform{
	"uppercase": css.Uppercase, "lowercase": css.Lowercase, "capitalize": css.Capitalize,
}

// foldText returns t with the text properties layer laid over it.
// A size in percent follows pct.
func (b *builder) foldText(t textState, layer odf.Props, pct percentRule) textState {
	if v, ok := layer.Lookup("fo:font-size"); ok {
		foldSize(&t.size, &t.chain[0], v, pct)
	}
	for i, suffix := range []string{"", "-asian", "-complex"} {
		if name, ok := layer.Lookup("style:font-name" + suffix); ok {
			if ff, ok := b.styles.Fonts[name]; ok {
				t.family[i] = ff.Family
				if i == 0 {
					t.generic, t.fixed = ff.Generic, ff.Pitch == "fixed"
				}
			} else {
				t.family[i] = name
			}
		} else if fam, ok := layer.Lookup(fontFamilyKey(suffix)); ok {
			t.family[i] = odf.Unquote(fam)
			if i == 0 {
				t.generic, t.fixed = layer.Get("style:font-family-generic"), layer.Get("style:font-pitch") == "fixed"
			}
		}
	}
	if v, ok := layer.Lookup("fo:font-weight"); ok {
		t.weight = fontWeight(v, t.weight)
	}
	if v, ok := layer.Lookup("fo:font-style"); ok {
		t.italic = v == "italic" || v == "oblique"
	}
	for i, suffix := range [2]string{"-asian", "-complex"} {
		t.scripts[i] = foldScriptText(t.scripts[i], layer, suffix, &t.chain[i+1], pct)
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
			t.auto = escapements[f[0]]
		}
	}
	if v, ok := layer.Lookup("fo:text-transform"); ok {
		t.transform = textTransforms[v]
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
	t.border, t.padding = foldBox(t.border, t.padding, layer)
	if v, ok := layer.Lookup("style:shadow"); ok {
		t.boxShadow = boxShadow(v)
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

// foldScriptText returns s with the Asian or complex text properties of
// layer, the attributes of suffix -asian or -complex, laid over it: a
// size follows pct ([foldSize]), chain being the size the text style's
// chain has reached.
func foldScriptText(s scriptText, layer odf.Props, suffix string, chain *float64, pct percentRule) scriptText {
	if v, ok := layer.Lookup("style:font-size" + suffix); ok {
		foldSize(&s.size, chain, v, pct)
	}
	if v, ok := layer.Lookup("style:font-weight" + suffix); ok {
		s.weight = fontWeight(v, s.weight)
	}
	if v, ok := layer.Lookup("style:font-style" + suffix); ok {
		s.italic = v == "italic" || v == "oblique"
	}
	if v, ok := layer.Lookup("style:language" + suffix); ok {
		s.lang = v
	}
	if v, ok := layer.Lookup("style:country" + suffix); ok {
		s.country = v
	}
	return s
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
	if n, err := intconv.Atoi(v); err == nil && n >= 100 && n <= 900 {
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
	for f := range strings.FieldsSeq(v) {
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
	// lineBreak and punctWrap are style:line-break and
	// style:punctuation-wrap, "" where no style sets them.
	lineBreak, punctWrap string
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
		// A value that does not read leaves Atoi's 0, which applyPara
		// raises to 1; one out of range its saturated bound.
		p.widows, _ = intconv.Atoi(v)
	}
	if v, ok := layer.Lookup("fo:orphans"); ok {
		// As fo:widows.
		p.orphans, _ = intconv.Atoi(v)
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
	if v, ok := layer.Lookup("style:line-break"); ok {
		p.lineBreak = v
	}
	if v, ok := layer.Lookup("style:punctuation-wrap"); ok {
		p.punctWrap = v
	}
	if dc := layer.Child("style:drop-cap"); dc != nil {
		p.dropCap = dc
	}
	if v, ok := layer.Lookup("style:contextual-spacing"); ok {
		p.contextual = v == "true"
	} else if v, ok := layer.Lookup("loext:contextual-spacing"); ok {
		// LibreOffice wrote the attribute in its extension namespace
		// before ODF 1.3 named it.
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
	for f := range strings.FieldsSeq(v) {
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
