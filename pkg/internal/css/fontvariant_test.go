package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestFontVariant_Shorthand checks the font-variant shorthand (CSS Fonts
// 4, 6.11): it sets font-variant-caps, resets it when left out, takes any
// value of its subproperties once each, and warns of the ones litho does
// not have.
func TestFontVariant_Shorthand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		decls   string
		want    FontVariant
		warning string
	}{
		{"font-variant: small-caps", SmallCaps, ""},
		{"font-variant-caps: small-caps", SmallCaps, ""},
		{"font-variant: all-small-caps", SmallCaps, ""},
		{"font-variant: small-caps oldstyle-nums", SmallCaps, "oldstyle-nums"},
		{"font-variant: tabular-nums ordinal small-caps slashed-zero", SmallCaps, "tabular-nums ordinal slashed-zero"},
		{"font-variant: stylistic(fancy) styleset(a, b) small-caps", SmallCaps, "stylistic() styleset()"},
		{"font-variant-caps: small-caps; font-variant: oldstyle-nums", VariantNormal, "oldstyle-nums"},
		{"font-variant-caps: small-caps; font-variant: normal", VariantNormal, ""},
		{"font-variant-caps: small-caps; font-variant: none", VariantNormal, "font-variant-ligatures"},
		{"font-variant: petite-caps", VariantNormal, "petite-caps"},
		{"font-variant: small-caps; font: 12px serif", VariantNormal, ""},
	} {
		c := compute(t, `<body><div id=t></div></body>`, `#t { `+tc.decls+` }`)
		if got := c.of("t").FontVariant; got != tc.want {
			t.Errorf("%s: font-variant-caps = %v, want %v", tc.decls, got, tc.want)
		}
		ws := c.c.Warnings()
		if tc.warning != "" && !hasWarning(ws, warn.CSSUnsupported, tc.warning) {
			t.Errorf("%s: no warning naming %q: %v", tc.decls, tc.warning, ws)
		}
		if tc.warning == "" && len(ws) > 0 {
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

// TestFontVariant_Longhands records which longhands of font-variant (CSS
// Fonts 4, 6.11) litho has: font-variant-caps, and none of the other six.
func TestFontVariant_Longhands(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"font-variant-ligatures", "font-variant-position", "font-variant-caps", "font-variant-numeric",
		"font-variant-alternates", "font-variant-east-asian", "font-variant-emoji",
	} {
		if got, want := knownProperty(name), name == "font-variant-caps"; got != want {
			t.Errorf("%s known = %v, want %v", name, got, want)
		}
	}
}
