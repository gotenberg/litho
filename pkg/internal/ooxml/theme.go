package ooxml

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// Theme is the part of a DrawingML theme the converters read (ECMA-376
// Part 1, 20.1.6): the colour scheme and the font scheme.
type Theme struct {
	// Colors maps the scheme colour names (dk1, lt1, dk2, lt2, accent1 to
	// accent6, hlink, folHlink) to their colours.
	Colors map[string]css.Color
	// Major and Minor are the heading and body fonts.
	Major, Minor FontScheme
}

// FontScheme is one font collection of a theme (ECMA-376 Part 1,
// 20.1.4.1.24 majorFont, 20.1.4.1.25 minorFont).
type FontScheme struct {
	// Latin, EastAsian and ComplexScript are the typefaces of the three
	// script classes, "" when the theme leaves one empty.
	Latin, EastAsian, ComplexScript string
	// Scripts maps an ISO 15924 script code ("Jpan", "Hans", "Arab") to
	// its typeface.
	Scripts map[string]string
}

// ParseTheme reads a theme part's root element, a:theme. A nil or partial
// theme yields what it holds.
func ParseTheme(root *Element) *Theme {
	t := &Theme{Colors: map[string]css.Color{}}
	elems := root.Child(NSA, "themeElements")
	if cs := elems.Child(NSA, "clrScheme"); cs != nil {
		for _, c := range cs.Children {
			if col, ok := SchemeColor(c, nil); ok {
				t.Colors[c.Local] = col
			}
		}
	}
	if fs := elems.Child(NSA, "fontScheme"); fs != nil {
		t.Major = parseFontScheme(fs.Child(NSA, "majorFont"))
		t.Minor = parseFontScheme(fs.Child(NSA, "minorFont"))
	}
	return t
}

// parseFontScheme reads a majorFont or minorFont element.
func parseFontScheme(e *Element) FontScheme {
	fs := FontScheme{Scripts: map[string]string{}}
	if e == nil {
		return fs
	}
	fs.Latin = attrOr(e.Child(NSA, "latin"), "typeface")
	fs.EastAsian = attrOr(e.Child(NSA, "ea"), "typeface")
	fs.ComplexScript = attrOr(e.Child(NSA, "cs"), "typeface")
	for _, c := range e.Children {
		if c.Is(NSA, "font") {
			if s, ok := c.Attr("script"); ok {
				fs.Scripts[s] = attrOr(c, "typeface")
			}
		}
	}
	return fs
}

// SchemeColor returns the colour a colour container element holds (a
// scheme entry such as a:dk1, or a fill's colour choice): its a:srgbClr,
// a:sysClr lastClr, or a:schemeClr resolved in theme, with lumMod and
// lumOff applied (ECMA-376 Part 1, 20.1.2.3).
func SchemeColor(e *Element, theme *Theme) (css.Color, bool) {
	if e == nil {
		return css.Color{}, false
	}
	for _, c := range e.Children {
		var col css.Color
		var ok bool
		switch {
		case c.Is(NSA, "srgbClr"):
			col, ok = HexColor(attrOr(c, "val"))
		case c.Is(NSA, "sysClr"):
			col, ok = HexColor(attrOr(c, "lastClr"))
		case c.Is(NSA, "schemeClr"):
			if theme != nil {
				col, ok = theme.Color(attrOr(c, "val"))
			}
		case c.Is(NSA, "prstClr"):
			col, ok = presetColor(attrOr(c, "val"))
		}
		if ok {
			return applyColorMods(col, c), true
		}
	}
	return css.Color{}, false
}

// presetColor returns the few preset colours (ECMA-376 Part 1,
// 20.1.10.47) documents use for shapes.
func presetColor(name string) (css.Color, bool) {
	switch name {
	case "black":
		return css.Color{A: 1}, true
	case "white":
		return css.Color{R: 1, G: 1, B: 1, A: 1}, true
	case "red":
		return css.Color{R: 1, A: 1}, true
	case "green":
		return css.Color{G: 128.0 / 255, A: 1}, true
	case "blue":
		return css.Color{B: 1, A: 1}, true
	case "yellow":
		return css.Color{R: 1, G: 1, A: 1}, true
	}
	return css.Color{}, false
}

