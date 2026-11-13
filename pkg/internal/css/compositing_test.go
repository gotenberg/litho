package css

import "testing"

func TestCompositing_Properties(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		check func(*Style) bool
	}{
		{"", func(s *Style) bool {
			return s.MixBlendMode == BlendNormal && s.Isolation == IsolationAuto && s.BackgroundBlendMode == BlendNormal
		}},
		{"mix-blend-mode: multiply", func(s *Style) bool { return s.MixBlendMode == BlendMultiply }},
		{"mix-blend-mode: LUMINOSITY", func(s *Style) bool { return s.MixBlendMode == BlendLuminosity }},
		{"mix-blend-mode: plus-lighter", func(s *Style) bool { return s.MixBlendMode == BlendPlusLighter }},
		// Chromium parses no plus-darker: the declaration is dropped.
		{"mix-blend-mode: screen; mix-blend-mode: plus-darker", func(s *Style) bool { return s.MixBlendMode == BlendScreen }},
		{"mix-blend-mode: multiply screen", func(s *Style) bool { return s.MixBlendMode == BlendNormal }},
		{"isolation: isolate", func(s *Style) bool { return s.Isolation == IsolationIsolate }},
		{"background-blend-mode: color-burn, screen", func(s *Style) bool { return s.BackgroundBlendMode == BlendColorBurn }},
		// background-blend-mode takes no plus-lighter (CSS Compositing 1,
		// 3.4); a list with one invalid entry is invalid whole.
		{"background-blend-mode: hue; background-blend-mode: plus-lighter", func(s *Style) bool { return s.BackgroundBlendMode == BlendHue }},
		{"background-blend-mode: hue; background-blend-mode: screen, nope", func(s *Style) bool { return s.BackgroundBlendMode == BlendHue }},
	}
	for _, tt := range tests {
		if s := styleOf(t, tt.decl); !tt.check(s) {
			t.Errorf("%q: mix-blend-mode %d, isolation %d, background-blend-mode %d", tt.decl, s.MixBlendMode, s.Isolation, s.BackgroundBlendMode)
		}
	}
}

func TestCompositing_NotInherited(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id="a"><p id="b">x</p></div>`, `#a { mix-blend-mode: multiply; isolation: isolate; background-blend-mode: screen }`)
	if s := c.of("b"); s.MixBlendMode != BlendNormal || s.Isolation != IsolationAuto || s.BackgroundBlendMode != BlendNormal {
		t.Errorf("child: %d %d %d", s.MixBlendMode, s.Isolation, s.BackgroundBlendMode)
	}
}
