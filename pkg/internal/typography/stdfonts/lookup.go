package stdfonts

import (
	"regexp"
	"strings"
)

// arialBlackGIDToUnicode is the table PDF.js builds for a non-embedded Arial
// Black (fonts.js, fallbackToSystemFont): [pdfjsStandardFontGlyphs], overlaid
// with [pdfjsArialBlackGlyphs].
var arialBlackGIDToUnicode = overlayGlyphMaps(pdfjsStandardFontGlyphs, pdfjsArialBlackGlyphs)

// arialBlackRE and calibriRE are the tests fallbackToSystemFont applies to
// the font name to pick a supplement, in that order.
var (
	arialBlackRE = regexp.MustCompile(`(?i)Arial-?Black`)
	calibriRE    = regexp.MustCompile(`(?i)Calibri`)
)

// IdentityGlyphMap returns the glyph ID to Unicode table PDF.js reads a
// non-embedded TrueType CIDFont with, when an Identity-H or Identity-V CMap
// draws it, or nil when PDF.js reads that font with none.
//
// name is the name PDF.js identifies the font by ([PDFJSFontName]). PDF.js
// builds the table only for a font that is standard, or mapped to a
// standard one, once its name is normalised (fonts.js, fallbackToSystemFont:
// isStandardFont, isMappedToStandardFont): its standard map, overlaid with
// the Arial Black supplement for a name matching /Arial-?Black/i, or else
// the Calibri supplement for one matching /Calibri/i. A subset prefix is not
// removed, so "ABCDEF+Arial" names no standard font. Every caller must treat
// the result as read-only: the tables are shared.
func IdentityGlyphMap(name string) map[uint16]rune {
	if !mapsToStandardFont(NormalizeFontName(name)) {
		return nil
	}
	switch {
	case arialBlackRE.MatchString(name):
		return arialBlackGIDToUnicode
	case calibriRE.MatchString(name):
		return calibriGIDToUnicode
	}
	return pdfjsStandardFontGlyphs
}

// PDFJSFontName returns the name PDF.js identifies a font by, from the
// /FontName of its font descriptor and the /BaseFont of its font dictionary
// (a CIDFont's, for a composite font). PDF.js takes the /FontName, and the
// /BaseFont instead when the two differ and the /BaseFont either extends the
// /FontName ("Arial" and "Arial,Bold") or is a font name PDF.js knows where
// the /FontName is not (evaluator.js, translateFont). An empty /FontName
// gives the /BaseFont.
func PDFJSFontName(fontName, baseFont string) string {
	if fontName == "" {
		return baseFont
	}
	if fontName != baseFont && baseFont != "" &&
		(strings.HasPrefix(baseFont, fontName) || !isKnownFontName(fontName) && isKnownFontName(baseFont)) {
		return baseFont
	}
	return fontName
}

// NormalizeFontName mirrors PDF.js's normalizeFontName (fonts_utils.js):
// commas and underscores become hyphens, and white space is dropped.
func NormalizeFontName(name string) string {
	name = strings.NewReplacer(",", "-", "_", "-").Replace(name)
	return strings.Join(strings.Fields(name), "")
}

// serifRE is the test PDF.js's isSerifFont applies to a name its serif
// table does not list.
var serifRE = regexp.MustCompile(`(?i)serif`)

// IsSerifFont reports whether PDF.js takes the Times-Roman metrics rather
// than Helvetica's for a font whose widths it has to guess, as it does for a
// simple font with no /Widths whose name is no standard font's (evaluator.js,
// getBaseFontMetrics through isSerifFont): the name up to its first hyphen
// is a family the serif table lists ([pdfjsSerifFonts]), or contains
// "serif" in any case. PDF.js reads no /Flags for this choice, and its
// pattern makes "SansSerif" a serif face. name is the name PDF.js passes:
// the /BaseFont as written when the font has a font descriptor, and the
// [NormalizeFontName] form of it when it has none (translateFont).
func IsSerifFont(name string) bool {
	family, _, _ := strings.Cut(name, "-")
	return pdfjsSerifFonts[family] || serifRE.MatchString(family)
}

// mapsToStandardFont reports whether PDF.js treats the normalised name as a
// standard font, or as a font mapped to one.
func mapsToStandardFont(name string) bool {
	if _, ok := pdfjsStdFontMap[name]; ok {
		return true
	}
	mapped, ok := pdfjsNonStdFontMap[name]
	if !ok {
		return false
	}
	_, ok = pdfjsStdFontMap[mapped]
	return ok
}

// isKnownFontName mirrors PDF.js's isKnownFontName (standard_fonts.js).
func isKnownFontName(name string) bool {
	name = NormalizeFontName(name)
	_, std := pdfjsStdFontMap[name]
	_, nonStd := pdfjsNonStdFontMap[name]
	return std || nonStd || pdfjsSerifFonts[name] || pdfjsSymbolsFonts[name]
}
