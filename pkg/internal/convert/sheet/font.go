package sheet

import (
	"context"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	// excel sets a superscript or subscript as Excel does
	// ([font.scriptShape]).
	excel bool
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
	if !numeric.Positive(out.size) {
		out.size = def.Size
	}
	if !numeric.Positive(out.size) {
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

// scriptShape returns how f draws a superscript or subscript: the share
// of its size it is set at and the share of its size its baseline moves
// up, negative for down. Under Excel's rules a script is set at two
// thirds of the size, a superscript raised half the size and a subscript
// lowered a fifth; Calc's are [scriptScale] and a third either way.
// ECMA-376 Part 1, 18.4.14 vertAlign names the position and leaves the
// size and the shift to the application.
//
// # Corpus evidence
//
//   - litho/xlsx: fonts prints a cell font of Calibri 11.04 superscript at
//     7.32 points (61 printer pixels of 92) raised 5.5 points, and of
//     subscript lowered 2.18; rich_text prints the runs of "H2O" and "mc2"
//     at 7.32 points, lowered 2.16 and raised 5.52 points; headers_footers_codes
//     and probe, xlsx, header scripts alike.
func (f font) scriptShape() (scale, shift float64) {
	switch {
	case f.script == Baseline:
		return 1, 0
	case f.excel && f.script == Superscript:
		return 2.0 / 3, 0.5
	case f.excel:
		return 2.0 / 3, -0.2
	case f.script == Superscript:
		return scriptScale, 0.33
	}
	return scriptScale, -0.33
}

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
//   - libreoffice/xlsx: sc/tdf165180_date1904.xlsx (Aptos Narrow) and
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
	if f.script != Baseline {
		scale, shift := f.scriptShape()
		st.FontSize = f.size * scale
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(f.size * shift)}
	}
}

// excelEmoji is the font setting of cell text under Excel's rules: emoji
// drawn as text, in a face of outlines where one covers them, as
// font-variant-emoji: text draws them (CSS Fonts 4, 6.12). Shared, never
// modified.
//
// # Corpus evidence
//
//   - litho/xlsx: intl_scripts: Excel prints the emoji of a cell in Segoe
//     UI Emoji outlines, in the text colour, where Word and PowerPoint
//     print the same font's colour layers.
var excelEmoji = &text.FontSettings{Emoji: text.EmojiText}

// textStyle returns the text stack's style of f under fallback, Excel's
// rules for [text.FallbackFontconfig] (see [builder.fallback]).
func (f font) textStyle(lang string, fallback text.Fallback) text.Style {
	w := 400
	if f.bold {
		w = 700
	}
	scale, _ := f.scriptShape()
	size := f.size * scale
	ts := text.Style{
		Families: text.BrowserFamilies(f.cssFamilies(), ""),
		Weight:   w,
		Italic:   f.italic,
		Size:     size,
		Lang:     lang,
		Fallback: fallback,
		Color:    typeset.Color{R: f.color.R, G: f.color.G, B: f.color.B, A: f.color.A},
		// Calc draws cell text without the font's kerning.
		NoKerning: true,
	}
	if fallback == text.FallbackFontconfig {
		ts.Settings = excelEmoji
	}
	return ts
}

// measurer measures strings in cell fonts, caching the widths.
type measurer struct {
	ctx   context.Context
	fonts *text.Fonts
	lang  string
	// fallback is the font fallback rule cell text is measured with.
	fallback text.Fallback
	// cache holds the width and the line height of each text measured, by
	// its font's key.
	cache map[measureKey]float64
	// lineCache holds the line height of each font.
	lineCache map[string]float64
	// keys holds the key of each font measured ([font.key]), so a cell
	// does not write its font's key again.
	keys map[fontID]string
}

// measureKey is a text and the key of its font.
type measureKey struct {
	font, text string
}

// fontID tells fonts apart by what [font.key] reads, a font's families by
// the list's first element and length: two lists of one backing array and
// length are one list. Fonts apart here may share a key.
type fontID struct {
	families *string
	n        int
	size     float64
	bold     bool
	italic   bool
	script   bool
	excel    bool
}

// maxMeasureCache caps the cached widths. Anti-DoS guard, not
// spec-mandated: past it widths are measured again.
const maxMeasureCache = 1 << 16

func newMeasurer(ctx context.Context, fonts *text.Fonts, lang string) *measurer {
	return &measurer{ctx: ctx, fonts: fonts, lang: lang, cache: map[measureKey]float64{}, lineCache: map[string]float64{}, keys: map[fontID]string{}}
}

// key returns f.key(), written once for each font.
func (m *measurer) key(f font) string {
	id := fontID{n: len(f.families), size: f.size, bold: f.bold, italic: f.italic, script: f.script != Baseline, excel: f.excel}
	if len(f.families) > 0 {
		id.families = &f.families[0]
	}
	if k, ok := m.keys[id]; ok {
		return k
	}
	k := f.key()
	if len(m.keys) < maxMeasureCache {
		m.keys[id] = k
	}
	return k
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
		if f.excel {
			b.WriteString("x")
		}
	}
	return b.String()
}

