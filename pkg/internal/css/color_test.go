package css

import (
	"math"
	"testing"
)

// TestParseColor_AsChromiumPrints locks every colour syntax of CSS Color 4
// and 5 to the DeviceRGB fill Chromium writes for it, within the four
// decimals Chromium writes, and to the alpha it computes.
//
// # Corpus evidence
//
//   - probe, html: each value painted as the background of a box by
//     Chromium 156 in the pinned image, read from the PDF's rg operators
//     and from getComputedStyle.
func TestParseColor_AsChromiumPrints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src        string
		r, g, b, a float64
	}{
		{"color(display-p3 1 0 0)", 1, 0, 0, 1},
		{"color(display-p3 0 1 0)", 0, 1, 0, 1},
		{"color(display-p3 0 0 1)", .0001, .0001, 1, 1},
		{"color(rec2020 0 1 0)", 0, 1, 0, 1},
		{"color(rec2020 1 0 0)", 1, 0, 0, 1},
		{"color(prophoto-rgb 0 1 0)", 0, 1, 0, 1},
		{"color(a98-rgb 0 1 0)", 0, 1, 0, 1},
		{"color(srgb 1.2 -0.1 0.5)", 1, 0, .5, 1},
		{"color(srgb-linear 0.5 0.5 0.5)", .7354, .7354, .7354, 1},
		{"color(xyz 0.5 0.5 0.5)", .7991, .7181, .7045, 1},
		{"color(xyz-d50 0.3 0.4 0.2)", .479, .7217, .4968, 1},
		{"color(display-p3-linear 0.5 0.2 0.1)", .7781, .4701, .3214, 1},
		{"lab(50 150 0)", 1, 0, .5031, 1},
		{"lab(50% 40 -20)", .6703, .3547, .6046, 1},
		{"lab(100 0 0)", .9999, 1, 1, 1},
		{"lab(110 0 0)", .9999, 1, 1, 1},
		{"lch(50 200 30)", 1, 0, .0102, 1},
		{"lch(60 40 120)", .4882, .6032, .3163, 1},
		{"oklab(0.6 0.1 -0.1)", .6254, .3887, .7275, 1},
		{"oklch(0.7 0.4 30)", 1, 0, 0, 1},
		{"oklch(0.7 0.1 30)", .8367, .5267, .4752, 1},
		{"oklch(1 0.4 30)", 1, .2005, .0232, 1},
		{"oklch(0.5 0.2 none)", .7049, .0236, .3708, 1},
		{"hwb(120 20% 30%)", .2, .7, .2, 1},
		{"hwb(none none none)", 1, 0, 0, 1},
		{"hsl(120deg 50% 50% / 0.5)", .25, .75, .25, 0.5},
		{"rgb(none 128 255)", 0, .502, 1, 1},
		{"rgb(300 -20 128)", 1, 0, .502, 1},
		{"rgb(10.6 20.4 30.5)", .0416, .08, .1196, 1},
		{"hsl(400 120% 50%)", 1, .7, 0, 1},
		{"color-mix(in srgb, red, blue)", .5, 0, .5, 1},
		{"color-mix(in oklch, red, blue)", .7299, 0, .7601, 1},
		{"color-mix(in lch, purple 50%, plum)", .6851, .36, .6829, 1},
		{"color-mix(in oklab, red 30%, transparent)", 1, 0, 0, 0.3},
		{"color-mix(in hsl longer hue, red, blue)", 0, 1, 0, 1},
		{"color-mix(in srgb, red 20%, blue 20%)", .5, 0, .5, 0.4},
		{"color-mix(in xyz, red, lime)", .7354, .7354, 0, 1},
		{"color-mix(red, blue)", .5504, .3256, .6365, 1},
		{"color-mix(in display-p3, red, blue)", .5016, .0385, .5692, 1},
		{"rgb(from red r g b)", 1, 0, 0, 1},
		{"rgb(from red calc(r / 2) g b / 0.5)", .5, 0, 0, 0.5},
		{"hsl(from green h s calc(l + 20))", 0, .902, 0, 1},
		{"oklch(from #08f l c calc(h + 180))", .8099, .4341, 0, 1},
		{"color(from red display-p3 r g b)", 1, 0, 0, 1},
		{"lab(from blue l a b / alpha)", 0, 0, 1, 1},
		{"light-dark(red, blue)", 1, 0, 0, 1},
		{"contrast-color(white)", 0, 0, 0, 1},
		{"contrast-color(navy)", 1, 1, 1, 1},
		{"Canvas", 1, 1, 1, 1},
		{"CanvasText", 0, 0, 0, 1},
		{"LinkText", 0, 0, .9333, 1},
		{"VisitedText", .3333, .102, .5451, 1},
		{"ActiveText", 1, 0, 0, 1},
		{"ButtonFace", .9373, .9373, .9373, 1},
		{"ButtonText", 0, 0, 0, 1},
		{"ButtonBorder", 0, 0, 0, 1},
		{"Field", 1, 1, 1, 1},
		{"FieldText", 0, 0, 0, 1},
		{"Highlight", 0, .2549, .7765, 0.8},
		{"HighlightText", 1, 1, 1, 1},
		{"SelectedItem", .098, .4039, .8235, 1},
		{"SelectedItemText", 1, 1, 1, 1},
		{"Mark", 1, 1, 0, 1},
		{"MarkText", 0, 0, 0, 1},
		{"GrayText", .502, .502, .502, 1},
		{"AccentColor", 0, .4588, 1, 1},
		{"AccentColorText", 1, 1, 1, 1},
		{"ActiveBorder", 0, 0, 0, 1},
		{"ActiveCaption", 1, 1, 1, 1},
		{"AppWorkspace", 1, 1, 1, 1},
		{"Background", 1, 1, 1, 1},
		{"ButtonHighlight", .9373, .9373, .9373, 1},
		{"ButtonShadow", .9373, .9373, .9373, 1},
		{"CaptionText", 0, 0, 0, 1},
		{"InactiveBorder", 0, 0, 0, 1},
		{"InactiveCaption", 1, 1, 1, 1},
		{"InactiveCaptionText", .502, .502, .502, 1},
		{"InfoBackground", 1, 1, 1, 1},
		{"InfoText", 0, 0, 0, 1},
		{"Menu", 1, 1, 1, 1},
		{"MenuText", 0, 0, 0, 1},
		{"Scrollbar", 1, 1, 1, 1},
		{"ThreeDDarkShadow", 0, 0, 0, 1},
		{"ThreeDFace", .9373, .9373, .9373, 1},
		{"ThreeDHighlight", 0, 0, 0, 1},
		{"ThreeDLightShadow", 0, 0, 0, 1},
		{"ThreeDShadow", 0, 0, 0, 1},
		{"Window", 1, 1, 1, 1},
		{"WindowFrame", 0, 0, 0, 1},
		{"WindowText", 0, 0, 0, 1},
		{"color(rec2020 0.5 0.5 0.5)", .5465, .5466, .5466, 1},
		{"color(rec2020 0.2 0.6 0.3)", 0, .6703, .3134, 1},
		{"color(a98-rgb 0.5 0.3 0.2)", .5621, .2949, .1799, 1},
		{"color(prophoto-rgb 0.5 0.3 0.2)", .7246, .3039, .2324, 1},
		{"color(display-p3 0.5 0.3 0.2)", .5328, .2878, .1771, 1},
		{"color-mix(in oklch, white, blue)", .4528, .6381, 1, 1},
		{"color-mix(in lch, white, blue)", .6859, .5379, 1, 1},
		{"color-mix(in hsl, white, blue)", .625, .625, .875, 1},
		{"color-mix(in hwb, white, blue)", .5, .5, 1, 1},
		{"color-mix(in oklch longer hue, red, blue)", 0, .5758, 0, 1},
		{"color-mix(in oklch increasing hue, red, blue)", 0, .5758, 0, 1},
		{"color-mix(in oklch decreasing hue, red, blue)", .7299, 0, .7601, 1},
		{"color-mix(in hsl decreasing hue, red, blue)", 1, 0, 1, 1},
		{"color-mix(in srgb, red 70%, blue)", .7, 0, .3, 1},
		{"color-mix(in srgb, red 60%, blue 60%)", .5, 0, .5, 1},
		{"color-mix(in srgb-linear, red, blue)", .7354, 0, .7354, 1},
		{"color-mix(in display-p3-linear, red, blue)", .7354, 0, .7354, 1},
		{"color-mix(in xyz-d50, red, blue)", .7354, 0, .7354, 1},
		{"color-mix(in lab, red, blue)", .7568, 0, .534, 1},
		{"color-mix(in srgb, rgb(255 0 0 / 0.5), blue)", .3333, 0, .6667, 0.75},
		{"color-mix(in oklch, oklch(0.5 0.2 none), oklch(0.7 0.1 120))", .4608, .5489, 0, 1},
		{"color-mix(in srgb, rgb(none 0 0), rgb(200 100 50))", .7843, .1961, .098, 1},
		{"color-mix(in oklch, rgb(none 0 0), rgb(200 100 50))", .2979, .133, .0506, 1},
		{"color(srgb 50% 0.5 none)", .5, .5, 0, 1},
		{"hsl(none 50% 50%)", .75, .25, .25, 1},
		{"hsl(120 50 50)", .25, .75, .25, 1},
		{"hsl(calc(60 * 2) 50% 50%)", .25, .75, .25, 1},
		{"hsl(calc(60deg * 2) 50% 50%)", .25, .75, .25, 1},
		{"rgb(calc(255 / 2) 0 0)", .5, 0, 0, 1},
		{"rgb(50% 0 0)", .5, 0, 0, 1},
		{"rgba(255, 0, 0)", 1, 0, 0, 1},
		{"rgb(255 0 0 / 50%)", 1, 0, 0, 0.5},
		{"hwb(120 60% 60%)", .5, .5, .5, 1},
		{"lab(50 -200 300)", 0, .6225, 0, 1},
		{"lch(50 -20 30)", .4663, .4664, .4663, 1},
		{"oklch(-0.2 0.1 30)", .0038, .0001, 0, 1},
		{"oklch(0.5 -0.1 30)", .3885, .3886, .3886, 1},
		{"oklab(1.2 0 0)", .9999, 1, 1, 1},
		{"rgb(from hsl(120 100% 25%) r g b / 0.5)", 0, .5, 0, 0.5},
		{"hsl(from rgb(0 0 0 / 0.3) h s 50)", .5, .5, .5, 0.3},
		{"hwb(from red h w b)", 1, 0, 0, 1},
		{"lch(from oklch(0.5 0.2 none) l c h)", .7049, .0236, .3708, 1},
		{"oklch(from white l c h)", 1, 1, 1, 1},
		{"color(from color(display-p3 1 0 0) srgb r g b)", 1, 0, 0, 1},
		{"color(from red xyz x y z)", 1, 0, 0, 1},
		{"rgb(from color-mix(in srgb, red, blue) r g b)", .5, 0, .5, 1},
		{"rgb(from red r g b / calc(alpha / 2))", 1, 0, 0, 0.5},
		{"rgb(from red 50% g b)", .5, 0, 0, 1},
		{"rgb(from red none g b)", 0, 0, 0, 1},
		{"light-dark(red, rgb(from blue r g b))", 1, 0, 0, 1},
		{"contrast-color(rgb(128 128 128))", 0, 0, 0, 1},
		{"contrast-color(rgb(118 118 118))", 0, 0, 0, 1},
		{"contrast-color(rgb(119 119 119))", 0, 0, 0, 1},
		{"contrast-color(rgb(150 150 150))", 0, 0, 0, 1},
		{"contrast-color(rgb(200 0 0))", 1, 1, 1, 1},
		{"contrast-color(rgb(255 0 0))", 0, 0, 0, 1},
		{"contrast-color(rgb(0 128 0))", 1, 1, 1, 1},
		{"contrast-color(rgb(0 160 0))", 0, 0, 0, 1},
		{"contrast-color(rgb(0 0 255))", 1, 1, 1, 1},
		{"contrast-color(rgb(255 128 0))", 0, 0, 0, 1},
		{"contrast-color(rgb(0 0 0 / 0))", 1, 1, 1, 1},
		{"contrast-color(color(display-p3 0 1 0))", 0, 0, 0, 1},
	}
	for _, tt := range tests {
		v := oneValue(tt.src)
		if len(v) != 1 {
			t.Fatalf("%s: %d component values", tt.src, len(v))
		}
		c, ok := parseColor(v[0])
		if !ok {
			t.Errorf("%s: refused", tt.src)
			continue
		}
		got := c.resolve(Color{})
		want := Color{R: tt.r, G: tt.g, B: tt.b, A: tt.a}
		if d := math.Max(math.Max(math.Abs(got.R-want.R), math.Abs(got.G-want.G)), math.Max(math.Abs(got.B-want.B), math.Abs(got.A-want.A))); d > 0.002 {
			t.Errorf("%s = %.4f %.4f %.4f / %.4f, want %.4f %.4f %.4f / %.4f", tt.src, got.R, got.G, got.B, got.A, want.R, want.G, want.B, want.A)
		}
	}
}

