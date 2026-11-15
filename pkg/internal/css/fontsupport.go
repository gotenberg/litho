package css

import "slices"

// fontFormats are the format() hints the HTML converter reads a font file
// of (CSS Fonts 4, 4.3.1, Table 1), lowercased: TrueType, OpenType with
// glyf or CFF outlines, collections, WOFF and WOFF2, and the -variations
// forms of the older syntax, which name the same formats holding
// variations. embedded-opentype and svg are refused.
var fontFormats = []string{
	"collection", "opentype", "truetype", "woff", "woff2",
	"opentype-variations", "truetype-variations", "woff-variations", "woff2-variations",
}

// fontFormatKeywords are the <font-format> keywords (CSS Fonts 4,
// 4.3.1), which font-format() takes (CSS Conditional 5, 2.2).
var fontFormatKeywords = []string{"collection", "embedded-opentype", "opentype", "svg", "truetype", "woff", "woff2"}

// fontTechs are the tech() keywords litho supports (CSS Fonts 4, 4.3.1),
// lowercased: OpenType layout features, which the shaper applies,
// variations, which litho instances for TrueType outlines (a CFF2 face
// draws its default instance, with a warning), the colour glyphs of COLR
// version 0 layers and of CBDT and sbix bitmaps, which package colorfont
// draws, and palettes, which font-palette selects. COLR version 1, SVG
// glyphs, Graphite, AAT layout and incremental transfer are not.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's CSS.supports is true for font-tech()
//     of features-opentype, features-aat, variations, palettes,
//     color-COLRv0, color-COLRv1, color-CBDT and color-sbix, and false
//     for color-svg, features-graphite and incremental.
//   - wpt: the font-palette reftests of css/css-fonts draw the COLR
//     version 0 layers of a font an @font-face rule loads.
var fontTechs = []string{"features-opentype", "variations", "color-colrv0", "color-cbdt", "color-sbix", "palettes"}

// ReadsFontFormat reports whether litho reads a font file of the format a
// lowercased format() hint of an @font-face src names (CSS Fonts 4,
// 4.3.1). @supports font-format() answers from the same list.
func ReadsFontFormat(f string) bool { return slices.Contains(fontFormats, f) }

// SupportsFontTech reports whether litho supports the font technology a
// lowercased tech() keyword of an @font-face src names (CSS Fonts 4,
// 4.3.1). @supports font-tech() answers from the same list.
func SupportsFontTech(t string) bool { return slices.Contains(fontTechs, t) }
