package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestJustifyShrink lets a justified line take the word that fits once its
// spaces, and the one before that word, give up their JustifyShrink share,
// when no more than a third of the word and its space reaches past the
// line's end, and
// shrinks the spaces to fit; a line with no room even so, a word reaching
// further, a line holding a tab and a container that does not justify
// break as before.
func TestJustifyShrink(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width float64
		align css.TextAlign
		first string
		xs    []float64
	}{
		// "aa bb cc" is 80px in 76px: one inner space and the next one
		// may give up 2.5px each, 5px, and the two spaces shrink by 2px.
		{"shrinks", "aa bb cc dd", 76, css.AlignJustify, "aa bb cc", []float64{0, 10, 28, 38, 56, 66}},
		{"no room", "aa bb cc dd", 74, css.AlignJustify, "aa bb", nil},
		// "a a a a a a bb" is 140px: in 131px " bb" reaches 9px past the
		// end, under a third of its 30px; in 129px 11px, within the 15px
		// the six spaces give up but over a third.
		{"word fits", "a a a a a a bb cc", 131, css.AlignJustify, "a a a a a a bb", nil},
		{"word reaches", "a a a a a a bb cc", 129, css.AlignJustify, "a a a a a a", nil},
		{"tab", "aa\tbb cc dd", 76, css.AlignJustify, "aabb", nil},
		{"start", "aa bb cc dd", 76, css.AlignStart, "aa bb", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := func(s *css.Style) {
				s.TextAlign = tt.align
				s.JustifyShrink = 0.25
				s.WhiteSpace = css.PreWrap
				s.TabSize = css.Pts(px(30))
			}
			res := run(t, doc(el(sty(m), txt(sty(m), tt.text))), page(tt.width, 50))
			lines, _ := lineTexts(res, 1)
			if len(lines) == 0 || trimSpaces(lines[0]) != tt.first {
				t.Fatalf("lines %q, first want %q", lines, tt.first)
			}
			if tt.xs == nil {
				return
			}
			runs := texts(res, 1)
			var xs []float64
			for _, g := range runs[0].run.Glyphs {
				if g.Text != " " {
					xs = append(xs, (runs[0].x+g.X)/css.Pt)
				}
			}
			for i, w := range tt.xs {
				if i >= len(xs) || !near(xs[i], w) {
					t.Fatalf("glyphs at %v, want %v", xs, tt.xs)
				}
			}
		})
	}
}

// trimSpaces drops the spaces that end s.
func trimSpaces(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
