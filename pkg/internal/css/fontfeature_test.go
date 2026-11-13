package css

import (
	"context"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestFontVariant_Shorthand checks the font-variant shorthand (CSS Fonts
// 4, 6.11): it hands each longhand its components, resets the ones it
// leaves out, and none sets font-variant-ligatures to none.
func TestFontVariant_Shorthand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		decls string
		want  func(*Style) bool
	}{
		{"font-variant: small-caps", func(s *Style) bool { return s.FontVariant == SmallCaps }},
		{"font-variant: all-petite-caps", func(s *Style) bool { return s.FontVariant == AllPetiteCaps }},
		{"font-variant: small-caps oldstyle-nums", func(s *Style) bool {
			return s.FontVariant == SmallCaps && s.FontVariantNumeric == NumericOldstyle
		}},
		{"font-variant: tabular-nums ordinal slashed-zero", func(s *Style) bool {
			return s.FontVariantNumeric == NumericTabular|NumericOrdinal|NumericSlashedZero
		}},
		{"font-variant: stylistic(fancy) styleset(a, b) historical-forms", func(s *Style) bool {
			a := s.FontVariantAlternates
			return a != nil && a.Stylistic == "fancy" && reflect.DeepEqual(a.Styleset, []string{"a", "b"}) && a.HistoricalForms
		}},
		{"font-variant: jis78 ruby full-width super emoji no-contextual", func(s *Style) bool {
			return s.FontVariantEastAsian == EastAsianJIS78|EastAsianRuby|EastAsianFullWidth &&
				s.FontVariantPosition == PositionSuper && s.FontVariantEmoji == EmojiEmoji &&
				s.FontVariantLigatures == LigaturesContextualOff
		}},
		{"font-variant-caps: small-caps; font-variant: oldstyle-nums", func(s *Style) bool {
			return s.FontVariant == VariantNormal && s.FontVariantNumeric == NumericOldstyle
		}},
		{"font-variant-numeric: ordinal; font-variant: none", func(s *Style) bool {
			return s.FontVariantNumeric == 0 && s.FontVariantLigatures == LigaturesCommonOff|LigaturesContextualOff
		}},
		{"font-variant: small-caps tabular-nums; font: 12px serif", func(s *Style) bool {
			return s.FontVariant == VariantNormal && s.FontVariantNumeric == 0
		}},
		{"font-feature-settings: 'liga' 0; font-language-override: 'TRK'; font: 12px serif", func(s *Style) bool {
			return s.FontFeatureSettings == nil && s.FontLanguageOverride == ""
		}},
	} {
		c := compute(t, `<body><div id=t></div></body>`, `#t { `+tc.decls+` }`)
		if !tc.want(c.of("t")) {
			t.Errorf("%s: unexpected style %+v", tc.decls, c.of("t"))
		}
		if ws := c.c.Warnings(); len(ws) > 0 {
			t.Errorf("%s: unexpected warnings %v", tc.decls, ws)
		}
	}
}

// TestFontVariant_Invalid checks that values outside the grammar of
// font-variant (CSS Fonts 4, 6.11) are refused: a component twice, normal
// or none beside another, a function given a non-name or a list it does
// not take.
func TestFontVariant_Invalid(t *testing.T) {
	t.Parallel()
	for _, v := range []string{
		"small-caps small-caps", "small-caps all-small-caps", "normal small-caps", "none small-caps",
		"oldstyle-nums lining-nums", "sub super", "stylistic(a, b)", "stylistic(1)", "swash()", "bold", "small-caps 3",
	} {
		c := compute(t, `<body><div id=t></div></body>`, `#t { font-variant-caps: small-caps; font-variant: `+v+` }`)
		if got := c.of("t").FontVariant; got != SmallCaps {
			t.Errorf("font-variant: %s applied: %v", v, got)
		}
		if !hasWarning(c.c.Warnings(), warn.CSSInvalid, `"font-variant"`) {
			t.Errorf("font-variant: %s: no invalid-value warning", v)
		}
	}
}

