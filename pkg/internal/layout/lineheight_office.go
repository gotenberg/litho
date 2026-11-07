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
	if st.LineRule == css.LineRuleCSS && least == 0 {
		return
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
		if f < 1 {
			h = f * (above + below)
			base = exactAscent * h
		}
	case css.LineRuleCSS:
	}
	if h < least {
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
// alone take, the atomic inlines left out.
func (s *inlineState) naturalExtent(l *lineBox) (above, below, text float64) {
	m := s.e.faceMetrics(s.root.st)
	above, below = m.Ascent+max(m.LineGap, 0), m.Descent
	textAbove, textBelow := above, below
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
