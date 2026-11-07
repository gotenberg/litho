package odt

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// baseStyle returns the style every node starts from: CSS's initial
// values with the white space the converter already processed kept as it
// is (pre-wrap: ODF 1.3, Part 3, 6.1.2 collapses white space at import,
// and the converter does it), orphans and widows off, LibreOffice's
// default when a style sets neither, and a word wider than its line
// broken where the line ends, as Writer breaks it.
func baseStyle() *css.Style {
	st := css.Initial()
	st.WhiteSpace = css.PreWrap
	st.OverflowWrap = css.BreakWord
	st.Orphans, st.Widows = 1, 1
	st.FontSize = defaultFontSize
	st.TabSize = css.Pts(defaultTabDistance)
	return st
}

// defaultTabDistance is the default tab stop distance when no style sets
// one, 1.25 cm, LibreOffice's default.
const defaultTabDistance = 1.25 * 72 / 2.54

// families returns the font family list of t: the western, asian and
// complex families in that order, then the western one's generic family.
func (t textState) families() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range t.family {
		if f != "" && !seen[strings.ToLower(f)] {
			seen[strings.ToLower(f)] = true
			out = append(out, f)
		}
	}
	if g := genericFamilies[t.generic]; g != "" {
		out = append(out, g)
	}
	if len(out) == 0 {
		out = []string{"serif"}
	}
	return out
}

// applyText sets the text properties t gives on st. inline adds what a
// run carries and a paragraph does not: its decorations, background,
// raise and shadow; a raised run is scaled as style:text-position asks.
func applyText(st *css.Style, t textState, inline bool) {
	st.FontFamilies = t.families()
	st.FontSize = t.size
	if st.FontSize <= 0 {
		st.FontSize = defaultFontSize
	}
	st.FontWeight = t.weight
	if st.FontWeight == 0 {
		st.FontWeight = 400
	}
	st.FontStyle = css.FontNormal
	if t.italic {
		st.FontStyle = css.FontItalic
	}
	st.Color = css.Color{A: 1}
	if t.color != nil {
		st.Color = *t.color
	}
	st.TextTransform = t.transform
	st.FontVariant = css.VariantNormal
	if t.smallCaps {
		st.FontVariant = css.SmallCaps
	}
	st.LetterSpacing = css.Dimension{Kind: css.Normal}
	if t.letterSet {
		st.LetterSpacing = css.Pts(t.letter)
	}
	// Writer kerns text whose style:letter-kerning is true alone (ODF 1.3,
	// Part 3, 20.316): absent, as false, its glyphs keep their advances.
	st.FontKerning = css.KerningNone
	if t.kern {
		st.FontKerning = css.KerningNormal
	}
	st.TextScale = 0
	if t.scale > 0 && t.scale != 1 {
		st.TextScale = t.scale
	}
	if !inline {
		return
	}
	if t.rel > 0 {
		st.FontSize = t.size * t.rel
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(t.pos * t.size)}
	}
	st.BackgroundColor = t.background
	var deco css.TextDecoration
	var line *decoLine
	for _, d := range []struct {
		l   *decoLine
		bit css.TextDecoration
	}{{&t.under, css.Underline}, {&t.through, css.LineThrough}, {&t.over, css.Overline}} {
		if d.l.on {
			deco |= d.bit
			if line == nil {
				line = d.l
			}
		}
	}
	st.TextDecoration = deco
	if line != nil {
		st.TextDecorationStyle = line.style
		st.TextDecorationColor = st.Color
		if line.color != nil {
			st.TextDecorationColor = *line.color
		}
		if line.bold {
			st.TextDecorationThickness = css.Pts(math.Max(st.FontSize/10, 1))
		}
	}
	st.TextShadow = nil
	if t.shadow != nil {
		st.TextShadow = []css.Shadow{*t.shadow}
	}
	if t.relief != "" {
		// Relief approximated as a shadow a quarter point away: embossed
		// lights the text from the top left, engraved from the bottom
		// right.
		off := 0.25 * st.FontSize / defaultFontSize
		if t.relief == "engraved" {
			off = -off
		}
		st.TextShadow = append(st.TextShadow, css.Shadow{X: off, Y: off, Color: textShadowColor})
	}
}

