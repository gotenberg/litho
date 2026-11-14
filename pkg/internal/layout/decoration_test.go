package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// decoPaths returns the paths of res's first page filled or stroked in c,
// with their bounds: the points they pass through, cut by the clips of
// the groups around them.
func decoPaths(res *Result, c css.Color) []typeset.Rect {
	want := typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}
	bounds := func(ops []typeset.PathOp) (x0, y0, x1, y1 float64) {
		x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, op := range ops {
			p := op.P[0]
			switch op.Kind {
			case typeset.MoveTo, typeset.LineTo:
			case typeset.CurveTo:
				p = op.P[2]
			default:
				continue
			}
			x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
		}
		return x0, y0, x1, y1
	}
	var out []typeset.Rect
	var walk func([]typeset.Fragment, [4]float64)
	walk = func(fs []typeset.Fragment, clip [4]float64) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil && v.Fill.Color == want || v.Stroke != nil && v.Stroke.Color == want {
					x0, y0, x1, y1 := bounds(v.Ops)
					x0, y0, x1, y1 = max(x0, clip[0]), max(y0, clip[1]), min(x1, clip[2]), min(y1, clip[3])
					out = append(out, typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0})
				}
			case *typeset.Group:
				in := clip
				if v.Clip != nil {
					x0, y0, x1, y1 := bounds(v.Clip.Ops)
					in = [4]float64{max(x0, clip[0]), max(y0, clip[1]), min(x1, clip[2]), min(y1, clip[3])}
				}
				walk(v.Content, in)
			}
		}
	}
	walk(res.Pages[0].Content, [4]float64{math.Inf(-1), math.Inf(-1), math.Inf(1), math.Inf(1)})
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
		s.TextDecorationSkipInk = css.SkipInkNone
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

// CSS Text Decoration 3, 2: a decoration propagated to a block container
// is drawn by the anonymous inline box that wraps the block's text, at the
// block's font size: the underline of a 10px block is 4px thick under its
// 40px block child, a tenth of 40px as Blink floors it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws the underline of a 16px Ahem block
//     10px thick under its 100px block child.
func TestTextDecoration_PropagatedToBlockTakesItsSize(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	outer := sty(func(s *css.Style) {
		s.TextDecoration = css.Underline
		s.TextDecorationColor = lime
		s.TextDecorationSkipInk = css.SkipInkNone
	})
	inner := sty(func(s *css.Style) { s.FontSize = px(40); s.TextDecorationSkipInk = css.SkipInkNone })
	res := runWith(t, doc(el(outer, el(inner, txt(inner, "a")))), Options{Page: page(200, 60), BrowserRules: true})
	got := decoPaths(res, lime)
	if len(got) != 1 || !near(got[0].Height, px(4)) || !near(got[0].Width, px(40)) {
		t.Fatalf("underline %+v, want one 40px wide and 4px thick", got)
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
// edge, however thick: CSS Text Decoration 4, 2.4 and 2.5 leave its exact
// position to the user agent.
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
		s.TextDecorationSkipInk = css.SkipInkNone
	})
	span := inlineSty()
	res := runWith(t, doc(el(block, txt(block, "aa"), el(span, txt(span, "bb")), txt(block, "cc"))), Options{Page: page(200, 60), BrowserRules: true})
	if got := decoPaths(res, lime); len(got) != 1 || !near(got[0].Width, px(60)) {
		t.Errorf("waves %+v, want one 60px long", got)
	}
}

// CSS Text Decoration 4, 2.10.5: an underline of skip-ink auto or all
// stops short of the glyphs it crosses, by its thickness on each side; one
// of none crosses them.
func TestTextDecoration_SkipInk(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	tests := []struct {
		name   string
		skip   css.SkipInk
		text   string
		width  float64
		office bool
	}{
		// "a a": glyphs at 0-10px and 20-30px, the space between; the
		// underline keeps 1px clear of each glyph.
		{"auto", css.SkipInkAuto, "a a", 8, false},
		{"none", css.SkipInkNone, "a a", 30, false},
		{"auto without BrowserRules", css.SkipInkAuto, "a a", 30, true},
		{"all", css.SkipInkAll, "a a", 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(func(s *css.Style) {
				s.TextDecoration = css.Underline
				s.TextDecorationColor = lime
				s.TextDecorationSkipInk = tt.skip
				s.TextUnderlineOffset = css.Pts(px(-3))
				s.TextDecorationThickness = css.Pts(px(1))
			})
			res := runWith(t, doc(el(st, txt(st, tt.text))), Options{Page: page(100, 20), BrowserRules: !tt.office})
			got := decoPaths(res, lime)
			if len(got) != 1 {
				t.Fatalf("%d underline paths, want 1", len(got))
			}
			if !near(got[0].Width, px(tt.width)) {
				t.Errorf("underline %+v, want %vpx wide", got[0], tt.width)
			}
		})
	}
}

