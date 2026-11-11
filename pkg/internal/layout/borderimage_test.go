package layout

import (
	"context"
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestBorderImage_TileSpans(t *testing.T) {
	tests := []struct {
		name              string
		rep               css.ImageRepeat
		start, size, n, k float64
		want              []span
	}{
		{"stretch", css.ImageStretch, 1, 7, 2, 1, []span{{1, 7}}},
		{"round half up", css.ImageRound, 0, 7, 2, 1, []span{{0, 1.75}, {1.75, 1.75}, {3.5, 1.75}, {5.25, 1.75}}},
		{"round at least one", css.ImageRound, 0, 1, 4, 1, []span{{0, 1}}},
		{"space", css.ImageSpace, 0, 7, 2, 1, []span{{0.25, 2}, {2.5, 2}, {4.75, 2}}},
		{"space none fits", css.ImageSpace, 0, 3, 2, 2, nil},
		{"repeat centred", css.ImageRepeatTile, 0, 4, 2, 1, []span{{-1, 2}, {1, 2}, {3, 2}}},
		{"repeat odd", css.ImageRepeatTile, 0, 3, 2, 1, []span{{-1.5, 2}, {0.5, 2}, {2.5, 2}}},
		{"zero scale stretches", css.ImageRepeatTile, 0, 3, 2, 0, []span{{0, 3}}},
		{"empty region", css.ImageRound, 0, 0, 2, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tileSpans(tt.rep, tt.start, tt.size, tt.n, tt.k)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if !near(got[i].at, tt.want[i].at) || !near(got[i].size, tt.want[i].size) {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestBorderImage_Widths(t *testing.T) {
	area := compose.Rect{Width: 10, Height: 6}
	bd := [4]float64{1, 2, 1, 2}
	all := [4]bool{true, true, true, true}
	src := nineSource{w: 4, h: 4, natural: true}
	slices := [4]float64{1, 1, 1, 1}
	tests := []struct {
		name  string
		width [4]css.Dimension
		draw  [4]bool
		want  [4]float64
	}{
		{"numbers", [4]css.Dimension{{Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 2}, {Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 0}}, all, [4]float64{1, 4, 1, 0}},
		{"percent and auto", [4]css.Dimension{{Kind: css.Percent, Value: 0.5}, {Kind: css.Auto}, css.Pts(1), {Kind: css.Percent, Value: 0.1}}, all, [4]float64{3, 1, 1, 1}},
		{"overlap scales down", [4]css.Dimension{css.Pts(6), css.Pts(1), css.Pts(6), css.Pts(1)}, all, [4]float64{3, 0.5, 3, 0.5}},
		{"cut side", [4]css.Dimension{css.Pts(1), css.Pts(1), css.Pts(1), css.Pts(1)}, [4]bool{true, false, true, true}, [4]float64{1, 0, 1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bi := &css.BorderImage{Width: tt.width}
			if got := imageWidths(bi, src, slices, bd, tt.draw, area); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// nineLoader serves a 3 by 3 px image: a red ring around a blue centre,
// its top-left pixel lime.
func nineLoader() func(context.Context, string) (*Picture, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := range 3 {
		for x := range 3 {
			img.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	img.Set(1, 1, color.NRGBA{B: 255, A: 255})
	img.Set(0, 0, color.NRGBA{G: 255, A: 255})
	p := &Picture{Source: source(img), Width: px(3), Height: px(3)}
	return func(context.Context, string) (*Picture, error) { return p, nil }
}

func TestBorderImage_Draw(t *testing.T) {
	slice1 := [4]css.Dimension{{Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 1}}
	ones := [4]css.Dimension{{Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 1}, {Kind: css.Number, Value: 1}}
	tests := []struct {
		name string
		mods func(*css.Style)
		want string
	}{
		{"stretch replaces the border", func(s *css.Style) {
			s.BorderImage = css.BorderImage{Source: "i.png", Slice: slice1, Width: ones}
		}, `
			______
			_GRRR_
			_R__R_
			_RRRR_
			______`},
		{"fill", func(s *css.Style) {
			s.BorderImage = css.BorderImage{Source: "i.png", Slice: slice1, Width: ones, Fill: true}
		}, `
			______
			_GRRR_
			_RBBR_
			_RRRR_
			______`},
		{"outset", func(s *css.Style) {
			s.BorderImage = css.BorderImage{
				Source: "i.png", Slice: slice1, Width: ones,
				Outset: [4]css.Dimension{css.Pts(px(1)), {Kind: css.Number}, {Kind: css.Number}, {Kind: css.Number}},
			}
		}, `
			_GRRR_
			_R__R_
			_R__R_
			_RRRR_
			______`},
		{"missing image draws the border", func(s *css.Style) {
			s.BorderImage = css.BorderImage{Source: "missing.png", Slice: slice1, Width: ones}
		}, `
			______
			_KKKK_
			_K__K_
			_KKKK_
			______`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(el(sty(width(2), height(1), margin(1), border(1, css.Solid, css.Color{A: 1}), tt.mods)))
			pics := nineLoader()
			loader := func(ctx context.Context, u string) (*Picture, error) {
				if u == "missing.png" {
					return nil, nil
				}
				return pics(ctx, u)
			}
			res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(6, 5), Image: loader})
			if err != nil {
				t.Fatal(err)
			}
			assertPixels(t, res, 1, tt.want)
		})
	}
}

func TestBorderImage_Gradient(t *testing.T) {
	// A gradient has no natural size: it takes the border image area's,
	// 6 by 4 px here, its top half red, and a 25% slice cuts it.
	g := &css.Gradient{Angle: 180, Stops: []css.GradientStop{
		{Color: red, Position: css.Dimension{Kind: css.Percent}},
		{Color: red, Position: css.Dimension{Kind: css.Percent, Value: 0.5}},
		{Color: blue, Position: css.Dimension{Kind: css.Percent, Value: 0.5}},
		{Color: blue, Position: css.Dimension{Kind: css.Percent, Value: 1}},
	}}
	q := css.Dimension{Kind: css.Percent, Value: 0.25}
	two := css.Dimension{Kind: css.Number, Value: 1}
	root := doc(el(sty(width(4), height(2), margin(1), border(1, css.Solid, css.Color{A: 1}), func(s *css.Style) {
		s.BorderImage = css.BorderImage{Gradient: g, Slice: [4]css.Dimension{q, q, q, q}, Width: [4]css.Dimension{two, two, two, two}}
	})))
	assertPixels(t, run(t, root, page(8, 6)), 1, `
		________
		_RRRRRR_
		_R____R_
		_B____B_
		_BBBBBB_
		________`)
}

func TestBorderImage_CropSource(t *testing.T) {
	pic, _ := nineLoader()(context.Background(), "i.png")
	p := &painter{e: &engine{ctx: context.Background()}}
	decode := func() image.Image {
		img, err := pic.Source.Decode(context.Background())
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return img
	}
	tests := []struct {
		name string
		r    compose.Rect
		want image.Rectangle
		same bool
	}{
		{"corner", compose.Rect{Width: px(1), Height: px(1)}, image.Rect(0, 0, 1, 1), false},
		{"middle rounded", compose.Rect{X: px(0.9), Y: px(1.1), Width: px(1.2), Height: px(0.8)}, image.Rect(0, 0, 1, 1), false},
		{"whole", compose.Rect{Width: px(3), Height: px(3)}, image.Rect(0, 0, 3, 3), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := p.cropSource(pic, tt.r, decode)
			if c == nil || image.Rect(0, 0, c.Width, c.Height) != tt.want || (c == pic.Source) != tt.same {
				t.Fatalf("got %+v", c)
			}
		})
	}
	if c := p.cropSource(pic, compose.Rect{Width: px(1), Height: px(1)}, decode); c != p.cropSource(pic, compose.Rect{Width: px(1), Height: px(1)}, decode) {
		t.Error("a part is cut once")
	}
	crop, err := p.cropSource(pic, compose.Rect{X: px(1), Width: px(1), Height: px(1)}, decode).Decode(context.Background())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := crop.At(0, 0); got != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("top edge pixel = %v, want red", got)
	}
}

func TestBorderImage_InlineStrip(t *testing.T) {
	// An inline box broken across two lines draws its border image over
	// the strip of its fragments end to end: a left-to-right gradient is
	// red where the first line's fragment ends and blue where the second
	// one's ends (CSS Backgrounds 3, 6.6).
	g := &css.Gradient{Angle: 90, Stops: []css.GradientStop{
		{Color: red, Position: css.Dimension{Kind: css.Auto}},
		{Color: blue, Position: css.Dimension{Kind: css.Auto}},
	}}
	one := css.Dimension{Kind: css.Number, Value: 1}
	sp := inlineSty(border(2, css.Solid, css.Color{A: 1}), func(s *css.Style) {
		s.BorderImage = css.BorderImage{Gradient: g, Slice: [4]css.Dimension{one, one, one, one}, Width: [4]css.Dimension{one, one, one, one}}
		s.Color = css.Color{}
	})
	root := doc(el(sty(width(40), func(s *css.Style) { s.LineHeight = css.Pts(px(20)) }), el(sp, txt(sp, "XX XX"))))
	img := render(t, run(t, root, page(60, 40)), 1)
	// Line 1: the fragment runs from x 0 to 22 (a left border and two
	// glyphs), its top edge at y 3; line 2's from 0 to 22 at y 23, with a
	// right border alone.
	end1, end2 := at(img, 19, 3), at(img, 19, 23)
	if !(end1.R > end1.B) || !(end2.B > end2.R) {
		t.Errorf("fragment ends: line 1 %v, line 2 %v; want reddish then bluish", end1, end2)
	}
	if c := at(img, 21, 8); c != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("line 1 draws a right edge: %v", c)
	}
	if c := at(img, 0, 28); c != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("line 2 draws a left edge: %v", c)
	}
}
