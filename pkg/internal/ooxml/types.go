package ooxml

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// The units of OOXML lengths, in points.
const (
	// TwipPt is one twentieth of a point (ECMA-376 Part 1, 17.18.105,
	// ST_TwipsMeasure).
	TwipPt = 1.0 / 20
	// EMUPt is one English Metric Unit, 1/12700 of a point (ECMA-376 Part
	// 1, 20.1.10.16, ST_Coordinate).
	EMUPt = 1.0 / 12700
)

// maxLength bounds every length read, in points: a few pages beyond the
// largest page side, so arithmetic on lengths stays finite. Anti-DoS
// guard, not spec-mandated.
const maxLength = 1e6

// Measure parses s as a length in units of unit points each, or as a
// universal measure with its own unit ("2.5cm", "12pt", ECMA-376 Part 1,
// 22.9.2.15 ST_UniversalMeasure), returning points clamped to a finite
// range and whether s parsed.
func Measure(s string, unit float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	scale := unit
	for _, u := range []struct {
		suffix string
		pt     float64
	}{{"mm", 72 / 25.4}, {"cm", 72 / 2.54}, {"in", 72}, {"pt", 1}, {"pc", 12}, {"pi", 12}} {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			s, scale = num, u.pt
			break
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return max(-maxLength, min(maxLength, v*scale)), true
}

// Twips parses a twips measure into points (ST_TwipsMeasure,
// ST_SignedTwipsMeasure).
func Twips(s string) (float64, bool) { return Measure(s, TwipPt) }

// EMU parses an EMU coordinate into points (ST_Coordinate).
func EMU(s string) (float64, bool) { return Measure(s, EMUPt) }

// Int parses a decimal integer, clamped to the range of an int32, and
// whether it parsed.
func Int(s string) (int, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) {
		return 0, false
	}
	return int(max(math.MinInt32, min(math.MaxInt32, math.Trunc(v)))), true // fits int: clamped to int32 above
}

// OnOff parses an ST_OnOff value (ECMA-376 Part 1, 22.9.2.7): an absent
// or empty value is true, as an empty element is; "0", "false" and "off"
// are false.
func OnOff(s string, present bool) bool {
	if !present {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

// Toggle returns the ST_OnOff value of e's val attribute: true for an
// element without one (ECMA-376 Part 1, 17.7.3, toggle properties).
func Toggle(e *Element) bool {
	v, ok := e.Attr("val")
	return OnOff(v, ok)
}

// HexColor parses an ST_HexColorRGB ("1F3864", ECMA-376 Part 1,
// 22.9.2.5) into an opaque colour; "auto" and anything else that does not
// parse report false.
func HexColor(s string) (css.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return css.Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return css.Color{}, false
	}
	return css.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}, true
}

// Tint lightens c towards white by the fraction 1-t of the way, as an
// ST_UcharHexNumber themeTint does (ECMA-376 Part 1, 17.3.2.41: the
// colour times t plus white times 1-t), t in [0, 1].
func Tint(c css.Color, t float64) css.Color {
	t = max(0, min(1, t))
	return css.Color{R: c.R*t + 1 - t, G: c.G*t + 1 - t, B: c.B*t + 1 - t, A: c.A}
}

// Shade darkens c towards black, the colour times s (ECMA-376 Part 1,
// 17.3.2.41, themeShade), s in [0, 1].
func Shade(c css.Color, s float64) css.Color {
	s = max(0, min(1, s))
	return css.Color{R: c.R * s, G: c.G * s, B: c.B * s, A: c.A}
}

// HexByte parses a two-digit hexadecimal byte ("BF") into a fraction of
// 255, and whether it parsed.
func HexByte(s string) (float64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 16, 8)
	if err != nil {
		return 0, false
	}
	return float64(v) / 255, true
}
