package layout

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// tiledPaths returns the paths of res's first page filled with a tiling
// pattern, and how many paths it holds besides.
func tiledPaths(res *Result) ([]*typeset.Path, int) {
	var tiled []*typeset.Path
	others := 0
	var walk func(fs []typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Group:
				walk(v.Content)
			case *typeset.Path:
				if v.Fill != nil && v.Fill.Tiling != nil {
					tiled = append(tiled, v)
				} else {
					others++
				}
			}
		}
	}
	walk(res.Pages[0].Content)
	return tiled, others
}

// TestBackgroundRepeat_OnePatternForEveryTile locks a repeated background
// image written as one path filled with a tiling pattern whose cell is one tile
// and whose steps and phase are the tiles' (CSS Backgrounds 3, 3.4 and
// 3.6; ISO 32000-1, 8.7.3.1).
func TestBackgroundRepeat_OnePatternForEveryTile(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		repeat [2]css.Repeat
		pos    [2]float64
		step   [2]float64
		first  [2]float64
		span   typeset.Rect
		pixels string
	}{
		{
			name: "repeat", repeat: [2]css.Repeat{css.RepeatRepeat, css.RepeatRepeat}, pos: [2]float64{1, 0},
			step: [2]float64{4, 4}, first: [2]float64{-3, 0}, span: typeset.Rect{Width: 10, Height: 8},
			pixels: strings.Repeat("BRRBBRRBBR\n", 8),
		},
		{
			name: "space", repeat: [2]css.Repeat{css.RepeatSpace, css.RepeatNoRepeat},
			step: [2]float64{6, 4}, first: [2]float64{0, 0}, span: typeset.Rect{Width: 10, Height: 4},
			pixels: strings.Repeat("RRBB__RRBB\n", 4) + strings.Repeat("__________\n", 4),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(el(sty(width(10), height(8), func(s *css.Style) {
				// The 2 by 1 image, red then blue, stretched to 4 by 4.
				s.BackgroundImage = "i.png"
				s.BackgroundSize = css.BackgroundSize{Width: css.Pts(px(4)), Height: css.Pts(px(4))}
				s.BackgroundRepeat = tt.repeat
				s.BackgroundPosition = [2]css.Dimension{css.Pts(px(tt.pos[0])), css.Pts(px(tt.pos[1]))}
			})))
			res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(10, 8), Image: imageLoader()})
			if err != nil {
				t.Fatal(err)
			}
			tiled, others := tiledPaths(res)
			if len(tiled) != 1 || others != 0 {
				t.Fatalf("%d paths filled with a pattern and %d others, want one alone", len(tiled), others)
			}
			pa, ti := tiled[0], tiled[0].Fill.Tiling
			if ti.Width != px(4) || ti.Height != px(4) || ti.XStep != px(tt.step[0]) || ti.YStep != px(tt.step[1]) {
				t.Errorf("cell %v by %v, steps %v and %v, want 4px cells stepping %vpx and %vpx", ti.Width, ti.Height, ti.XStep, ti.YStep, tt.step[0], tt.step[1])
			}
			if ti.Transform.E != px(tt.first[0]) || ti.Transform.F != px(tt.first[1]) {
				t.Errorf("first cell at (%v, %v), want (%vpx, %vpx)", ti.Transform.E, ti.Transform.F, tt.first[0], tt.first[1])
			}
			got := pa.Ops[2].P[0]
			if pa.Ops[0].P[0].X != px(tt.span.X) || got.X != px(tt.span.X+tt.span.Width) || got.Y != px(tt.span.Y+tt.span.Height) {
				t.Errorf("path from %v to %v, want the span %v in px", pa.Ops[0].P[0], got, tt.span)
			}
			assertPixels(t, res, 1, tt.pixels)
		})
	}
}

// TestBackgroundRepeat_GradientsStayTiles locks a repeated gradient
// painted tile by tile, never through a pattern, and one whose tiles pass
// maxBackgroundTiles as its average colour, as is a single tile.
func TestBackgroundRepeat_GradientsStayTiles(t *testing.T) {
	t.Parallel()
	g := &css.Gradient{Angle: 90, Stops: gstops(red, 0.0, red, 2.0, blue, 2.0, blue, 4.0)}
	for name, tt := range map[string]struct {
		grad  *css.Gradient
		size  float64
		paths int
	}{
		"repeated":           {g, 4, 6},
		"one tile":           {solid(lime), 10, 1},
		"tiles past the cap": {solid(lime), 0.05, 1},
	} {
		root := doc(el(sty(width(10), height(8), func(s *css.Style) {
			s.BackgroundGradient = tt.grad
			s.BackgroundSize = css.BackgroundSize{Width: css.Pts(px(tt.size)), Height: css.Pts(px(tt.size))}
		})))
		if tiled, others := tiledPaths(run(t, root, page(10, 8))); len(tiled) != 0 || others != tt.paths {
			t.Errorf("%s: %d paths filled with a pattern and %d others, want %d plain paths", name, len(tiled), others, tt.paths)
		}
	}
}
