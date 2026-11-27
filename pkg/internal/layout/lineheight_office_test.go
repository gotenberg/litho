package layout

import (
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestLineRule_Office lays line boxes out under the Office line rules and
// the minimum line height as LibreOffice prints them, and under
// css.LineRuleCentred with the glyphs centred, as Word lays the lines of
// a document grid, a line holding a run taller than a pitch of the grid
// growing to the whole pitches it needs.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.0.0, a .fodt in Liberation
//     Mono 10pt, whose natural line is 11.35pt with the baseline 8.35pt
//     down: fo:line-height 30pt puts the baseline 24pt down, 6pt puts it
//     4.8pt down, both four fifths; 200% and 150% keep it 8.35pt down and
//     add the room below, 60% puts it at four fifths of 6.8pt;
//     line-height-at-least 30pt puts it 27pt down, the room above, and 6pt
//     changes nothing; a line mixing 10pt and 20pt text at 150% is 1.5
//     times the 20pt text's height with the baseline under its ascent.
//   - probe, docx: on a grid of 15.6 points, a line holding a 26-point
//     Calibri run among 10.5-point text is three pitches tall, 46.8
//     points, the run's baseline 32.3 below the line's top, where
//     centring puts it.
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
	centred := func(v float64) func(*css.Style) {
		return func(s *css.Style) { s.LineRule = css.LineRuleCentred; s.LineHeight = css.Pts(px(v)) }
	}
	grid := func(v float64) func(*css.Style) {
		return func(s *css.Style) {
			s.LineRule, s.LineHeight, s.GridPitch = css.LineRuleCentred, css.Pts(px(v)), px(v)
		}
	}
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
		{"centred", []func(*css.Style){centred(30)}, false, [2]float64{18, 48}, 60},
		{"centred mixed sizes", []func(*css.Style){centred(30)}, true, [2]float64{21, 51}, 60},
		{"grid", []func(*css.Style){grid(12)}, false, [2]float64{9, 21}, 24},
		{"grid, a run taller than a pitch", []func(*css.Style){grid(12)}, true, [2]float64{18, 42}, 48},
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

// TestLineRule_InlinePicture sizes a line holding a picture alone, taller
// than its text, as Writer does: the picture's height at 100%, the room of
// a proportional spacing above 100% added for the text alone, below it,
// and below 100% the whole natural line, the font's descent included,
// shrunk.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a .fodt in Liberation
//     Serif 12pt, whose line is 13.8pt, holding an as-char picture 1cm
//     tall between line breaks: at 100% the line is the picture alone, at
//     115% and 200% 2.07pt and 13.8pt more, the picture's top at the
//     line's top. At 80% Writer makes the line 25.56pt, which four fifths
//     of the natural line, 24.76pt, misses.
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
		{"100%", 1, [2]float64{8, 48}},
		{"150%", 1.5, [2]float64{8, 58}},
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

// TestWordRules_LineHeight sizes a line from its runs under
// css.Style.WordRules, as Word sizes a WordprocessingML line: the
// container's font counts only on an empty line and, from mode 15, on a
// line of pictures alone, and the line's height rounds to a twentieth of
// a point.
//
// # Corpus evidence
//
//   - litho/docx paper-*: Word spaces table rows of Arial 10 point text in
//     paragraphs whose mark is Calibri 11 points 12 points apart, the Arial
//     line and the border, not the Calibri line; Times New Roman 12 point
//     lines over a Calibri 11 point mark at 259/240 14.885 points apart,
//     not the 15.27 the mark's descent would make.
func TestWordRules_LineHeight(t *testing.T) {
	multiple := func(f float64) func(*css.Style) {
		return func(s *css.Style) {
			s.LineRule = css.LineRuleMultiple
			s.LineHeight = css.Dimension{Kind: css.Number, Value: f}
		}
	}
	small := inlineSty(func(s *css.Style) { s.FontSize = px(10) })
	img := func() *Node {
		return &Node{Kind: Replaced, Style: inlineSty(), Picture: pic(color.NRGBA{R: 255, A: 255}, 10, 10)}
	}
	tests := []struct {
		name      string
		mods      []func(*css.Style)
		kids      []*Node
		baselines [2]float64
	}{
		{"single", nil, []*Node{el(small, txt(small, "ab")), nil, el(small, txt(small, "cd"))}, [2]float64{8, 18}},
		{"multiple", []func(*css.Style){multiple(1.5)}, []*Node{el(small, txt(small, "ab")), nil, el(small, txt(small, "cd"))}, [2]float64{8, 23}},
		{"empty line", nil, []*Node{nil, el(small, txt(small, "a"))}, [2]float64{28, 28}},
		// A 10px picture alone on a line over a 20px mark: from mode 15
		// the mark's line, 30px at 150%, the picture's bottom at its
		// descent; before, the picture alone.
		{"picture", []func(*css.Style){multiple(1.5)}, []*Node{el(small, txt(small, "a")), nil, img(), nil, el(small, txt(small, "b"))}, [2]float64{8, 53}},
		{"picture before 15", []func(*css.Style){multiple(1.5), func(s *css.Style) { s.WordRules = 14 }}, []*Node{el(small, txt(small, "a")), nil, img(), nil, el(small, txt(small, "b"))}, [2]float64{8, 33}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(append([]func(*css.Style){func(s *css.Style) { s.FontSize = px(20); s.WordRules = 15 }}, tt.mods...)...)
			kids := make([]*Node, len(tt.kids))
			for i, k := range tt.kids {
				kids[i] = k
				if k == nil {
					kids[i] = br(st)
				}
			}
			res := run(t, doc(el(st, kids...)), page(100, 200))
			_, pos := lineTexts(res, 1)
			last := pos[len(pos)-1][1]
			if len(pos) == 0 || !near(pos[0][1], tt.baselines[0]) || !near(last, tt.baselines[1]) {
				t.Errorf("baselines %v, want first %v and last %v", pos, tt.baselines[0], tt.baselines[1])
			}
		})
	}
	t.Run("twips", func(t *testing.T) {
		// A 10.03px line is 7.5225pt, which rounds to 7.5pt.
		st := sty(func(s *css.Style) { s.FontSize = px(10.03); s.WordRules = 15 })
		res := run(t, doc(el(st, txt(st, "ab"), br(st), txt(st, "cd"))), page(100, 200))
		_, pos := lineTexts(res, 1)
		if len(pos) != 2 || !near(pos[1][1]-pos[0][1], 10) {
			t.Errorf("baselines %v, want 10px apart", pos)
		}
	})
}

