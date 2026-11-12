package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// exactAscent is the share of an exact or shrunk line height above the
// baseline: Writer puts the baseline four fifths of a fixed line height
// below the line's top, whatever the glyphs (LibreOffice's
// SwTextFormatter::CalcRealHeight).
const exactAscent = 0.8

// twips is the number of twentieths of a point in a point: Word rounds the
// height of a line it sizes from its runs to the nearest one, a
// measurement of Word (ECMA-376 Part 1, 17.3.1.33 states spacing in
// twentieths of a point and leaves the layout unit to the application).
//
// # Corpus evidence
//
//   - litho/docx paper-*: Word spaces Calibri 11 point lines at 259/240
//     14.50 points apart, where their natural height gives 14.493, and
//     Times New Roman 12 point lines 14.885 apart on average, where it
//     gives 14.893.
const twips = 20

// officeLine applies the Office line spacing of the block container to the
// line l built by the CSS rules: its LineRule and its MinLineHeight (see
// [css.LineRule]). The line's items share its baseline, which moves with
// them; a line under no Office rule and no minimum keeps its CSS box.
func (s *inlineState) officeLine(l *lineBox) {
	st := s.root.st
	least := 0.0
	if v := st.MinLineHeight; v > 0 && !math.IsInf(v, 1) {
		least = v
	}
	if st.LineRule == css.LineRuleCSS && least == 0 && st.WordRules == 0 {
		return
	}
	if st.WordRules >= wordPictureStrut {
		s.dropPictures(l)
	}
	h, base := l.h, l.baseline
	switch st.LineRule {
	case css.LineRuleExact:
		h = s.e.lineHeight(st)
		base = exactAscent * h
	case css.LineRuleMultiple:
		f := 1.0
		if st.LineHeight.Kind == css.Number && st.LineHeight.Value > 0 {
			f = clamp(st.LineHeight.Value)
		}
		above, below, text := s.naturalExtent(l)
		// Writer adds the room of a proportional spacing above 100% for
		// the text of the line alone: an as-character picture taller than
		// the text keeps its height, the room going below it; below 100%
		// the whole line shrinks.
		h = above + below + (f-1)*text
		base = above
		l.slack = max((f-1)*text, 0)
		if f < 1 {
			h = f * (above + below)
			base = exactAscent * h
		}
	case css.LineRuleCSS:
		if st.WordRules > 0 {
			// Single spacing is the natural height of the runs.
			above, below, _ := s.naturalExtent(l)
			h, base = above+below, above
		}
	}
	if st.WordRules > 0 && st.LineRule != css.LineRuleExact {
		// Word lays lines out in twentieths of a point.
		h = math.Round(h*twips) / twips
	}
	if h < least {
		l.slack = 0
		// An at least line height adds its room above the glyphs: the
		// baseline keeps its distance from the line's bottom, as Writer
		// spaces "at least" lines.
		base += least - h
		h = least
	}
	delta := base - l.baseline
	for _, it := range l.items {
		it.baseline += delta
	}
	for _, f := range l.frags {
		f.top += delta
		f.bottom += delta
	}
	l.h, l.baseline = h, base
}

// naturalExtent returns the extent of l above and below its baseline as
// Writer measures a line's natural height: the face ascent plus its line
// gap of each glyph run and of the container's strut above, the descents
// below, each run where its baseline shift puts it, and the margin boxes
// of the atomic inlines. text is the height the glyph runs and the strut
// alone take, the atomic inlines left out. Under css.Style.WordRules
// the strut counts only on an empty line and, from mode 15, on a line of
// pictures alone, as Word measures a line; before mode 15 such a line is
// its pictures' height, which a proportional spacing does not scale.
func (s *inlineState) naturalExtent(l *lineBox) (above, below, text float64) {
	m := s.e.faceMetrics(s.root.st)
	above, below = m.Ascent+max(m.LineGap, 0), m.Descent
	textAbove, textBelow := above, below
	if mode := s.root.st.WordRules; mode > 0 {
		runs, atomics := lineContent(l)
		if runs || atomics && mode < wordPictureStrut {
			above, below = 0, 0
			textAbove, textBelow = 0, 0
		}
	}
	for _, it := range l.items {
		shift := it.baseline - l.baseline
		up := it.ascent
		switch {
		case it.text != nil && it.atomic == nil:
			up += max(it.gap, 0)
			textAbove = max(textAbove, up-shift)
			textBelow = max(textBelow, it.descent+shift)
		case it.atomic == nil:
			continue
		}
		above = max(above, up-shift)
		below = max(below, it.descent+shift)
	}
	return above, below, textAbove + textBelow
}

// wordPictureStrut is the first Word compatibility mode that sizes a line
// of pictures alone with its paragraph mark's font and sits the pictures'
// bottoms at the font's descent, a measurement of Word.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/128197_compat14 and 128197_compat15:
//     the same paragraph of 36 point text and a 2 point tall shape on a
//     line of its own at 276/240; Word 2010's mode draws the shape's
//     line 2 points tall, Word 2013's as tall as a 36 point text line.
//   - litho/docx paper-*: Word ends the line of a 108 point picture alone,
//     over a Calibri 11 point mark at 259/240, 1.1 points below the
//     picture, the room the proportional spacing adds, where the picture
//     on the baseline would leave the mark's 2.95 point descent too.
const wordPictureStrut = 15

// dropPictures moves the atomic inlines of a line holding no glyph run
// down by the descent of the container's font, so their bottoms sit at
// it, as Word places the pictures of such a line from mode 15.
func (s *inlineState) dropPictures(l *lineBox) {
	runs, atomics := lineContent(l)
	if runs || !atomics {
		return
	}
	d := s.e.faceMetrics(s.root.st).Descent
	for _, it := range l.items {
		if it.atomic != nil {
			it.baseline += d
		}
	}
}

// lineContent reports whether l holds a glyph run and an atomic inline.
func lineContent(l *lineBox) (runs, atomics bool) {
	for _, it := range l.items {
		switch {
		case it.atomic != nil:
			atomics = true
		case it.text != nil && len(it.text.Glyphs) > 0:
			runs = true
		}
	}
	return runs, atomics
}