// lang returns the BCP 47 tag of t's language and country, "" for none.
func (t textState) langTag() string {
	if t.lang == "" || t.lang == "zxx" || t.lang == "none" {
		return ""
	}
	if t.country != "" && t.country != "none" {
		return t.lang + "-" + t.country
	}
	return t.lang
}

// faceMetrics returns the ascent with the line gap and the descent of the
// face t selects, over its font size, as LibreOffice measures a font: the
// external leading counts above the ascent. Liberation Serif's without
// fonts or a face.
func (b *builder) faceMetrics(t textState) (ascent, descent float64) {
	const fallbackAscent, fallbackDescent = 0.933, 0.216
	if b.fonts == nil {
		return fallbackAscent, fallbackDescent
	}
	key := strings.Join(t.families(), ",") + "|" + strconv.Itoa(t.weight) + "|" + strconv.FormatBool(t.italic)
	if m, ok := b.metricsCache[key]; ok {
		return m[0], m[1]
	}
	const size = 100
	ascent, descent = fallbackAscent, fallbackDescent
	if fc := b.fonts.Match(text.Style{Families: text.BrowserFamilies(t.families()), Weight: max(t.weight, 100), Italic: t.italic, Size: size}); fc != nil {
		m := fc.Metrics(size)
		if a, d := (m.Ascent+m.LineGap)/size, m.Descent/size; a > 0 && a < 10 && d >= 0 && d < 10 {
			ascent, descent = a, d
		}
	}
	b.metricsCache[key] = [2]float64{ascent, descent}
	return ascent, descent
}

// raise returns the baseline shift of a run in Writer's automatic
// escapement (ODF 1.3, Part 3, 20.374, "super" and "sub"): a superscript
// rises until its ascent meets the full-size ascent, a subscript drops
// until its descent meets the full-size descent.
func (b *builder) raise(t textState) float64 {
	asc, desc := b.faceMetrics(t)
	if t.auto > 0 {
		return asc * t.size * (1 - t.rel)
	}
	return -desc * t.size * (1 - t.rel)
}

// lineRatio returns the height of a line of the face t selects in
// LibreOffice's measure, ascent, descent and line gap, over its font size;
// 1.15, Liberation Serif's, without fonts.
func (b *builder) lineRatio(t textState) float64 {
	const fallback = 1.15
	if b.fonts == nil {
		return fallback
	}
	asc, desc := b.faceMetrics(t)
	if r := asc + desc; r > 0 {
		return r
	}
	return fallback
}

// lineRule sets the line spacing of a paragraph block st from the
// paragraph's line spacing p, its text properties being t (ODF 1.3, Part
// 3, 20.315 fo:line-height, 20.317 style:line-height-at-least and 20.318
// style:line-spacing), with Writer's rules (see [css.LineRule]): a
// percentage multiplies the line's natural height, the room added below
// the glyphs; a length makes every line that tall, the baseline at four
// fifths; at least is a minimum over single spacing, the room above the
// glyphs; a leading adds its room below each line and above the first.
func (b *builder) lineRule(st *css.Style, p paraState, t textState) {
	switch p.line { //nolint:exhaustive // lineNormal keeps the initial line-height
	case lineProportional:
		st.LineRule = css.LineRuleMultiple
		st.LineHeight = css.Dimension{Kind: css.Number, Value: p.lineVal}
	case lineFixed:
		st.LineRule = css.LineRuleExact
		st.LineHeight = css.Pts(p.lineVal)
	case lineAtLeast:
		st.LineRule = css.LineRuleMultiple
		st.LineHeight = css.Dimension{Kind: css.Number, Value: 1}
		st.MinLineHeight = p.lineVal
	case lineSpacing:
		// Writer adds the leading below the glyphs of each line, and once
		// more above the paragraph's first line (measured: two lines of
		// 12pt Liberation Serif with a 0.5 cm leading take 2 x 28 pt plus
		// 14.2 pt).
		lead := max(p.lineVal, 0)
		st.LineRule = css.LineRuleMultiple
		st.LineHeight = css.Dimension{Kind: css.Number, Value: 1 + lead/math.Max(b.lineRatio(t)*t.size, 1)}
		st.Padding[css.Top] = css.Pts(p.padding[css.Top] + lead)
	}
}

