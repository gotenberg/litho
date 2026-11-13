package css

import (
	"strings"
	"testing"
)

func TestClipPath_Values(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		check func(c ClipPath) bool
	}{
		{"", func(c ClipPath) bool { return c.None() }},
		{"clip-path: none", func(c ClipPath) bool { return c.None() }},
		{"clip-path: url(#c)", func(c ClipPath) bool { return strings.HasSuffix(c.URL, "#c") && c.Shape == nil }},
		{"clip-path: padding-box", func(c ClipPath) bool { return c.Boxed && c.Shape == nil && c.Box == GeoPadding }},
		{"clip-path: fill-box", func(c ClipPath) bool { return c.Box == GeoContent }},
		{"clip-path: view-box", func(c ClipPath) bool { return c.Box == GeoBorder }},
		{"clip-path: inset(10px 20%)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Kind == ShapeInset && s.Inset[Top] == pts(7.5) && s.Inset[Right] == pct(0.2) && s.Inset[Bottom] == pts(7.5) && c.Box == GeoBorder
		}},
		{"clip-path: content-box inset(1px round 4px / 2px)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && c.Box == GeoContent && s.Radii[TopLeft] == Radius{H: pts(3), V: pts(1.5)}
		}},
		{"clip-path: circle(50% at right 20%)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Kind == ShapeCircle && s.Radius[0] == ShapeRadius{Side: RadiusLength, Length: pct(0.5)} && s.Center == [2]Dimension{pct(1), pct(0.2)}
		}},
		{"clip-path: circle()", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Radius[0].Side == RadiusClosest && s.Center == [2]Dimension{pct(0.5), pct(0.5)}
		}},
		{"clip-path: circle(farthest-corner at 10px 20px)", func(c ClipPath) bool {
			return c.Shape != nil && c.Shape.Radius[0].Side == RadiusFarthestCorner
		}},
		{"clip-path: ellipse(farthest-side 10px)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Kind == ShapeEllipse && s.Radius[0].Side == RadiusFarthest && s.Radius[1].Length == pts(7.5)
		}},
		{"clip-path: polygon(evenodd, 0 0, 100% 0, 50% 4px)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.EvenOdd && len(s.Points) == 3 && s.Points[2] == [2]Dimension{pct(0.5), pts(3)}
		}},
		{`clip-path: path(nonzero, "M0 0 L10 0 L0 10 Z")`, func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Kind == ShapePath && !s.EvenOdd && s.Path == "M0 0 L10 0 L0 10 Z"
		}},
		// rect() and xywh() compute to an inset of the same rectangle.
		{"clip-path: rect(10px auto 50% 0)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Kind == ShapeInset && s.Inset[Top] == pts(7.5) && s.Inset[Right] == pct(0) && s.Inset[Bottom] == pct(0.5) && s.Inset[Left] == pts(0)
		}},
		{"clip-path: xywh(10px 0 50% 20px)", func(c ClipPath) bool {
			s := c.Shape
			return s != nil && s.Inset[Left] == pts(7.5) && s.Inset[Right] == Dimension{Kind: Calc, Value: -7.5, Fraction: 0.5} &&
				s.Inset[Bottom] == Dimension{Kind: Calc, Value: -15, Fraction: 1}
		}},
	}
	for _, tt := range tests {
		if c := styleOf(t, tt.decl).ClipPath; !tt.check(c) {
			t.Errorf("%q: %+v %+v", tt.decl, c, c.Shape)
		}
	}
}

func TestClipPath_Invalid(t *testing.T) {
	t.Parallel()
	for _, v := range []string{
		"circle(1px 2px)", "ellipse(1px)", "inset()", "inset(1px 2px 3px 4px 5px)", "polygon()",
		"polygon(1px)", "polygon(evenodd)", "polygon(foo, 0 0)", "path(10)", "rect(1px 2px 3px)",
		"xywh(0 0 -1px 2px)", "border-box padding-box", "circle() circle()", "url(#a) border-box",
		"circle(-1px)", "inset(1px round)", "ellipse(closest-corner 1px)",
	} {
		if c := styleOf(t, "clip-path: inset(1px); clip-path: "+v).ClipPath; c.Shape == nil || c.Shape.Kind != ShapeInset {
			t.Errorf("%q was accepted: %+v", v, c)
		}
	}
}

func TestClip_Values(t *testing.T) {
	t.Parallel()
	auto := Dimension{Kind: Auto}
	tests := []struct {
		decl string
		want ClipRect
	}{
		{"", ClipRect{}},
		{"clip: auto", ClipRect{}},
		{"clip: rect(1px, 2px, 3px, 4px)", ClipRect{Set: true, Edges: [4]Dimension{pts(0.75), pts(1.5), pts(2.25), pts(3)}}},
		{"clip: rect(1px 2px 3px 4px)", ClipRect{Set: true, Edges: [4]Dimension{pts(0.75), pts(1.5), pts(2.25), pts(3)}}},
		{"clip: rect(auto, auto, 3px, auto)", ClipRect{Set: true, Edges: [4]Dimension{auto, auto, pts(2.25), auto}}},
		{"clip: rect(1px, 2px, 3px, 4px); clip: rect(1%, 2px, 3px, 4px)", ClipRect{Set: true, Edges: [4]Dimension{pts(0.75), pts(1.5), pts(2.25), pts(3)}}},
		{"clip: rect(1px, 2px, 3px, 4px); clip: rect(1px, 2px, 3px)", ClipRect{Set: true, Edges: [4]Dimension{pts(0.75), pts(1.5), pts(2.25), pts(3)}}},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decl).Clip; got != tt.want {
			t.Errorf("%q: %+v, want %+v", tt.decl, got, tt.want)
		}
	}
}

// TestClipPath_PolygonRound locks the polygon() grammar of CSS Shapes 1,
// 3.1: an optional fill rule, then round and a length, before the first
// comma.
func TestClipPath_PolygonRound(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]float64{
		"polygon(round 40px, 0 0, 100% 0, 0 100%)":        30,
		"polygon(evenodd round 4px, 0 0, 100% 0, 0 100%)": 3,
		"polygon(0 0, 100% 0, 0 100%)":                    0,
	} {
		c := styleOf(t, "clip-path: "+v).ClipPath
		if c.Shape == nil || c.Shape.Round != want || len(c.Shape.Points) != 3 {
			t.Errorf("%s = %+v, want round %v", v, c.Shape, want)
		}
	}
	if c := styleOf(t, "clip-path: polygon(round, 0 0, 1px 0, 0 1px)").ClipPath; c.Shape != nil {
		t.Errorf("round without a length parsed: %+v", c.Shape)
	}
}
