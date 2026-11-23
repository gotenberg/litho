package docx

import (
	"math"
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// defaultFamily is the typeface of text whose properties name none: Word
// and LibreOffice fall back on Times New Roman (ECMA-376 Part 1,
// 17.7.5.1 leaves the default to the application).
const defaultFamily = "Times New Roman"

// defaultFontSize is the font size of text whose properties set none, 10
// points (ECMA-376 Part 1, 17.3.2.38: sz absent is 20 half-points).
const defaultFontSize = 10

// blockStyle returns a fresh style of a block box with the document's text
// defaults: preserved spaces that wrap, as Word lays text out, and the
// default tab interval.
func (d *document) blockStyle() *css.Style {
	st := d.blockOf()
	return &st
}

// blockOf is [document.blockStyle] as a value, for a style built on the
// stack and then shared ([document.share]).
func (d *document) blockOf() css.Style {
	st := css.InitialValue()
	st.Display = css.Block
	st.WhiteSpace = css.PreWrap
	st.FontFamilies = []string{defaultFamily, fallbackSerif, secondFallback[fallbackSerif]}
	st.FontSize = defaultFontSize
	st.TabSize = css.Pts(d.settings.defaultTab)
	st.OverflowWrap = css.BreakWord
	// Word sizes a line from its runs and keeps a paragraph's widows and
	// orphans its own way.
	st.WordRules = max(d.settings.compat, earliestCompat)
	if d.settings.compat >= 15 {
		// Word shrinks the spaces of justified lines in Word 2013's
		// compatibility mode and later.
		st.JustifyShrink = justifyShrink
	}
	return st
}

// earliestCompat is the compatibility mode of a document that names none:
// Word 2007's, the earliest MS-DOCX, 2.3.4 compatibilityMode defines.
const earliestCompat = 12

// justifyShrink is the share of a space Word lets a justified line of a
// Word 2013 document shrink to take one more word (MS-DOCX, 2.3.4
// compatibilityMode 15), a measurement of Word: no space of a justified
// line it prints narrows by more than about a fifth.
//
// # Corpus evidence
//
//   - litho/docx paper-*: the narrowest space of 345 justified Calibri 11
//     point lines Word prints is 78.6% of its natural width, and Word
//     takes no word that needs its spaces narrower than 78%.
const justifyShrink = 0.22

// families returns the font family list of run properties: the ASCII
// and high ANSI typefaces, the fallback face of the first one's class,
// then the East Asian and complex script typefaces, so the text stack's
// per-character fallback picks the East Asian face for CJK text. Latin
// text in a missing face takes its class's fallback, as LibreOffice
// substitutes it, never the East Asian face.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/fdo73550.docx: Segoe UI, a roman face
//     in the font table, prints in a serif where its East Asian Calibri is
//     installed.
func (d *document) families(r runProps) []string {
	var out []string
	add := func(f string) {
		f = strings.TrimSpace(f)
		if f == "" {
			return
		}
		for _, o := range out {
			if strings.EqualFold(o, f) {
				return
			}
		}
		out = append(out, f)
	}
	add(r.fonts.ascii)
	add(r.fonts.hAnsi)
	if len(out) > 0 {
		for _, f := range d.fallbackFor(out[0]) {
			add(f)
		}
	}
	add(r.fonts.eastAsia)
	add(r.fonts.cs)
	if len(out) == 0 {
		add(defaultFamily)
	}
	for _, f := range d.fallbackFor(out[0]) {
		add(f)
	}
	return out
}

// runLigatures is the font-variant-ligatures of every run: no common
// ligatures, as Word prints none, whatever w14:ligatures ([MS-DOCX],
// CT_Ligatures), which names the ligatures a run asks for, says: Word
// leaves the font's standard ligatures out at standard and
// standardContextual too, where LibreOffice draws them at every value,
// none included.
//
// # Corpus evidence
//
//   - litho/docx, libreoffice/docx, poi/docx, openxmlsdk/docx: none of
//     the 870 Word references draws a ligature glyph (no ToUnicode entry
//     of two letters or more), 185 of their documents holding "fi" or
//     "fl" without w14:ligatures; litho/docx run-w14-effects and
//     libreoffice/docx ooxmlexport/TextEffects_Props3d_Ligatures_NumForm_NumSpacing.docx
//     set standard ligatures and print "fi" as two glyphs.
//   - probe, docx: Word draws "office affinity" in Calibri with
//     w14:ligatures standard as separate f, f and i glyphs; LibreOffice
//     of gotenberg/gotenberg:9.4.1 draws the ffi and fl ligatures of
//     Carlito at none, standard, all and without the element.
const runLigatures = css.LigaturesCommonOff

// applyRun sets the character properties of r on st (ECMA-376 Part 1,
// 17.3.2), which already holds the paragraph's inherited properties.
// lineMult is the paragraph's proportional line spacing, 0 for none.
func (d *document) applyRun(st *css.Style, r runProps, lineMult float64) {
	st.FontFamilies = d.families(r)
	st.FontSize = defaultFontSize
	if r.size != nil {
		st.FontSize = *r.size
	}
	st.FontWeight = 400
	if boolOf(r.bold) {
		st.FontWeight = 700
	}
	st.FontStyle = css.FontNormal
	if boolOf(r.italic) {
		st.FontStyle = css.FontItalic
	}
	st.Color = css.Color{A: 1}
	if r.color != nil && !r.color.auto {
		st.Color = r.color.c
	} else if bg := d.autoTextBackground(r); bg != nil && luminance(*bg) < autoWhiteBelow {
		// Automatic colour turns white on a dark background (17.3.2.6).
		st.Color = css.Color{R: 1, G: 1, B: 1, A: 1}
	} else if d.boxText != nil {
		st.Color = *d.boxText
	}
	st.TextDecoration = 0
	st.TextDecorationStyle = css.DecorationSolid
	st.TextDecorationColor = st.Color
	st.TextDecorationThickness = css.Dimension{Kind: css.Auto}
	if u := r.underline; u != nil && u.style != "none" && u.style != "" {
		st.TextDecoration |= css.Underline
		switch {
		case strings.HasPrefix(u.style, "double"):
			st.TextDecorationStyle = css.DecorationDouble
		case strings.HasPrefix(u.style, "dotted"):
			st.TextDecorationStyle = css.DecorationDotted
		case strings.Contains(strings.ToLower(u.style), "dash"):
			st.TextDecorationStyle = css.DecorationDashed
		case strings.HasPrefix(strings.ToLower(u.style), "wav"):
			st.TextDecorationStyle = css.DecorationWavy
		}
		if u.style == "thick" || strings.HasSuffix(u.style, "Heavy") {
			st.TextDecorationThickness = css.Pts(st.FontSize / 10)
		}
		if u.color != nil && !u.color.auto {
			st.TextDecorationColor = u.color.c
		}
	}
	if boolOf(r.strike) || boolOf(r.dstrike) {
		st.TextDecoration |= css.LineThrough
		if boolOf(r.dstrike) && st.TextDecoration&css.Underline == 0 {
			st.TextDecorationStyle = css.DecorationDouble
		}
	}
	st.BackgroundColor = css.Transparent
	if bg := runBackground(r); bg != nil {
		st.BackgroundColor = *bg
	}
	st.TextTransform = css.TransformNone
	if boolOf(r.caps) {
		st.TextTransform = css.Uppercase
	}
	st.FontVariant = css.VariantNormal
	if boolOf(r.smallCaps) && !boolOf(r.caps) {
		st.FontVariant = css.SmallCaps
		st.WordSmallCaps = true
	}
	st.FontVariantLigatures = runLigatures
	st.LetterSpacing = css.Dimension{Kind: css.Normal}
	if r.spacing != nil && *r.spacing != 0 {
		st.LetterSpacing = css.Pts(*r.spacing)
	}
	// LibreOffice kerns a run only when its kern property is set and its
	// size reaches it (17.3.2.19), the document default included: every
	// other run is set without the font's kerning.
	st.FontKerning = css.KerningNone
	if r.kern != nil && *r.kern > 0 && st.FontSize >= *r.kern {
		st.FontKerning = css.KerningNormal
	}
	st.TextScale = 0
	if r.scale != nil && *r.scale != 1 {
		st.TextScale = *r.scale
	}
	st.VerticalAlign = css.VerticalAlign{}
	if r.vertAlign != nil {
		switch *r.vertAlign {
		case "superscript":
			// LibreOffice sets superscript and subscript at 58% of the size,
			// raised by 33% and lowered by 8%.
			st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(st.FontSize * 0.33)}
			st.FontSize *= 0.58
		case "subscript":
			st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(-st.FontSize * 0.08)}
			st.FontSize *= 0.58
		}
	}
	if r.position != nil && *r.position != 0 {
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(*r.position)}
	}
	st.TextShadow = legacyShadows(r)
	st.TextEffects = nil
	if r.glow != nil || r.textShadow != nil || r.reflection != nil {
		st.TextEffects = &css.OfficeEffects{Glow: r.glow, Shadow: r.textShadow, Reflection: r.reflection}
	}
	if r.textSolid != nil {
		st.Color = *r.textSolid
	}
	st.TextPaint = r.textPaint
	if st.TextPaint == nil && boolOf(r.outline) {
		st.TextPaint = &css.OfficeTextPaint{NoFill: true, Outline: &css.OfficeOutline{Color: st.Color, Width: legacyOutline}}
	}
	// Word draws emoji in Segoe UI Emoji, at its advances and line, and
	// breaks no line between two of them or an emoji and a letter.
	st.OfficeEmoji, st.KeepEmoji = true, true
	st.FontVariantEmoji = css.EmojiNormal
	if monochromeEmoji(r) {
		st.FontVariantEmoji = css.EmojiText
	}
	if lineMult > 0 {
		st.LineHeight = css.Pts(d.proportionalLine(st, lineMult))
	}
}

