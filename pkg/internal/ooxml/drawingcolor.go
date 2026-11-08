package ooxml

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// The gammas of DrawingML's linear RGB (ECMA-376 Part 1, 20.1.2.3.30
// scrgbClr): LibreOffice converts between sRGB and the linear space a
// tint, a shade and the component transforms work in with a plain power
// of 2.3, not the sRGB curve (oox's Color::toCrgb and toRgb).
const (
	decGamma = 2.3
	incGamma = 1 / decGamma
)

// SchemeResolver returns the colour a scheme colour name stands for
// (ECMA-376 Part 1, 20.1.10.54 ST_SchemeColorVal: "accent1", "tx1",
// "bg2", "phClr", ...), false when it names none.
type SchemeResolver func(name string) (css.Color, bool)

// DrawingColor returns the colour a DrawingML colour container holds (a
// solidFill, a gradient stop, a buClr, a fillRef...): the first colour
// choice among its children, its transforms applied as
// [DrawingColorChoice] does.
func DrawingColor(e *Element, scheme SchemeResolver) (css.Color, bool) {
	for _, c := range e.Kids() {
		if col, ok := DrawingColorChoice(c, scheme); ok {
			return col, true
		}
	}
	return css.Color{}, false
}

// DrawingColorChoice returns the colour of a colour choice element
// (ECMA-376 Part 1, 20.1.2.3: srgbClr, scrgbClr, hslClr, sysClr,
// schemeClr resolved through scheme, prstClr) with its colour transforms
// applied in document order, as LibreOffice applies them: alpha, alphaMod
// and alphaOff; red, green and blue with their Mod and Off in linear RGB;
// hue, sat and lum with their Mod and Off in HSL; tint and shade in linear
// RGB; gray, comp and inv. gamma and invGamma are ignored. false when c
// is no colour choice or names no colour.
func DrawingColorChoice(c *Element, scheme SchemeResolver) (css.Color, bool) {
	if c == nil || c.Space != NSA {
		return css.Color{}, false
	}
	var col css.Color
	var ok bool
	switch c.Local {
	case "srgbClr":
		col, ok = HexColor(attrOr(c, "val"))
	case "scrgbClr":
		col = css.Color{R: gammaOut(pct(c, "r")), G: gammaOut(pct(c, "g")), B: gammaOut(pct(c, "b")), A: 1}
		ok = true
	case "hslClr":
		h, _ := Int(attrOr(c, "hue"))
		col = fromHSL(math.Mod(float64(h)/60000/360, 1), clamp01(pct(c, "sat")), clamp01(pct(c, "lum")), 1)
		ok = true
	case "sysClr":
		if col, ok = HexColor(attrOr(c, "lastClr")); !ok {
			col, ok = systemColor(attrOr(c, "val"))
		}
	case "schemeClr":
		if scheme != nil {
			col, ok = scheme(attrOr(c, "val"))
		}
	case "prstClr":
		col, ok = PresetColor(attrOr(c, "val"))
	}
	if !ok {
		return css.Color{}, false
	}
	return transformColor(col, c), true
}

// pct returns c's ST_Percentage attribute name as a fraction, 100,000 for 1.
func pct(c *Element, name string) float64 {
	v, _ := Int(attrOr(c, name))
	return float64(v) / 100000
}

// clamp01 clamps v to [0, 1].
func clamp01(v float64) float64 { return max(0, min(1, v)) }

// gammaIn converts an sRGB component to DrawingML's linear RGB.
func gammaIn(v float64) float64 { return math.Pow(clamp01(v), decGamma) }

// gammaOut converts a linear RGB component back to sRGB.
func gammaOut(v float64) float64 { return math.Pow(clamp01(v), incGamma) }

// systemColor returns the colour of a system colour name (ECMA-376 Part
// 1, 20.1.10.58 ST_SystemColorVal) without a lastClr: the window text
// black, the window white, as LibreOffice's defaults.
func systemColor(name string) (css.Color, bool) {
	switch name {
	case "windowText", "menuText", "captionText", "btnText", "infoText", "3dDkShadow", "highlightText":
		return css.Color{A: 1}, true
	case "window", "menu", "btnFace", "3dLight", "btnHighlight", "infoBk", "background":
		return css.Color{R: 1, G: 1, B: 1, A: 1}, true
	}
	return css.Color{}, false
}

