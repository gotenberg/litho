package text

import (
	"slices"
	"unicode"
)

// wordFace is the vertical metrics, in em, Word lays the lines of a
// Microsoft face out with.
type wordFace struct {
	ascent, descent float64
}

// wordFaces maps the family key of a Microsoft face to the vertical metrics
// Word records for it, for the faces whose metric-compatible stand-in
// ([metricAliasGroups]) records others. A stand-in that records Word's own
// metrics has no entry: Carlito for Calibri, Liberation Sans for Arial,
// Liberation Serif for Times New Roman and Liberation Mono for Courier
// New.
//
// # Corpus evidence
//
//   - litho/docx paper-*: Word's PDFs record Cambria at an ascent of 0.950
//     em and a descent of 0.222 em, a line of 1.172 em; Caladea's metrics
//     make one of 1.15 em, and each Cambria heading sits 0.4 to 0.8 point
//     off Word's.
//   - poi/docx document/heading123 and document/58067: Word spaces single
//     spaced Cambria 12 point lines 14.0 to 14.25 points apart, 14.06 on
//     average, 1.172 em.
//   - libreoffice/docx, openxmlsdk/docx, poi/docx, litho/docx: Word spaces
//     single spaced Calibri 11 point lines as Carlito's 1.2207 em, Arial
//     10 point lines as Liberation Sans's 1.15 em with its line gap, Times
//     New Roman 12 point lines as Liberation Serif's 1.15 em, Courier New
//     10 point lines 1.125 to 1.15 em apart as Liberation Mono's 1.133.
var wordFaces = map[string]wordFace{
	familyKey("Cambria"): {ascent: 0.950, descent: 0.222},
}

// wordMetrics returns the vertical metrics Word lays a line of fc out with
// at size points when style asks for them ([Style.WordMetrics]) and fc
// stands in for the Microsoft face style names: the first family of the
// style that fc is, or is a metric alias of, decides. ok is false when fc
// is the named face itself or Word records the stand-in's own metrics.
func (fc *Face) wordMetrics(style Style, size float64) (m Metrics, ok bool) {
	if !style.WordMetrics || fc == nil {
		return Metrics{}, false
	}
	if style.OfficeEmoji && fc.colourTables() != 0 {
		m = fc.Metrics(size)
		m.Ascent, m.Descent, m.LineGap = segoeEmojiAscent*size, segoeEmojiDescent*size, 0
		return m, true
	}
	for _, f := range style.Families {
		key := familyKey(f)
		if slices.Contains(fc.keys, key) {
			return Metrics{}, false
		}
		aliases := metricAliases[key]
		if !slices.ContainsFunc(fc.keys, func(k string) bool { return slices.Contains(aliases, k) }) {
			continue
		}
		w, found := wordFaces[key]
		if !found {
			return Metrics{}, false
		}
		m = fc.Metrics(size)
		m.Ascent, m.Descent, m.LineGap = w.ascent*size, w.descent*size, 0
		return m, true
	}
	return Metrics{}, false
}

// MetricsFor returns the face's vertical metrics at the size of style, as
// [Face.Metrics] does, or, when style asks for Word's metrics
// ([Style.WordMetrics]) and the face stands in for a Microsoft face the
// style names, the ones Word lays that face's lines out with.
func (fc *Face) MetricsFor(style Style) Metrics {
	if m, ok := fc.wordMetrics(style, style.Size); ok {
		return m
	}
	if style.settings().SizeAdjust.Value > 0 {
		return fc.Metrics(style.Size * adjustFactor(&style, fc, fc.parsedFace()))
	}
	return fc.Metrics(style.Size)
}

// segoeEmojiAdvance is the advance, in em, of most glyphs of Segoe UI
// Emoji, the face Word and PowerPoint draw emoji in: 2812 units of 2048.
// A colour face stands in for it in an Office run ([Style.OfficeEmoji]).
//
// # Corpus evidence
//
//   - probe, docx: Word draws twenty emoji at 9, 12 and 36 points in
//     Segoe UI Emoji, fifteen of them advancing 1.373 ems (12.36, 16.48
//     and 49.43 points), the thumb, the fire, the globe, the pizza and the
//     bulb 1.233, 1.092, 1.303, 1.092 and 1.022; the keycaps and the
//     coloured heart and smile 1.373 at 24 points. The embedded program's
//     hmtx gives 2812 units to 87 glyphs, the most of any advance. Noto
//     Color Emoji advances 2550 units, 1.245 ems: the line of twenty ends
//     12.7 points short at 9 points, 17.3 at 12, where 1.373 ems ends it
//     10.3 and 13.4 points long, and each of the fifteen falls in place.
//   - probe, pptx: PowerPoint lays out the same lines at the same
//     advances, 316.22 points for the twenty at 12 points where Word's
//     take 316.15, and draws each emoji as a bitmap.
//   - probe, docx: Word centres the drawing in the advance: a 36 point
//     smile inks from 4.47 to 44.72 points of its 49.43; Noto's from 2.53
//     to 41.53 of 44.82.
const segoeEmojiAdvance = 2812.0 / 2048