// TestParseColor_RefusesWhatChromiumRefuses locks the values Chromium 156
// does not parse (CSS.supports false): a single colour or three in
// color-mix(), none in the legacy syntax, an angle added to a channel
// keyword, device-cmyk() and a custom colour space.
func TestParseColor_RefusesWhatChromiumRefuses(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"color-mix(in oklab, teal, olive, blue)", "color-mix(in srgb, red)", "color-mix(in srgb, red 50%)",
		"hsl(120, 50%, none)", "hsl(from red calc(h + 120deg) s l)", "device-cmyk(0 0 0 1)", "color(--foo 1 0 0)",
		"color-mix(in srgb, red 120%, blue)", "color-mix(in srgb, red -10%, blue)", "color-mix(in lab longer hue, red, blue)",
		"rgb(from red r g)", "lab(from red l a b c)", "light-dark(red)",
	} {
		v := oneValue(src)
		if _, ok := parseColor(v[0]); ok {
			t.Errorf("%s: accepted, want refused", src)
		}
	}
}

// TestColor_CurrentColorInFunctions locks a colour function of
// currentColor: it resolves against the element's color, and an element
// that inherits it explicitly resolves it again against its own (CSS
// Color 5, 3 and 4).
func TestColor_CurrentColorInFunctions(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a><div id=b></div></div><p id=p></p>`,
		`#a { color: #123456; background-color: color-mix(in srgb, currentColor, blue) }
		#b { color: rgb(0 128 0); background-color: inherit }
		#p { color: red; color: rgb(from currentColor r g calc(b + 255)) }`)
	if got, want := c.of("a").BackgroundColor, (Color{R: 0.0353, G: 0.102, B: 0.6686, A: 1}); !nearColor(got, want) {
		t.Errorf("color-mix() of currentColor = %+v, want %+v", got, want)
	}
	if got, want := c.of("b").BackgroundColor, (Color{R: 0, G: 0.251, B: 0.5, A: 1}); !nearColor(got, want) {
		t.Errorf("inherited color-mix() of currentColor = %+v, want %+v", got, want)
	}
	if got, want := c.of("p").Color, (Color{R: 0, G: 0, B: 1, A: 1}); !nearColor(got, want) {
		t.Errorf("color: rgb(from currentColor ...) = %+v, want the parent's black made blue %+v", got, want)
	}
	if !c.of("b").BorderColorIsCurrent(Top) || c.of("a").BackgroundColor == c.of("b").BackgroundColor {
		t.Errorf("the inherited colour did not resolve again")
	}
}

func nearColor(a, b Color) bool {
	return math.Abs(a.R-b.R) < 0.002 && math.Abs(a.G-b.G) < 0.002 && math.Abs(a.B-b.B) < 0.002 && math.Abs(a.A-b.A) < 0.002
}
