package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// decoPaths returns the paths of res's first page filled or stroked in c,
// with their bounds.
func decoPaths(res *Result, c css.Color) []typeset.Rect {
	want := typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}
	var out []typeset.Rect
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil && v.Fill.Color == want || v.Stroke != nil && v.Stroke.Color == want {
					x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
					for _, op := range v.Ops {
						if op.Kind != typeset.MoveTo && op.Kind != typeset.LineTo {
							continue
						}
						p := op.P[0]
						x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
					}
					out = append(out, typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0})
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	return out
}

// CSS Text Decoration 3, 2.5: an underline keeps one thickness and one
// position on each line for the decorations of one decorating box, under
// superscripts and larger text alike, and the runs of other boxes it
// spans draw one line.
func TestTextDecoration_OneUnderlinePerDecoratingBox(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	block := sty(func(s *css.Style) {
		s.TextDecoration = css.Underline
		s.TextDecorationColor = lime
		s.LineHeight = css.Dimension{Kind: css.Number, Value: 3}
	})
	sup := inlineSty(func(s *css.Style) {
		s.VerticalAlign = css.VerticalAlign{Kind: css.Super}
		s.FontSize = px(20)
	})
	res := runWith(t, doc(el(block, txt(block, "aa"), el(sup, txt(sup, "bb")), txt(block, "cc"))), Options{Page: page(200, 60), BrowserRules: true})
	got := decoPaths(res, lime)
	if len(got) != 1 {
		t.Fatalf("%d underlines %+v, want one across the three runs", len(got), got)
	}
	// Ahem 10px: 20px of text and 40px of superscript; the block's
	// thickness, one pixel, at its baseline plus Blink's gap of one pixel.
	if r := got[0]; !near(r.Width, px(80)) || !near(r.Height, px(1)) {
		t.Errorf("underline %+v, want 80px wide and 1px thick", r)
	}
}

// CSS Text Decoration 3, 4: a text shadow copies the decorations of the
// text as well as its glyphs, in the shadow's colour.
func TestTextShadow_ShadowsDecorations(t *testing.T) {
	lime, blue := css.Color{G: 1, A: 1}, css.Color{B: 1, A: 1}
	st := sty(func(s *css.Style) {
		s.TextDecoration = css.Underline | css.LineThrough
		s.TextDecorationColor = lime
		s.TextShadow = []css.Shadow{{X: px(2), Y: px(3), Color: blue}}
	})
	res := run(t, doc(el(st, txt(st, "a"))), page(100, 100))
	own, shadow := decoPaths(res, lime), decoPaths(res, blue)
	if len(own) != 2 || len(shadow) != 2 {
		t.Fatalf("decorations %+v, shadows %+v: want an underline and a line-through each", own, shadow)
	}
	for i := range own {
		if !near(shadow[i].X-own[i].X, px(2)) || !near(shadow[i].Y-own[i].Y, px(3)) {
			t.Errorf("shadow %+v of %+v, want it 2px right and 3px down", shadow[i], own[i])
		}
	}
}

// Chromium draws an overline grown up from one pixel below the glyphs' top
// edge, however thick, a departure from CSS Text Decoration 4, 2.6 that
// leaves the side it grows to open.
func TestTextDecoration_OverlineGrowsUp(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	st := sty(func(s *css.Style) {
		s.TextDecoration = css.Overline
		s.TextDecorationColor = lime
		s.TextDecorationThickness = css.Pts(px(6))
		s.LineHeight = css.Dimension{Kind: css.Number, Value: 3}
	})
	res := runWith(t, doc(el(st, txt(st, "a"))), Options{Page: page(100, 100), BrowserRules: true})
	got := decoPaths(res, lime)
	// Line height 30px: the Ahem glyph's top, its ascent line, is 10px
	// down.
	if len(got) != 1 || !near(got[0].Y, px(5)) || !near(got[0].Height, px(6)) {
		t.Fatalf("overline %+v, want 6px thick ending at 11px", got)
	}
}

// A wavy line keeps its phase across the runs of its decorating box: with
// [Options.BrowserRules] the runs draw one wave, as Chromium does.
func TestTextDecoration_WavyLineJoinsRuns(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	block := sty(func(s *css.Style) {
		s.TextDecoration = css.Underline
		s.TextDecorationStyle = css.DecorationWavy
		s.TextDecorationColor = lime
	})
	span := inlineSty()
	res := runWith(t, doc(el(block, txt(block, "aa"), el(span, txt(span, "bb")), txt(block, "cc"))), Options{Page: page(200, 60), BrowserRules: true})
	if got := decoPaths(res, lime); len(got) != 1 || !near(got[0].Width, px(60)) {
		t.Errorf("waves %+v, want one 60px long", got)
	}
}
