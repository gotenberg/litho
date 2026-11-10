package typography

import "testing"

// TestStandard14GlyphBounds pins the glyph boxes the Adobe Font Metrics give,
// read through the names [Standard14Width] reads.
func TestStandard14GlyphBounds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		font, glyph string
		want        [4]float64
		ok          bool
	}{
		{"Helvetica", "H", [4]float64{77, 0, 646, 718}, true},
		{"Arial", "H", [4]float64{77, 0, 646, 718}, true},
		{"ABCDEF+Helvetica", "H", [4]float64{77, 0, 646, 718}, true},
		{"Helvetica", "space", [4]float64{}, true},
		{"Times-Roman", "g", [4]float64{28, -218, 470, 460}, true},
		{"Helvetica", "nosuchglyph", [4]float64{}, false},
		{"NotAStandardFont", "H", [4]float64{}, false},
	} {
		got, ok := Standard14GlyphBounds(tc.font, tc.glyph)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Standard14GlyphBounds(%q, %q) = %v, %v, want %v, %v", tc.font, tc.glyph, got, ok, tc.want, tc.ok)
		}
	}
}

// TestStandard14GlyphBoundsCoverEveryFont pins that the table holds every
// standard font and that every glyph the widths name has a box.
func TestStandard14GlyphBoundsCoverEveryFont(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"Courier", "Courier-Bold", "Courier-BoldOblique", "Courier-Oblique",
		"Helvetica", "Helvetica-Bold", "Helvetica-BoldOblique", "Helvetica-Oblique",
		"Symbol", "Times-Bold", "Times-BoldItalic", "Times-Italic", "Times-Roman", "ZapfDingbats",
	} {
		if len(standard14GlyphBounds[name]) == 0 {
			t.Errorf("no glyph boxes for %s", name)
		}
		for glyph := range standard14GlyphWidths[name] {
			if _, ok := standard14GlyphBounds[name][glyph]; !ok {
				t.Errorf("%s: %q has a width and no box", name, glyph)
			}
		}
	}
}

// TestStandard14CodeGlyph pins the glyph each code selects: through
// WinAnsiEncoding for a text font, through the built-in encoding for Symbol and
// ZapfDingbats.
func TestStandard14CodeGlyph(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		font string
		code byte
		want string
	}{
		{"Helvetica", 'H', "H"},
		{"Helvetica", 0xE9, "eacute"},
		{"Helvetica", 0x80, "Euro"},
		{"Helvetica", 0xA0, "space"},
		{"Helvetica", 0xAD, "hyphen"},
		{"Times-Roman", ' ', "space"},
		{"Symbol", 'a', "alpha"},
		{"ZapfDingbats", 'a', "a60"},
		{"NotAStandardFont", 'H', ""},
	} {
		if got := Standard14CodeGlyph(tc.font, tc.code); got != tc.want {
			t.Errorf("Standard14CodeGlyph(%q, %#x) = %q, want %q", tc.font, tc.code, got, tc.want)
		}
	}
}