// width returns the advance of s set in f on one line.
func (m *measurer) width(f font, s string) float64 {
	if s == "" {
		return 0
	}
	k := measureKey{font: m.key(f), text: s}
	if w, ok := m.cache[k]; ok {
		return w
	}
	w := m.runsWidth([]text.ParagraphRun{{Text: s, Style: f.textStyle(m.lang, m.fallback)}})
	if len(m.cache) < maxMeasureCache {
		m.cache[k] = w
	}
	return w
}

// runsWidth returns the widest line of the runs.
func (m *measurer) runsWidth(runs []text.ParagraphRun) float64 {
	p, _, err := m.fonts.Shape(m.ctx, runs, text.ParagraphOptions{})
	if err != nil {
		// A text that cannot be shaped measures nothing, as an empty one:
		// the page that draws it reports the failure, a cancelled ctx
		// included.
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
//   - libreoffice/xlsx: sc/tdf130104_indent.xlsx, rows of Calibri 10 point text and
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
	k := measureKey{font: m.key(f), text: "h\x00" + s}
	if v, ok := m.cache[k]; ok {
		return v
	}
	p, _, err := m.fonts.Shape(m.ctx, []text.ParagraphRun{{Text: s, Style: f.textStyle(m.lang, m.fallback)}}, text.ParagraphOptions{})
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
	k := m.key(f)
	if h, ok := m.lineCache[k]; ok {
		return h
	}
	ts := f.textStyle(m.lang, m.fallback)
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

// descent returns the descent of a line of f, the part of
// [measurer.lineHeight] below the baseline.
func (m *measurer) descent(f font) float64 {
	k := "d\x00" + m.key(f)
	if d, ok := m.lineCache[k]; ok {
		return d
	}
	ts := f.textStyle(m.lang, m.fallback)
	d := ts.Size * 0.2
	if fc := m.fonts.Match(ts); fc != nil {
		if mt := fc.Metrics(ts.Size); mt.Ascent+mt.Descent > 0 {
			d = mt.Descent
		}
	}
	if len(m.lineCache) < maxMeasureCache {
		m.lineCache[k] = d
	}
	return d
}

// printedLine returns the line GDI gives f on Excel's printer, in points:
// its face's ascent and descent each rounded to a printer pixel at f's
// size in whole pixels, and its line gap truncated, as the standard row
// sums them (xlsx standardRow).
func (m *measurer) printedLine(f font) float64 {
	k := "p\x00" + m.key(f)
	if h, ok := m.lineCache[k]; ok {
		return h
	}
	ts := f.textStyle(m.lang, m.fallback)
	h := m.lineHeight(f)
	if fc := m.fonts.Match(ts); fc != nil {
		mt := fc.Metrics(1)
		em := math.Round(ts.Size / excelPixel)
		if px := math.Round(float64(mt.Ascent*em)) + math.Round(float64(mt.Descent*em)) + math.Floor(float64(mt.LineGap*em)+1e-9); px > 0 {
			h = px * excelPixel
		}
	}
	if len(m.lineCache) < maxMeasureCache {
		m.lineCache[k] = h
	}
	return h
}

// screenLine returns the line GDI gives f on a screen of 96 pixels an
// inch, in pixels, with the 2 pixels of a cell's insets: its face's ascent
// and descent each rounded to a pixel at f's size in whole pixels, and its
// line gap truncated, as the standard row sums them (xlsx standardRow).
func (m *measurer) screenLine(f font) float64 {
	k := "s\x00" + m.key(f)
	if h, ok := m.lineCache[k]; ok {
		return h
	}
	ts := f.textStyle(m.lang, m.fallback)
	em := math.Round(float64(ts.Size * 96 / 72))
	h := 2 + math.Round(float64(m.lineHeight(f)*96/72))
	if fc := m.fonts.Match(ts); fc != nil {
		mt := fc.Metrics(1)
		if px := math.Round(float64(mt.Ascent*em)) + math.Round(float64(mt.Descent*em)) + math.Floor(float64(mt.LineGap*em)+1e-9); px > 0 {
			h = 2 + px
		}
	}
	if len(m.lineCache) < maxMeasureCache {
		m.lineCache[k] = h
	}
	return h
}

// lineGap returns the line gap of f's face at f's size: the external
// leading GDI adds above a line's ascent, which Excel sets above the text
// of a cell and of a header part ([pageBuilder.excelShift]).
func (m *measurer) lineGap(f font) float64 {
	k := "g\x00" + m.key(f)
	if g, ok := m.lineCache[k]; ok {
		return g
	}
	g := 0.0
	ts := f.textStyle(m.lang, m.fallback)
	if fc := m.fonts.Match(ts); fc != nil {
		g = max(fc.Metrics(ts.Size).LineGap, 0)
	}
	if len(m.lineCache) < maxMeasureCache {
		m.lineCache[k] = g
	}
	return g
}

// formatSize writes a font size for a cache key.
func formatSize(v float64) string {
	return strconvFloat(v)
}