// textAligns maps fo:text-align (ODF 1.3, Part 3, 20.216; XSL 1.0, 7.16.9)
// to css.
var textAligns = map[string]css.TextAlign{
	"start": css.AlignStart, "end": css.AlignEnd, "left": css.AlignLeft,
	"right": css.AlignRight, "center": css.AlignCenter, "justify": css.AlignJustify,
}

// applyPara sets the paragraph properties p gives on the block st, whose
// text properties t are already set.
func (b *builder) applyPara(st *css.Style, p paraState, t textState) {
	st.Display = css.Block
	for i := range st.Margin {
		st.Margin[i] = css.Pts(p.margin[i])
		st.Padding[i] = css.Pts(p.padding[i])
	}
	st.Border = p.border
	st.TextIndent = css.Pts(p.indent)
	if a, ok := textAligns[p.align]; ok {
		st.TextAlign = a
	}
	st.TextAlignLast = css.AlignAuto
	if st.TextAlign == css.AlignJustify {
		// fo:text-align-last applies to a justified paragraph alone; its
		// last line is start-aligned otherwise (20.217).
		switch p.alignLast {
		case "justify":
			st.TextAlignLast = css.AlignJustify
		case "center":
			st.TextAlignLast = css.AlignCenter
		default:
			st.TextAlignLast = css.AlignStart
		}
	}
	b.lineRule(st, p, t)
	if p.keepNext {
		st.BreakAfter = css.BreakAvoid
	}
	if p.keepTogether {
		st.BreakInside = css.InsideAvoid
	}
	st.Widows, st.Orphans = max(p.widows, 1), max(p.orphans, 1)
	st.BreakBefore = pageBreak(p.breakBefore, st.BreakBefore)
	if br := pageBreak(p.breakAfter, css.BreakAuto); br != css.BreakAuto {
		st.BreakAfter = br
	}
	st.BackgroundColor = p.background
	if p.bgImage != nil {
		b.backgroundImage(st, p.bgImage)
	}
	st.BoxShadow = p.shadow
	if hasBorder(st.Border) {
		// Writer draws a bordered paragraph whole on each page it spans:
		// the bottom border before the cut, the top one after it.
		st.BoxDecorationBreak = css.DecorationCloneAll
	}
	st.TabStops = b.tabStops(p.tabs, p.margin[css.Left])
	if p.tabDist > 0 {
		st.TabSize = css.Pts(p.tabDist)
	}
	if strings.HasPrefix(p.writing, "rl") {
		st.Direction = css.RTL
	}
}

// pageBreak maps fo:break-before and fo:break-after (ODF 1.3, Part 3,
// 20.191 and 20.192): page breaks the page, column the column; even-page
// and odd-page (ODF 1.3) break to a left or right page.
func pageBreak(v string, cur css.Break) css.Break {
	switch v {
	case "page":
		return css.Page
	case "column":
		return css.Column
	case "even-page":
		return css.LeftPage
	case "odd-page":
		return css.RightPage
	}
	return cur
}

// tabTypes maps style:type of a style:tab-stop (ODF 1.3, Part 3,
// 19.502) to its alignment.
var tabTypes = map[string]css.TabAlign{"left": css.TabLeft, "center": css.TabCenter, "right": css.TabRight, "char": css.TabDecimal}

