package sheet

import (
	"context"
	"strings"
	"unicode/utf8"

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
	out := font{families: f.Families, size: f.Size, bold: f.Bold, italic: f.Italic, under: f.Underline,
		strike: f.Strike, color: f.Color, script: f.Script}
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

// apply sets the font of st.
func (f font) apply(st *css.Style) {
	st.FontFamilies = append([]string(nil), f.families...)
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
		Families: text.BrowserFamilies(f.families),
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

// visible returns the characters of s, set in f from x, whose advance
// reaches into [x0, x1], and the x of the first kept: LibreOffice leaves
// the glyphs a cell's clip hides wholly out of the PDF it writes.
func (m *measurer) visible(f font, s string, x, x0, x1 float64) (string, float64) {
	p, _, err := m.fonts.Shape(m.ctx, []text.ParagraphRun{{Text: s, Style: f.textStyle(m.lang)}}, text.ParagraphOptions{Direction: text.LeftToRight})
	if err != nil || p.Len() != utf8.RuneCountInString(s) {
		return s, x
	}
	rs := []rune(s)
	first, last := -1, -1
	firstX := x
	pos := x
	for i := range rs {
		a := p.Advance(i)
		if pos+a > x0+epsilon && pos < x1-epsilon {
			if first < 0 {
				first, firstX = i, pos
			}
			last = i
		}
		pos += a
	}
	if first < 0 {
		return "", x
	}
	return string(rs[first : last+1]), firstX
}
