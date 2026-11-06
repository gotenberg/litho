package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// gstops returns gradient stops of colours at px positions, NaN for none.
func gstops(v ...any) []css.GradientStop {
	var out []css.GradientStop
	for i := 0; i < len(v); i += 2 {
		s := css.GradientStop{Position: css.Dimension{Kind: css.Auto}}
		if c, ok := v[i].(css.Color); ok {
			s.Color = c
		} else {
			s.Hint = true
		}
		switch p := v[i+1].(type) {
		case float64:
			if !math.IsNaN(p) {
				s.Position = css.Pts(px(p))
			}
		case css.Dimension:
			s.Position = p
		}
		out = append(out, s)
	}
	return out
}

// grad sets a background gradient.
func grad(g *css.Gradient) func(*css.Style) {
	return func(s *css.Style) { s.BackgroundGradient = g }
}

func TestGradient_LinearSharpStops(t *testing.T) {
	t.Parallel()
	g := &css.Gradient{Angle: 90, Stops: gstops(lime, 2.0, blue, 0.0, blue, 4.0, red, 0.0)}
	res := run(t, doc(el(sty(width(6), height(2), grad(g)))), page(6, 2))
	assertPixels(t, res, 1, `
		GGBBRR
		GGBBRR`)
}

func TestGradient_ToCornerAndRepeat(t *testing.T) {
	t.Parallel()
	// to bottom right on a square: the line runs along the diagonal, the
	// switch at its middle cuts the anti-diagonal.
	g := &css.Gradient{Corner: [2]int8{1, 1}, Stops: gstops(red, css.Dimension{Kind: css.Percent, Value: 0.5}, blue, css.Dimension{Kind: css.Percent, Value: 0.5})}
	res := run(t, doc(el(sty(width(4), height(4), grad(g)))), page(4, 4))
	assertPixels(t, res, 1, `
		RRR?
		RR?B
		R?BB
		?BBB`)
	// A 2px period from 1px repeats both ways along a 6px line.
	g = &css.Gradient{Angle: 90, Repeating: true, Stops: gstops(red, 1.0, red, 2.0, blue, 2.0, blue, 3.0)}
	res = run(t, doc(el(sty(width(6), height(1), grad(g)))), page(6, 1))
	assertPixels(t, res, 1, `BRBRBR`)
}

func TestGradient_RadialCircle(t *testing.T) {
	t.Parallel()
	g := &css.Gradient{Kind: css.RadialGradient, Circle: true, Size: css.RadialExplicit,
		Radius: [2]css.Dimension{css.Pts(px(2)), css.Pts(px(2))},
		Center: [2]css.Dimension{{Kind: css.Percent, Value: 0.5}, {Kind: css.Percent, Value: 0.5}},
		Stops:  gstops(red, css.Dimension{Kind: css.Percent, Value: 1}, blue, css.Dimension{Kind: css.Percent, Value: 1})}
	res := run(t, doc(el(sty(width(6), height(6), grad(g)))), page(6, 6))
	assertPixels(t, res, 1, `
		BBBBBB
		BBRRBB
		BRRRRB
		BRRRRB
		BBRRBB
		BBBBBB`)
}

func TestGradient_BackgroundSizeTilesAndClip(t *testing.T) {
	t.Parallel()
	// A 2px tile of red then blue, repeated over the padding box from the
	// content box's corner.
	g := &css.Gradient{Angle: 90, Stops: gstops(red, 1.0, blue, 1.0)}
	res := run(t, doc(el(sty(width(4), height(1), padding(1), grad(g), func(s *css.Style) {
		s.BackgroundSize = css.BackgroundSize{Kind: css.SizeExplicit, Width: css.Pts(px(2)), Height: css.Dimension{Kind: css.Auto}}
		s.BackgroundOrigin = css.ContentEdge
	}))), page(6, 3))
	assertPixels(t, res, 1, `
		BRBRBR
		BRBRBR
		BRBRBR`)
}

func TestGradient_PageBackground(t *testing.T) {
	t.Parallel()
	st := css.Initial()
	st.BackgroundGradient = &css.Gradient{Angle: 180, Stops: gstops(red, 2.0, blue, 2.0)}
	pb := page(3, 4)
	pb.Background = st
	res := run(t, doc(), pb)
	assertPixels(t, res, 1, `
		RRR
		RRR
		BBB
		BBB`)
	hidden := el(sty(display(css.DisplayNone)))
	res = run(t, hidden, pb)
	assertPixels(t, res, 1, `
		___
		___
		___
		___`)
}

func TestGradient_ColorStops(t *testing.T) {
	t.Parallel()
	e := newEngine(t.Context(), nil, Options{}, 100, 100)
	for _, tc := range []struct {
		name  string
		g     *css.Gradient
		solid compose.Color
	}{
		{"one stop", &css.Gradient{Stops: gstops(red, math.NaN())}, compose.Color{R: 1, A: 1}},
		{"repeating without period: equidistant average", &css.Gradient{Repeating: true, Stops: gstops(black, 3.0, black, 3.0, css.Color{R: 0.5, B: 0.5, A: 1}, 3.0, css.Color{R: 0.5, B: 0.5, A: 1}, 3.0)},
			compose.Color{R: 0.25, B: 0.25, A: 1}},
		{"radial before its centre", &css.Gradient{Stops: gstops(red, -3.0, blue, -1.0)}, compose.Color{B: 1, A: 1}},
	} {
		_, _, _, solid, ok := e.colorStops(tc.g, px(10), 0, px(10), tc.name == "radial before its centre", 0)
		if ok || math.Abs(solid.R-tc.solid.R) > 1e-6 || math.Abs(solid.B-tc.solid.B) > 1e-6 || solid.A != tc.solid.A {
			t.Errorf("%s: solid %v %v, want %v", tc.name, solid, ok, tc.solid)
		}
	}
	// Too many periods fall back to the average colour, with a warning.
	g := &css.Gradient{Repeating: true, Stops: gstops(red, 0.0, blue, 0.01)}
	if _, _, _, _, ok := e.colorStops(g, 1000, 0, 1000, false, 0); ok || len(e.warnings) != 1 {
		t.Errorf("unrolled %v, warnings %v", ok, e.warnings)
	}
	// A hint and a change of alpha split their ramp; the stops stay in
	// order within [0, 1].
	g = &css.Gradient{Stops: gstops(red, 0.0, nil, 2.0, css.Color{B: 1, A: 0.5}, 10.0)}
	stops, lo, hi, _, ok := e.colorStops(g, px(10), 0, px(10), false, 0)
	if !ok || len(stops) < gradientSplits || !(hi > lo) || stops[0].Offset != 0 || stops[len(stops)-1].Offset != 1 {
		t.Fatalf("stops %v over [%v, %v]", stops, lo, hi)
	}
	sh := &compose.Shading{To: compose.Point{X: 1}, Stops: stops}
	doc := &compose.Document{Pages: []*compose.Page{{Width: 10, Height: 10, Content: []compose.Fragment{
		&compose.Path{Ops: rectOps(compose.Rect{Width: 10, Height: 10}), Fill: &compose.Fill{Shading: sh}},
	}}}}
	if err := doc.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
