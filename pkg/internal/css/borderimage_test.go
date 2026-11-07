package css

import (
	"reflect"
	"testing"
)

func num(v float64) Dimension { return Dimension{Kind: Number, Value: v} }

func TestBorderImage_Longhands(t *testing.T) {
	t.Parallel()
	initial := BorderImage{
		Slice:  [4]Dimension{pct(1), pct(1), pct(1), pct(1)},
		Width:  [4]Dimension{num(1), num(1), num(1), num(1)},
		Outset: [4]Dimension{num(0), num(0), num(0), num(0)},
	}
	with := func(f func(*BorderImage)) BorderImage {
		b := initial
		f(&b)
		return b
	}
	tests := []struct {
		name, decls string
		want        BorderImage
	}{
		{"initial", "", initial},
		{"source", "border-image-source: url(a.png)", with(func(b *BorderImage) { b.Source = "a.png" })},
		{"slice fill first", "border-image-slice: fill 10 20%", with(func(b *BorderImage) {
			b.Slice, b.Fill = [4]Dimension{num(10), pct(0.2), num(10), pct(0.2)}, true
		})},
		{"slice fill last", "border-image-slice: 1 2 3 4 fill", with(func(b *BorderImage) {
			b.Slice, b.Fill = [4]Dimension{num(1), num(2), num(3), num(4)}, true
		})},
		{"slice negative", "border-image-slice: -1", initial},
		{"slice fill middle", "border-image-slice: 1 fill 2", initial},
		{"width", "border-image-width: 2 10px auto 5%", with(func(b *BorderImage) {
			b.Width = [4]Dimension{num(2), pts(7.5), {Kind: Auto}, pct(0.05)}
		})},
		{"outset", "border-image-outset: 1px 2", with(func(b *BorderImage) {
			b.Outset = [4]Dimension{pts(0.75), num(2), pts(0.75), num(2)}
		})},
		{"outset percent", "border-image-outset: 10%", initial},
		{"repeat", "border-image-repeat: round space", with(func(b *BorderImage) { b.Repeat = [2]ImageRepeat{ImageRound, ImageSpace} })},
		{"repeat one", "border-image-repeat: repeat", with(func(b *BorderImage) { b.Repeat = [2]ImageRepeat{ImageRepeatTile, ImageRepeatTile} })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := styleOf(t, tt.decls)
			got := s.BorderImage
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s:\n got %+v\nwant %+v", tt.decls, got, tt.want)
			}
		})
	}
}

func TestBorderImage_Shorthand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, decls string
		check       func(b BorderImage) bool
	}{
		{"full", "border-image: url(a.png) 40% 15% 20% 5% / 1 / 2px stretch round", func(b BorderImage) bool {
			return b.Source == "a.png" && b.Slice == [4]Dimension{pct(0.4), pct(0.15), pct(0.2), pct(0.05)} &&
				b.Width[0] == num(1) && b.Outset[0] == pts(1.5) && b.Repeat == [2]ImageRepeat{ImageStretch, ImageRound}
		}},
		{"resets outset", "border-image-outset: 2; border-image: url(a.png) 27 fill space", func(b BorderImage) bool {
			return b.Outset[0] == num(0) && b.Fill && b.Slice[0] == num(27) && b.Repeat[1] == ImageSpace
		}},
		{"outset only", "border-image: linear-gradient(red, blue) 33% / / 50px", func(b BorderImage) bool {
			return b.Gradient != nil && b.Width[0] == num(1) && b.Outset[0] == pts(37.5)
		}},
		{"repeat first", "border-image: repeat url(a.png)", func(b BorderImage) bool {
			return b.Repeat[0] == ImageRepeatTile && b.Slice[0] == pct(1)
		}},
		{"two slashes empty", "border-image: url(a.png) 10 / /", func(b BorderImage) bool { return b.Source == "" }},
		{"border resets", "border-image: url(a.png); border: 1px solid", func(b BorderImage) bool { return b.Source == "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := styleOf(t, tt.decls)
			if !tt.check(s.BorderImage) {
				t.Errorf("%s: %+v", tt.decls, s.BorderImage)
			}
		})
	}
}

func TestBorderImage_MaskBorder(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "mask-border: url(a.png) 20% fill")
	m := s.MaskBorder
	if m.Source != "a.png" || !m.Fill || m.Slice[0] != pct(0.2) || m.Width[0].Kind != Auto || s.BorderImage.HasImage() {
		t.Errorf("mask-border = %+v", m)
	}
	if s = styleOf(t, ""); s.MaskBorder.Slice[0] != num(0) || s.MaskBorder.Width[0].Kind != Auto {
		t.Errorf("initial mask-border = %+v", s.MaskBorder)
	}
}
