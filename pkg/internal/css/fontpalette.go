package css

import (
	"math"
	"strings"
)

// PaletteKind is the form of a [FontPalette].
type PaletteKind uint8

// The font-palette values (CSS Fonts 4, 9.1).
const (
	// PaletteNormal draws a colour font in its default palette.
	PaletteNormal PaletteKind = iota
	// PaletteLight draws it in the first palette it marks as usable on a
	// light background.
	PaletteLight
	// PaletteDark draws it in the first palette it marks as usable on a
	// dark background.
	PaletteDark
	// PaletteNamed draws it in the palette an @font-palette-values rule
	// of [FontPalette.Name] defines for its family.
	PaletteNamed
	// PaletteMixed draws it in a mix of two palettes, palette-mix().
	PaletteMixed
)

// FontPalette is a computed font-palette (CSS Fonts 4, 9.1): a keyword,
// the name of an @font-palette-values rule, or a palette-mix() of two
// palettes. [Style.FontPalette] is nil for normal. A FontPalette is shared
// between styles and never modified.
type FontPalette struct {
	Kind PaletteKind
	// Name is the dashed identifier of a named palette, as written.
	Name string
	// Mix is the mix of a mixed palette.
	Mix *PaletteMix
}

// PaletteMix is a palette-mix() (CSS Fonts 4, 9.3): each entry of the
// palette the colour of the entries of From and To mixed as color-mix()
// mixes two colours (CSS Color 5, 3).
type PaletteMix struct {
	From, To FontPalette
	// Weight is the share of To, from 0 to 1, and Alpha the factor the
	// mixed alpha takes when the percentages sum under 100%.
	Weight, Alpha float64
	space         colorSpace
	hue           hueMethod
}

// MixColors returns the mix of a, an entry of From, and b, the same entry
// of To, in the mix's interpolation space.
func (m *PaletteMix) MixColors(a, b Color) Color {
	out := interpolate(srgbColor(a.R, a.G, a.B, a.A), srgbColor(b.R, b.G, b.B, b.A), m.Weight, m.space, m.hue)
	out.alpha *= m.Alpha
	return out.paint()
}

// parseFontPalette parses a font-palette value (CSS Fonts 4, 9.1): normal,
// light, dark, a <dashed-ident> or a palette-mix(). normal is a nil
// palette.
func parseFontPalette(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	p, ok := fontPaletteOf(t, 0)
	if !ok {
		return nil, false
	}
	if p.Kind == PaletteNormal {
		return (*FontPalette)(nil), true
	}
	return &p, true
}

// fontPaletteOf parses one palette of a font-palette value, a palette-mix()
// of nested ones up to [maxColorDepth] deep.
func fontPaletteOf(t token, depth int) (FontPalette, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		switch k := lower(t.value); {
		case k == "normal":
			return FontPalette{}, true
		case k == "light":
			return FontPalette{Kind: PaletteLight}, true
		case k == "dark":
			return FontPalette{Kind: PaletteDark}, true
		case strings.HasPrefix(t.value, "--") && len(t.value) > 2:
			return FontPalette{Kind: PaletteNamed, Name: t.value}, true
		}
	case tFunction:
		if lower(t.value) == "palette-mix" && depth < maxColorDepth {
			return paletteMix(t.children, depth)
		}
	}
	return FontPalette{}, false
}

// paletteMix parses the arguments of palette-mix() (CSS Fonts 4, 9.3): a
// <color-interpolation-method>, then two palettes, each with an optional
// percentage from 0% to 100%, which complete one another as color-mix()'s
// do.
func paletteMix(args []token, depth int) (FontPalette, bool) {
	parts := splitCommas(args)
	if len(parts) != 3 {
		return FontPalette{}, false
	}
	space, hue, ok := parseInterpolation(parts[0])
	if !ok {
		return FontPalette{}, false
	}
	var pals [2]FontPalette
	var pcts [2]float64
	var given [2]bool
	for i, p := range parts[1:] {
		l := significant(p)
		if len(l) == 0 || len(l) > 2 {
			return FontPalette{}, false
		}
		pi := 0
		if len(l) == 2 {
			v, isPct := mixPercent(l[0])
			if isPct {
				pi = 1
			} else if v, isPct = mixPercent(l[1]); !isPct {
				return FontPalette{}, false
			}
			if v < 0 || v > 100 {
				return FontPalette{}, false
			}
			pcts[i], given[i] = v, true
		}
		if pals[i], ok = fontPaletteOf(l[pi], depth+1); !ok {
			return FontPalette{}, false
		}
	}
	switch {
	case !given[0] && !given[1]:
		pcts = [2]float64{50, 50}
	case !given[0]:
		pcts[0] = 100 - pcts[1]
	case !given[1]:
		pcts[1] = 100 - pcts[0]
	}
	sum := pcts[0] + pcts[1]
	// CSS Fonts 4, 9.3 follows color-mix(): percentages summing to zero
	// make the function invalid.
	if sum <= 0 {
		return FontPalette{}, false
	}
	m := &PaletteMix{From: pals[0], To: pals[1], Weight: pcts[1] / sum, Alpha: math.Min(sum, 100) / 100, space: space, hue: hue}
	return FontPalette{Kind: PaletteMixed, Mix: m}, true
}

