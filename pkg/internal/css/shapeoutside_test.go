package css

import "testing"

// TestShapeOutside_Parse holds shape-outside to CSS Shapes 1, 6.1: none,
// a <basic-shape> and a <shape-box> in either order, each optional, the
// reference box defaulting to margin-box, or an <image>; the SVG
// <geometry-box> keywords are no <shape-box>.
func TestShapeOutside_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		set   bool
		kind  ShapeKind
		shape bool
		box   GeometryBox
		image bool
	}{
		{"shape-outside: none", false, 0, false, 0, false},
		{"shape-outside: circle(50%)", true, ShapeCircle, true, GeoMargin, false},
		{"shape-outside: content-box", true, 0, false, GeoContent, false},
		{"shape-outside: border-box inset(10px)", true, ShapeInset, true, GeoBorder, false},
		{"shape-outside: polygon(0 0, 10px 0, 0 10px) padding-box", true, ShapePolygon, true, GeoPadding, false},
		{"shape-outside: path('M0 0 L10 0 L0 10 Z')", true, ShapePath, true, GeoMargin, false},
		{"shape-outside: shape(from 0 0, line to 10px 0, line by 0 10px, close)", true, ShapeShape, true, GeoMargin, false},
		{"shape-outside: linear-gradient(black, transparent)", true, 0, false, GeoContent, true},
		{"shape-outside: url(a.png)", true, 0, false, GeoContent, true},
		{"shape-outside: fill-box", false, 0, false, 0, false},
		{"shape-outside: circle() ellipse()", false, 0, false, 0, false},
		{"shape-outside: border-box content-box", false, 0, false, 0, false},
	}
	for _, tt := range tests {
		so := styleOf(t, tt.decl).ShapeOutside
		if !tt.set {
			if so != nil && so.Set {
				t.Errorf("%s set, want none", tt.decl)
			}
			continue
		}
		switch {
		case so == nil || !so.Set:
			t.Errorf("%s = none, want a shape", tt.decl)
		case (so.Shape != nil) != tt.shape || tt.shape && so.Shape.Kind != tt.kind:
			t.Errorf("%s shape %+v, want kind %v", tt.decl, so.Shape, tt.kind)
		case so.Box != tt.box || so.HasImage() != tt.image:
			t.Errorf("%s box %v image %v, want %v %v", tt.decl, so.Box, so.HasImage(), tt.box, tt.image)
		}
	}
}

// TestShapeOutside_MarginAndThreshold holds shape-margin to a non-negative
// <length-percentage> (CSS Shapes 1, 6.3) and shape-image-threshold to an
// <opacity-value> clamped to [0, 1] (6.2); a style that sets neither, nor
// shape-outside, allocates nothing.
func TestShapeOutside_MarginAndThreshold(t *testing.T) {
	t.Parallel()
	if so := styleOf(t, "shape-margin: 0; shape-image-threshold: 0").ShapeOutside; so != nil {
		t.Errorf("initial values allocate %+v", so)
	}
	so := styleOf(t, "shape-margin: 10px; shape-image-threshold: 150%").ShapeOutside
	if so == nil || !near(so.Margin.Value/Pt, 10) || so.Threshold != 1 {
		t.Fatalf("shape-margin 10px, threshold 150%% = %+v", so)
	}
	if so := styleOf(t, "shape-margin: 20%").ShapeOutside; so == nil || so.Margin.Kind != Percent || so.Margin.Value != 0.2 {
		t.Errorf("shape-margin 20%% = %+v", so)
	}
	if so := styleOf(t, "shape-margin: -1px").ShapeOutside; so != nil {
		t.Errorf("negative shape-margin accepted: %+v", so)
	}
}
