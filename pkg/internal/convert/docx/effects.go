package docx

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// effectList returns the effects of a shape or a picture whose properties
// are spPr and whose style reference is style (ECMA-376 Part 1, 20.1.8.26
// effectLst, 20.1.4.2.7 effectRef), nil for none, and warns of the
// members litho does not draw and of a 3-D scene or shape, which it draws
// flat.
func (d *document) effectList(spPr, style *ooxml.Element) *css.OfficeEffects {
	l := ooxml.ParseEffectList(spPr, style, d.theme)
	for _, n := range l.Undrawn {
		d.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("a drawing's %s is not drawn", n))
	}
	if p := l.Effects.Preset; p != nil {
		if _, _, ok := officefx.PresetCopies(p, l.Effects.Rules); !ok {
			d.warn(warn.ContentUnsupported, warn.Omitted, "a drawing's preset shadow "+p.Name+" is not drawn")
			l.Effects.Preset = nil
		}
	}
	if threeD(spPr) {
		d.warn(warn.ContentUnsupported, warn.Approximated, "a drawing's 3-D bevel, extrusion or camera is drawn flat")
	}
	if l.Effects == (css.OfficeEffects{}) {
		return nil
	}
	return &l.Effects
}

// threeD reports a 3-D scene with a camera other than the flat front one,
// or a 3-D shape with a bevel or a depth (20.1.5.8 scene3d, 20.1.5.12
// sp3d).
func threeD(spPr *ooxml.Element) bool {
	if cam := spPr.Child(ooxml.NSA, "scene3d").Child(ooxml.NSA, "camera"); cam != nil {
		if p, _ := cam.Attr("prst"); p != "orthographicFront" || cam.Child(ooxml.NSA, "rot") != nil {
			return true
		}
	}
	sp3d := spPr.Child(ooxml.NSA, "sp3d")
	if sp3d == nil {
		return false
	}
	for _, name := range []string{"extrusionH", "contourW"} {
		if v, ok := sp3d.Attr(name); ok && v != "0" {
			return true
		}
	}
	return sp3d.Child(ooxml.NSA, "bevelT") != nil || sp3d.Child(ooxml.NSA, "bevelB") != nil
}

// withEffects returns the picture pic with the effects fx drawn around
// it: the reflection, shadow and glow under it, its silhouette the paths
// it paints, or its box for a raster picture, then the vector copies of
// its preset shadow ([officefx.PresetUnder]); a blur, a fill overlay or a
// soft edge turn it into one bitmap ([officefx.Content]); an inner shadow
// paints over its fills and under its lines. pic itself when fx is nil.
func (d *document) withEffects(pic *layout.Picture, fx *css.OfficeEffects) *layout.Picture {
	if fx == nil || pic == nil {
		return pic
	}
	out := &layout.Picture{Width: pic.Width, Height: pic.Height, NoNaturalWidth: pic.NoNaturalWidth, NoNaturalHeight: pic.NoNaturalHeight, NoRatio: pic.NoRatio}
	out.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
		var content []typeset.Fragment
		if pic.Vector != nil {
			content = pic.Vector(ctx, r)
		} else if pic.Source != nil {
			content = []typeset.Fragment{&typeset.Image{Rect: r, Source: pic.Source}}
		}
		if len(content) == 0 {
			return nil
		}
		src := &officefx.Source{Paths: silhouette(content, r, pic.Vector == nil), Bounds: r, Content: content}
		under, notes, err := officefx.Under(ctx, src, fx, &d.effectPixels)
		d.effectFailed(notes, err)
		if fx.Preset != nil {
			copies, _ := officefx.PresetUnder(src, fx.Preset, fx.Rules)
			under = append(under, copies...)
		}
		inner, notes, err := officefx.Inner(ctx, src, fx, &d.effectPixels)
		d.effectFailed(notes, err)
		whole, notes, err := officefx.Content(ctx, src, fx, &d.effectPixels)
		d.effectFailed(notes, err)
		if whole != nil {
			content = []typeset.Fragment{whole}
		}
		if inner != nil {
			content = overFills(content, inner)
		}
		return append(under, content...)
	}
	return out
}

