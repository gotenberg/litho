package text

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/colorfont"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestPalette_Colors locks how a palette resolves against a face of two
// palettes of two entries (CSS Fonts 4, 9): the base palette by index,
// the default for an index past the face's, the overrides, a family the
// rule does not list, and a mix entry by entry.
func TestPalette_Colors(t *testing.T) {
	colr, cpal := fontbuild.ColorLayers(map[uint16][]fontbuild.Layer{1: {{GID: 1, Palette: 0}}},
		[][][4]uint8{{{255, 0, 0, 255}, {0, 255, 0, 255}}, {{0, 0, 255, 255}, {0, 0, 0, 255}}})
	prog := fontbuild.TrueType{PostScriptName: "P", Family: "P", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 500}}, Extra: map[string][]byte{"COLR": colr, "CPAL": cpal}}.Build()
	cf, err := colorfont.Parse(prog)
	if err != nil {
		t.Fatal(err)
	}
	red, green, blue, black := typeset.Color{R: 1, A: 1}, typeset.Color{G: 1, A: 1}, typeset.Color{B: 1, A: 1}, typeset.Color{A: 1}
	white := typeset.Color{R: 1, G: 1, B: 1, A: 1}
	half := func(a, b typeset.Color) typeset.Color {
		return typeset.Color{R: (a.R + b.R) / 2, G: (a.G + b.G) / 2, B: (a.B + b.B) / 2, A: (a.A + b.A) / 2}
	}
	cases := []struct {
		name string
		p    *Palette
		want []typeset.Color
	}{
		{"normal", &Palette{}, nil},
		{"index", &Palette{PaletteRule: PaletteRule{Base: PaletteIndex, Index: 1}}, []typeset.Color{blue, black}},
		{"index past the palettes", &Palette{PaletteRule: PaletteRule{Base: PaletteIndex, Index: 7}}, nil},
		{"overrides", &Palette{PaletteRule: PaletteRule{Overrides: []PaletteColor{{1, white}, {9, white}}}}, []typeset.Color{red, white}},
		{"family listed", &Palette{Rules: []PaletteRule{{Base: PaletteIndex, Index: 1, Families: []string{"q", "MY FACE"}}}}, []typeset.Color{blue, black}},
		{"family not listed", &Palette{Rules: []PaletteRule{{Base: PaletteIndex, Index: 1, Families: []string{"q"}}}}, nil},
		{"last rule of the family", &Palette{Rules: []PaletteRule{
			{Base: PaletteIndex, Index: 1, Families: []string{"My Face"}},
			{Overrides: []PaletteColor{{0, white}}, Families: []string{"My Face"}},
			{Base: PaletteIndex, Index: 1, Families: []string{"other"}},
		}}, []typeset.Color{white, green}},
		{"light without types", &Palette{PaletteRule: PaletteRule{Base: PaletteLight}}, nil},
		{"mix", &Palette{From: &Palette{}, To: &Palette{PaletteRule: PaletteRule{Base: PaletteIndex, Index: 1}}, Mix: half}, []typeset.Color{half(red, blue), half(green, black)}},
	}
	for _, c := range cases {
		if got := c.p.colors(cf, "My Face"); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