// Chromium's skip-ink auto crosses the glyphs of Han, kana and Hangul and
// skips any other, a Latin letter among them included.
func TestIdeographic(t *testing.T) {
	for s, want := range map[string]bool{"漢": true, "かカ": true, "한": true, "ㄅ": true, "a": false, "漢a": false, "ж": false, "": false} {
		if got := ideographic(s); got != want {
			t.Errorf("ideographic(%q) = %v, want %v", s, got, want)
		}
	}
}

// CSS Text Decoration 4, 2.7 and 2.8.1: text-underline-position: under
// sets the underline under the decorating box's descent, one pixel below
// it as Chromium draws it, and from-font at the face's underline
// position; text-underline-offset counts from either.
func TestTextDecoration_UnderlinePosition(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	tests := []struct {
		name   string
		pos    css.UnderlinePosition
		offset float64
		top    float64
	}{
		// Ahem 10px, line height 1: baseline 8px, descent 2px.
		{"auto", 0, 0, 9},
		{"under", css.UnderUnder, 0, 11},
		{"under with offset", css.UnderUnder, 2, 13},
		{"auto with offset", 0, 2, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(func(s *css.Style) {
				s.TextDecoration = css.Underline
				s.TextDecorationColor = lime
				s.TextDecorationSkipInk = css.SkipInkNone
				s.TextUnderlinePosition = tt.pos
				if tt.offset != 0 {
					s.TextUnderlineOffset = css.Pts(px(tt.offset))
				}
			})
			res := runWith(t, doc(el(st, txt(st, "a"))), Options{Page: page(100, 20), BrowserRules: true})
			if got := decoPaths(res, lime); len(got) != 1 || !near(got[0].Y, px(tt.top)) {
				t.Errorf("underline %+v, want its top at %vpx", got, tt.top)
			}
		})
	}
}

// CSS Text Decoration 3, 2.3 leaves the dash pattern to the user agent:
// under BrowserRules a dashed line takes Blink's, dashes of two
// thicknesses from 3px up with the gap fitted so the line ends on a dash,
// and a line too short for two dashes is solid.
func TestBlinkDashes(t *testing.T) {
	for _, tc := range []struct {
		w, thick  float64
		dash, gap float64
	}{
		{200, 1, 3, 80.0 / 39},
		{200, 4, 8, 4},
		{200, 6, 12, 6.8},
		{200, 8, 16, 7},
		{20, 4, 20.0 / 20 * 8, 20.0 / 20 * 4},
	} {
		got := blinkDashes(px(tc.w), px(tc.thick))
		if len(got) != 2 || !near(got[0], px(tc.dash)) || !near(got[1], px(tc.gap)) {
			t.Errorf("blinkDashes(%vpx, %vpx) = %v, want %vpx and %vpx", tc.w, tc.thick, got, tc.dash, tc.gap)
		}
	}
	if got := blinkDashes(px(10), px(4)); got != nil {
		t.Errorf("blinkDashes(10px, 4px) = %v, want a solid line", got)
	}
}

// Under BrowserRules a wavy underline is Blink's wave: one cubic a
// wavelength of 1 + 2·round(2t + 0.5) pixels, its axis a thickness and a
// pixel and a half below the line's top.
func TestTextDecoration_WavyIsBlinksWave(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	st := sty(func(s *css.Style) {
		s.TextDecoration = css.Underline
		s.TextDecorationStyle = css.DecorationWavy
		s.TextDecorationColor = lime
		s.TextDecorationThickness = css.Pts(px(4))
		s.TextDecorationSkipInk = css.SkipInkNone
	})
	res := runWith(t, doc(el(st, txt(st, "aaaaaaaaaa"))), Options{Page: page(200, 60), BrowserRules: true})
	var curves []typeset.PathOp
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Stroke != nil && v.Stroke.Color == (typeset.Color{G: 1, A: 1}) {
					for _, op := range v.Ops {
						if op.Kind == typeset.CurveTo {
							curves = append(curves, op)
						}
					}
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if len(curves) < 2 {
		t.Fatalf("%d curves, want a wave of cubics", len(curves))
	}
	if got := curves[1].P[2].X - curves[0].P[2].X; !near(got, px(19)) {
		t.Errorf("wavelength %v, want 19px", got)
	}
	if got := curves[0].P[0].Y - curves[0].P[2].Y; !near(got, px(13.5)) {
		t.Errorf("control distance %v, want 13.5px", got)
	}
}