// overFills returns content with f painted over its fills and under its
// first stroked path, the lines a shape paints after its fills.
func overFills(content []typeset.Fragment, f typeset.Fragment) []typeset.Fragment {
	i := len(content)
	for k, c := range content {
		if p, ok := c.(*typeset.Path); ok && p.Stroke != nil {
			i = k
			break
		}
	}
	out := make([]typeset.Fragment, 0, len(content)+1)
	out = append(out, content[:i]...)
	out = append(out, f)
	return append(out, content[i:]...)
}

// effectFailed settles what drawing a drawing's effects noted and its
// error: an effect left out or drawn coarser warns; an error that ends
// the conversion fails it, any other leaves the effect out with a
// warning.
func (d *document) effectFailed(notes officefx.Notes, err error) {
	if notes.Skipped {
		d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("drawing effects past %d pixels are not drawn", effect.MaxPagePixels))
	}
	if notes.Reduced {
		d.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("drawing effects past %d pixels a bitmap are drawn at a lower resolution", effect.MaxPixels))
	}
	switch {
	case err == nil:
	case limits.Stops(err):
		if d.err == nil {
			d.err = fmt.Errorf("drawing effects: %w", err)
		}
	default:
		d.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("a drawing's effects are not drawn: %v", err))
	}
}

// silhouette returns the outlines the fragments frags paint, in page
// points: each filled path by its rule, each stroked one at its width, a
// group's under its transform; a picture's box r when box is set, the
// silhouette of a raster picture.
func silhouette(frags []typeset.Fragment, r typeset.Rect, box bool) []effect.Path {
	if box {
		return []effect.Path{{Ops: ooxml.RectPath(r).Ops}}
	}
	var out []effect.Path
	var walk func(frags []typeset.Fragment, m typeset.Matrix, depth int)
	walk = func(frags []typeset.Fragment, m typeset.Matrix, depth int) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil {
					rule := effect.NonZero
					if v.Fill.Rule == typeset.EvenOdd {
						rule = effect.EvenOdd
					}
					out = append(out, effect.Path{Ops: mapOps(v.Ops, m), Rule: rule})
				}
				if v.Stroke != nil {
					out = append(out, effect.Path{Ops: mapOps(v.Ops, m), Width: max(v.Stroke.Width, 0.1)})
				}
			case *typeset.Image:
				out = append(out, effect.Path{Ops: mapOps(ooxml.RectPath(v.Rect).Ops, m)})
			case *typeset.Group:
				if depth < layout.MaxVectorDepth {
					t := v.Transform
					if t == (typeset.Matrix{}) {
						t = typeset.Identity
					}
					walk(v.Content, compose(m, t), depth+1)
				}
			}
		}
	}
	walk(frags, typeset.Identity, 0)
	return out
}

// mapOps returns ops mapped by m.
func mapOps(ops []typeset.PathOp, m typeset.Matrix) []typeset.PathOp {
	if m == typeset.Identity {
		return ops
	}
	out := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		out[i] = op
		for j := range op.P {
			p := op.P[j]
			out[i].P[j] = typeset.Point{X: float64(m.A*p.X) + float64(m.C*p.Y) + m.E, Y: float64(m.B*p.X) + float64(m.D*p.Y) + m.F}
		}
	}
	return out
}

// compose returns the map that applies b, then a.
func compose(a, b typeset.Matrix) typeset.Matrix {
	return typeset.Matrix{
		A: float64(a.A*b.A) + float64(a.C*b.B),
		B: float64(a.B*b.A) + float64(a.D*b.B),
		C: float64(a.A*b.C) + float64(a.C*b.D),
		D: float64(a.B*b.C) + float64(a.D*b.D),
		E: float64(a.A*b.E) + float64(a.C*b.F) + a.E,
		F: float64(a.B*b.E) + float64(a.D*b.F) + a.F,
	}
}
