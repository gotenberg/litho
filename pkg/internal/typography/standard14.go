package typography

//go:generate go run ../../../cmd/gen-base14-bounds

import "github.com/gotenberg/litho/pkg/internal/typography/encodings"

// standard14Alternatives are the alternative names of standard fonts that PDF
// Reference 1.7, Appendix H, implementation note 62 (to section 5.5.1) says
// Acrobat viewers accept, spelled as [canonicalFontName] spells them. ISO
// 32000-1, 9.6.2.2 names only the fourteen; the alternatives are read as
// their standard font because producers write them without /Widths, and the
// viewers the note describes position their text with the standard metrics.
//
// # Corpus evidence
//
//   - govdocs: fonts named Arial, TimesNewRoman or CourierNew, plain or with
//     a comma style, and no /Widths were the only fonts a PDF/A-2b
//     remediation given Liberation programs refused for declaring no widths.
//     Read as their standard fonts, they take a program, and the residual of
//     ISO 19005-2, 6.2.11.4.1 t1 falls from 830 to 728.
var standard14Alternatives = map[string]string{
	"Arial":                    "Helvetica",
	"Arial-Bold":               "Helvetica-Bold",
	"Arial-Italic":             "Helvetica-Oblique",
	"Arial-BoldItalic":         "Helvetica-BoldOblique",
	"TimesNewRoman":            "Times-Roman",
	"TimesNewRoman-Bold":       "Times-Bold",
	"TimesNewRoman-Italic":     "Times-Italic",
	"TimesNewRoman-BoldItalic": "Times-BoldItalic",
	"CourierNew":               "Courier",
	"CourierNew-Bold":          "Courier-Bold",
	"CourierNew-Italic":        "Courier-Oblique",
	"CourierNew-BoldItalic":    "Courier-BoldOblique",
}

// standard14Name returns the standard font baseFont names, directly or by an
// alternative name, or "".
func standard14Name(baseFont string) string {
	name := canonicalFontName(baseFont)
	if alt, ok := standard14Alternatives[name]; ok {
		return alt
	}
	if isStandard14(name) {
		return name
	}
	return ""
}

// Standard14Width returns the advance width the Adobe Font Metrics give glyph
// in the standard font baseFont names, in glyph space units, and whether
// baseFont names one and that font carries the glyph. A subset prefix is
// dropped and a comma style suffix read as a hyphen one ("Helvetica,Bold" is
// Helvetica-Bold), and the alternative names of [standard14Alternatives]
// stand for their standard font. No other name does, since a font of another
// name is drawn with its own metrics. Every glyph of the four Courier fonts
// is 600 units wide.
func Standard14Width(baseFont, glyph string) (float64, bool) {
	name := standard14Name(baseFont)
	if isCourier(name) {
		return courierWidth, glyph != "" && glyph != ".notdef"
	}
	metrics, ok := standard14GlyphWidths[name]
	if !ok {
		return 0, false
	}
	w, ok := metrics[glyph]
	return float64(w), ok
}

// IsStandard14 reports whether baseFont names one of the standard 14 fonts,
// read as [Standard14Width] reads it.
func IsStandard14(baseFont string) bool {
	return standard14Name(baseFont) != ""
}

// Standard14GlyphBounds returns the bounding box the Adobe Font Metrics give
// glyph in the standard font baseFont names, as llx, lly, urx and ury in glyph
// space units, and whether baseFont names one and that font carries the
// glyph. baseFont is read as [Standard14Width] reads it. A glyph that draws
// nothing, such as space, has an all-zero box.
func Standard14GlyphBounds(baseFont, glyph string) ([4]float64, bool) {
	b, ok := standard14GlyphBounds[standard14Name(baseFont)][glyph]
	if !ok {
		return [4]float64{}, false
	}
	return [4]float64{float64(b[0]), float64(b[1]), float64(b[2]), float64(b[3])}, true
}

// Standard14CodeGlyph returns the name of the glyph code selects in the
// standard font baseFont names, drawn by a simple font dictionary whose
// /Encoding is WinAnsiEncoding, or that states none for Symbol and
// ZapfDingbats, which then use their built-in encodings (ISO 32000-1, 9.6.6.1
// and Annex D). It returns "" for a code the encoding leaves unused and for a
// baseFont that names no standard font.
func Standard14CodeGlyph(baseFont string, code byte) string {
	name := standard14Name(baseFont)
	if name == "" {
		return ""
	}
	if builtin, _, _ := builtinGlyphNames(name); builtin != nil {
		return builtin[code]
	}
	r, ok := encodings.WinAnsiEncoding[code]
	if !ok {
		return ""
	}
	// No-break space and soft hyphen show the space and hyphen glyphs: the
	// Adobe Glyph List has no name of their own the metrics carry.
	if glyph, ok := duplicateCodeGlyph[r]; ok {
		return glyph
	}
	return encodings.RuneToGlyph(r)
}