// monochromeEmoji reports whether Word draws the emoji of a run of the
// properties r as text, one glyph in the run's colour, where it draws
// those of other runs in colour: a run whose resolved colour is stated
// other than auto (ECMA-376 Part 1, 17.3.2.6 color), directly or by its
// character or paragraph style, or a run shaded with a fill other than
// auto (17.3.2.32 shd). A highlight, a paragraph's shading and a run
// shading of fill auto keep the colour; bold changes nothing. litho draws
// such an emoji in the text presentation (CSS Fonts 4, 6.12,
// font-variant-emoji: text), a variation selector 16 included (see
// text.Style.OfficeEmoji): in a face that holds its outline, else, as no
// face of the image does for most emoji, in colour.
//
// # Corpus evidence
//
//   - probe, docx: Word draws the smile and the party popper as one
//     Segoe UI Emoji glyph each in the run's colour in runs of w:color
//     C00000, 000000, 4472C4 with themeColor accent1 and FFFFFF over a
//     shading, and of a w:shd of fill 3060C0 without a colour; in colour
//     layers in runs of w:color auto and of w:highlight yellow.
//   - probe, docx: in the run's colour as one glyph in a run of a
//     character style of w:color C00000, in a run of no colour in a
//     paragraph of a style of w:color C00000, in a run of the Hyperlink
//     style (0563C1), in a bold run of w:color C00000 (stroked, render
//     mode 2), and U+2764 and U+263A followed by U+FE0F in a run of
//     w:color C00000; in colour layers in a run of no colour in a
//     paragraph shaded 3060C0 and in a run of w:shd fill auto.
func monochromeEmoji(r runProps) bool {
	return r.color != nil && !r.color.auto || r.shading != nil
}

