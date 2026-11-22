package css

import "testing"

// TestSizing_Keywords holds the sizing keywords to CSS Sizing 3, 3.1 and
// CSS Sizing 4, 3.1 on width and on flex-basis (CSS Flexbox 1, 7.3.3),
// with the prefixed spellings Chromium parses and none it refuses: an
// invalid value leaves the initial auto. litho reads fit-content() on
// width, height and their minimums and maximums alone: on flex-basis it
// stays invalid.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) answers
//     CSS.supports true for the -webkit- spellings on width and false on
//     flex-basis, false for every -moz- spelling, true for stretch and the
//     three content keywords on both.
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
		{"width: fit-content(10px)", func(s *Style) DimKind { return s.Width.Kind }, FitContentOf},
		{"flex-basis: fit-content(10px)", func(s *Style) DimKind { return s.FlexBasis.Kind }, Auto},
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

func TestFitContentFunction_Parse(t *testing.T) {
	t.Parallel()
	// CSS Sizing 4, 3.2: fit-content(<length-percentage [0,∞]>) on width,
	// height and their minimums and maximums; it clears a minimum's auto.
	for _, tc := range []struct {
		decls string
		get   func(*Style) Dimension
		kind  DimKind
		limit Dimension
	}{
		{"width: fit-content(100px)", func(s *Style) Dimension { return s.Width }, FitContentOf, Pts(75)},
		{"max-width: fit-content(50%)", func(s *Style) Dimension { return s.MaxWidth }, FitContentOf, Dimension{Kind: Percent, Value: 0.5}},
		{"min-height: fit-content(20px)", func(s *Style) Dimension { return s.MinHeight }, FitContentOf, Pts(15)},
		{"width: fit-content(-1px)", func(s *Style) Dimension { return s.Width }, Auto, Pts(0)},
		{"width: fit-content()", func(s *Style) Dimension { return s.Width }, Auto, Pts(0)},
		{"width: fit-content(1px, 2px)", func(s *Style) Dimension { return s.Width }, Auto, Pts(0)},
	} {
		st := styleOf(t, tc.decls)
		d := tc.get(st)
		if d.Kind != tc.kind || !dimEq(d.FitLimit(), tc.limit) {
			t.Errorf("%q: %+v limit %+v, want kind %v limit %+v", tc.decls, d, d.FitLimit(), tc.kind, tc.limit)
		}
	}
	if st := styleOf(t, "min-width: auto; min-width: fit-content(10px)"); st.MinWidthAuto {
		t.Error("min-width: fit-content() keeps the auto flag")
	}
	if d := styleOf(t, "width: fit-content(calc(10px + 50%))").Width; d.Kind != FitContentOf || d.FitLimit().Kind != Calc {
		t.Errorf("a calc() argument: %+v", d)
	}
}
