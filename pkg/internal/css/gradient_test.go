package css

import (
	"math"
	"reflect"
	"testing"
)

func TestGradient_Parses(t *testing.T) {
	t.Parallel()
	blue, red, black := rgb(0, 0, 255), rgb(255, 0, 0), rgb(0, 0, 0)
	auto := Dimension{Kind: Auto}
	stop := func(c Color, d Dimension) GradientStop { return GradientStop{Color: c, Position: d} }
	center := [2]Dimension{pct(0.5), pct(0.5)}
	for _, tc := range []struct {
		decl string
		want *Gradient
	}{
		{"background-image: linear-gradient(red, blue)", &Gradient{Angle: 180, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background: linear-gradient(45deg, red 10px, blue)", &Gradient{Angle: 45, Stops: []GradientStop{stop(red, pts(7.5)), stop(blue, auto)}}},
		{"background-image: linear-gradient(-0.25turn, red, blue)", &Gradient{Angle: 270, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: linear-gradient(to left, red, blue)", &Gradient{Angle: 270, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: linear-gradient(to top, red, blue)", &Gradient{Angle: 0, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: linear-gradient(to bottom left, red, blue)", &Gradient{Angle: 180, Corner: [2]int8{-1, 1}, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: repeating-linear-gradient(red 0 1em, 50%, currentcolor)", &Gradient{Angle: 180, Repeating: true, Stops: []GradientStop{
			stop(red, pts(0)), stop(red, pts(15)), {Position: pct(0.5), Hint: true}, stop(blue, auto),
		}}},
		{"background-image: linear-gradient(red)", &Gradient{Angle: 180, Stops: []GradientStop{stop(red, auto)}}},
		{"background-image: radial-gradient(red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Center: center, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: radial-gradient(circle closest-side at left 25%, red, blue)", &Gradient{
			Kind: RadialGradient, Angle: 180, Circle: true, Size: ClosestSide,
			Center: [2]Dimension{pct(0), pct(0.25)}, Stops: []GradientStop{stop(red, auto), stop(blue, auto)},
		}},
		{"background-image: radial-gradient(10px, red, blue)", &Gradient{
			Kind: RadialGradient, Angle: 180, Circle: true, Size: RadialExplicit,
			Radius: [2]Dimension{pts(7.5), pts(7.5)}, Center: center, Stops: []GradientStop{stop(red, auto), stop(blue, auto)},
		}},
		{"background-image: repeating-radial-gradient(ellipse 50% 20px at 0 0, black, red)", &Gradient{
			Kind: RadialGradient, Angle: 180, Repeating: true, Size: RadialExplicit,
			Radius: [2]Dimension{pct(0.5), pts(15)}, Center: [2]Dimension{pts(0), pts(0)}, Stops: []GradientStop{stop(black, auto), stop(red, auto)},
		}},
		{"background-image: conic-gradient(red, blue)", &Gradient{Kind: ConicGradient, Center: center, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: repeating-conic-gradient(from -90deg at 10px 25%, red 0 25%, blue 90deg calc(50% + 0.25turn), 75%, black 1rad)", &Gradient{
			Kind: ConicGradient, Repeating: true, Angle: 270, Center: [2]Dimension{pts(7.5), pct(0.25)},
			Stops: []GradientStop{stop(red, pct(0)), stop(red, pct(0.25)), stop(blue, pct(0.25)), stop(blue, pct(0.75)), {Position: pct(0.75), Hint: true}, stop(black, pct(1/(2*math.Pi)))},
		}},
		{"background-image: conic-gradient(at left in oklch longer hue, red, blue)", &Gradient{Kind: ConicGradient, Center: [2]Dimension{pct(0), pct(0.5)}, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: linear-gradient(in hsl to right, red, blue)", &Gradient{Angle: 90, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: radial-gradient(circle in srgb-linear, red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Circle: true, Center: center, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		// CSS Images 4, 2.5: image(<color>) is a solid-colour image.
		{"background-image: image(blue)", &Gradient{Angle: 180, Stops: []GradientStop{stop(blue, auto), stop(blue, auto)}}},
		{"background-image: image-set(linear-gradient(red, red) 2x, linear-gradient(blue, blue) 1x)", &Gradient{Angle: 180, Stops: []GradientStop{stop(blue, auto), stop(blue, auto)}}},
	} {
		s := styleOf(t, tc.decl)
		if s.BackgroundImage != "" || !reflect.DeepEqual(exported(s.BackgroundGradient), tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.decl, s.BackgroundGradient, tc.want)
		}
	}
}

func TestGradient_RejectsInvalid(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{
		"linear-gradient()",
		"linear-gradient(45deg)",
		"linear-gradient(to middle, red, blue)",
		"linear-gradient(to left right, red, blue)",
		"linear-gradient(45deg 10deg, red, blue)",
		"linear-gradient(red, 10%)",
		"linear-gradient(10%, red)",
		"linear-gradient(red, 10%, 20%, blue)",
		"linear-gradient(red 1px 2px 3px, blue)",
		"radial-gradient(circle 10%, red, blue)",
		"radial-gradient(circle 1px 2px, red, blue)",
		"radial-gradient(ellipse 1px, red, blue)",
		"radial-gradient(circle circle, red, blue)",
		"radial-gradient(at nowhere, red, blue)",
		"radial-gradient(closest-side 1px, red, blue)",
	} {
		s := styleOf(t, "background-image: url(a.png); background-image: "+decl)
		if s.BackgroundGradient != nil || s.BackgroundImage == "" {
			t.Errorf("%s was not rejected: %+v", decl, s.BackgroundGradient)
		}
	}
}

// exported returns a copy of g without its unexported fields, which the
// tables of TestGradient_Parses leave out.
func exported(g *Gradient) *Gradient {
	if g == nil {
		return nil
	}
	c := *g
	c.space, c.hue, c.interpolated = 0, 0, false
	c.Stops = make([]GradientStop, len(g.Stops))
	for i, s := range g.Stops {
		s.sc = spaceColor{}
		c.Stops[i] = s
	}
	return &c
}

// TestGradient_InterpolationSpace locks the colour space a gradient
// interpolates in (CSS Images 4, 3.5.2; CSS Color 4, 13.3): the one its
// method names, else sRGB for legacy colours and Oklab for any other, a
// colour with a missing component being no legacy colour (4.1.2),
// and a hue taken from the other stop where one is missing (13.4).
func TestGradient_InterpolationSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl         string
		interpolated bool
		mid          Color
	}{
		{"linear-gradient(red, blue)", false, Color{R: 0.5, B: 0.5, A: 1}},
		{"linear-gradient(#f00, hwb(240 0% 0%))", false, Color{R: 0.5, B: 0.5, A: 1}},
		{"linear-gradient(in srgb, red, blue)", false, Color{R: 0.5, B: 0.5, A: 1}},
		{"linear-gradient(red, color(srgb 0 0 1))", true, Color{R: 0.5504, G: 0.3256, B: 0.6365, A: 1}},
		{"linear-gradient(in oklch, red, blue)", true, Color{R: 0.7299, B: 0.7601, A: 1}},
		{"linear-gradient(in hsl longer hue, red, blue)", true, Color{G: 1, A: 1}},
		{"linear-gradient(rgb(none 255 0), rgb(255, 0, 0))", true, Color{R: 0.8163, G: 0.6604, B: 0.0018, A: 1}},
		{"linear-gradient(in srgb, rgb(none 0 0), rgb(200 100 50))", true, Color{R: 200.0 / 255, G: 50.0 / 255, B: 25.0 / 255, A: 1}},
	}
	for _, tt := range tests {
		g := styleOf(t, "background-image: "+tt.decl).BackgroundGradient
		if g == nil || g.Interpolated() != tt.interpolated {
			t.Errorf("%s: %+v", tt.decl, g)
			continue
		}
		if got := g.Mix(0, 1, 0.5); !nearColor(got, tt.mid) {
			t.Errorf("%s: midpoint %+v, want %+v", tt.decl, got, tt.mid)
		}
	}
}

func TestGradient_URLClearsGradient(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "background: linear-gradient(red, blue); background-image: url(a.png)")
	if s.BackgroundGradient != nil || s.BackgroundImage == "" {
		t.Errorf("gradient %+v image %q", s.BackgroundGradient, s.BackgroundImage)
	}
}

// TestGradient_LegacyPrefixed checks the prefixed legacy linear and radial
// gradients, read as Chromium reads them: an angle counterclockwise from
// the right, the side or corner a line starts at, a corner line running
// from corner to corner, and a radial centre then shape and size before
// the stops.
func TestGradient_LegacyPrefixed(t *testing.T) {
	t.Parallel()
	red, blue := rgb(255, 0, 0), rgb(0, 0, 255)
	auto := Dimension{Kind: Auto}
	stops := []GradientStop{{Color: red, Position: auto}, {Color: blue, Position: auto}}
	center := [2]Dimension{pct(0.5), pct(0.5)}
	for _, tc := range []struct {
		decl string
		want *Gradient
	}{
		{"-webkit-linear-gradient(red, blue)", &Gradient{Angle: 180, Stops: stops}},
		{"-webkit-linear-gradient(top, red, blue)", &Gradient{Angle: 180, Stops: stops}},
		{"-webkit-linear-gradient(left, red, blue)", &Gradient{Angle: 90, Stops: stops}},
		{"-webkit-linear-gradient(right, red, blue)", &Gradient{Angle: 270, Stops: stops}},
		{"-webkit-linear-gradient(bottom, red, blue)", &Gradient{Angle: 0, Stops: stops}},
		{"-webkit-linear-gradient(0deg, red, blue)", &Gradient{Angle: 90, Stops: stops}},
		{"-webkit-linear-gradient(90deg, red, blue)", &Gradient{Angle: 0, Stops: stops}},
		{"-webkit-linear-gradient(30deg, red, blue)", &Gradient{Angle: 60, Stops: stops}},
		{"-webkit-linear-gradient(left top, red, blue)", &Gradient{Angle: 180, Corner: [2]int8{1, 1}, Diagonal: true, Stops: stops}},
		{"-webkit-linear-gradient(bottom right, red, blue)", &Gradient{Angle: 180, Corner: [2]int8{-1, -1}, Diagonal: true, Stops: stops}},
		{"-webkit-repeating-linear-gradient(left, red, blue)", &Gradient{Angle: 90, Repeating: true, Stops: stops}},
		{"-webkit-radial-gradient(red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Center: center, Stops: stops}},
		{"-webkit-radial-gradient(center, circle cover, red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Circle: true, Center: center, Stops: stops}},
		{"-webkit-radial-gradient(left top, circle contain, red, blue)", &Gradient{
			Kind: RadialGradient, Angle: 180, Circle: true, Size: ClosestSide, Center: [2]Dimension{pct(0), pct(0)}, Stops: stops,
		}},
		{"-webkit-radial-gradient(20px 30px, red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Center: [2]Dimension{pts(15), pts(22.5)}, Stops: stops}},
		{"-webkit-radial-gradient(center, 20px 30px, red, blue)", &Gradient{
			Kind: RadialGradient, Angle: 180, Size: RadialExplicit, Radius: [2]Dimension{pts(15), pts(22.5)}, Center: center, Stops: stops,
		}},
		{"-webkit-radial-gradient(ellipse closest-corner, red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Size: ClosestCorner, Center: center, Stops: stops}},
	} {
		s := styleOf(t, "background-image: "+tc.decl)
		if !reflect.DeepEqual(exported(s.BackgroundGradient), tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.decl, s.BackgroundGradient, tc.want)
		}
	}
	for _, decl := range []string{
		"-webkit-linear-gradient(to right, red, blue)",
		"-webkit-linear-gradient(left left, red, blue)",
		"-webkit-radial-gradient(circle at center, red, blue)",
		"-webkit-linear-gradient(top 10px, red, blue)",
	} {
		if s := styleOf(t, "background-image: "+decl); s.BackgroundGradient != nil {
			t.Errorf("%s: parsed as %+v", decl, s.BackgroundGradient)
		}
	}
}
