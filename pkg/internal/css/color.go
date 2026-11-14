package css

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// This file parses the <color> values of CSS Color 4 and 5: the named,
// system and hex colours, the colour functions of every space, color(),
// color-mix(), the relative colour syntax, light-dark() and
// contrast-color(). A colour keeps its space until it is painted, when it
// becomes sRGB.

// colorValue is a specified <color>: a colour, currentColor, or a colour
// function that refers to currentColor.
type colorValue struct {
	c Color
	// sc is the colour in its own space, which an interpolation reads.
	sc      spaceColor
	current bool
	// legacy marks a colour of the legacy sRGB formats, which interpolate
	// in sRGB by default (CSS Color 4, 13.3): a named, system or hex
	// colour, rgb(), hsl() or hwb().
	legacy bool
	// expr, when set, is a colour function that refers to currentColor,
	// such as color-mix(in srgb, currentColor, red): its value is
	// resolved against each element's color (CSS Color 5, 3 and 4). It is
	// shared and never mutated.
	expr *token
}

// resolve returns the colour against cur, the element's color.
func (v colorValue) resolve(cur Color) Color {
	switch {
	case v.current:
		return cur
	case v.expr != nil:
		e := colorEnv{current: &cur}
		if sc, ok := e.parse(*v.expr, 0); ok {
			return sc.paint()
		}
	}
	return v.c
}

// space returns the colour in its own space against cur, the element's
// color.
func (v colorValue) space(cur Color) spaceColor {
	switch {
	case v.current:
		return srgbColor(cur.R, cur.G, cur.B, cur.A)
	case v.expr != nil:
		e := colorEnv{current: &cur}
		if sc, ok := e.parse(*v.expr, 0); ok {
			return sc
		}
	}
	return v.sc
}

// dependsOnCurrent reports whether the value resolves against the
// element's color.
func (v colorValue) dependsOnCurrent() bool { return v.current || v.expr != nil }

// parseColor parses a <color> (CSS Color 4, 4.1 and CSS Color 5, 2).
func parseColor(t token) (colorValue, bool) {
	if t.isIdent("currentcolor") {
		return colorValue{current: true, legacy: true}, true
	}
	e := colorEnv{}
	sc, ok := e.parse(t, 0)
	if !ok {
		return colorValue{}, false
	}
	if e.usedCurrent {
		expr := t
		return colorValue{expr: &expr}, true
	}
	return colorValue{c: sc.paint(), sc: sc, legacy: legacySRGB(t) && sc.none == [4]bool{}}, true
}

// legacySRGB reports a colour written in a legacy sRGB format (CSS Color
// 4, 13.3): a keyword, a hex colour, or rgb(), rgba(), hsl(), hsla() or
// hwb() without an origin colour. parseColor also takes a colour with a
// missing component out of the legacy formats, whose syntax has no none
// (4.1.2): such a colour interpolates in Oklab by default, as Chromium
// draws it.
//
// # Corpus evidence
//
//   - wpt: css-images/gradient/gradient-analogous-missing-components-004:
//     Chromium 156 (gotenberg/gotenberg:9.4.1) draws the middle of
//     linear-gradient(to right, rgb(none 255 0), rgb(255, 0, 0)) as
//     rgb(208 167 0), the Oklab midpoint, not the sRGB yellow.
func legacySRGB(t token) bool {
	switch t.typ { //nolint:exhaustive // the other token types are no colours
	case tIdent, tHash:
		return true
	case tFunction:
		switch lower(t.value) {
		case "rgb", "rgba", "hsl", "hsla", "hwb":
			a := significant(t.children)
			return len(a) == 0 || !a[0].isIdent("from")
		}
	}
	return false
}

// colorSpace is a colour space of CSS Color 4: the predefined RGB and XYZ
// spaces of color() (10), Lab, LCH, Oklab and OkLCh (9), HSL (7) and HWB
// (8).
type colorSpace uint8

// The colour spaces.
const (
	spaceSRGB colorSpace = iota
	spaceSRGBLinear
	spaceDisplayP3
	spaceDisplayP3Linear
	spaceA98
	spaceProPhoto
	spaceRec2020
	spaceXYZD50
	spaceXYZD65
	spaceLab
	spaceLCH
	spaceOKLab
	spaceOKLCh
	spaceHSL
	spaceHWB
	numSpaces
)

// predefinedSpaces maps the <predefined-rgb> and <xyz-space> names of
// color() to their space (CSS Color 4, 10.1). xyz is xyz-d65 (10.9).
var predefinedSpaces = map[string]colorSpace{
	"srgb": spaceSRGB, "srgb-linear": spaceSRGBLinear, "display-p3": spaceDisplayP3,
	"display-p3-linear": spaceDisplayP3Linear, "a98-rgb": spaceA98, "prophoto-rgb": spaceProPhoto,
	"rec2020": spaceRec2020, "xyz": spaceXYZD65, "xyz-d50": spaceXYZD50, "xyz-d65": spaceXYZD65,
}

// interpolationSpaces maps the <color-space> names of a
// <color-interpolation-method> to their space (CSS Color 4, 12.1).
var interpolationSpaces = func() map[string]colorSpace {
	m := map[string]colorSpace{"lab": spaceLab, "lch": spaceLCH, "oklab": spaceOKLab, "oklch": spaceOKLCh, "hsl": spaceHSL, "hwb": spaceHWB}
	for k, v := range predefinedSpaces {
		m[k] = v
	}
	return m
}()

// componentKind is what a colour component stands for, which decides how
// a missing component carries over to another space (CSS Color 4, 13.4).
type componentKind uint8

// The analogous component kinds; kindOther carries over to nothing.
const (
	kindOther componentKind = iota
	kindRed
	kindGreen
	kindBlue
	kindLightness
	kindColorfulness
	kindHue
	kindOpponentA
	kindOpponentB
)

// spaceKinds lists the kind of each component of each space.
var spaceKinds = func() [numSpaces][3]componentKind {
	var k [numSpaces][3]componentKind
	for s := spaceSRGB; s <= spaceXYZD65; s++ {
		k[s] = [3]componentKind{kindRed, kindGreen, kindBlue}
	}
	k[spaceLab] = [3]componentKind{kindLightness, kindOpponentA, kindOpponentB}
	k[spaceOKLab] = k[spaceLab]
	k[spaceLCH] = [3]componentKind{kindLightness, kindColorfulness, kindHue}
	k[spaceOKLCh] = k[spaceLCH]
	k[spaceHSL] = [3]componentKind{kindHue, kindColorfulness, kindLightness}
	k[spaceHWB] = [3]componentKind{kindHue, kindOther, kindOther}
	return k
}()