// TestWriterLines_LineHeight sizes a line from its glyph runs and atomic
// inlines under css.Office.WriterLines, as Writer sizes a paragraph's
// line: the container's font counts on an empty line alone, and in the
// room a proportional spacing adds below a line of pictures alone.
func TestWriterLines_LineHeight(t *testing.T) {
	multiple := func(f float64) func(*css.Style) {
		return func(s *css.Style) {
			s.LineRule = css.LineRuleMultiple
			s.LineHeight = css.Dimension{Kind: css.Number, Value: f}
		}
	}
	small := inlineSty(func(s *css.Style) { s.FontSize = px(10) })
	img := func() *Node {
		return &Node{Kind: Replaced, Style: inlineSty(), Picture: pic(color.NRGBA{R: 255, A: 255}, 10, 10)}
	}
	edged := inlineSty(func(s *css.Style) {
		s.FontSize = px(10)
		s.Padding[css.Top], s.Padding[css.Bottom] = css.Pts(px(3)), css.Pts(px(4))
		s.Margin[css.Top] = css.Pts(px(2))
		bd := css.Border{Width: 0.75, Style: css.Solid, Color: css.Color{A: 1}}
		s.Border = [4]css.Border{bd, bd, bd, bd}
	})
	tests := []struct {
		name      string
		f         float64
		kids      []*Node
		baselines [2]float64
	}{
		{"runs smaller than the font", 1, []*Node{el(small, txt(small, "ab")), nil, el(small, txt(small, "cd"))}, [2]float64{8, 18}},
		{"multiple", 1.5, []*Node{el(small, txt(small, "ab")), nil, el(small, txt(small, "cd"))}, [2]float64{8, 23}},
		{"empty line", 1, []*Node{nil, el(small, txt(small, "a"))}, [2]float64{28, 28}},
		// A 10px picture alone on a line over a 20px font: the picture's
		// height at 100%; at 150% the room of the font's 20px line, 10px,
		// below it.
		{"picture", 1, []*Node{el(small, txt(small, "a")), nil, img(), nil, el(small, txt(small, "b"))}, [2]float64{8, 28}},
		{"picture 150%", 1.5, []*Node{el(small, txt(small, "a")), nil, img(), nil, el(small, txt(small, "b"))}, [2]float64{8, 43}},
		// A run's padding, border and margin above and below grow its
		// line: 3 + 1 + 2 above the 8px ascent, 4 + 1 below the 2px
		// descent, so the next line's baseline is 2 + 5 + 8 lower.
		{"edges", 1, []*Node{el(edged, txt(edged, "ab")), nil, el(small, txt(small, "cd"))}, [2]float64{14, 29}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(multiple(tt.f), func(s *css.Style) { s.FontSize = px(20); s.WriterLines = true })
			kids := make([]*Node, len(tt.kids))
			for i, k := range tt.kids {
				kids[i] = k
				if k == nil {
					kids[i] = br(st)
				}
			}
			res := run(t, doc(el(st, kids...)), page(100, 200))
			_, pos := lineTexts(res, 1)
			if len(pos) == 0 || !near(pos[0][1], tt.baselines[0]) || !near(pos[len(pos)-1][1], tt.baselines[1]) {
				t.Errorf("baselines %v, want first %v and last %v", pos, tt.baselines[0], tt.baselines[1])
			}
		})
	}
}
