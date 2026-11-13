package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/officefx"
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
		if f >= 1 && st.WordRules == 0 {
			below = s.picturesDescent(l, below)
		}
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
// alone take, the atomic inlines left out. Under css.Style.WordRules a
// run's text effects add the room Word gives them
// ([officefx.WordRunReach]), which the proportional spacing scales with
// the glyphs'; under css.Style.WordRules
// the strut counts only on an empty line and, from mode 15, on a line of
// pictures alone, as Word measures a line; before mode 15 such a line is
// its pictures' height, which a proportional spacing does not scale.
// Under css.Office.WriterLines the strut counts on an empty line alone,
// and in text on a line of pictures alone (see [writerStrut]).
func (s *inlineState) naturalExtent(l *lineBox) (above, below, text float64) {
	st := s.root.st
	m := s.e.faceMetrics(st)
	above, below = m.Ascent+max(m.LineGap, 0), m.Descent
	textAbove, textBelow := above, below
	runs, atomics := lineContent(l)
	switch {
	case st.WordRules > 0:
		if runs || atomics && st.WordRules < wordPictureStrut {
			above, below = 0, 0
			textAbove, textBelow = 0, 0
		}
	case st.WriterLines:
		strut, textStrut := writerStrut(runs, atomics)
		if !strut {
			above, below = 0, 0
		}
		if !textStrut {
			textAbove, textBelow = 0, 0
		}
	}
	for _, it := range l.items {
		shift := it.baseline - l.baseline
		up, down := it.ascent, it.descent
		switch {
		case it.text != nil && it.atomic == nil:
			up += max(it.gap, 0)
			if st.WordRules > 0 {
				a, b := officefx.WordRunReach(it.box.st.TextEffects, it.text.Size, it.descent)
				up, down = up+a, down+b
			}
			textAbove = max(textAbove, up-shift)
			textBelow = max(textBelow, down+shift)
		case it.atomic == nil:
			continue
		}
		above = max(above, up-shift)
		below = max(below, down+shift)
	}
	return above, below, textAbove + textBelow
}

// writerStrut reports whether the font of a block container under
// css.Office.WriterLines counts in the natural height of a line holding
// glyph runs (runs) and atomic inlines (atomics), as Writer sizes a
// paragraph's line: on an empty line alone. textStrut reports whether it
// counts in the height of the line's text, which a proportional spacing
// scales: on a line holding no glyph run, so a line of pictures alone
// takes the room of a line of the container's font.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, .fodt paragraphs
//     setting no line height, in Liberation Serif, DejaVu Sans and
//     Carlito at 8, 12 and 24 points: lines 9.2, 13.8 and 27.6 points
//     apart in Liberation Serif, 9.3, 14 and 27.95 in DejaVu Sans, 9.75,
//     14.65 and 29.3 in Carlito, the first baseline 7.5, 11.2 and 22.4
//     points below the top margin in Liberation Serif, its ascent and its
//     whole line gap, 0.5 points lower than half the gap would put it at
//     24 points.
//   - probe, odt: a Liberation Serif 24 point paragraph whose text is all
//     8 points spaces its lines 9.2 points apart, as an 8 point paragraph
//     does, the 24 point font adding nothing; the same at fo:line-height
//     100%, and 115% adds 15% of 9.2 points.
//   - probe, odt: a 12 point line holding a 24 point run puts the
//     baseline 22.4 points below the line's top and the next line 5.2
//     points below it, the 24 point run's ascent and descent; a
//     DejaVu Sans or Carlito run in a Liberation Serif line takes the
//     tallest ascent and the deepest descent of the two faces.
//   - probe, odt: a picture 0.2 cm tall alone in a Liberation Serif
//     12 point paragraph makes its line 5.7 points tall, at fo:line-height
//     100% too, where the font's line is 13.8; at 115% the line takes
//     2.07 points more below the picture, 15% of the font's line. A 2 cm
//     picture alone ends its line at its bottom, followed by a space it
//     takes the font's 2.6 point descent more.
//   - probe, odt: an empty paragraph, the paragraphs of a table's cells,
//     of a list's items and of a header are 13.8 points tall in
//     Liberation Serif 12 points; a superscript at 58% grows no line.
func writerStrut(runs, atomics bool) (strut, textStrut bool) {
	return !runs && !atomics, !runs
}

// picturesDescent returns the extent below the baseline of a line l
// whose natural extent below it is below, as Writer measures a line of
// pictures alone at a proportional spacing of 100% or more: the deepest
// descent of its atomic inlines, the container's font left out, so a
// picture on the baseline ends the line. A line holding a glyph run, a
// space included, keeps below. A picture shorter than the font's ascent
// is not measured.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a 108 point
//     as-character picture alone in a Carlito 11 point paragraph, or on a
//     line of its own between line breaks, makes the line 108 points tall
//     at 100%, 109.07 at 108% and 114.7 at 150%, the room of the text
//     alone added; the 2.9 point descent counts only when
//     a space follows the picture. A 1 cm picture between line breaks in
//     Liberation Serif 12 point loses the 2.6 point descent at 100%, 115%
//     and 200% alike.
//   - litho/odt paper-margins-none-a4-landscape: the caption under the
//     picture of each paper document sits 3 points higher than the
//     descent would put it, which keeps the last line on the first page.
func (s *inlineState) picturesDescent(l *lineBox, below float64) float64 {
	runs, atomics := lineContent(l)
	if runs || !atomics {
		return below
	}
	below = 0
	for _, it := range l.items {
		if it.atomic != nil {
			below = max(below, it.descent+it.baseline-l.baseline)
		}
	}
	return below
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