// hueIndex returns the index of the hue component of a cylindrical
// space, -1 for a rectangular one.
func hueIndex(s colorSpace) int {
	for i, k := range spaceKinds[s] {
		if k == kindHue {
			return i
		}
	}
	return -1
}

// spaceColor is a colour in a space, its components in the reference
// ranges of CSS Color 4 (sRGB 0 to 1, HSL saturation and lightness 0 to
// 100, Lab lightness 0 to 100, Oklab lightness 0 to 1, hues in degrees),
// with straight alpha. none marks the missing components, alpha last
// (CSS Color 4, 4.4).
type spaceColor struct {
	space colorSpace
	c     [3]float64
	alpha float64
	none  [4]bool
}

func srgbColor(r, g, b, a float64) spaceColor {
	return spaceColor{space: spaceSRGB, c: [3]float64{r, g, b}, alpha: a}
}

// paint returns the colour as litho paints it: sRGB, each channel clipped
// to its range. CSS Color 4, 14.2 maps a colour outside sRGB by reducing
// its chroma in Oklch; Chromium clips each channel of the converted
// colour instead, and litho follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints color(display-p3 1 0 0) as DeviceRGB
//     1 0 0, lch(50 200 30) as 1 0 0.0102, oklch(0.7 0.4 30) as 1 0 0.
func (s spaceColor) paint() Color {
	c := s.convert(spaceSRGB)
	clip := func(v float64) float64 {
		if !numeric.Finite(v) {
			return 0
		}
		return clamp01(v)
	}
	a := s.alpha
	if s.none[3] {
		a = 0
	}
	return Color{R: clip(c.c[0]), G: clip(c.c[1]), B: clip(c.c[2]), A: clip(a)}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// convert converts the colour to the space t (CSS Color 4, 11), a
// missing component as zero, and marks nothing missing: the origin of a
// relative colour, whose channel keywords Chromium binds to the plain
// converted values.
func (s spaceColor) convert(t colorSpace) spaceColor {
	c := s.c
	for i := range c {
		if s.none[i] {
			c[i] = 0
		}
	}
	out := spaceColor{space: t, alpha: s.alpha, c: c}
	if s.none[3] {
		out.alpha = 0
	}
	if s.space != t {
		out.c = fromXYZ(t, toXYZ(s.space, c))
	}
	return out
}

// to converts the colour to the space t for an interpolation (CSS Color
// 4, 11): a missing component converts as zero, and the converted colour
// marks missing the components analogous to the missing ones (13.4), and
// a hue that is powerless, its colour achromatic (4.4.1).
func (s spaceColor) to(t colorSpace) spaceColor {
	if s.space == t {
		return s
	}
	out := s.convert(t)
	out.alpha, out.none[3] = s.alpha, s.none[3]
	for i, k := range spaceKinds[s.space] {
		if !s.none[i] || k == kindOther {
			continue
		}
		for j, tk := range spaceKinds[t] {
			if tk == k {
				out.none[j] = true
			}
		}
	}
	if h := hueIndex(t); h >= 0 && powerless(t, out.c) {
		out.none[h] = true
	}
	return out
}

// powerless reports an achromatic colour of a cylindrical space, whose
// hue is powerless (CSS Color 4, 4.4.1), with the thresholds of the
// sample code of 19.
func powerless(s colorSpace, c [3]float64) bool {
	switch s { //nolint:exhaustive // only the cylindrical spaces have a hue
	case spaceHSL:
		return math.Abs(c[1]) <= 0.001
	case spaceHWB:
		return c[1]+c[2] >= 100-0.001
	case spaceLCH:
		return c[1] <= 0.0015
	case spaceOKLCh:
		return c[1] <= 0.000004
	}
	return false
}

type vec3 = [3]float64

func mul3(m *[3][3]float64, v vec3) vec3 {
	return vec3{
		float64(m[0][0]*v[0]) + float64(m[0][1]*v[1]) + float64(m[0][2]*v[2]),
		float64(m[1][0]*v[0]) + float64(m[1][1]*v[1]) + float64(m[1][2]*v[2]),
		float64(m[2][0]*v[0]) + float64(m[2][1]*v[1]) + float64(m[2][2]*v[2]),
	}
}

// The conversion matrices of CSS Color 4, 19.
var (
	srgbToXYZ = [3][3]float64{
		{506752.0 / 1228815, 87881.0 / 245763, 12673.0 / 70218},
		{87098.0 / 409605, 175762.0 / 245763, 12673.0 / 175545},
		{7918.0 / 409605, 87881.0 / 737289, 1001167.0 / 1053270},
	}
	xyzToSRGB = [3][3]float64{
		{12831.0 / 3959, -329.0 / 214, -1974.0 / 3959},
		{-851781.0 / 878810, 1648619.0 / 878810, 36519.0 / 878810},
		{705.0 / 12673, -2585.0 / 12673, 705.0 / 667},
	}
	p3ToXYZ = [3][3]float64{
		{608311.0 / 1250200, 189793.0 / 714400, 198249.0 / 1000160},
		{35783.0 / 156275, 247089.0 / 357200, 198249.0 / 2500400},
		{0, 32229.0 / 714400, 5220557.0 / 5000800},
	}
	xyzToP3 = [3][3]float64{
		{446124.0 / 178915, -333277.0 / 357830, -72051.0 / 178915},
		{-14852.0 / 17905, 63121.0 / 35810, 423.0 / 17905},
		{11844.0 / 330415, -50337.0 / 660830, 316169.0 / 330415},
	}
	proPhotoToXYZ = [3][3]float64{
		{0.79776664490064230, 0.13518129740053308, 0.03134773412839220},
		{0.28807482881940130, 0.71183523424187300, 0.00008993693872564},
		{0, 0, 0.82510460251046020},
	}
	xyzToProPhoto = [3][3]float64{
		{1.34578688164715830, -0.25557208737979464, -0.05110186497554526},
		{-0.54463070512490190, 1.50824774284514680, 0.02052744743642139},
		{0, 0, 1.21196754563894520},
	}
	a98ToXYZ = [3][3]float64{
		{573536.0 / 994567, 263643.0 / 1420810, 187206.0 / 994567},
		{591459.0 / 1989134, 6239551.0 / 9945670, 374412.0 / 4972835},
		{53769.0 / 1989134, 351524.0 / 4972835, 4929758.0 / 4972835},
	}
	xyzToA98 = [3][3]float64{
		{1829569.0 / 896150, -506331.0 / 896150, -308931.0 / 896150},
		{-851781.0 / 878810, 1648619.0 / 878810, 36519.0 / 878810},
		{16779.0 / 1248040, -147721.0 / 1248040, 1266979.0 / 1248040},
	}
	rec2020ToXYZ = [3][3]float64{
		{63426534.0 / 99577255, 20160776.0 / 139408157, 47086771.0 / 278816314},
		{26158966.0 / 99577255, 472592308.0 / 697040785, 8267143.0 / 139408157},
		{0, 19567812.0 / 697040785, 295819943.0 / 278816314},
	}
	xyzToRec2020 = [3][3]float64{
		{30757411.0 / 17917100, -6372589.0 / 17917100, -4539589.0 / 17917100},
		{-19765991.0 / 29648200, 47925759.0 / 29648200, 467509.0 / 29648200},
		{792561.0 / 44930125, -1921689.0 / 44930125, 42328811.0 / 44930125},
	}
	d65ToD50 = [3][3]float64{
		{1.0479297925449969, 0.022946870601609652, -0.05019226628920524},
		{0.02962780877005599, 0.9904344267538799, -0.017073799063418826},
		{-0.009243040646204504, 0.015055191490298152, 0.7518742814281371},
	}
	d50ToD65 = [3][3]float64{
		{0.955473421488075, -0.02309845494876471, 0.06325924320057072},
		{-0.0283697093338637, 1.0099953980813041, 0.021041441191917323},
		{0.012314014864481998, -0.020507649298898964, 1.330365926242124},
	}
	xyzToLMS = [3][3]float64{
		{0.8190224379967030, 0.3619062600528904, -0.1288737815209879},
		{0.0329836539323885, 0.9292868615863434, 0.0361446663506424},
		{0.0481771893596242, 0.2642395317527308, 0.6335478284694309},
	}
	lmsToOKLab = [3][3]float64{
		{0.2104542683093140, 0.7936177747023054, -0.0040720430116193},
		{1.9779985324311684, -2.4285922420485799, 0.4505937096174110},
		{0.0259040424655478, 0.7827717124575296, -0.8086757549230774},
	}
	lmsToXYZ = [3][3]float64{
		{1.2268798758459243, -0.5578149944602171, 0.2813910456659647},
		{-0.0405757452148008, 1.1122868032803170, -0.0717110580655164},
		{-0.0763729366746601, -0.4214933324022432, 1.5869240198367816},
	}
	okLabToLMS = [3][3]float64{
		{1, 0.3963377773761749, 0.2158037573099136},
		{1, -0.1055613458156586, -0.0638541728258133},
		{1, -0.0894841775298119, -1.2914855480194092},
	}
	// d50White is the D50 white point of CSS Color 4, 19.
	d50White = vec3{0.3457 / 0.3585, 1, (1 - 0.3457 - 0.3585) / 0.3585}
)

// signed applies f to the magnitude of v and keeps its sign: the extended
// transfer functions of CSS Color 4, 19.
func signed(v float64, f func(float64) float64) float64 {
	if v < 0 {
		return -f(-v)
	}
	return f(v)
}

func linSRGB(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a <= 0.04045 {
			return a / 12.92
		}
		return detmath.Pow((a+0.055)/1.055, 2.4)
	})
}

