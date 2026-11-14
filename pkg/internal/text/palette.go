package text

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/colorfont"
)

// PaletteBase names the palette of a colour face a [Palette] starts from.
type PaletteBase uint8

// The palettes a [Palette] starts from (CSS Fonts 4, 9.1 and 9.2).
const (
	// PaletteNormal is the face's first palette, its default (OpenType,
	// CPAL).
	PaletteNormal PaletteBase = iota
	// PaletteLight is the first palette the face marks as usable on a
	// light background, else its first.
	PaletteLight
	// PaletteDark is the first palette the face marks as usable on a dark
	// background, else its first.
	PaletteDark
	// PaletteIndex is the palette of [Palette.Index], else the first.
	PaletteIndex
)

// Palette selects and changes the colours the layered glyphs of a colour
// face are drawn in: CSS font-palette with the @font-palette-values rule
// it names (CSS Fonts 4, 9), or a palette-mix() of two palettes. A
// Palette is shared between styles and never modified once a style holds
// it.
type Palette struct {
	PaletteRule
	// Rules, when not nil, are the @font-palette-values rules of a named
	// palette, in cascade order: a face takes the last that lists its
	// family, or its first palette when none does, and the palette's own
	// rule is ignored.
	Rules []PaletteRule
	// From and To, when both are set, make the palette a mix of the two,
	// each entry the colour Mix gives of the entries of From and To; the
	// rest is then ignored.
	From, To *Palette
	Mix      func(a, b typeset.Color) typeset.Color
}

// PaletteRule is a palette a face may take: a base palette and the
// entries that replace some of its colours.
type PaletteRule struct {
	Base  PaletteBase
	Index int
	// Families are the families a rule of [Palette.Rules] applies to, as
	// an @font-palette-values rule names them, matched against a face's
	// declared family without regard to ASCII case.
	Families []string
	// Overrides replace entries of the base palette; an index past the
	// face's entries is ignored (CSS Fonts 4, 9.2, override-colors).
	Overrides []PaletteColor
}

// PaletteColor is one entry of a palette set by a [Palette].
type PaletteColor struct {
	Index int
	Color typeset.Color
}

// maxPaletteMixDepth caps the nesting of mixed palettes [Palette.colors]
// resolves. Anti-DoS guard, not spec-mandated: a palette-mix() nests
// within the colour nesting bound of the style, and real styles nest one
// or two.
const maxPaletteMixDepth = 16

// colors returns the colours of p for a face of declared family family
// and colour tables cf, entry by entry, nil when p draws the face's first
// palette unchanged.
func (p *Palette) colors(cf *colorfont.Font, family string) []typeset.Color {
	return p.resolve(cf, family, 0)
}

func (p *Palette) resolve(cf *colorfont.Font, family string, depth int) []typeset.Color {
	if p == nil || depth > maxPaletteMixDepth {
		return nil
	}
	if p.From != nil && p.To != nil && p.Mix != nil {
		a := p.From.resolve(cf, family, depth+1)
		b := p.To.resolve(cf, family, depth+1)
		if a == nil {
			a = toColors(cf.Palette(0))
		}
		if b == nil {
			b = toColors(cf.Palette(0))
		}
		out := make([]typeset.Color, min(len(a), len(b)))
		for i := range out {
			out[i] = p.Mix(a[i], b[i])
		}
		return out
	}
	r := &p.PaletteRule
	if p.Rules != nil {
		r = nil
		for i := len(p.Rules) - 1; i >= 0; i-- {
			if containsFold(p.Rules[i].Families, family) {
				r = &p.Rules[i]
				break
			}
		}
		if r == nil {
			return nil
		}
	}
	base := 0
	switch r.Base {
	case PaletteLight:
		base, _ = cf.PaletteForLight()
	case PaletteDark:
		base, _ = cf.PaletteForDark()
	case PaletteIndex:
		if r.Index >= 0 && r.Index < cf.NumPalettes() {
			base = r.Index
		}
	}
	if base == 0 && len(r.Overrides) == 0 {
		return nil
	}
	out := toColors(cf.Palette(base))
	for _, o := range r.Overrides {
		if o.Index >= 0 && o.Index < len(out) {
			out[o.Index] = o.Color
		}
	}
	return out
}

// containsFold reports whether list holds s without regard to ASCII case.
func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// toColors converts CPAL colours to the model's.
func toColors(in []colorfont.Color) []typeset.Color {
	out := make([]typeset.Color, len(in))
	for i, c := range in {
		out[i] = typeset.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: float64(c.A) / 255}
	}
	return out
}

// palette returns the palette a run of style s draws the layered glyphs of
// face fc in, whose program is cf, nil for its first palette: the
// [FontSettings.Palette] of s resolved against the face's palettes. A
// face without layered glyphs, or whose colour tables do not read, takes
// none.
func (st *layoutState) palette(fc *Face, cf *typeset.Face, s *Style) []typeset.Color {
	p := s.settings().Palette
	if p == nil || fc == nil || cf == nil || fc.colourTables()&colourLayers == 0 {
		return nil
	}
	tables, ok := st.colorTables[cf.Key]
	if !ok {
		var err error
		if tables, err = colorfont.Parse(cf.Program); err != nil {
			tables = nil
		}
		if st.colorTables == nil {
			st.colorTables = map[string]*colorfont.Font{}
		}
		st.colorTables[cf.Key] = tables
	}
	if tables == nil {
		return nil
	}
	return p.colors(tables, fc.DeclaredFamily())
}
