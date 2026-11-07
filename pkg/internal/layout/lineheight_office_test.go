package layout

import (
	"image/color"
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

// TestLineRule_InlinePicture sizes a line holding a picture taller than
// its text as Writer does: the picture's height and the text's descent at
// 100%, the room of a proportional spacing above 100% added for the text
// alone, below it, and the whole line shrunk below 100%.
//
// # Corpus evidence
//
//   - LibreOffice 9.0.0 image: a .fodt in Liberation Serif 12pt, whose
//     line is 13.8pt, holding an as-char picture 1cm tall: at 100% the
//     line is the picture plus the 2.6pt descent, at 115% and 200% 2.07pt
//     and 13.8pt more, the picture's top at the line's top; at 80% four
//     fifths of the natural line, the baseline at four fifths of it.
func TestLineRule_InlinePicture(t *testing.T) {
	multiple := func(f float64) func(*css.Style) {
		return func(s *css.Style) {
			s.LineRule = css.LineRuleMultiple
			s.LineHeight = css.Dimension{Kind: css.Number, Value: f}
		}
	}
	tests := []struct {
		name string
		f    float64
		// baselines are those of the lines before and after the picture's.
		baselines [2]float64
	}{
		{"100%", 1, [2]float64{8, 50}},
		{"150%", 1.5, [2]float64{8, 60}},
		{"60%", 0.6, [2]float64{4.8, 30}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(multiple(tt.f))
			img := &Node{Kind: Replaced, Style: inlineSty(), Picture: pic(color.NRGBA{R: 255, A: 255}, 10, 30)}
			root := doc(el(st, txt(st, "a"), br(st), img, br(st), txt(st, "b")))
			res := run(t, root, page(100, 200))
			_, pos := lineTexts(res, 1)
			if len(pos) != 2 || !near(pos[0][1], tt.baselines[0]) || !near(pos[1][1], tt.baselines[1]) {
				t.Errorf("baselines %v, want %v", pos, tt.baselines)
			}
		})
	}
}
