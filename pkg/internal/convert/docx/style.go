package docx

import (
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// defaultFamily is the typeface of text whose properties name none: Word
// and LibreOffice fall back on Times New Roman (ECMA-376 Part 1,
// 17.7.5.1 leaves the default to the application).
const defaultFamily = "Times New Roman"

// fallbackFamily ends every family list: the face fontconfig substitutes
// for an unknown family where LibreOffice runs Gotenberg, the first of
// 60-latin.conf's sans-serif preference list.
const fallbackFamily = "DejaVu Sans"

// defaultFontSize is the font size of text whose properties set none, 10
// points (ECMA-376 Part 1, 17.3.2.38: sz absent is 20 half-points).
const defaultFontSize = 10

// blockStyle returns a fresh style of a block box with the document's text
// defaults: preserved spaces that wrap, as Word lays text out, and the
// default tab interval.
func (d *document) blockStyle() *css.Style {
	st := css.Initial()
	st.Display = css.Block
	st.WhiteSpace = css.PreWrap
	st.FontFamilies = []string{defaultFamily, fallbackFamily}
	st.FontSize = defaultFontSize
	st.TabSize = css.Pts(d.settings.defaultTab)
	st.OverflowWrap = css.BreakWord
	return st
}

// families returns the font family list of run properties: the ASCII,
// high ANSI, East Asian and complex script typefaces in that order, so
// the text stack's per-character fallback picks the East Asian face for
// CJK text, then the fallback family.
func families(r runProps) []string {
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
	add(r.fonts.eastAsia)
	add(r.fonts.cs)
	if len(out) == 0 {
		add(defaultFamily)
	}
	add(fallbackFamily)
	return out
}

// applyRun sets the character properties of r on st (ECMA-376 Part 1,
// 17.3.2), which already holds the paragraph's inherited properties.
// lineMult is the paragraph's proportional line spacing, 0 for none.
func (d *document) applyRun(st *css.Style, r runProps, lineMult float64) {
	st.FontFamilies = families(r)
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
	} else if bg := runBackground(r); bg != nil && luminance(*bg) < 0.4 {
		// Automatic colour turns white on a dark background (17.3.2.6).
		st.Color = css.Color{R: 1, G: 1, B: 1, A: 1}
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
		case strings.HasPrefix(u.style, "dotted") || strings.HasPrefix(u.style, "dottedHeavy"):
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
	}
	st.LetterSpacing = css.Dimension{Kind: css.Normal}
	if r.spacing != nil && *r.spacing != 0 {
		st.LetterSpacing = css.Pts(*r.spacing)
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
	st.TextShadow = nil
	if boolOf(r.shadow) || boolOf(r.emboss) || boolOf(r.imprint) {
		// An approximation: Word's shadow is a grey copy offset down and
		// right; emboss and imprint are drawn as one too.
		off := max(st.FontSize/20, 0.5)
		st.TextShadow = []css.Shadow{{X: off, Y: off, Color: css.Color{R: 0.6, G: 0.6, B: 0.6, A: 1}}}
	}
	if lineMult > 0 {
		st.LineHeight = css.Dimension{Kind: css.Number, Value: lineMult * d.lineFactor(st)}
	}
}

// runBackground returns the background of a run: its highlight, else its
// shading (17.3.2.15, 17.3.2.32).
func runBackground(r runProps) *css.Color {
	if r.highlight != nil {
		return r.highlight
	}
	return r.shading
}

// luminance returns the relative luminance of c, roughly.
func luminance(c css.Color) float64 { return 0.299*c.R + 0.587*c.G + 0.114*c.B }

// lineFactor returns the normal line height of st's first available face,
// ascent plus descent plus line gap, as a factor of the font size: what a
// proportional line spacing multiplies (17.3.1.33, lineRule auto).
func (d *document) lineFactor(st *css.Style) float64 {
	key := strings.Join(st.FontFamilies, ",")
	if st.FontWeight >= 600 {
		key += ",b"
	}
	if st.FontStyle != css.FontNormal {
		key += ",i"
	}
	if f, ok := d.lineFactors[key]; ok {
		return f
	}
	f := 1.15
	if d.fonts != nil {
		const size = 100
		fc := d.fonts.Match(text.Style{Families: text.BrowserFamilies(st.FontFamilies), Weight: st.FontWeight, Italic: st.FontStyle != css.FontNormal, Size: size})
		if fc != nil {
			m := fc.Metrics(size)
			if v := (m.Ascent + m.Descent + m.LineGap) / size; v > 0.5 && v < 5 {
				f = v
			}
		}
	}
	d.lineFactors[key] = f
	return f
}

// inlineStyle returns a copy of the paragraph style ps as an inline box,
// for a run.
func inlineStyle(ps *css.Style) *css.Style {
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
	return &st
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
