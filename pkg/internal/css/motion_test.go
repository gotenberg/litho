package css

import (
	"math"
	"testing"
)

// TestMotionPath_Computes holds the longhands of CSS Motion Path 1, 2 and
// the offset shorthand (2.6) to their grammars.
func TestMotionPath_Computes(t *testing.T) {
	t.Parallel()
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	tests := []struct {
		decls string
		check func(m *MotionPath) bool
	}{
		{"", func(m *MotionPath) bool { return m == nil }},
		{"offset-path: none; offset-distance: 0; offset-rotate: auto; offset-anchor: auto; offset-position: normal", func(m *MotionPath) bool { return m == nil }},
		{"offset-path: path('M 0 0 L 10 10')", func(m *MotionPath) bool {
			return m.Path.Kind == OffsetShape && m.Path.Shape.Kind == ShapePath && m.Path.Shape.Path == "M 0 0 L 10 10" && m.Path.Box == GeoBorder
		}},
		{"offset-path: circle(10px) padding-box", func(m *MotionPath) bool {
			return m.Path.Kind == OffsetShape && m.Path.Shape.Kind == ShapeCircle && m.Path.Box == GeoPadding
		}},
		{"offset-path: content-box", func(m *MotionPath) bool { return m.Path.Kind == OffsetBox && m.Path.Box == GeoContent }},
		{"offset-path: margin-box", func(m *MotionPath) bool { return m == nil }},
		{"offset-path: url(#p)", func(m *MotionPath) bool { return m.Path.Kind == OffsetURL }},
		{"offset-path: ray(45deg)", func(m *MotionPath) bool {
			r := m.Path.Ray
			return m.Path.Kind == OffsetRay && near(r.Angle, rad(45)) && r.Size == RayClosestSide && !r.Contain && !r.AtSet
		}},
		{"offset-path: ray(farthest-corner contain at 10px 20% 0.5turn)", func(m *MotionPath) bool {
			r := m.Path.Ray
			return near(r.Angle, math.Pi) && r.Size == RayFarthestCorner && r.Contain && r.AtSet && dimEq(r.At[0], pts(7.5)) && dimEq(r.At[1], pct(0.2))
		}},
		{"offset-path: ray(sides)", func(m *MotionPath) bool { return m == nil }},
		{"offset-path: ray(10deg 20deg)", func(m *MotionPath) bool { return m == nil }},
		{"offset-distance: 50%", func(m *MotionPath) bool { return dimEq(m.Distance, pct(0.5)) }},
		{"offset-distance: 20px", func(m *MotionPath) bool { return dimEq(m.Distance, pts(15)) }},
		{"offset-rotate: reverse", func(m *MotionPath) bool { return !m.Rotate.Fixed && near(m.Rotate.Angle, math.Pi) }},
		{"offset-rotate: 30deg auto", func(m *MotionPath) bool { return !m.Rotate.Fixed && near(m.Rotate.Angle, rad(30)) }},
		{"offset-rotate: 30deg", func(m *MotionPath) bool { return m.Rotate.Fixed && near(m.Rotate.Angle, rad(30)) }},
		{"offset-rotate: auto reverse", func(m *MotionPath) bool { return m == nil }},
		{"offset-anchor: left top", func(m *MotionPath) bool {
			return m.AnchorSet && dimEq(m.Anchor[0], pct(0)) && dimEq(m.Anchor[1], pct(0))
		}},
		{"offset-position: auto", func(m *MotionPath) bool { return m.Position == OffsetAuto }},
		{"offset-position: 10px 30%", func(m *MotionPath) bool {
			return m.Position == OffsetAt && dimEq(m.Start[0], pts(7.5)) && dimEq(m.Start[1], pct(0.3))
		}},
		{"offset: path('M 0 0 H 10') 75% 10deg / 0 0", func(m *MotionPath) bool {
			return m.Path.Kind == OffsetShape && dimEq(m.Distance, pct(0.75)) && m.Rotate.Fixed && near(m.Rotate.Angle, rad(10)) && m.AnchorSet
		}},
		{"offset: 10px 20px ray(90deg) reverse 40px", func(m *MotionPath) bool {
			return m.Position == OffsetAt && m.Path.Kind == OffsetRay && dimEq(m.Distance, pts(30)) && !m.Rotate.Fixed && near(m.Rotate.Angle, math.Pi)
		}},
		{"offset: auto", func(m *MotionPath) bool { return m.Position == OffsetAuto && m.Path.Kind == OffsetNone }},
		{"offset-rotate: 90deg; offset: path('M 0 0')", func(m *MotionPath) bool { return !m.Rotate.Fixed && m.Rotate.Angle == 0 }},
		{"offset: / left top", func(m *MotionPath) bool { return m == nil }},
		{"offset: path('M 0 0') 10px 20px", func(m *MotionPath) bool { return m == nil }},
	}
	for _, tt := range tests {
		st := styleOf(t, tt.decls)
		if !tt.check(st.MotionPath) {
			t.Errorf("%q: computed %+v", tt.decls, st.MotionPath)
		}
	}
}

// TestMotionPath_NotInherited holds the offset longhands not inherited,
// and an offset-path other than none to the effects of a transform (CSS
// Motion Path 1, 2.1).
func TestMotionPath_NotInherited(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=p><div id=c></div></div>`, `#p { offset-path: ray(0deg); offset-distance: 5px }`)
	if !s.of("p").HasOffsetPath() {
		t.Error("parent has no offset path")
	}
	if s.of("c").MotionPath != nil {
		t.Errorf("child motion %+v, want nil", s.of("c").MotionPath)
	}
}

// TestMotionPath_Supports holds @supports to what litho parses.
func TestMotionPath_Supports(t *testing.T) {
	t.Parallel()
	s := compute(t, `<div id=a></div>`, `
		@supports (offset-path: ray(45deg closest-side contain)) and (offset: 10px 10px path('M0 0') 50% auto / center) { #a { order: 1 } }
		@supports (offset-rotate: 45) { #a { order: 2 } }`)
	if got := s.of("a").Order; got != 1 {
		t.Errorf("order %d, want 1", got)
	}
}
