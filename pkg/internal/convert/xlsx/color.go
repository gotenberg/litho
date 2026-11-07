package xlsx

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// defaultPalette is the legacy indexed colour table (ECMA-376 Part 1,
// 18.8.27 indexedColors, the default palette its note lists), which a
// stylesheet's colors/indexedColors replaces entry by entry.
var defaultPalette = [64]uint32{
	0x000000, 0xFFFFFF, 0xFF0000, 0x00FF00, 0x0000FF, 0xFFFF00, 0xFF00FF, 0x00FFFF,
	0x000000, 0xFFFFFF, 0xFF0000, 0x00FF00, 0x0000FF, 0xFFFF00, 0xFF00FF, 0x00FFFF,
	0x800000, 0x008000, 0x000080, 0x808000, 0x800080, 0x008080, 0xC0C0C0, 0x808080,
	0x9999FF, 0x993366, 0xFFFFCC, 0xCCFFFF, 0x660066, 0xFF8080, 0x0066CC, 0xCCCCFF,
	0x000080, 0xFF00FF, 0xFFFF00, 0x00FFFF, 0x800080, 0x800000, 0x008080, 0x0000FF,
	0x00CCFF, 0xCCFFFF, 0xCCFFCC, 0xFFFF99, 0x99CCFF, 0xFF99CC, 0xCC99FF, 0xFFCC99,
	0x3366FF, 0x33CCCC, 0x99CC00, 0xFFCC00, 0xFF9900, 0xFF6600, 0x666699, 0x969696,
	0x003366, 0x339966, 0x003300, 0x333300, 0x993300, 0x993366, 0x333399, 0x333333,
}

// The indexes past the palette that name system colours (MS-XLS,
// 2.5.161 Icv; the OOXML indexed colours share them): the window text,
// the window background, the comment background and text.
const (
	indexWindowText = 64
	indexWindowBack = 65
	indexNoteBack   = 80
	indexNoteText   = 81
)

// themeColorNames maps a SpreadsheetML theme colour index onto the theme's
// colour scheme (ECMA-376 Part 1, 18.8.19 theme: an index into the
// clrScheme). Excel and LibreOffice read indexes 0 and 1, and 2 and 3,
// swapped against the scheme's document order: 0 is lt1, 1 dk1.
var themeColorNames = [...]string{"lt1", "dk1", "lt2", "dk2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"}

// rgb returns the opaque colour of a 0xRRGGBB value.
func rgb(v uint32) css.Color {
	return css.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}
}

// color resolves a CT_Color element (ECMA-376 Part 1, 18.8.19): its theme
// index, else its ARGB rgb, else its palette index, with its tint. The
// order theme, rgb, indexed is Excel's and LibreOffice's when an element
// carries several. It reports false for auto, a missing element or an
// unknown index, which the caller resolves to its context's automatic
// colour.
func (r *reader) color(e *ooxml.Element) (css.Color, bool) {
	if e == nil {
		return css.Color{}, false
	}
	var c css.Color
	ok := false
	if v, has := e.Attr("theme"); has {
		if i, iok := ooxml.Int(v); iok && i >= 0 && i < len(themeColorNames) {
			c, ok = r.theme.Color(themeColorNames[i])
		}
	} else if v, has := e.Attr("rgb"); has {
		c, ok = argb(v)
	} else if v, has := e.Attr("indexed"); has {
		if i, iok := ooxml.Int(v); iok {
			c, ok = r.styles.indexed(i)
		}
	}
	if !ok {
		return css.Color{}, false
	}
	if v, has := e.Attr("tint"); has {
		if t, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			c = tint(c, t)
		}
	}
	return c, true
}

// argb parses an ST_UnsignedIntHex ARGB colour ("FF1F3864"); the alpha
// byte is ignored, as Excel and LibreOffice draw every cell colour
// opaque. Six digits read as RGB.
func argb(s string) (css.Color, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 8 {
		s = s[2:]
	}
	return ooxml.HexColor(s)
}

// tint lightens or darkens c by t in [-1, 1] in the HLS space
// (ECMA-376 Part 1, 18.8.19 tint): a negative tint scales the luminance
// down by 1+t, a positive one moves it the fraction t towards white.
func tint(c css.Color, t float64) css.Color {
	t = max(-1, min(1, t))
	h, s, l := toHSL(c)
	if t < 0 {
		l *= 1 + t
	} else {
		l = l*(1-t) + t
	}
	return fromHSL(h, s, max(0, min(1, l)), c.A)
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

// fromHSL converts hue, saturation and lightness back to a colour of
// alpha a.
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
