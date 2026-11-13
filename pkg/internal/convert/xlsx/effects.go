package xlsx

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// effectList returns the effects of a drawing shape or picture whose
// properties are spPr and whose style reference is style (ECMA-376 Part
// 1, 20.1.8.26 effectLst, 20.1.4.2.7 effectRef), nil for none, and warns
// of the members litho does not draw. An effect list kept in an
// extension, as a14:hiddenEffects keeps the effects a shape had before
// they were switched off, is not the shape's: Excel prints none of it.
//
// # Corpus evidence
//
//   - poi/xlsx spreadsheet/47504: Excel prints the outer shadow of the
//     theme's effect style 2 that a shape's effectRef names, a 100 pixels
//     an inch bitmap under the vector shape.
//   - libreoffice/xlsx sc/tdf165655: Excel prints no shadow for the outer
//     shadows its shapes keep in a14:hiddenEffects.
func (r *reader) effectList(spPr, style *ooxml.Element) *css.OfficeEffects {
	l := ooxml.ParseEffectList(spPr, style, r.theme)
	for _, n := range l.Undrawn {
		r.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("a drawing's %s is not drawn", n))
	}
	if l.Effects == (css.OfficeEffects{}) {
		return nil
	}
	return &l.Effects
}

// lateWarnings gathers the warnings of the drawings' effects, which are
// drawn as the pages are laid out, after the reader is done. It is safe
// for concurrent use.
type lateWarnings struct {
	mu   sync.Mutex
	list []typeset.Warning
	seen map[string]bool
}

// lateKey addresses the lateWarnings of a conversion on its context.
type lateKey struct{}

// withLateWarnings returns a context that carries a new lateWarnings, and
// it.
func withLateWarnings(ctx context.Context) (context.Context, *lateWarnings) {
	l := &lateWarnings{seen: map[string]bool{}}
	return context.WithValue(ctx, lateKey{}, l), l
}

// add records a warning once; a nil l drops it.
func (l *lateWarnings) add(code warn.Code, effect warn.Effect, msg string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := code.String() + "\x00" + effect.String() + "\x00" + msg
	if l.seen[key] {
		return
	}
	l.seen[key] = true
	l.list = append(l.list, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// take returns the warnings gathered.
func (l *lateWarnings) take() []typeset.Warning {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.list
}

// underEffects returns the bitmaps of the effects of the leaves, drawn
// under them, for the anchor's box at rect: each leaf's reflection,
// shadow and glow, in the leaf's own transform. An effect left out is
// reported to late; an error that ends the conversion leaves every
// effect out, which the layout's own check of ctx then reports.
func underEffects(ctx context.Context, leaves []leaf, rect typeset.Rect, pixels *effect.Page, late *lateWarnings) []typeset.Fragment {
	var out []typeset.Fragment
	for _, l := range leaves {
		if l.fx == nil {
			continue
		}
		box := l.box(rect)
		var content []typeset.Fragment
		var paths []effect.Path
		if l.pic != nil {
			if l.pic.Source != nil {
				content = []typeset.Fragment{&typeset.Image{Rect: box, Source: l.pic.Source}}
			}
			paths = []effect.Path{{Ops: ooxml.RectPath(box).Ops}}
		} else {
			for _, f := range opaque(l.style.Paint(l.geom, box)) {
				p, ok := f.(*typeset.Path)
				if !ok {
					continue
				}
				content = append(content, p)
				if p.Fill != nil {
					rule := effect.NonZero
					if p.Fill.Rule == typeset.EvenOdd {
						rule = effect.EvenOdd
					}
					paths = append(paths, effect.Path{Ops: p.Ops, Rule: rule})
				}
				if p.Stroke != nil {
					paths = append(paths, effect.Path{Ops: p.Ops, Width: max(p.Stroke.Width, 0.1)})
				}
			}
		}
		frags, err := officefx.Under(ctx, &officefx.Source{Paths: paths, Bounds: box, Content: content}, l.fx, pixels)
		switch {
		case errors.Is(err, officefx.ErrPagePixels):
			late.add(warn.LimitReached, warn.Omitted, fmt.Sprintf("drawing effects past %d pixels are not drawn", effect.MaxPagePixels))
		case limits.Stops(err):
			return nil
		case err != nil:
			late.add(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("a drawing's effects are not drawn: %v", err))
		}
		if l.fx.SoftEdge > 0 {
			late.add(warn.ContentUnsupported, warn.Omitted, "a drawing's soft edge is not drawn")
		}
		if len(frags) == 0 {
			continue
		}
		if m, ok := transform(box, l.rot, l.flipH, l.flipV); ok {
			out = append(out, &typeset.Group{Transform: m, Content: frags})
		} else {
			out = append(out, frags...)
		}
	}
	return out
}