// runBackground returns the background of a run: its highlight, else its
// shading (17.3.2.15, 17.3.2.32).
func runBackground(r runProps) *css.Color {
	if r.highlight != nil {
		return r.highlight
	}
	return r.shading
}

// autoTextBackground returns the background that decides the colour of
// a run's automatic text (ECMA-376 Part 1, 17.3.2.6 color, auto): the
// run's shading, else the paragraph's, else the table cell's; none for a
// highlighted run, whose automatic text Word paints black on every
// highlight colour.
//
// # Corpus evidence
//
//   - probe, docx: Word paints automatic text white on a paragraph shaded
//     202020 and on a cell shaded 202020, black on a paragraph shaded
//     404040 and on cells shaded 404040 and 606060; black on the
//     highlights black, darkBlue, darkRed, darkGray, blue and red.
func (d *document) autoTextBackground(r runProps) *css.Color {
	switch {
	case r.highlight != nil:
		return nil
	case r.shading != nil:
		return r.shading
	}
	return d.under
}

// autoWhiteBelow is the [luminance] of a run's background below which
// Word paints the run's automatic text white (ECMA-376 Part 1, 17.3.2.6
// leaves the choice to the consumer). Word's switch lies between the
// lightest background it was measured to paint white text on and the
// darkest it paints black text on; the bound is a quarter, between them.
//
// # Corpus evidence
//
//   - probe, docx: Word paints automatic text white on runs shaded
//     101010, 202020, 303030 and 383838 (0.220), 800000, 000080, 1F3864
//     and C00000 (0.225), black on runs shaded 404040 (0.251), 484848,
//     505050, 585858, 008000 (0.295), 0070C0 and 7030A0, and on a run
//     shaded pct50 of 000000 over FFFFFF.
//   - probe, docx: Word paints automatic text white on a run shaded
//     202020 (0.125), black on runs shaded 606060 (0.376), 808080 and
//     A0A0A0, and on a run and a paragraph shaded 3060C0 (0.363).
//   - litho/docx run-highlight-shading: Word paints automatic text black
//     on a solid shading of 2F5496 (0.316) and on its pct75 and pct90
//     mixes with FFF2CC, and on highlights yellow, green, cyan, magenta
//     (0.413) and light grey.
//   - litho/docx styles-custom-theme: Word paints the automatic text of
//     a paragraph shaded accent6 under themeFillShade BF white; it paints
//     the shading 33,57,87 (0.209), the theme colour, over the 2C4D75
//     stored beside it.
const autoWhiteBelow = 0.25

