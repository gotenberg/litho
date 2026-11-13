package css

import "testing"

// TestSizing_Keywords holds the sizing keywords to CSS Sizing 3, 3.1 and
// CSS Sizing 4, 3.1 on width and on flex-basis (CSS Flexbox 1, 7.3.3),
// with the prefixed spellings Chromium parses and none it refuses: an
// invalid value leaves the initial auto.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) answers
//     CSS.supports true for the -webkit- spellings on width and false on
//     flex-basis, false for every -moz- spelling and for fit-content() on
//     width, true for stretch and the three content keywords on both.
func TestSizing_Keywords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		get  func(*Style) DimKind
		want DimKind
	}{
		{"width: min-content", func(s *Style) DimKind { return s.Width.Kind }, MinContent},
		{"width: -webkit-fill-available", func(s *Style) DimKind { return s.Width.Kind }, Stretch},
		{"width: -webkit-fit-content", func(s *Style) DimKind { return s.Width.Kind }, FitContent},
		{"width: -moz-available", func(s *Style) DimKind { return s.Width.Kind }, Auto},
		{"width: -moz-fit-content", func(s *Style) DimKind { return s.Width.Kind }, Auto},
		{"width: fit-content(10px)", func(s *Style) DimKind { return s.Width.Kind }, Auto},
		{"flex-basis: min-content", func(s *Style) DimKind { return s.FlexBasis.Kind }, MinContent},
		{"flex-basis: max-content", func(s *Style) DimKind { return s.FlexBasis.Kind }, MaxContent},
		{"flex-basis: fit-content", func(s *Style) DimKind { return s.FlexBasis.Kind }, FitContent},
		{"flex-basis: stretch", func(s *Style) DimKind { return s.FlexBasis.Kind }, Stretch},
		{"flex-basis: content", func(s *Style) DimKind { return s.FlexBasis.Kind }, FlexContent},
		{"flex-basis: -webkit-fill-available", func(s *Style) DimKind { return s.FlexBasis.Kind }, Auto},
		{"flex-basis: -webkit-min-content", func(s *Style) DimKind { return s.FlexBasis.Kind }, Auto},
	}
	for _, tt := range tests {
		if got := tt.get(styleOf(t, tt.decl)); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.decl, got, tt.want)
		}
	}
}
