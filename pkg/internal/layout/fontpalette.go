package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// palette returns the text stack's form of the font-palette p, nil for
// normal: a keyword, the palette of the @font-palette-values rule p names
// for the families the rule lists, or a mix of two palettes (CSS Fonts 4,
// 9). A name no rule defines draws as normal (CSS Fonts 4, 9.1).
func (e *engine) palette(p *css.FontPalette) *text.Palette {
	if p == nil {
		return nil
	}
	if tp, ok := e.palettes[p]; ok {
		return tp
	}
	tp := e.paletteOf(*p, 0)
	if e.palettes == nil {
		e.palettes = map[*css.FontPalette]*text.Palette{}
	}
	e.palettes[p] = tp
	return tp
}

// maxPaletteDepth caps the nesting of palette-mix() the engine resolves.
// Anti-DoS guard, not spec-mandated: the parser bounds the nesting by the
// colour nesting bound, and real styles nest once.
const maxPaletteDepth = 16

func (e *engine) paletteOf(p css.FontPalette, depth int) *text.Palette {
	switch p.Kind {
	case css.PaletteLight:
		return &text.Palette{PaletteRule: text.PaletteRule{Base: text.PaletteLight}}
	case css.PaletteDark:
		return &text.Palette{PaletteRule: text.PaletteRule{Base: text.PaletteDark}}
	case css.PaletteNamed:
		var rules []*css.PaletteValues
		if e.opts.PaletteValues != nil {
			rules = e.opts.PaletteValues(p.Name)
		}
		if len(rules) == 0 {
			return nil
		}
		tp := &text.Palette{}
		for _, v := range rules {
			r := text.PaletteRule{Families: v.Families}
			switch v.Base {
			case css.PaletteLight:
				r.Base = text.PaletteLight
			case css.PaletteDark:
				r.Base = text.PaletteDark
			case css.PaletteNamed:
				r.Base, r.Index = text.PaletteIndex, v.BaseIndex
			}
			for _, o := range v.Overrides {
				r.Overrides = append(r.Overrides, text.PaletteColor{Index: o.Index, Color: typeset.Color(o.Color)})
			}
			tp.Rules = append(tp.Rules, r)
		}
		return tp
	case css.PaletteMixed:
		if depth >= maxPaletteDepth || p.Mix == nil {
			return nil
		}
		m := p.Mix
		from, to := e.paletteOf(m.From, depth+1), e.paletteOf(m.To, depth+1)
		if from == nil {
			from = &text.Palette{}
		}
		if to == nil {
			to = &text.Palette{}
		}
		return &text.Palette{From: from, To: to, Mix: func(a, b typeset.Color) typeset.Color {
			return typeset.Color(m.MixColors(css.Color(a), css.Color(b)))
		}}
	}
	return nil
}
