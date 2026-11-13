package layout

import (
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// emphasisMark is an emphasis mark shaped at half its text's size (CSS
// Text Decoration 3, 3.1): its glyph runs from its start, its advance,
// and its em box.
type emphasisMark struct {
	spans               []text.Span
	width               float64
	emAscent, emDescent float64
}

// emphasis returns the emphasis mark of the text of box b, nil for none.
func (e *engine) emphasis(b *box) *emphasisMark {
	st := b.st
	if st.TextEmphasisMark == "" {
		return nil
	}
	k := spaceKey{st, b.lang}
	if m, ok := e.emMarks[k]; ok {
		return m
	}
	var m *emphasisMark
	ts := e.textStyle(st, b.lang)
	ts.Size = e.fontSize(st) / 2
	ts.LetterSpacing, ts.WordSpacing = 0, 0
	if ts.Size > 0 {
		mp, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: st.TextEmphasisMark, Style: ts}}, text.ParagraphOptions{Direction: text.LeftToRight})
		if err != nil {
			e.shapeFailed(err, "emphasis mark", -1)
		} else if mp.Len() > 0 {
			ml := mp.Line(0, mp.Len(), false)
			// The em box is that of the face that draws the mark.
			m = &emphasisMark{spans: ml.Spans, width: ml.Width, emAscent: 0.8 * ts.Size, emDescent: 0.2 * ts.Size}
			for _, sp := range ml.Spans {
				if sp.Text != nil && sp.EmAscent+sp.EmDescent > 0 {
					m.emAscent, m.emDescent = sp.EmAscent, sp.EmDescent
					break
				}
			}
		}
	}
	e.emMarks[k] = m
	return m
}

// takesMark reports whether the character r takes an emphasis mark: not
// a separator (Zs, Zl, Zp), a control or format character (Cc, Cf) or
// punctuation (P*) (CSS Text Decoration 3, 3.1), as Chromium draws them.
func takesMark(r rune) bool {
	return !unicode.In(r, unicode.Z, unicode.Cc, unicode.Cf, unicode.P)
}

// emphasisExtent returns how far the emphasis marks of item it reach
// above (over) or below its baseline: the mark's em box against the em
// box of its text's first available font, as Chromium sets marks, and 0,
// false for an item without marks.
func (s *inlineState) emphasisExtent(it *lineItem) (float64, bool, bool) {
	if it.text == nil || it.box.st.TextEmphasisMark == "" {
		return 0, false, false
	}
	m := s.e.emphasis(it.box)
	if m == nil {
		return 0, false, false
	}
	em := s.e.faceMetrics(it.box.st)
	if it.box.st.TextEmphasisPosition == css.EmphasisUnder {
		return em.EmDescent + m.emAscent + m.emDescent, false, true
	}
	return em.EmAscent + m.emAscent + m.emDescent, true, true
}

// markRuns returns the glyph runs of the emphasis marks of item it: one
// mark centred over or under each typographic character unit that takes
// one, its em box against the text's (CSS Text Decoration 3, 3.1 to
// 3.5), their origins relative to the item's line box; nil for none.
func (e *engine) markRuns(it *lineItem) []typeset.Text {
	t := it.text
	if t == nil || len(t.Glyphs) == 0 {
		return nil
	}
	m := e.emphasis(it.box)
	if m == nil {
		return nil
	}
	st := it.box.st
	em := e.faceMetrics(st)
	base := it.baseline - em.EmAscent - m.emDescent
	if st.TextEmphasisPosition == css.EmphasisUnder {
		base = it.baseline + em.EmDescent + m.emAscent
	}
	hs := hscale(t)
	// The units of the run: a cluster starts at a glyph that carries its
	// characters and ends where the next one starts.
	var centres []float64
	gs := t.Glyphs
	for i := 0; i < len(gs); {
		j := i + 1
		for j < len(gs) && gs[j].Text == "" {
			j++
		}
		r, _ := utf8.DecodeRuneInString(gs[i].Text)
		if gs[i].Text != "" && takesMark(r) {
			x0, x1 := float64(gs[i].X*hs), it.w
			if j < len(gs) {
				x1 = float64(gs[j].X * hs)
			}
			centres = append(centres, (x0+x1)/2)
		}
		i = j
	}
	if len(centres) == 0 {
		return nil
	}
	var out []typeset.Text
	for _, sp := range m.spans {
		if sp.Text == nil {
			continue
		}
		c := *sp.Text
		c.Glyphs = make([]typeset.Glyph, 0, len(centres)*len(sp.Text.Glyphs))
		for _, x := range centres {
			for _, g := range sp.Text.Glyphs {
				g.X += x - float64(m.width/2) + sp.X
				c.Glyphs = append(c.Glyphs, g)
			}
		}
		c.Origin = typeset.Point{X: t.Origin.X, Y: base}
		c.Parent = nil
		out = append(out, c)
	}
	return out
}

// emphasisMarks paints the emphasis marks of item it of line l in
// text-emphasis-color. A mark is decoration: it paints as an artifact,
// and a reader takes the text without it.
func (p *painter) emphasisMarks(it *lineItem, l *lineBox, page int) {
	color, _ := colorOf(it.box.st.TextEmphasisColor)
	if color.A <= 0 {
		return
	}
	m := p.e.emphasis(it.box)
	for _, c := range p.e.markRuns(it) {
		c.Origin = typeset.Point{X: l.x + c.Origin.X + p.dx, Y: l.y + c.Origin.Y + p.dy}
		c.Color = color
		top := c.Origin.Y - m.emAscent
		p.emit(it.box, true, &c, top, top+m.emAscent+m.emDescent, page)
	}
}