// PresetColor returns the colour of a DrawingML preset colour name
// (ECMA-376 Part 1, 20.1.10.47 ST_PresetColorVal): the CSS named colours,
// with the abbreviations dk, lt and med for dark, light and medium.
func PresetColor(name string) (css.Color, bool) {
	n := strings.ToLower(name)
	for _, p := range [...][2]string{{"dk", "dark"}, {"lt", "light"}, {"med", "medium"}} {
		if rest, ok := strings.CutPrefix(n, p[0]); ok {
			n = p[1] + rest
			break
		}
	}
	if c, current, ok := css.ParseColor(n); ok && !current && c.A > 0 {
		return c, true
	}
	return css.Color{}, false
}

// transformColor applies the colour transforms of the colour choice c to
// col, in document order (ECMA-376 Part 1, 20.1.2.3).
func transformColor(col css.Color, c *Element) css.Color {
	for _, m := range c.Children {
		if m.Space != NSA {
			continue
		}
		v := pct(m, "val")
		switch m.Local {
		case "alpha":
			col.A = clamp01(v)
		case "alphaMod":
			col.A = clamp01(col.A * v)
		case "alphaOff":
			col.A = clamp01(col.A + v)
		case "red", "redMod", "redOff":
			col.R = linearComponent(col.R, m.Local[3:], v)
		case "green", "greenMod", "greenOff":
			col.G = linearComponent(col.G, m.Local[5:], v)
		case "blue", "blueMod", "blueOff":
			col.B = linearComponent(col.B, m.Local[4:], v)
		case "hue", "hueMod", "hueOff", "sat", "satMod", "satOff", "lum", "lumMod", "lumOff", "comp":
			col = hslTransform(col, m.Local, v)
		case "tint":
			col.R, col.G, col.B = tintComponent(col.R, v), tintComponent(col.G, v), tintComponent(col.B, v)
		case "shade":
			s := clamp01(v)
			col.R, col.G, col.B = gammaOut(gammaIn(col.R)*s), gammaOut(gammaIn(col.G)*s), gammaOut(gammaIn(col.B)*s)
		case "gray":
			g := 0.22*col.R + 0.72*col.G + 0.06*col.B
			col.R, col.G, col.B = g, g, g
		case "inv":
			col.R, col.G, col.B = 1-col.R, 1-col.G, 1-col.B
		}
	}
	return col
}

// tintComponent tints one sRGB component towards white by 1-t of the way
// in linear RGB (ECMA-376 Part 1, 20.1.2.3.34 tint).
func tintComponent(c, t float64) float64 {
	t = clamp01(t)
	return gammaOut(1 - (1-gammaIn(c))*t)
}

// linearComponent sets ("" suffix), scales ("Mod") or offsets ("Off") one
// sRGB component in linear RGB.
func linearComponent(c float64, op string, v float64) float64 {
	l := gammaIn(c)
	switch op {
	case "":
		l = v
	case "Mod":
		l *= v
	case "Off":
		l += v
	}
	return gammaOut(l)
}

// hslTransform applies a hue, saturation or luminance transform, or the
// complement, in HSL space. A hue value is in 60,000ths of a degree, the
// percentage scale of v read as such.
func hslTransform(col css.Color, op string, v float64) css.Color {
	h, s, l := toHSL(col)
	deg := v * 100000 / 60000 / 360
	switch op {
	case "hue":
		h = deg
	case "hueMod":
		h *= v
	case "hueOff":
		h += deg
	case "comp":
		h += 0.5
	case "sat":
		s = v
	case "satMod":
		s *= v
	case "satOff":
		s += v
	case "lum":
		l = v
	case "lumMod":
		l *= v
	case "lumOff":
		l += v
	}
	h = math.Mod(h, 1)
	if h < 0 {
		h++
	}
	return fromHSL(h, clamp01(s), clamp01(l), col.A)
}
