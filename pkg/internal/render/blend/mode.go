package blend

import "math"

// Mode is a standard blend mode of ISO 32000-1, 11.3.5, blending by the
// formulas of ISO 32000-1 unless [Mode.In] selects another [Edition]. The
// zero value is Normal.
type Mode uint8

// The standard blend modes: the separable ones of Table 136, then the
// non-separable ones of Table 137.
const (
	Normal Mode = iota
	Multiply
	Screen
	Overlay
	Darken
	Lighten
	ColorDodge
	ColorBurn
	HardLight
	SoftLight
	Difference
	Exclusion
	Hue
	Saturation
	Color
	Luminosity
)

var modeNames = [...]string{
	Normal:     "Normal",
	Multiply:   "Multiply",
	Screen:     "Screen",
	Overlay:    "Overlay",
	Darken:     "Darken",
	Lighten:    "Lighten",
	ColorDodge: "ColorDodge",
	ColorBurn:  "ColorBurn",
	HardLight:  "HardLight",
	SoftLight:  "SoftLight",
	Difference: "Difference",
	Exclusion:  "Exclusion",
	Hue:        "Hue",
	Saturation: "Saturation",
	Color:      "Color",
	Luminosity: "Luminosity",
}

// ParseMode returns the mode a blend-mode name designates, the name the
// graphics state holds in BlendMode. Compatible is a synonym for Normal
// (Table 136). An unknown name reports false.
func ParseMode(name string) (Mode, bool) {
	if name == "Compatible" {
		return Normal, true
	}
	for m, n := range modeNames {
		if n == name {
			return Mode(m), true //nolint:gosec // m indexes modeNames, 16 entries, so it fits a uint8.
		}
	}
	return Normal, false
}

// String returns the mode's PDF name.
func (m Mode) String() string {
	if b := m.base(); int(b) < len(modeNames) {
		return modeNames[b]
	}
	return "Normal"
}

// Separable reports whether the mode applies to each colour component on its
// own (Table 136) rather than to the colour as a whole (Table 137).
func (m Mode) Separable() bool { return m.base() < Hue }

// Blend returns B(cb, cs), the blend function of the mode applied to the
// unpremultiplied backdrop colour cb and source colour cs, RGB components in
// [0, 1].
func (m Mode) Blend(cb, cs [3]float32) [3]float32 {
	switch m.base() {
	case Normal:
		return cs
	case Hue:
		// Table 137: SetLum(SetSat(Cs, Sat(Cb)), Lum(Cb)).
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case Saturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case Color:
		return setLum(cs, lum(cb))
	case Luminosity:
		return setLum(cb, lum(cs))
	case Multiply, Screen, Overlay, Darken, Lighten, ColorDodge, ColorBurn,
		HardLight, SoftLight, Difference, Exclusion:
		return [3]float32{m.channel(cb[0], cs[0]), m.channel(cb[1], cs[1]), m.channel(cb[2], cs[2])}
	}
	return cs
}

// channel applies a separable blend function of Table 136 to one component.
func (m Mode) channel(cb, cs float32) float32 {
	switch m.base() {
	case Multiply:
		return cb * cs
	case Screen:
		return screen(cb, cs)
	case Overlay:
		return hardLight(cs, cb)
	case Darken:
		return min(cb, cs)
	case Lighten:
		return max(cb, cs)
	case ColorDodge:
		// Per ISO 32000-1, Table 136; ISO 32000-2, Table 136 returns 0 first
		// where cb is 0, which parts from it only where cs is 1.
		if m&edition2 != 0 && cb == 0 {
			return 0
		}
		if cs < 1 {
			return min(1, cb/(1-cs))
		}
		return 1
	case ColorBurn:
		// Per ISO 32000-1, Table 136; ISO 32000-2, Table 136 returns 1 first
		// where cb is 1, which parts from it only where cs is 0.
		if m&edition2 != 0 && cb == 1 {
			return 1
		}
		if cs > 0 {
			return 1 - min(1, (1-cb)/cs)
		}
		return 0
	case HardLight:
		return hardLight(cb, cs)
	case SoftLight:
		if cs <= 0.5 {
			return cb - (1-2*cs)*cb*(1-cb)
		}
		return cb + (2*cs-1)*(softLightD(cb)-cb)
	case Difference:
		if cb > cs {
			return cb - cs
		}
		return cs - cb
	case Exclusion:
		return cb + cs - 2*cb*cs
	case Normal, Hue, Saturation, Color, Luminosity:
		// Normal selects the source; the non-separable modes never reach a
		// single component.
	}
	return cs
}

func screen(cb, cs float32) float32 { return cb + cs - cb*cs }

// hardLight is Table 136's HardLight: Multiply(cb, 2cs) up to 0.5, then
// Screen(cb, 2cs - 1). Overlay is HardLight with its arguments swapped.
func hardLight(cb, cs float32) float32 {
	if cs <= 0.5 {
		return cb * 2 * cs
	}
	return screen(cb, 2*cs-1)
}

// softLightD is the D(x) of Table 136's SoftLight.
func softLightD(x float32) float32 {
	if x <= 0.25 {
		return ((16*x-12)*x + 4) * x
	}
	return float32(math.Sqrt(float64(x)))
}

// lum is Lum(C) of 11.3.5: 0.3 R + 0.59 G + 0.11 B.
func lum(c [3]float32) float32 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }

// setLum is SetLum(C, l) of 11.3.5: shift C to luminosity l, then clip it
// into gamut.
func setLum(c [3]float32, l float32) [3]float32 {
	d := l - lum(c)
	return clipColor([3]float32{c[0] + d, c[1] + d, c[2] + d})
}

// clipColor is ClipColor(C) of 11.3.5: pull an out-of-gamut colour toward its
// own luminosity until every component lies in [0, 1].
func clipColor(c [3]float32) [3]float32 {
	l := lum(c)
	n := min(c[0], c[1], c[2])
	x := max(c[0], c[1], c[2])
	if n < 0 && l-n > 0 {
		for i := range c {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
	}
	if x > 1 && x-l > 0 {
		for i := range c {
			c[i] = l + (c[i]-l)*(1-l)/(x-l)
		}
	}
	return c
}

// sat is Sat(C) of 11.3.5: the spread between the largest and the smallest
// component.
func sat(c [3]float32) float32 { return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2]) }

// setSat is SetSat(C, s) of 11.3.5: keep the order of C's components, give
// them spread s, the smallest at 0.
func setSat(c [3]float32, s float32) [3]float32 {
	lo, mid, hi := 0, 1, 2
	if c[lo] > c[mid] {
		lo, mid = mid, lo
	}
	if c[mid] > c[hi] {
		mid, hi = hi, mid
	}
	if c[lo] > c[mid] {
		lo, mid = mid, lo
	}
	var out [3]float32
	if c[hi] > c[lo] {
		out[mid] = (c[mid] - c[lo]) * s / (c[hi] - c[lo])
		out[hi] = s
	}
	return out
}