// applyColorMods applies the colour transforms of c the converters honour:
// lumMod and lumOff in HSL space, tint and shade, and alpha (ECMA-376 Part
// 1, 20.1.2.3).
func applyColorMods(col css.Color, c *Element) css.Color {
	for _, m := range c.Children {
		v, ok := Int(attrOr(m, "val"))
		if !ok || m.Space != NSA {
			continue
		}
		f := float64(v) / 100000
		switch m.Local {
		case "lumMod", "lumOff":
			h, s, l := toHSL(col)
			if m.Local == "lumMod" {
				l *= f
			} else {
				l += f
			}
			col = fromHSL(h, s, max(0, min(1, l)), col.A)
		case "tint":
			col = Tint(col, f)
		case "shade":
			col = Shade(col, f)
		case "alpha":
			col.A = max(0, min(1, f))
		}
	}
	return col
}

// Color returns the theme colour of a WordprocessingML ST_ThemeColor name
// (ECMA-376 Part 1, 17.18.97: dark1, text1, background1, accent1...) or a
// DrawingML scheme name (dk1, tx1, bg1, accent1...), with the default
// colour mapping (text on dark 1, background on light 1).
func (t *Theme) Color(name string) (css.Color, bool) {
	if t == nil {
		return css.Color{}, false
	}
	key := strings.ToLower(name)
	switch key {
	case "dark1", "text1", "tx1":
		key = "dk1"
	case "light1", "background1", "bg1":
		key = "lt1"
	case "dark2", "text2", "tx2":
		key = "dk2"
	case "light2", "background2", "bg2":
		key = "lt2"
	case "hyperlink":
		key = "hlink"
	case "followedhyperlink", "folhlink":
		key = "folHlink"
	}
	c, ok := t.Colors[key]
	return c, ok
}

// Font returns the typeface of a WordprocessingML ST_Theme font name
// (ECMA-376 Part 1, 17.18.96: majorAscii, minorHAnsi, majorEastAsia,
// minorBidi...), "" for none.
func (t *Theme) Font(name string) string {
	if t == nil {
		return ""
	}
	fs := t.Minor
	if strings.HasPrefix(name, "major") {
		fs = t.Major
	}
	switch {
	case strings.HasSuffix(name, "EastAsia"):
		return fs.EastAsian
	case strings.HasSuffix(name, "Bidi"):
		return fs.ComplexScript
	}
	return fs.Latin
}

// toHSL converts c to hue, saturation and lightness, each in [0, 1].
func toHSL(c css.Color) (h, s, l float64) {
	hi, lo := max(c.R, c.G, c.B), min(c.R, c.G, c.B)
	l = (hi + lo) / 2
	if hi == lo {
		return 0, 0, l
	}
	d := hi - lo
	if l > 0.5 {
		s = d / (2 - hi - lo)
	} else {
		s = d / (hi + lo)
	}
	switch hi {
	case c.R:
		h = (c.G - c.B) / d
		if c.G < c.B {
			h += 6
		}
	case c.G:
		h = (c.B-c.R)/d + 2
	default:
		h = (c.R-c.G)/d + 4
	}
	return h / 6, s, l
}

// fromHSL converts hue, saturation and lightness back to a colour of alpha
// a.
func fromHSL(h, s, l, a float64) css.Color {
	if s == 0 {
		return css.Color{R: l, G: l, B: l, A: a}
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	hue := func(t float64) float64 {
		switch {
		case t < 0:
			t++
		case t > 1:
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return css.Color{R: hue(h + 1.0/3), G: hue(h), B: hue(h - 1.0/3), A: a}
}