func gamSRGB(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a > 0.0031308 {
			return float64(1.055*detmath.Pow(a, 1/2.4)) - 0.055
		}
		return 12.92 * a
	})
}

func linProPhoto(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a <= 16.0/512 {
			return a / 16
		}
		return detmath.Pow(a, 1.8)
	})
}

func gamProPhoto(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a >= 1.0/512 {
			return detmath.Pow(a, 1/1.8)
		}
		return 16 * a
	})
}

func linA98(v float64) float64 {
	return signed(v, func(a float64) float64 { return detmath.Pow(a, 563.0/256) })
}

func gamA98(v float64) float64 {
	return signed(v, func(a float64) float64 { return detmath.Pow(a, 256.0/563) })
}

// The constants of the Rec. ITU-R BT.2020-2 transfer function.
const (
	rec2020Alpha = 1.09929682680944
	rec2020Beta  = 0.018053968510807
)

// linRec2020 linearises a rec2020 component by the inverse of the BT.2020
// opto-electronic transfer function. CSS Color 4, 10.8 and its sample
// code take the BT.1886 transfer, a pure 2.4 power; Chromium keeps the
// BT.2020 one, and litho follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints color(rec2020 0.5 0.5 0.5) as
//     DeviceRGB 0.5466 each (139.4 of 255), the BT.2020 curve; the 2.4
//     power gives 0.4725.
func linRec2020(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a < rec2020Beta*4.5 {
			return a / 4.5
		}
		return detmath.Pow((a+rec2020Alpha-1)/rec2020Alpha, 1/0.45)
	})
}

func gamRec2020(v float64) float64 {
	return signed(v, func(a float64) float64 {
		if a < rec2020Beta {
			return a * 4.5
		}
		return float64(rec2020Alpha*detmath.Pow(a, 0.45)) - (rec2020Alpha - 1)
	})
}

func mapVec(v vec3, f func(float64) float64) vec3 {
	return vec3{f(v[0]), f(v[1]), f(v[2])}
}

// toXYZ converts components of space s to CIE XYZ relative to D65 (CSS
// Color 4, 19).
func toXYZ(s colorSpace, c vec3) vec3 {
	switch s {
	case spaceSRGB:
		return mul3(&srgbToXYZ, mapVec(c, linSRGB))
	case spaceSRGBLinear:
		return mul3(&srgbToXYZ, c)
	case spaceDisplayP3:
		return mul3(&p3ToXYZ, mapVec(c, linSRGB))
	case spaceDisplayP3Linear:
		return mul3(&p3ToXYZ, c)
	case spaceA98:
		return mul3(&a98ToXYZ, mapVec(c, linA98))
	case spaceProPhoto:
		return mul3(&d50ToD65, mul3(&proPhotoToXYZ, mapVec(c, linProPhoto)))
	case spaceRec2020:
		return mul3(&rec2020ToXYZ, mapVec(c, linRec2020))
	case spaceXYZD50:
		return mul3(&d50ToD65, c)
	case spaceXYZD65:
		return c
	case spaceLab:
		return mul3(&d50ToD65, labToXYZ(c))
	case spaceLCH:
		return mul3(&d50ToD65, labToXYZ(fromPolar(c)))
	case spaceOKLab:
		return okLabToXYZ(c)
	case spaceOKLCh:
		return okLabToXYZ(fromPolar(c))
	case spaceHSL:
		r, g, b := hslToRGB(c[0], c[1]/100, c[2]/100)
		return toXYZ(spaceSRGB, vec3{r, g, b})
	case spaceHWB:
		return toXYZ(spaceSRGB, hwbToRGB(c[0], c[1]/100, c[2]/100))
	case numSpaces:
	}
	return c
}

