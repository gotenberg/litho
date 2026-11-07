package odt

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// baseStyle returns the style every node starts from: CSS's initial
// values with the white space the converter already processed kept as it
// is (pre-wrap: ODF 1.3, Part 3, 6.1.2 collapses white space at import,
// and the converter does it), and orphans and widows off, LibreOffice's
// default when a style sets neither.
func baseStyle() *css.Style {
	st := css.Initial()
	st.WhiteSpace = css.PreWrap
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

// lineRatio returns the height of a line of the face t selects in
// LibreOffice's measure, ascent, descent and line gap, over its font size;
// 1.15, Liberation Serif's, without fonts.
func (b *builder) lineRatio(t textState) float64 {
	const fallback = 1.15
	if b.fonts == nil {
		return fallback
	}
	key := strings.Join(t.families(), ",") + "|" + strings.Repeat("b", t.weight/100) + map[bool]string{true: "i"}[t.italic]
	if r, ok := b.ratios[key]; ok {
		return r
	}
	const size = 100
	fc := b.fonts.Match(text.Style{Families: text.BrowserFamilies(t.families()), Weight: max(t.weight, 100), Italic: t.italic, Size: size})
	r := fallback
	if fc != nil {
		m := fc.Metrics(size)
		if s := (m.Ascent + m.Descent + m.LineGap) / size; s > 0 && s < 10 {
			r = s
		}
	}
	b.ratios[key] = r
	return r
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
	switch p.line { //nolint:exhaustive // lineNormal keeps the initial line-height
	case lineProportional:
		st.LineHeight = css.Dimension{Kind: css.Number, Value: p.lineVal * b.lineRatio(t)}
	case lineFixed:
		st.LineHeight = css.Pts(p.lineVal)
	case lineAtLeast:
		st.MinLineHeight = p.lineVal
	case lineSpacing:
		st.LineHeight = css.Dimension{Kind: css.Number, Value: b.lineRatio(t) + p.lineVal/math.Max(t.size, 1)}
	}
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
	st.TabStops = b.tabStops(p.tabs)
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

// tabStops reads a style:tab-stops element (ODF 1.3, Part 3, 17.8): the
// stops in increasing position, measured from the paragraph's left margin
// as Writer does for a document that sets TabsRelativeToIndent, its
// default, or from the page area's left edge otherwise.
func (b *builder) tabStops(ts *odf.Element) []css.TabStop {
	var out []css.TabStop
	for _, t := range ts.All(odf.NSStyle, "tab-stop") {
		pos, ok := odf.Length(t.A(odf.NSStyle, "position"))
		if !ok || pos < 0 || len(out) >= maxTabStops {
			continue
		}
		stop := css.TabStop{Pos: pos, Align: tabTypes[t.A(odf.NSStyle, "type")]}
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
			switch t.A(odf.NSStyle, "leader-style") {
			case "dotted":
				stop.Leader = css.TabLeaderDots
			case "dash", "long-dash", "dot-dash", "dot-dot-dash":
				stop.Leader = css.TabLeaderHyphens
			case "solid":
				stop.Leader = css.TabLeaderUnderscore
			}
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
