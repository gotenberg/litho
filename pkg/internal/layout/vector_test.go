package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// vec returns a vector picture of natural size w by h px that paints its
// left half red and its right half blue, changed by mods.
func vec(w, h float64, mods ...func(*Picture)) *Picture {
	p := &Picture{Width: px(w), Height: px(h), Vector: func(_ context.Context, r compose.Rect) []compose.Fragment {
		half := r.Width / 2
		return []compose.Fragment{&compose.Group{Opacity: 1, Content: []compose.Fragment{
			&compose.Path{Ops: rectOps(compose.Rect{X: r.X, Y: r.Y, Width: half, Height: r.Height}), Fill: &compose.Fill{Color: compose.Color{R: 1, A: 1}}},
			&compose.Path{Ops: rectOps(compose.Rect{X: r.X + half, Y: r.Y, Width: half, Height: r.Height}), Fill: &compose.Fill{Color: compose.Color{B: 1, A: 1}}},
		}}}
	}}
	for _, m := range mods {
		m(p)
	}
	return p
}

func noSize(p *Picture) { p.NoNaturalWidth, p.NoNaturalHeight = true, true }

func TestVector_Draws(t *testing.T) {
	root := doc(&Node{Kind: Replaced, Style: sty(margin(1)), Picture: vec(4, 2)})
	assertPixels(t, run(t, root, page(6, 4)), 1, `
		______
		_RRBB_
		_RRBB_
		______`)
}

func TestVector_Sizing(t *testing.T) {
	tests := []struct {
		name string
		pic  *Picture
		mods []func(*css.Style)
		want [2]float64
	}{
		{"natural", vec(40, 20), nil, [2]float64{40, 20}},
		{"ratio only fills the containing block", vec(300, 75, noSize), nil, [2]float64{100, 25}},
		{"ratio only with a height", vec(300, 75, noSize), []func(*css.Style){height(10)}, [2]float64{40, 10}},
		{"no size no ratio", vec(300, 150, noSize, func(p *Picture) { p.NoRatio = true }), nil, [2]float64{300, 150}},
		{"no ratio with a width", vec(300, 150, noSize, func(p *Picture) { p.NoRatio = true }), []func(*css.Style){width(50)}, [2]float64{50, 150}},
		{"width only", vec(40, 150, func(p *Picture) { p.NoNaturalHeight, p.NoRatio = true, true }), nil, [2]float64{40, 150}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(&Node{Kind: Replaced, Style: sty(tt.mods...), Picture: tt.pic}), page(100, 400))
			b := kid(t, rb, 0, 0)
			if got := [2]float64{b.w / css.Pt, b.h / css.Pt}; !near(got[0], tt.want[0]) || !near(got[1], tt.want[1]) {
				t.Errorf("size %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVector_RatioOnlyShrinksToZero(t *testing.T) {
	// In a shrink-to-fit container a picture without natural size takes
	// no width, as Chromium sizes it.
	pic := &Node{Kind: Replaced, Style: sty(), Picture: vec(300, 75, noSize)}
	rb := boxes(t, doc(el(sty(float(css.FloatLeft)), pic)), page(100, 100))
	if b := kid(t, rb, 0, 0); b.borderW() != 0 {
		t.Errorf("float %v wide, want 0", b.borderW()/css.Pt)
	}
}

func TestVector_ObjectFit(t *testing.T) {
	contain := func(s *css.Style) { s.ObjectFit = css.FitContain }
	root := doc(&Node{Kind: Replaced, Style: sty(width(4), height(4), contain), Picture: vec(4, 2)})
	assertPixels(t, run(t, root, page(4, 4)), 1, `
		____
		RRBB
		RRBB
		____`)
}

func TestVector_Background(t *testing.T) {
	loader := func(_ context.Context, _ string) (*Picture, error) { return vec(2, 1), nil }
	root := doc(el(sty(width(6), height(2), func(s *css.Style) { s.BackgroundImage = "i.svg" })))
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(6, 2), Image: loader})
	if err != nil {
		t.Fatal(err)
	}
	assertPixels(t, res, 1, "RBRBRB\nRBRBRB")
}

func TestVector_BackgroundWithoutSizeCovers(t *testing.T) {
	loader := func(_ context.Context, _ string) (*Picture, error) {
		return vec(300, 150, noSize, func(p *Picture) { p.NoRatio = true }), nil
	}
	root := doc(el(sty(width(6), height(2), func(s *css.Style) { s.BackgroundImage = "i.svg" })))
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(6, 2), Image: loader})
	if err != nil {
		t.Fatal(err)
	}
	assertPixels(t, res, 1, "RRRBBB\nRRRBBB")
}

func TestVector_DeepGroupsLeftOut(t *testing.T) {
	deep := vec(4, 2)
	inner := deep.Vector
	deep.Vector = func(ctx context.Context, r compose.Rect) []compose.Fragment {
		f := inner(ctx, r)
		for range MaxVectorDepth + 1 {
			f = []compose.Fragment{&compose.Group{Opacity: 1, Content: f}}
		}
		return f
	}
	res := run(t, doc(&Node{Kind: Replaced, Style: sty(), Picture: deep}), page(6, 4))
	for _, pg := range res.Pages {
		if depthOf(pg.Content) > composeGroupDepth {
			t.Errorf("depth %d", depthOf(pg.Content))
		}
	}
}