// fromXYZ converts CIE XYZ relative to D65 to components of space s.
func fromXYZ(s colorSpace, xyz vec3) vec3 {
	switch s {
	case spaceSRGB:
		return mapVec(mul3(&xyzToSRGB, xyz), gamSRGB)
	case spaceSRGBLinear:
		return mul3(&xyzToSRGB, xyz)
	case spaceDisplayP3:
		return mapVec(mul3(&xyzToP3, xyz), gamSRGB)
	case spaceDisplayP3Linear:
		return mul3(&xyzToP3, xyz)
	case spaceA98:
		return mapVec(mul3(&xyzToA98, xyz), gamA98)
	case spaceProPhoto:
		return mapVec(mul3(&xyzToProPhoto, mul3(&d65ToD50, xyz)), gamProPhoto)
	case spaceRec2020:
		return mapVec(mul3(&xyzToRec2020, xyz), gamRec2020)
	case spaceXYZD50:
		return mul3(&d65ToD50, xyz)
	case spaceXYZD65:
		return xyz
	case spaceLab:
		return xyzToLab(mul3(&d65ToD50, xyz))
	case spaceLCH:
		return toPolar(xyzToLab(mul3(&d65ToD50, xyz)))
	case spaceOKLab:
		return xyzToOKLab(xyz)
	case spaceOKLCh:
		return toPolar(xyzToOKLab(xyz))
	case spaceHSL:
		return rgbToHSL(fromXYZ(spaceSRGB, xyz))
	case spaceHWB:
		return rgbToHWB(fromXYZ(spaceSRGB, xyz))
	case numSpaces:
	}
	return xyz
}

// labToXYZ converts CIE Lab to XYZ relative to D50 (CSS Color 4, 19).
func labToXYZ(lab vec3) vec3 {
	const kappa, eps = 24389.0 / 27, 216.0 / 24389
	f1 := lab[0]/116 + 16.0/116
	f0 := f1 + lab[1]/500
	f2 := f1 - lab[2]/200
	cube := func(f, lin float64) float64 {
		if f*f*f > eps {
			return f * f * f
		}
		return lin * 116 / kappa
	}
	y := lab[0] / kappa
	if lab[0] > kappa*eps {
		f := (lab[0] + 16) / 116
		y = f * f * f
	}
	xyz := vec3{cube(f0, lab[0]/116+lab[1]/500), y, cube(f2, lab[0]/116-lab[2]/200)}
	return vec3{xyz[0] * d50White[0], xyz[1] * d50White[1], xyz[2] * d50White[2]}
}

// xyzToLab converts XYZ relative to D50 to CIE Lab.
func xyzToLab(xyz vec3) vec3 {
	const kappa, eps = 24389.0 / 27, 216.0 / 24389
	var f vec3
	for i := range f {
		v := xyz[i] / d50White[i]
		if v > eps {
			f[i] = detmath.Cbrt(v)
		} else {
			f[i] = (float64(kappa*v) + 16) / 116
		}
	}
	return vec3{float64(116*f[1]) - 16, 500 * (f[0] - f[1]), 200 * (f[1] - f[2])}
}

func okLabToXYZ(lab vec3) vec3 {
	lms := mul3(&okLabToLMS, lab)
	return mul3(&lmsToXYZ, mapVec(lms, func(v float64) float64 { return v * v * v }))
}

func xyzToOKLab(xyz vec3) vec3 {
	return mul3(&lmsToOKLab, mapVec(mul3(&xyzToLMS, xyz), detmath.Cbrt))
}

// toPolar converts rectangular Lab or Oklab to LCH or OkLCh (CSS Color 4,
// 9.5), the hue in [0, 360).
func toPolar(lab vec3) vec3 {
	h := detmath.Atan2(lab[2], lab[1]) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return vec3{lab[0], detmath.Hypot(lab[1], lab[2]), h}
}

// fromPolar converts LCH or OkLCh to rectangular Lab or Oklab (CSS Color
// 4, 9.6).
func fromPolar(lch vec3) vec3 {
	r := lch[2] * math.Pi / 180
	return vec3{lch[0], lch[1] * detmath.Cos(r), lch[1] * detmath.Sin(r)}
}

// hslToRGB converts HSL, saturation and lightness as fractions, to sRGB
// (CSS Color 4, 7.1, the sample code). Saturation past 1 is kept: the
// colour is then outside sRGB.
func hslToRGB(h, s, l float64) (r, g, b float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - float64(a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1))))
	}
	return f(0), f(8), f(4)
}

// rgbToHSL converts sRGB to HSL, saturation and lightness 0 to 100 (CSS
// Color 4, 7.2).
func rgbToHSL(c vec3) vec3 {
	mx, mn := math.Max(c[0], math.Max(c[1], c[2])), math.Min(c[0], math.Min(c[1], c[2]))
	hue, sat, light := 0.0, 0.0, float64((mn+mx)/2)
	d := mx - mn
	const eps = 1.0 / 100000
	if d != 0 {
		if math.Abs(light) > eps && math.Abs(1-light) >= eps {
			sat = (mx - light) / math.Min(light, 1-light)
		}
		hue = rgbHue(c, mx, d)
	}
	if sat < 0 {
		hue += 180
		sat = -sat
	}
	if hue >= 360 {
		hue -= 360
	}
	return vec3{hue, sat * 100, light * 100}
}

// rgbHue is the hue of an sRGB colour whose largest channel is mx and
// whose range is d, not zero (CSS Color 4, 8.2).
func rgbHue(c vec3, mx, d float64) float64 {
	var h float64
	switch mx {
	case c[0]:
		h = (c[1] - c[2]) / d
		if c[1] < c[2] {
			h += 6
		}
	case c[1]:
		h = (c[2]-c[0])/d + 2
	default:
		h = (c[0]-c[1])/d + 4
	}
	return h * 60
}

// hwbToRGB converts HWB, whiteness and blackness as fractions, to sRGB
// (CSS Color 4, 8.1): whiteness and blackness summing past 1 are scaled
// down to a grey.
func hwbToRGB(h, w, b float64) vec3 {
	if w+b >= 1 {
		g := w / (w + b)
		return vec3{g, g, g}
	}
	r, g, bl := hslToRGB(h, 1, 0.5)
	k := 1 - w - b
	return vec3{float64(r*k) + w, float64(g*k) + w, float64(bl*k) + w}
}

// rgbToHWB converts sRGB to HWB, whiteness and blackness 0 to 100 (CSS
// Color 4, 8.2).
func rgbToHWB(c vec3) vec3 {
	mx, mn := math.Max(c[0], math.Max(c[1], c[2])), math.Min(c[0], math.Min(c[1], c[2]))
	h := 0.0
	if d := mx - mn; d != 0 {
		h = rgbHue(c, mx, d)
	}
	if h >= 360 {
		h -= 360
	}
	return vec3{h, mn * 100, (1 - mx) * 100}
}