// luminance returns the relative luminance of c, roughly.
func luminance(c css.Color) float64 {
	return float64(0.299*c.R) + float64(0.587*c.G) + float64(0.114*c.B)
}

// proportionalLine returns the height of a line of text in st under a
// proportional line spacing mult (17.3.1.33, lineRule auto): the face's
// ascent, descent and line gap each rounded to twips, their sum times mult
// truncated to twips, as LibreOffice computes it.
func (d *document) proportionalLine(st *css.Style, mult float64) float64 {
	a, de, g := d.lineMetrics(st)
	twips := math.Round(a*st.FontSize*20) + math.Round(de*st.FontSize*20) + math.Round(g*st.FontSize*20)
	return math.Max(1, math.Floor(twips*mult)/20)
}

// lineMetrics returns the ascent, descent and line gap of st's first
// available face as factors of the font size.
func (d *document) lineMetrics(st *css.Style) (float64, float64, float64) {
	key := strings.Join(st.FontFamilies, ",")
	if st.FontWeight >= 600 {
		key += ",b"
	}
	if st.FontStyle != css.FontNormal {
		key += ",i"
	}
	if f, ok := d.lineFactors[key]; ok {
		return f[0], f[1], f[2]
	}
	f := [3]float64{0.9, 0.25, 0}
	if d.fonts != nil {
		const size = 100
		fc := d.fonts.Match(text.Style{Families: text.BrowserFamilies(st.FontFamilies, ""), Weight: st.FontWeight, Italic: st.FontStyle != css.FontNormal, Size: size})
		if fc != nil {
			m := fc.Metrics(size)
			if v := (m.Ascent + m.Descent + m.LineGap) / size; v > 0.5 && v < 5 {
				f = [3]float64{m.Ascent / size, m.Descent / size, m.LineGap / size}
			}
		}
	}
	d.lineFactors[key] = f
	return f[0], f[1], f[2]
}

// inlineStyle returns a copy of the paragraph style ps as an inline box,
// for a run.
func inlineStyle(ps *css.Style) *css.Style {
	st := inlineOf(ps)
	return &st
}

