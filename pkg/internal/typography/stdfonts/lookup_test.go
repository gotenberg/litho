package stdfonts

import "testing"

// TestIdentityGlyphMapFollowsPDFJSNames pins the gate PDF.js applies
// (fonts.js, fallbackToSystemFont): a font that is standard or mapped to a
// standard font once its name is normalised gets a table, every other name
// none. A subset prefix is not removed, as it is not in PDF.js.
func TestIdentityGlyphMapFollowsPDFJSNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"Arial",
		"ArialMT",
		"Arial,Bold",        // comma style suffix
		"Arial_Bold",        // underscore style suffix
		"Arial-BoldMT",      // standard map entry
		"Helvetica",         // standard 14
		"Times New Roman",   // white space dropped: TimesNewRoman
		"TimesNewRomanPSMT", // a serif face PDF.js reads in the same order
		"CourierNew",
		"Calibri",
		"Calibri,Bold",
		"Impact",          // non-standard map entry mapped to Helvetica
		"ArialBlack,Bold", // standard map entry
	} {
		if IdentityGlyphMap(name) == nil {
			t.Errorf("IdentityGlyphMap(%q) = nil, want a table", name)
		}
	}
	for _, name := range []string{
		"",
		"ABCDEF+Arial", // subset prefix
		"ArialMT,Bold", // ArialMT-Bold is in neither map
		"ComicSansMS",  // mapped, but to a font that is not standard
		"Tahoma",       // in neither map
		"MS-Gothic",    // mapped to MS Gothic, not standard
		"SomeFontNobodyShips",
	} {
		if got := IdentityGlyphMap(name); got != nil {
			t.Errorf("IdentityGlyphMap(%q) returned a table of %d entries, want nil", name, len(got))
		}
	}
}

// TestIdentityGlyphMapPicksTheSupplement pins the supplement order of
// fallbackToSystemFont: Arial Black first, then Calibri, else the standard map
// alone.
func TestIdentityGlyphMapPicksTheSupplement(t *testing.T) {
	t.Parallel()

	arial, black, calibri := IdentityGlyphMap("Arial"), IdentityGlyphMap("Arial-Black"), IdentityGlyphMap("Calibri")
	// GID 227 is ł in Arial Black only, GID 4 is A in Calibri only, and both
	// keep the standard map's ! elsewhere.
	if arial[227] == black[227] || black[227] != 'ł' {
		t.Errorf("GID 227: Arial %q, Arial Black %q, want the supplement's ł in Arial Black only", arial[227], black[227])
	}
	if arial[4] != '!' || black[4] != '!' || calibri[4] != 'A' {
		t.Errorf("GID 4: Arial %q, Arial Black %q, Calibri %q, want ! ! A", arial[4], black[4], calibri[4])
	}
	// The Cyrillic of the Arial order (GID 590 is Ф).
	if arial[590] != 'Ф' {
		t.Errorf("Arial GID 590 = %q, want Ф", arial[590])
	}
}

// TestPDFJSFontName pins the /FontName and /BaseFont choice of
// translateFont (evaluator.js).
func TestPDFJSFontName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ fontName, baseFont, want string }{
		{"Arial", "Arial", "Arial"},
		{"", "Arial", "Arial"},
		{"Arial", "Arial,Bold", "Arial,Bold"},                // the base font extends the font name
		{"wg09np", "Wingdings-Regular", "Wingdings-Regular"}, // only the base font is known
		{"Arial", "Helvetica", "Arial"},                      // both known: the font name wins
		{"Foo", "Bar", "Foo"},                                // neither known
	} {
		if got := PDFJSFontName(tc.fontName, tc.baseFont); got != tc.want {
			t.Errorf("PDFJSFontName(%q, %q) = %q, want %q", tc.fontName, tc.baseFont, got, tc.want)
		}
	}
}