// maxColorDepth caps the nesting of colour functions, a color-mix() of
// relative colours of color-mix()es: 16. Real styles nest two. Anti-DoS
// guard, not spec-mandated: it bounds the parser's recursion.
const maxColorDepth = 16

// colorEnv parses colours: current is the element's color currentColor
// stands for, nil while a declaration is parsed, when usedCurrent records
// that the colour depends on it.
type colorEnv struct {
	current     *Color
	usedCurrent bool
}

// parse parses a <color>.
func (e *colorEnv) parse(t token, depth int) (spaceColor, bool) {
	if depth > maxColorDepth {
		return spaceColor{}, false
	}
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		return e.keyword(lower(t.value))
	case tHash:
		return parseHex(t.value)
	case tFunction:
		args := t.children
		switch lower(t.value) {
		case "rgb", "rgba":
			return e.rgb(args, depth)
		case "hsl", "hsla":
			return e.hsl(args, depth)
		case "hwb":
			return e.polarFunc(spaceHWB, args, depth)
		case "lab":
			return e.polarFunc(spaceLab, args, depth)
		case "lch":
			return e.polarFunc(spaceLCH, args, depth)
		case "oklab":
			return e.polarFunc(spaceOKLab, args, depth)
		case "oklch":
			return e.polarFunc(spaceOKLCh, args, depth)
		case "color":
			return e.colorFunc(args, depth)
		case "color-mix":
			return e.mix(args, depth)
		case "light-dark":
			return e.lightDark(args, depth)
		case "contrast-color":
			return e.contrast(args, depth)
		}
	}
	return spaceColor{}, false
}

// keyword parses a named colour, a system colour, transparent or
// currentColor (CSS Color 4, 6).
func (e *colorEnv) keyword(n string) (spaceColor, bool) {
	switch n {
	case "transparent":
		// CSS Color 4, 6.3: transparent black.
		return srgbColor(0, 0, 0, 0), true
	case "currentcolor":
		if e.current == nil {
			e.usedCurrent = true
			return srgbColor(0, 0, 0, 1), true
		}
		c := e.current
		return srgbColor(c.R, c.G, c.B, c.A), true
	}
	if rgb, ok := namedColors[n]; ok {
		return srgbColor(float64(rgb[0])/255, float64(rgb[1])/255, float64(rgb[2])/255, 1), true
	}
	if c, ok := systemColors[n]; ok {
		return srgbColor(float64(c[0])/255, float64(c[1])/255, float64(c[2])/255, float64(c[3])/255), true
	}
	return spaceColor{}, false
}

// parseHex parses the digits of a hex colour (CSS Color 4, 5.2).
func parseHex(s string) (spaceColor, bool) {
	var d [8]float64
	n := len(s)
	if n != 3 && n != 4 && n != 6 && n != 8 {
		return spaceColor{}, false
	}
	for i := range n {
		c := rune(s[i])
		if !isHexDigit(c) {
			return spaceColor{}, false
		}
		switch {
		case c >= 'a':
			d[i] = float64(c - 'a' + 10)
		case c >= 'A':
			d[i] = float64(c - 'A' + 10)
		default:
			d[i] = float64(c - '0')
		}
	}
	ch := func(i int) float64 {
		if n <= 4 {
			return d[i] * 17 / 255
		}
		return (float64(d[2*i]*16) + d[2*i+1]) / 255
	}
	c := srgbColor(ch(0), ch(1), ch(2), 1)
	if n == 4 || n == 8 {
		c.alpha = ch(3)
	}
	return c, true
}

// colorArgs are the arguments of a colour function in the modern syntax
// (CSS Color 4, 4.1.1): an origin colour for the relative syntax (CSS
// Color 5, 4.1), the components, and the alpha, nil when omitted.
type colorArgs struct {
	origin *spaceColor
	ch     []token
	alpha  *token
	legacy bool
}

// splitColorArgs splits the arguments of a colour function into its
// channels and alpha, in the legacy comma syntax (CSS Color 4, 4.1.2),
// which allows neither none nor an origin, or the modern space syntax.
// n is the number of channels before the alpha.
func (e *colorEnv) splitColorArgs(list []token, n int, legacyOK bool, depth int) (colorArgs, bool) {
	args := significant(list)
	var out colorArgs
	for _, a := range args {
		if a.typ == tComma {
			out.legacy = true
		}
	}
	if out.legacy {
		if !legacyOK || len(args) != 2*n-1 && len(args) != 2*n+1 {
			return colorArgs{}, false
		}
		for i := 1; i < len(args); i += 2 {
			if args[i].typ != tComma {
				return colorArgs{}, false
			}
		}
		for i := 0; i < len(args); i += 2 {
			if args[i].isIdent("none") {
				return colorArgs{}, false
			}
			if i/2 < n {
				out.ch = append(out.ch, args[i])
			} else {
				out.alpha = &args[i]
			}
		}
		return out, true
	}
	if len(args) >= 2 && args[0].isIdent("from") {
		o, ok := e.parse(args[1], depth+1)
		if !ok {
			return colorArgs{}, false
		}
		out.origin = &o
		args = args[2:]
	}
	switch {
	case len(args) == n:
		out.ch = args
	case len(args) == n+2 && args[n].isDelim('/'):
		out.ch, out.alpha = args[:n], &args[n+1]
	default:
		return colorArgs{}, false
	}
	return out, true
}

// compKind is what a colour component accepts.
type compKind uint8

const (
	// compNumber is a <number> or a <percentage> of a reference.
	compNumber compKind = iota
	// compHue is a <hue>: a number of degrees or an angle (CSS Color 4,
	// 4.3).
	compHue
)

// component parses one component of the modern syntax: a number, a
// percentage of ref, none, a channel keyword of vars or a math function.
// isPct reports a percentage, which the legacy syntax checks.
func component(t token, kind compKind, ref float64, vars *channelVars) (v float64, isPct, none, ok bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		return t.num, false, false, numeric.Finite(t.num)
	case tPercentage:
		if kind == compHue {
			return 0, false, false, false
		}
		return t.num / 100 * ref, true, false, numeric.Finite(t.num)
	case tDimension:
		if kind == compHue {
			v, ok := parseAngle(t)
			return v, false, false, ok
		}
	case tIdent:
		if t.isIdent("none") {
			return 0, false, true, true
		}
		if vars != nil {
			for i, n := range vars.names {
				if n != "" && t.isIdent(n) {
					return vars.vals[i], false, false, true
				}
			}
		}
	case tFunction:
		if !isCalc(&t) {
			return 0, false, false, false
		}
		v, ty, ok := calcConst(&t, vars)
		switch {
		case !ok:
		case ty == typeNumber:
			return v, false, false, true
		case ty == typePct && kind != compHue:
			return v / 100 * ref, true, false, true
		case ty == typeAngle && kind == compHue:
			return v, false, false, true
		}
	}
	return 0, false, false, false
}

