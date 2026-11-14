package odt

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// formula builds the node of the formula the draw:object obj holds (ODF
// 1.3, Part 3, 10.4.6.2 and 14.6), in a frame of geometry g whose
// svg:desc or svg:title is alt: a replaced box of the frame's size, its
// own size when the frame states none, drawing the formula fitted to it
// as Writer fits it, anchored and placed as any frame's picture. It is a
// Formula structure element whose alternate description is alt, else
// the formula's tokens in reading order (ISO 32000-1, 14.8.4.5). ok is
// false for an object that holds no formula; a formula that draws
// nothing, one that holds only its StarMath text among them, gives a nil
// node with a warning, and the frame's replacement picture is tried
// next.
//
// # Corpus evidence
//
//   - probe, odt: an as-character formula sits on its line as the frame
//     of a picture of its size and graphic style does, whatever the
//     settings' MathBaselineAlignment says: top to line puts the frame's
//     top at the line's, centre to text centres it on the text.
func (b *builder) formula(obj *odf.Element, g frameGeom, alt string) (n *layout.Node, ok bool) {
	f, err := odf.ReadFormula(b.ctx, b.pkg, obj)
	if err != nil {
		if b.stop(err) {
			return nil, true
		}
		b.warn("formula-malformed", warn.PartMalformed, warn.Omitted, "a formula does not parse and is left out: "+err.Error())
		return nil, true
	}
	if f == nil {
		return nil, false
	}
	for _, w := range f.Warnings {
		b.warn("formula-"+w.Code.String(), w.Code, w.Effect, w.Message)
	}
	if f.Root == nil {
		b.warn("formula-annotation", warn.ContentUnsupported, warn.Omitted, "a formula that holds only its StarMath text is not drawn")
		return nil, true
	}
	if b.fonts == nil {
		return nil, true
	}
	if b.formulas == nil {
		b.formulas = formula.NewSetter(b.fonts, nil)
	}
	fit, err := f.Lay(b.ctx, b.formulas, b.fonts)
	if err != nil {
		if b.stop(err) {
			return nil, true
		}
		b.warn("formula-layout", warn.ContentUnsupported, warn.Omitted, "a formula is left out: "+err.Error())
		return nil, true
	}
	if fit == nil {
		return nil, true
	}
	for _, w := range fit.Warnings {
		b.warn("formula-"+w.Code.String(), w.Code, w.Effect, w.Message)
	}
	w, h := fit.Size()
	if g.hasW && g.w > 0 {
		w = g.w
	}
	if g.hasH && g.h > 0 {
		h = g.h
	}
	st := b.frameBox(g)
	st.Display = css.Inline
	if alt == "" {
		alt = fit.AltText
	}
	pic := &layout.Picture{Width: w, Height: h, NoRatio: true, Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		return fit.Draw(r)
	}}
	return b.node(&layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Role: "Formula", Alt: alt}), true
}
