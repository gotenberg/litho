package odf

import (
	"math"
	"strconv"
	"strings"
)

// maxLength bounds a length in points: 1,000,000, some 350 metres.
// Anti-DoS guard, not spec-mandated: no page or box of a real document
// comes near, and a bounded value keeps the layout's arithmetic finite.
const maxLength = 1_000_000

// unitPoints maps the length units of ODF 1.3, Part 3, 18.3.18 (XSL 1.0,
// 5.9.13) to points.
var unitPoints = map[string]float64{
	"pt": 1, "pc": 12, "in": 72, "cm": 72 / 2.54, "mm": 72 / 25.4, "px": 0.75,
}

// Length parses an ODF length, a signed number and a unit (ODF 1.3, Part
// 3, 18.3.18, length): its value in points and whether it parsed. A
// number without a unit, the ODF 1.0 producers' habit, reads as
// centimetres would be a guess and is refused; so are values past
// [maxLength].
func Length(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	i := len(s)
	for i > 0 && (s[i-1] >= 'a' && s[i-1] <= 'z' || s[i-1] >= 'A' && s[i-1] <= 'Z') {
		i--
	}
	f, ok := unitPoints[strings.ToLower(s[i:])]
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	v *= f
	if math.Abs(v) > maxLength {
		return 0, false
	}
	return v, true
}

// Percent parses an ODF percentage, "115%" (ODF 1.3, Part 3, 18.3.23): the
// fraction, 1.15, and whether it parsed.
func Percent(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "%") {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-1]), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e6 {
		return 0, false
	}
	return v / 100, true
}

// Color is an sRGB colour, each channel from 0 to 255.
type Color struct {
	R, G, B uint8
}

// ParseColor parses an ODF colour, "#rrggbb" (ODF 1.3, Part 3, 18.3.8,
// color), and whether it parsed. "transparent", where an attribute allows
// it, is not a colour: the caller tests for it.
func ParseColor(s string) (Color, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 4 && s[0] == '#' {
		// #rgb, which LibreOffice reads though the schema does not allow it.
		s = "#" + strings.Repeat(s[1:2], 2) + strings.Repeat(s[2:3], 2) + strings.Repeat(s[3:4], 2)
	}
	if len(s) != 7 || s[0] != '#' {
		return Color{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return Color{}, false
	}
	return Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, true //nolint:gosec // 24 bits, each byte masked by the conversion
}

// Unquote returns a font family name with its quotes removed: svg:font-family
// and fo:font-family are CSS font-family lists (XSL 1.0, 7.8.2); the first
// family is returned.
func Unquote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s[0] == '\'' || s[0] == '"' {
		if j := strings.IndexByte(s[1:], s[0]); j >= 0 {
			return s[1 : j+1]
		}
		return strings.Trim(s, `'"`)
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