// alphaOf parses the alpha of a colour function (CSS Color 4, 4.2),
// clamped to [0, 1], defaulting to def when omitted.
func alphaOf(t *token, def float64, vars *channelVars, legacy bool) (v float64, none, ok bool) {
	if t == nil {
		return def, false, true
	}
	if legacy && t.isIdent("none") {
		return 0, false, false
	}
	v, _, none, ok = component(*t, compNumber, 1, vars)
	return clamp01(v), none, ok
}

// relativeVars binds the channel keywords of a relative colour (CSS Color
// 5, 4.1) to the origin converted to the function's space, scaled to
// the function's reference ranges by k, a missing component as zero.
func relativeVars(origin *spaceColor, s colorSpace, names [3]string, k [3]float64) *channelVars {
	if origin == nil {
		return nil
	}
	o := origin.convert(s)
	v := &channelVars{names: [4]string{names[0], names[1], names[2], "alpha"}}
	for i := range 3 {
		v.vals[i] = o.c[i] * k[i]
	}
	v.vals[3] = o.alpha
	return v
}

// finish applies the alpha of a parsed colour function: its own, or the
// origin's for a relative colour that omits it (CSS Color 5, 4.1).
func finish(c spaceColor, a colorArgs, vars *channelVars) (spaceColor, bool) {
	def := 1.0
	if vars != nil {
		def = vars.vals[3]
	}
	alpha, none, ok := alphaOf(a.alpha, def, vars, a.legacy)
	c.alpha, c.none[3] = alpha, none
	return c, ok
}

// rgb parses rgb() and rgba() (CSS Color 4, 5.1; CSS Color 5, 4.3). The
// legacy syntax takes three numbers or three percentages. Channels clamp
// to [0, 255] at parsed-value time.
func (e *colorEnv) rgb(list []token, depth int) (spaceColor, bool) {
	a, ok := e.splitColorArgs(list, 3, true, depth)
	if !ok {
		return spaceColor{}, false
	}
	vars := relativeVars(a.origin, spaceSRGB, [3]string{"r", "g", "b"}, [3]float64{255, 255, 255})
	c := spaceColor{space: spaceSRGB}
	var pcts [3]bool
	for i, t := range a.ch {
		v, pct, none, ok := component(t, compNumber, 255, vars)
		if !ok {
			return spaceColor{}, false
		}
		pcts[i], c.none[i] = pct, none
		c.c[i] = math.Max(0, math.Min(255, v)) / 255
	}
	if a.legacy && (pcts[0] != pcts[1] || pcts[1] != pcts[2]) {
		return spaceColor{}, false
	}
	return finish(c, a, vars)
}

// hsl parses hsl() and hsla() (CSS Color 4, 7; CSS Color 5, 4.4). The
// legacy syntax takes percentages for saturation and lightness. A
// negative saturation clamps to 0 at parsed-value time; a saturation past
// 100% is kept.
func (e *colorEnv) hsl(list []token, depth int) (spaceColor, bool) {
	a, ok := e.splitColorArgs(list, 3, true, depth)
	if !ok {
		return spaceColor{}, false
	}
	vars := relativeVars(a.origin, spaceHSL, [3]string{"h", "s", "l"}, [3]float64{1, 1, 1})
	c := spaceColor{space: spaceHSL}
	for i, t := range a.ch {
		kind := compNumber
		if i == 0 {
			kind = compHue
		}
		v, pct, none, ok := component(t, kind, 100, vars)
		if !ok || a.legacy && i > 0 && !pct {
			return spaceColor{}, false
		}
		c.c[i], c.none[i] = v, none
	}
	c.c[1] = math.Max(0, c.c[1])
	return finish(c, a, vars)
}

// polarRefs are the percentage reference ranges of the components of
// hwb(), lab(), lch(), oklab() and oklch() (CSS Color 4, 8 and 9.3), and
// their channel keywords (CSS Color 5, 4.5 to 4.9).
var polarRefs = map[colorSpace]struct {
	ref   [3]float64
	names [3]string
}{
	spaceHWB:   {[3]float64{0, 100, 100}, [3]string{"h", "w", "b"}},
	spaceLab:   {[3]float64{100, 125, 125}, [3]string{"l", "a", "b"}},
	spaceLCH:   {[3]float64{100, 150, 0}, [3]string{"l", "c", "h"}},
	spaceOKLab: {[3]float64{1, 0.4, 0.4}, [3]string{"l", "a", "b"}},
	spaceOKLCh: {[3]float64{1, 0.4, 0}, [3]string{"l", "c", "h"}},
}

// polarFunc parses hwb(), lab(), lch(), oklab() and oklch(), which have
// only the modern syntax (CSS Color 4, 8 and 9.3). Lightness clamps to
// its range and chroma to 0 at parsed-value time.
func (e *colorEnv) polarFunc(s colorSpace, list []token, depth int) (spaceColor, bool) {
	a, ok := e.splitColorArgs(list, 3, false, depth)
	if !ok {
		return spaceColor{}, false
	}
	refs := polarRefs[s]
	vars := relativeVars(a.origin, s, refs.names, [3]float64{1, 1, 1})
	hue := hueIndex(s)
	c := spaceColor{space: s}
	for i, t := range a.ch {
		kind := compNumber
		if i == hue {
			kind = compHue
		}
		v, _, none, ok := component(t, kind, refs.ref[i], vars)
		if !ok {
			return spaceColor{}, false
		}
		c.c[i], c.none[i] = v, none
	}
	switch s { //nolint:exhaustive // hwb clamps nothing
	case spaceLab, spaceLCH:
		c.c[0] = math.Max(0, math.Min(100, c.c[0]))
	case spaceOKLab, spaceOKLCh:
		c.c[0] = math.Max(0, math.Min(1, c.c[0]))
	}
	if s == spaceLCH || s == spaceOKLCh {
		c.c[1] = math.Max(0, c.c[1])
	}
	return finish(c, a, vars)
}

