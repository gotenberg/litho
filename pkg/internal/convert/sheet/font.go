package sheet

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The font of a cell no style names one for: Calc's default cell style,
// Liberation Sans 10 points, black.
const (
	defaultFamily   = "Liberation Sans"
	defaultFontSize = 10
)

// maxFontSize caps a cell's font size, in points. Anti-DoS guard, not
// spec-mandated: Excel stops at 409 points.
const maxFontSize = 1638

// font is a resolved cell font.
type font struct {
	families []string
	size     float64
	bold     bool
	italic   bool
	under    Underline
	strike   bool
	color    css.Color
	script   Script
}

// zeroFont reports a font that names nothing, which takes the default.
func zeroFont(f Font) bool {
	return len(f.Families) == 0 && f.Size == 0 && !f.Bold && !f.Italic && f.Underline == NoUnderline &&
		!f.Strike && f.Color.A == 0 && f.Script == Baseline
}

// resolveFont returns f with what it leaves zero taken from def, then
// from Calc's defaults.
func resolveFont(f, def Font) font {
	if zeroFont(f) {
		f = def
	}
	out := font{
		families: f.Families, size: f.Size, bold: f.Bold, italic: f.Italic, under: f.Underline,
		strike: f.Strike, color: f.Color, script: f.Script,
	}
	if len(out.families) == 0 {
		out.families = def.Families
	}
	if len(out.families) == 0 {
		out.families = []string{defaultFamily}
	}
	if !(out.size > 0) {
		out.size = def.Size
	}
	if !(out.size > 0) {
		out.size = defaultFontSize
	}
	out.size = min(out.size, maxFontSize)
	if out.color.A == 0 {
		out.color = def.Color
	}
	if out.color.A == 0 {
		out.color = css.Color{A: 1}
	}
	return out
}

// overlay returns base with the run font r applied: what r names
// replaces base's.
func overlay(base font, r *Font) font {
	if r == nil {
		return base
	}
	out := base
	if len(r.Families) > 0 {
		out.families = r.Families
	}
	if r.Size > 0 {
		out.size = min(r.Size, maxFontSize)
	}
	out.bold, out.italic, out.under, out.strike, out.script = r.Bold, r.Italic, r.Underline, r.Strike, r.Script
	if r.Color.A > 0 {
		out.color = r.Color
	}
	return out
}

// scriptScale and the offsets are how Calc draws a superscript and a
// subscript: 58% of the size, raised 33% or lowered 33% (the "Automatic"
// position of SvxEscapementItem, DFLT_ESC_PROP and DFLT_ESC_AUTO).
const scriptScale = 0.58

// fontconfigAliases are the family names fontconfig reads as its generic
// families (fontconfig's 45-latin.conf and 49-sansserif.conf), which a
// book may name and LibreOffice resolves through fontconfig.
var fontconfigAliases = map[string]string{
	"sans":      "sans-serif",
	"serif":     "serif",
	"mono":      "monospace",
	"monospace": "monospace",
}

// substituteFamily is the face LibreOffice draws a family with that no
// installed font and no metric alias serves, in the pinned image.
//
// # Corpus evidence
//
//   - xlsx: sc/tdf165180_date1904.xlsx (Aptos Narrow) and
//     sc/tdf134392.xlsx (FreeSans) print in Noto Sans.
const substituteFamily = "Noto Sans"

// cssFamilies returns f's family list with fontconfig's aliases resolved,
// then the substitute of an unknown family.
func (f font) cssFamilies() []string {
	out := make([]string, 0, len(f.families)+1)
	for _, fam := range f.families {
		if g, ok := fontconfigAliases[strings.ToLower(strings.TrimSpace(fam))]; ok {
			fam = g
		}
		out = append(out, fam)
	}
	return append(out, substituteFamily)
}

// apply sets the font of st.
func (f font) apply(st *css.Style) {
	st.FontFamilies = f.cssFamilies()
	st.FontSize = f.size
	st.FontWeight = 400
	if f.bold {
		st.FontWeight = 700
	}
	st.FontStyle = css.FontNormal
	if f.italic {
		st.FontStyle = css.FontItalic
	}
	st.Color = f.color
	st.TextDecoration = 0
	switch f.under {
	case Single, SingleAccounting:
		st.TextDecoration |= css.Underline
		st.TextDecorationStyle = css.DecorationSolid
	case Double, DoubleAccounting:
		st.TextDecoration |= css.Underline
		st.TextDecorationStyle = css.DecorationDouble
	case NoUnderline:
	}
	if f.strike {
		st.TextDecoration |= css.LineThrough
	}
	st.TextDecorationColor = f.color
	switch f.script {
	case Superscript:
		st.FontSize = f.size * scriptScale
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(f.size * 0.33)}
	case Subscript:
		st.FontSize = f.size * scriptScale
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(-f.size * 0.33)}
	case Baseline:
	}
}