// segoeEmojiAscent and segoeEmojiDescent are the ascent and descent, in
// em, of Segoe UI Emoji's lines in Word: its OS/2 usWinAscent and
// usWinDescent, 2210 and 514 units of 2048 ([Style.OfficeEmoji]).
//
// # Corpus evidence
//
//   - probe, docx: Word's PDF embeds Segoe UI Emoji with winAscent 2210
//     and winDescent 514. Its first line, of 9 point emoji, sets the
//     baseline 9.78 points below the top margin, where Calibri's ascent
//     sets 8.57; lines of emoji at 12 points are 25.25 points apart with
//     8 points after each paragraph and lines of 1.0792, 17.25 points,
//     1.332 ems; 36 point lines of emoji 51.25 apart, 1.319 ems. Segoe's
//     2724 units give 1.330.
//   - probe, pptx: PowerPoint keeps its lines of emoji 1.2 sizes apart,
//     28.84 points at 24 points, 14.42 at 12, as it spaces text alone.
const (
	segoeEmojiAscent  = 2210.0 / 2048
	segoeEmojiDescent = 514.0 / 2048
)

// segoeAdvances sets the advance of every glyph of glyphs that advances
// to w, its drawing moved by half the change so it stays centred: a
// colour face's glyphs standing in for Segoe UI Emoji's
// ([segoeEmojiAdvance]). A flag, a glyph whose cluster in text starts
// with a regional indicator, keeps its own advance: Segoe UI Emoji draws
// no flag, but the two letters it stands for.
//
// # Corpus evidence
//
//   - probe, docx: Word draws the flags of France and Japan at 24 points
//     as the letters F, R, J and P, 0.30 to 0.46 ems each, 0.82 ems a
//     flag; Noto Color Emoji's flags advance 1.245 ems.
//   - litho/pptx: text-complex-scripts prints in PowerPoint the flag of
//     France at 24 points 20.99 points wide, 0.88 ems.
func segoeAdvances(glyphs []shapedGlyph, text []rune, w float64) {
	for k := range glyphs {
		g := &glyphs[k]
		if g.advance <= 0 {
			continue
		}
		if c := g.cluster(); c < len(text) && isRegionalIndicator(text[c]) {
			continue
		}
		g.xoff += float64((w - g.advance) / 2)
		g.advance = w
	}
}

// segoeTextAdvances sets the advance of every glyph of glyphs whose
// cluster in text starts with a character of the Emoji_Presentation
// property (UTS #51, Annex A), or with one the emoji presentation
// selector follows (VS16, U+FE0F), to w, its drawing moved by half the
// change, and reports whether it set one: the glyphs of a face drawing
// an Office run's emoji as text, which Word draws as Segoe UI Emoji's
// outlines at Segoe's advances ([segoeEmojiAdvance]) and line.
//
// # Corpus evidence
//
//   - probe, docx: Word draws the smile and the party popper of runs of
//     stated colours C00000, 000000 and accent1 4472C4, and of a run
//     shaded 3060C0, as one Segoe UI Emoji glyph each in the run's colour,
//     33.02, 32.90, 33.02 and 32.89 points apart at 24 points, where the
//     colour layers of a run of automatic colour lie 33.02 apart: 1.373
//     ems. The baselines of the six paragraphs lie 42.77 and 43.00 points
//     apart, as Segoe's line spaces them, whichever way the emoji draw.
//   - probe, docx: in a run of w:color C00000 Word draws U+2764 and
//     U+263A, text by default, each followed by U+FE0F, as Segoe UI Emoji
//     glyphs 33.02 points apart at 24 points, on a baseline 43.00 points
//     below the paragraph before, as the smiles of the paragraphs around.
func segoeTextAdvances(glyphs []shapedGlyph, text []rune, w float64) bool {
	set := false
	for k := range glyphs {
		g := &glyphs[k]
		c := g.cluster()
		if g.advance > 0 && c < len(text) && (unicode.Is(emojiPresentation, text[c]) || c+1 < len(text) && text[c+1] == variationEmoji) {
			g.xoff += float64((w - g.advance) / 2)
			g.advance = w
			set = true
		}
	}
	return set
}