// colorFunc parses color() (CSS Color 4, 10.1; CSS Color 5, 5.1): a
// predefined space, three components with 100% as 1, unclamped.
func (e *colorEnv) colorFunc(list []token, depth int) (spaceColor, bool) {
	args := significant(list)
	var origin *spaceColor
	if len(args) >= 2 && args[0].isIdent("from") {
		o, ok := e.parse(args[1], depth+1)
		if !ok {
			return spaceColor{}, false
		}
		origin, args = &o, args[2:]
	}
	if len(args) == 0 || args[0].typ != tIdent {
		return spaceColor{}, false
	}
	s, ok := predefinedSpaces[lower(args[0].value)]
	if !ok {
		return spaceColor{}, false
	}
	a := colorArgs{origin: origin}
	switch rest := args[1:]; {
	case len(rest) == 3:
		a.ch = rest
	case len(rest) == 5 && rest[3].isDelim('/'):
		a.ch, a.alpha = rest[:3], &rest[4]
	default:
		return spaceColor{}, false
	}
	names := [3]string{"r", "g", "b"}
	if s == spaceXYZD50 || s == spaceXYZD65 {
		names = [3]string{"x", "y", "z"}
	}
	vars := relativeVars(origin, s, names, [3]float64{1, 1, 1})
	c := spaceColor{space: s}
	for i, t := range a.ch {
		v, _, none, ok := component(t, compNumber, 1, vars)
		if !ok {
			return spaceColor{}, false
		}
		c.c[i], c.none[i] = v, none
	}
	return finish(c, a, vars)
}

// hueMethod is a <hue-interpolation-method> (CSS Color 4, 13.6).
type hueMethod uint8

// The hue interpolation methods; shorter is the default.
const (
	hueShorter hueMethod = iota
	hueLonger
	hueIncreasing
	hueDecreasing
)

// parseInterpolation parses a <color-interpolation-method>: in, a space,
// and for a cylindrical space an optional hue method (CSS Color 4, 12.1).
func parseInterpolation(l []token) (colorSpace, hueMethod, bool) {
	l = significant(l)
	if len(l) < 2 || !l[0].isIdent("in") || l[1].typ != tIdent {
		return 0, 0, false
	}
	s, ok := interpolationSpaces[lower(l[1].value)]
	if !ok {
		return 0, 0, false
	}
	switch {
	case len(l) == 2:
		return s, hueShorter, true
	case len(l) == 4 && hueIndex(s) >= 0 && l[2].typ == tIdent && l[3].isIdent("hue"):
		m, ok := map[string]hueMethod{"shorter": hueShorter, "longer": hueLonger, "increasing": hueIncreasing, "decreasing": hueDecreasing}[lower(l[2].value)]
		return s, m, ok
	}
	return 0, 0, false
}

// interpolate interpolates a and b in the space s at progress p, from a
// at 0 to b at 1 (CSS Color 4, 13): in premultiplied alpha (13.5), a
// component missing in one colour taking the other's (13.4), the hue by
// method (13.6).
func interpolate(a, b spaceColor, p float64, s colorSpace, method hueMethod) spaceColor {
	a, b = a.to(s), b.to(s)
	for i := range 4 {
		switch {
		case a.none[i] && !b.none[i]:
			a = setComponent(a, i, b)
		case b.none[i] && !a.none[i]:
			b = setComponent(b, i, a)
		}
	}
	out := spaceColor{space: s}
	out.none[3] = a.none[3] && b.none[3]
	aa, ba := a.alpha, b.alpha
	if a.none[3] {
		aa, ba = 1, 1
	}
	out.alpha = aa + float64((ba-aa)*p)
	hue := hueIndex(s)
	for i := range 3 {
		out.none[i] = a.none[i] && b.none[i]
		x, y := a.c[i], b.c[i]
		if i == hue {
			x, y = fixHues(x, y, method)
			out.c[i] = x + float64((y-x)*p)
			continue
		}
		x, y = float64(x*aa), float64(y*ba)
		v := x + float64((y-x)*p)
		if out.alpha != 0 {
			v /= out.alpha
		}
		out.c[i] = v
	}
	if hue >= 0 {
		out.c[hue] = math.Mod(out.c[hue], 360)
		if out.c[hue] < 0 {
			out.c[hue] += 360
		}
	}
	return out
}

// setComponent returns c with its component i, the alpha for 3, taken
// from o.
func setComponent(c spaceColor, i int, o spaceColor) spaceColor {
	if i == 3 {
		c.alpha = o.alpha
	} else {
		c.c[i] = o.c[i]
	}
	c.none[i] = false
	return c
}

// fixHues adjusts two hues for the interpolation method (CSS Color 4,
// 13.6), both first reduced to [0, 360).
//
// Under longer, two equal hues turn a full circle upward: the clause
// adds 360 to the second hue when θ₂ - θ₁ is in (-180, 0] (13.6.2).
// Chromium's screen turns downward there and its print draws no turn at
// all; the two disagree with each other, so litho keeps the clause.
//
// # Corpus evidence
//
//   - wpt: css-images/gradient/gradient-none-interpolation, row
//     noneHueLonger (oklch 90 to a none hue): Chromium's screenshot
//     passes red, purple, blue, then green; its PDF route keeps an olive
//     ramp with no hue change.
func fixHues(a, b float64, m hueMethod) (float64, float64) {
	norm := func(h float64) float64 {
		h = math.Mod(h, 360)
		if h < 0 {
			h += 360
		}
		return h
	}
	a, b = norm(a), norm(b)
	d := b - a
	switch m {
	case hueShorter:
		if d > 180 {
			a += 360
		} else if d < -180 {
			b += 360
		}
	case hueLonger:
		if d > 0 && d < 180 {
			a += 360
		} else if d > -180 && d <= 0 {
			b += 360
		}
	case hueIncreasing:
		if d < 0 {
			b += 360
		}
	case hueDecreasing:
		if d > 0 {
			a += 360
		}
	}
	return a, b
}

// mix parses color-mix() (CSS Color 5, 3): an optional interpolation
// method, Oklab by default, and two colours, each with an optional
// percentage from 0% to 100%. CSS Color 5, 3 takes one or more colours;
// Chromium parses exactly two, and litho follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 refuses color-mix(in srgb, red) and
//     color-mix(in oklab, teal, olive, blue) (CSS.supports false).
func (e *colorEnv) mix(list []token, depth int) (spaceColor, bool) {
	parts := splitCommas(list)
	space, method := spaceOKLab, hueShorter
	if len(parts) == 3 {
		var ok bool
		if space, method, ok = parseInterpolation(parts[0]); !ok {
			return spaceColor{}, false
		}
		parts = parts[1:]
	}
	if len(parts) != 2 {
		return spaceColor{}, false
	}
	var cols [2]spaceColor
	var pcts [2]float64
	var given [2]bool
	for i, p := range parts {
		c, pct, has, ok := e.mixItem(p, depth)
		if !ok {
			return spaceColor{}, false
		}
		cols[i], pcts[i], given[i] = c, pct, has
	}
	// CSS Color 5, 3.2 and 3.3: an omitted percentage takes what the
	// other leaves of 100%; a sum under 100% scales both up and
	// multiplies the result's alpha by the sum.
	switch {
	case !given[0] && !given[1]:
		pcts = [2]float64{50, 50}
	case !given[0]:
		pcts[0] = 100 - pcts[1]
	case !given[1]:
		pcts[1] = 100 - pcts[0]
	}
	sum := pcts[0] + pcts[1]
	p, mult := 0.5, 0.0
	if sum > 0 {
		p = pcts[1] / sum
		mult = math.Min(sum, 100) / 100
	}
	out := interpolate(cols[0], cols[1], p, space, method)
	out.alpha *= mult
	return out, true
}

