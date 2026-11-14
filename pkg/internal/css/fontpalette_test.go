package css

import (
	"context"
	"math"
	"reflect"
	"testing"
)

// TestFontPalette_Values checks the font-palette grammar (CSS Fonts 4, 9.1
// and 9.3): the keywords, a dashed identifier, palette-mix() with its
// percentages completed as color-mix()'s, inheritance, and the values
// refused.
func TestFontPalette_Values(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		decls string
		want  func(*FontPalette) bool
	}{
		{"font-palette: normal", func(p *FontPalette) bool { return p == nil }},
		{"font-palette: light", func(p *FontPalette) bool { return p != nil && p.Kind == PaletteLight }},
		{"font-palette: DARK", func(p *FontPalette) bool { return p != nil && p.Kind == PaletteDark }},
		{"font-palette: --Mine", func(p *FontPalette) bool { return p != nil && p.Kind == PaletteNamed && p.Name == "--Mine" }},
		{"font-palette: palette-mix(in srgb, --a 25%, light)", func(p *FontPalette) bool {
			return p != nil && p.Kind == PaletteMixed && p.Mix.From.Name == "--a" && p.Mix.To.Kind == PaletteLight &&
				math.Abs(p.Mix.Weight-0.75) < 1e-9 && p.Mix.Alpha == 1
		}},
		{"font-palette: palette-mix(in oklab, 20% dark, normal 30%)", func(p *FontPalette) bool {
			return p != nil && math.Abs(p.Mix.Weight-0.6) < 1e-9 && math.Abs(p.Mix.Alpha-0.5) < 1e-9
		}},
		{"font-palette: --a; font-palette: mine", func(p *FontPalette) bool { return p != nil && p.Name == "--a" }},
		{"font-palette: --a; font-palette: palette-mix(--a, --b)", func(p *FontPalette) bool { return p != nil && p.Name == "--a" }},
		{"font-palette: --a; font-palette: palette-mix(in srgb, --a 0%, --b 0%)", func(p *FontPalette) bool { return p != nil && p.Name == "--a" }},
	} {
		c := compute(t, `<body><div id=t><span id=k></span></div></body>`, `#t { `+tc.decls+` }`)
		if !tc.want(c.of("t").FontPalette) {
			t.Errorf("%s: font-palette %+v", tc.decls, c.of("t").FontPalette)
		}
		if c.of("k").FontPalette != c.of("t").FontPalette {
			t.Errorf("%s: not inherited", tc.decls)
		}
	}
}

// TestFontPaletteValues checks @font-palette-values (CSS Fonts 4, 9.2):
// the families, base-palette and override-colors descriptors, a
// descriptor that does not parse dropped, and the last rule of a name
// winning.
func TestFontPaletteValues(t *testing.T) {
	t.Parallel()
	sheet, err := Parse(context.Background(), []byte(`
@font-palette-values --a {
  font-family: "Fancy Font", Other;
  base-palette: 3;
  override-colors: 1 #ff0000, 4 rgb(0 0 255 / 50%);
}
@font-palette-values --b { font-family: X; base-palette: light; override-colors: 1 currentColor; }
@font-palette-values --b { font-family: Y; base-palette: dark; }
@font-palette-values --c { font-family: serif; base-palette: -1; }
@font-palette-values nodash { font-family: Z; }
`), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(Media{}, []Sheet{{Sheet: sheet}})
	all := r.FontPaletteValues("--a")
	if len(all) != 1 {
		t.Fatalf("--a: %d rules, want 1", len(all))
	}
	a := all[0]
	want := &PaletteValues{
		Name: "--a", Families: []string{"Fancy Font", "Other"}, Base: PaletteNamed, BaseIndex: 3,
		Overrides: []PaletteOverride{{1, Color{R: 1, A: 1}}, {4, Color{B: 1, A: 0.5}}},
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("--a = %+v, want %+v", a, want)
	}
	if b := r.FontPaletteValues("--b"); len(b) != 2 || b[1].Base != PaletteDark || !reflect.DeepEqual(b[1].Families, []string{"Y"}) || b[0].Overrides != nil {
		t.Errorf("--b = %+v, want both rules in order, the colour of currentColor dropped", b)
	}
	if c := r.FontPaletteValues("--c"); len(c) != 1 || c[0].Families != nil || c[0].Base != PaletteNormal {
		t.Errorf("--c = %+v, want no family and the default base", c)
	}
	if r.FontPaletteValues("nodash") != nil {
		t.Error("a rule named without a dashed identifier was kept")
	}
}

// TestPaletteMix_MixColors checks that a palette mix mixes two entries as
// color-mix() would in its space.
func TestPaletteMix_MixColors(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div id=t></div></body>`, `#t { font-palette: palette-mix(in srgb, --a 25%, --b 75%) }`)
	m := c.of("t").FontPalette.Mix
	got := m.MixColors(Color{R: 1, A: 1}, Color{B: 1, A: 1})
	if math.Abs(got.R-0.25) > 1e-6 || math.Abs(got.B-0.75) > 1e-6 || got.A != 1 {
		t.Errorf("mix = %+v, want 25%% red and 75%% blue", got)
	}
}