// tabStops reads a style:tab-stops element (ODF 1.3, Part 3, 17.8) of a
// paragraph whose start indent is indent: the stops in increasing
// position, measured from the containing block's content box. Writer
// counts the positions from the paragraph's indent when the document sets
// TabsRelativeToIndent, its default, and from the page area's edge
// otherwise. The slice is never nil: a paragraph lays its tabs out the
// Office way, its custom stops first, then the multiples of TabSize.
func (b *builder) tabStops(ts *odf.Element, indent float64) []css.TabStop {
	out := []css.TabStop{}
	if b.tabsRelative {
		indent = max(indent, 0)
	} else {
		indent = 0
	}
	for _, t := range ts.All(odf.NSStyle, "tab-stop") {
		pos, ok := odf.Length(t.A(odf.NSStyle, "position"))
		if !ok || pos < 0 || len(out) >= maxTabStops {
			continue
		}
		stop := css.TabStop{Pos: pos + indent, Align: tabTypes[t.A(odf.NSStyle, "type")]}
		switch t.A(odf.NSStyle, "leader-text") {
		case ".", "…":
			stop.Leader = css.TabLeaderDots
		case "-":
			stop.Leader = css.TabLeaderHyphens
		case "_":
			stop.Leader = css.TabLeaderUnderscore
		case "·":
			stop.Leader = css.TabLeaderMiddleDots
		case "", " ":
			if t.A(odf.NSStyle, "leader-text") == " " || t.A(odf.NSStyle, "leader-style") == "none" {
				break
			}
			switch t.A(odf.NSStyle, "leader-style") {
			case "dotted":
				stop.Leader = css.TabLeaderDots
			case "dash", "long-dash", "dot-dash", "dot-dot-dash":
				stop.Leader = css.TabLeaderHyphens
			case "solid":
				stop.Leader = css.TabLeaderUnderscore
			}
		default:
			// A leader character the contract has no value for fills
			// with the nearest: dots.
			stop.Leader = css.TabLeaderDots
			b.warn("tab-leader", "a tab leader character other than . - _ · is drawn as dots")
		}
		out = append(out, stop)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Pos < out[j-1].Pos; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// maxTabStops bounds the tab stops of one paragraph: 64. Anti-DoS guard,
// not spec-mandated: Writer's ruler holds a few dozen, and the layout
// scans them per tab.
const maxTabStops = 64

// backgroundImage sets the background picture a style:background-image
// names on st (ODF 1.3, Part 3, 17.3): its repeat and position.
func (b *builder) backgroundImage(st *css.Style, bi *odf.Element) {
	href := bi.A(odf.NSXLink, "href")
	if href == "" {
		if bi.Child(odf.NSOffice, "binary-data") != nil {
			b.warn("background-binary", "an inline background picture is not drawn")
		}
		return
	}
	st.BackgroundImage = pictureURL(href)
	switch bi.A(odf.NSStyle, "repeat") {
	case "no-repeat":
		st.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
	case "stretch":
		st.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
		st.BackgroundSize = css.BackgroundSize{Kind: css.SizeExplicit, Width: css.Dimension{Kind: css.Percent, Value: 1}, Height: css.Dimension{Kind: css.Percent, Value: 1}}
	}
	if pos := strings.Fields(bi.A(odf.NSStyle, "position")); len(pos) > 0 {
		at := map[string]float64{"left": 0, "top": 0, "center": 0.5, "right": 1, "bottom": 1}
		for _, p := range pos {
			v, ok := at[p]
			if !ok {
				continue
			}
			switch p {
			case "top", "bottom":
				st.BackgroundPosition[1] = css.Dimension{Kind: css.Percent, Value: v}
			case "left", "right":
				st.BackgroundPosition[0] = css.Dimension{Kind: css.Percent, Value: v}
			default:
				st.BackgroundPosition = [2]css.Dimension{{Kind: css.Percent, Value: v}, {Kind: css.Percent, Value: v}}
			}
		}
	}
}

// textWidth returns the advance of s set in st's font, 0 without fonts.
func (b *builder) textWidth(s string, st *css.Style) float64 {
	if b.fonts == nil || s == "" {
		return 0
	}
	ts := text.Style{Families: text.BrowserFamilies(st.FontFamilies), Weight: st.FontWeight, Italic: st.FontStyle != css.FontNormal, Size: st.FontSize}
	p, _, err := b.fonts.Shape(b.ctx, []text.ParagraphRun{{Text: s, Style: ts}}, text.ParagraphOptions{})
	if err != nil || p.Len() == 0 {
		return 0
	}
	return p.Line(0, p.Len(), false).Width
}
