package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// softHyphen is U+00AD SOFT HYPHEN, a hyphenation opportunity a line
// ending there marks with the hyphenate-character (CSS Text 3, 5.4).
const softHyphen = '­'

// hyphenAuto is the hyphen hyphenate-character: auto picks, U+2010 HYPHEN
// when the font covers it and U+002D HYPHEN-MINUS when not, as Chromium
// does (CSS Text 4, 5.5 leaves the choice to the user agent).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) ends a line broken at a
//     soft hyphen with U+2010 in Liberation Serif and DejaVu Sans Mono, and
//     with the string hyphenate-character gives, nothing for "".
const hyphenAuto = '‐'

// hyphenMark is the hyphen a line broken at a soft hyphen ends with,
// shaped in the soft hyphen's style.
type hyphenMark struct {
	width float64
	spans []text.Span
}

// hyphenates reports a line break before character i of p that follows a
// soft hyphen whose hyphens is not none: the line before it ends with the
// hyphenate-character.
func (p *ifcPara) hyphenates(i int) bool {
	return i > 0 && i < p.p.Len() && p.p.Rune(i-1) == softHyphen && p.styleAt(i-1).Hyphens != css.HyphensNone &&
		p.infos[p.p.RunOf(i-1)].kind == runText
}

// hyphen returns the mark a line ending at the soft hyphen of box b ends
// with, nil for none: an empty hyphenate-character, or one no face draws.
func (e *engine) hyphen(b *box) *hyphenMark {
	k := spaceKey{b.st, b.lang}
	if m, ok := e.hyphens[k]; ok {
		return m
	}
	s := b.st.HyphenateCharacter.Text
	ts := e.textStyle(b.st, b.lang)
	if !b.st.HyphenateCharacter.Given {
		s = "-"
		if fc := e.fonts.Match(ts); fc != nil && e.fonts.Covers(fc.Family(), false, hyphenAuto) {
			s = string(hyphenAuto)
		}
	}
	var m *hyphenMark
	if s != "" && e.fontSize(b.st) > 0 {
		dir := text.LeftToRight
		if b.st.Direction == css.RTL {
			dir = text.RightToLeft
		}
		mp, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: s, Style: ts}}, text.ParagraphOptions{Direction: dir})
		if err != nil {
			e.shapeFailed(err, "hyphen", -1)
		} else if mp.Len() > 0 {
			ml := mp.Line(0, mp.Len(), false)
			m = &hyphenMark{width: ml.Width, spans: ml.Spans}
		}
	}
	e.hyphens[k] = m
	return m
}

// hyphenWidth returns the advance of the mark a line broken before
// character i of p ends with, 0 when the break does not hyphenate.
func (e *engine) hyphenWidth(p *ifcPara, i int) float64 {
	if !p.hyphenates(i) {
		return 0
	}
	if m := e.hyphen(p.infos[p.p.RunOf(i-1)].box); m != nil {
		return m.width
	}
	return 0
}
