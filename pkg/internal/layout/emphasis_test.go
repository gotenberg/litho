package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestEmphasisMarks locks where emphasis marks sit: one over each
// character but separators and punctuation, centred on it, its em box on
// the text's, at half the text's size (CSS Text Decoration 3, 3.1 to
// 3.5), and the line's room taken before the line grows, as for a ruby.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws the Ahem mark "x" of 40px Ahem text
//     "XX X." as 20px squares centred on the three letters, none on the
//     space or the full stop, their bottom on the text's top; marks under
//     the text start at its bottom; a 40px line of line height 1 grows by
//     the mark where nothing above gives it room.
func TestEmphasisMarks(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		pos           css.EmphasisPosition
		margin, markY float64
		height        float64
	}{
		{"over in a margin", css.EmphasisOver, 30, 26, 40},
		{"over", css.EmphasisOver, 0, 16, 60},
		{"under", css.EmphasisUnder, 0, 56, 60},
	} {
		block := sty(func(s *css.Style) {
			s.FontSize = px(40)
			s.Margin[css.Top] = css.Pts(px(c.margin))
			s.TextEmphasisMark, s.TextEmphasisPosition = "x", c.pos
		})
		root := el(sty(), el(block, txt(block, "XX X.")))
		res := run(t, root, page(600, 300))
		var mark *placedText
		for _, p := range texts(res, 1) {
			if p.text == "xxx" {
				mark = &p
			}
		}
		if mark == nil {
			t.Fatalf("%s: no mark run in %v", c.name, texts(res, 1))
		}
		if y := mark.y / css.Pt; !near(y, c.markY) {
			t.Errorf("%s: marks at y %g, want %g", c.name, y, c.markY)
		}
		for i, want := range []float64{10, 50, 130} {
			if x := (mark.x + mark.run.Glyphs[i].X) / css.Pt; !near(x, want) {
				t.Errorf("%s: mark %d at x %g, want %g", c.name, i, x, want)
			}
		}
		if mark.run.Parent != nil {
			t.Errorf("%s: a mark is an artifact", c.name)
		}
		if h := rect(kid(t, boxes(t, root, page(600, 300)), 0))[3]; !near(h, c.height) {
			t.Errorf("%s: block %gpx tall, want %g", c.name, h, c.height)
		}
	}
}
