package ods

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// formula returns the picture of the formula the draw:object obj holds
// (ODF 1.3, Part 3, 10.4.6.2 and 14.6), fitted to its frame as Calc
// fits it, and the formula's alternate description; ok is false for an
// object that holds no formula. A formula that draws nothing, one that
// holds only its StarMath text among them, gives a nil picture with a
// warning, and the frame's replacement picture is tried next.
//
// # Corpus evidence
//
//   - probe, ods: a² + b² laid out at 12 pt in an 8 by 2 cm frame of a
//     cell prints at 45.496 pt and 2.09 times wider still, filling the
//     frame.
func (r *reader) formula(obj *odf.Element) (pic *layout.Picture, alt string, ok bool) {
	f, err := odf.ReadFormula(r.ctx, r.pkg, obj)
	if err != nil {
		if r.stop(err) {
			return nil, "", true
		}
		r.warn("formula-malformed", warn.PartMalformed, warn.Omitted, "a formula does not parse and is left out: "+err.Error())
		return nil, "", true
	}
	if f == nil {
		return nil, "", false
	}
	for _, w := range f.Warnings {
		r.warn("formula-"+w.Code.String(), w.Code, w.Effect, w.Message)
	}
	if f.Root == nil {
		r.warn("formula-annotation", warn.ContentUnsupported, warn.Omitted, "a formula that holds only its StarMath text is not drawn")
		return nil, "", true
	}
	if r.fonts == nil {
		return nil, "", true
	}
	if r.mathSetter == nil {
		r.mathSetter = formula.NewSetter(r.fonts, nil)
	}
	fit, err := f.Lay(r.ctx, r.mathSetter, r.fonts)
	if err != nil {
		if r.stop(err) {
			return nil, "", true
		}
		r.warn("formula-layout", warn.ContentUnsupported, warn.Omitted, "a formula is left out: "+err.Error())
		return nil, "", true
	}
	if fit == nil {
		return nil, "", true
	}
	for _, w := range fit.Warnings {
		r.warn("formula-"+w.Code.String(), w.Code, w.Effect, w.Message)
	}
	return &layout.Picture{Width: 1, Height: 1, Vector: func(_ context.Context, rect typeset.Rect) []typeset.Fragment {
		return fit.Draw(rect)
	}}, fit.AltText, true
}
