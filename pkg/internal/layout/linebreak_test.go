package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestLineBreak_Strictness(t *testing.T) {
	lb := func(v css.LineBreak) func(*css.Style) { return func(s *css.Style) { s.LineBreak = v } }
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []string
	}{
		// Three units a line: a small kana may start a line but under
		// strict.
		{"strict keeps small kana", sty(lb(css.LineBreakStrict)), "漢字漢ぁ漢", []string{"漢字", "漢ぁ漢"}},
		{"normal breaks before small kana", sty(lb(css.LineBreakNormal)), "漢字漢ぁ漢", []string{"漢字漢", "ぁ漢"}},
		{"auto breaks before the prolonged mark", sty(), "漢字漢ー漢", []string{"漢字漢", "ー漢"}},
		{"normal keeps iteration marks", sty(lb(css.LineBreakNormal)), "漢字漢々漢", []string{"漢字", "漢々漢"}},
		{"loose breaks before iteration marks", sty(lb(css.LineBreakLoose)), "漢字漢々漢", []string{"漢字漢", "々漢"}},
		{"no break after an opening bracket", sty(), "漢字「ー漢", []string{"漢字", "「ー漢"}},
		// keep-all closes the opportunities between CJK letters, the ones
		// line-break opens included (CSS Text 3, 5.3).
		{"keep-all keeps small kana", sty(lb(css.LineBreakNormal), func(s *css.Style) { s.WordBreak = css.KeepAll }), "漢字漢ぁ漢", []string{"漢字漢ぁ漢"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(30, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJustify_CJK(t *testing.T) {
	justify := func(j css.TextJustify) func(*css.Style) {
		return func(s *css.Style) { s.TextAlign, s.TextJustify = css.AlignJustify, j }
	}
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []float64
	}{
		// A 55px line of five 10px units and a sixth that wraps: auto
		// spreads the 5px left over the four gaps between ideographs.
		{"auto between ideographs", sty(justify(css.JustifyAuto)), "漢字漢字漢字", []float64{0, 11.25, 22.5, 33.75, 45}},
		{"inter-word leaves ideographs", sty(justify(css.JustifyInterWord)), "漢字漢字漢字", []float64{0, 10, 20, 30, 40}},
		{"inter-character between letters", sty(justify(css.JustifyInterCharacter)), "abcde f", []float64{0, 11.25, 22.5, 33.75, 45}},
		{"none", sty(justify(css.JustifyNone)), "漢字漢字漢字", []float64{0, 10, 20, 30, 40}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(55, 100))
			var xs []float64
			for _, r := range texts(res, 1) {
				if len(xs) >= 5 {
					break
				}
				for _, g := range r.run.Glyphs {
					if len(xs) < 5 {
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
