package layout

import (
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// TestFontFeatures_Order checks the features the font properties hand the
// shaper and their order, Chromium's (FontFeatureRange::FromFontDescription):
// historical-forms, the ligatures, the East Asian and numeric forms,
// font-feature-settings, then the position, which therefore wins over a
// setting of subs or sups; letter spacing turns the optional ligatures off
// under the browser's rules alone.
func TestFontFeatures_Order(t *testing.T) {
	t.Parallel()
	on := func(tag string) text.Feature { return text.Feature{Tag: tag, Value: 1} }
	off := func(tag string) text.Feature { return text.Feature{Tag: tag} }
	for _, tc := range []struct {
		name    string
		browser bool
		st      css.Style
		spacing float64
		want    []text.Feature
	}{
		{"normal", true, css.Style{}, 0, nil},
		{"every property", true, css.Style{
			FontVariantAlternates: css.Alternates{HistoricalForms: true},
			FontVariantLigatures:  css.LigaturesCommonOff | css.LigaturesDiscretionary,
			FontVariantEastAsian:  css.EastAsianJIS04 | css.EastAsianRuby,
			FontVariantNumeric:    css.NumericOldstyle | css.NumericSlashedZero,
			FontFeatureSettings:   []css.FeatureSetting{{Tag: "sups", Value: 0}},
			FontVariantPosition:   css.PositionSuper,
		}, 0, []text.Feature{
			on("hist"), off("liga"), off("clig"), on("dlig"), on("jp04"), on("ruby"),
			on("onum"), on("zero"), off("sups"), on("sups"),
		}},
		{"letter spacing", true, css.Style{FontVariantLigatures: css.LigaturesDiscretionary}, 2,
			[]text.Feature{off("liga"), off("clig"), off("calt")}},
		{"letter spacing outside the browser", false, css.Style{}, 2, nil},
	} {
		e := &engine{opts: Options{BrowserRules: tc.browser}}
		ts := text.Style{LetterSpacing: tc.spacing}
		if got := e.fontFeatures(&tc.st, &ts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: features %v, want %v", tc.name, got, tc.want)
		}
	}
}
