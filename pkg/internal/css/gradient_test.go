package css

import (
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
			stop(red, pts(0)), stop(red, pts(15)), {Position: pct(0.5), Hint: true}, stop(blue, auto)}}},
		{"background-image: linear-gradient(red)", &Gradient{Angle: 180, Stops: []GradientStop{stop(red, auto)}}},
		{"background-image: radial-gradient(red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Center: center, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: radial-gradient(circle closest-side at left 25%, red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Circle: true, Size: ClosestSide,
			Center: [2]Dimension{pct(0), pct(0.25)}, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: radial-gradient(10px, red, blue)", &Gradient{Kind: RadialGradient, Angle: 180, Circle: true, Size: RadialExplicit,
			Radius: [2]Dimension{pts(7.5), pts(7.5)}, Center: center, Stops: []GradientStop{stop(red, auto), stop(blue, auto)}}},
		{"background-image: repeating-radial-gradient(ellipse 50% 20px at 0 0, black, red)", &Gradient{Kind: RadialGradient, Angle: 180, Repeating: true, Size: RadialExplicit,
			Radius: [2]Dimension{pct(0.5), pts(15)}, Center: [2]Dimension{pts(0), pts(0)}, Stops: []GradientStop{stop(black, auto), stop(red, auto)}}},
	} {
		s := styleOf(t, tc.decl)
		if s.BackgroundImage != "" || !reflect.DeepEqual(s.BackgroundGradient, tc.want) {
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

func TestGradient_ConicWarnsAndComputesToNone(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=t></div>`, `#t { background: conic-gradient(red, blue) }`)
	if s := c.of("t"); s.BackgroundGradient != nil || s.BackgroundImage != "" {
		t.Errorf("conic gradient computed to %+v %q", s.BackgroundGradient, s.BackgroundImage)
	}
	if !hasWarning(c.c.Warnings(), "unsupported-css-value", "conic-gradient") {
		t.Errorf("no warning in %v", c.c.Warnings())
	}
}

func TestGradient_URLClearsGradient(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "background: linear-gradient(red, blue); background-image: url(a.png)")
	if s.BackgroundGradient != nil || s.BackgroundImage == "" {
		t.Errorf("gradient %+v image %q", s.BackgroundGradient, s.BackgroundImage)
	}
}