// textStyle returns the text stack's style of f.
func (f font) textStyle(lang string) text.Style {
	w := 400
	if f.bold {
		w = 700
	}
	size := f.size
	if f.script != Baseline {
		size *= scriptScale
	}
	return text.Style{
		Families: text.BrowserFamilies(f.cssFamilies()),
		Weight:   w,
		Italic:   f.italic,
		Size:     size,
		Lang:     lang,
		Color:    compose.Color{R: f.color.R, G: f.color.G, B: f.color.B, A: f.color.A},
		// Calc draws cell text without the font's kerning.
		NoKerning: true,
	}
}

// measurer measures strings in cell fonts, caching the widths.
type measurer struct {
	ctx   context.Context
	fonts *text.Fonts
	lang  string
	cache map[string]float64
	// lineCache holds the line height of each font.
	lineCache map[string]float64
}

// maxMeasureCache caps the cached widths. Anti-DoS guard, not
// spec-mandated: past it widths are measured again.
const maxMeasureCache = 1 << 16

func newMeasurer(ctx context.Context, fonts *text.Fonts, lang string) *measurer {
	return &measurer{ctx: ctx, fonts: fonts, lang: lang, cache: map[string]float64{}, lineCache: map[string]float64{}}
}

// key returns the cache key of f.
func (f font) key() string {
	var b strings.Builder
	b.WriteString(strings.Join(f.families, ","))
	b.WriteByte('|')
	b.WriteString(strings.TrimRight(strings.TrimRight(formatSize(f.size), "0"), "."))
	if f.bold {
		b.WriteString("|b")
	}
	if f.italic {
		b.WriteString("|i")
	}
	if f.script != Baseline {
		b.WriteString("|s")
	}
	return b.String()
}

// width returns the advance of s set in f on one line.
func (m *measurer) width(f font, s string) float64 {
	if s == "" {
		return 0
	}
	k := f.key() + "\x00" + s
	if w, ok := m.cache[k]; ok {
		return w
	}
	w := m.runsWidth([]text.ParagraphRun{{Text: s, Style: f.textStyle(m.lang)}})
	if len(m.cache) < maxMeasureCache {
		m.cache[k] = w
	}
	return w
}

// runsWidth returns the widest line of the runs.
func (m *measurer) runsWidth(runs []text.ParagraphRun) float64 {
	p, _, err := m.fonts.Shape(m.ctx, runs, text.ParagraphOptions{})
	if err != nil {
		return 0
	}
	return p.MaxContent()
}

// textLineHeight returns the height of a line of the text s in the font
// f: the font's own, or the tallest of the faces the text falls back to
// for the characters the font lacks, as Calc's edit engine sets a line to
// its tallest portion.
//
// # Corpus evidence
//
//   - xlsx: sc/tdf130104_indent.xlsx, rows of Calibri 10 point text and
//     Chinese text without a height of their own, prints them 15.65
//     points apart, the line of Noto Sans CJK's 14.5 points and the
//     padding, where its Latin rows are 12.75.
func (m *measurer) textLineHeight(f font, s string) float64 {
	h := m.lineHeight(f)
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return h
	}
	k := f.key() + "\x00h\x00" + s
	if v, ok := m.cache[k]; ok {
		return v
	}
	p, _, err := m.fonts.Shape(m.ctx, []text.ParagraphRun{{Text: s, Style: f.textStyle(m.lang)}}, text.ParagraphOptions{})
	if err == nil {
		for _, sp := range p.Line(0, p.Len(), false).Spans {
			h = max(h, sp.Ascent+sp.Descent)
		}
	}
	if len(m.cache) < maxMeasureCache {
		m.cache[k] = h
	}
	return h
}

// lineHeight returns the height of a line of f: its face's ascent plus
// descent, as Calc sizes a cell's text line.
func (m *measurer) lineHeight(f font) float64 {
	k := f.key()
	if h, ok := m.lineCache[k]; ok {
		return h
	}
	ts := f.textStyle(m.lang)
	h := ts.Size * 1.2
	if fc := m.fonts.Match(ts); fc != nil {
		mt := fc.Metrics(ts.Size)
		if a := mt.Ascent + mt.Descent; a > 0 {
			h = a
		}
	}
	if len(m.lineCache) < maxMeasureCache {
		m.lineCache[k] = h
	}
	return h
}

// formatSize writes a font size for a cache key.
func formatSize(v float64) string {
	return strconvFloat(v)
}