// mixItem parses one colour of color-mix() and its percentage, in either
// order.
func (e *colorEnv) mixItem(l []token, depth int) (c spaceColor, pct float64, hasPct, ok bool) {
	l = significant(l)
	if len(l) == 0 || len(l) > 2 {
		return spaceColor{}, 0, false, false
	}
	ci := 0
	if len(l) == 2 {
		v, isPct := mixPercent(l[0])
		if isPct {
			ci = 1
		} else if v, isPct = mixPercent(l[1]); !isPct {
			return spaceColor{}, 0, false, false
		}
		if v < 0 || v > 100 {
			return spaceColor{}, 0, false, false
		}
		pct, hasPct = v, true
	}
	c, ok = e.parse(l[ci], depth+1)
	return c, pct, hasPct, ok
}

// mixPercent reads a <percentage> of color-mix(), a math function
// included.
func mixPercent(t token) (float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are not percentages
	case tPercentage:
		return t.num, numeric.Finite(t.num)
	case tFunction:
		if isCalc(&t) {
			if v, ty, ok := calcConst(&t, nil); ok && ty == typePct {
				return v, true
			}
		}
	}
	return 0, false
}

// lightDark parses light-dark() (CSS Color 5, 6.2), which takes its first
// colour under a light colour scheme. litho does not support color-scheme
// and warns of it, so every element's used scheme is light, as Chromium
// prints a page that sets none.
func (e *colorEnv) lightDark(list []token, depth int) (spaceColor, bool) {
	parts := splitCommas(list)
	if len(parts) != 2 {
		return spaceColor{}, false
	}
	var out spaceColor
	for i, p := range parts {
		p = significant(p)
		if len(p) != 1 {
			return spaceColor{}, false
		}
		c, ok := e.parse(p[0], depth+1)
		if !ok {
			return spaceColor{}, false
		}
		if i == 0 {
			out = c
		}
	}
	return out, true
}

// contrast parses contrast-color() (CSS Color 5, 7): white or black,
// whichever contrasts more with the colour. The specification leaves the
// contrast measure to the user agent; litho takes the WCAG 2.1 contrast
// ratio of the colour painted in sRGB, which gives Chromium's choice for
// every colour measured, and white on a tie.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 resolves contrast-color() to black for
//     rgb(118 118 118), rgb(255 0 0), rgb(0 160 0) and white, to white
//     for rgb(200 0 0), rgb(0 128 0), navy and transparent, as the WCAG
//     2.1 ratio decides.
func (e *colorEnv) contrast(list []token, depth int) (spaceColor, bool) {
	l := significant(list)
	if len(l) != 1 {
		return spaceColor{}, false
	}
	c, ok := e.parse(l[0], depth+1)
	if !ok {
		return spaceColor{}, false
	}
	p := c.paint()
	lum := relativeLuminance(p)
	if (lum+0.05)/0.05 > 1.05/(lum+0.05) {
		return srgbColor(0, 0, 0, 1), true
	}
	return srgbColor(1, 1, 1, 1), true
}

// numberOrPercent parses a <number> or a <percentage>, a math function
// included, the percentage as a fraction: an <opacity-value> (CSS Color
// 4, 3.3).
func numberOrPercent(t token) (float64, bool) {
	if t.typ == tIdent {
		return 0, false
	}
	v, _, _, ok := component(t, compNumber, 1, nil)
	return v, ok
}

// relativeLuminance is the WCAG 2.1 relative luminance of an sRGB colour,
// its alpha ignored.
func relativeLuminance(c Color) float64 {
	return float64(0.2126*linSRGB(c.R)) + float64(0.7152*linSRGB(c.G)) + float64(0.0722*linSRGB(c.B))
}

// systemColors maps the system colours of CSS Color 4, 6.2 and the
// deprecated ones of its Appendix A to the RGBA values Chromium prints,
// alpha 0 to 255. The specification leaves their values to the user
// agent and the platform.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 in the pinned image computes and prints
//     each of these values; Highlight is the one translucent colour.
var systemColors = map[string][4]uint8{
	"accentcolor": {0, 117, 255, 255}, "accentcolortext": {255, 255, 255, 255},
	"activetext": {255, 0, 0, 255}, "buttonborder": {0, 0, 0, 255},
	"buttonface": {239, 239, 239, 255}, "buttontext": {0, 0, 0, 255},
	"canvas": {255, 255, 255, 255}, "canvastext": {0, 0, 0, 255},
	"field": {255, 255, 255, 255}, "fieldtext": {0, 0, 0, 255},
	"graytext": {128, 128, 128, 255}, "highlight": {0, 65, 198, 204},
	"highlighttext": {255, 255, 255, 255}, "linktext": {0, 0, 238, 255},
	"mark": {255, 255, 0, 255}, "marktext": {0, 0, 0, 255},
	"selecteditem": {25, 103, 210, 255}, "selecteditemtext": {255, 255, 255, 255},
	"visitedtext":  {85, 26, 139, 255},
	"activeborder": {0, 0, 0, 255}, "activecaption": {255, 255, 255, 255},
	"appworkspace": {255, 255, 255, 255}, "background": {255, 255, 255, 255},
	"buttonhighlight": {239, 239, 239, 255}, "buttonshadow": {239, 239, 239, 255},
	"captiontext": {0, 0, 0, 255}, "inactiveborder": {0, 0, 0, 255},
	"inactivecaption": {255, 255, 255, 255}, "inactivecaptiontext": {128, 128, 128, 255},
	"infobackground": {255, 255, 255, 255}, "infotext": {0, 0, 0, 255},
	"menu": {255, 255, 255, 255}, "menutext": {0, 0, 0, 255},
	"scrollbar": {255, 255, 255, 255}, "threeddarkshadow": {0, 0, 0, 255},
	"threedface": {239, 239, 239, 255}, "threedhighlight": {0, 0, 0, 255},
	"threedlightshadow": {0, 0, 0, 255}, "threedshadow": {0, 0, 0, 255},
	"window": {255, 255, 255, 255}, "windowframe": {0, 0, 0, 255},
	"windowtext": {0, 0, 0, 255},
}