// inlineOf is [inlineStyle] as a value, for a style built on the stack and
// then shared ([document.share]).
func inlineOf(ps *css.Style) css.Style {
	st := *ps
	st.Display = css.Inline
	st.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	st.Padding = st.Margin
	st.Border = [4]css.Border{}
	st.BackgroundColor = css.Transparent
	st.TextIndent = css.Pts(0)
	st.Width, st.Height = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	st.BreakBefore, st.BreakAfter, st.BreakInside = css.BreakAuto, css.BreakAuto, css.InsideAuto
	st.Page = ""
	st.Float, st.Position = css.FloatNone, css.Static
	st.Columns = css.Columns{}
	return st
}

// hasCJK reports whether s holds a Han, Hiragana, Katakana or Hangul
// character.
func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

// legacyOutline is the width in points of the stroke of a run of
// w:outline (ECMA-376 Part 1, 17.3.2.23), whose glyphs Word leaves
// unfilled.
//
// # Corpus evidence
//
//   - litho/docx run-effects: Word strokes the outlines of an 18 point run
//     of w:outline 0.25 point wide in the run's colour, unfilled.
const legacyOutline = 0.25

// The grey of the legacy text effects' copies.
var (
	legacyShadowGrey = css.Color{R: 0xC0 / 255.0, G: 0xC0 / 255.0, B: 0xC0 / 255.0, A: 1}
	legacyReliefGrey = css.Color{R: 0.6, G: 0.6, B: 0.6, A: 1}
)

// legacyShadows returns the copies of the glyphs Word paints under a run
// of w:shadow, w:emboss or w:imprint (ECMA-376 Part 1, 17.3.2.31,
// 17.3.2.13, 17.3.2.18), the first painted on top, nil for none: a
// shadow is a copy in #C0C0C0 three quarters of a point right and down;
// an emboss a grey copy a point right and down under a black one a point
// left and up; an imprint the reverse. The offsets do not follow the
// font size.
//
// # Corpus evidence
//
//   - litho/docx run-effects: at 18 points Word paints the shadow's copy
//     0.75 point right and down in grey 0.753, the emboss's grey 0.6 copy
//     right and down of its black one by two points, the imprint's black
//     copy right and down of its grey one, the run between them.
//   - libreoffice/docx ooxmlexport/fdo76016: a 16 point shadow's copy is
//     0.75 point right and down in grey 0.753.
//   - openxmlsdk/docx v2FxTestFiles/wordprocessing/format/format-font2: a 7
//     point imprint's black copy is a point right and down of the white
//     run, its grey 0.6 copy a point left and up.
func legacyShadows(r runProps) []css.Shadow {
	black := css.Color{A: 1}
	switch {
	case boolOf(r.emboss):
		return []css.Shadow{{X: -1, Y: -1, Color: black}, {X: 1, Y: 1, Color: legacyReliefGrey}}
	case boolOf(r.imprint):
		return []css.Shadow{{X: -1, Y: -1, Color: legacyReliefGrey}, {X: 1, Y: 1, Color: black}}
	case boolOf(r.shadow):
		return []css.Shadow{{X: 0.75, Y: 0.75, Color: legacyShadowGrey}}
	}
	return nil
}

// runBorder draws the border b of a run (ECMA-376 Part 1, 17.3.2.4 bdr)
// around its text on every side, its space (17.18.2, the distance from
// the text) between them, or none for nil or a border of no line.
//
// # Corpus evidence
//
//   - openxmlsdk/docx O14ISOStrict/Word/bdr-Text_Border-themeColor-text1.docx
//     and rPr-bdr-Text_Border-val_dotDash_color_auto.docx: Word draws a
//     box around the run's text, a direct bdr and one of a character
//     style alike.
func runBorder(st *css.Style, b *borderSpec) {
	if b == nil || b.Style == css.BorderNone || b.Width <= 0 {
		return
	}
	for side := range st.Border {
		st.Border[side] = b.Border
		st.Padding[side] = css.Pts(b.space)
	}
}
