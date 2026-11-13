package css

import (
	"math"
	"testing"
)

func TestFilter_Properties(t *testing.T) {
	t.Parallel()
	one := func(k FilterKind, amount float64) func(*Style) bool {
		return func(s *Style) bool {
			return len(s.Filter) == 1 && s.Filter[0].Kind == k && math.Abs(s.Filter[0].Amount-amount) < 1e-9
		}
	}
	tests := []struct {
		decl  string
		check func(*Style) bool
	}{
		{"", func(s *Style) bool { return s.Filter == nil && s.BackdropFilter == nil }},
		{"filter: none", func(s *Style) bool { return s.Filter == nil }},
		{"filter: blur(4px)", one(FilterBlur, 3)},
		{"filter: blur()", one(FilterBlur, 0)},
		{"filter: grayscale(50%)", one(FilterGrayscale, 0.5)},
		// grayscale, invert, opacity and sepia clamp to 1; brightness,
		// contrast and saturate do not (Filter Effects 1, 13.1).
		{"filter: grayscale(2)", one(FilterGrayscale, 1)},
		{"filter: sepia(150%)", one(FilterSepia, 1)},
		{"filter: brightness(2.5)", one(FilterBrightness, 2.5)},
		{"filter: saturate()", one(FilterSaturate, 1)},
		{"filter: hue-rotate(0.25turn)", one(FilterHueRotate, 90)},
		{"filter: hue-rotate(0)", one(FilterHueRotate, 0)},
		{"filter: hue-rotate()", one(FilterHueRotate, 0)},
		{"filter: url(#f)", func(s *Style) bool {
			return len(s.Filter) == 1 && s.Filter[0].Kind == FilterURL && s.Filter[0].URL == "#f"
		}},
		{"filter: drop-shadow(2px 4px 6px red)", func(s *Style) bool {
			f := s.Filter
			return len(f) == 1 && f[0].Kind == FilterDropShadow && f[0].Shadow.X == 1.5 && f[0].Shadow.Y == 3 && f[0].Shadow.Blur == 4.5 && f[0].Shadow.Color == Color{R: 1, A: 1}
		}},
		{"color: lime; filter: drop-shadow(1px 1px)", func(s *Style) bool {
			return len(s.Filter) == 1 && s.Filter[0].Shadow.Color == Color{G: 1, A: 1}
		}},
		{"filter: contrast(1.2) blur(1px) url(#a) invert()", func(s *Style) bool {
			f := s.Filter
			return len(f) == 4 && f[0].Kind == FilterContrast && f[1].Kind == FilterBlur && f[2].Kind == FilterURL && f[3].Kind == FilterInvert && f[3].Amount == 1
		}},
		{"backdrop-filter: blur(10px)", func(s *Style) bool {
			return s.Filter == nil && len(s.BackdropFilter) == 1 && s.BackdropFilter[0].Kind == FilterBlur
		}},
		{"-webkit-filter: invert(1)", one(FilterInvert, 1)},
		// Invalid values drop the declaration.
		{"filter: sepia(1); filter: blur(-1px)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: blur(10%)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: brightness(-1)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: hue-rotate(90)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: drop-shadow(1px 1px 1px 1px)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: drop-shadow(inset 1px 1px)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: blur(1px), invert(1)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: none blur(1px)", one(FilterSepia, 1)},
		{"filter: sepia(1); filter: shadow(1px)", one(FilterSepia, 1)},
	}
	for _, tt := range tests {
		if s := styleOf(t, tt.decl); !tt.check(s) {
			t.Errorf("%q: filter %+v, backdrop-filter %+v", tt.decl, s.Filter, s.BackdropFilter)
		}
	}
}

func TestFilter_NotInherited(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id="a"><p id="b">x</p></div>`, `#a { filter: blur(2px); backdrop-filter: invert(1) }`)
	if s := c.of("b"); s.Filter != nil || s.BackdropFilter != nil {
		t.Errorf("child: %+v %+v", s.Filter, s.BackdropFilter)
	}
}

func TestFilter_InheritedDropShadowTakesTheChildColour(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id="a"><p id="b">x</p></div>`, `#a { color: red; filter: drop-shadow(1px 1px) } #b { color: blue; filter: inherit }`)
	if s := c.of("b"); len(s.Filter) != 1 || s.Filter[0].Shadow.Color != (Color{B: 1, A: 1}) {
		t.Errorf("child: %+v, want a blue drop shadow", s.Filter)
	}
}
