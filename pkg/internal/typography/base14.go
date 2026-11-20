package typography

import "strings"

// fixedWidth returns the width every glyph of the standard font name advances
// by, and whether name is one of the fixed-pitch fonts, the four Courier
// fonts, whose AFM files give every glyph one width.
func fixedWidth(name string) (int16, bool) {
	w, ok := standard14FixedWidths[name]
	return w, ok
}

// stdFontMap maps common font names to their Standard 14 metric equivalents.
// It keys on the spelling [canonicalFontName] returns.
var stdFontMap = map[string]string{
	// Standard 14 identity mappings
	"Times-Roman": "Times-Roman", "Helvetica": "Helvetica", "Courier": "Courier",
	"Symbol": "Symbol", "ZapfDingbats": "ZapfDingbats",
	"Times-Bold": "Times-Bold", "Times-Italic": "Times-Italic", "Times-BoldItalic": "Times-BoldItalic",
	"Helvetica-Bold": "Helvetica-Bold", "Helvetica-Oblique": "Helvetica-Oblique", "Helvetica-BoldOblique": "Helvetica-BoldOblique",
	"Courier-Bold": "Courier-Bold", "Courier-Oblique": "Courier-Oblique", "Courier-BoldOblique": "Courier-BoldOblique",
	// Arial → Helvetica
	"Arial": "Helvetica", "ArialMT": "Helvetica",
	"Arial-Bold": "Helvetica-Bold", "Arial-BoldMT": "Helvetica-Bold",
	"Arial-Italic": "Helvetica-Oblique", "Arial-ItalicMT": "Helvetica-Oblique",
	"Arial-BoldItalic": "Helvetica-BoldOblique", "Arial-BoldItalicMT": "Helvetica-BoldOblique",
	"ArialNarrow": "Helvetica", "ArialNarrow-Bold": "Helvetica-Bold",
	"ArialNarrow-Italic": "Helvetica-Oblique", "ArialNarrow-BoldItalic": "Helvetica-BoldOblique",
	"ArialBlack": "Helvetica", "ArialBlack-Bold": "Helvetica-Bold",
	"ArialBlack-Italic": "Helvetica-Oblique", "ArialBlack-BoldItalic": "Helvetica-BoldOblique",
	// TimesNewRoman → Times-Roman
	"TimesNewRoman": "Times-Roman", "TimesNewRomanPS": "Times-Roman", "TimesNewRomanPSMT": "Times-Roman",
	"TimesNewRoman-Bold": "Times-Bold", "TimesNewRomanPS-Bold": "Times-Bold", "TimesNewRomanPS-BoldMT": "Times-Bold",
	"TimesNewRoman-Italic": "Times-Italic", "TimesNewRomanPS-Italic": "Times-Italic", "TimesNewRomanPS-ItalicMT": "Times-Italic",
	"TimesNewRoman-BoldItalic": "Times-BoldItalic", "TimesNewRomanPS-BoldItalic": "Times-BoldItalic", "TimesNewRomanPS-BoldItalicMT": "Times-BoldItalic",
	// CourierNew → Courier
	"CourierNew": "Courier", "CourierNewPS-BoldItalicMT": "Courier-BoldOblique",
	"CourierNew-Bold": "Courier-Bold", "CourierNewPS-BoldMT": "Courier-Bold",
	"CourierNew-Italic": "Courier-Oblique", "CourierNewPS-ItalicMT": "Courier-Oblique",
	"CourierNew-BoldItalic": "Courier-BoldOblique",
	// CenturyGothic → Helvetica (sans-serif fallback)
	"CenturyGothic": "Helvetica", "CenturyGothic-Bold": "Helvetica-Bold",
	"CenturyGothic-Italic": "Helvetica-Oblique", "CenturyGothic-BoldItalic": "Helvetica-BoldOblique",
	// Georgia → Times-Roman (serif fallback)
	"Georgia": "Times-Roman", "Georgia-Bold": "Times-Bold",
	"Georgia-Italic": "Times-Italic", "Georgia-BoldItalic": "Times-BoldItalic",
	// Verdana → Helvetica
	"Verdana": "Helvetica", "Verdana-Bold": "Helvetica-Bold",
	"Verdana-Italic": "Helvetica-Oblique", "Verdana-BoldItalic": "Helvetica-BoldOblique",
	// Tahoma → Helvetica
	"Tahoma": "Helvetica", "Tahoma-Bold": "Helvetica-Bold",
	// Trebuchet → Helvetica
	"TrebuchetMS": "Helvetica", "TrebuchetMS-Bold": "Helvetica-Bold",
	"TrebuchetMS-Italic": "Helvetica-Oblique", "TrebuchetMS-BoldItalic": "Helvetica-BoldOblique",
	// Calibri → Helvetica
	"Calibri": "Helvetica", "Calibri-Bold": "Helvetica-Bold",
	"Calibri-Italic": "Helvetica-Oblique", "Calibri-BoldItalic": "Helvetica-BoldOblique",
	// Cambria → Times-Roman
	"Cambria": "Times-Roman", "Cambria-Bold": "Times-Bold",
	"Cambria-Italic": "Times-Italic", "Cambria-BoldItalic": "Times-BoldItalic",
}

// canonicalFontName strips a subset prefix ("ABCDEF+Helvetica" becomes
// "Helvetica") and turns a comma style suffix into a hyphen one
// ("Helvetica,Bold" becomes "Helvetica-Bold").
func canonicalFontName(name string) string {
	if idx := strings.Index(name, "+"); idx >= 0 && idx <= 6 {
		name = name[idx+1:]
	}
	return strings.ReplaceAll(name, ",", "-")
}

// normalizeBase14Name returns the Standard 14 name whose metrics stand in for
// name: its [canonicalFontName] mapped through [stdFontMap], or the canonical
// name itself when the map has no entry.
func normalizeBase14Name(name string) string {
	name = canonicalFontName(name)
	if mapped, ok := stdFontMap[name]; ok {
		return mapped
	}

	return name
}

// isStandard14 returns true if the (normalized) font name is one of the 14
// standard base fonts.
func isStandard14(name string) bool {
	if _, ok := fixedWidth(name); ok {
		return true
	}
	_, ok := standard14GlyphWidths[name]
	return ok
}
