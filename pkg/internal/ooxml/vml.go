package ooxml

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// VMLStyle is the CSS-like style attribute of a VML shape (ECMA-376 Part
// 4, 14.1.2.19 style): its declarations by lower-cased property name, the
// last one winning.
type VMLStyle map[string]string

// maxVMLDeclarations caps the declarations read from one style attribute.
// Anti-DoS guard, not spec-mandated: Word writes about twenty.
const maxVMLDeclarations = 256

// ParseVMLStyle parses a VML style attribute ("position:absolute;
// width:12pt", Part 4, 14.1.2.19).
func ParseVMLStyle(s string) VMLStyle {
	st := VMLStyle{}
	for i, decl := range strings.Split(s, ";") {
		if i >= maxVMLDeclarations {
			break
		}
		name, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		st[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(val)
	}
	return st
}

// Length returns the property name as a length in points and whether it
// is set: a CSS length with its unit, or a number in units of unit points
// each, the shape's coordinate units inside a group and pixels at the top
// level (Part 4, 14.1.2.19).
func (s VMLStyle) Length(name string, unit float64) (float64, bool) {
	v, ok := s[name]
	if !ok {
		return 0, false
	}
	if num, ok := strings.CutSuffix(v, "px"); ok {
		v, unit = num, 0.75
	}
	return Measure(v, unit)
}

// vmlColors are the sixteen colour names of VML (Part 4, 14.1.2.19
// ST_ColorType, the HTML 4 names).
//
//nolint:gochecknoglobals // static table of the named colours
var vmlColors = map[string]uint32{
	"black": 0x000000, "silver": 0xC0C0C0, "gray": 0x808080, "white": 0xFFFFFF,
	"maroon": 0x800000, "red": 0xFF0000, "purple": 0x800080, "fuchsia": 0xFF00FF,
	"green": 0x008000, "lime": 0x00FF00, "olive": 0x808000, "yellow": 0xFFFF00,
	"navy": 0x000080, "blue": 0x0000FF, "teal": 0x008080, "aqua": 0x00FFFF,
}

// VMLColor parses a VML colour (Part 4, 14.1.2.19 fillcolor): "#rgb",
// "#rrggbb" or a colour name, an index in brackets that may follow
// ignored. A system colour or anything else that does not parse reports
// false.
func VMLColor(s string) (css.Color, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	if hex, ok := strings.CutPrefix(s, "#"); ok && len(hex) == 3 {
		s = "#" + string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if strings.HasPrefix(s, "#") {
		return HexColor(s)
	}
	if v, ok := vmlColors[strings.ToLower(s)]; ok {
		return css.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}, true
	}
	return css.Color{}, false
}

// VMLFraction parses a VML fraction (Part 4, 14.1.2.5 opacity): a number,
// a percentage, or a 16.16 fixed-point value with an f suffix ("32768f" is
// one half), clamped to [0, 1].
func VMLFraction(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	scale := 1.0
	switch {
	case strings.HasSuffix(s, "f"):
		s, scale = s[:len(s)-1], 1.0/65536
	case strings.HasSuffix(s, "%"):
		s, scale = s[:len(s)-1], 0.01
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v != v {
		return 0, false
	}
	return max(0, min(1, v*scale)), true
}

// VMLPair parses a pair of numbers ("21600,21600", Part 4, 14.1.2.19
// coordsize and coordorigin), each defaulting to def when absent.
func VMLPair(s string, def float64) (float64, float64) {
	a, b, _ := strings.Cut(s, ",")
	x, y := def, def
	if v, err := strconv.ParseFloat(strings.TrimSpace(a), 64); err == nil && v == v {
		x = max(-maxLength, min(maxLength, v))
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(b), 64); err == nil && v == v {
		y = max(-maxLength, min(maxLength, v))
	}
	return x, y
}
