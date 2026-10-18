package typography

import "testing"

// TestGlyphWidth_CodeOutsideStatedWidths pins the width of a code a simple
// font's /Widths array does not cover: the /MissingWidth of its descriptor,
// 0 when it states none (ISO 32000-1, 9.8.1, Table 122). Only a font whose
// widths no /Widths array states falls back to its program and bounding box.
func TestGlyphWidth_CodeOutsideStatedWidths(t *testing.T) {
	t.Parallel()

	bbox := [4]float64{-100, -200, 1063, 900}
	for _, tc := range []struct {
		name string
		font *Font
		want float64
	}{
		{"no missing width", &Font{Subtype: "Type1", FirstChar: 48, Widths: []float64{444}, widthStated: []bool{true}, FontBBox: bbox}, 0},
		{"stated missing width", &Font{Subtype: "Type1", FirstChar: 48, Widths: []float64{444}, widthStated: []bool{true}, MissingWidth: 250, FontBBox: bbox}, 250},
		{"no widths array", &Font{Subtype: "Type1", FirstChar: 48, Widths: []float64{444}, FontBBox: bbox}, 1163},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.font.GlyphWidth(' '); got != tc.want {
				t.Fatalf("GlyphWidth(' ') = %v, want %v", got, tc.want)
			}
			if got := tc.font.GlyphWidth('0'); got != 444 {
				t.Fatalf("GlyphWidth('0') = %v, want 444", got)
			}
		})
	}
}
