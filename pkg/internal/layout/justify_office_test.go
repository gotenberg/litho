package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestJustify_Office checks the two Office justification methods on
// lines of 10px units: Word's distribute justifies at the spaces and
// spreads a line with none between its characters; PowerPoint's dist
// grows each space by up to 1.2 times its width and spreads the rest
// between every two units, the space included.
func TestJustify_Office(t *testing.T) {
	justify := func(a css.TextAlign, j css.TextJustify) func(*css.Style) {
		return func(s *css.Style) { s.TextAlign, s.TextJustify = a, j }
	}
	tests := []struct {
		name  string
		st    *css.Style
		text  string
		width float64
		want  []float64
	}{
		{"words: a line of one word between its letters", sty(justify(css.AlignJustify, css.JustifyWordsElseCharacters)), "abcde f", 55, []float64{0, 11.25, 22.5, 33.75, 45}},
		{"words: a line with a space at the space", sty(justify(css.AlignJustify, css.JustifyWordsElseCharacters)), "ab cd ef", 55, []float64{0, 10, 20, 35, 45}},
		{"capped: the space alone under its growth", sty(justify(css.AlignJustifyAll, css.JustifyDistributeCapped)), "ab cd", 55, []float64{0, 10, 20, 35, 45}},
		// 30px free: the space grows 12px, the 18px left go 4.5px to each
		// of the four gaps.
		{"capped: the rest between every unit", sty(justify(css.AlignJustifyAll, css.JustifyDistributeCapped)), "ab cd", 80, []float64{0, 14.5, 29, 55.5, 70}},
		{"capped: a word alone between its letters", sty(justify(css.AlignJustifyAll, css.JustifyDistributeCapped)), "abc", 50, []float64{0, 20, 40}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(tt.width, 100))
			var xs []float64
			for _, r := range texts(res, 1) {
				for _, g := range r.run.Glyphs {
					if len(xs) < len(tt.want) {
						xs = append(xs, (r.x+g.X)/css.Pt)
					}
				}
			}
			for i := range tt.want {
				if i >= len(xs) || !near(xs[i], tt.want[i]) {
					t.Fatalf("glyphs at %v, want %v", xs, tt.want)
				}
			}
		})
	}
}