// TestFontFeatureSettings checks font-feature-settings (CSS Fonts 4,
// 6.12): a four-character tag, then on, off or a non-negative integer, 1
// when left out; anything else leaves the inherited value.
func TestFontFeatureSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  []FeatureSetting
	}{
		{`"liga"`, []FeatureSetting{{"liga", 1}}},
		{`"liga" off, 'ss03' 3, "kern" on`, []FeatureSetting{{"liga", 0}, {"ss03", 3}, {"kern", 1}}},
		{`normal`, nil},
		{`"lig"`, []FeatureSetting{{"zero", 1}}},
		{`"liga" -1`, []FeatureSetting{{"zero", 1}}},
		{`liga`, []FeatureSetting{{"zero", 1}}},
		{`"liga" 1.5`, []FeatureSetting{{"zero", 1}}},
	} {
		c := compute(t, `<body><div id=t></div></body>`, `body { font-feature-settings: "zero" } #t { font-feature-settings: `+tc.value+` }`)
		if got := c.of("t").FontFeatureSettings; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("font-feature-settings: %s = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestFontSynthesis_SmallCaps checks the small-caps keyword of the
// font-synthesis shorthand (CSS Fonts 4, 5.5).
func TestFontSynthesis_SmallCaps(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{"none": true, "weight style": true, "small-caps": false, "weight small-caps position": false} {
		c := compute(t, `<body><div id=t></div></body>`, `#t { font-synthesis: `+value+` }`)
		if got := c.of("t").NoSynthesizeSmallCaps; got != want {
			t.Errorf("font-synthesis: %s: no small-caps synthesis = %v, want %v", value, got, want)
		}
	}
}

// TestFontFeatureValues checks @font-feature-values (CSS Fonts 4, 6.9) and
// the resolution of font-variant-alternates against it: rules of a family
// merge, a name is case-sensitive, a family is not, and values outside
// what a feature type takes are ignored.
func TestFontFeatureValues(t *testing.T) {
	t.Parallel()
	sheet, err := Parse(context.Background(), []byte(`
@font-feature-values Fancy Font, "Other" {
  @styleset { nice: 2 4; bad: -1; }
  @swash { flowing: 1; two: 1 2; }
  @character-variant { alt: 3 2; }
}
@font-feature-values fancy font { @stylistic { Old: 7; } @styleset { nice: 5; } }
`), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(Media{}, []Sheet{{Sheet: sheet}})
	v := r.FontFeatureValues("FANCY FONT")
	if v == nil || r.FontFeatureValues("other") == nil || r.FontFeatureValues("none") != nil {
		t.Fatalf("families not resolved: %v", v)
	}
	a := Alternates{
		Stylistic: "Old", Swash: "two", Styleset: []string{"nice", "bad", "missing"},
		CharacterVariant: []string{"alt"}, HistoricalForms: true,
	}
	want := []FeatureSetting{{"salt", 7}, {"ss05", 1}, {"cv03", 2}, {"hist", 1}}
	if got := a.Resolve(v); !reflect.DeepEqual(got, want) {
		t.Errorf("resolved %v, want %v", got, want)
	}
	a.Stylistic = "old"
	if got := a.Resolve(nil); !reflect.DeepEqual(got, []FeatureSetting{{"hist", 1}}) {
		t.Errorf("resolved without values %v", got)
	}
}

func TestFontSizeAdjust(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]FontSizeAdjust{
		"none":                {},
		"0.5":                 {Set: true, Value: 0.5},
		"cap-height 0.7":      {Set: true, Metric: 1, Value: 0.7},
		"ic-height from-font": {Set: true, Metric: 4, FromFont: true},
		"-1":                  {Set: true, Value: 0.3},
		"bold 0.5":            {Set: true, Value: 0.3},
	} {
		c := compute(t, `<body><div id=t></div></body>`, `body { font-size-adjust: 0.3 } #t { font-size-adjust: `+value+` }`)
		if got := c.of("t").FontSizeAdjust; got != want {
			t.Errorf("font-size-adjust: %s = %+v, want %+v", value, got, want)
		}
	}
}

// TestFontVariationSettings pins font-variation-settings (CSS Fonts 4,
// 8.2): a list of string tags and numbers, the last value of a repeated
// tag kept, normal, and the invalid values that keep the inherited one.
func TestFontVariationSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  []VariationSetting
	}{
		{`"wght" 700`, []VariationSetting{{"wght", 700}}},
		{`"wght" 700, 'slnt' -10.5, "wght" 300`, []VariationSetting{{"slnt", -10.5}, {"wght", 300}}},
		{`"opsz" calc(6 * 2)`, []VariationSetting{{"opsz", 12}}},
		{`normal`, nil},
		{`"wgh" 300`, []VariationSetting{{"wdth", 80}}},
		{`"wght"`, []VariationSetting{{"wdth", 80}}},
		{`wght 300`, []VariationSetting{{"wdth", 80}}},
		{`"wght" 300px`, []VariationSetting{{"wdth", 80}}},
	} {
		c := compute(t, `<body><div id=t></div></body>`, `body { font-variation-settings: "wdth" 80 } #t { font-variation-settings: `+tc.value+` }`)
		if got := c.of("t").FontVariationSettings; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("font-variation-settings: %s = %v, want %v", tc.value, got, tc.want)
		}
	}
	c := compute(t, `<body><div id=t></div><div id=u></div></body>`,
		`body { font-optical-sizing: none; font-variation-settings: "wght" 300 } #t { font: 12px serif } #u { font-optical-sizing: auto }`)
	if st := c.of("t"); st.FontOpticalSizing != OpticalSizingAuto || st.FontVariationSettings != nil {
		t.Errorf("the font shorthand left font-optical-sizing %v, font-variation-settings %v", st.FontOpticalSizing, st.FontVariationSettings)
	}
	if st := c.of("u"); st.FontOpticalSizing != OpticalSizingAuto || len(st.FontVariationSettings) != 1 {
		t.Errorf("font-optical-sizing %v, inherited font-variation-settings %v", st.FontOpticalSizing, st.FontVariationSettings)
	}
}