// PaletteValues is one @font-palette-values rule (CSS Fonts 4, 9.2): the
// palette it names for the families it lists.
type PaletteValues struct {
	// Name is the rule's dashed identifier.
	Name string
	// Families are the families the palette applies to.
	Families []string
	// Base is the palette the rule starts from: [PaletteNormal] for the
	// font's default, [PaletteLight], [PaletteDark], or [PaletteNamed]
	// for the palette of index BaseIndex.
	Base      PaletteKind
	BaseIndex int
	// Overrides replace entries of the base palette, in order.
	Overrides []PaletteOverride
}

// PaletteOverride is one entry of override-colors: a palette index and
// its colour.
type PaletteOverride struct {
	Index int
	Color Color
}

// maxPaletteOverrides caps the entries of one override-colors descriptor.
// Anti-DoS guard, not spec-mandated: a CPAL palette counts at most 65,535
// entries and real rules override a handful.
const maxPaletteOverrides = 1 << 16

// parsePaletteValues parses an @font-palette-values rule (CSS Fonts 4,
// 9.2): its <dashed-ident> prelude and the font-family, base-palette and
// override-colors descriptors. A rule without a font-family is valid and
// applies to no family. A descriptor that does not parse is dropped; a
// colour of override-colors must be absolute, not currentColor.
func parsePaletteValues(prelude, block []token) (PaletteValues, bool) {
	p := significant(prelude)
	if len(p) != 1 || p[0].typ != tIdent || !strings.HasPrefix(p[0].value, "--") || len(p[0].value) <= 2 {
		return PaletteValues{}, false
	}
	pv := PaletteValues{Name: p[0].value}
	newListParser(block).declarations(func(d item) bool {
		if d.kind != itemDeclaration {
			return true
		}
		v := significant(d.value)
		switch lower(d.name) {
		case "font-family":
			if fams, ok := paletteFamilies(v); ok {
				pv.Families = fams
			}
		case "base-palette":
			t, ok := single(v)
			if !ok {
				return true
			}
			switch {
			case t.isIdent("light"):
				pv.Base, pv.BaseIndex = PaletteLight, 0
			case t.isIdent("dark"):
				pv.Base, pv.BaseIndex = PaletteDark, 0
			default:
				if n, ok := parseInteger(t); ok && n >= 0 {
					pv.Base, pv.BaseIndex = PaletteNamed, n
				}
			}
		case "override-colors":
			if o, ok := paletteOverrides(d.value); ok {
				pv.Overrides = o
			}
		}
		return true
	})
	return pv, true
}

// paletteFamilies parses the font-family descriptor of
// @font-palette-values: a comma list of family names, each a string or
// identifiers, no generic family among them (CSS Fonts 4, 9.2.1).
func paletteFamilies(v []token) ([]string, bool) {
	var out []string
	for _, part := range splitCommas(v) {
		p := significant(part)
		var words []string
		for _, t := range p {
			if t.typ != tIdent && (t.typ != tString || len(p) != 1) {
				return nil, false
			}
			words = append(words, t.value)
		}
		if len(words) == 0 {
			return nil, false
		}
		name := strings.Join(words, " ")
		if p[0].typ == tIdent && len(p) == 1 && genericFamilyKeyword(lower(name)) {
			return nil, false
		}
		out = append(out, name)
	}
	return out, len(out) > 0
}

// genericFamilyKeyword reports a generic family keyword, which the
// font-family descriptor does not take.
func genericFamilyKeyword(k string) bool {
	switch k {
	case "serif", "sans-serif", "cursive", "fantasy", "monospace", "system-ui", "math", "emoji", "fangsong",
		"ui-serif", "ui-sans-serif", "ui-monospace", "ui-rounded":
		return true
	}
	return false
}

// paletteOverrides parses override-colors (CSS Fonts 4, 9.2.3): a comma
// list of a non-negative <integer> and an absolute colour.
func paletteOverrides(v []token) ([]PaletteOverride, bool) {
	parts := splitCommas(v)
	if len(parts) == 0 || len(parts) > maxPaletteOverrides {
		return nil, false
	}
	out := make([]PaletteOverride, 0, len(parts))
	for _, part := range parts {
		p := significant(part)
		if len(p) != 2 {
			return nil, false
		}
		n, ok := parseInteger(p[0])
		if !ok || n < 0 {
			return nil, false
		}
		c, ok := parseColor(p[1])
		if !ok || c.dependsOnCurrent() {
			return nil, false
		}
		out = append(out, PaletteOverride{Index: n, Color: c.resolve(Color{A: 1}, false)})
	}
	return out, true
}

// paletteValuesRule is an @font-palette-values rule and its media
// condition.
type paletteValuesRule struct {
	values PaletteValues
	cond   *mediaCond
	layer  int
}

// FontPaletteValues returns the @font-palette-values rules of name whose
// media conditions match, in cascade order: for a family two of them
// list, the later wins (CSS Fonts 4, 9.2). nil means none. The result is
// shared and must not be modified.
func (r *Resolver) FontPaletteValues(name string) []*PaletteValues {
	return r.paletteValues[name]
}
