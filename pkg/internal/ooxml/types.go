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
// or empty value is true, as an empty element is; "0", "false", "off" and
// "none" are false.
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

// Tint lightens c towards white as Word computes a themeTint (ECMA-376
// Part 1, 17.3.2.41): the lightness of c in HSL taken to l·t + 1 - t, each
// component then cut down to a whole 255th; t in [0, 1].
//
// # Corpus evidence
//
//   - litho/docx run-theme-fonts-colors: Word paints the eleven theme
//     colours of the Office theme under themeTint 99 and themeShade BF and
//     80; the rule gives 22 of the 27 colours exactly and the others within
//     one level (156,195,229 for Word's 156,194,229), where a blend of the
//     components towards white or black is off by up to 33.
//   - libreoffice/docx, litho/docx, openxmlsdk/docx, poi/docx: of the 106
//     w:color and w:shd values Word stored beside a theme colour and a tint
//     or a shade, the rule gives 80 exactly, the blend 23.
func Tint(c css.Color, t float64) css.Color {
	t = clamp01(t)
	h, s, l := toHSL(c)
	return wordLevels(fromHSL(h, s, clamp01(float64(l*t)+1-t), c.A))
}

// Shade darkens c towards black as Word computes a themeShade (ECMA-376
// Part 1, 17.3.2.41): the lightness of c in HSL scaled by s, each
// component then cut down to a whole 255th; s in [0, 1]. The evidence is
// [Tint]'s.
func Shade(c css.Color, s float64) css.Color {
	s = clamp01(s)
	h, sat, l := toHSL(c)
	return wordLevels(fromHSL(h, sat, float64(l*s), c.A))
}

// wordLevels cuts each component of c down to a whole 255th.
func wordLevels(c css.Color) css.Color {
	f := func(v float64) float64 { return math.Floor(clamp01(v)*255) / 255 }
	return css.Color{R: f(c.R), G: f(c.G), B: f(c.B), A: c.A}
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
