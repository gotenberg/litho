package ooxml

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
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
// applied in document order, as Office applies them: alpha, alphaMod
// and alphaOff; red, green and blue with their Mod and Off in linear RGB;
// hue, sat and lum with their Mod and Off in HSL; tint and shade in linear
// RGB; gray and comp; inv in linear RGB; gamma and invGamma on the sRGB
// curve. false when c is no colour choice or names no colour.
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
		col, ok = sysColor(c)
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

// gammaIn converts an sRGB component to DrawingML's linear RGB
// (ECMA-376 Part 1, 20.1.2.3.30 scrgbClr) on the sRGB transfer curve
// (IEC 61966-2-1), as Office converts it for a tint, a shade, an scrgbClr
// and the red, green and blue transforms.
//
// # Corpus evidence
//
//   - litho/pptx fill-solid: PowerPoint fills 4472C4 under shade 50% with
//     47,82,143, scrgbClr 100%, 50%, 0 with 255,188,0, 404040 under
//     blueOff 50% with 64,64,196 and under greenMod 200% with 64,90,64:
//     the sRGB curve gives each exactly, a power of 2.3 50,84,145,
//     255,189,0, 64,64,195 and 64,87,64.
//   - libreoffice/pptx, litho/pptx, openxmlsdk/pptx, poi/pptx: of the
//     tint and shade colours of every DrawingML colour choice, resolved
//     against its theme, 232 of PowerPoint's fills, strokes and gradient
//     stops are the sRGB curve's exactly, 76 the power of 2.3's. Of the
//     14 tints the curve does not give, nine are 000000 at 75%, whose
//     64,64,64 the slides paint as text, four are table-style tints of
//     three Office 2007 templates (openxmlsdk/pptx 2007MoCal_MonSun,
//     2007calendar, 2008calendar) that PowerPoint paints as a plain blend
//     towards white, one is a colour of the slide.
//   - libreoffice/docx, litho/docx, openxmlsdk/docx, poi/docx: Word paints
//     the sRGB curve's colour for 32 shades of DrawingML shapes, the power
//     of 2.3's for none; libreoffice/xlsx, openxmlsdk/xlsx: Excel for 17,
//     none.
func gammaIn(v float64) float64 { return toLinear(clamp01(v)) }

// gammaOut converts a linear RGB component back to sRGB, as [gammaIn]
// reads it.
func gammaOut(v float64) float64 { return fromLinear(clamp01(v)) }

// sysColor returns the colour of a sysClr (ECMA-376 Part 1, 20.1.2.3.33):
// the system colour its val names, as LibreOffice resolves it on a system
// of default colours, and its lastClr, the colour the system had when the
// file was saved, only for a name it does not know. LibreOffice prints a
// windowText sysClr black whatever lastClr says.
func sysColor(c *Element) (css.Color, bool) {
	if col, ok := systemColor(attrOr(c, "val")); ok {
		return col, true
	}
	return HexColor(attrOr(c, "lastClr"))
}

// systemColor returns the colour of a system colour name (ECMA-376 Part
// 1, 20.1.10.58 ST_SystemColorVal): the window text black, the window
// white, as LibreOffice's defaults.
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
			g := grayLevel(col)
			col.R, col.G, col.B = g, g, g
		case "inv":
			col.R, col.G, col.B = invComponent(col.R), invComponent(col.G), invComponent(col.B)
		case "gamma":
			col.R, col.G, col.B = gammaShift(col.R), gammaShift(col.G), gammaShift(col.B)
		case "invGamma":
			col.R, col.G, col.B = gammaIn(col.R), gammaIn(col.G), gammaIn(col.B)
		}
	}
	return col
}

// grayLevel returns the grey of a gray transform (ECMA-376 Part 1,
// 20.1.2.3.9 gray): the luma of the sRGB components weighted as ITU-R
// BT.709 weighs them.
//
// # Corpus evidence
//
//   - litho/pptx fill-solid: PowerPoint fills ED7D31 under gray with
//     143,143,143; the BT.709 weights give 143, LibreOffice's 0.22, 0.72
//     and 0.06 give 145, the BT.601 weights 150, the luminance in linear
//     light 155. One case: the weights are those that fit it.
func grayLevel(c css.Color) float64 {
	return float64(0.2126*c.R) + float64(0.7152*c.G) + float64(0.0722*c.B)
}

// invComponent inverts one sRGB component in linear light (ECMA-376
// Part 1, 20.1.2.3.17 inv), as Office inverts it.
//
// # Corpus evidence
//
//   - litho/pptx fill-solid: PowerPoint fills 4472C4 under inv with
//     248,235,179; the inverse in linear light is 248,235,179, the
//     inverse of the sRGB components 187,141,59.
func invComponent(c float64) float64 { return gammaOut(1 - gammaIn(c)) }

// gammaShift applies the sRGB gamma shift of a gamma transform (ECMA-376
// Part 1, 20.1.2.3.8 gamma) to one component: the component read as
// linear light and encoded on the sRGB curve. invGamma (20.1.2.3.18)
// decodes it, as [gammaIn] does.
//
// # Corpus evidence
//
//   - litho/pptx fill-solid: PowerPoint fills 4472C4 under gamma with
//     141,178,227 and under invGamma with 15,43,141: the sRGB encoding and
//     decoding of the components give both exactly.
func gammaShift(c float64) float64 { return gammaOut(c) }

// tintComponent tints one sRGB component towards white by 1-t of the way
// in linear RGB (ECMA-376 Part 1, 20.1.2.3.34 tint).
func tintComponent(c, t float64) float64 {
	t = clamp01(t)
	return gammaOut(1 - float64((1-gammaIn(c))*t))
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
// percentage scale of v read as such. A saturation scaled past 1 stays
// past it ([freeSaturation]).
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
	if op == "satMod" && s > 1 {
		return freeSaturation(h, s, clamp01(l), col.A)
	}
	return fromHSL(h, clamp01(s), clamp01(l), col.A)
}

// freeSaturation returns the colour of hue h, saturation s past 1 and
// lightness l in HSL, each component cut to [0, 1], as Office converts a
// saturation a satMod scales past 1 (ECMA-376 Part 1, 20.1.2.3.27
// satMod): HSL's arithmetic at s, not at 1, so the components move past
// the gamut and the cut changes their lightness.
//
// # Corpus evidence
//
//   - libreoffice/pptx sd/tdf114848: PowerPoint writes the stops of
//     4F81BD under tint 52% satMod 300%, shade 20% satMod 300% and tint
//     40% satMod 250% as 158,193,255, 0,54,146 and 190,211,249: the
//     saturation free gives 158,193,255, 0,54,146 and 190,211,249 (with
//     the sRGB curve for the tint and the shade), clamped to 1
//     160,193,255, 0,56,123 and 190,211,249.
//   - openxmlsdk/pptx Text_withEffects_100chars: PowerPoint's glow of
//     F79646 under satMod 180% is premultiplied 76,42,0 at alpha 77,
//     about 253,139,0: the saturation free gives 255,143,0, clamped to 1
//     255,149,62.
func freeSaturation(h, s, l, a float64) css.Color {
	c := fromHSL(h, s, l, a)
	c.R, c.G, c.B = clamp01(c.R), clamp01(c.G), clamp01(c.B)
	return c
}
