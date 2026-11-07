package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestLineRule_Office lays line boxes out under the Office line rules and
// the minimum line height as LibreOffice prints them.
//
// # Corpus evidence
//
//   - LibreOffice 9.0.0 image: a .fodt in Liberation Mono 10pt, whose
//     natural line is 11.35pt with the baseline 8.35pt down: fo:line-height
//     30pt puts the baseline 24pt down, 6pt puts it 4.8pt down, both four
//     fifths; 200% and 150% keep it 8.35pt down and add the room below,
//     60% puts it at four fifths of 6.8pt; line-height-at-least 30pt puts
//     it 27pt down, the room above, and 6pt changes nothing; a line mixing
//     10pt and 20pt text at 150% is 1.5 times the 20pt text's height with
//     the baseline under its ascent.
func TestLineRule_Office(t *testing.T) {
	small := func(s *css.Style) { s.FontSize = px(10) }
	big := inlineSty(func(s *css.Style) { s.FontSize = px(20) })
	exact := func(v float64) func(*css.Style) {
		return func(s *css.Style) { s.LineRule = css.LineRuleExact; s.LineHeight = css.Pts(px(v)) }
	}
	multiple := func(f float64) func(*css.Style) {
		return func(s *css.Style) {
			s.LineRule = css.LineRuleMultiple
			s.LineHeight = css.Dimension{Kind: css.Number, Value: f}
		}
	}
	least := func(v float64) func(*css.Style) { return func(s *css.Style) { s.MinLineHeight = px(v) } }
	tests := []struct {
		name string
		mods []func(*css.Style)
		big  bool
		// baselines are the baselines of the two lines, height the block's.
		baselines [2]float64
		height    float64
	}{
		{"css", nil, false, [2]float64{8, 18}, 20},
		{"exact taller", []func(*css.Style){exact(30)}, false, [2]float64{24, 54}, 60},
		{"exact shorter", []func(*css.Style){exact(6)}, false, [2]float64{4.8, 10.8}, 12},
		{"multiple 1.5", []func(*css.Style){multiple(1.5)}, false, [2]float64{8, 23}, 30},
		{"multiple 2", []func(*css.Style){multiple(2)}, false, [2]float64{8, 28}, 40},
		{"multiple 0.6", []func(*css.Style){multiple(0.6)}, false, [2]float64{4.8, 10.8}, 12},
		{"at least taller", []func(*css.Style){least(30)}, false, [2]float64{28, 58}, 60},
		{"at least shorter", []func(*css.Style){least(5)}, false, [2]float64{8, 18}, 20},
		{"exact with an at least", []func(*css.Style){exact(6), least(10)}, false, [2]float64{8.8, 18.8}, 20},
		{"multiple mixed sizes", []func(*css.Style){multiple(1.5)}, true, [2]float64{16, 46}, 60},
		{"at least mixed sizes", []func(*css.Style){least(30)}, true, [2]float64{26, 56}, 60},
		{"exact mixed sizes", []func(*css.Style){exact(30)}, true, [2]float64{24, 54}, 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(append([]func(*css.Style){small}, tt.mods...)...)
			kids := []*Node{txt(st, "ab"), br(st), txt(st, "cd")}
			if tt.big {
				kids = []*Node{txt(st, "a"), el(big, txt(big, "B")), br(st), txt(st, "c"), el(big, txt(big, "D"))}
			}
			root := doc(el(st, kids...))
			res := run(t, root, page(100, 200))
			_, pos := lineTexts(res, 1)
			if len(pos) != 2 || !near(pos[0][1], tt.baselines[0]) || !near(pos[1][1], tt.baselines[1]) {
				t.Errorf("baselines %v, want %v", pos, tt.baselines)
			}
			rb := boxes(t, root, page(100, 200))
			if got := rect(kid(t, rb, 0, 0)); !near(got[3], tt.height) {
				t.Errorf("block %v, want height %v", got, tt.height)
			}
		})
	}
}

// TestLineRule_EmptyLine sizes an empty line under an Office rule like a
// line of the container's font.
func TestLineRule_EmptyLine(t *testing.T) {
	st := sty(func(s *css.Style) { s.LineRule = css.LineRuleExact; s.LineHeight = css.Pts(px(30)) })
	root := doc(el(st, br(st), txt(st, "a")))
	res := run(t, root, page(100, 200))
	_, pos := lineTexts(res, 1)
	if len(pos) != 1 || !near(pos[0][1], 54) {
		t.Errorf("baselines %v, want the second line's at 54", pos)
	}
}
