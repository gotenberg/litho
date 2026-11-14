package xlsx

import (
	"math"
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
			c, ok = r.systemColors[themeColorNames[i]]
			if !ok {
				c, ok = r.theme.Color(themeColorNames[i])
			}
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

// The scales of Excel's HLS: hue, lightness and saturation run from 0 to
// hlsMax, a component from 0 to rgbMax.
const (
	hlsMax = 240
	rgbMax = 255
)

// tint lightens or darkens c by t in [-1, 1] as Excel computes a CT_Color
// tint (ECMA-376 Part 1, 18.8.19 tint): in whole-number HLS of 240 levels
// (hlsMax), its lightness L taken to the whole part of L·(1+t) for a
// negative tint, of L·(1-t) + 240 - the whole part of 240·(1-t) for a
// positive one, and back to whole components.
//
// # Corpus evidence
//
//   - libreoffice/xlsx, litho/xlsx, openxmlsdk/xlsx, poi/xlsx: of the 180
//     distinct theme colours and tints whose colour under one candidate
//     rule Excel's PDF paints, this rule gives 179, the lightness in
//     floating-point HSL 77; litho/xlsx theme_custom paints 7B2D8E under
//     tint 0.4 as 188,106,208, the floating-point HSL giving 189,108,208.
//     The one it misses (E0E9F4 at 0.4, openxmlsdk/xlsx
//     VendorRatingCriteriaWorkbook) is one level from the 235,240,248
//     Excel paints there.
func tint(c css.Color, t float64) css.Color {
	t = max(-1, min(1, t))
	h, l, s := toHLS(level(c.R), level(c.G), level(c.B))
	if t < 0 {
		l = int(float64(l) * (1 + t)) // fits int: at most hlsMax
	} else {
		keep := int(hlsMax * (1 - t))                             // fits int: at most hlsMax
		l = int(float64(float64(l)*(1-t)) + float64(hlsMax-keep)) // fits int: at most 2 hlsMax
	}
	l = max(0, min(hlsMax, l))
	r, g, b := fromHLS(h, l, s)
	return css.Color{R: float64(r) / rgbMax, G: float64(g) / rgbMax, B: float64(b) / rgbMax, A: c.A}
}

// level returns a component in [0, 1] as a whole number to rgbMax.
func level(v float64) int {
	return int(math.Round(max(0, min(1, v)) * rgbMax)) // fits int: at most rgbMax
}

// toHLS converts whole components to whole hue, lightness and saturation
// as Windows' RGB-to-HLS conversion rounds them (Microsoft Knowledge Base
// 29240); a grey has saturation 0 and the hue 2/3 of hlsMax.
func toHLS(r, g, b int) (h, l, s int) {
	hi, lo := max(r, g, b), min(r, g, b)
	l = ((hi+lo)*hlsMax + rgbMax) / (2 * rgbMax)
	if hi == lo {
		return hlsMax * 2 / 3, l, 0
	}
	d, sum := hi-lo, hi+lo
	if l <= hlsMax/2 {
		s = (d*hlsMax + sum/2) / sum
	} else {
		s = (d*hlsMax + (2*rgbMax-sum)/2) / (2*rgbMax - sum)
	}
	part := func(v int) int { return ((hi-v)*(hlsMax/6) + d/2) / d }
	switch hi {
	case r:
		h = part(b) - part(g)
	case g:
		h = hlsMax/3 + part(r) - part(b)
	default:
		h = 2*hlsMax/3 + part(g) - part(r)
	}
	return ((h % hlsMax) + hlsMax) % hlsMax, l, s
}

// fromHLS converts whole hue, lightness and saturation back to whole
// components, as Windows' HLS-to-RGB conversion rounds them; a grey
// rounds its lightness to the nearest component.
//
// # Corpus evidence
//
//   - libreoffice/xlsx, openxmlsdk/xlsx: Excel paints 000000 under tint
//     0.05 as 13,13,13, 12.75 rounded, and FFFFFF under tint -0.15 as
//     217,217,217, 216.75 rounded, where the conversion's truncation
//     gives 12 and 216.
func fromHLS(h, l, s int) (r, g, b int) {
	if s == 0 {
		v := (l*rgbMax + hlsMax/2) / hlsMax
		return v, v, v
	}
	var m2 int
	if l <= hlsMax/2 {
		m2 = (l*(hlsMax+s) + hlsMax/2) / hlsMax
	} else {
		m2 = l + s - (l*s+hlsMax/2)/hlsMax
	}
	m1 := 2*l - m2
	comp := func(hue int) int {
		hue = ((hue % hlsMax) + hlsMax) % hlsMax
		var v int
		switch {
		case hue < hlsMax/6:
			v = m1 + ((m2-m1)*hue+hlsMax/12)/(hlsMax/6)
		case hue < hlsMax/2:
			v = m2
		case hue < hlsMax*2/3:
			v = m1 + ((m2-m1)*(hlsMax*2/3-hue)+hlsMax/12)/(hlsMax/6)
		default:
			v = m1
		}
		return max(0, min(rgbMax, (v*rgbMax+hlsMax/2)/hlsMax))
	}
	return comp(h + hlsMax/3), comp(h), comp(h - hlsMax/3)
}

// systemColors returns the scheme colours of the theme root that name a
// system colour (ECMA-376 Part 1, 20.1.2.3.33 sysClr): the window text
// black and the window white, as LibreOffice reads them from the system
// rather than from their lastClr, which records what the writer's screen
// showed.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/cond_format_theme_color2.xlsx, whose theme's dk1 is the
//     window text with lastClr 00FF00, prints its text in theme colour 1
//     black.
func systemColors(root *ooxml.Element) map[string]css.Color {
	scheme := root.Find(ooxml.NSA, "clrScheme")
	out := map[string]css.Color{}
	for _, entry := range scheme.Kids() {
		sys := entry.Child(ooxml.NSA, "sysClr")
		if sys == nil {
			continue
		}
		switch attr(sys, "val") {
		case "windowText":
			out[entry.Local] = css.Color{A: 1}
		case "window":
			out[entry.Local] = css.Color{R: 1, G: 1, B: 1, A: 1}
		}
	}
	return out
}
